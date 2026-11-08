package foldersync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func analysisFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	m, err := Open(filepath.Join(t.TempDir(), "db"), &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	run := newID()
	config := Config{ID: newID(), Name: "snapshot", LocalRoot: "D:/left", RemoteRoot: "/right", Account: "bound-app", Mode: "both"}
	if err = m.event(run, "run_started", config); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Local: []Entry{{Path: "a", ID: "local", Size: 3, SHA256: strings.Repeat("a", 64)}}, Remote: []Entry{{Path: "a", ID: "remote", Size: 3, SHA256: strings.Repeat("b", 64)}}}
	if err = m.event(run, "snapshot_before", snapshot); err != nil {
		t.Fatal(err)
	}
	return m, run
}

func TestAnalysisSurvivesReopenAndExportsEveryPage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	m, err := Open(dir, &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := newID()
	if err = m.event(run, "run_started", Config{ID: newID(), Account: "account-app", Mode: "both", LocalRoot: "D:/left", RemoteRoot: "/right"}); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Local: []Entry{}, Remote: []Entry{}}
	fileCount := 0
	for group := 0; group < 15; group++ {
		count := 10
		if group == 0 {
			count = 25
		}
		for copy := 0; copy < count; copy++ {
			snapshot.Local = append(snapshot.Local, Entry{ID: fmt.Sprintf("%d-%d", group, copy), Path: fmt.Sprintf("g-%02d/f-%02d", group, copy), Size: 3, SHA256: fmt.Sprintf("%064x", group+1)})
			fileCount++
		}
	}
	if err = m.event(run, "snapshot_before", snapshot); err != nil {
		t.Fatal(err)
	}
	header, err := m.AnalyzeRun(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	page, err := m.AnalysisPage(header.ID, "", 0)
	if err != nil || page.Analysis.ID != header.ID || len(page.Rows) != 100 || !page.More {
		t.Fatalf("reopen page %+v %v", page, err)
	}
	request := httptest.NewRequest("GET", "/api/directory-analyses/"+header.ID+"?export=1", nil)
	request.SetPathValue("report", header.ID)
	response := httptest.NewRecorder()
	m.HandleAnalysis(response, request)
	if response.Code != 200 {
		t.Fatalf("export %d %s", response.Code, response.Body.String())
	}
	var exported struct {
		Rows   []json.RawMessage `json:"rows"`
		Groups []AnalysisGroup   `json:"groups"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Rows) != fileCount+15 || len(exported.Groups) != 15 {
		t.Fatalf("incomplete export: rows=%d groups=%d", len(exported.Rows), len(exported.Groups))
	}
	members := 0
	for _, g := range exported.Groups {
		members += len(g.Members)
		if g.MembersMore {
			t.Fatal("export retained a member page flag")
		}
	}
	if members != fileCount || len(exported.Groups[0].Members) != 25 {
		t.Fatalf("incomplete group members: %d", members)
	}
}
func TestAnalysisImmutableSnapshotRollback(t *testing.T) {
	m, run := analysisFixture(t)
	before, err := m.Events(run)
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.AnalyzeRun(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.AnalyzeRun(context.Background(), run)
	if err != nil || a.ID == b.ID {
		t.Fatal("new report did not get own identity")
	}
	page, err := m.AnalysisPage(a.ID, "content_conflict", 0)
	if err != nil || len(page.Rows) != 1 || page.Analysis.SourceScope != "bound-app" || page.Matched != "1" {
		t.Fatalf("page %+v %v", page, err)
	}
	after, err := m.Events(run)
	if err != nil || encoded(before) != encoded(after) {
		t.Fatal("analysis rewrote synchronization evidence")
	}
	if _, err = m.db.Exec(`UPDATE directory_analysis_rows SET kind='same' WHERE report_id=?`, a.ID); err == nil {
		t.Fatal("report details were mutable")
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_report BEFORE INSERT ON directory_analysis_rows BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.AnalyzeRun(context.Background(), run); err == nil {
		t.Fatal("partial report was accepted")
	}
	var n int
	if err = m.db.QueryRow(`SELECT count(*) FROM directory_analysis_reports`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("partial report header persisted: %d %v", n, err)
	}
}
func TestAnalysisDoesNotInterpretMissingSnapshotAsEmpty(t *testing.T) {
	m, _ := analysisFixture(t)
	for _, body := range []string{`{}`, `{"local":null,"remote":[]}`, `{"local":[],"remote":null}`} {
		run := newID()
		if err := m.event(run, "run_started", Config{}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.db.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run, "snapshot_before", time.Now().UTC().Format(time.RFC3339Nano), body); err != nil {
			t.Fatal(err)
		}
		if _, err := m.AnalyzeRun(context.Background(), run); err == nil {
			t.Fatalf("missing evidence became empty comparison: %s", body)
		}
	}
}
func TestAnalysisLargeDuplicateGroupsArePaginatedExactly(t *testing.T) {
	m, run := analysisFixture(t)
	snapshot := Snapshot{Local: []Entry{}, Remote: []Entry{}}
	for i := 0; i < 125; i++ {
		snapshot.Local = append(snapshot.Local, Entry{Path: fmt.Sprintf("copy-%03d", i), ID: fmt.Sprint(i), Size: 9007199254740993, SHA256: strings.Repeat("a", 64)})
	}
	other := newID()
	if err := m.event(other, "run_started", Config{ID: run, Account: "bound-app", Mode: "both", LocalRoot: "D:/left", RemoteRoot: "/right"}); err != nil {
		t.Fatal(err)
	}
	if err := m.event(other, "snapshot_before", snapshot); err != nil {
		t.Fatal(err)
	}
	a, err := m.AnalyzeRun(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	page, err := m.AnalysisPage(a.ID, "only_left", 0)
	if err != nil || len(page.Rows) != 100 || !page.More || page.Rows[0].Left.Size != "9007199254740993" {
		t.Fatalf("page %+v %v", page, err)
	}
	groups, more, err := m.AnalysisGroups(a.ID, 0)
	if err != nil || more || len(groups) != 1 || len(groups[0].Members) != 20 || !groups[0].MembersMore {
		t.Fatalf("groups %+v %v", groups, err)
	}
	raw, err := json.Marshal(page)
	if err != nil || !strings.Contains(string(raw), `"size":"9007199254740993"`) {
		t.Fatal("size lost precision")
	}
}
