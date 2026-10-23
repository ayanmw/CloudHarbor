package config

import "testing"

func TestPublicOriginEnvironment(t *testing.T) {
	t.Setenv("PUBLIC_ORIGIN", "https://nas.example:8443")
	if Load().PublicOrigin != "https://nas.example:8443" {
		t.Fatal("public origin configuration ignored")
	}
}

func TestDownloadDirectoryCompatibility(t *testing.T) {
	t.Setenv("LOCAL_DOWNLOAD_DIR", "")
	t.Setenv("LOCAL_DOWN_DIR", "/legacy-mount")
	if got := Load().LocalDownDir; got != "/legacy-mount" {
		t.Fatalf("legacy mount ignored: %q", got)
	}
	t.Setenv("LOCAL_DOWNLOAD_DIR", "/preferred-mount")
	if got := Load().LocalDownDir; got != "/preferred-mount" {
		t.Fatalf("canonical value lost: %q", got)
	}
}
