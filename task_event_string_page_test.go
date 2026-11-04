package main

import (
	"encoding/json"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestTaskEventStringPagePreservesInt64Identity(t *testing.T) {
	events := []taskstore.Event{{Sequence: 9223372036854775806, JobID: 9007199254740993, Kind: "test"}}
	body, err := json.Marshal(taskEventStringPage(events, 1, 9223372036854775806, true, nil))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Events []struct {
			Sequence string `json:"sequence"`
			JobID    string `json:"job_id"`
		} `json:"events"`
		Next *string `json:"next_before_sequence"`
	}
	if err = json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Sequence != "9223372036854775806" || page.Events[0].JobID != "9007199254740993" || page.Next == nil || *page.Next != "9223372036854775806" {
		t.Fatalf("precision lost: %s", body)
	}
	body, err = json.Marshal(taskEventStringPage(nil, 0, 0, false, nil))
	if err != nil || !strings.Contains(string(body), `"next_before_sequence":null`) {
		t.Fatalf("terminal cursor: %s %v", body, err)
	}
}
