package uploads

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

type sessionTestTransport func(*http.Request) (*http.Response, error)

func (f sessionTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUploadWorkerCarriesSessionObserverToSDK(t *testing.T) {
	for _, rapid := range []bool{false, true} {
		t.Run(fmt.Sprint("rapid_", rapid), func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			http.DefaultTransport = sessionTestTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"errno":0,"return_type":1,"uploadid":"worker-private-session","block_list":[0]}`
				switch r.URL.Query().Get("method") {
				case "upload":
					body = `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
				case "create":
					body = `{"errno":0,"fs_id":1,"path":"/target/a","size":3}`
				}
				if rapid {
					if r.URL.Query().Get("method") != "precreate" {
						t.Fatal("rapid upload sent another request")
					}
					body = `{"errno":0,"return_type":2,"fs_id":1,"path":"/target/a","size":3}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, progress func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
				return baiduapi.NewClient("token", "app").UploadLocalFile(ctx, local, remote, progress)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("abc")); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				current, e := m.Get(b.ID)
				if e != nil {
					t.Fatal(e)
				}
				if current.Items[0].State == "confirmed" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("worker did not confirm: %+v", current)
				}
				time.Sleep(20 * time.Millisecond)
			}
			wantSession, wantPre := 5, 1
			if rapid {
				wantSession, wantPre = 0, 2
			}
			var count int
			if err = m.db.QueryRow(`SELECT count(*) FROM events WHERE item=? AND error LIKE 'upload_session_evidence_v1:%'`, b.Items[0].ID).Scan(&count); err != nil || count != wantSession {
				t.Fatalf("worker evidence=%d err=%v", count, err)
			}
			if err = m.db.QueryRow(`SELECT count(*) FROM events WHERE item=? AND substr(error,1,?)=?`, b.Items[0].ID, len(precreateEvidencePrefix), precreateEvidencePrefix).Scan(&count); err != nil || count != wantPre {
				t.Fatalf("precreate count=%d err=%v", count, err)
			}
		})
	}
}

func fixtureSessionPlan(remote string) baiduapi.UploadSessionPlan {
	md := md5.Sum([]byte("abc"))
	sha := sha256.Sum256([]byte("abc"))
	return baiduapi.UploadSessionPlan{PartAdmissionVersion: 1, UploadID: "private-upload-session", RemotePath: remote, Size: 3, BlockSize: baiduapi.UploadBlockSize, LocalSHA256: fmt.Sprintf("%x", sha), BlockMD5: []string{fmt.Sprintf("%x", md)}, NeededParts: []int{0}, CredentialFingerprint: strings.Repeat("a", 64), AppKeyFingerprint: strings.Repeat("b", 64), ObservedAtUTC: time.Now().UTC()}
}
func TestSessionEvidenceBoundToAttemptPersistsWithoutChangingItemBytes(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			_ = m.Close()
		}
	}()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	record := m.sessionEvidenceRecorder(item, attempt)
	plan := fixtureSessionPlan("/target/a")
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	receipt := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
	admission := baiduapi.UploadPartAdmission(receipt)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}

	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err == nil {
		t.Fatal("duplicate receipt accepted")
	}
	m.finishUploadAttempt(item, attempt)
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err == nil {
		t.Fatal("late attempt observation accepted")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = nil
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := m.db.Query(`SELECT error,recorded FROM events WHERE item=? AND error LIKE 'upload_session_evidence_v1:%' ORDER BY sequence`, item)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var body, at string
		if err = rows.Scan(&body, &at); err != nil {
			t.Fatal(err)
		}
		var evidence uploadSessionEvidence
		if err = json.Unmarshal([]byte(strings.TrimPrefix(body, sessionEvidencePrefix)), &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.AttemptID != attempt || evidence.PlanDigest != plan.Digest() || at == "" {
			t.Fatalf("bad evidence=%+v at=%s", evidence, at)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count=%d", count)
	}
	rows.Close()
	journal, journalErr := m.readSessionJournal(item, attempt)
	if journalErr != nil || len(journal.ConfirmedParts) != 1 || len(journal.PendingParts) != 0 || len(journal.NeverAdmittedParts) != 0 {
		t.Fatalf("confirmed journal=%+v err=%v", journal, journalErr)
	}
	current, err := m.Get(b.ID)
	if err != nil || current.Items[0].Sent != "0" || current.Items[0].State != "receiving" {
		t.Fatalf("item changed=%+v err=%v", current, err)
	}
	public, _ := json.Marshal(current)
	if strings.Contains(string(public), plan.UploadID) {
		t.Fatal("private session leaked through public batch")
	}
}

func TestSessionEvidenceWriteFailureAndScopeMismatchCannotAdvance(t *testing.T) {
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
	m.beginUploadAttempt(item, attempt)
	record := m.sessionEvidenceRecorder(item, attempt)
	plan := fixtureSessionPlan("/other/a")
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err == nil {
		t.Fatal("wrong remote path accepted")
	}
	plan.RemotePath = "/target/a"
	if _, err = m.db.Exec(`CREATE TRIGGER fail_session_evidence BEFORE INSERT ON events WHEN NEW.error LIKE 'upload_session_evidence_v1:%' BEGIN SELECT RAISE(ABORT,'fixture evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err == nil {
		t.Fatal("write failure acknowledged")
	}
	receipt := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err == nil {
		t.Fatal("part without durable plan accepted")
	}
	if _, err = m.db.Exec(`DROP TRIGGER fail_session_evidence`); err != nil {
		t.Fatal(err)
	}
	m = restartStoppedEvidenceFixture(t, m, item, attempt)
	record = m.sessionEvidenceRecorder(item, attempt)
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	admission := baiduapi.UploadPartAdmission(receipt)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}
	receipt.MD5 = strings.Repeat("0", 32)
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err == nil {
		t.Fatal("wrong hash accepted")
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM events WHERE item=? AND error LIKE 'upload_session_evidence_v1:%'`, item).Scan(&count); err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	receipt.MD5 = plan.BlockMD5[0]
	if _, err = m.db.Exec(`CREATE TRIGGER fail_part BEFORE INSERT ON events WHEN NEW.state='part_confirmed' BEGIN SELECT RAISE(ABORT,'fixture part failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err == nil {
		t.Fatal("part write failure acknowledged")
	}
	if m.ConcurrencyStatus().AdmissionError == "" {
		t.Fatal("part receipt failure did not stop admission")
	}
	if _, err = m.db.Exec(`DROP TRIGGER fail_part`); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err != nil {
		t.Fatalf("failed part write consumed receipt: %v", err)
	}
}

func TestPartAdmissionPersistsAcrossRestartAndFailedWriteDoesNotAdmit(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			_ = m.Close()
		}
	}()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	record := m.sessionEvidenceRecorder(item, attempt)
	plan := fixtureSessionPlan("/target/a")
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	initial, initialErr := m.readSessionJournal(item, attempt)
	if initialErr != nil || len(initial.NeverAdmittedParts) != 1 || len(initial.PendingParts) != 0 {
		t.Fatalf("initial=%+v err=%v", initial, initialErr)
	}
	admission := baiduapi.UploadPartAdmission{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_admission BEFORE INSERT ON events WHEN NEW.state='part_admitted' BEGIN SELECT RAISE(ABORT,'fixture admission failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err == nil {
		t.Fatal("failed admission acknowledged")
	}
	receipt := baiduapi.UploadPartReceipt(admission)
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err == nil {
		t.Fatal("receipt accepted without durable admission")
	}
	if _, err = m.db.Exec(`DROP TRIGGER fail_admission`); err != nil {
		t.Fatal(err)
	}
	m = restartStoppedEvidenceFixture(t, m, item, attempt)
	record = m.sessionEvidenceRecorder(item, attempt)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err == nil {
		t.Fatal("duplicate admission accepted")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = nil
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := m.readSessionJournal(item, attempt)
	if err != nil || len(journal.PendingParts) != 1 || journal.PendingParts[0] != 0 || len(journal.ConfirmedParts) != 0 || len(journal.NeverAdmittedParts) != 0 {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestSessionJournalRejectsUnboundAndLegacyEvidence(t *testing.T) {
	for _, kind := range []string{"unbound-receipt", "wrong-state", "legacy-plan"} {
		t.Run(kind, func(t *testing.T) {
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
			m.beginUploadAttempt(item, attempt)
			record := m.sessionEvidenceRecorder(item, attempt)
			plan := fixtureSessionPlan("/target/a")
			if kind == "legacy-plan" {
				plan.PartAdmissionVersion = 0
				body, _ := json.Marshal(uploadSessionEvidence{AttemptID: attempt, PlanDigest: plan.Digest(), Checkpoint: baiduapi.UploadSessionCheckpoint{Plan: &plan}})
				_, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'session_observed',0,?,?)`, item, now(), sessionEvidencePrefix+string(body))
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
					t.Fatal(err)
				}
				part := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
				if kind == "unbound-receipt" {
					body, _ := json.Marshal(uploadSessionEvidence{AttemptID: attempt, PlanDigest: plan.Digest(), Checkpoint: baiduapi.UploadSessionCheckpoint{Part: &part}})
					_, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'part_confirmed',3,?,?)`, item, now(), sessionEvidencePrefix+string(body))
					if err != nil {
						t.Fatal(err)
					}
				} else {
					admission := baiduapi.UploadPartAdmission(part)
					if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
						t.Fatal(err)
					}
					if _, err = m.db.Exec(`UPDATE events SET state='part_confirmed' WHERE item=? AND state='part_admitted'`, item); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err = m.readSessionJournal(item, attempt); err == nil {
				t.Fatal("inconsistent journal accepted")
			}
		})
	}
}

func TestSessionJournalKeepsPastAndCurrentAttemptsSeparateAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			_ = m.Close()
		}
	}()
	size := int64(baiduapi.UploadBlockSize)*2 + 1
	b, err := m.Create("/target", []Input{{Path: "a", Size: fmt.Sprint(size)}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	plan := fixtureSessionPlan("/target/a")
	plan.Size = size
	plan.BlockMD5 = []string{strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)}
	plan.NeededParts = []int{0, 1, 2}
	a, bAttempt := strings.Repeat("a", 32), strings.Repeat("b", 32)
	m.beginUploadAttempt(item, a)
	record := m.sessionEvidenceRecorder(item, a)
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	part := func(index int) baiduapi.UploadPartAdmission {
		return baiduapi.UploadPartAdmission{PlanDigest: plan.Digest(), Index: index, MD5: plan.BlockMD5[index], ObservedAtUTC: plan.ObservedAtUTC.Add(-time.Minute)}
	}
	admission := part(0)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}
	receipt := baiduapi.UploadPartReceipt(admission)
	receipt.ObservedAtUTC = plan.ObservedAtUTC.Add(-2 * time.Minute)
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err != nil {
		t.Fatal(err)
	}
	admission = part(1)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}
	m.finishUploadAttempt(item, a)
	m.beginUploadAttempt(item, bAttempt)
	record = m.sessionEvidenceRecorder(item, bAttempt)
	plan.UploadID = "another-private-session"
	if err = record(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	admission = part(2)
	if err = record(baiduapi.UploadSessionCheckpoint{Admission: &admission}); err != nil {
		t.Fatal(err)
	}
	receipt = baiduapi.UploadPartReceipt(admission)
	if err = record(baiduapi.UploadSessionCheckpoint{Part: &receipt}); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = nil
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	past, err := m.readSessionJournal(item, a)
	if err != nil {
		t.Fatal(err)
	}
	current, err := m.readSessionJournal(item, bAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(past.ConfirmedParts, []int{0}) || !reflect.DeepEqual(past.PendingParts, []int{1}) || !reflect.DeepEqual(past.NeverAdmittedParts, []int{2}) || !reflect.DeepEqual(current.ConfirmedParts, []int{2}) || len(current.PendingParts) != 0 || !reflect.DeepEqual(current.NeverAdmittedParts, []int{0, 1}) || past.LastSequence >= current.LastSequence {
		t.Fatalf("past=%+v current=%+v", past, current)
	}
}

func TestSessionJournalPreservesBoundAccountScopeAndCanonicalUTCDigest(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			_ = m.Close()
		}
	}()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	plan := fixtureSessionPlan("/target/a")
	checked := time.Now().In(time.FixedZone("fixtureUTC8", 8*60*60))
	scope := baiduapi.UploadAccountScope{AccountFingerprint: strings.Repeat("c", 64), CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, CheckedAtUTC: checked}
	plan.AccountScope = &scope
	digest := plan.Digest()
	_, offset := scope.CheckedAtUTC.Zone()
	if offset != 8*60*60 {
		t.Fatal("digest mutated caller scope")
	}
	if err = m.sessionEvidenceRecorder(item, attempt)(baiduapi.UploadSessionCheckpoint{Plan: &plan}); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = nil
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := m.readSessionJournal(item, attempt)
	if err != nil || journal.PlanDigest != digest || journal.Plan.AccountScope == nil || journal.Plan.AccountScope.AccountFingerprint != strings.Repeat("c", 64) || !journal.Plan.AccountScope.CheckedAtUTC.Equal(checked) {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestCreateJournalDurabilityAndRestart(t *testing.T) {
	for _, mode := range []string{"admission-only", "receipt", "legacy"} {
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
			attempt := strings.Repeat("c", 32)
			m.beginUploadAttempt(item, attempt)
			record := m.sessionEvidenceRecorder(item, attempt)
			plan := fixtureSessionPlan("/target/a")
			if mode != "legacy" {
				plan.CreateAdmissionVersion = 1
			}
			for _, c := range []baiduapi.UploadSessionCheckpoint{
				{Plan: &plan},
				{Admission: &baiduapi.UploadPartAdmission{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}},
				{Part: &baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}},
			} {
				if err = record(c); err != nil {
					t.Fatal(err)
				}
			}
			a := baiduapi.UploadCreateAdmission{PlanDigest: plan.Digest(), ObservedAtUTC: time.Now().UTC()}
			r := baiduapi.UploadCreateReceipt{PlanDigest: plan.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a(1)", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
			if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &r}); err == nil {
				t.Fatal("receipt without admission")
			}
			if mode != "legacy" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_create BEFORE INSERT ON events WHEN NEW.state='create_admitted' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
					t.Fatal(err)
				}
				if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &a}); err == nil {
					t.Fatal("failed admission accepted")
				}
				if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &r}); err == nil {
					t.Fatal("failed write admitted create")
				}
				if _, err = m.db.Exec(`DROP TRIGGER fail_create`); err != nil {
					t.Fatal(err)
				}
				m = restartStoppedEvidenceFixture(t, m, item, attempt)
				record = m.sessionEvidenceRecorder(item, attempt)
				if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &a}); err != nil {
					t.Fatal(err)
				}
				if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &a}); err == nil {
					t.Fatal("duplicate create")
				}
				bad := r
				bad.Receipt.Path = "/other/a"
				if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &bad}); err == nil {
					t.Fatal("wrong parent accepted")
				}
				if mode == "receipt" {
					if _, err = m.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON events WHEN NEW.state='create_receipt_observed' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
						t.Fatal(err)
					}
					if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &r}); err == nil {
						t.Fatal("failed receipt write accepted")
					}
					journal, readErr := m.readSessionJournal(item, attempt)
					if readErr != nil || journal.CreateAdmission == nil || journal.CreateReceipt != nil {
						t.Fatalf("failed receipt changed journal: %+v %v", journal, readErr)
					}
					if _, err = m.db.Exec(`DROP TRIGGER fail_receipt`); err != nil {
						t.Fatal(err)
					}
					if err = record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &r}); err != nil {
						t.Fatal(err)
					}
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
			j, err := m.readSessionJournal(item, attempt)
			if err != nil {
				t.Fatal(err)
			}
			if j.CreateTracked != (mode != "legacy") || (j.CreateAdmission != nil) != (mode != "legacy") || (j.CreateReceipt != nil) != (mode == "receipt") {
				t.Fatalf("wrong restart classification: %+v", j)
			}
			if j.CreateReceipt != nil && j.CreateReceipt.Receipt.Path != "/target/a(1)" {
				t.Fatal("lost actual identity")
			}
		})
	}
}

