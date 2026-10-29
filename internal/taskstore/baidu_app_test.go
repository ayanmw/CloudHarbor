package taskstore

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestBaiduAppConfigPersistsPrivatelyAndSavesOnlyWhenEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveBaiduAppConfigIfEmpty("app-id-12345", "app-key-12345", "secret-key-12345", "sign-key-12345"); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveBaiduAppConfigIfEmpty("replacement-id", "replacement-app", "replacement-secret", "replacement-sign"); !errors.Is(err, ErrBaiduAppConfigExists) {
		t.Fatalf("second save error = %v, want already configured", err)
	}
	gotID, gotKey, gotSecret, gotSign, err := s.LoadBaiduAppConfig()
	if err != nil || gotID != "app-id-12345" || gotKey != "app-key-12345" || gotSecret != "secret-key-12345" || gotSign != "sign-key-12345" {
		t.Fatalf("loaded config = (%q,%q,%q,%q,%v)", gotID, gotKey, gotSecret, gotSign, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	gotID, gotKey, gotSecret, gotSign, err = s.LoadBaiduAppConfig()
	if err != nil || gotID != "app-id-12345" || gotKey != "app-key-12345" || gotSecret != "secret-key-12345" || gotSign != "sign-key-12345" {
		t.Fatalf("restart config = (%q,%q,%q,%q,%v)", gotID, gotKey, gotSecret, gotSign, err)
	}
}
