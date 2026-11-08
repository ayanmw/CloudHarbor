package uploads

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestReceiptReconciliationRequiresSavedReceiptAndAtomicCommit(t *testing.T) {
	for _, mode := range []string{"success", "rapid-success", "shutdown", "read-failure", "different-account", "missing-receipt", "commit-failure"} {
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
			attempt := strings.Repeat("a", 32)
			if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
				t.Fatal(err)
			}
			if _, err = m.claimUploadAttempt(item, attempt); err != nil {
				t.Fatal(err)
			}
			plan := fixtureSessionPlan("/target/a")
			plan.CreateAdmissionVersion = 1
			scope := baiduapi.UploadAccountScope{AccountFingerprint: strings.Repeat("d", 64), CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, CheckedAtUTC: time.Now().UTC()}
			plan.AccountScope = &scope
			record := m.sessionEvidenceRecorder(item, attempt)
			part := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
			a := baiduapi.UploadPartAdmission(part)
			receipt := baiduapi.UploadReceipt{Path: "/target/a(1)", FsID: "42", Size: "3"}
			checkpoints := []baiduapi.UploadSessionCheckpoint{{Plan: &plan}, {Admission: &a}, {Part: &part}, {CreateAdmission: &baiduapi.UploadCreateAdmission{PlanDigest: plan.Digest(), ObservedAtUTC: time.Now().UTC()}}}
			if mode != "missing-receipt" {
				checkpoints = append(checkpoints, baiduapi.UploadSessionCheckpoint{CreateReceipt: &baiduapi.UploadCreateReceipt{PlanDigest: plan.Digest(), Receipt: receipt, ObservedAtUTC: time.Now().UTC()}})
			}
			if mode == "rapid-success" {
				intent := baiduapi.UploadPrecreateIntent{RemotePath: plan.RemotePath, Size: plan.Size, LocalSHA256: plan.LocalSHA256, BlockMD5: plan.BlockMD5, CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, AccountScope: &scope, ObservedAtUTC: time.Now().UTC()}
				pre := m.precreateEvidenceRecorder(item, attempt)
				if err = pre(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
					t.Fatal(err)
				}
				if err = pre(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &baiduapi.UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: receipt, ObservedAtUTC: time.Now().UTC()}}); err != nil {
					t.Fatal(err)
				}
				checkpoints = nil
			}
			for _, c := range checkpoints {
				if err = record(c); err != nil {
					t.Fatal(err)
				}
			}
			m.finishUploadAttempt(item, attempt)
			if err = m.update(item, "unknown", 3, ""); err != nil {
				t.Fatal(err)
			}
			calls := 0
			m.SetReceiptVerifier(func(_ context.Context, old baiduapi.UploadAccountScope, r baiduapi.UploadReceipt) (baiduapi.UploadReceiptVerification, error) {
				calls++
				if mode == "shutdown" {
					m.cancel()
				}
				if old.AccountFingerprint != scope.AccountFingerprint || r != receipt {
					t.Fatal("wrong receipt")
				}
				current := old
				current.CredentialFingerprint = strings.Repeat("e", 64)
				if mode == "different-account" {
					current.AccountFingerprint = strings.Repeat("f", 64)
				}
				if mode == "read-failure" {
					return baiduapi.UploadReceiptVerification{}, errors.New("fixture")
				}
				return baiduapi.UploadReceiptVerification{Receipt: r, CurrentScope: current, ObservedAtUTC: time.Now().UTC()}, nil
			})
			if mode == "commit-failure" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_verify BEFORE INSERT ON events WHEN NEW.state='receipt_verified' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = m.ReconcileReceipt(context.Background(), b.ID, item)
			got, e := m.Get(b.ID)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "success" || mode == "rapid-success" {
				if err != nil || got.Items[0].State != "confirmed" || got.Items[0].ActualPath != receipt.Path {
					t.Fatalf("got=%+v err=%v", got, err)
				}
			} else if err == nil || got.Items[0].State != "unknown" {
				t.Fatalf("unverified release: %+v %v", got, err)
			}
			if mode == "missing-receipt" {
				if calls != 0 {
					t.Fatal("read without saved receipt")
				}
			} else if calls != 1 {
				t.Fatal("verification repeated")
			}
			detail, readErr := m.sessionDetails(b.ID, item)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "success" || mode == "rapid-success" {
				if detail.VerificationStatus != "verified" || detail.VerificationRecordedUTC == "" || detail.VerificationObservedUTC == "" {
					t.Fatalf("lost verification summary: %+v", detail)
				}
				if err = m.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, openErr := Open(m.dir, nil)
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer reopened.Close()
				after, afterErr := reopened.sessionDetails(b.ID, item)
				if afterErr != nil || after.State != "confirmed" || after.VerificationStatus != "verified" || after.VerificationObservedUTC != detail.VerificationObservedUTC {
					t.Fatalf("restart summary=%+v err=%v", after, afterErr)
				}
			}
		})
	}
}

