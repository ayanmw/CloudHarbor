package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandlePageOAuth(response, httptest.NewRequest("GET", "/oauth", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `/app/assets/`) || strings.Contains(response.Body.String(), `onclick="getDeviceCode()"`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("OAuth page did not mount bundled workspace: %d", response.Code)
	}
}
