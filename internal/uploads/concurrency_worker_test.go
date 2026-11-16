package uploads

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestUploadWorkersUseTwoSlotsWithoutDuplicateFiles(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, report func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		started <- remote
		select {
		case <-release:
		case <-ctx.Done():
			return baiduapi.UploadReceipt{}, ctx.Err()
		}
		return baiduapi.UploadReceipt{FsID: "1", Path: remote, Size: "1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer unblock()
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}, {Path: "b", Size: "1"}, {Path: "c", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range b.Items {
		if err = m.Receive(context.Background(), b.ID, item.ID, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case remote := <-started:
			if seen[remote] {
				t.Fatal("duplicate runner", remote)
			}
			seen[remote] = true
		case <-time.After(4 * time.Second):
			t.Fatal("second worker did not start")
		}
	}
	if err = m.SetConcurrency(1); err != nil {
		t.Fatal(err)
	}
	select {
	case remote := <-started:
		t.Fatal("third runner exceeded slots", remote)
	default:
	}
	if status := m.ConcurrencyStatus(); status.Admitted != 2 || status.Limit != 1 {
		t.Fatalf("active=%+v", status)
	}
	unblock()
	select {
	case remote := <-started:
		if seen[remote] {
			t.Fatal("file replayed", remote)
		}
		seen[remote] = true
	case <-time.After(4 * time.Second):
		t.Fatal("remaining file not admitted")
	}
	if len(seen) != 3 {
		t.Fatalf("runners=%v", seen)
	}
}

func TestUploadWorkersUnknownPausesPeerBeforeFinalizingAndBlocksNextClaim(t *testing.T) {
	started := make(chan int32, 3)
	unknownRelease, peerRelease := make(chan struct{}), make(chan struct{})
	var onceA, onceB sync.Once
	unblockA := func() { onceA.Do(func() { close(unknownRelease) }) }
	unblockB := func() { onceB.Do(func() { close(peerRelease) }) }
	var calls atomic.Int32
	m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, report func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		call := calls.Add(1)
		if e := report("uploading", 0); e != nil {
			return baiduapi.UploadReceipt{}, e
		}
		started <- call
		if call == 1 {
			select {
			case <-unknownRelease:
			case <-ctx.Done():
				return baiduapi.UploadReceipt{}, ctx.Err()
			}
			return baiduapi.UploadReceipt{}, baiduapi.ErrUploadUnconfirmed
		}
		select {
		case <-peerRelease:
		case <-ctx.Done():
			return baiduapi.UploadReceipt{}, ctx.Err()
		}
		if e := report("finalizing", 1); e != nil {
			return baiduapi.UploadReceipt{}, e
		}
		return baiduapi.UploadReceipt{FsID: "1", Path: remote, Size: "1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer unblockB()
	defer unblockA()
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}, {Path: "b", Size: "1"}, {Path: "c", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range b.Items {
		if err = m.Receive(context.Background(), b.ID, item.ID, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(4 * time.Second):
			t.Fatal("parallel workers did not start")
		}
	}
	unblockA()
	waitState := func(state string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			got, e := m.Get(b.ID)
			if e != nil {
				t.Fatal(e)
			}
			for _, item := range got.Items {
				if item.State == state {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("missing state", state)
	}
	waitState("unknown")
	unblockB()
	waitState("paused")
	if calls.Load() != 2 {
		t.Fatalf("unknown admitted third file: %d", calls.Load())
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got.Items {
		if item.State == "confirmed" {
			t.Fatal("peer finalized after batch unknown")
		}
	}
}

func TestUploadWorkersCloseWaitsForBothReturnedReceipts(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	dir := t.TempDir()
	m, err := Open(dir, func(_ context.Context, _, remote string, _ func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		started <- struct{}{}
		<-release
		return baiduapi.UploadReceipt{FsID: "1", Path: remote, Size: "1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer unblock()
	if err = m.SetConcurrency(2); err != nil {
		m.Close()
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}, {Path: "b", Size: "1"}})
	if err != nil {
		m.Close()
		t.Fatal(err)
	}
	for _, item := range b.Items {
		if err = m.Receive(context.Background(), b.ID, item.ID, strings.NewReader("x")); err != nil {
			unblock()
			m.Close()
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(4 * time.Second):
			unblock()
			m.Close()
			t.Fatal("workers not started")
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	<-m.ctx.Done()
	select {
	case err = <-closed:
		t.Fatalf("closed before runners returned: %v", err)
	default:
	}
	unblock()
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got.Items {
		if item.State != "confirmed" {
			t.Fatalf("lost return receipt: %+v", item)
		}
	}
}

func TestUploadWorkerPoolKeepsDirectoryRunnerSerial(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var active, maximum atomic.Int32
	m, err := Open(t.TempDir(), func(context.Context, string, string, func(string, int64) error, func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		t.Error("directory entered file runner")
		return baiduapi.UploadReceipt{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer unblock()
	m.SetDirectoryRunner(func(_ context.Context, remote string) (DirectoryReceipt, error) {
		current := active.Add(1)
		if current > maximum.Load() {
			maximum.Store(current)
		}
		defer active.Add(-1)
		started <- struct{}{}
		<-release
		return DirectoryReceipt{Status: "existing", Path: remote, RemoteID: "1", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	for _, name := range []string{"a", "b"} {
		if _, err = m.createManifest("/target", nil, []string{name}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("first directory did not start")
	}
	if maximum.Load() != 1 {
		t.Fatal("overlapping directory workers")
	}
	unblock()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("second directory did not start")
	}
	if maximum.Load() != 1 {
		t.Fatal("directories were not serial")
	}
}
