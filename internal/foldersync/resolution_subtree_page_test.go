package foldersync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubtreeHTTPBoundsNormalReadsAndPreservesFullExport(t *testing.T) {
	m, err := Open(t.TempDir(), &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	run := newID()
	c := Config{ID: newID(), Account: "account", Mode: "both", LocalRoot: "D:/fixture", RemoteRoot: "/fixture"}
	l := Entry{Path: "folder", ID: "local", Directory: true}
	r := Entry{Path: "folder", ID: "remote", Directory: true}
	snapshot := Snapshot{Local: []Entry{l}, Remote: []Entry{r}}
	for i := 0; i < 125; i++ {
		p := fmt.Sprintf("folder/%03d", i)
		snapshot.Local = append(snapshot.Local, Entry{Path: p, ID: fmt.Sprint("l", i), Size: 7, SHA256: strings.Repeat("a", 64)})
		snapshot.Remote = append(snapshot.Remote, Entry{Path: p, ID: fmt.Sprint("r", i), Size: 9, SHA256: strings.Repeat("b", 64)})
	}
	if err = m.event(run, "run_started", c); err != nil {
		t.Fatal(err)
	}
	if err = m.event(run, "snapshot_before", snapshot); err != nil {
		t.Fatal(err)
	}
	if err = m.saveStep(run, StepRecord{Ordinal: 1, Status: "conflict", Step: Step{Action: "conflict", Path: "folder", Local: &l, Remote: &r}}); err != nil {
		t.Fatal(err)
	}
	full, err := m.CreateResolution(context.Background(), run, 1, "keep_remote")
	if err != nil {
		t.Fatal(err)
	}
	var original string
	if err = m.db.QueryRow(`SELECT body FROM sync_resolutions WHERE id=?`, full.ID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	header, err := m.ResolutionSummary(context.Background(), full.ID)
	if err != nil || !header.SubtreeReview.Paged || header.SubtreeReview.ItemCount != "126" || len(header.SubtreeReview.Items) != 0 {
		t.Fatalf("summary %+v %v", header, err)
	}
	if header.SubtreeReview.PlanVersion != "subtree-steps-v1" || header.SubtreeReview.PlanStepCount != "250" {
		t.Fatal("ordered plan metadata missing")
	}
	steps, err := m.SubtreeStepsPage(context.Background(), full.ID, 200)
	if err != nil || steps.Total != "250" || len(steps.Steps) != 50 || steps.More || steps.Steps[0].Action != "download" {
		t.Fatalf("step page %+v %v", steps, err)
	}
	history, _, err := m.Resolutions(context.Background(), run, 1, 0)
	if err != nil || len(history) != 1 || len(history[0].SubtreeReview.Items) != 0 {
		t.Fatal("history expanded the subtree")
	}
	page, err := m.ResolutionSubtreePage(context.Background(), full.ID, 100)
	if err != nil || page.Total != "126" || len(page.Items) != 26 || page.More || page.Items[0].Path != "folder/099" {
		t.Fatalf("page %+v %v", page, err)
	}
	for _, q := range []string{"offset=-1", "offset=no", "offset=0&offset=100"} {
		request := httptest.NewRequest("GET", "/subtree?"+q, nil)
		request.SetPathValue("decision", full.ID)
		response := httptest.NewRecorder()
		m.HandleResolutionSubtree(response, request)
		if response.Code != 400 {
			t.Fatalf("invalid page accepted: %s", q)
		}
	}
	end, err := m.ResolutionSubtreePage(context.Background(), full.ID, 200)
	if err != nil || len(end.Items) != 0 || end.More || end.Total != "126" {
		t.Fatalf("end page %+v %v", end, err)
	}
	request := httptest.NewRequest("GET", "/resolution?export=1", nil)
	request.SetPathValue("decision", full.ID)
	response := httptest.NewRecorder()
	m.HandleResolution(response, request)
	var exported Resolution
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &exported) != nil || len(exported.SubtreeReview.Items) != 126 {
		t.Fatal("export was truncated")
	}
	planRequest := httptest.NewRequest("GET", "/subtree-plan?export=1", nil)
	planRequest.SetPathValue("decision", full.ID)
	planResponse := httptest.NewRecorder()
	m.HandleSubtreeSteps(planResponse, planRequest)
	var planExport struct {
		Count string `json:"step_count"`
		Steps []Step `json:"steps"`
	}
	if planResponse.Code != 200 || json.Unmarshal(planResponse.Body.Bytes(), &planExport) != nil || planExport.Count != "250" || len(planExport.Steps) != 250 {
		t.Fatal("complete ordered plan export was truncated")
	}
	if _, err = m.db.Exec(`UPDATE sync_resolution_subtree_steps SET body='{}' WHERE decision_id=?`, full.ID); err == nil {
		t.Fatal("mutable ordered step")
	}
	if _, err = m.db.Exec(`DELETE FROM sync_resolution_subtree_steps WHERE decision_id=?`, full.ID); err == nil {
		t.Fatal("deletable ordered step")
	}
	var after string
	if err = m.db.QueryRow(`SELECT body FROM sync_resolutions WHERE id=?`, full.ID).Scan(&after); err != nil || after != original {
		t.Fatal("summary rewrote immutable evidence")
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_subtree_plan BEFORE INSERT ON sync_resolution_subtree_steps BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CreateResolution(context.Background(), run, 1, "keep_remote"); err == nil {
		t.Fatal("injected plan persistence failure accepted")
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_resolutions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("partial decision survived failed step write")
	}
}
