package intakeapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type filteredSearchBackend struct {
	*fakeBackend
	calls    int
	filter   taskstore.CloudInventorySearchFilter
	expected *int64
}

func (b *filteredSearchBackend) SearchCachedCloudFiltered(_ context.Context, q string, offset, limit int, expected *int64, filter taskstore.CloudInventorySearchFilter) ([]taskstore.CloudInventoryEntry, int, taskstore.CloudInventorySearchCoverage, error) {
	b.calls++
	b.filter = filter
	b.expected = expected
	return []taskstore.CloudInventoryEntry{}, 0, taskstore.CloudInventorySearchCoverage{Generation: 7, Status: "incomplete"}, nil
}
func TestCloudSearchFilteredContract(t *testing.T) {
	b := &filteredSearchBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&scope="+url.QueryEscape("/课程%_")+"&kind=file&cache_generation=7&identity_format=string", nil))
	var result struct{ Scope, Kind string }
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || b.calls != 1 || b.filter.Scope != "/课程%_" || b.filter.Kind != "file" || b.expected == nil || *b.expected != 7 || result.Scope != b.filter.Scope || result.Kind != "file" {
		t.Fatalf("code=%d calls=%d filter=%+v body=%s", w.Code, b.calls, b.filter, w.Body.String())
	}
}
func TestCloudSearchInvalidFiltersNeverCallBackend(t *testing.T) {
	for _, query := range []string{"scope=relative", "scope=/a/../b", "scope=", "scope=/&scope=/a", "kind=video", "kind=", "kind=file&kind=file", "directories_only=1&kind=directory", "directories_only=1&scope=/", "q=again"} {
		b := &filteredSearchBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&"+query, nil))
		if w.Code != 400 || b.calls != 0 {
			t.Fatalf("query=%s status=%d calls=%d", query, w.Code, b.calls)
		}
	}
}

func TestCloudSearchExtraFilterContract(t *testing.T) {
	b := &filteredSearchBackend{fakeBackend: &fakeBackend{}}
	w := httptest.NewRecorder()
	New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&extensions=pdf,mp4&modified_from=100&modified_before=200&identity_format=string", nil))
	var result struct {
		Extensions string
		From       string `json:"modified_from"`
		Before     string `json:"modified_before"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || b.calls != 1 || b.filter.Extensions != "pdf,mp4" || b.filter.ModifiedFrom != 100 || b.filter.ModifiedBefore != 200 || result.Extensions != "pdf,mp4" || result.From != "100" || result.Before != "200" {
		t.Fatalf("status=%d calls=%d filter=%+v body=%s", w.Code, b.calls, b.filter, w.Body.String())
	}
}
func TestCloudSearchExtraFiltersRejectBeforeBackend(t *testing.T) {
	for _, query := range []string{"extensions=", "extensions=PDF", "extensions=.pdf", "extensions=pdf,pdf", "extensions=pdf&extensions=mp4", "extensions=pdf&kind=directory", "modified_from=0", "modified_from=-1", "modified_from=01", "modified_from=", "modified_from=100&modified_from=100", "modified_before=200&modified_before=200", "modified_before=253402300801", "modified_from=200&modified_before=100", "modified_from=100&modified_before=100", "modified_before=200&directories_only=1"} {
		b := &filteredSearchBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		New(b, nil).HandleInventorySearch(w, httptest.NewRequest("GET", "/api/cloud-inventory/search?q=lesson&"+query, nil))
		if w.Code != 400 || b.calls != 0 {
			t.Fatalf("query=%s status=%d calls=%d", query, w.Code, b.calls)
		}
	}
}
