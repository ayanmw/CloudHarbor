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

func TestInvalidIntegerEnvironmentFailsValidation(t *testing.T) {
	for _, key := range []string{"SCAN_INTERVAL", "MAX_PARALLEL", "MAX_RETRIES", "RETRY_BACKOFF_SECONDS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("SCAN_INTERVAL", "300")
			t.Setenv("MAX_PARALLEL", "2")
			t.Setenv("MAX_RETRIES", "3")
			t.Setenv("RETRY_BACKOFF_SECONDS", "5")
			t.Setenv("SERVER_PORT", "8080")
			t.Setenv(key, "not-a-number")
			if err := Load().Validate(); err == nil {
				t.Fatalf("invalid %s should fail validation", key)
			}
		})
	}
}

func TestUnsetIntegerEnvironmentUsesDefaults(t *testing.T) {
	for _, key := range []string{"SCAN_INTERVAL", "MAX_PARALLEL", "MAX_RETRIES", "RETRY_BACKOFF_SECONDS"} {
		t.Setenv(key, "")
	}
	if err := Load().Validate(); err != nil {
		t.Fatalf("unset integer settings should use valid defaults: %v", err)
	}
}
