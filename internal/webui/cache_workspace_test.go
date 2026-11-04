package webui

import (
	"net/http/httptest"
	"testing"
)

func TestCloudCacheWorkspaceRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(w, httptest.NewRequest("GET", "/cloud/cache", nil))
	if w.Code != 200 {
		t.Fatalf("cache route=%d", w.Code)
	}
}
