package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type realUploadPartTrace struct {
	SessionSHA256 string    `json:"session_sha256"`
	Path          string    `json:"path"`
	Part          string    `json:"part"`
	Started       time.Time `json:"started_at_utc"`
	FirstBodyRead time.Time `json:"first_body_read_at_utc"`
	Finished      time.Time `json:"response_headers_at_utc"`
	BodyBytes     int64     `json:"body_bytes_read"`
	Status        int       `json:"status"`
}

type realUploadBodyObserver struct {
	io.ReadCloser
	observe func(int)
}

func (r realUploadBodyObserver) Read(b []byte) (int, error) {
	n, err := r.ReadCloser.Read(b)
	r.observe(n)
	return n, err
}

type realUploadObservingTransport struct {
	base   http.RoundTripper
	mu     sync.Mutex
	traces []*realUploadPartTrace
}

// Observe and forward actual requests, without substituting responses, delaying
// transport, logging URLs/tokens/upload IDs, or retrying failed writes.
func (o *realUploadObservingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "d.pcs.baidu.com" || r.URL.Path != "/rest/2.0/pcs/superfile2" || r.URL.Query().Get("method") != "upload" {
		return o.base.RoundTrip(r)
	}
	query := r.URL.Query()
	digest := sha256.Sum256([]byte(query.Get("uploadid")))
	trace := &realUploadPartTrace{SessionSHA256: hex.EncodeToString(digest[:]), Path: query.Get("path"), Part: query.Get("partseq"), Started: time.Now().UTC()}
	o.mu.Lock()
	o.traces = append(o.traces, trace)
	o.mu.Unlock()
	r.Body = realUploadBodyObserver{ReadCloser: r.Body, observe: func(n int) {
		o.mu.Lock()
		defer o.mu.Unlock()
		if n > 0 && trace.FirstBodyRead.IsZero() {
			trace.FirstBodyRead = time.Now().UTC()
		}
		trace.BodyBytes += int64(n)
	}}
	response, err := o.base.RoundTrip(r)
	o.mu.Lock()
	trace.Finished = time.Now().UTC()
	if response != nil {
		trace.Status = response.StatusCode
	}
	o.mu.Unlock()
	return response, err
}

