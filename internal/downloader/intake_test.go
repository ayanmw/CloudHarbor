package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func intakeFixture(t *testing.T) (*Downloader, *provider, *atomic.Int32, *atomic.Bool, *atomic.Bool) {
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected download during intake")
		io.WriteString(w, "abc")
	})
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdown(t, d) })
	p.lists["/source"] = []baiduapi.FileInfo{{FsID: 123, Path: "/source/a.txt", ServerFilename: "a.txt", Size: 3, Mtime: 42}}
	p.lists["/queue"] = []baiduapi.FileInfo{}
	base := http.DefaultTransport
	calls, unavailable := &atomic.Int32{}, &atomic.Bool{}
	loseReply := &atomic.Bool{}
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "list" && unavailable.Load() {
			return nil, io.ErrUnexpectedEOF
		}
		if r.URL.Query().Get("opera") != "move" {
			return base.RoundTrip(r)
		}
		calls.Add(1)
		r.ParseForm()
		var items []struct{ Path, Dest, Newname string }
		if json.Unmarshal([]byte(r.Form.Get("filelist")), &items) != nil || len(items) != 1 {
			t.Fatal("bad move request")
		}
		item := items[0]
		var source baiduapi.FileInfo
		for i, f := range p.lists[path.Dir(item.Path)] {
			if f.Path == item.Path {
				source = f
				p.lists[path.Dir(item.Path)] = append(p.lists[path.Dir(item.Path)][:i], p.lists[path.Dir(item.Path)][i+1:]...)
				break
			}
		}
		if source.FsID == 0 {
			t.Error("missing source in fake move")
		}
		source.Path = path.Join(item.Dest, item.Newname)
		source.ServerFilename = item.Newname
		p.lists[item.Dest] = append(p.lists[item.Dest], source)
		if loseReply.Load() {
			unavailable.Store(true)
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"info":[{"errno":0}]}`))}, nil
	})
	return d, p, calls, unavailable, loseReply
}

func TestIntakePathValidationRejectsTrailingSpaceAndDotNames(t *testing.T) {
	for _, source := range []string{"/source/file ", "/source/file."} {
		if err := validateIntakePaths([]string{source}, "/queue"); !errors.Is(err, ErrIntakeInvalid) {
			t.Fatalf("validateIntakePaths(%q) error = %v, want ErrIntakeInvalid", source, err)
		}
	}
}

func TestIntakeBlockChangesRefreshEligibleSchedulerCandidates(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := baiduapi.FileInfo{FsID: 902, Path: "/queue/moved.bin", ServerFilename: "moved.bin", Size: 3}
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending", Created: time.Now()}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.rebuildCandidates()

	intakeID := "0123456789abcdef0123456789abcdef"
	record := intakeRecord{Intake: Intake{Items: []IntakeItem{{Target: info.Path, State: "unknown"}}}}
	d.intakeMutex.Lock()
	d.intakes[intakeID] = record
	d.intakeMutex.Unlock()
	d.refreshIntakeBlocks()
	if job.candidate != nil {
		t.Fatal("unknown intake left a runnable scheduler candidate")
	}

	record.Items[0].State = "moved"
	d.intakeMutex.Lock()
	d.intakes[intakeID] = record
	d.intakeMutex.Unlock()
	d.refreshIntakeBlocks()
	if job.candidate == nil {
		t.Fatal("resolved intake did not restore the already-scanned candidate")
	}
}

func TestRunningMoveWithoutTaskIDStaysQuarantinedAndDoesNotRejectToken(t *testing.T) {
	for _, directory := range []int{0, 1} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			d, p, _, _, _ := intakeFixture(t)
			p.lists["/source"][0].IsDir = directory
			base := http.DefaultTransport
			var requests int
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("opera") != "move" {
					return base.RoundTrip(r)
				}
				requests++
				// An async root can already be visible while the operation runs.
				moved := p.lists["/source"][0]
				moved.Path = "/queue/a.txt"
				p.lists["/source"] = []baiduapi.FileInfo{}
				p.lists["/queue"] = []baiduapi.FileInfo{moved}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":111}`))}, nil
			})
			preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []func(context.Context, string) (Intake, error){d.CommitIntake, d.CommitIntake, d.RecheckIntake} {
				result, err := action(context.Background(), preview.ID)
				if err != nil || result.Items[0].State != "unknown" || result.Items[0].Error != "async_move_unconfirmed" {
					t.Fatalf("running move admitted: %+v %v", result, err)
				}
			}
			if requests != 1 || len(d.GetTasks()) != 0 || !d.AuthenticationReady() {
				t.Fatal("running work replayed, admitted, or rejected credential")
			}
		})
	}
}

