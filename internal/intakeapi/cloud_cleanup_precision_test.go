package intakeapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCleanupPreviewStringIdentityPreservesLargeIDsAndLegacy(t *testing.T) {
	entry := taskstore.CloudInventoryEntry{FsID: 9007199254740993, Path: "/copy", Name: "copy", Size: 9007199254740995, ProviderMtime: 9007199254740997}
	for _, exact := range []bool{false, true} {
		f := &fakeBackend{duplicatePreviewGroups: []taskstore.CloudDuplicateGroup{{SHA256: strings.Repeat("a", 64), Size: entry.Size, Keep: entry, Delete: []taskstore.CloudInventoryEntry{entry}}}, duplicatePreviewTotal: 1, duplicatePreviewDeleteCount: 1}
		route := "/api/cloud-cleanup/duplicates"
		if exact {
			route += "?identity_format=string"
		}
		w := httptest.NewRecorder()
		New(f, nil).HandleDuplicatePreview(w, httptest.NewRequest("GET", route, nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var data struct {
			Groups []struct {
				Size json.RawMessage `json:"size"`
				Keep struct {
					FsID json.RawMessage `json:"fs_id"`
				} `json:"keep"`
			} `json:"groups"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		want := `9007199254740993`
		if exact {
			want = `"9007199254740993"`
		}
		if string(data.Groups[0].Keep.FsID) != want {
			t.Fatalf("exact=%v: %s", exact, w.Body.String())
		}
	}
}
func TestEmptyPreviewStringIdentityAndInvalidFormat(t *testing.T) {
	s := New(&fakeBackend{}, nil)
	for _, format := range []string{"string", "broken"} {
		w := httptest.NewRecorder()
		s.HandleEmptyCloudDirectories(w, httptest.NewRequest("GET", "/api/cloud-cleanup/empty-directories?identity_format="+format, nil))
		if format == "broken" {
			if w.Code != 400 {
				t.Fatalf("invalid format=%d", w.Code)
			}
			continue
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"fs_id":"9"`) {
			t.Fatalf("response %d %s", w.Code, w.Body.String())
		}
	}
}

func TestDuplicateScanStringStatusDoesNotLeakAccountAndUsesExactCounters(t *testing.T) {
	s := New(&fakeBackend{}, nil)
	w := httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan?identity_format=string", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "account_fingerprint") || !strings.Contains(w.Body.String(), `"total_files":"`) || !strings.Contains(w.Body.String(), `"worker_active":false`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan?estimate=1&identity_format=string", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"estimated_bytes":"`) {
		t.Fatalf("estimate %d %s", w.Code, w.Body.String())
	}
}

type emptyPagedBackend struct{ *fakeBackend }

func (f *emptyPagedBackend) PreviewEmptyCloudDirectoriesPage(context.Context, int64, int, *int64) (taskstore.CloudEmptyDirectoryPage, error) {
	return taskstore.CloudEmptyDirectoryPage{Entries: []taskstore.CloudInventoryEntry{{FsID: 9007199254740993, Path: "/empty", Name: "empty", Directory: true}}, Generation: 9007199254740995, Total: 1, Coverage: "complete", RunID: strings.Repeat("a", 32)}, nil
}
func TestEmptyPreviewPagedFormatIsBoundedAndExact(t *testing.T) {
	s := New(&emptyPagedBackend{&fakeBackend{}}, nil)
	w := httptest.NewRecorder()
	s.HandleEmptyCloudDirectories(w, httptest.NewRequest("GET", "/api/cloud-cleanup/empty-directories?identity_format=string&limit=50&offset=0", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cache_generation":"9007199254740995"`) || !strings.Contains(w.Body.String(), `"fs_id":"9007199254740993"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
