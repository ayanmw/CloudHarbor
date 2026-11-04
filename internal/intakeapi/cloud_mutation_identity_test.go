package intakeapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/downloader"
)

type mutationCaptureBackend struct {
	*fakeBackend
	request downloader.CloudMutationRequest
}

func (f *mutationCaptureBackend) MutateCloud(_ context.Context, r downloader.CloudMutationRequest) (downloader.CloudMutationResult, error) {
	f.request = r
	f.mutationCalls++
	return downloader.CloudMutationResult{OperationID: strings.Repeat("a", 32), Status: "confirmed", ObjectID: r.Expected.FsID}, nil
}
func TestCloudMutationOptInStringExpectedIdentity(t *testing.T) {
	f := &mutationCaptureBackend{fakeBackend: &fakeBackend{}}
	s := New(f, nil)
	w := httptest.NewRecorder()
	body := `{"operation":"rename","expected":{"fs_id":"9007199254740993","path":"/old","server_filename":"old","size":"9223372036854775807","isdir":0,"server_mtime":"123"},"new_name":"new"}`
	s.HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations?identity_format=string", strings.NewReader(body)))
	if w.Code != 200 || f.request.Expected.FsID != 9007199254740993 || f.request.Expected.Size != 9223372036854775807 || !strings.Contains(w.Body.String(), `"object_id":"9007199254740993"`) {
		t.Fatalf("status=%d request=%+v body=%s", w.Code, f.request, w.Body.String())
	}
}

func TestCloudMutationLegacyRequestStillReachesBackend(t *testing.T) {
	f := &mutationCaptureBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	New(f, nil).HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{"operation":"rename","expected":{"fs_id":123,"path":"/old","server_filename":"old","size":5,"isdir":0,"server_mtime":1},"new_name":"new"}`)))
	if w.Code != 200 || f.request.Operation != "rename" || f.request.Expected.FsID != 123 || f.request.NewName != "new" {
		t.Fatalf("legacy request lost: %+v", f.request)
	}
}

func TestCloudMutationStringIdentityRejectsNoncanonicalNumbersWithoutMutation(t *testing.T) {
	for _, value := range []string{"-1", "01", "1e3", "9223372036854775808", ""} {
		f := &mutationCaptureBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		body := `{"operation":"delete","expected":{"fs_id":"` + value + `","size":"0","server_mtime":"1"}}`
		New(f, nil).HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations?identity_format=string", strings.NewReader(body)))
		if w.Code != 400 || f.mutationCalls != 0 {
			t.Fatalf("invalid value=%q status=%d calls=%d", value, w.Code, f.mutationCalls)
		}
	}
}

func TestCleanupRetainedIdentityUsesExactStringFormat(t *testing.T) {
	f := &mutationCaptureBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	body := `{"operation":"delete","expected":{"fs_id":"9007199254740993","path":"/copy","server_filename":"copy","size":"7","isdir":0,"server_mtime":"9"},"expected_content_sha256":"` + strings.Repeat("a", 64) + `","expected_retained":{"fs_id":"9007199254740995","path":"/keep","server_filename":"keep","size":"7","isdir":0,"server_mtime":"10"}}`
	New(f, nil).HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations?identity_format=string", strings.NewReader(body)))
	if w.Code != 200 || f.request.ExpectedRetained == nil || f.request.ExpectedRetained.FsID != 9007199254740995 {
		t.Fatalf("retained precision status=%d request=%+v", w.Code, f.request)
	}
}
