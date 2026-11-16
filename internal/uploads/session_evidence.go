package uploads

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

const sessionEvidencePrefix = "upload_session_evidence_v1:"

type uploadSessionEvidence struct {
	AttemptID  string                           `json:"attempt_id"`
	PlanDigest string                           `json:"plan_digest"`
	Checkpoint baiduapi.UploadSessionCheckpoint `json:"checkpoint"`
}

// The observer is synchronous and attempt-scoped. Commit before admitting the
// next remote operation; no item state/byte count is inferred from this journal.
func (m *Manager) sessionEvidenceRecorder(item, attempt string) func(baiduapi.UploadSessionCheckpoint) error {
	var plan *baiduapi.UploadSessionPlan
	var durableDigest string
	createAdmitted, createReceived := false, false
	neededParts := map[int]bool{}
	confirmed := map[int]bool{}
	admitted := map[int]bool{}
	return func(checkpoint baiduapi.UploadSessionCheckpoint) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !validID(item) || !validID(attempt) || m.activeAttempts[item].ID != attempt || m.activeAttempts[item].RunnerReturned {
			return errors.New("upload session observation is outside its active attempt")
		}
		kinds := 0
		for _, present := range []bool{checkpoint.Plan != nil, checkpoint.Part != nil, checkpoint.Admission != nil, checkpoint.CreateAdmission != nil, checkpoint.CreateReceipt != nil} {
			if present {
				kinds++
			}
		}
		if kinds != 1 {
			return errors.New("invalid upload session checkpoint")
		}
		if checkpoint.Admission != nil || checkpoint.CreateAdmission != nil {
			if err := m.checkMutationAdmissionLocked(item); err != nil {
				return err
			}
		}
		if plan == nil && checkpoint.Plan == nil {
			journal, err := m.readSessionJournal(item, attempt)
			if err != nil {
				return err
			}
			plan = &journal.Plan
			durableDigest = journal.PlanDigest
			for _, index := range plan.NeededParts {
				neededParts[index] = true
			}
			for _, index := range journal.ConfirmedParts {
				confirmed[index] = true
				admitted[index] = true
			}
			for _, index := range journal.PendingParts {
				admitted[index] = true
			}
			createAdmitted = journal.CreateAdmission != nil
			createReceived = journal.CreateReceipt != nil
		}
		state := "part_confirmed"
		var sent int64
		digest := ""
		if checkpoint.Plan != nil {
			if plan != nil || !checkpoint.Plan.Valid() {
				return errors.New("invalid or repeated upload session plan")
			}
			if checkpoint.Plan.PrecreateDigest != "" {
				journal, err := m.readPrecreateJournal(item, attempt)
				if err != nil {
					return err
				}
				if !matchesPrecreate(*checkpoint.Plan, journal) {
					return errors.New("session does not match durable precreate")
				}
			}
			var target, relative string
			var size int64
			if err := m.db.QueryRow(`SELECT b.target,i.relative_path,i.size FROM items i JOIN batches b ON b.id=i.batch WHERE i.id=?`, item).Scan(&target, &relative, &size); err != nil {
				return err
			}
			if checkpoint.Plan.RemotePath != path.Join(target, relative) || checkpoint.Plan.Size != size {
				return errors.New("upload session plan does not match the staged item")
			}
			copy := *checkpoint.Plan
			copy.BlockMD5 = append([]string(nil), copy.BlockMD5...)
			copy.NeededParts = append([]int{}, copy.NeededParts...)
			copy.ObservedAtUTC = copy.ObservedAtUTC.UTC()
			if copy.AccountScope != nil {
				scope := *copy.AccountScope
				scope.CheckedAtUTC = scope.CheckedAtUTC.UTC()
				copy.AccountScope = &scope
			}
			checkpoint.Plan = &copy
			state = "session_observed"
			digest = copy.Digest()
		} else if checkpoint.CreateAdmission != nil || checkpoint.CreateReceipt != nil {
			if plan == nil || plan.CreateAdmissionVersion != 1 || len(confirmed) != len(neededParts) {
				return errors.New("create checkpoint requires confirmed planned parts")
			}
			digest = durableDigest
			if checkpoint.CreateAdmission != nil {
				a := *checkpoint.CreateAdmission
				a.ObservedAtUTC = a.ObservedAtUTC.UTC()
				if !a.Valid() || a.PlanDigest != digest || createAdmitted {
					return errors.New("invalid create admission")
				}
				checkpoint.CreateAdmission = &a
				state = "create_admitted"
			} else {
				r := *checkpoint.CreateReceipt
				r.ObservedAtUTC = r.ObservedAtUTC.UTC()
				if !r.Valid() || r.PlanDigest != digest || !createAdmitted || createReceived || r.Receipt.Size != fmt.Sprint(plan.Size) || path.Dir(r.Receipt.Path) != path.Dir(plan.RemotePath) {
					return errors.New("invalid create receipt")
				}
				checkpoint.CreateReceipt = &r
				state = "create_receipt_observed"
				sent = plan.Size
			}
		} else {
			if createAdmitted {
				return errors.New("part after create admission")
			}
			var receipt baiduapi.UploadPartReceipt
			if checkpoint.Part != nil {
				receipt = *checkpoint.Part
			} else {
				receipt = baiduapi.UploadPartReceipt(*checkpoint.Admission)
			}
			receipt.ObservedAtUTC = receipt.ObservedAtUTC.UTC()
			if checkpoint.Part != nil {
				checkpoint.Part = &receipt
			} else {
				admission := baiduapi.UploadPartAdmission(receipt)
				checkpoint.Admission = &admission
				state = "part_admitted"
			}
			if plan == nil || !receipt.Valid() || receipt.PlanDigest != durableDigest || receipt.Index >= len(plan.BlockMD5) || receipt.MD5 != plan.BlockMD5[receipt.Index] || confirmed[receipt.Index] {
				return errors.New("upload part receipt does not match the durable session plan")
			}
			if !neededParts[receipt.Index] {
				return errors.New("upload part was not in the provider plan")
			}
			if checkpoint.Admission != nil && admitted[receipt.Index] || checkpoint.Part != nil && !admitted[receipt.Index] {
				return errors.New("upload part admission/receipt sequence is invalid")
			}
			digest = receipt.PlanDigest
			if checkpoint.Part != nil {
				sent = min(int64(baiduapi.UploadBlockSize), plan.Size-int64(receipt.Index)*baiduapi.UploadBlockSize)
			}
		}
		body, err := json.Marshal(uploadSessionEvidence{AttemptID: attempt, PlanDigest: digest, Checkpoint: checkpoint})
		if err != nil {
			return err
		}
		if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,?,?,?,?)`, item, state, sent, now(), sessionEvidencePrefix+string(body)); err != nil {
			return m.evidencePersistenceFailureLocked(err)
		}
		if checkpoint.Plan != nil {
			plan = checkpoint.Plan
			durableDigest = digest
			for _, index := range plan.NeededParts {
				neededParts[index] = true
			}
		} else if checkpoint.CreateAdmission != nil {
			createAdmitted = true
		} else if checkpoint.CreateReceipt != nil {
			createReceived = true
		} else if checkpoint.Part != nil {
			confirmed[checkpoint.Part.Index] = true
		} else {
			admitted[checkpoint.Admission.Index] = true
		}
		return nil
	}
}
