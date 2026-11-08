package foldersync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSubtreeArchivePreservesContentAndRejectsChanges(t *testing.T) {
	for _, scenario := range []string{"directory", "nested_history_name", "file", "changed_child", "occupied", "owner", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			p := "folder"
			if scenario == "file" {
				if err := os.WriteFile(filepath.Join(root, p), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(root, p, "empty"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, p, "child"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "nested_history_name" {
				if err := os.MkdirAll(filepath.Join(root, p, privateDirectory), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, p, privateDirectory, "note"), []byte("nested"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original, err := ScanLocal(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			rid, err := rootIdentity(root)
			if err != nil {
				t.Fatal(err)
			}
			c := Config{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LocalRoot: root, LocalID: rid}
			run := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			target := filepath.Join(root, privateDirectory, run, "archive", p)
			if scenario == "changed_child" {
				if err = os.WriteFile(filepath.Join(root, p, "child"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "occupied" {
				if err = os.MkdirAll(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "owner" {
				if err = os.MkdirAll(filepath.Join(root, privateDirectory), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, privateDirectory, "owner"), []byte("another"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			receipt, err := archiveLocalSubtree(ctx, c, run, p, original)
			if scenario != "directory" && scenario != "nested_history_name" && scenario != "file" {
				var notStarted *NotStartedError
				if !errors.As(err, &notStarted) {
					t.Fatalf("wanted not started, got %v", err)
				}
				if _, err = os.Stat(filepath.Join(root, p)); err != nil {
					t.Fatal("source lost", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if receipt.ArchivePath != target || receipt.SourcePath != filepath.Join(root, p) || receipt.ExpectedSHA256 != receipt.ObservedSHA256 || receipt.Finished.IsZero() {
				t.Fatalf("receipt %+v", receipt)
			}
			if _, err = os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
				t.Fatalf("source still present %v", err)
			}
			content := target
			if scenario == "directory" || scenario == "nested_history_name" {
				content = filepath.Join(target, "child")
				if info, e := os.Stat(filepath.Join(target, "empty")); e != nil || !info.IsDir() {
					t.Fatal("empty directory lost", e)
				}
			}
			body, err := os.ReadFile(content)
			if err != nil || string(body) != "original" {
				t.Fatal("archive content", string(body), err)
			}
			remaining, err := ScanLocal(context.Background(), root)
			if err != nil || len(remaining) != 0 {
				t.Fatal("private archive leaked into scan", remaining, err)
			}
		})
	}
}
