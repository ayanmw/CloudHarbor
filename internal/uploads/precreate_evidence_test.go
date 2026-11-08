package uploads

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestPrecreateJournalFailuresAndRestart(t *testing.T) {
	for _, mode := range []string{"intent-only", "rapid", "intent-write-failure", "receipt-write-failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			m, err := Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if m != nil {
					m.Close()
				}
			}()
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			item := b.Items[0].ID
			attempt := strings.Repeat("e", 32)
			m.beginUploadAttempt(item, attempt)
			plan := fixtureSessionPlan("/target/a")
			intent := baiduapi.UploadPrecreateIntent{RemotePath: plan.RemotePath, Size: plan.Size, LocalSHA256: plan.LocalSHA256, BlockMD5: plan.BlockMD5, CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, ObservedAtUTC: time.Now().UTC()}
			record := m.precreateEvidenceRecorder(item, attempt)
			receipt := baiduapi.UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a(1)", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
			if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &receipt}); err == nil {
				t.Fatal("unbound rapid receipt")
			}
			if mode == "intent-write-failure" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_precreate BEFORE INSERT ON events WHEN NEW.state='precreate_admitted' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
					t.Fatal(err)
				}
				if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err == nil {
					t.Fatal("intent failure accepted")
				}
				if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &receipt}); err == nil {
					t.Fatal("failed write advanced memory")
				}
				if _, err = m.db.Exec(`DROP TRIGGER fail_precreate`); err != nil {
					t.Fatal(err)
				}
				m = restartStoppedEvidenceFixture(t, m, item, attempt)
				record = m.precreateEvidenceRecorder(item, attempt)
			}
			if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
				t.Fatal(err)
			}
			if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err == nil {
				t.Fatal("duplicate intent")
			}
			bad := receipt
			bad.Receipt.Path = "/elsewhere/a"
			if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &bad}); err == nil {
				t.Fatal("wrong parent accepted")
			}
			if mode == "receipt-write-failure" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_rapid BEFORE INSERT ON events WHEN NEW.state='rapid_receipt_observed' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
					t.Fatal(err)
				}
				if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &receipt}); err == nil {
					t.Fatal("receipt failure accepted")
				}
				if m.ConcurrencyStatus().AdmissionError == "" {
					t.Fatal("rapid receipt failure did not stop admission")
				}
			} else if mode == "rapid" {
				if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &receipt}); err != nil {
					t.Fatal(err)
				}
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			m = nil
			m, err = Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			j, err := m.readPrecreateJournal(item, attempt)
			if err != nil || j.IntentDigest != intent.Digest() || (j.RapidReceipt != nil) != (mode == "rapid") {
				t.Fatalf("journal=%+v err=%v", j, err)
			}
		})
	}
}
func TestMultipartPlanRequiresMatchingPrecreate(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	attempt := strings.Repeat("f", 32)
	m.beginUploadAttempt(item, attempt)
	p := fixtureSessionPlan("/target/a")
	intent := baiduapi.UploadPrecreateIntent{RemotePath: p.RemotePath, Size: p.Size, LocalSHA256: p.LocalSHA256, BlockMD5: p.BlockMD5, CredentialFingerprint: p.CredentialFingerprint, AppKeyFingerprint: p.AppKeyFingerprint, ObservedAtUTC: time.Now().UTC()}
	p.PrecreateDigest = intent.Digest()
	record := m.sessionEvidenceRecorder(item, attempt)
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &p}); err == nil {
		t.Fatal("missing precreate accepted")
	}
	if err = m.precreateEvidenceRecorder(item, attempt)(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
		t.Fatal(err)
	}
	bad := p
	bad.CredentialFingerprint = strings.Repeat("c", 64)
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &bad}); err == nil {
		t.Fatal("different credential accepted")
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &p}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.readSessionJournal(item, attempt); err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`DELETE FROM events WHERE item=? AND state='precreate_admitted'`, item); err != nil {
		t.Fatal(err)
	}
	if _, err = m.readSessionJournal(item, attempt); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatal("orphaned session was not distinguished from absent evidence")
	}
}
