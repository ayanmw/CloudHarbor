package intakeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryAPIExplicitRequestLimitMode(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, nil)
	for _, scoped := range []bool{false, true} {
		handler := server.HandleInventoryScan
		endpoint := "/api/cloud-inventory/scan"
		if scoped {
			handler = server.HandleInventoryRefresh
			endpoint = "/api/cloud-inventory/refresh"
		}
		body := `{"request_limit_mode":"unlimited","request_budget":0}`
		if scoped {
			body = `{"directory":"/target","request_limit_mode":"unlimited","request_budget":0}`
		}
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
		if w.Code != http.StatusAccepted {
			t.Fatalf("mode response=%d %s", w.Code, w.Body.String())
		}
		if scoped && backend.refreshLimitMode != "unlimited" || !scoped && backend.inventoryLimitMode != "unlimited" {
			t.Fatal("mode was not forwarded")
		}
		for _, bad := range []string{`{"request_limit_mode":"unlimited","request_budget":1}`, `{"request_limit_mode":"limited","request_budget":0}`, `{"request_limit_mode":"other"}`} {
			w = httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(bad)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("accepted %s: %d", bad, w.Code)
			}
		}
	}
}
