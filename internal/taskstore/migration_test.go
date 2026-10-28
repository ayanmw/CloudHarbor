package taskstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeLegacyStore(t *testing.T, dir string, records map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 1, Scope: "/queue"})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBody, 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range records {
		if err := os.WriteFile(filepath.Join(dir, "jobs", name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenMigratesV1JobsWithoutFabricatingPastTimestamps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	first := `{"version":1,"file":{"fs_id":1,"path":"/queue/one.bin","server_filename":"one.bin","size":1},"mode":"keep","status":"done","attempts":0,"failures":0,"created":"2026-10-20T00:00:00Z","updated":"2026-10-21T00:00:00Z"}`
	second := `{"version":1,"file":{"fs_id":2,"path":"/queue/two.bin","server_filename":"two.bin","size":2},"mode":"delete","status":"pending","attempts":0,"failures":0,"created":"2026-10-22T00:00:00Z","updated":"2026-10-22T00:00:00Z"}`
	writeLegacyStore(t, dir, map[string]string{"1.json": first, "2.json": second})
	for name, body := range map[string]string{
		"credentials.json": `{"version":1,"access_token":"sidecar-token"}`,
		"settings.json":    `{"download_mode":"delete"}`,
		"account.json":     `{"version":1,"uk":"123"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	intakeDir := filepath.Join(dir, "intake")
	if err := os.Mkdir(intakeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intakeDir, "0123456789abcdef0123456789abcdef.json"), []byte(`{"state":"finished"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.Load()
	if err != nil || string(jobs[1]) != first || string(jobs[2]) != second {
		t.Fatalf("migrated payload mismatch: jobs=%q err=%v", jobs, err)
	}
	wantTimes := map[int64][2]string{
		1: {"2026-10-20T00:00:00Z", "2026-10-21T00:00:00Z"},
		2: {"2026-10-22T00:00:00Z", "2026-10-22T00:00:00Z"},
	}
	for _, id := range []int64{1, 2} {
		events, err := store.Events(id)
		if err != nil || len(events) != 1 || events[0].Kind != "legacy_import" || events[0].OccurredAtUTC != nil || events[0].CloudObservedAtUTC != nil || events[0].RecordedAtUTC.IsZero() {
			t.Fatalf("legacy evidence has fabricated time or missing import evidence: events=%#v err=%v", events, err)
		}
		var evidence struct {
			SourceFormat  int    `json:"source_format"`
			LegacyCreated string `json:"legacy_created"`
			LegacyUpdated string `json:"legacy_updated"`
		}
		if err = json.Unmarshal(events[0].Evidence, &evidence); err != nil || evidence.SourceFormat != 1 || evidence.LegacyCreated != wantTimes[id][0] || evidence.LegacyUpdated != wantTimes[id][1] {
			t.Fatalf("legacy import evidence lost original timestamps: evidence=%s err=%v", events[0].Evidence, err)
		}
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != sqliteSchemaVersion {
		t.Fatalf("manifest not switched after migration: %#v err=%v", got, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "jobs", "1.json")); err != nil {
		t.Fatalf("legacy source was removed before verified cleanup policy: %v", err)
	}
	credential, err := store.LoadCredentials()
	if err != nil || string(credential) != `{"version":1,"access_token":"sidecar-token"}` {
		t.Fatalf("credential sidecar changed during migration: %q err=%v", credential, err)
	}
	settings, err := store.LoadSettings()
	if err != nil || string(settings) != `{"download_mode":"delete"}` {
		t.Fatalf("settings sidecar changed during migration: %q err=%v", settings, err)
	}
	if err = store.BindAccount("123"); err != nil {
		t.Fatalf("account binding changed during migration: %v", err)
	}
	intakes, err := store.LoadIntakes()
	if err != nil || len(intakes) != 1 {
		t.Fatalf("intake journal changed during migration: count=%d err=%v", len(intakes), err)
	}
}

func TestFailedV1MigrationKeepsLegacySourceAuthoritative(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	writeLegacyStore(t, dir, map[string]string{"1.json": `{invalid`})
	if store, err := Open(dir, "/queue"); err == nil {
		store.Close()
		t.Fatal("opened corrupt v1 queue")
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != 1 {
		t.Fatalf("failed migration changed authoritative version: %#v err=%v", got, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "jobs", "1.json")); err != nil {
		t.Fatalf("failed migration modified source record: %v", err)
	}
}

func TestOpenRetriesInterruptedV1MigrationFromOriginalJobs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	record := `{"version":1,"file":{"fs_id":1,"path":"/queue/one.bin","server_filename":"one.bin","size":1},"mode":"keep","status":"pending"}`
	writeLegacyStore(t, dir, map[string]string{"1.json": record})
	for name, body := range map[string]string{"queue.sqlite.migrating": "partial temp", "queue.sqlite": "partial published file"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.Load()
	if err != nil || string(jobs[1]) != record {
		t.Fatalf("interrupted migration did not recover from v1: %q err=%v", jobs[1], err)
	}
	events, err := store.Events(1)
	if err != nil || len(events) != 1 || string(events[0].Evidence) != `{"source_format":1,"legacy_created":null,"legacy_updated":null}` || events[0].OccurredAtUTC != nil {
		t.Fatalf("missing legacy times were not kept explicitly unknown: events=%#v err=%v", events, err)
	}
}
