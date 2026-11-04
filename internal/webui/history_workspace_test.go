package webui

import (
	"net/http/httptest"
	"testing"
)

func TestCloudHistoryWorkspaceRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(w, httptest.NewRequest("GET", "/cloud/history", nil))
	if w.Code != 200 {
		t.Fatalf("history route=%d", w.Code)
	}
}
