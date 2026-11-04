package webui

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestCloudPageFormsDoNotSwallowLaterBrowseForm(t *testing.T) {
	markup := strings.SplitN(string(indexCloud), "<script", 2)[0]
	tags := regexp.MustCompile(`(?i)</?form\b[^>]*>`).FindAllString(markup, -1)
	depth := 0
	for _, tag := range tags {
		if strings.HasPrefix(tag, "</") {
			depth--
		} else {
			depth++
		}
		if depth < 0 || depth > 1 {
			t.Fatalf("invalid form nesting at %s depth=%d", tag, depth)
		}
	}
	if depth != 0 {
		t.Fatal("unclosed form consumes the directory browser")
	}
}

func TestWorkspaceServesEmbeddedBundleAndLegacyShell(t *testing.T) {
	s := NewServer(nil)
	w := httptest.NewRecorder()
	s.HandleWorkspace(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/app/assets/") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("workspace response %d %s", w.Code, w.Body.String())
	}
	// Operations now has its own Vue entry regression; cloud still uses this legacy shell.
	for _, render := range []func(*httptest.ResponseRecorder){func(w *httptest.ResponseRecorder) { s.HandlePageCloud(w, httptest.NewRequest("GET", "/cloud", nil)) }} {
		w = httptest.NewRecorder()
		render(w)
		if !strings.Contains(w.Body.String(), "workspace-legacy.css") || !strings.Contains(w.Body.String(), "workspace-legacy.js") {
			t.Fatal("legacy shell missing")
		}
	}
	w = httptest.NewRecorder()
	s.HandlePageLogin(w, httptest.NewRequest("GET", "/login", nil))
	if !strings.Contains(w.Body.String(), `class="workspace-legacy workspace-login"`) || strings.Contains(w.Body.String(), `/res/workspace-legacy`) {
		t.Fatal("public login must inline its theme")
	}
	w = httptest.NewRecorder()
	s.HandleWorkspace(w, httptest.NewRequest("GET", "/not-a-page", nil))
	if w.Code != 404 {
		t.Fatal("unknown route served workspace")
	}
}

func TestCloudWorkspaceModulesAreEmbeddedAndServed(t *testing.T) {
	s := NewServer(nil)
	page := httptest.NewRecorder()
	s.HandlePageCloud(page, httptest.NewRequest("GET", "/cloud", nil))
	for _, module := range []struct{ file, symbol string }{
		{"cloud_browse_route.js", "CloudBrowseRoute"}, {"cloud_properties.js", "CloudProperties"},
		{"cloud_actions.js", "CloudActions"}, {"cloud_progress.js", "CloudProgress"}, {"cloud_scan_limits.js", "CloudScanLimits"},
	} {
		if !strings.Contains(page.Body.String(), `src="/res/`+module.file+`"`) {
			t.Fatalf("cloud page omitted %s", module.file)
		}
		response := httptest.NewRecorder()
		s.HandleRes(response, httptest.NewRequest("GET", "/res/"+module.file, nil))
		if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "javascript") || !strings.Contains(response.Body.String(), "globalThis."+module.symbol) {
			t.Fatalf("embedded module %s: HTTP %d MIME %s", module.file, response.Code, response.Header().Get("Content-Type"))
		}
	}
}
