package uploads

import (
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestResumeAdmissionRequiresUnchangedUnambiguousSource(t *testing.T) {
	for _, mode := range []string{"success", "pending-part", "create-admitted", "changed-sequence", "changed-account", "write-failure"} {
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
			item := b.Items[0].ID
			if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
				t.Fatal(err)
			}
			old, newID := strings.Repeat("a", 32), strings.Repeat("b", 32)
			if claimed, e := m.claimUploadAttempt(item, old); e != nil || !claimed {
				t.Fatalf("claim=%v %v", claimed, e)
			}
			p := fixtureSessionPlan("/target/a")
			p.CreateAdmissionVersion = 1
			scope := baiduapi.UploadAccountScope{AccountFingerprint: strings.Repeat("d", 64), CredentialFingerprint: p.CredentialFingerprint, AppKeyFingerprint: p.AppKeyFingerprint, CheckedAtUTC: time.Now().UTC()}
			p.AccountScope = &scope
			record := m.sessionEvidenceRecorder(item, old)
			if err = record(baiduapi.UploadSessionCheckpoint{Plan: &p}); err != nil {
				t.Fatal(err)
			}
			if mode == "pending-part" || mode == "create-admitted" {
				a := baiduapi.UploadPartAdmission{PlanDigest: p.Digest(), Index: 0, MD5: p.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
				if err = record(baiduapi.UploadSessionCheckpoint{Admission: &a}); err != nil {
					t.Fatal(err)
				}
				if mode == "create-admitted" {
					part := baiduapi.UploadPartReceipt(a)
					if err = record(baiduapi.UploadSessionCheckpoint{Part: &part}); err != nil {
						t.Fatal(err)
					}
					if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &baiduapi.UploadCreateAdmission{PlanDigest: p.Digest(), ObservedAtUTC: time.Now().UTC()}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err = m.recordUploadAttemptReturned(item, old); err != nil {
				t.Fatal(err)
			}
			m.finishUploadAttempt(item, old)
			if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
				t.Fatal(err)
			}
			if claimed, e := m.claimUploadAttempt(item, newID); e != nil || !claimed {
				t.Fatalf("second=%v %v", claimed, e)
			}
			j, err := m.readSessionJournal(item, old)
			if err != nil {
				t.Fatal(err)
			}
			current := scope
			current.CredentialFingerprint = strings.Repeat("e", 64)
			a := baiduapi.UploadResumeAdmission{SourceAttemptID: old, SourceSequence: j.LastSequence, SourcePlanDigest: j.PlanDigest, CurrentScope: current, RemainingParts: j.NeverAdmittedParts, ObservedAtUTC: time.Now().UTC()}
			if mode == "changed-sequence" {
				a.SourceSequence++
			}
			if mode == "changed-account" {
				a.CurrentScope.AccountFingerprint = strings.Repeat("f", 64)
			}
			if mode == "write-failure" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_bridge BEFORE INSERT ON events WHEN NEW.state='session_resume_admitted' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
					t.Fatal(err)
				}
			}
			bridge := m.resumeAdmissionRecorder(item, newID)
			err = bridge(a)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if err = bridge(a); err == nil {
					t.Fatal("duplicate bridge")
				}
			} else if err == nil {
				t.Fatal("unsafe source admitted")
			}
			if mode == "write-failure" {
				if _, err = m.db.Exec(`DROP TRIGGER fail_bridge`); err != nil {
					t.Fatal(err)
				}
				m = restartStoppedEvidenceFixture(t, m, item, newID)
				bridge = m.resumeAdmissionRecorder(item, newID)
				if err = bridge(a); err != nil {
					t.Fatal("failed write advanced memory", err)
				}
			}
			after, err := m.readSessionJournal(item, old)
			if err != nil || after.Plan.Digest() != p.Digest() || after.Plan.CredentialFingerprint != scope.CredentialFingerprint {
				t.Fatal("historical scope changed", err)
			}
		})
	}
}
