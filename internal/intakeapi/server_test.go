package intakeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/downloader"
)

type fakeBackend struct {
	commits  int
	previews int
}

func (f *fakeBackend) ListCloud(context.Context, string, int, int) (downloader.CloudListing, error) {
	return downloader.CloudListing{Directory: "/", Entries: []downloader.CloudEntry{{ID: "3400000000000000123", Path: "/a", Name: "a"}}}, nil
}
func (f *fakeBackend) PreviewIntake(context.Context, []string) (downloader.Intake, error) {
	f.previews++
	return downloader.Intake{ID: strings.Repeat("a", 32), State: "preview"}, nil
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
