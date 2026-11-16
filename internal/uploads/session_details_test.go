package uploads

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestUploadSessionDetailsIsReadOnlyAndScoped(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, batch, item, query string
		want                       int
	}{{"GET", b.ID, b.Items[0].ID, "", 200}, {"POST", b.ID, b.Items[0].ID, "", 405}, {"GET", strings.Repeat("e", 32), b.Items[0].ID, "", 404}, {"GET", b.ID, b.Items[0].ID, "?action=resume", 400}} {
		r := httptest.NewRequest(tc.method, "/session"+tc.query, nil)
		r.SetPathValue("batch", tc.batch)
		r.SetPathValue("item", tc.item)
		w := httptest.NewRecorder()
		m.HandleSessionDetails(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v code=%d body=%s", tc, w.Code, w.Body.String())
		}
		if w.Code == 200 && (!strings.Contains(w.Body.String(), `"status":"no_evidence"`) || strings.Contains(w.Body.String(), "credential_fingerprint")) {
			t.Fatal(w.Body.String())
		}
	}
	current, err := m.Get(b.ID)
	if err != nil || current.Items[0].State != "receiving" {
		t.Fatal("read changed item", err)
	}
}

func TestSessionDetailsExposeClassificationWithoutPrivateManifest(t *testing.T) {
	for _, mode := range []string{"session_ready", "parts_unconfirmed", "create_unconfirmed", "create_receipt", "precreate_unconfirmed", "rapid_receipt"} {
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
			if ok, e := m.claimUploadAttempt(item, attempt); e != nil || !ok {
				t.Fatalf("claim %v %v", ok, e)
			}
			p := fixtureSessionPlan("/target/a")
			p.CreateAdmissionVersion = 1
			if mode == "precreate_unconfirmed" || mode == "rapid_receipt" {
				intent := baiduapi.UploadPrecreateIntent{RemotePath: p.RemotePath, Size: p.Size, LocalSHA256: p.LocalSHA256, BlockMD5: p.BlockMD5, CredentialFingerprint: p.CredentialFingerprint, AppKeyFingerprint: p.AppKeyFingerprint, ObservedAtUTC: time.Now().UTC()}
				record := m.precreateEvidenceRecorder(item, attempt)
				if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
					t.Fatal(err)
				}
				if mode == "rapid_receipt" {
					r := baiduapi.UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
					if err = record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &r}); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				record := m.sessionEvidenceRecorder(item, attempt)
				if err = record(baiduapi.UploadSessionCheckpoint{Plan: &p}); err != nil {
					t.Fatal(err)
				}
				if mode != "session_ready" {
					a := baiduapi.UploadPartAdmission{PlanDigest: p.Digest(), Index: 0, MD5: p.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
					if err = record(baiduapi.UploadSessionCheckpoint{Admission: &a}); err != nil {
						t.Fatal(err)
					}
					if mode != "parts_unconfirmed" {
						part := baiduapi.UploadPartReceipt(a)
						if err = record(baiduapi.UploadSessionCheckpoint{Part: &part}); err != nil {
							t.Fatal(err)
						}
						if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &baiduapi.UploadCreateAdmission{PlanDigest: p.Digest(), ObservedAtUTC: time.Now().UTC()}}); err != nil {
							t.Fatal(err)
						}
						if mode == "create_receipt" {
							if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &baiduapi.UploadCreateReceipt{PlanDigest: p.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}}); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
			d, err := m.sessionDetails(b.ID, item)
			if err != nil || d.Status != mode || d.Sequence == "0" || d.EvidenceUTC == "" {
				t.Fatalf("detail=%+v err=%v", d, err)
			}
			encoded, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"upload_id", "credential_fingerprint", "local_sha256", p.UploadID} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("private evidence leaked %s", secret)
				}
			}
		})
	}
}
