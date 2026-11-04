package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationHistoryPageServesEmbeddedVueWorkspace(t *testing.T) {
	response := httptest.NewRecorder()
	NewServer(nil).HandleWorkspace(response, httptest.NewRequest("GET", "/settings/migrations", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "/app/assets/") {
		t.Fatalf("migration history status=%d", response.Code)
	}
}
