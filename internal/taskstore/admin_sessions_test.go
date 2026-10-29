package taskstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAdminSessionPersistsAcrossRestartAndRevocationIsDurable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 29, 4, 5, 6, 7, time.UTC)
	if err = s.CreateAdminSession("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", created); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	active, err := s.AdminSessionActive(hash)
	if err != nil || !active {
		t.Fatalf("session after restart active=%v err=%v", active, err)
	}
	if err = s.RevokeAdminSession(hash, created.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var storedCreated, storedRevoked int64
	if err = s.db.QueryRow(`SELECT created_at_utc,revoked_at_utc FROM admin_sessions WHERE session_hash=?`, hash).Scan(&storedCreated, &storedRevoked); err != nil {
		t.Fatal(err)
	}
	if storedCreated != created.UnixNano() || storedRevoked != created.Add(time.Minute).UnixNano() {
		t.Fatalf("session evidence timestamps = %d/%d", storedCreated, storedRevoked)
	}
	active, err = s.AdminSessionActive(hash)
	if err != nil || active {
		t.Fatalf("revoked session active=%v err=%v", active, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	active, err = s.AdminSessionActive(hash)
	if err != nil || active {
		t.Fatalf("revocation after restart active=%v err=%v", active, err)
	}
}

func TestRevokeAllAdminSessionsRetainsRevocationEvidence(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	created, revoked := time.Now().UTC(), time.Now().UTC().Add(time.Second)
	for _, hash := range []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if err = s.CreateAdminSession(hash, created); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.RevokeAllAdminSessions(revoked); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		active, activeErr := s.AdminSessionActive(hash)
		if activeErr != nil || active {
			t.Fatalf("global revoke active=%v err=%v", active, activeErr)
		}
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM admin_sessions WHERE revoked_at_utc=?`, revoked.UnixNano()).Scan(&count); err != nil || count != 2 {
		t.Fatalf("revocation evidence rows=%d err=%v", count, err)
	}
}
