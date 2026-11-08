package intakeapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type directorySearchBackend struct {
	*fakeBackend
	directoryCalls int
	generation     *int64
}

func (b *directorySearchBackend) SearchCachedCloudDirectories(_ context.Context, q string, offset, limit int, expected *int64) ([]taskstore.CloudInventoryEntry, int, taskstore.CloudInventorySearchCoverage, error) {
	b.directoryCalls++
	b.generation = expected
	return []taskstore.CloudInventoryEntry{{FsID: 9007199254740993, Path: "/lesson", Name: "lesson", Directory: true}}, 1, taskstore.CloudInventorySearchCoverage{Generation: 7, Status: "partial"}, nil
}

func TestCloudDirectorySearchOptInUsesDirectoryBackendAndExactIDs(t *testing.T) {
	b := &directorySearchBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&directories_only=1&cache_generation=7&identity_format=string", nil))
	if w.Code != 200 || b.directoryCalls != 1 || b.generation == nil || *b.generation != 7 || !strings.Contains(w.Body.String(), `"directories_only":true`) || !strings.Contains(w.Body.String(), `"fs_id":"9007199254740993"`) {
		t.Fatalf("calls=%d status=%d body=%s", b.directoryCalls, w.Code, w.Body.String())
	}
}

func TestCloudDirectorySearchRejectsInvalidFilterBeforeBackend(t *testing.T) {
	for _, q := range []string{"directories_only=0", "directories_only=true", "directories_only=", "directories_only=1&directories_only=1"} {
		b := &directorySearchBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&"+q, nil))
		if w.Code != 400 || b.directoryCalls != 0 {
			t.Fatalf("q=%s status=%d calls=%d", q, w.Code, b.directoryCalls)
		}
	}
}