func TestIntakePreviewCommitIsSingleUseAndFreezesMode(t *testing.T) {
	d, _, calls, _, _ := intakeFixture(t)
	d.SetDownloadMode(DownloadModeDelete)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("preview moved a source")
	}
	d.SetDownloadMode(DownloadModeKeep)
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" {
		t.Fatalf("commit failed: %+v %v", result, err)
	}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err != nil || calls.Load() != 1 {
		t.Fatal("committed intake replayed")
	}
	tasks := d.GetTasks()
	if len(tasks) != 1 || tasks[0].DownloadMode != DownloadModeDelete || tasks[0].Status != "pending" {
		t.Fatalf("wrong admitted task: %+v", tasks)
	}
}

func TestIntakeRejectsAmbiguousPathsAndChangedSource(t *testing.T) {
	d, p, calls, _, _ := intakeFixture(t)
	for _, paths := range [][]string{{"/"}, {"/queue"}, {"/queue/a"}, {"/source/../source/a.txt"}, {"/source/a.txt", "/source/a.txt"}, {"/source", "/source/a.txt"}} {
		if _, err := d.PreviewIntake(context.Background(), paths); err == nil {
			t.Fatalf("ambiguous paths accepted: %v", paths)
		}
	}
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	p.lists["/source"][0].Mtime++
	if _, err = d.CommitIntake(context.Background(), preview.ID); !errors.Is(err, ErrIntakeConflict) || calls.Load() != 0 {
		t.Fatalf("changed source moved: %v", err)
	}
}

func TestUnknownIntakeSurvivesRestartAndQuarantinesUntilRechecked(t *testing.T) {
	d, _, calls, unavailable, loseReply := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	loseReply.Store(true)
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" {
		t.Fatalf("lost reply not isolated: %+v %v", result, err)
	}
	unavailable.Store(false)
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	if len(d.GetTasks()) != 0 {
		t.Fatal("unconfirmed target entered download queue")
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	if _, err = reopened.CommitIntake(context.Background(), preview.ID); err != nil || calls.Load() != 1 {
		t.Fatal("restart replayed uncertain move")
	}
	if err = reopened.ScanAndDownload(); err != nil || len(reopened.GetTasks()) != 0 {
		t.Fatal("restart forgot target quarantine")
	}
	result, err = reopened.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" || len(reopened.GetTasks()) != 1 || calls.Load() != 1 {
		t.Fatalf("read-only recheck failed: %+v %v", result, err)
	}
}

func TestIntakePreviewDoesNotSurviveAuthenticationChangeOrRestart(t *testing.T) {
	d, _, calls, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Time{})
	if _, err = d.CommitIntake(context.Background(), preview.ID); !errors.Is(err, ErrIntakeStale) {
		t.Fatalf("stale auth preview accepted: %v", err)
	}
	fresh, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	if _, err = reopened.CommitIntake(context.Background(), fresh.ID); !errors.Is(err, ErrIntakeStale) || calls.Load() != 0 {
		t.Fatalf("restart reused preview: %v", err)
	}
}

