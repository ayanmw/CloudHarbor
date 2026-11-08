package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"time"
)

func loadCloudRefreshGlobalItem(tx *sql.Tx, account string, id int64) (*CloudInventoryRefreshChangeItem, error) {
	var item CloudInventoryRefreshChangeItem
	var directory int
	var observed sql.NullInt64
	err := tx.QueryRow(`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, id).Scan(&item.FsID, &item.Path, &item.Name, &directory, &item.Size, &item.ProviderMtime, &item.ContentMD5, &observed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Directory = directory == 1
	if observed.Valid {
		t := time.UnixMicro(observed.Int64).UTC()
		item.ObservedAtUTC = &t
	}
	return &item, nil
}

type CacheSyncChange struct {
	ID       string                           `json:"fs_id"`
	Action   string                           `json:"action"`
	Old      *CloudInventoryRefreshChangeItem `json:"old"`
	New      *CloudInventoryRefreshChangeItem `json:"new"`
	Evidence []string                         `json:"source_change_sequences"`
	Recorded time.Time                        `json:"recorded_at_utc"`
	Observed time.Time                        `json:"cloud_observed_at_utc"`
}
type CacheSyncView struct {
	RunID    string            `json:"run_id"`
	Status   string            `json:"status"`
	Root     string            `json:"root"`
	Changes  []CacheSyncChange `json:"changes"`
	RawCount int               `json:"raw_count"`
	Derived  bool              `json:"derived_from_immutable_observations"`
}

type cacheObservation struct {
	Old, New *CloudInventoryRefreshChangeItem
}

func reduceCacheObservations(observations []cacheObservation) (old, newItem *CloudInventoryRefreshChangeItem, ambiguous bool) {
	if len(observations) == 0 {
		return nil, nil, true
	}
	old, newItem = observations[0].Old, observations[len(observations)-1].New
	// Missing source and present destination can be published in either order.
	if len(observations) == 2 {
		var missing, present *CloudInventoryRefreshChangeItem
		for _, o := range observations {
			if o.Old != nil && o.New == nil {
				missing = o.Old
			}
			if o.Old == nil && o.New != nil {
				present = o.New
			}
		}
		if missing != nil && present != nil && missing.FsID == present.FsID && missing.Path != present.Path {
			return missing, present, false
		}
	}
	for i := 1; i < len(observations); i++ {
		previous, current := observations[i-1].New, observations[i].Old
		if previous == nil || current == nil || previous.FsID != current.FsID || previous.Path != current.Path || previous.Directory != current.Directory || previous.Size != current.Size || previous.ProviderMtime != current.ProviderMtime || previous.ContentMD5 != current.ContentMD5 {
			return old, newItem, true
		}
	}
	return old, newItem, old == nil && newItem == nil
}

// CacheSyncChanges correlates immutable directory observations by stable fs_id.
// It does not rewrite old evidence or claim to be a provider operation receipt.
func (s *Store) CacheSyncChanges(account, run string) (CacheSyncView, error) {
	view := CacheSyncView{RunID: run, Changes: []CacheSyncChange{}, Derived: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.QueryRow(`SELECT status,root_path FROM cloud_inventory_refresh_runs WHERE account_fingerprint=? AND run_id=?`, account, run).Scan(&view.Status, &view.Root); err != nil {
		return view, err
	}
	rows, err := s.db.Query(`SELECT change_sequence,old_snapshot,new_snapshot,recorded_at_utc,cloud_observed_at_utc FROM cloud_inventory_refresh_changes WHERE account_fingerprint=? AND run_id=? ORDER BY change_sequence LIMIT 10001`, account, run)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	groups := map[int64]*CacheSyncChange{}
	observations := map[int64][]cacheObservation{}
	for rows.Next() {
		view.RawCount++
		if view.RawCount > 10000 {
			return view, fmt.Errorf("本轮超过10000条原始变化，请按目录查看原始分页明细，未返回部分汇总")
		}
		var seq, at, observed int64
		var oldRaw, newRaw sql.NullString
		if err = rows.Scan(&seq, &oldRaw, &newRaw, &at, &observed); err != nil {
			return view, err
		}
		var old, newItem *CloudInventoryRefreshChangeItem
		if oldRaw.Valid {
			old = &CloudInventoryRefreshChangeItem{}
			if err = json.Unmarshal([]byte(oldRaw.String), old); err != nil {
				return view, err
			}
		}
		if newRaw.Valid {
			newItem = &CloudInventoryRefreshChangeItem{}
			if err = json.Unmarshal([]byte(newRaw.String), newItem); err != nil {
				return view, err
			}
		}
		id := int64(0)
		if old != nil {
			id = old.FsID
		} else if newItem != nil {
			id = newItem.FsID
		}
		if id <= 0 {
			return view, fmt.Errorf("缓存变化身份损坏")
		}
		group := groups[id]
		if group == nil {
			group = &CacheSyncChange{ID: strconv.FormatInt(id, 10), Evidence: []string{}}
			groups[id] = group
		}
		observations[id] = append(observations[id], cacheObservation{old, newItem})
		group.Evidence = append(group.Evidence, strconv.FormatInt(seq, 10))
		group.Recorded = time.UnixMicro(at).UTC()
		group.Observed = time.UnixMicro(observed).UTC()
	}
	if err = rows.Err(); err != nil {
		return view, err
	}
	for id, g := range groups {
		var ambiguous bool
		g.Old, g.New, ambiguous = reduceCacheObservations(observations[id])
		switch {
		case ambiguous:
			g.Action = "multiple_observations"
		case g.Old == nil:
			g.Action = "added"
		case g.New == nil:
			g.Action = "deleted"
		case g.Old.FsID != g.New.FsID:
			g.Action = "replaced"
		case path.Dir(g.Old.Path) != path.Dir(g.New.Path) && g.Old.Name != g.New.Name:
			g.Action = "moved_and_renamed"
		case path.Dir(g.Old.Path) != path.Dir(g.New.Path):
			g.Action = "moved"
		case g.Old.Name != g.New.Name:
			g.Action = "renamed"
		default:
			g.Action = "modified"
		}
		if view.Status != "complete" && g.Action == "deleted" {
			g.Action = "absence_observed"
		}
		view.Changes = append(view.Changes, *g)
	}
	sort.Slice(view.Changes, func(i, j int) bool {
		a, b := view.Changes[i], view.Changes[j]
		ap, bp := "", ""
		if a.New != nil {
			ap = a.New.Path
		} else if a.Old != nil {
			ap = a.Old.Path
		}
		if b.New != nil {
			bp = b.New.Path
		} else if b.Old != nil {
			bp = b.Old.Path
		}
		return ap < bp
	})
	return view, nil
}
