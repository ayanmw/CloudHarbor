package uploads

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestUploadParallelReceiptFailureStopsAdmissionButRetainsPeerReceipt(t *testing.T) {
	type entered struct {
		role   int32
		remote string
	}
	started := make(chan entered, 3)
	firstRelease, peerRelease := make(chan struct{}), make(chan struct{})
	var firstOnce, peerOnce sync.Once
	var calls atomic.Int32
	unblockFirst := func() { firstOnce.Do(func() { close(firstRelease) }) }
	unblockPeer := func() { peerOnce.Do(func() { close(peerRelease) }) }
	dir := t.TempDir()
	m, err := Open(dir, func(_ context.Context, _, remote string, report func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		role := calls.Add(1)
		if e := report("finalizing", 1); e != nil {
			return baiduapi.UploadReceipt{}, e
		}
		started <- entered{role, remote}
		if role == 1 {
			<-firstRelease
		} else {
			<-peerRelease
		}
		return baiduapi.UploadReceipt{FsID: "1", Path: remote, Size: "1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unblockFirst(); unblockPeer(); m.Close() }()
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
	firstPath := ""
	for i := 0; i < 2; i++ {
		select {
		case e := <-started:
			if e.role == 1 {
				firstPath = strings.TrimPrefix(e.remote, "/target/")
			}
		case <-time.After(4 * time.Second):
			t.Fatal("parallel attempts not started")
		}
	}
	failedID := ""
	for _, item := range b.Items {
		if item.Path == firstPath {
			failedID = item.ID
		}
	}
	if !validID(failedID) {
		t.Fatal("first item not identified")
	}
	if _, err = m.db.Exec(`CREATE TRIGGER reject_parallel_receipt BEFORE INSERT ON events WHEN NEW.state='confirmed' AND NEW.item='` + failedID + `' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	unblockFirst()
	deadline := time.Now().Add(3 * time.Second)
	for m.ConcurrencyStatus().AdmissionError == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.ConcurrencyStatus().AdmissionError == "" {
		t.Fatal("persistence failure did not stop admission")
	}
	if work, e := m.claimNextUpload(); e != nil || work != nil {
		t.Fatalf("new file admitted after receipt failure: %+v %v", work, e)
	}
	unblockPeer()
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.State]++
	}
	if counts["unknown"] != 1 || counts["confirmed"] != 1 || counts["queued"] != 1 || calls.Load() != 2 {
		t.Fatalf("recovery=%v calls=%d", counts, calls.Load())
	}
}