func TestGetIntakesProjectsExpiredPreviewWithoutChangingJournal(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	d.intakeMutex.Lock()
	record := d.intakes[preview.ID]
	record.Expires = time.Now().Add(-time.Second)
	d.intakes[preview.ID] = record
	d.intakeMutex.Unlock()

	var projected *Intake
	for _, intake := range d.GetIntakes() {
		if intake.ID == preview.ID {
			projected = &intake
			break
		}
	}
	if projected == nil || projected.State != "expired" {
		t.Fatalf("expired preview should be projected as expired: %+v", projected)
	}
	stored, err := d.intakeJournal.LoadIntakes()
	if err != nil {
		t.Fatal(err)
	}
	var persisted intakeRecord
	if err = json.Unmarshal(stored[preview.ID], &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.State != "preview" {
		t.Fatalf("read projection rewrote the persisted state: %q", persisted.State)
	}
}

func TestGetIntakesProjectsAuthenticationAndRestartPreviewsExpired(t *testing.T) {
	t.Run("authentication epoch", func(t *testing.T) {
		d, _, _, _, _ := intakeFixture(t)
		preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
		if err != nil {
			t.Fatal(err)
		}
		d.SetAuthentication(d.getClient(), true, time.Time{})
		for _, intake := range d.GetIntakes() {
			if intake.ID == preview.ID && intake.State == "expired" {
				return
			}
		}
		t.Fatal("preview tied to the previous auth epoch was not projected as expired")
	})

	t.Run("process restart", func(t *testing.T) {
		d, _, _, _, _ := intakeFixture(t)
		preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
		if err != nil {
			t.Fatal(err)
		}
		cfg, client := d.cfg, d.getClient()
		shutdown(t, d)
		reopened, err := OpenDownloader(cfg, client)
		if err != nil {
			t.Fatal(err)
		}
		defer shutdown(t, reopened)
		for _, intake := range reopened.GetIntakes() {
			if intake.ID == preview.ID && intake.State == "expired" {
				return
			}
		}
		t.Fatal("preview from a previous process boot was not projected as expired")
	})
}

func TestLoadIntakesAcceptsCompletedHistoryFromPriorQueueDirectory(t *testing.T) {
	d, _, calls, unavailable, loseReply := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	loseReply.Store(true)
	if result, commitErr := d.CommitIntake(context.Background(), preview.ID); commitErr != nil || result.Items[0].State != "unknown" {
		t.Fatalf("lost move reply was not isolated: result=%+v err=%v", result, commitErr)
	}
	unavailable.Store(false)
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}, false); err != nil {
		t.Fatal(err)
	}
	cfg, client := d.cfg, d.getClient()
	shutdown(t, d)
	reopened, err := OpenDownloader(cfg, client)
	if err != nil {
		t.Fatalf("completed history from the previous queue directory must load after restart: %v", err)
	}
	defer shutdown(t, reopened)
	if calls.Load() != 1 {
		t.Fatalf("history load replayed a remote move: calls=%d", calls.Load())
	}
}

func TestRecheckIntakeUsesRecordedQueueDirectory(t *testing.T) {
	d, _, calls, unavailable, loseReply := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	loseReply.Store(true)
	if result, commitErr := d.CommitIntake(context.Background(), preview.ID); commitErr != nil || result.Items[0].State != "unknown" {
		t.Fatalf("lost move reply was not isolated: result=%+v err=%v", result, commitErr)
	}
	unavailable.Store(false)
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}, false); err != nil {
		t.Fatal(err)
	}
	result, err := d.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" || calls.Load() != 1 {
		t.Fatalf("recheck did not confirm the item in its recorded queue without resending: result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestPreviewIntakeRejectedDuringQueueMaintenance(t *testing.T) {
	d, _, calls, _, _ := intakeFixture(t)
	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()

	if _, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"}); err == nil {
		t.Fatal("intake must not begin while path maintenance or an async local migration owns queue maintenance")
	}
	if calls.Load() != 0 {
		t.Fatalf("maintenance rejection sent a remote move: %d", calls.Load())
	}
}

func TestIntakeStorageFailureBeforeSendingNeverMovesSource(t *testing.T) {
	d, _, calls, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "intake", preview.ID+".json")
	if err = os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(record, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err == nil || calls.Load() != 0 || d.QueueError() == nil {
		t.Fatalf("move escaped failed journal: %v", err)
	}
}

func TestRestartBetweenRootRegistrationAndMoveReceiptPreservesPendingRoot(t *testing.T) {
	d, p, calls, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := d.findIntake(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.State = "running"
	record.Items[0].State = "sending"
	if err = d.saveIntake(record); err != nil {
		t.Fatal(err)
	}
	destination := record.Items[0].Source
	destination.Path = record.Items[0].Target
	p.lists["/source"] = nil
	p.lists["/queue"] = []baiduapi.FileInfo{destination}
	if err = d.registerIntakeItem(record, record.Items[0], destination); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	if err = reopened.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, reopened, destination.FsID, "pending")
	result, err := reopened.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" || calls.Load() != 0 {
		t.Fatalf("restart recheck failed: %v", err)
	}
	if result.Items[0].SyncConfirmedAtUTC != "" || result.Items[0].QueueRegisteredAtUTC == "" {
		t.Fatal("recheck must record local registration verification without inventing an old synchronous receipt time")
	}
	assertStatus(t, reopened, destination.FsID, "pending")
}

