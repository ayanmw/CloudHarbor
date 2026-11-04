package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPathSettingsPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(response, httptest.NewRequest("GET", "/settings/paths", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("path settings page did not mount bundled workspace: %d", response.Code)
	}
}
