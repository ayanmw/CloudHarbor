package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandlePageMonitorHistory(response, httptest.NewRequest("GET", "/monitor-history", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || strings.Contains(response.Body.String(), `/res/monitor_history.js`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("monitor page did not mount bundled workspace: %d", response.Code)
	}
}
