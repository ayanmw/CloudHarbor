package taskstore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialsAndAccountBindingAreIndependentFromQueueSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCredentials([]byte(`{"version":1,"access_token":"private-access"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BindAccount("3400000000000000123"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body, err := s.LoadCredentials()
	if err != nil || string(body) != `{"version":1,"access_token":"private-access"}` {
		t.Fatalf("credential roundtrip: %q %v", body, err)
	}
	os.Remove(filepath.Join(dir, "credentials.json"))
	if err = s.BindAccount("999"); !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("removing credentials bypassed account binding: %v", err)
	}
	jobs, err := s.Load()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("credential became task: %v %v", jobs, err)
	}
}

func TestRecordsSurviveReopenAndScopeCannotChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	records, err := s.Load()
	if err != nil || string(records[42]) != `{"status":"pending"}` {
		t.Fatalf("records=%s err=%v", records[42], err)
	}
	s.Close()
	if other, err := Open(dir, "/another-queue"); err == nil {
		other.Close()
		t.Fatal("accepted different cloud queue")
	}
}

func TestStoreRejectsCorruptRecordAndSecondWriter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if other, err := Open(dir, "/preview"); err == nil {
		other.Close()
		t.Fatal("accepted second writer")
	}
	if err = s.Save(1, []byte(`{broken`)); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	if err = os.WriteFile(filepath.Join(dir, "jobs", "1.json"), []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); err == nil {
		t.Fatal("silently ignored corrupt job")
	}
}

func TestStoreLockReleasedByProcessExit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreProcessHelper$")
	cmd.Env = append(os.Environ(), "NAS_STORE_HELPER="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := filepath.Join(dir, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper failed to acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s, err := Open(dir, "/preview"); err == nil {
		s.Close()
		t.Fatal("cross-process lock not enforced")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatalf("crash left permanent lock: %v", err)
	}
	s.Close()
}

func TestStoreProcessHelper(t *testing.T) {
	dir := os.Getenv("NAS_STORE_HELPER")
	if dir == "" {
		return
	}
	s, err := Open(dir, "/preview")
	if err != nil {
		os.Exit(2)
	}
	defer s.Close()
	if err = os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
	for {
		time.Sleep(time.Hour)
	}
}
