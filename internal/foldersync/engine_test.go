package foldersync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/durablefs"
)

type fixtureRemote struct {
	entries []Entry
	payload string
	calls   int
}

func (f *fixtureRemote) Inspect(context.Context, string) (string, string, error) {
	return "account", "remote-root", nil
}
func (f *fixtureRemote) Scan(context.Context, Config, []Entry) ([]Entry, error) {
	return append([]Entry(nil), f.entries...), nil
}
func (f *fixtureRemote) Apply(context.Context, Config, Step, string, func(string, any) error) (any, error) {
	f.calls++
	return nil, fmt.Errorf("fixture remote does not write")
}
func (f *fixtureRemote) Download(_ context.Context, _ Config, _ Entry, p string) error {
	return os.WriteFile(p, []byte(f.payload), 0600)
}
func waitRun(t *testing.T, m *Manager, c, id string) Run {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := m.Runs(c)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.ID == id && r.Status != "running" {
				m.mu.Lock()
				active := m.active
				m.mu.Unlock()
				if !active {
					return r
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
	return Run{}
}

func TestPullLocalDivergenceNeverAdvancesCommonBaseline(t *testing.T) {
	root := t.TempDir()
	sum := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	f := &fixtureRemote{payload: "old", entries: []Entry{{ID: "1", Path: "a", Size: 3, SHA256: sum("old")}}}
	m, err := Open(t.TempDir(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c, err := m.Create(context.Background(), Config{Name: "pull", Mode: "pull", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "complete" {
		t.Fatal(got)
	}
	if err = os.WriteFile(filepath.Join(root, "a"), []byte("own"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "conflict" {
		t.Fatal(got)
	}
	baseline, err := m.baseline(c.ID)
	if err != nil || baseline.Local[0].SHA256 != sum("old") {
		t.Fatal("local divergence polluted common baseline")
	}
	f.payload = "new"
	f.entries[0].SHA256 = sum("new")
	r, err = m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "conflict" {
		t.Fatal(got)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "a")); err != nil || string(raw) != "own" {
		t.Fatal("remote update overwrote local divergence")
	}
}

func TestDeleteRecheckRejectsAtomicReplacementAfterBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := localEntry(context.Background(), root, "a")
	if err != nil {
		t.Fatal(err)
	}
	c := Config{ID: newID(), LocalRoot: root}
	m := &Manager{}
	backup, err := m.backup(c, newID(), "a", expected.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement")
	if err = os.WriteFile(replacement, []byte("new atomic edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = durablefs.ReplaceFile(replacement, target); err != nil {
		t.Fatal(err)
	}
	if err = verifyLocalExpected(context.Background(), c, expected); err == nil {
		t.Fatal("replacement was accepted for deletion")
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "new atomic edit" {
		t.Fatal("new edit was removed")
	}
	if raw, err := os.ReadFile(backup); err != nil || string(raw) != "old" {
		t.Fatal("backup evidence changed")
	}
}
func TestBaselineFailureRollsBackWithoutDeadlock(t *testing.T) {
	root := t.TempDir()
	m, err := Open(t.TempDir(), &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c, err := m.Create(context.Background(), Config{Name: "test", Mode: "pull", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER reject_baseline BEFORE INSERT ON sync_baselines BEGIN SELECT RAISE(ABORT,'injected baseline failure'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "failed" {
		t.Fatalf("%+v", got)
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_baselines`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("baseline published %d %v", count, err)
	}
}
func TestPullAndRenameWithEditConverges(t *testing.T) {
	root := t.TempDir()
	f := &fixtureRemote{payload: "old"}
	sum := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	f.entries = []Entry{{ID: "1", Path: "a", Size: 3, SHA256: sum("old")}}
	m, err := Open(t.TempDir(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c, err := m.Create(context.Background(), Config{Name: "pull", Mode: "pull", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "complete" {
		t.Fatalf("%+v", got)
	}
	f.payload = "new"
	f.entries[0].Path = "b"
	f.entries[0].SHA256 = sum("new")
	r, err = m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "complete" {
		t.Fatalf("%+v", got)
	}
	if _, err = os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatal("old path remains")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "b")); err != nil || string(raw) != "new" {
		t.Fatal("new contents missing")
	}
	if raw, err := os.ReadFile(filepath.Join(root, privateDirectory, r.ID, "a")); err != nil || string(raw) != "old" {
		t.Fatal("old content was not retained")
	}
}
func TestPathBoundaryAndMaliciousRemote(t *testing.T) {
	for _, p := range []string{"..", "../escape", "a/../escape", "/outside", "a\\b", ".nas-sync-history/a"} {
		if safeRelative(p) {
			t.Fatalf("unsafe relative accepted: %q", p)
		}
	}
	root := t.TempDir()
	volume := filepath.VolumeName(root) + string(filepath.Separator)
	if !overlap(volume, root) || !overlap(root, volume) || !under("/folder", "/") {
		t.Fatal("root overlap not detected")
	}
	f := &fixtureRemote{entries: []Entry{{ID: "bad", Path: "..", Directory: true}}}
	m, err := Open(t.TempDir(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c, err := m.Create(context.Background(), Config{Name: "bad", Mode: "pull", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, r.ID); got.Status != "failed" {
		t.Fatalf("%+v", got)
	}
}
func TestRestartDoesNotReplayUnknownIntent(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	f := &fixtureRemote{}
	m, err := Open(dir, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Create(context.Background(), Config{Name: "restart", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	r := Run{ID: newID(), ConfigID: c.ID, Status: "running", Started: time.Now().UTC()}
	if _, err = m.db.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, r.ID, c.ID, r.Status, encoded(r)); err != nil {
		t.Fatal(err)
	}
	if err = m.saveStep(r.ID, StepRecord{Ordinal: 1, Status: "intent", Step: Step{Action: "upload", Path: "a"}}); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = Open(dir, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Start(c.ID); err == nil {
		t.Fatal("unknown intent was admitted again")
	}
	if f.calls != 0 {
		t.Fatal("restart performed a remote write")
	}
	events, err := m.Events(r.ID)
	if err != nil || len(events) < 3 {
		t.Fatalf("recovery evidence missing: %d %v", len(events), err)
	}
}