func TestIntakeBatchReportsPartialFailureAndRejectsNewDestinationConflict(t *testing.T) {
	d, p, calls, _, _ := intakeFixture(t)
	for _, name := range []string{"b.txt", "c.txt"} {
		f := p.lists["/source"][0]
		f.FsID += int64(len(p.lists["/source"]))
		f.Path = "/source/" + name
		f.ServerFilename = name
		p.lists["/source"] = append(p.lists["/source"], f)
	}
	base := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("opera") == "move" {
			r.ParseForm()
			if strings.Contains(r.Form.Get("filelist"), "b.txt") {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"info":[{"errno":-8}]}`))}, nil
			}
		}
		return base.RoundTrip(r)
	})
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt", "/source/b.txt", "/source/c.txt"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" || result.Items[1].State != "failed" || result.Items[2].State != "moved" || calls.Load() != 2 {
		t.Fatalf("partial results lost: %+v %v", result, err)
	}
	second, err := d.PreviewIntake(context.Background(), []string{"/source/b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	conflict := p.lists["/source"][0]
	conflict.FsID = 999
	conflict.Path = "/queue/b.txt"
	p.lists["/queue"] = append(p.lists["/queue"], conflict)
	if _, err = d.CommitIntake(context.Background(), second.ID); !errors.Is(err, ErrIntakeConflict) || calls.Load() != 2 {
		t.Fatal("new target conflict was overwritten")
	}
}

func TestUnobservedMoveIsNotAssumedFailedWhenSourceStillExists(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	base := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("opera") == "move" {
			return nil, io.ErrUnexpectedEOF
		}
		return base.RoundTrip(r)
	})
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" {
		t.Fatal("unobserved move was guessed to have failed")
	}
	result, err = d.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" {
		t.Fatal("read-only recheck ignored possible late operation")
	}
}

func TestUnexpectedAsyncMoveDoesNotAdmitFolderFromRootIdentityAlone(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	p.lists["/source"][0].IsDir = 1
	base := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(r)
		if r.URL.Query().Get("opera") == "move" && err == nil {
			response.Body.Close()
			response.Body = io.NopCloser(strings.NewReader(`{"errno":0,"taskid":123,"info":[{"errno":0}]}`))
		}
		return response, err
	})
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" || len(d.GetTasks()) != 0 {
		t.Fatalf("async acceptance admitted unfinished folder: %+v %v", result, err)
	}
	result, err = d.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" {
		t.Fatal("root presence was treated as async folder completion")
	}
}

type observingIntakeJournal struct {
	intakeJournal
	saved func([]byte)
}

func (j observingIntakeJournal) SaveIntake(id string, body []byte) error {
	if err := j.intakeJournal.SaveIntake(id, body); err != nil {
		return err
	}
	j.saved(body)
	return nil
}
func TestAuthenticationRevokedWhileSendingJournalSavesPreventsMove(t *testing.T) {
	d, _, calls, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	d.intakeJournal = observingIntakeJournal{intakeJournal: d.intakeJournal, saved: func(body []byte) {
		var r intakeRecord
		if json.Unmarshal(body, &r) != nil {
			t.Fatal("invalid journal payload")
		}
		if r.Items[0].State == "sending" {
			d.SetAuthentication(d.getClient(), false, time.Time{})
		}
	}}
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "not_sent" || calls.Load() != 0 {
		t.Fatalf("revoked authorization still moved source: %+v %v calls=%d", result, err, calls.Load())
	}
}

type failingIntakeJournal struct {
	intakeJournal
	fail func() bool
}

func (j failingIntakeJournal) SaveIntake(id string, body []byte) error {
	if j.fail() {
		return errors.New("injected journal failure")
	}
	return j.intakeJournal.SaveIntake(id, body)
}

func TestLostDirectoryCompletionReceiptCannotBeRecoveredFromRootOnly(t *testing.T) {
	d, p, calls, _, _ := intakeFixture(t)
	p.lists["/source"][0].IsDir = 1
	base := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(r)
		if r.URL.Query().Get("opera") == "move" && err == nil {
			response.Body.Close()
			response.Body = io.NopCloser(strings.NewReader(`{"errno":0,"taskid":123,"info":[{"errno":0}]}`))
		}
		return response, err
	})
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	d.intakeJournal = failingIntakeJournal{intakeJournal: d.intakeJournal, fail: func() bool { return calls.Load() > 0 }}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err == nil {
		t.Fatal("journal failure ignored")
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	result, err := reopened.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" || len(reopened.GetTasks()) != 0 {
		t.Fatalf("lost directory completion receipt admitted root: %+v %v", result, err)
	}
}

func TestDurableSynchronousReceiptAllowsDirectoryRecoveryAfterLookupFailure(t *testing.T) {
	d, p, calls, _, _ := intakeFixture(t)
	p.lists["/source"][0].IsDir = 1
	base := http.DefaultTransport
	var failChecks atomic.Bool
	failChecks.Store(true)
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "list" && calls.Load() > 0 && failChecks.Load() {
			return nil, io.ErrUnexpectedEOF
		}
		return base.RoundTrip(r)
	})
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "unknown" || !result.Items[0].SyncConfirmed {
		t.Fatal("sync receipt was not retained before lookup")
	}
	shutdown(t, d)
	failChecks.Store(false)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	result, err = reopened.RecheckIntake(context.Background(), preview.ID)
	if err != nil || result.Items[0].State != "moved" || len(reopened.GetTasks()) != 1 || calls.Load() != 1 {
		t.Fatalf("durable directory receipt did not recover: %v", err)
	}
}

func TestIntakeStageTimesSurviveRestart(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	result, err := d.CommitIntake(context.Background(), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result.Items[0])
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sync_confirmed_at_utc", "queue_registered_at_utc"} {
		value, ok := fields[key].(string)
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if !ok || err != nil || parsed.Before(before) || parsed.After(time.Now().UTC()) || !strings.HasSuffix(value, "Z") {
			t.Fatalf("missing or invalid %s: %v", key, fields)
		}
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	stored, err := reopened.findIntake(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(stored.Items[0])
	if string(raw) != string(after) {
		t.Fatalf("stage evidence changed across restart: %s -> %s", raw, after)
	}
}

func TestIntakeTaskLinkRequiresDurableBatchOrigin(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CommitIntake(context.Background(), preview.ID); err != nil {
		t.Fatal(err)
	}
	check := func(d *Downloader, want bool) {
		t.Helper()
		raw, _ := json.Marshal(d.GetIntakes())
		var records []struct {
			Items []struct {
				DownloadTask *struct {
					TaskID   string `json:"task_id"`
					Status   string `json:"status"`
					Observed string `json:"observed_at_utc"`
				} `json:"download_task"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &records); err != nil {
			t.Fatal(err)
		}
		link := records[0].Items[0].DownloadTask
		if (link != nil) != want {
			t.Fatalf("want link %v, got %s", want, raw)
		}
		if link != nil && (link.TaskID != "123" || link.Status != "pending" || !strings.HasSuffix(link.Observed, "Z")) {
			t.Fatalf("invalid link: %s", raw)
		}
	}
	check(d, true)
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	check(reopened, true)
	reopened.queue.mu.Lock()
	origin := reopened.queue.jobs[123].IntakeID
	reopened.queue.jobs[123].IntakeID = ""
	reopened.queue.mu.Unlock()
	check(reopened, false)
	reopened.queue.mu.Lock()
	reopened.queue.jobs[123].IntakeID = origin
	reopened.queue.mu.Unlock()
	reopened.intakeMutex.Lock()
	isolated := reopened.intakes[preview.ID]
	isolated.Items[0].State = "unknown"
	reopened.intakes[preview.ID] = isolated
	reopened.intakeMutex.Unlock()
	check(reopened, false)
	reopened.intakeMutex.Lock()
	isolated.Items[0].State = "moved"
	reopened.intakes[preview.ID] = isolated
	reopened.intakeMutex.Unlock()
	reopened.intakeMutex.Lock()
	r := reopened.intakes[preview.ID]
	r.ID = strings.Repeat("f", 32)
	reopened.intakes = map[string]intakeRecord{r.ID: r}
	reopened.intakeMutex.Unlock()
	check(reopened, false)
}

