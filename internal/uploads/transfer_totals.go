package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
)

const transferSamplePrefix = "upload_transfer_sample_v1:"

type TransferTotals struct {
	SampleCount           string  `json:"sample_count"`
	PayloadBytesRead      string  `json:"payload_bytes_read"`
	TimedPayloadBytesRead string  `json:"timed_payload_bytes_read"`
	ActiveNanos           string  `json:"active_nanos"`
	ZeroDurationSamples   string  `json:"zero_duration_samples"`
	AverageBytesPerSecond *string `json:"average_bytes_per_second"`
}
type ItemTransferTotals struct {
	CurrentEstimate *CurrentTransferEstimate `json:"current_estimate,omitempty"`
	ItemID          string                   `json:"item_id"`
	Path            string                   `json:"path"`
	TransferTotals
}
type BatchTransferTotals struct {
	BatchID       string               `json:"batch_id"`
	ObservedAtUTC string               `json:"observed_at_utc"`
	TotalItems    int                  `json:"total_items"`
	Items         []ItemTransferTotals `json:"items"`
	TransferTotals
}
type transferAccumulator struct{ count, payload, timedPayload, nanos, zero big.Int }

func (a *transferAccumulator) add(bytes, nanos int64) {
	a.count.Add(&a.count, big.NewInt(1))
	a.payload.Add(&a.payload, big.NewInt(bytes))
	a.nanos.Add(&a.nanos, big.NewInt(nanos))
	if nanos > 0 {
		a.timedPayload.Add(&a.timedPayload, big.NewInt(bytes))
	} else {
		a.zero.Add(&a.zero, big.NewInt(1))
	}
}
func (a *transferAccumulator) totals() TransferTotals {
	out := TransferTotals{SampleCount: a.count.String(), PayloadBytesRead: a.payload.String(), TimedPayloadBytesRead: a.timedPayload.String(), ActiveNanos: a.nanos.String(), ZeroDurationSamples: a.zero.String()}
	if a.nanos.Sign() > 0 {
		rate := new(big.Int).Mul(&a.timedPayload, big.NewInt(1000000000))
		rate.Quo(rate, &a.nanos)
		value := rate.String()
		out.AverageBytesPerSecond = &value
	}
	return out
}

func (m *Manager) transferStatistics(batch string) (BatchTransferTotals, error) {
	out := BatchTransferTotals{BatchID: batch, Items: []ItemTransferTotals{}}
	liveBefore := m.snapshotUploadAttempts()
	tx, err := m.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT (SELECT COUNT(*) FROM items WHERE batch=b.id) FROM batches b WHERE id=?`, batch).Scan(&out.TotalItems); err != nil {
		return out, err
	}
	rows, err := tx.Query(`SELECT e.item,i.relative_path,e.sent,e.error,i.state FROM events e JOIN items i ON i.id=e.item WHERE i.batch=? AND substr(e.error,1,?)=? ORDER BY e.sequence`, batch, len(transferSamplePrefix), transferSamplePrefix)
	if err != nil {
		return out, err
	}
	total := &transferAccumulator{}
	sums := map[string]*transferAccumulator{}
	paths := map[string]string{}
	states := map[string]string{}
	attempts := map[string]*attemptTransferAccumulator{}
	for rows.Next() {
		var item, path, message, state string
		var bytes int64
		if err = rows.Scan(&item, &path, &bytes, &message, &state); err != nil {
			rows.Close()
			return out, err
		}
		var evidence uploadTransferEvidence
		if err = json.Unmarshal([]byte(strings.TrimPrefix(message, transferSamplePrefix)), &evidence); err != nil || !validID(evidence.AttemptID) || !validTransferSample(evidence.Sample) || bytes != evidence.Sample.PayloadBytesRead {
			rows.Close()
			return out, fmt.Errorf("invalid stored transfer observation")
		}
		if sums[item] == nil {
			sums[item] = &transferAccumulator{}
			paths[item] = path
		}
		// Only live attempts need per-part ETA bookkeeping; retaining every old
		// file's part map would scale with all historical blocks in a large batch.
		if live, ok := liveBefore[item]; ok && live.State == "uploading" && live.ID == evidence.AttemptID && state == "uploading" {
			if attempts[item] == nil {
				attempts[item] = &attemptTransferAccumulator{}
			}
			if err = attempts[item].add(evidence); err != nil {
				rows.Close()
				return out, err
			}
		}
		states[item] = state
		sums[item].add(bytes, evidence.Sample.ActiveNanos)
		total.add(bytes, evidence.Sample.ActiveNanos)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	liveAfter := m.snapshotUploadAttempts()
	keys := make([]string, 0, len(sums))
	for item := range sums {
		keys = append(keys, item)
	}
	sort.Strings(keys)
	for _, item := range keys {
		var estimate *CurrentTransferEstimate
		if before, ok := liveBefore[item]; ok && before.State == "uploading" && attempts[item] != nil && liveAfter[item] == before && states[item] == "uploading" {
			estimate = attempts[item].estimate(before)
		}
		out.Items = append(out.Items, ItemTransferTotals{ItemID: item, Path: paths[item], TransferTotals: sums[item].totals(), CurrentEstimate: estimate})
	}
	out.TransferTotals = total.totals()
	out.ObservedAtUTC = now()
	return out, nil
}

func (m *Manager) HandleTransferStatistics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	batch := r.PathValue("batch")
	if !validID(batch) || r.URL.RawQuery != "" {
		http.Error(w, "传输统计请求无效", 400)
		return
	}
	result, err := m.transferStatistics(batch)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "上传传输统计不可读", 500)
		}
		return
	}
	write(w, result)
}
