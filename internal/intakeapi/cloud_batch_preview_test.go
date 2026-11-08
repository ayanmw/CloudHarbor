package intakeapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/downloader"
)

type previewBackend struct {
	*fakeBackend
	request downloader.CloudBatchPreviewRequest
	calls   int
	err     error
}

func (b *previewBackend) PreviewCloudBatch(_ context.Context, r downloader.CloudBatchPreviewRequest) (downloader.CloudBatchPreviewResult, error) {
	b.calls++
	b.request = r
	return downloader.CloudBatchPreviewResult{RequestID: r.RequestID, Phase: r.Phase, SelectionIDs: []string{"9007199254740993"}}, b.err
}

const validPreviewJSON = `{"request_id":"read-1","phase":"sources","operation":"move","destination_directory":"/target","expected":[{"fs_id":"9007199254740993","path":"/source/a","server_filename":"a","size":"7","isdir":0,"server_mtime":"1"}]}`

func TestCloudBatchPreviewHTTPPreservesExactIdentityAndIsReadOnly(t *testing.T) {
	b := &previewBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	New(b, nil).HandleCloudBatchPreview(w, httptest.NewRequest("POST", "/api/cloud/mutations/preview", strings.NewReader(validPreviewJSON)))
	if w.Code != 200 || b.calls != 1 || b.request.Expected[0].FsID != 9007199254740993 || !strings.Contains(w.Body.String(), `"9007199254740993"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, b.calls, w.Body.String())
	}
	if b.mutationCalls != 0 {
		t.Fatal("preview dispatched mutation")
	}
}
func TestCloudBatchPreviewHTTPRejectsInvalidWireWithoutBackendReads(t *testing.T) {
	for _, body := range []string{strings.Replace(validPreviewJSON, `"9007199254740993"`, `9007199254740993`, 1), strings.Replace(validPreviewJSON, `"9007199254740993"`, `"01"`, 1), strings.Replace(validPreviewJSON, `"move"`, `"delete"`, 1), strings.Replace(validPreviewJSON, `"sources"`, `"invalid"`, 1), strings.TrimSuffix(validPreviewJSON, "}") + `,"new_name":"secret"}`, validPreviewJSON + ` {}`, `{}`} {
		b := &previewBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		New(b, nil).HandleCloudBatchPreview(w, httptest.NewRequest("POST", "/api/cloud/mutations/preview", strings.NewReader(body)))
		if w.Code != 400 || b.calls != 0 || b.mutationCalls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", w.Code, b.calls, w.Body.String())
		}
	}
}
func TestCloudBatchPreviewHTTPMethodAndErrorsDoNotLeakProviderSecrets(t *testing.T) {
	b := &previewBackend{fakeBackend: &fakeBackend{}, err: errors.New("access_token=must-not-leak")}
	s := New(b, nil)
	w := httptest.NewRecorder()
	s.HandleCloudBatchPreview(w, httptest.NewRequest("GET", "/api/cloud/mutations/preview", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "POST" || b.calls != 0 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.HandleCloudBatchPreview(w, httptest.NewRequest("POST", "/api/cloud/mutations/preview", strings.NewReader(validPreviewJSON)))
	if w.Code != 502 || strings.Contains(w.Body.String(), "must-not-leak") || !strings.Contains(w.Body.String(), "读取操作可安全重试") {
		t.Fatal(w.Code, w.Body.String())
	}
}
