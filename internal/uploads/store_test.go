package uploads

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestStagingPersistsAndRestartIsolatesAnUnconfirmedRemoteWrite(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.Create("/target", []Input{{Path: "folder/a.txt", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), batch.ID, batch.Items[0].ID, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if err = m.update(batch.Items[0].ID, "finalizing", 5, ""); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	read, err := m.Get(batch.ID)
	if err != nil || read.Items[0].State != "unknown" {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	if _, err = os.Stat(filepath.Join(dir, batch.Items[0].ID+".data")); err != nil {
		t.Fatal(err)
	}
}
func TestUploadRejectsTraversalAndShortBody(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, p := range []string{"../x", "/x", "a/../x", "a\\x"} {
		if _, err = m.Create("/target", []Input{{Path: p, Size: "5"}}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	b, err := m.Create("/target", []Input{{Path: "x", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("hi")); err == nil {
		t.Fatal("short content accepted")
	}
	read, _ := m.Get(b.ID)
	if read.Items[0].State != "receiving" {
		t.Fatal(read.Items[0].State)
	}
}

func TestWorkerConfirmsAndReclaimsOnlyItsStagingFile(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	m, err := Open(dir, func(ctx context.Context, local, remote string, report func(string, int64) error) (baiduapi.UploadReceipt, error) {
		calls++
		if remote != "/target/folder/a.txt" {
			t.Errorf("target=%s", remote)
		}
		for _, phase := range []string{"hashing", "submitting", "uploading", "finalizing"} {
			if err := report(phase, 5); err != nil {
				return baiduapi.UploadReceipt{}, err
			}
		}
		return baiduapi.UploadReceipt{Path: remote, FsID: "123", Size: "5"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "folder/a.txt", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		read, err := m.Get(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if read.Items[0].State == "confirmed" {
			if read.Items[0].ActualPath != "/target/folder/a.txt" || read.Items[0].Sent != "5" {
				t.Fatal(read)
			}
			m.Close()
			if calls != 1 {
				t.Fatal(calls)
			}
			if _, err = os.Stat(filepath.Join(dir, b.Items[0].ID+".data")); !os.IsNotExist(err) {
				t.Fatal("confirmed payload not reclaimed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("worker did not confirm")
}
func TestRestartRequeuesOnlySafeStagesAndNeverRetriesUnknown(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}, {Path: "b", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range b.Items {
		if err = m.Receive(context.Background(), b.ID, i.ID, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	m.update(b.Items[0].ID, "uploading", 1, "")
	m.update(b.Items[1].ID, "submitting", 0, "")
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Retry(b.ID)
	read, _ := m.Get(b.ID)
	states := map[string]string{}
	for _, i := range read.Items {
		states[i.Path] = i.State
	}
	if states["a"] != "queued" || states["b"] != "unknown" {
		t.Fatal(states)
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	read, _ = m.Get(b.ID)
	for _, i := range read.Items {
		if i.Path == "a" && i.State != "cancelled" || i.Path == "b" && i.State != "unknown" {
			t.Fatal(read)
		}
	}
}

func TestCancellationAndStartupReclaimOnlyOwnedPayloads(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, item+".data")); !os.IsNotExist(err) {
		t.Fatal("cancelled payload retained")
	}
	os.WriteFile(filepath.Join(dir, item+".data"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(dir, item+".part"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(dir, "user.part"), []byte("keep"), 0600)
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, suffix := range []string{".data", ".part"} {
		if _, err = os.Stat(filepath.Join(dir, item+suffix)); !os.IsNotExist(err) {
			t.Fatal("interrupted payload retained")
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "user.part")); err != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestRestoredUploadWaitsForAccountVerification(t *testing.T) {
	var ready atomic.Bool
	m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, report func(string, int64) error) (baiduapi.UploadReceipt, error) {
		return baiduapi.UploadReceipt{Path: remote, FsID: "1", Size: "1"}, nil
	}, ready.Load)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "file", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	read, _ := m.Get(b.ID)
	if read.Items[0].State != "queued" {
		t.Fatal("claimed before account verification")
	}
	ready.Store(true)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		read, _ = m.Get(b.ID)
		if read.Items[0].State == "confirmed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("verified upload did not resume")
}
