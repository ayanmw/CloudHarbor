package library

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestLockedFileFailureDoesNotQuarantineLibrary(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	name := filepath.Join(root, "locked.txt")
	if e := os.WriteFile(name, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := windows.UTF16PtrFromString(name)
	if e != nil {
		t.Fatal(e)
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(h)
	l, e := m.List(context.Background(), "", "", 0, 50)
	if e != nil {
		t.Fatal(e)
	}
	req := WriteRequest{ID: opID('a'), Kind: "rename", RootRevision: c.Revision, Path: "locked.txt", Revision: l.Entries[0].Revision, Target: "new.txt", ParentID: parent}
	op, e := m.Write(context.Background(), req, nil)
	if e == nil || op.Status != "failed" {
		t.Fatalf("definite sharing failure quarantined: %+v %v", op, e)
	}
	if e = m.checkUncertain(); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(name); e != nil || string(b) != "keep" {
		t.Fatal(e)
	}
}
