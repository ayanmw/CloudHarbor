package foldersync

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/directoryanalysis"
)

type cachedAnalysisFixture struct{ fixtureRemote }

func (*cachedAnalysisFixture) CachedAnalysis(ctx context.Context, roots []string) (CachedAnalysisSource, error) {
	return CachedAnalysisSource{Scope: "account-app", Generation: "7", ReadAt: time.Now().UTC(), Inputs: []directoryanalysis.Input{{Label: "网盘缓存", Root: roots[0], Complete: true}, {Label: "网盘缓存", Root: roots[1], Complete: true}}}, nil
}
func TestAnalyzeCachedDirectoriesFreezesSourceWithoutSyncRun(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "state"), &cachedAnalysisFixture{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	header, err := m.AnalyzeCachedDirectories(context.Background(), "/a", "/b")
	if err != nil {
		t.Fatal(err)
	}
	if header.SourceKind != "cloud_cache" || header.SourceRun != "" || header.CacheGeneration != "7" || header.SourceScope != "account-app" || header.Summary.Relation != "identical_tree" {
		t.Fatalf("header %+v", header)
	}
	page, err := m.AnalysisPage(header.ID, "", 0)
	if err != nil || page.Analysis.ID != header.ID {
		t.Fatalf("read %+v %v", page, err)
	}
	request := httptest.NewRequest("POST", "/api/directory-analyses", strings.NewReader(`{"left_root":"/a","right_root":"/b"}`))
	response := httptest.NewRecorder()
	m.HandleAnalysis(response, request)
	var created Analysis
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.SourceKind != "cloud_cache" {
		t.Fatalf("create status %d", response.Code)
	}
	request = httptest.NewRequest("GET", "/api/directory-analyses/"+created.ID+"?export=1", nil)
	request.SetPathValue("report", created.ID)
	response = httptest.NewRecorder()
	m.HandleAnalysis(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"cache_generation":"7"`) {
		t.Fatalf("export status %d", response.Code)
	}
	for _, body := range []string{`{"left_root":"/a","right_root":"/b","scan":true}`, `{"left_root":"/a","right_root":"/b"}{}`, `null`} {
		request = httptest.NewRequest("POST", "/api/directory-analyses", strings.NewReader(body))
		response = httptest.NewRecorder()
		m.HandleAnalysis(response, request)
		if response.Code == 200 {
			t.Fatalf("accepted invalid body %s", body)
		}
	}
	for _, right := range []string{"/a", "/a/sub", "/"} {
		if _, err = m.AnalyzeCachedDirectories(context.Background(), "/a", right); err == nil {
			t.Fatalf("accepted overlapping %q", right)
		}
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_runs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("created sync run %d %v", count, err)
	}
}
