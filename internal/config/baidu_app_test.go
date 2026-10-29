package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBaiduAppConfigPrefersEnvFileAndFallsBackAsPair(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("BAIDU_APP_ID=env-id\nBAIDU_APP_KEY=env-app\nBAIDU_SECRET_KEY=env-secret\nBAIDU_SIGN_KEY=env-sign\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := loadDotEnv(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"BAIDU_APP_ID", "BAIDU_APP_KEY", "BAIDU_SECRET_KEY", "BAIDU_SIGN_KEY"} {
		t.Setenv(name, "")
	}
	cfg, err := LoadFrom(envPath)
	if err != nil || cfg.BaiduAppID != "env-id" || cfg.BaiduAppKey != "env-app" || cfg.BaiduSecretKey != "env-secret" || cfg.BaiduSignKey != "env-sign" {
		t.Fatalf("dotenv app credentials = %#v, %v", cfg, err)
	}
	id, key, secret, sign, source, err := ResolveBaiduAppConfig(values["BAIDU_APP_ID"], values["BAIDU_APP_KEY"], values["BAIDU_SECRET_KEY"], values["BAIDU_SIGN_KEY"], "queue-id", "queue-app", "queue-secret", "queue-sign")
	if err != nil || id != "env-id" || key != "env-app" || secret != "env-secret" || sign != "env-sign" || source != "env" {
		t.Fatalf("got (%q,%q,%q,%q,%q,%v)", id, key, secret, sign, source, err)
	}
	id, key, secret, sign, source, err = ResolveBaiduAppConfig("", "", "", "", "queue-id", "queue-app", "queue-secret", "queue-sign")
	if err != nil || id != "queue-id" || key != "queue-app" || secret != "queue-secret" || sign != "queue-sign" || source != "queue" {
		t.Fatalf("queue fallback got (%q,%q,%q,%q,%q,%v)", id, key, secret, sign, source, err)
	}
}

func TestResolveBaiduAppConfigRejectsPartialEnvironmentPair(t *testing.T) {
	_, _, _, _, _, err := ResolveBaiduAppConfig("", "env-app", "", "", "queue-id", "queue-app", "queue-secret", "queue-sign")
	if err == nil {
		t.Fatal("partial .env pair must not combine with queue sidecar")
	}
}

func TestResolveBaiduAppConfigRejectsPartialQueueSidecar(t *testing.T) {
	_, _, _, _, _, err := ResolveBaiduAppConfig("", "", "", "", "queue-id", "queue-app", "", "queue-sign")
	if err == nil {
		t.Fatal("partial sidecar credentials were accepted")
	}
}