func TestReceiptReconciliationHTTPRejectsReadAndUnscopedRequests(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	calls := 0
	m.SetReceiptVerifier(func(context.Context, baiduapi.UploadAccountScope, baiduapi.UploadReceipt) (baiduapi.UploadReceiptVerification, error) {
		calls++
		return baiduapi.UploadReceiptVerification{}, nil
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, batch, query, body string
		want                       int
	}{{"GET", b.ID, "", "", 405}, {"POST", b.ID, "?force=true", "", 400}, {"POST", b.ID, "", "yes", 400}, {"POST", strings.Repeat("f", 32), "", "", 404}} {
		r := httptest.NewRequest(tc.method, "/verify"+tc.query, strings.NewReader(tc.body))
		r.SetPathValue("batch", tc.batch)
		r.SetPathValue("item", b.Items[0].ID)
		w := httptest.NewRecorder()
		m.HandleReceiptReconciliation(w, r)
		if w.Code != tc.want {
			t.Fatalf("code=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid request performed verification")
	}
}

func TestCloseWaitsForReceiptVerificationAndKeepsUnknown(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	attempt := strings.Repeat("a", 32)
	if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
		t.Fatal(err)
	}
	if _, err = m.claimUploadAttempt(item, attempt); err != nil {
		t.Fatal(err)
	}
	plan := fixtureSessionPlan("/target/a")
	scope := baiduapi.UploadAccountScope{AccountFingerprint: strings.Repeat("d", 64), CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, CheckedAtUTC: time.Now().UTC()}
	intent := baiduapi.UploadPrecreateIntent{RemotePath: plan.RemotePath, Size: 3, LocalSHA256: plan.LocalSHA256, BlockMD5: plan.BlockMD5, CredentialFingerprint: scope.CredentialFingerprint, AppKeyFingerprint: scope.AppKeyFingerprint, AccountScope: &scope, ObservedAtUTC: time.Now().UTC()}
	record := m.precreateEvidenceRecorder(item, attempt)
	if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &baiduapi.UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	m.finishUploadAttempt(item, attempt)
	if err = m.update(item, "unknown", 3, ""); err != nil {
		t.Fatal(err)
	}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	m.SetReceiptVerifier(func(ctx context.Context, _ baiduapi.UploadAccountScope, _ baiduapi.UploadReceipt) (baiduapi.UploadReceiptVerification, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return baiduapi.UploadReceiptVerification{}, ctx.Err()
	})
	result := make(chan error, 1)
	go func() { result <- m.ReconcileReceipt(context.Background(), b.ID, item) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("verification did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel verification")
	}
	select {
	case <-closed:
		t.Fatal("database closed before verification finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err = <-result; err == nil {
		t.Fatal("shutdown confirmed upload")
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(b.ID)
	if err != nil || got.Items[0].State != "unknown" {
		t.Fatalf("restart=%+v err=%v", got, err)
	}
	var failures int
	if err = reopened.db.QueryRow(`SELECT count(*) FROM events WHERE item=? AND state='receipt_verification_failed'`, item).Scan(&failures); err != nil || failures != 1 {
		t.Fatalf("lost cancellation evidence: %d %v", failures, err)
	}
}
