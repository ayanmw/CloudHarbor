package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandlePageOperations(response, httptest.NewRequest("GET", "/operations", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || strings.Contains(response.Body.String(), `/res/cloud_operations.js`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("operation page did not mount bundled workspace: %d", response.Code)
	}
}