func TestCreateJournalRejectsContradictoryEvidence(t *testing.T) {
	for _, mode := range []string{"wrong-state", "wrong-size", "wrong-parent", "wrong-digest", "duplicate-admission", "receipt-without-admission", "part-after-create"} {
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
			attempt := strings.Repeat("d", 32)
			m.beginUploadAttempt(item, attempt)
			plan := fixtureSessionPlan("/target/a")
			plan.CreateAdmissionVersion = 1
			record := m.sessionEvidenceRecorder(item, attempt)
			part := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
			admit := baiduapi.UploadPartAdmission(part)
			for _, c := range []baiduapi.UploadSessionCheckpoint{{Plan: &plan}, {Admission: &admit}, {Part: &part}} {
				if err = record(c); err != nil {
					t.Fatal(err)
				}
			}
			a := baiduapi.UploadCreateAdmission{PlanDigest: plan.Digest(), ObservedAtUTC: time.Now().UTC()}
			if mode != "receipt-without-admission" {
				if err = record(baiduapi.UploadSessionCheckpoint{CreateAdmission: &a}); err != nil {
					t.Fatal(err)
				}
			}
			r := baiduapi.UploadCreateReceipt{PlanDigest: plan.Digest(), Receipt: baiduapi.UploadReceipt{Path: "/target/a", FsID: "42", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
			c := baiduapi.UploadSessionCheckpoint{CreateReceipt: &r}
			state := "create_receipt_observed"
			sent := int64(3)
			switch mode {
			case "wrong-state":
				state = "confirmed"
			case "wrong-size":
				r.Receipt.Size = "4"
			case "wrong-parent":
				r.Receipt.Path = "/other/a"
			case "wrong-digest":
				r.PlanDigest = strings.Repeat("f", 64)
			case "duplicate-admission":
				c = baiduapi.UploadSessionCheckpoint{CreateAdmission: &a}
				state = "create_admitted"
				sent = 0
			case "part-after-create":
				c = baiduapi.UploadSessionCheckpoint{Part: &part}
				state = "part_confirmed"
			}
			body, err := json.Marshal(uploadSessionEvidence{AttemptID: attempt, PlanDigest: plan.Digest(), Checkpoint: c})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,?,?,?,?)`, item, state, sent, now(), sessionEvidencePrefix+string(body)); err != nil {
				t.Fatal(err)
			}
			j, err := m.readSessionJournal(item, attempt)
			if err == nil || j.CreateReceipt != nil || j.CreateAdmission != nil || j.PlanDigest != "" {
				t.Fatalf("unsafe classification: %+v %v", j, err)
			}
		})
	}
}
