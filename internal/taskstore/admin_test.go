package taskstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdministratorKeyPersistsIndependentlyOfCredentials(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.AdministratorKey("")
	if err != nil || len(key) < 32 {
		t.Fatalf("key not generated: %v", err)
	}
	s.Close()
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.AdministratorKey("")
	if err != nil || key != again {
		t.Fatal("administrator key changed on restart")
	}
	if _, err = s.AdministratorKey("short"); err == nil {
		t.Fatal("weak administrator password accepted")
	}
	jobs, err := s.Load()
	if err != nil || len(jobs) != 0 {
		t.Fatal("private key treated as a job")
	}
}

func TestAdministratorKeyAcceptsConfiguredSimpleKeyRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = os.WriteFile(filepath.Join(dir, "admin-key.json"), []byte(`{"version":1,"key":"adminadmin"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := s.AdministratorKey("")
	if err != nil || key != "adminadmin" {
		t.Fatalf("simple administrator key was rejected: key=%q err=%v", key, err)
	}
}
