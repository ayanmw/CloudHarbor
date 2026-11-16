package uploads

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestQueuedUploadPauseResumePersistsAndRetainsPayload(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m != nil {
			_ = m.Close()
		}
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	state, err := m.Pause(b.ID, item)
	if err != nil || state != "paused" {
		t.Fatalf("pause: %s %v", state, err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	read, err := m.Get(b.ID)
	if err != nil || read.Items[0].State != "paused" {
		t.Fatal("restart lost pause")
	}
	if _, err = os.Stat(filepath.Join(dir, item+".data")); err != nil {
		t.Fatal("pause removed payload")
	}
	if err = m.Resume(b.ID, item); err != nil {
		t.Fatal(err)
	}
	read, err = m.Get(b.ID)
	if err != nil || read.Items[0].State != "queued" || read.Items[0].Sent != "0" {
		t.Fatal("resume did not create fresh queued attempt")
	}
}

func waitUploadState(t *testing.T, m *Manager, batch, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := m.Get(batch)
		if err == nil && len(b.Items) > 0 && b.Items[0].State == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := m.Get(batch)
	t.Fatalf("expected %s, got %+v", state, b)
}

func TestActiveUploadPauseWaitsForSafeCheckpoint(t *testing.T) {
	for _, stage := range []string{"hashing", "uploading", "submitting"} {
		t.Run(stage, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			dir := t.TempDir()
			m, err := Open(dir, func(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
				if err := report(stage, 0); err != nil {
					return baiduapi.UploadReceipt{}, err
				}
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return baiduapi.UploadReceipt{}, ctx.Err()
				}
				return baiduapi.UploadReceipt{}, report("finalizing", 1)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			item := b.Items[0].ID
			if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start")
			}
			state, err := m.Pause(b.ID, item)
			if err != nil || state != "pause_requested" {
				t.Fatal(state, err)
			}
			read, err := m.Get(b.ID)
			if err != nil || read.Items[0].State != stage || !read.Items[0].PauseRequested {
				t.Fatal("pause falsely acknowledged before request drained")
			}
			if err = m.Resume(b.ID, item); err == nil {
				t.Fatal("resumed before paused checkpoint")
			}
			close(release)
			waitUploadState(t, m, b.ID, "paused")
			read, _ = m.Get(b.ID)
			if read.Items[0].Sent != "1" {
				t.Fatal("checkpoint lost newly confirmed bytes")
			}
			if _, err = os.Stat(filepath.Join(dir, item+".data")); err != nil {
				t.Fatal("pause reclaimed payload")
			}
		})
	}
}

func TestPauseDoesNotReleaseUnknownSubmission(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		if err := report("finalizing", 0); err != nil {
			return baiduapi.UploadReceipt{}, err
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return baiduapi.UploadReceipt{}, ctx.Err()
		}
		return baiduapi.UploadReceipt{}, baiduapi.ErrUploadUnconfirmed
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	if _, err = m.Pause(b.ID, item); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitUploadState(t, m, b.ID, "unknown")
	if err = m.Resume(b.ID, item); err == nil {
		t.Fatal("pause released an unknown mutation")
	}
}

func TestRestartHonorsPauseOnlyForSafeStages(t *testing.T) {
	for _, stage := range []string{"hashing", "uploading", "submitting", "finalizing"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			m, err := Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if m != nil {
					_ = m.Close()
				}
			})
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			item := b.Items[0].ID
			if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
				t.Fatal(err)
			}
			if err = m.update(item, stage, 1, ""); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Pause(b.ID, item); err != nil {
				t.Fatal(err)
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			m, err = Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "paused"
			if stage == "submitting" || stage == "finalizing" {
				want = "unknown"
			}
			read, err := m.Get(b.ID)
			if err != nil || read.Items[0].State != want {
				t.Fatalf("restart %s: %+v %v", stage, read, err)
			}
			if want == "unknown" {
				if err = m.Resume(b.ID, item); err == nil {
					t.Fatal("unknown released")
				}
			}
		})
	}
}

func TestPauseStorageFailureIsAtomic(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER reject_pause BEFORE INSERT ON events WHEN NEW.error='upload_pause_requested_v1' BEGIN SELECT RAISE(ABORT,'fixture pause write failure');END;`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Pause(b.ID, item); err == nil {
		t.Fatal("accepted failed intent write")
	}
	read, err := m.Get(b.ID)
	if err != nil || read.Items[0].State != "queued" || m.isPauseRequested(item) {
		t.Fatal("failed pause partially applied")
	}
}

func TestPauseRequestDoesNotHideConfirmedCompletion(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	dir := t.TempDir()
	m, err := Open(dir, func(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		if err := report("finalizing", 3); err != nil {
			return baiduapi.UploadReceipt{}, err
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return baiduapi.UploadReceipt{}, ctx.Err()
		}
		return baiduapi.UploadReceipt{Path: "/target/a", FsID: "123", Size: "3"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	if _, err = m.Pause(b.ID, item); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitUploadState(t, m, b.ID, "confirmed")
	if err = m.Resume(b.ID, item); err == nil {
		t.Fatal("confirmed item resumed")
	}
}

func TestResumeStorageFailureKeepsPausedPayloadAndIntent(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Pause(b.ID, item); err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER reject_resume BEFORE INSERT ON events WHEN NEW.error='upload_pause_cleared_v1' BEGIN SELECT RAISE(ABORT,'fixture resume write failure');END;`); err != nil {
		t.Fatal(err)
	}
	if err = m.Resume(b.ID, item); err == nil {
		t.Fatal("accepted failed resume write")
	}
	read, err := m.Get(b.ID)
	if err != nil || read.Items[0].State != "paused" || !m.isPauseRequested(item) {
		t.Fatal("failed resume partially applied")
	}
	if _, err = m.db.Exec(`DROP TRIGGER reject_resume`); err != nil {
		t.Fatal(err)
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, item+".data")); !os.IsNotExist(err) {
		t.Fatal("stopping paused item did not reclaim payload")
	}
}

func TestRetryClearsPriorFailedAttemptPauseAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m != nil {
			_ = m.Close()
		}
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	if err = m.update(item, "uploading", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Pause(b.ID, item); err != nil {
		t.Fatal(err)
	}
	if err = m.update(item, "failed", 0, "fixture definite failure"); err != nil {
		t.Fatal(err)
	}
	m.setPauseRequested(item, false)
	if err = m.Retry(b.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.isPauseRequested(item) {
		t.Fatal("old failed-attempt pause resurrected after explicit retry")
	}
	read, err := m.Get(b.ID)
	if err != nil || read.Items[0].State != "queued" {
		t.Fatal("retry did not remain queued")
	}
}