func TestIntakeMonitorEvidenceIsFirstObservationAndSurvivesRestart(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	// The complete downloader suite exposed a race: this evidence-only fixture
	// rejects downloads, but ScanAndDownload also wakes the transfer scheduler.
	// Pause transfers explicitly while retaining real monitor scans and restart.
	if _, err := d.SetQueuePaused(true); err != nil {
		t.Fatal(err)
	}
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err != nil {
		t.Fatal(err)
	}
	evidence := func(d *Downloader) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(d.GetIntakes())
		var records []struct {
			Items []struct {
				Task struct {
					Monitor json.RawMessage `json:"monitor_observation"`
				} `json:"download_task"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &records); err != nil {
			t.Fatal(err)
		}
		return records[0].Items[0].Task.Monitor
	}
	if len(evidence(d)) != 0 {
		t.Fatal("registration invented monitor observation")
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	first := evidence(d)
	var observed struct {
		RunID string `json:"run_id"`
		Path  string `json:"path"`
		Time  string `json:"observed_at_utc"`
	}
	if err = json.Unmarshal(first, &observed); err != nil {
		t.Fatalf("missing evidence: %s %v", first, err)
	}
	if !validIntakeID(observed.RunID) || observed.Path != "/queue/a.txt" || !strings.HasSuffix(observed.Time, "Z") {
		t.Fatalf("invalid observation: %s", first)
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	if string(first) != string(evidence(d)) {
		t.Fatal("later scan replaced first observation")
	}
	shutdown(t, d)
	reopened, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, reopened)
	if string(first) != string(evidence(reopened)) {
		t.Fatal("restart changed first observation")
	}
}

func TestIntakeMonitorEvidenceFailureDoesNotPublishObservation(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err != nil {
		t.Fatal(err)
	}
	d.clientMutex.RLock()
	epoch := d.authEpoch
	d.clientMutex.RUnlock()
	d.queue.mu.Lock()
	info := d.queue.jobs[123].Info
	d.queue.mu.Unlock()
	if err = d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	err = d.queue.reconcileMonitorObserved([]baiduapi.FileInfo{info}, epoch, time.Now().UTC(), strings.Repeat("a", 32))
	if err == nil || d.QueueError() == nil || d.ctx.Err() == nil {
		t.Fatal("failed observation persistence must stop scheduling")
	}
	d.queue.mu.Lock()
	observation := d.queue.jobs[123].MonitorObservation
	d.queue.mu.Unlock()
	if observation != nil {
		t.Fatal("failed persistence published observation")
	}
}

func TestIntakeWaitReasonsReflectCurrentQueue(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	preview, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.CommitIntake(context.Background(), preview.ID); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		raw, _ := json.Marshal(d.GetIntakes())
		var records []struct {
			Items []struct {
				Task struct {
					Reason string `json:"wait_reason"`
				} `json:"download_task"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &records); err != nil {
			t.Fatal(err)
		}
		if records[0].Items[0].Task.Reason != want {
			t.Fatalf("want %s, got %s", want, raw)
		}
	}
	check("awaiting_scan")
	d.queue.mu.Lock()
	d.queue.userPaused = true
	d.queue.mu.Unlock()
	check("queue_paused")
	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()
	check("maintenance")
	d.queue.mu.Lock()
	d.queue.maintenance = false
	d.queue.userPaused = false
	job := d.queue.jobs[123]
	d.queue.eligible[123] = &job.Info
	d.queue.mu.Unlock()
	check("waiting_schedule")
	d.queue.mu.Lock()
	d.queue.blockedSources[job.Info.Path] = true
	d.queue.mu.Unlock()
	check("source_isolated")
	d.queue.mu.Lock()
	delete(d.queue.blockedSources, job.Info.Path)
	changed := job.Info
	changed.Size++
	d.queue.eligible[123] = &changed
	d.queue.mu.Unlock()
	check("source_changed")
	d.clientMutex.Lock()
	managed, ready := d.authManaged, d.authReady
	d.authManaged = true
	d.authReady = false
	d.clientMutex.Unlock()
	check("authorization_wait")
	d.clientMutex.Lock()
	d.authManaged, d.authReady = managed, ready
	d.clientMutex.Unlock()
	d.queue.mu.Lock()
	d.queue.stopping = true
	d.queue.mu.Unlock()
	check("stopped")
	d.queue.mu.Lock()
	d.queue.err = errors.New("test storage failure")
	d.queue.mu.Unlock()
	check("storage_error")
	d.queue.mu.Lock()
	d.queue.err = nil
	d.queue.stopping = false
	d.queue.mu.Unlock()
	d.queue.mu.Lock()
	job.Status = "done"
	d.queue.mu.Unlock()
	check("")
}
