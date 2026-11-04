package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestLocalMigrationRejectsInvalidCounterFormatBeforeReadingStore(t *testing.T) {
	for _, query := range []string{"counter_format=number", "counter_format=string&counter_format=string"} {
		response := httptest.NewRecorder()
		handleLocalDirectoryMigration(response, httptest.NewRequest("GET", "/api/settings/local-migration?"+query, nil))
		if response.Code != 400 {
			t.Fatalf("query=%s status=%d", query, response.Code)
		}
	}
}

func TestLocalMigrationCounterFormatPreservesInt64(t *testing.T) {
	m := taskstore.LocalDirectoryMigration{TotalBytes: 9223372036854775807, ProcessedBytes: 9007199254740993, TotalFiles: 5, ProcessedFiles: 2}
	for _, exact := range []bool{false, true} {
		data, err := json.Marshal(localMigrationResponse(m, exact))
		if err != nil {
			t.Fatal(err)
		}
		want := `"processed_bytes":9007199254740993`
		if exact {
			want = `"processed_bytes":"9007199254740993"`
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("format=%v: %s", exact, data)
		}
	}
}
