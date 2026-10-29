package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCloudRenameRechecksIdentityAndPersistsConfirmedOperation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 91, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	request := CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"}
	result, err := d.MutateCloud(context.Background(), request)
	if err != nil || result.Status != "confirmed" || result.DestinationPath != "/source/new.txt" {
		t.Fatalf("mutation=%+v err=%v", result, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "confirmed" || operations[0].ResponseObservedAtUTC == nil {
		t.Fatalf("remote evidence=%+v err=%v", operations, err)
	}
	if len(p.actions) != 1 {
		t.Fatalf("provider mutation calls=%d", len(p.actions))
	}
}

func TestCloudRenameRejectsChangedSourceIdentityBeforeMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	current := baiduapi.FileInfo{FsID: 92, Path: "/source/new.txt", ServerFilename: "new.txt", Size: 11, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{current}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	expected := current
	expected.Path = "/source/old.txt"
	expected.ServerFilename = "old.txt"
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: expected, NewName: "renamed.txt"})
	if err == nil {
		t.Fatal("changed object identity accepted")
	}
	if len(p.actions) != 0 {
		t.Fatalf("cloud mutation sent despite identity mismatch: %v", p.actions)
	}
}

func TestEmptyDirectoryDeleteRechecksLiveChildrenBeforeMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	dir := baiduapi.FileInfo{FsID: 93, Path: "/source/empty", ServerFilename: "empty", IsDir: 1, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{dir}
	p.lists[dir.Path] = []baiduapi.FileInfo{{FsID: 94, Path: "/source/empty/new-child", ServerFilename: "new-child", Size: 1}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: dir, RequireEmptyDirectory: true})
	if !errors.Is(err, ErrIntakeConflict) {
		t.Fatalf("non-empty remote directory was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("non-empty directory delete was sent: %v", p.actions)
	}
}

func TestDuplicateDeleteRehashesContentBeforeRemoteMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	item := baiduapi.FileInfo{FsID: 95, Path: "/source/copy.bin", ServerFilename: "copy.bin", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{item}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	digest := sha256.Sum256([]byte("payload"))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: item, ExpectedContentSHA256: hex.EncodeToString(digest[:])})
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("verified delete=%+v err=%v", result, err)
	}
	if len(p.actions) != 1 {
		t.Fatalf("expected one provider delete after digest recheck, got %v", p.actions)
	}
}

func TestDuplicateDeleteRejectsChangedContentBeforeRemoteMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	item := baiduapi.FileInfo{FsID: 96, Path: "/source/copy.bin", ServerFilename: "copy.bin", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{item}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: item, ExpectedContentSHA256: strings.Repeat("0", 64)})
	if !errors.Is(err, ErrIntakeStale) {
		t.Fatalf("content change was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("delete sent for unverified content: %v", p.actions)
	}
}
