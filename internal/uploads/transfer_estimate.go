package uploads

import (
	"fmt"
	"math/big"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

type uploadAttemptSnapshot struct {
	ID, State      string
	Revision       uint64
	RunnerReturned bool
}

func (m *Manager) beginUploadAttempt(item, attempt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeAttempts == nil {
		m.activeAttempts = map[string]uploadAttemptSnapshot{}
	}
	m.activeAttempts[item] = uploadAttemptSnapshot{ID: attempt, State: "hashing", Revision: 1}
}
func (m *Manager) setUploadAttemptPhase(item, attempt, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setUploadAttemptPhaseLocked(item, attempt, state)
}
func (m *Manager) setUploadAttemptPhaseLocked(item, attempt, state string) {
	if current, ok := m.activeAttempts[item]; ok && current.ID == attempt {
		current.State = state
		current.Revision++
		m.activeAttempts[item] = current
	}
}
func (m *Manager) finishUploadAttempt(item, attempt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeAttempts[item].ID == attempt {
		delete(m.activeAttempts, item)
	}
}
func (m *Manager) snapshotUploadAttempts() map[string]uploadAttemptSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]uploadAttemptSnapshot{}
	if m.ctx.Err() != nil {
		return result
	}
	for item, value := range m.activeAttempts {
		if m.isPauseRequested(item) {
			continue
		}
		result[item] = value
	}
	return result
}

type CurrentTransferEstimate struct {
	BasisTimedPayloadBytes string `json:"basis_timed_payload_bytes"`
	BasisActiveNanos       string `json:"basis_active_nanos"`
	AttemptID              string `json:"attempt_id"`
	PlannedPayloadBytes    string `json:"planned_payload_bytes"`
	ConfirmedPayloadBytes  string `json:"confirmed_payload_bytes"`
	RemainingPayloadBytes  string `json:"remaining_payload_bytes"`
	EstimatedRequestNanos  string `json:"estimated_request_nanos"`
	BasisSampleCount       string `json:"basis_sample_count"`
}
type attemptTransferAccumulator struct {
	id                 string
	totals             transferAccumulator
	planned            *int64
	partCount          int
	confirmed          int64
	incomplete, failed bool
	parts              map[int]bool
}

func (a *attemptTransferAccumulator) add(e uploadTransferEvidence) error {
	s := e.Sample
	if a.id != e.AttemptID {
		*a = attemptTransferAccumulator{id: e.AttemptID, parts: map[int]bool{}}
	}
	if a.parts[s.PartIndex] {
		return fmt.Errorf("duplicate transfer observation")
	}
	a.parts[s.PartIndex] = true
	a.totals.add(s.PayloadBytesRead, s.ActiveNanos)
	if s.PlannedPayloadBytes == nil {
		a.incomplete = true
		return nil
	}
	if a.planned != nil && (*a.planned != *s.PlannedPayloadBytes || a.partCount != s.PlannedPartCount) {
		return fmt.Errorf("inconsistent attempt plan")
	}
	value := *s.PlannedPayloadBytes
	a.planned = &value
	a.partCount = s.PlannedPartCount
	if !s.PartConfirmed {
		a.failed = true
	} else {
		a.confirmed += s.PartPayloadBytes
	}
	if a.confirmed > value || len(a.parts) > a.partCount {
		return fmt.Errorf("attempt exceeds planned payload")
	}
	return nil
}
func (a *attemptTransferAccumulator) estimate(live uploadAttemptSnapshot) *CurrentTransferEstimate {
	if a.id == "" || a.id != live.ID || live.State != "uploading" || a.incomplete || a.failed || a.planned == nil || a.totals.nanos.Sign() <= 0 || a.totals.timedPayload.Sign() <= 0 {
		return nil
	}
	remaining := *a.planned - a.confirmed
	if remaining <= 0 {
		return nil
	}
	// Use the unrounded ratio and round ETA up; no old attempts or wall gaps.
	numerator := new(big.Int).Mul(big.NewInt(remaining), &a.totals.nanos)
	numerator.Add(numerator, new(big.Int).Sub(&a.totals.timedPayload, big.NewInt(1)))
	eta := numerator.Quo(numerator, &a.totals.timedPayload)
	return &CurrentTransferEstimate{AttemptID: a.id, BasisTimedPayloadBytes: a.totals.timedPayload.String(), BasisActiveNanos: a.totals.nanos.String(), PlannedPayloadBytes: big.NewInt(*a.planned).String(), ConfirmedPayloadBytes: big.NewInt(a.confirmed).String(), RemainingPayloadBytes: big.NewInt(remaining).String(), EstimatedRequestNanos: eta.String(), BasisSampleCount: a.totals.count.String()}
}

// Legacy observations have no plan fields and remain valid historical samples.
func validTransferPlan(s baiduapi.UploadTransferSample) bool {
	if s.PlannedPayloadBytes == nil {
		return s.PlannedPartCount == 0 && s.PartPayloadBytes == 0 && !s.PartConfirmed
	}
	return s.PlannedPartCount > 0 && s.PlannedPartCount <= 10000 && *s.PlannedPayloadBytes >= 0 && *s.PlannedPayloadBytes <= int64(s.PlannedPartCount)*baiduapi.UploadBlockSize && s.PartPayloadBytes >= 0 && s.PartPayloadBytes <= baiduapi.UploadBlockSize && s.PartPayloadBytes <= *s.PlannedPayloadBytes && s.PayloadBytesRead <= s.PartPayloadBytes
}
