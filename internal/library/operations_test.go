package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T) (*Manager, string, Config, string) {
	t.Helper()
	m, root, _ := fixture(t)
	c, e := m.Configure(root, "")
	if e != nil {
		t.Fatal(e)
	}
	l, e := m.List(context.Background(), "", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	return m, root, c, l.DirectoryID
}
func opID(n byte) string { return strings.Repeat(string(n), 32) }
func TestManagementUploadMoveTrashRestore(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	ctx := context.Background()
	body := []byte("真实内容\n")
	req := WriteRequest{ID: opID('a'), Kind: "upload", RootRevision: c.Revision, Target: "中文.txt", ParentID: parent, Size: int64(len(body))}
	op, e := m.Write(ctx, req, bytes.NewReader(body))
	if e != nil || op.Status != "succeeded" {
		t.Fatalf("upload %+v %v", op, e)
	}
	sum := sha256.Sum256(body)
	if op.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("missing content proof")
	}
	if got, e := os.ReadFile(filepath.Join(root, "中文.txt")); e != nil || !bytes.Equal(got, body) {
		t.Fatal("content", e)
	}
	replay, e := m.Write(ctx, req, nil)
	if e != nil || replay.ID != op.ID || replay.SHA256 != op.SHA256 {
		t.Fatal("idempotent replay", e)
	}
	conflict := req
	conflict.ID = opID('b')
	if _, e = m.Write(ctx, conflict, bytes.NewReader([]byte("bad"))); !errors.Is(e, os.ErrExist) {
		t.Fatalf("overwrite accepted %v", e)
	}
	req.Target = "other.txt"
	if _, e = m.Write(ctx, req, nil); !errors.Is(e, ErrConflict) {
		t.Fatalf("ID reuse accepted: %v", e)
	}
	mk := WriteRequest{ID: opID('c'), Kind: "mkdir", RootRevision: c.Revision, Target: "资料", ParentID: parent}
	if _, e = m.Write(ctx, mk, nil); e != nil {
		t.Fatal(e)
	}
	l, e := m.List(ctx, "", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	var file, folder Entry
	for _, v := range l.Entries {
		if v.Directory {
			folder = v
		} else {
			file = v
		}
	}
	move := WriteRequest{ID: opID('d'), Kind: "move", RootRevision: c.Revision, Path: file.Path, Revision: file.Revision, Target: "资料/改名.txt", ParentID: folder.ID}
	if op, e = m.Write(ctx, move, nil); e != nil || op.Status != "succeeded" {
		t.Fatalf("move %+v %v", op, e)
	}
	l, e = m.List(ctx, "资料", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	del := WriteRequest{ID: opID('e'), Kind: "trash", RootRevision: c.Revision, Path: l.Entries[0].Path, Revision: l.Entries[0].Revision}
	trash, e := m.Write(ctx, del, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "资料", "改名.txt")); !os.IsNotExist(e) {
		t.Fatal("not trashed")
	}
	restore := WriteRequest{ID: opID('f'), Kind: "restore", RootRevision: c.Revision, TrashID: trash.ID}
	if _, e = m.Write(ctx, restore, nil); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(root, "资料", "改名.txt"))
	if e != nil || !bytes.Equal(got, body) {
		t.Fatal("restore", e)
	}
	l, e = m.List(ctx, "", "", 0, 50)
	if e != nil || l.Total != 1 {
		t.Fatalf("private area leaked %+v %v", l, e)
	}
	if _, e = m.Write(ctx, WriteRequest{ID: opID('1'), Kind: "restore", RootRevision: c.Revision, TrashID: trash.ID}, nil); e == nil {
		t.Fatal("double restore accepted")
	}
}
func TestManagementRejectsUnsafeStaleAndIncompleteWrites(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	ctx := context.Background()
	for i, p := range []string{"../escape", ".env", ".CLOUDHARBOR-LIBRARY/x", "a/b", "CON"} {
		req := WriteRequest{ID: strings.Repeat(string(rune('a'+i)), 32), Kind: "upload", RootRevision: c.Revision, Target: p, ParentID: parent, Size: 1}
		if _, e := m.Write(ctx, req, strings.NewReader("x")); e == nil {
			t.Fatalf("unsafe target accepted %q", p)
		}
	}
	req := WriteRequest{ID: opID('f'), Kind: "upload", RootRevision: c.Revision, Target: "short.bin", ParentID: parent, Size: 3}
	op, e := m.Write(ctx, req, strings.NewReader("x"))
	if e == nil || op.Status == "succeeded" {
		t.Fatal("short read accepted")
	}
	if _, e = os.Stat(filepath.Join(root, "short.bin")); !os.IsNotExist(e) {
		t.Fatal("partial file published")
	}
	req.ID = opID('1')
	req.RootRevision = "stale"
	if _, e = m.Write(ctx, req, nil); !errors.Is(e, ErrConflict) {
		t.Fatal("stale root", e)
	}
	req.ID = opID('2')
	req.RootRevision = c.Revision
	req.ParentID = "replaced"
	if _, e = m.Write(ctx, req, nil); !errors.Is(e, ErrChanged) {
		t.Fatal("stale parent", e)
	}
}
func TestManagementRestartQuarantinesPreparedOperation(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	state := m.stateDir
	req := WriteRequest{ID: opID('a'), Kind: "mkdir", RootRevision: c.Revision, Target: "never-replay", ParentID: parent}
	if e := m.prepareOperation(Operation{ID: req.ID, Request: req, Status: "prepared", StartedAt: utcNow()}); e != nil {
		t.Fatal(e)
	}
	m.Close()
	reopened, e := Open(state, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	op, e := reopened.Operation(req.ID)
	if e != nil || op.Status != "unknown" {
		t.Fatalf("restart %+v %v", op, e)
	}
	if _, e = reopened.Write(context.Background(), WriteRequest{ID: opID('b'), Kind: "mkdir", RootRevision: c.Revision, Target: "blocked", ParentID: parent}, nil); !errors.Is(e, ErrUncertain) {
		t.Fatal("unknown did not block", e)
	}
	if _, e = reopened.Configure(root, c.Revision); !errors.Is(e, ErrUncertain) {
		t.Fatal("switch lost quarantine", e)
	}
	if _, e = os.Stat(filepath.Join(root, "never-replay")); !os.IsNotExist(e) {
		t.Fatal("replayed")
	}
}
func TestPrivateAreaConflictHasDurableFailureAndNoAdoption(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	if e := os.Mkdir(filepath.Join(root, privateArea), 0700); e != nil {
		t.Fatal(e)
	}
	req := WriteRequest{ID: opID('a'), Kind: "mkdir", RootRevision: c.Revision, Target: "never", ParentID: parent}
	op, e := m.Write(context.Background(), req, nil)
	if e == nil || op.ID != req.ID || op.Status != "failed" {
		t.Fatalf("failure evidence lost %+v %v", op, e)
	}
	saved, e := m.Operation(req.ID)
	if e != nil || saved.Status != "failed" {
		t.Fatal("failure not durable", saved, e)
	}
	if _, e = os.Stat(filepath.Join(root, privateArea, "owner.json")); !os.IsNotExist(e) {
		t.Fatal("adopted foreign area")
	}
}
func TestNativeNoReplacePreservesCompetingTarget(t *testing.T) {
	m, root, c, _ := writeFixture(t)
	os.WriteFile(filepath.Join(root, "source"), []byte("source"), 0600)
	os.WriteFile(filepath.Join(root, "target"), []byte("target"), 0600)
	r, e := m.openRoot(c)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	id, _, e := checkedItem(r, "source")
	if e != nil {
		t.Fatal(e)
	}
	if e = renameNoReplace(r, "source", "target", id, c.RootID); !errors.Is(e, os.ErrExist) {
		t.Fatalf("no-replace: %v", e)
	}
	for _, name := range []string{"source", "target"} {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil || string(b) != name {
			t.Fatal("changed competing file", name, e)
		}
	}
}

type uploadReaderFunc func([]byte) (int, error)

func (f uploadReaderFunc) Read(p []byte) (int, error) { return f(p) }
func TestUploadRejectsReplacedStageAndParent(t *testing.T) {
	for _, change := range []string{"stage", "parent"} {
		t.Run(change, func(t *testing.T) {
			m, root, c, _ := writeFixture(t)
			if e := os.Mkdir(filepath.Join(root, "dest"), 0700); e != nil {
				t.Fatal(e)
			}
			l, e := m.List(context.Background(), "dest", "", 0, 50)
			if e != nil {
				t.Fatal(e)
			}
			req := WriteRequest{ID: opID('a'), Kind: "upload", RootRevision: c.Revision, Target: "dest/file.bin", ParentID: l.DirectoryID, Size: 3}
			input := strings.NewReader("abc")
			changed := false
			reader := uploadReaderFunc(func(p []byte) (int, error) {
				if !changed {
					changed = true
					var e error
					if change == "stage" {
						stage := filepath.Join(root, privateArea, "staging", req.ID)
						e = os.Rename(stage, stage+"-original")
						if e == nil {
							e = os.WriteFile(stage, []byte("bad"), 0600)
						}
					} else {
						e = os.Rename(filepath.Join(root, "dest"), filepath.Join(root, "old-dest"))
						if e == nil {
							e = os.Mkdir(filepath.Join(root, "dest"), 0700)
						}
					}
					if e != nil {
						t.Fatal(e)
					}
				}
				return input.Read(p)
			})
			op, e := m.Write(context.Background(), req, reader)
			if !errors.Is(e, ErrChanged) || op.Status == "succeeded" {
				t.Fatalf("replacement accepted %+v %v", op, e)
			}
			if _, e = os.Stat(filepath.Join(root, "dest", "file.bin")); !os.IsNotExist(e) {
				t.Fatal("published into replaced target")
			}
		})
	}
}
func TestDirectoryManagementStaleSourceAndRestoreCollision(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	ctx := context.Background()
	os.Mkdir(filepath.Join(root, "folder"), 0700)
	os.WriteFile(filepath.Join(root, "folder", "inside.txt"), []byte("keep"), 0600)
	l, e := m.List(ctx, "", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	folder := l.Entries[0]
	req := WriteRequest{ID: opID('a'), Kind: "rename", RootRevision: c.Revision, Path: "folder", Revision: folder.Revision, Target: "renamed", ParentID: parent}
	if _, e = m.Write(ctx, req, nil); e != nil {
		t.Fatal("directory rename", e)
	}
	l, e = m.List(ctx, "", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	folder = l.Entries[0]
	req = WriteRequest{ID: opID('b'), Kind: "move", RootRevision: c.Revision, Path: "renamed", Revision: folder.Revision, Target: "renamed/sub", ParentID: folder.ID}
	if _, e = m.Write(ctx, req, nil); !errors.Is(e, ErrUnsafe) {
		t.Fatal("self move", e)
	}
	req = WriteRequest{ID: opID('c'), Kind: "trash", RootRevision: c.Revision, Path: "renamed", Revision: folder.Revision}
	trash, e := m.Write(ctx, req, nil)
	if e != nil {
		t.Fatal(e)
	}
	os.Mkdir(filepath.Join(root, "renamed"), 0700)
	restore := WriteRequest{ID: opID('d'), Kind: "restore", RootRevision: c.Revision, TrashID: trash.ID}
	if _, e = m.Write(ctx, restore, nil); !errors.Is(e, os.ErrExist) {
		t.Fatal("restore replaced live directory", e)
	}
	os.Remove(filepath.Join(root, "renamed"))
	restore.ID = opID('e')
	if _, e = m.Write(ctx, restore, nil); e != nil {
		t.Fatal("restore directory", e)
	}
	b, e := os.ReadFile(filepath.Join(root, "renamed", "inside.txt"))
	if e != nil || string(b) != "keep" {
		t.Fatal("nested content", e)
	}
	os.WriteFile(filepath.Join(root, "file"), []byte("before"), 0600)
	l, e = m.List(ctx, "", "file", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	entry := l.Entries[0]
	os.WriteFile(filepath.Join(root, "file"), []byte("changed-content"), 0600)
	req = WriteRequest{ID: opID('f'), Kind: "trash", RootRevision: c.Revision, Path: entry.Path, Revision: entry.Revision}
	if _, e = m.Write(ctx, req, nil); !errors.Is(e, ErrChanged) {
		t.Fatal("stale source accepted", e)
	}
}
func TestEmptyUploadPublishesContentProof(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	req := WriteRequest{ID: opID('a'), Kind: "upload", RootRevision: c.Revision, Target: "empty.txt", ParentID: parent, Size: 0}
	op, e := m.Write(context.Background(), req, strings.NewReader(""))
	if e != nil || op.Status != "succeeded" || op.Bytes != 0 || op.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty upload %+v %v", op, e)
	}
	i, e := os.Stat(filepath.Join(root, "empty.txt"))
	if e != nil || i.Size() != 0 {
		t.Fatal(e)
	}
}
