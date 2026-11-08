package uploads

import (
	"errors"
	"strings"
	"testing"
)

func TestUploadNamespaceAdmissionBlocksUnknownAndPreservesOperationEvidence(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "folder/a", Size: "3"}, {Path: "peer", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item, attempt := b.Items[0].ID, strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	guard := m.namespaceAdmissionGuard(item, attempt)
	operation := strings.Repeat("b", 32)
	release, err := guard("/target/folder", operation)
	if err != nil {
		t.Fatal(err)
	}
	release(nil)
	var body, recorded string
	if err = m.db.QueryRow(`SELECT error,recorded FROM events WHERE item=? AND state='namespace_admitted'`, item).Scan(&body, &recorded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, operation) || !strings.Contains(body, attempt) || !strings.Contains(body, "/target/folder") || recorded == "" {
		t.Fatalf("evidence=%s time=%s", body, recorded)
	}
	if err = m.update(b.Items[1].ID, "unknown", 0, "peer unknown"); err != nil {
		t.Fatal(err)
	}
	if release, err = guard("/target/folder", strings.Repeat("c", 32)); !errors.Is(err, errUploadBatchBlocked) {
		if release != nil {
			release(nil)
		}
		t.Fatalf("unknown namespace admitted: %v", err)
	}
	if status := m.ConcurrencyStatus(); status.Admitted != 1 {
		t.Fatal("guard failure lost owner")
	}
}

func TestUploadNamespaceRemoteIntentFailureStopsAdmissionBeforeUnlock(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "folder/a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item, attempt := b.Items[0].ID, strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	release, err := m.namespaceAdmissionGuard(item, attempt)("/target/folder", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	release(errors.New("remote prepared intent failed"))
	release(nil)
	if status := m.ConcurrencyStatus(); status.AdmissionError != "remote prepared intent failed" || status.Admitted != 1 {
		t.Fatalf("status=%+v", status)
	}
}

func TestUploadNamespaceAdmissionRejectsUnrelatedPathAndFailedEvidence(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "folder/a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item, attempt := b.Items[0].ID, strings.Repeat("a", 32)
	m.beginUploadAttempt(item, attempt)
	guard := m.namespaceAdmissionGuard(item, attempt)
	if release, err := guard("/unrelated", strings.Repeat("b", 32)); err == nil {
		release(nil)
		t.Fatal("unrelated namespace admitted")
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_namespace_admission BEFORE INSERT ON events WHEN NEW.state='namespace_admitted' BEGIN SELECT RAISE(ABORT,'namespace evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if release, err := guard("/target/folder", strings.Repeat("b", 32)); err == nil {
		release(nil)
		t.Fatal("admitted without durable evidence")
	}
	_ = m.ConcurrencyStatus() // Failure must release the management lock.
}
