package intakeapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestIncompleteDirectoryExactResponseKeepsGenerationAndFilters(t *testing.T) {
	b := &fakeBackend{incompleteDirectoryPage: taskstore.CloudInventoryIncompleteDirectoryPage{Entries: []taskstore.CloudInventoryIncompleteDirectory{{Path: "/old", Reason: "stale", Status: "stale", CachedDirectory: true}}, Total: 1, Generation: 9007199254740993}}
	w := httptest.NewRecorder()
	New(b, nil).HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest("GET", "/api/cloud-inventory/incomplete-directories?identity_format=string&cache_generation=9007199254740993&reason=stale&q=old&limit=25", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cache_generation":"9007199254740993"`) || !strings.Contains(w.Body.String(), `"total":"1"`) || !strings.Contains(w.Body.String(), `"source":"local_cache"`) || !strings.Contains(w.Body.String(), `"query":"old"`) || w.Header().Get("Cache-Control") != "no-store" || b.cloudCalls != 0 {
		t.Fatalf("status=%d body=%s provider=%d", w.Code, w.Body.String(), b.cloudCalls)
	}
	w = httptest.NewRecorder()
	New(b, nil).HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest("GET", "/api/cloud-inventory/incomplete-directories?identity_format=string&cache_generation=7", nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"cache_generation":"9007199254740993"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestIncompleteDirectoryExactValidationAndLegacyFormat(t *testing.T) {
	b := &fakeBackend{incompleteDirectoryPage: taskstore.CloudInventoryIncompleteDirectoryPage{Entries: []taskstore.CloudInventoryIncompleteDirectory{}, Generation: 17}}
	for _, query := range []string{"identity_format=bad", "identity_format=string&identity_format=string", "identity_format=string&cache_generation=017"} {
		w := httptest.NewRecorder()
		New(b, nil).HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest("GET", "/api/cloud-inventory/incomplete-directories?"+query, nil))
		if w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	New(b, nil).HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest("GET", "/api/cloud-inventory/incomplete-directories", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cache_generation":17`) || strings.Contains(w.Body.String(), `"source"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
