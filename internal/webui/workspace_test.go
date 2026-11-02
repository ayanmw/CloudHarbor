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
	for _, render := range []func(*httptest.ResponseRecorder){func(w *httptest.ResponseRecorder) { s.HandlePageCloud(w, httptest.NewRequest("GET", "/cloud", nil)) }, func(w *httptest.ResponseRecorder) {
		s.HandlePageOperations(w, httptest.NewRequest("GET", "/operations", nil))
	}} {
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
