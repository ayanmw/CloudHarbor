package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

const precreateEvidencePrefix = "upload_precreate_evidence_v1:"

type precreateEvidence struct {
	AttemptID    string                             `json:"attempt_id"`
	IntentDigest string                             `json:"intent_digest"`
	Checkpoint   baiduapi.UploadPrecreateCheckpoint `json:"checkpoint"`
}
type precreateJournal struct {
	Intent       baiduapi.UploadPrecreateIntent
	IntentDigest string
	RapidReceipt *baiduapi.UploadCreateReceipt
	LastSequence int64
}

func validRapidReceipt(p baiduapi.UploadPrecreateIntent, r baiduapi.UploadCreateReceipt) bool {
	return r.Valid() && r.PlanDigest == p.Digest() && r.Receipt.Size == fmt.Sprint(p.Size) && path.Dir(r.Receipt.Path) == path.Dir(p.RemotePath)
}
func matchesPrecreate(p baiduapi.UploadSessionPlan, j precreateJournal) bool {
	i := j.Intent
	return j.RapidReceipt == nil && p.PrecreateDigest == j.IntentDigest && p.RemotePath == i.RemotePath && p.Size == i.Size && p.LocalSHA256 == i.LocalSHA256 && reflect.DeepEqual(p.BlockMD5, i.BlockMD5) && p.CredentialFingerprint == i.CredentialFingerprint && p.AppKeyFingerprint == i.AppKeyFingerprint && reflect.DeepEqual(p.AccountScope, i.AccountScope)
}
func (m *Manager) precreateEvidenceRecorder(item, attempt string) func(baiduapi.UploadPrecreateCheckpoint) error {
	var intent *baiduapi.UploadPrecreateIntent
	received := false
	return func(c baiduapi.UploadPrecreateCheckpoint) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !validID(item) || !validID(attempt) || m.activeAttempts[item].ID != attempt || m.activeAttempts[item].RunnerReturned {
			return errors.New("precreate outside active attempt")
		}
		if (c.Intent == nil) == (c.RapidReceipt == nil) {
			return errors.New("invalid precreate checkpoint")
		}
		state := "precreate_admitted"
		digest := ""
		sent := int64(0)
		if c.Intent != nil {
			if err := m.checkMutationAdmissionLocked(item); err != nil {
				return err
			}
			if intent != nil || !c.Intent.Valid() {
				return errors.New("invalid or duplicate precreate intent")
			}
			var target, relative string
			var size int64
			if err := m.db.QueryRow(`SELECT b.target,i.relative_path,i.size FROM items i JOIN batches b ON b.id=i.batch WHERE i.id=?`, item).Scan(&target, &relative, &size); err != nil {
				return err
			}
			if c.Intent.RemotePath != path.Join(target, relative) || c.Intent.Size != size {
				return errors.New("precreate item mismatch")
			}
			copy := *c.Intent
			copy.BlockMD5 = append([]string(nil), copy.BlockMD5...)
			copy.ObservedAtUTC = copy.ObservedAtUTC.UTC()
			if copy.AccountScope != nil {
				scope := *copy.AccountScope
				scope.CheckedAtUTC = scope.CheckedAtUTC.UTC()
				copy.AccountScope = &scope
			}
			c.Intent = &copy
			digest = copy.Digest()
		} else {
			if intent == nil || received || !validRapidReceipt(*intent, *c.RapidReceipt) {
				return errors.New("rapid receipt without matching intent")
			}
			copy := *c.RapidReceipt
			copy.ObservedAtUTC = copy.ObservedAtUTC.UTC()
			c.RapidReceipt = &copy
			digest = intent.Digest()
			state = "rapid_receipt_observed"
			sent = intent.Size
		}
		body, err := json.Marshal(precreateEvidence{attempt, digest, c})
		if err != nil {
			return err
		}
		if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,?,?,?,?)`, item, state, sent, now(), precreateEvidencePrefix+string(body)); err != nil {
			return m.evidencePersistenceFailureLocked(err)
		}
		if c.Intent != nil {
			intent = c.Intent
		} else {
			received = true
		}
		return nil
	}
}
func (m *Manager) readPrecreateJournal(item, attempt string) (precreateJournal, error) {
	var result precreateJournal
	invalid := errors.New("invalid precreate journal")
	if !validID(item) || !validID(attempt) {
		return result, invalid
	}
	var target, relative string
	var size int64
	if err := m.db.QueryRow(`SELECT b.target,i.relative_path,i.size FROM items i JOIN batches b ON b.id=i.batch WHERE i.id=?`, item).Scan(&target, &relative, &size); err != nil {
		return result, err
	}
	rows, err := m.db.Query(`SELECT sequence,error,recorded,state,sent FROM events WHERE item=? AND substr(error,1,?)=? ORDER BY sequence`, item, len(precreateEvidencePrefix), precreateEvidencePrefix)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var seq, sent int64
		var body, recorded, state string
		if err = rows.Scan(&seq, &body, &recorded, &state, &sent); err != nil {
			return precreateJournal{}, err
		}
		var e precreateEvidence
		if json.Unmarshal([]byte(strings.TrimPrefix(body, precreateEvidencePrefix)), &e) != nil || !validID(e.AttemptID) {
			return precreateJournal{}, invalid
		}
		if e.AttemptID != attempt {
			continue
		}
		if _, err = time.Parse(time.RFC3339Nano, recorded); err != nil || seq <= result.LastSequence {
			return precreateJournal{}, invalid
		}
		c := e.Checkpoint
		if (c.Intent == nil) == (c.RapidReceipt == nil) {
			return precreateJournal{}, invalid
		}
		if c.Intent != nil {
			if found || !c.Intent.Valid() || c.Intent.Digest() != e.IntentDigest || c.Intent.RemotePath != path.Join(target, relative) || c.Intent.Size != size || state != "precreate_admitted" || sent != 0 {
				return precreateJournal{}, invalid
			}
			found = true
			result.Intent = *c.Intent
			result.IntentDigest = e.IntentDigest
		} else {
			if !found || result.RapidReceipt != nil || e.IntentDigest != result.IntentDigest || !validRapidReceipt(result.Intent, *c.RapidReceipt) || state != "rapid_receipt_observed" || sent != size {
				return precreateJournal{}, invalid
			}
			result.RapidReceipt = c.RapidReceipt
		}
		result.LastSequence = seq
	}
	if err = rows.Err(); err != nil {
		return precreateJournal{}, err
	}
	if !found {
		return precreateJournal{}, sql.ErrNoRows
	}
	return result, nil
}
