package foldersync

import (
	"database/sql"
	"encoding/json"
	"path"
	"sort"
	"time"
)

type Change struct {
	Side     string    `json:"side"`
	Action   string    `json:"action"`
	Old      *Entry    `json:"old"`
	New      *Entry    `json:"new"`
	Detected time.Time `json:"detected_at_utc"`
}

// ObservedChanges describes identities observed since the successful baseline,
// independently of the concrete operations chosen to converge the directories.
func ObservedChanges(base, now Snapshot) []Change {
	result := []Change{}
	at := time.Now().UTC()
	for side, sets := range map[string][2][]Entry{"local": {base.Local, now.Local}, "remote": {base.Remote, now.Remote}} {
		old, current := index(sets[0]), index(sets[1])
		ids := map[string][]*Entry{}
		for _, e := range current {
			ids[e.ID] = append(ids[e.ID], e)
		}
		oldCount := map[string]int{}
		for _, e := range old {
			oldCount[e.ID]++
		}
		used := map[string]bool{}
		for _, before := range old {
			var after *Entry
			if oldCount[before.ID] == 1 && len(ids[before.ID]) == 1 {
				after = ids[before.ID][0]
			} else {
				after = current[before.Path]
			}
			if after == nil {
				result = append(result, Change{side, "deleted", before, nil, at})
				continue
			}
			used[after.Path] = true
			action := ""
			switch {
			case before.ID != after.ID:
				action = "replaced"
			case path.Dir(before.Path) != path.Dir(after.Path) && path.Base(before.Path) != path.Base(after.Path):
				action = "moved_and_renamed"
			case path.Dir(before.Path) != path.Dir(after.Path):
				action = "moved"
			case before.Path != after.Path:
				action = "renamed"
			case !sameContent(before, after) || before.Mtime != after.Mtime || before.MD5 != after.MD5:
				action = "modified"
			}
			if action != "" {
				result = append(result, Change{side, action, before, after, at})
			}
		}
		for p, after := range current {
			if !used[p] {
				result = append(result, Change{side, "added", nil, after, at})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Side != result[j].Side {
			return result[i].Side < result[j].Side
		}
		p := func(c Change) string {
			if c.New != nil {
				return c.New.Path
			}
			return c.Old.Path
		}
		return p(result[i]) < p(result[j])
	})
	return result
}
func (m *Manager) Changes(run string, offset int) ([]Change, int, error) {
	var count int
	err := m.db.QueryRow(`SELECT coalesce(json_array_length(evidence),0) FROM sync_events WHERE run_id=? AND kind='detected_changes' ORDER BY sequence LIMIT 1`, run).Scan(&count)
	if err == sql.ErrNoRows {
		return []Change{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := m.db.Query(`SELECT j.value FROM sync_events e,json_each(e.evidence) j WHERE e.run_id=? AND e.kind='detected_changes' ORDER BY cast(j.key AS INTEGER) LIMIT 100 OFFSET ?`, run, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []Change{}
	for rows.Next() {
		var raw string
		var c Change
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, 0, err
		}
		result = append(result, c)
	}
	return result, count, rows.Err()
}
