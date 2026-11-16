package webui

import (
	"net/http/httptest"
	"testing"
)

func TestLocalLibraryWorkspaceRoute(t *testing.T) {
	w := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(w, httptest.NewRequest("GET", "/library", nil))
	if w.Code != 200 {
		t.Fatalf("library workspace=%d", w.Code)
	}
}
