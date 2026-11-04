package webui

import (
	"net/http/httptest"
	"testing"
)

func TestCloudFilesWorkspaceRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(w, httptest.NewRequest("GET", "/cloud/files", nil))
	if w.Code != 200 {
		t.Fatalf("cloud files status=%d", w.Code)
	}
}
