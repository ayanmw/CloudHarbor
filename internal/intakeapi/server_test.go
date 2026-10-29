package intakeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type fakeBackend struct {
	commits    int
	previews   int
	cloudErr   error
	previewErr error
}

func (f *fakeBackend) ListCloud(context.Context, string, int, int) (downloader.CloudListing, error) {
	return downloader.CloudListing{Directory: "/", Entries: []downloader.CloudEntry{{ID: "3400000000000000123", Path: "/a", Name: "a"}}}, f.cloudErr
}
func (f *fakeBackend) PreviewIntake(context.Context, []string) (downloader.Intake, error) {
	f.previews++
	return downloader.Intake{ID: strings.Repeat("a", 32), State: "preview"}, f.previewErr
}
func (f *fakeBackend) CommitIntake(context.Context, string) (downloader.Intake, error) {
	f.commits++
	return downloader.Intake{State: "finished", Items: []downloader.IntakeItem{{State: "unknown"}}}, nil
}
func (f *fakeBackend) RecheckIntake(context.Context, string) (downloader.Intake, error) {
	return downloader.Intake{Items: []downloader.IntakeItem{{State: "unknown"}}}, nil
}
func (f *fakeBackend) GetIntakes() []downloader.Intake {
	return []downloader.Intake{{ID: strings.Repeat("a", 32), State: "finished", Items: []downloader.IntakeItem{{State: "unknown"}}}}
}
func (f *fakeBackend) ListCachedCloud(context.Context, string, int, int) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error) {
	return nil, taskstore.CloudInventoryDirectory{Status: "complete"}, 0, nil
}
func (f *fakeBackend) GetCachedCloudDirectorySize(context.Context, string) (taskstore.CloudDirectorySize, error) {
	return taskstore.CloudDirectorySize{Complete: true}, nil
}
func (f *fakeBackend) CloudInventoryScanStatus() (taskstore.CloudScanState, error) {
	return taskstore.CloudScanState{Status: "complete"}, nil
}
func (f *fakeBackend) StartCloudInventoryScan(context.Context) (taskstore.CloudScanState, error) {
	return taskstore.CloudScanState{Status: "running"}, nil
}
func (f *fakeBackend) PauseCloudInventoryScan() {}
func (f *fakeBackend) CloudDuplicateScanStatus() (taskstore.CloudDuplicateScanState, error) {
	return taskstore.CloudDuplicateScanState{Status: "paused"}, nil
}
func (f *fakeBackend) StartCloudDuplicateScan(context.Context) (taskstore.CloudDuplicateScanState, error) {
	return taskstore.CloudDuplicateScanState{Status: "running"}, nil
}
func (f *fakeBackend) PauseCloudDuplicateScan() {}
func (f *fakeBackend) CloudDuplicatePreview() ([]taskstore.CloudDuplicateGroup, error) {
	return []taskstore.CloudDuplicateGroup{}, nil
}
func (f *fakeBackend) PreviewEmptyCloudDirectories(context.Context) ([]taskstore.CloudInventoryEntry, error) {
	return []taskstore.CloudInventoryEntry{{FsID: 9, Path: "/empty", Name: "empty", Directory: true}}, nil
}
func (f *fakeBackend) MutateCloud(context.Context, downloader.CloudMutationRequest) (downloader.CloudMutationResult, error) {
	return downloader.CloudMutationResult{OperationID: strings.Repeat("b", 32), Status: "confirmed"}, nil
}

func TestIntakeHTTPUsesStrictInputsAndNeverAddsManualRelease(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest("POST", "/api/intakes/preview", strings.NewReader(`{"paths":["/a"],"target":"/elsewhere"}`)))
	if w.Code != 400 || backend.previews != 0 {
		t.Fatal("client-selected target accepted")
	}
	w = httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest("POST", "/api/intakes/preview", strings.NewReader(`{"paths":["/a"]}`)))
	if w.Code != 200 || backend.previews != 1 {
		t.Fatal("preview not invoked")
	}
	w = httptest.NewRecorder()
	server.HandleAction(w, httptest.NewRequest("POST", "/api/intakes/"+strings.Repeat("a", 32)+"/commit", strings.NewReader(`{}`)))
	if w.Code != 200 || backend.commits != 1 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal("partial/unknown result hidden")
	}
	for _, action := range []string{"release", "acknowledge", "force", "confirm-manual"} {
		w = httptest.NewRecorder()
		server.HandleAction(w, httptest.NewRequest("POST", "/api/intakes/"+strings.Repeat("a", 32)+"/"+action, nil))
		if w.Code != 404 {
			t.Fatal("manual release route exists")
		}
	}
}

