package uploads

import (
	"context"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// Raw recorder fixtures rebuild ownership after a required process restart.
// No provider calls occur here; SDK resume/replay boundaries have separate tests.
func restartStoppedEvidenceFixture(t *testing.T, old *Manager, item, attempt string) *Manager {
	t.Helper()
	if old.ConcurrencyStatus().AdmissionError == "" {
		t.Fatal("evidence failure did not stop admission")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(old.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.Close() })
	next.beginUploadAttempt(item, attempt)
	return next
}

func TestPrecreateEvidenceFailureStopsOtherBatchAdmissionImmediately(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	a, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "b", Size: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(a.Items[0].ID, attempt)
	if _, err = m.db.Exec(`CREATE TRIGGER reject_precreate_evidence BEFORE INSERT ON events WHEN NEW.state='precreate_admitted' BEGIN SELECT RAISE(ABORT,'selective evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	plan := fixtureSessionPlan("/target/a")
	intent := baiduapi.UploadPrecreateIntent{RemotePath: plan.RemotePath, Size: plan.Size, LocalSHA256: plan.LocalSHA256, BlockMD5: plan.BlockMD5, CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, ObservedAtUTC: time.Now().UTC()}
	if err = m.precreateEvidenceRecorder(a.Items[0].ID, attempt)(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err == nil {
		t.Fatal("evidence failure ignored")
	}
	if m.ConcurrencyStatus().AdmissionError == "" {
		t.Fatal("evidence failure did not stop admissions")
	}
	if claimed, err := m.claimUploadAttempt(b.Items[0].ID, strings.Repeat("b", 32)); err != nil || claimed {
		t.Fatalf("other batch admitted after evidence failure: %v %v", claimed, err)
	}
}

func TestTransferEvidenceFailureStopsAdmissionWithoutInventingAcknowledgedBytes(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER reject_transfer_evidence BEFORE INSERT ON events WHEN NEW.error LIKE 'upload_transfer_sample_v1:%' BEGIN SELECT RAISE(ABORT,'transfer evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	m.beginUploadAttempt(b.Items[0].ID, strings.Repeat("a", 32))
	sample := baiduapi.UploadTransferSample{PartIndex: 0, PayloadBytesRead: 2, ActiveNanos: 12345, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	if err = m.recordTransferSample(b.Items[0].ID, strings.Repeat("a", 32), sample); err == nil {
		t.Fatal("transfer evidence failure ignored")
	}
	if m.ConcurrencyStatus().AdmissionError == "" {
		t.Fatal("transfer evidence failure did not stop admission")
	}
	got, err := m.Get(b.ID)
	if err != nil || got.Items[0].Sent != "0" || got.Items[0].State != "receiving" {
		t.Fatalf("observation changed acknowledgement: %+v %v", got, err)
	}
}

func TestTransferObservationRejectsReturnedFinishedAndDifferentOwners(t *testing.T) {
	for _, mode := range []string{"returned", "finished", "different"} {
		t.Run(mode, func(t *testing.T) {
			m, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			item, attempt := b.Items[0].ID, strings.Repeat("a", 32)
			m.beginUploadAttempt(item, attempt)
			switch mode {
			case "returned":
				if err = m.recordUploadAttemptReturned(item, attempt); err != nil {
					t.Fatal(err)
				}
			case "finished":
				m.finishUploadAttempt(item, attempt)
			case "different":
				m.beginUploadAttempt(item, strings.Repeat("b", 32))
			}
			sample := baiduapi.UploadTransferSample{PartIndex: 0, PayloadBytesRead: 2, ActiveNanos: 12345, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
			if err = m.recordTransferSample(item, attempt, sample); err == nil {
				t.Fatal("late transfer observation accepted")
			}
			var count int
			if err = m.db.QueryRow(`SELECT COUNT(*) FROM events WHERE error LIKE 'upload_transfer_sample_v1:%'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("late evidence count=%d err=%v", count, err)
			}
			if m.ConcurrencyStatus().AdmissionError != "" {
				t.Fatal("late observation poisoned current admission")
			}
		})
	}
}
