package webui

import (
	"net/http/httptest"
	"testing"
)

func TestCloudCleanupWorkspaceRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(w, httptest.NewRequest("GET", "/cloud/cleanup", nil))
	if w.Code != 200 {
		t.Fatalf("cleanup route=%d", w.Code)
	}
}
