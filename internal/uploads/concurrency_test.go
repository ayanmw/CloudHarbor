package uploads

import (
	"context"
	"strings"
	"testing"
)

func TestUploadConcurrencyDefaultsAndPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status := m.ConcurrencyStatus(); status.Limit != 1 || status.Maximum != 2 || status.Admitted != 0 || status.UpdatedAtUTC != "" {
		t.Fatalf("default=%+v", status)
	}
	for _, limit := range []int{-1, 0, 3, 32} {
		if err = m.SetConcurrency(limit); err == nil {
			t.Fatalf("accepted limit=%d", limit)
		}
	}
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	before := m.ConcurrencyStatus()
	if before.Limit != 2 || before.UpdatedAtUTC == "" {
		t.Fatalf("saved=%+v", before)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	after := m.ConcurrencyStatus()
	if after.Limit != 2 || after.UpdatedAtUTC != before.UpdatedAtUTC || after.Admitted != 0 || after.Finishing != 0 {
		t.Fatalf("reopened=%+v", after)
	}
}

func TestUploadConcurrencyAdmissionLimitAndReductionDoNotKillAttempts(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "0"}, {Path: "b", Size: "0"}, {Path: "c", Size: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range b.Items {
		if err = m.Receive(context.Background(), b.ID, item.ID, strings.NewReader("")); err != nil {
			t.Fatal(err)
		}
	}
	claim := func(index int, want bool) {
		t.Helper()
		ok, e := m.claimUploadAttempt(b.Items[index].ID, strings.Repeat(string(rune('a'+index)), 32))
		if e != nil || ok != want {
			t.Fatalf("claim %d=%v err=%v expected=%v", index, ok, e, want)
		}
	}
	claim(0, true)
	claim(1, false)
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	claim(1, true)
	if err = m.SetConcurrency(1); err != nil {
		t.Fatal(err)
	}
	if status := m.ConcurrencyStatus(); status.Admitted != 2 || status.Limit != 1 {
		t.Fatalf("downgrade killed attempts: %+v", status)
	}
	claim(2, false)
	m.finishUploadAttempt(b.Items[0].ID, strings.Repeat("a", 32))
	claim(2, false)
	if err = m.recordUploadAttemptReturned(b.Items[1].ID, strings.Repeat("b", 32)); err != nil {
		t.Fatal(err)
	}
	if status := m.ConcurrencyStatus(); status.Admitted != 0 || status.Finishing != 1 {
		t.Fatalf("returned runner shown active: %+v", status)
	}
	claim(2, false)
	m.finishUploadAttempt(b.Items[1].ID, strings.Repeat("b", 32))
	claim(2, true)
}

func TestUploadConcurrencyEvidenceFailureDoesNotChangeEffectiveLimit(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.db.Exec(`CREATE TRIGGER fail_upload_setting BEFORE INSERT ON upload_setting_events BEGIN SELECT RAISE(ABORT,'setting evidence failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err = m.SetConcurrency(2); err == nil {
		t.Fatal("ignored setting persistence failure")
	}
	if status := m.ConcurrencyStatus(); status.Limit != 1 || status.UpdatedAtUTC != "" {
		t.Fatalf("failed save changed memory: %+v", status)
	}
	var n int
	if err = m.db.QueryRow(`SELECT COUNT(*) FROM upload_settings`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial settings=%d err=%v", n, err)
	}
}
