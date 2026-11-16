package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
)

// Explicit real-account acceptance only. The original credential file is read
// into memory; all jobs, manifests and content belong to a new isolated root.
// The controller kills the first process after its durable partial-ready proof.
func TestRealOwnedMonitorLifecycle(t *testing.T) {
	descriptor := os.Getenv("NAS_REAL_MONITOR_FIXTURE")
	if descriptor == "" {
		t.Skip("requires explicit owned real monitor fixture")
	}
	phase := os.Getenv("NAS_REAL_MONITOR_PHASE")
	if phase != "interrupt" && phase != "complete" && phase != "verify" {
		t.Fatal("invalid real monitor phase")
	}
	var f struct {
		Release      string `json:"release"`
		RemoteQueue  string `json:"remote_queue"`
		RootID       int64  `json:"root_id"`
		RootName     string `json:"root_name"`
		FileName     string `json:"file_name"`
		Size         int64  `json:"size"`
		SHA256       string `json:"sha256"`
		LocalRoot    string `json:"local_root"`
		EvidenceBase string `json:"evidence_base"`
	}
	raw, err := os.ReadFile(descriptor)
	if err != nil || json.Unmarshal(raw, &f) != nil {
		t.Fatal("invalid real monitor descriptor")
	}
	evidenceRoot := filepath.Dir(descriptor)
	inside := func(value string) bool {
		rel, e := filepath.Rel(evidenceRoot, value)
		return e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	if !filepath.IsAbs(f.Release) || !filepath.IsAbs(f.LocalRoot) || !inside(f.LocalRoot) || !inside(f.EvidenceBase) || !strings.HasPrefix(filepath.Base(f.LocalRoot), "local-monitor-real-") || !strings.HasPrefix(f.RemoteQueue, "/_NAS_Downloader_UploadResume_20261107_") || strings.Contains(strings.TrimPrefix(f.RemoteQueue, "/"), "/") || f.RootName != "monitor-tree" || f.FileName != "restart-partial.bin" || f.RootID <= 0 || f.Size != 8388705 || len(f.SHA256) != 64 {
		t.Fatal("real monitor fixture boundary invalid")
	}
	credentialPath := filepath.Join(f.Release, ".nas-downloader", "queue", "credentials.json")
	credentialRaw, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal("original credential file unavailable")
	}
	originalCredentialSHA := sha256.Sum256(credentialRaw)
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	if json.Unmarshal(credentialRaw, &credentials) != nil || credentials.AccessToken == "" || credentials.AppKey == "" {
		t.Fatal("original credentials incomplete")
	}
	if phase == "interrupt" {
		if _, err = os.Stat(f.LocalRoot); !os.IsNotExist(err) {
			t.Fatal("first phase requires a new local root; never replay")
		}
	}
	if err = os.MkdirAll(f.LocalRoot, 0700); err != nil {
		t.Fatal(err)
	}
	client := baiduapi.NewClient(credentials.AccessToken, credentials.AppKey)
	d, err := OpenDownloader(&config.Config{LocalDownDir: f.LocalRoot, QueueDir: f.RemoteQueue, QueueDirExplicit: true, LocalDownDirExplicit: true, MaxParallel: 2, MaxRetries: 0}, client)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := d.Shutdown(ctx); e != nil {
			t.Error(e)
		}
	}()
	user, err := client.UserID()
	if err != nil {
		t.Fatal(err)
	}
	if err = d.queue.store.BindAccount(user); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	d.SetDownloadMode(DownloadModeKeep)
	checkNoCredentialCopy := func() {
		t.Helper()
		current, e := os.ReadFile(credentialPath)
		if e != nil || sha256.Sum256(current) != originalCredentialSHA {
			t.Fatal("original credentials changed")
		}
		e = filepath.WalkDir(filepath.Join(f.LocalRoot, stateDirectory), func(path string, entry os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Name() == "credentials.json" || entry.Name() == "baidu-app.json" || entry.Name() == "admin-key.json" {
				return fmt.Errorf("unexpected credential sidecar")
			}
			// Windows exclusively locks byte zero of writer.lock. Empty files
			// cannot contain either nonempty credential; do not read its handle.
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if info.Size() == 0 {
				return nil
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if bytes.Contains(b, []byte(credentials.AccessToken)) || bytes.Contains(b, []byte(credentials.AppKey)) {
				return fmt.Errorf("credential content persisted in isolated state")
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	readJob := func() *queueJob {
		d.queue.mu.Lock()
		defer d.queue.mu.Unlock()
		j := d.queue.jobs[f.RootID]
		if j == nil {
			return nil
		}
		copy := *j
		copy.TransferStats = copyTransferStatistics(j.TransferStats)
		return &copy
	}
	rootActive := func() bool { d.queue.mu.Lock(); defer d.queue.mu.Unlock(); return d.queue.active[f.RootID] != nil }
	writeProof := func(value any) {
		t.Helper()
		b, e := json.MarshalIndent(value, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(f.EvidenceBase+"-"+phase+".json", b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	checkNoCredentialCopy()
	var beforeIdle, afterIdle *queueJob
	if phase != "interrupt" {
		beforeIdle = readJob()
		if beforeIdle == nil {
			t.Fatal("original isolated root not restored")
		}
		time.Sleep(1500 * time.Millisecond)
		afterIdle = readJob()
		a, _ := json.Marshal(beforeIdle.TransferStats)
		b, _ := json.Marshal(afterIdle.TransferStats)
		if !bytes.Equal(a, b) {
			t.Fatal("offline/idle time changed measured transfer statistics")
		}
	}
	if phase == "verify" {
		if afterIdle.Status != "done" || afterIdle.SourceState != "confirmed" {
			t.Fatalf("completed isolated root not restored: status=%s source=%s", afterIdle.Status, afterIdle.SourceState)
		}
		checkNoCredentialCopy()
		writeProof(map[string]any{"phase": phase, "recorded_at_utc": time.Now().UTC(), "job": afterIdle, "idle_stats_unchanged": true, "credentials_not_copied": true})
		return
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join(f.RootName, f.FileName)
	key := sha256.Sum256([]byte(relative))
	partial := filepath.Join(f.LocalRoot, stateDirectory, hex.EncodeToString(key[:]), "content.part")
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		job := readJob()
		if job != nil && (job.Status == "failed" || (job.Status == "source_unknown" && job.SourceState != "sending") || job.Status == "auth_wait") {
			t.Fatalf("isolated monitor stopped: status=%s source=%s error=%s", job.Status, job.SourceState, job.Error)
		}
		if phase == "interrupt" {
			stat, e := os.Stat(partial)
			if e == nil && stat.Size() >= 512*1024 && stat.Size() < f.Size {
				if empty, e := os.Stat(filepath.Join(f.LocalRoot, f.RootName, "empty")); e != nil || !empty.IsDir() {
					t.Fatal("empty child directory was not precreated")
				}
				m, e := d.queue.store.CurrentDownloadManifest(context.Background(), f.RootID)
				if e != nil || m == nil {
					t.Fatal("no complete manifest before transfer")
				}
				entries, e := d.queue.store.DownloadManifestEntries(context.Background(), m.ID, 0, 10)
				if e != nil || len(entries) != 3 {
					t.Fatal("manifest did not preserve root/file/empty directory")
				}
				checkNoCredentialCopy()
				writeProof(map[string]any{"phase": phase, "recorded_at_utc": time.Now().UTC(), "job": job, "partial_bytes": stat.Size(), "manifest": m, "entries": entries, "empty_directory_precreated": true, "credentials_not_copied": true})
				fmt.Println("REAL_MONITOR_PARTIAL_READY")
				time.Sleep(30 * time.Second)
				t.Fatal("controller did not terminate the partial-download process; never replay")
			}
			if job != nil && job.Status == "done" {
				t.Fatal("missed interruption window; never replay")
			}
		} else if job != nil && job.Status == "done" {
			if rootActive() {
				time.Sleep(40 * time.Millisecond)
				continue
			}
			job = readJob()
			if job.SourceState != "confirmed" {
				t.Fatal("root completed without confirmed keep postprocessing")
			}
			file, e := os.Open(filepath.Join(f.LocalRoot, relative))
			if e != nil {
				t.Fatal(e)
			}
			h := sha256.New()
			size, e := io.Copy(h, file)
			file.Close()
			if e != nil || size != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
				t.Fatal("published content differs from owned fixture")
			}
			manifest, e := d.queue.store.CurrentDownloadManifest(context.Background(), f.RootID)
			if e != nil || manifest == nil {
				t.Fatal("current manifest missing")
			}
			entries, e := d.queue.store.DownloadManifestEntries(context.Background(), manifest.ID, 0, 10)
			if e != nil || len(entries) != 3 {
				t.Fatal("complete manifest entries missing")
			}
			events, e := d.queue.store.Events(f.RootID)
			if e != nil {
				t.Fatal(e)
			}
			var observations []map[string]any
			for _, e := range events {
				if e.Kind == "source_operation_confirmed" || e.Kind == "download_attempt_started" || e.Kind == "download_attempt_finished" || e.Kind == "startup_recovery" {
					observations = append(observations, map[string]any{"kind": e.Kind, "sequence": e.Sequence, "recorded_at_utc": e.RecordedAtUTC, "attempt_id": e.AttemptID, "operation_id": e.OperationID})
				}
			}
			if job.TransferStats == nil || job.TransferStats.ActiveNanos <= 0 || job.TransferStats.NetworkBytes <= 0 {
				t.Fatal("measured transfer statistics missing")
			}
			checkNoCredentialCopy()
			writeProof(map[string]any{"phase": phase, "recorded_at_utc": time.Now().UTC(), "job": job, "before_idle": beforeIdle, "after_idle": afterIdle, "idle_stats_unchanged": true, "manifest": manifest, "entries": entries, "events": observations, "sha256": f.SHA256, "bytes": size, "credentials_not_copied": true})
			if job.TransferStats.NetworkBytes < f.Size && !job.TransferStats.Partial {
				t.Fatal("recovered measured statistics omit bytes but are marked complete")
			}
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("real monitor observation deadline; do not restart or replay automatically")
}
