package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandlePageTaskEvents(response, httptest.NewRequest("GET", "/task-events", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || strings.Contains(response.Body.String(), `/res/task_event_history.js`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("event page did not switch to bundled workspace: %d %s", response.Code, response.Body.String())
	}
}
