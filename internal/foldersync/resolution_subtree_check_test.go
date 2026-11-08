package foldersync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type subtreeCheckRemote struct {
	fixtureRemote
	trace string
}

func (f *subtreeCheckRemote) Scan(ctx context.Context, c Config, cache []Entry) ([]Entry, error) {
	f.trace = ContextRunID(ctx)
	return f.fixtureRemote.Scan(ctx, c, cache)
}

func TestSubtreeCheckDetectsDescendantsWithUnchangedDirectoryID(t *testing.T) {
	for _, scenario := range []string{"ready", "added", "removed", "replaced", "metadata", "local_resurrected", "observation_only"} {
		t.Run(scenario, func(t *testing.T) {
			hash := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
			root := t.TempDir()
			folder := filepath.Join(root, "folder")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "child"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			f := &subtreeCheckRemote{fixtureRemote: fixtureRemote{entries: []Entry{{ID: "dir", Path: "folder", Directory: true}, {ID: "file", Path: "folder/child", Size: 3, SHA256: hash("old")}}}}
			m, err := Open(t.TempDir(), f, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			c, err := m.Create(context.Background(), Config{Name: "subtree", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			initial, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, initial.ID); got.Status != "complete" {
				t.Fatal(got)
			}
			if err = os.Remove(filepath.Join(folder, "child")); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(folder); err != nil {
				t.Fatal(err)
			}
			f.entries[1].Size = 5
			f.entries[1].SHA256 = hash("right")
			source, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, source.ID); got.Status != "conflict" {
				t.Fatal(got)
			}
			decision, err := m.CreateResolution(context.Background(), source.ID, 1, "keep_remote")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "added":
				f.entries = append(f.entries, Entry{ID: "extra", Path: "folder/new", Size: 3, SHA256: hash("new")})
			case "removed":
				f.entries = f.entries[:1]
			case "replaced":
				f.entries[1].ID = "replacement"
			case "metadata":
				f.entries[1].Mtime++
			case "observation_only":
				f.entries[1].Observed = time.Now().UTC()
			case "local_resurrected":
				os.Mkdir(folder, 0700)
				os.WriteFile(filepath.Join(folder, "new"), []byte("new"), 0600)
			}
			check, err := m.CheckResolution(context.Background(), decision.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantVerified := scenario == "ready" || scenario == "observation_only"
			if wantVerified && check.Status != "verified" || !wantVerified && check.Status != "rejected" {
				t.Fatalf("check %+v", check)
			}
			if check.CheckScope != "subtree" || check.Subtree == nil || check.Subtree.Root != "folder" || f.trace != check.ID || f.calls != 0 {
				t.Fatalf("scope proof %+v trace %s", check, f.trace)
			}
			if check.ExecutionSupported == nil || !*check.ExecutionSupported {
				t.Fatal("complete supported plan not executable")
			}
			if !wantVerified {
				if _, err = m.StartResolution(decision.ID, check.ID); err == nil {
					t.Fatal("rejected check admitted execution")
				}
			}

		})
	}
}
