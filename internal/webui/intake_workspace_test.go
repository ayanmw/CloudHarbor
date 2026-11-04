package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIntakePageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandlePageIntake(response, httptest.NewRequest("GET", "/intake", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || strings.Contains(response.Body.String(), `/res/intake.js`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("intake page did not mount bundled workspace: %d", response.Code)
	}
}
