package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (*Manager, string, string) {
	t.Helper()
	base := t.TempDir()
	root, state := filepath.Join(base, "files"), filepath.Join(base, "state")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := Open(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, root, state
}

func TestConfigurationAndObservationsSurviveReopen(t *testing.T) {
	m, root, state := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "中文.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := m.Configure(root, "")
	if err != nil || c.Revision == "" {
		t.Fatalf("configure: %+v %v", c, err)
	}
	l, err := m.List(context.Background(), "", "中文", 0, 50)
	if err != nil || l.Stale || l.Total != 1 || l.Entries[0].Size != 5 || l.ObservedAt == "" {
		t.Fatalf("list: %+v %v", l, err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Config()
	if err != nil || got.Revision != c.Revision || got.Root != root {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	if err = os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	l, err = m.List(context.Background(), "", "", 0, 50)
	if err == nil || !l.Stale || l.Total != 1 || l.Entries[0].Name != "中文.txt" || l.ObservedAt == "" {
		t.Fatalf("offline erased observation: %+v %v", l, err)
	}
}

func TestRootReplacementAndInvalidPathsAreRejected(t *testing.T) {
	m, root, _ := fixture(t)
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret", "/absolute", `a\b`, "a/../b", "a//b", "CON", "a:stream"} {
		if _, err := m.List(context.Background(), p, "", 0, 50); err == nil {
			t.Errorf("unsafe path accepted: %q", p)
		}
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.List(context.Background(), "", "", 0, 50); err == nil {
		t.Fatal("replacement root accepted")
	}
}

func TestProtectedRootsAndStaleConfigurationAreRejected(t *testing.T) {
	m, root, state := fixture(t)
	for _, p := range []string{state, filepath.Dir(state), filepath.Join(state, "nested")} {
		if _, err := m.Configure(p, ""); err == nil {
			t.Errorf("protected root accepted: %s", p)
		}
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(root, ""); err == nil {
		t.Fatal("stale config version accepted")
	}
}

func TestPagingFilteringAndRefreshUseCurrentFiles(t *testing.T) {
	m, root, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.txt", "a.txt", ".env"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	l, err := m.List(context.Background(), "", "", 1, 1)
	if err != nil || l.Total != 3 || len(l.Entries) != 1 || l.Entries[0].Name != "a.txt" {
		t.Fatalf("page: %+v %v", l, err)
	}
	old := l.Entries[0].Revision
	if err = os.WriteFile(filepath.Join(root, "a.txt"), []byte("longer content"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err = m.List(context.Background(), "", "a.txt", 0, 50)
	if err != nil || l.Total != 1 || l.Entries[0].Revision == old {
		t.Fatalf("changed entry not refreshed: %+v %v", l, err)
	}
	if err = os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	l, err = m.List(context.Background(), "", "a.txt", 0, 50)
	if err != nil || l.Total != 0 || l.Stale {
		t.Fatalf("deleted entry retained in live list: %+v %v", l, err)
	}
}

func TestSymbolicLinksAreNotPublished(t *testing.T) {
	m, root, _ := fixture(t)
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	l, err := m.List(context.Background(), "", "", 0, 50)
	if err != nil || l.Total != 0 {
		t.Fatalf("link published: %+v %v", l, err)
	}
	if _, err = m.List(context.Background(), "link", "", 0, 50); err == nil {
		t.Fatal("link traversal accepted")
	}
}

func TestPrivateNamesAreExcludedAcrossCaseVariants(t *testing.T) {
	m, root, _ := fixture(t)
	if err := os.WriteFile(filepath.Join(root, ".ENV"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".GiT"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public.txt"), []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	l, err := m.List(context.Background(), "", "", 0, 50)
	if err != nil || l.Total != 1 || l.Entries[0].Name != "public.txt" || l.Excluded != 2 {
		t.Fatalf("private case variants published: %+v %v", l, err)
	}
	if _, err = m.List(context.Background(), ".GiT", "", 0, 50); err == nil {
		t.Fatal("private directory case variant accepted")
	}
}
