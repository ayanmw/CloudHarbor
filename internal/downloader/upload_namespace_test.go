package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestUploadParentNamespaceDenialPreventsActualMkdir(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	denied := errors.New("batch became unknown before mkdir")
	guards := 0
	ctx := baiduapi.WithUploadNamespaceAdmissionGuard(context.Background(), func(target, operation string) (func(error), error) {
		guards++
		if target != "/parent" || len(operation) != 32 {
			t.Errorf("admission target=%s operation=%s", target, operation)
		}
		return nil, denied
	})
	local := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := d.UploadStagedFile(ctx, local, "/parent/a", func(string, int64) error { return nil }, nil)
	p.mu.Lock()
	writes := len(p.actions)
	p.mu.Unlock()
	if !errors.Is(err, denied) || guards != 1 || writes != 0 {
		t.Fatalf("guard=%d writes=%d err=%v", guards, writes, err)
	}
	operations, err := d.queue.store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("unsent operation=%+v err=%v", operations, err)
	}
}

func TestUploadParentNamespaceReleasesAfterIntentBeforeHTTP(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	released := false
	guardedOperation := ""
	p.onCreate = func(target string) {
		if !released {
			t.Error("network request started before batch admission lock released")
		}
		p.mu.Lock()
		p.lists["/"] = []baiduapi.FileInfo{{FsID: 456, Path: target, ServerFilename: "parent", IsDir: 1}}
		p.mu.Unlock()
	}
	ctx := baiduapi.WithUploadNamespaceAdmissionGuard(context.Background(), func(target, operation string) (func(error), error) {
		guardedOperation = operation
		return func(persistErr error) {
			operations, err := d.queue.store.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].ID != operation || operations[0].Status != "prepared" {
				t.Errorf("released before prepared intent: %+v %v", operations, err)
			}
			released = true
		}, nil
	})
	result, _, err := d.EnsureUploadDirectory(ctx, "/parent")
	if err != nil || result.Status != "confirmed" || guardedOperation == "" || result.OperationID != guardedOperation || !released {
		t.Fatalf("result=%+v err=%v released=%v guard=%s", result, err, released, guardedOperation)
	}
}

func TestUploadParentNamespaceReleasesWhenRemoteIntentCannotPersist(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	released := false
	ctx := baiduapi.WithUploadNamespaceAdmissionGuard(context.Background(), func(string, string) (func(error), error) {
		if err := d.queue.store.Close(); err != nil {
			t.Error(err)
		}
		return func(persistErr error) { released = true }, nil
	})
	_, _, err := d.EnsureUploadDirectory(ctx, "/parent")
	if err == nil || !released {
		t.Fatalf("intent failure lock leak: released=%v err=%v", released, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.actions) != 0 {
		t.Fatalf("sent without durable remote intent: %v", p.actions)
	}
}
