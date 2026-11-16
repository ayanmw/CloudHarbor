package uploads

import (
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestTransferEstimateRequiresLiveMatchingAttemptAndCurrentPlan(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m != nil {
			_ = m.Close()
		}
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "10"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.update(item, "uploading", 0, ""); err != nil {
		t.Fatal(err)
	}
	plan := int64(10)
	sample := baiduapi.UploadTransferSample{PlannedPayloadBytes: &plan, PlannedPartCount: 2, PartPayloadBytes: 5, PartConfirmed: true, PayloadBytesRead: 5, ActiveNanos: 1000000000, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	first, second := strings.Repeat("a", 32), strings.Repeat("b", 32)
	m.beginUploadAttempt(item, first)
	if err = m.recordTransferSample(item, first, sample); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		result, err := m.transferStatistics(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		estimate := result.Items[0].CurrentEstimate
		if want == "" {
			if estimate != nil {
				t.Fatalf("invented live ETA: %+v", estimate)
			}
			return
		}
		if estimate == nil || estimate.EstimatedRequestNanos != want {
			t.Fatalf("wrong ETA: %+v", estimate)
		}
	}
	m.finishUploadAttempt(item, first)
	check("")
	m.beginUploadAttempt(item, first)
	m.setUploadAttemptPhase(item, first, "uploading")
	check("1000000000")
	m.finishUploadAttempt(item, first)
	check("")
	m.beginUploadAttempt(item, second)
	m.setUploadAttemptPhase(item, second, "uploading")
	check("")
	// New attempt has its own rate and confirmed amount; the older 5-byte sample
	// must not shorten this attempt's remaining 9-byte estimate.
	sample.PartPayloadBytes = 1
	sample.PayloadBytesRead = 1
	if err = m.recordTransferSample(item, second, sample); err != nil {
		t.Fatal(err)
	}
	check("9000000000")
	m.setUploadAttemptPhase(item, second, "finalizing")
	check("")
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.update(item, "uploading", 0, ""); err != nil {
		t.Fatal(err)
	}
	check("")
}

func TestTransferEstimateSuppressesFailedZeroTimeAndLegacyEvidence(t *testing.T) {
	plan := int64(10)
	attempt := strings.Repeat("a", 32)
	base := baiduapi.UploadTransferSample{PlannedPayloadBytes: &plan, PlannedPartCount: 2, PartPayloadBytes: 5, PartConfirmed: true, PayloadBytesRead: 5, ActiveNanos: 1000000000, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	for _, kind := range []string{"failed", "zero_time", "legacy", "complete", "zero_payload"} {
		t.Run(kind, func(t *testing.T) {
			s := base
			switch kind {
			case "failed":
				s.PartConfirmed = false
			case "zero_time":
				s.ActiveNanos = 0
			case "legacy":
				s.PlannedPayloadBytes = nil
				s.PlannedPartCount = 0
				s.PartPayloadBytes = 0
				s.PartConfirmed = false
			case "zero_payload":
				zero := int64(0)
				s.PlannedPayloadBytes = &zero
				s.PartPayloadBytes = 0
				s.PayloadBytesRead = 0
				s.PlannedPartCount = 1
			case "complete":
				small := int64(5)
				s.PlannedPayloadBytes = &small
				s.PlannedPartCount = 1
			}
			a := &attemptTransferAccumulator{}
			if err := a.add(uploadTransferEvidence{AttemptID: attempt, Sample: s}); err != nil {
				t.Fatal(err)
			}
			if a.estimate(uploadAttemptSnapshot{ID: attempt, State: "uploading"}) != nil {
				t.Fatal("unreliable estimate displayed")
			}
		})
	}
}
