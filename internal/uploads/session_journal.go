package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

// This is a local journal classification, never a remote-resume authorization.
type uploadSessionJournal struct {
	Plan                                             baiduapi.UploadSessionPlan
	CreateTracked                                    bool
	CreateAdmission                                  *baiduapi.UploadCreateAdmission
	CreateReceipt                                    *baiduapi.UploadCreateReceipt
	PlanDigest                                       string
	NeverAdmittedParts, PendingParts, ConfirmedParts []int
	LastSequence                                     int64
}

func (m *Manager) readSessionJournal(item, attempt string) (uploadSessionJournal, error) {
	return m.readSessionJournalChain(item, attempt, map[string]bool{})
}
func (m *Manager) readSessionJournalChain(item, attempt string, visited map[string]bool) (uploadSessionJournal, error) {
	result := uploadSessionJournal{}
	invalid := errors.New("upload session journal is incomplete or inconsistent")
	if !validID(item) || !validID(attempt) {
		return result, invalid
	}
	if visited[attempt] {
		return result, invalid
	}
	visited[attempt] = true
	bridge, bridgeSequence, bridgeErr := m.readResumeBridge(item, attempt)
	if bridgeErr != nil && !errors.Is(bridgeErr, sql.ErrNoRows) {
		return result, bridgeErr
	}
	var inherited *uploadSessionJournal
	if bridgeErr == nil {
		source, err := m.readSessionJournalChain(item, bridge.Admission.SourceAttemptID, visited)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, invalid
			}
			return result, err
		}
		a := bridge.Admission
		if !source.CreateTracked || source.CreateAdmission != nil || len(source.PendingParts) != 0 || source.LastSequence != a.SourceSequence || source.PlanDigest != a.SourcePlanDigest || !slices.Equal(source.NeverAdmittedParts, a.RemainingParts) || baiduapi.ValidateUploadSessionScope(source.Plan, a.CurrentScope) != nil {
			return result, invalid
		}
		inherited = &source
	}
	var target, relative string
	var size int64
	if err := m.db.QueryRow(`SELECT b.target,i.relative_path,i.size FROM items i JOIN batches b ON b.id=i.batch WHERE i.id=?`, item).Scan(&target, &relative, &size); err != nil {
		return result, err
	}
	rows, err := m.db.Query(`SELECT sequence,error,recorded,state,sent FROM events WHERE item=? AND substr(error,1,?)=? ORDER BY sequence`, item, len(sessionEvidencePrefix), sessionEvidencePrefix)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	found := false
	var planSequence int64
	needed := map[int]bool{}
	admitted := map[int]bool{}
	confirmed := map[int]bool{}
	if inherited != nil {
		found = true
		result.Plan = inherited.Plan
		result.PlanDigest = inherited.PlanDigest
		result.CreateTracked = inherited.CreateTracked
		result.LastSequence = bridgeSequence
		for _, index := range result.Plan.NeededParts {
			needed[index] = true
		}
		for _, index := range inherited.ConfirmedParts {
			confirmed[index] = true
			admitted[index] = true
		}
	}
	for rows.Next() {
		var sequence int64
		var body, recorded, state string
		var sent int64
		if err = rows.Scan(&sequence, &body, &recorded, &state, &sent); err != nil {
			return uploadSessionJournal{}, err
		}
		var evidence uploadSessionEvidence
		if json.Unmarshal([]byte(strings.TrimPrefix(body, sessionEvidencePrefix)), &evidence) != nil || !validID(evidence.AttemptID) {
			return uploadSessionJournal{}, invalid
		}
		if evidence.AttemptID != attempt {
			continue
		}
		if _, err = time.Parse(time.RFC3339Nano, recorded); err != nil || sequence <= result.LastSequence {
			return uploadSessionJournal{}, invalid
		}
		c := evidence.Checkpoint
		kinds := 0
		for _, present := range []bool{c.Plan != nil, c.Part != nil, c.Admission != nil, c.CreateAdmission != nil, c.CreateReceipt != nil} {
			if present {
				kinds++
			}
		}
		if kinds != 1 {
			return uploadSessionJournal{}, invalid
		}
		if c.Plan != nil {
			if state != "session_observed" || sent != 0 || found || !c.Plan.Valid() || c.Plan.RemotePath != path.Join(target, relative) || c.Plan.Size != size || c.Plan.Digest() != evidence.PlanDigest {
				return uploadSessionJournal{}, invalid
			}
			found = true
			planSequence = sequence
			result.Plan = *c.Plan
			result.CreateTracked = c.Plan.CreateAdmissionVersion == 1
			result.PlanDigest = evidence.PlanDigest
			for _, index := range c.Plan.NeededParts {
				needed[index] = true
			}
		} else if c.CreateAdmission != nil || c.CreateReceipt != nil {
			if !found || !result.CreateTracked || len(confirmed) != len(needed) || evidence.PlanDigest != result.PlanDigest {
				return uploadSessionJournal{}, invalid
			}
			if c.CreateAdmission != nil {
				a := c.CreateAdmission
				if !a.Valid() || a.PlanDigest != result.PlanDigest || result.CreateAdmission != nil || state != "create_admitted" || sent != 0 {
					return uploadSessionJournal{}, invalid
				}
				result.CreateAdmission = a
			} else {
				r := c.CreateReceipt
				if !r.Valid() || r.PlanDigest != result.PlanDigest || result.CreateAdmission == nil || result.CreateReceipt != nil || state != "create_receipt_observed" || sent != size || r.Receipt.Size != fmt.Sprint(size) || path.Dir(r.Receipt.Path) != path.Dir(result.Plan.RemotePath) {
					return uploadSessionJournal{}, invalid
				}
				result.CreateReceipt = r
			}
		} else {
			if result.CreateAdmission != nil {
				return uploadSessionJournal{}, invalid
			}
			if !found {
				return uploadSessionJournal{}, invalid
			}
			var part baiduapi.UploadPartReceipt
			if c.Part != nil {
				part = *c.Part
			} else {
				part = baiduapi.UploadPartReceipt(*c.Admission)
			}
			if !part.Valid() || part.PlanDigest != result.PlanDigest || evidence.PlanDigest != result.PlanDigest || part.Index >= len(result.Plan.BlockMD5) || part.MD5 != result.Plan.BlockMD5[part.Index] || !needed[part.Index] || confirmed[part.Index] {
				return uploadSessionJournal{}, invalid
			}
			if c.Admission != nil {
				if state != "part_admitted" || sent != 0 || admitted[part.Index] {
					return uploadSessionJournal{}, invalid
				}
				admitted[part.Index] = true
			} else {
				if state != "part_confirmed" || sent != min(int64(baiduapi.UploadBlockSize), result.Plan.Size-int64(part.Index)*baiduapi.UploadBlockSize) || !admitted[part.Index] {
					return uploadSessionJournal{}, invalid
				}
				confirmed[part.Index] = true
			}
		}
		result.LastSequence = sequence
	}
	if err = rows.Err(); err != nil {
		return uploadSessionJournal{}, err
	}
	if !found {
		return uploadSessionJournal{}, sql.ErrNoRows
	}
	if inherited == nil && result.Plan.PrecreateDigest != "" {
		if err = rows.Close(); err != nil {
			return uploadSessionJournal{}, err
		}
		precreate, readErr := m.readPrecreateJournal(item, attempt)
		if readErr != nil {
			if errors.Is(readErr, sql.ErrNoRows) {
				return uploadSessionJournal{}, invalid
			}
			return uploadSessionJournal{}, readErr
		}
		if !matchesPrecreate(result.Plan, precreate) || precreate.LastSequence >= planSequence {
			return uploadSessionJournal{}, invalid
		}
	}
	// Preserve provider order; wall-clock timestamps do not establish ordering.
	for _, index := range result.Plan.NeededParts {
		if confirmed[index] {
			result.ConfirmedParts = append(result.ConfirmedParts, index)
		} else if admitted[index] {
			result.PendingParts = append(result.PendingParts, index)
		} else {
			result.NeverAdmittedParts = append(result.NeverAdmittedParts, index)
		}
	}
	return result, nil
}