func TestDuplicateCleanupEndpointsExposeStatusPreviewAndPause(t *testing.T) {
	s := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest(http.MethodPost, "/api/cloud-cleanup/duplicate-scan", nil))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("start response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleDuplicatePreview(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicates", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"groups":[]`) {
		t.Fatalf("preview response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest(http.MethodDelete, "/api/cloud-cleanup/duplicate-scan", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("pause response %d", w.Code)
	}
}

func TestCloudMutationAPIRequiresStrictExpectedIdentity(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{"operation":"delete","expected":{"fs_id":3,"path":"/a","server_filename":"a","size":1,"isdir":0,"server_mtime":4},"force":true}`)))
	if w.Code != 400 {
		t.Fatalf("unknown mutation field accepted: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{"operation":"delete","expected":{"fs_id":3,"path":"/a","server_filename":"a","size":1,"isdir":0,"server_mtime":4}}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("mutation response=%d %s", w.Code, w.Body.String())
	}
}

func TestCloudInventoryEndpointsExposeCacheSizeAndBackgroundScan(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventory(w, httptest.NewRequest("GET", "/api/cloud-inventory?dir=%2F", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"complete"`) {
		t.Fatalf("inventory response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventorySize(w, httptest.NewRequest("GET", "/api/cloud-inventory/size?dir=%2F", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"complete":true`) {
		t.Fatalf("size response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("GET", "/api/cloud-inventory/scan", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"complete"`) || !strings.Contains(w.Body.String(), `"pending_count":0`) || strings.Contains(w.Body.String(), `"pending":[`) {
		t.Fatalf("scan status response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("POST", "/api/cloud-inventory/scan", nil))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("scan start response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("DELETE", "/api/cloud-inventory/scan", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("scan pause response %d", w.Code)
	}
}

func TestEmptyDirectoryPreviewEndpointReturnsCandidatesAndScanEvidence(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleEmptyCloudDirectories(w, httptest.NewRequest("GET", "/api/cloud-cleanup/empty-directories", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"path":"/empty"`) || !strings.Contains(w.Body.String(), `"scan_run_id":""`) || !strings.Contains(w.Body.String(), `"status":"complete"`) {
		t.Fatalf("empty directory preview response %d %s", w.Code, w.Body.String())
	}
}

func TestCloudListingPreservesIDsAndBoundsPagination(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?dir=/&offset=-1", nil))
	if w.Code != 400 {
		t.Fatal("negative pagination accepted")
	}
	w = httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=/", nil))
	var value struct {
		Entries []struct {
			ID string `json:"id"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil || value.Entries[0].ID != "3400000000000000123" {
		t.Fatal("cloud ID lost precision")
	}
}

func TestReadOnlyCloudFailuresShowSafeRetryableDiagnostics(t *testing.T) {
	backend := &fakeBackend{cloudErr: &baiduapi.APIError{Operation: "list", Status: http.StatusOK, Code: -9}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=%2Fqueue", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "HTTP=200") || !strings.Contains(w.Body.String(), "错误码=-9") || !strings.Contains(w.Body.String(), "可安全重试") || strings.Contains(w.Body.String(), "重发移动") {
		t.Fatalf("read-only cloud failure did not explain safe retry: status=%d body=%q", w.Code, w.Body.String())
	}

	backend.previewErr = errors.New("Get https://pan.baidu.com/?access_token=secret-value: dial tcp refused")
	w = httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest(http.MethodPost, "/api/intakes/preview", strings.NewReader(`{"paths":["/a"]}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "可安全重试") || strings.Contains(w.Body.String(), "secret-value") || strings.Contains(w.Body.String(), "重发移动") {
		t.Fatalf("read-only preview failure leaked request details or used mutation warning: status=%d body=%q", w.Code, w.Body.String())
	}
}