func TestRealOwnedParallelUploadAndNameConflict(t *testing.T) {
	descriptor := os.Getenv("NAS_REAL_UPLOAD_MATRIX_FIXTURE")
	if descriptor == "" {
		t.Skip("requires an explicitly owned real upload matrix fixture")
	}
	var f struct{ Release, LocalRoot, RemoteRoot, Evidence string }
	raw, err := os.ReadFile(descriptor)
	if err != nil || json.Unmarshal(raw, &f) != nil {
		t.Fatal("invalid descriptor")
	}
	base := filepath.Dir(descriptor)
	inside := func(value string) bool {
		r, e := filepath.Rel(base, value)
		return e == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)) && !filepath.IsAbs(r)
	}
	if !filepath.IsAbs(f.Release) || !inside(f.LocalRoot) || !inside(f.Evidence) || !strings.HasPrefix(filepath.Base(f.LocalRoot), "local-upload-matrix-real-") || !strings.HasPrefix(f.RemoteRoot, "/_NAS_Downloader_UploadMatrix_20261107_") || strings.Contains(strings.TrimPrefix(f.RemoteRoot, "/"), "/") {
		t.Fatal("fixture boundary invalid")
	}
	for _, path := range []string{f.LocalRoot, f.Evidence} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("requires fresh fixture and evidence; never replay")
		}
	}
	credentialPath := filepath.Join(f.Release, ".nas-downloader", "queue", "credentials.json")
	credentialRaw, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal("original credentials unavailable")
	}
	originalSHA := sha256.Sum256(credentialRaw)
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	if json.Unmarshal(credentialRaw, &credentials) != nil || credentials.AccessToken == "" || credentials.AppKey == "" {
		t.Fatal("original credentials incomplete")
	}
	client := baiduapi.NewClient(credentials.AccessToken, credentials.AppKey)
	proof := map[string]any{"root": f.RemoteRoot, "started_at_utc": time.Now().UTC()}
	save := func() {
		b, e := json.MarshalIndent(proof, "", "  ")
		if e != nil || os.WriteFile(f.Evidence, b, 0600) != nil {
			t.Fatal("cannot persist proof")
		}
	}
	proof["mkdir_intent_at_utc"] = time.Now().UTC()
	save()
	if err = client.CreateDirectory(f.RemoteRoot); err != nil {
		t.Fatal("fixture creation unconfirmed; never replay")
	}
	originalTransport := http.DefaultTransport
	observer := &realUploadObservingTransport{base: originalTransport}
	http.DefaultTransport = observer
	defer func() { http.DefaultTransport = originalTransport }()
	d, err := downloader.OpenDownloader(&config.Config{LocalDownDir: f.LocalRoot, QueueDir: f.RemoteRoot + "/monitor", QueueDirExplicit: true, LocalDownDirExplicit: true, MaxParallel: 2}, client)
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
	if err != nil || d.BindAccount(user) != nil {
		t.Fatal("isolated account binding failed")
	}
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	m, err := Open(filepath.Join(f.LocalRoot, ".nas-downloader", "uploads"), d.UploadStagedFile, d.AuthenticationReady)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetReceiptVerifier(d.VerifyUploadReceipt)
	if err = m.SetConcurrency(2); err != nil {
		t.Fatal(err)
	}
	const size = 4*1024*1024 + 97
	content := map[string][]byte{}
	inputs := []Input{{Path: "parallel-a.bin", Size: strconv.Itoa(size)}, {Path: "parallel-b.bin", Size: strconv.Itoa(size)}}
	hashes := map[string]string{}
	for _, input := range inputs {
		b := make([]byte, size)
		if _, err = rand.Read(b); err != nil {
			t.Fatal(err)
		}
		content[input.Path] = b
		h := sha256.Sum256(b)
		hashes[input.Path] = hex.EncodeToString(h[:])
	}
	proof["expected_sha256"] = hashes
	proof["parallel_batch_intent_at_utc"] = time.Now().UTC()
	save()
	batch, err := m.Create(f.RemoteRoot, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range batch.Items {
		if err = m.Receive(context.Background(), batch.ID, item.ID, bytes.NewReader(content[item.Path])); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(batchID string, count int) Batch {
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			b, e := m.Get(batchID)
			if e != nil {
				t.Fatal(e)
			}
			confirmed := 0
			for _, item := range b.Items {
				if item.State == "failed" || item.State == "unknown" {
					proof["stopped_batch"] = b
					save()
					t.Fatalf("upload stopped: %s; no retry", item.State)
				}
				if item.State == "confirmed" {
					confirmed++
				}
			}
			if confirmed == count {
				return b
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("upload deadline; no retry")
		return Batch{}
	}
	finished := wait(batch.ID, 2)
	observer.mu.Lock()
	traces := make([]realUploadPartTrace, len(observer.traces))
	for i, trace := range observer.traces {
		traces[i] = *trace
	}
	observer.mu.Unlock()
	overlap := false
	for _, a := range traces {
		for _, b := range traces {
			if a.SessionSHA256 != b.SessionSHA256 && a.Status == 200 && b.Status == 200 && a.BodyBytes > 0 && b.BodyBytes > 0 && !a.FirstBodyRead.IsZero() && !b.FirstBodyRead.IsZero() && a.FirstBodyRead.Before(b.Finished) && b.FirstBodyRead.Before(a.Finished) {
				overlap = true
			}
		}
	}
	proof["parallel_batch"] = finished
	proof["actual_part_traces"] = traces
	proof["distinct_sessions_body_request_overlap"] = overlap
	save()
	if !overlap {
		t.Fatal("two actual provider part requests did not overlap; do not infer from admission count")
	}
	verify := func(info baiduapi.FileInfo, expected string) {
		link, e := client.GetDownloadLink(info.FsID)
		if e != nil {
			t.Fatal("readback link unavailable")
		}
		r, _, e := client.DownloadFile(link)
		if e != nil {
			t.Fatal("readback failed")
		}
		h := sha256.New()
		n, e := io.Copy(h, r)
		r.Close()
		if e != nil || n != info.Size || hex.EncodeToString(h.Sum(nil)) != expected {
			t.Fatal("readback content mismatch")
		}
	}
	entries, err := client.ListDir(f.RemoteRoot)
	if err != nil || len(entries) != 2 {
		t.Fatal("parallel remote entries missing")
	}
	var original baiduapi.FileInfo
	for _, info := range entries {
		verify(info, hashes[info.ServerFilename])
		if info.ServerFilename == "parallel-a.bin" {
			original = info
		}
	}
	proof["parallel_readback_sha_verified"] = true
	proof["original_before_conflict"] = original
	proof["conflict_batch_intent_at_utc"] = time.Now().UTC()
	save()
	conflictContent := []byte("Owned same-name conflict: retain both distinct contents.\n")
	conflictSHA := sha256.Sum256(conflictContent)
	conflict, err := m.Create(f.RemoteRoot, []Input{{Path: "parallel-a.bin", Size: strconv.Itoa(len(conflictContent))}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), conflict.ID, conflict.Items[0].ID, bytes.NewReader(conflictContent)); err != nil {
		t.Fatal(err)
	}
	conflict = wait(conflict.ID, 1)
	proof["conflict_batch"] = conflict
	save()
	if conflict.Items[0].ActualPath == original.Path || !strings.HasPrefix(conflict.Items[0].ActualPath, f.RemoteRoot+"/") {
		t.Fatal("same-name upload did not return a distinct owned actual path")
	}
	after, err := client.GetFileInfo(original.FsID)
	if err != nil || after.Path != original.Path || after.Size != original.Size || after.Mtime != original.Mtime {
		t.Fatal("original changed after conflict")
	}
	entries, err = client.ListDir(f.RemoteRoot)
	if err != nil || len(entries) != 3 {
		t.Fatal("conflict did not retain all three files")
	}
	newFound := false
	for _, info := range entries {
		if info.Path == conflict.Items[0].ActualPath && info.FsID != original.FsID {
			verify(info, hex.EncodeToString(conflictSHA[:]))
			proof["conflict_new_identity"] = info
			newFound = true
		}
	}
	if !newFound {
		t.Fatal("new conflict identity not found")
	}
	proof["original_after_conflict"] = after
	proof["same_name_retained_both_verified"] = true
	proof["conflict_sha256"] = hex.EncodeToString(conflictSHA[:])
	current, err := os.ReadFile(credentialPath)
	if err != nil || sha256.Sum256(current) != originalSHA {
		t.Fatal("original credentials changed")
	}
	proof["original_credentials_unchanged"] = true
	proof["finished_at_utc"] = time.Now().UTC()
	save()
}
