package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCachedCloudStringFormatKeepsGenerationAndEntryNumbersExact(t *testing.T) {
	size := int64(9223372036854775807)
	data, err := json.Marshal(cachedCloudEntriesResponse([]taskstore.CloudInventoryEntry{{FsID: 9007199254740993, Size: size, DirectorySizeBytes: &size}}, true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fs_id":"9007199254740993"`) || !strings.Contains(string(data), `"directory_size_bytes":"9223372036854775807"`) {
		t.Fatalf("bad entry: %s", data)
	}
	w := httptest.NewRecorder()
	New(&fakeBackend{}, nil).HandleInventory(w, httptest.NewRequest("GET", "/api/cloud-inventory?identity_format=string", nil))
	var result struct {
		Generation string `json:"cache_generation"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Generation != "7" {
		t.Fatalf("bad generation: %s", w.Body.String())
	}
}

func TestCloudDirectorySizeOptInCountersAreStrings(t *testing.T) {
	w := httptest.NewRecorder()
	New(&fakeBackend{}, nil).HandleInventorySize(w, httptest.NewRequest("GET", "/api/cloud-inventory/size?identity_format=string", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bytes":"0"`) || !strings.Contains(w.Body.String(), `"files":"0"`) {
		t.Fatalf("size response: %s", w.Body.String())
	}
}

func TestCloudListingStringIdentityPreservesExactFileMetadata(t *testing.T) {
	f := &fakeBackend{cloudListings: map[int]downloader.CloudListing{0: {Directory: "/", Entries: []downloader.CloudEntry{{FsID: 9007199254740993, Size: 9223372036854775807, ProviderSize: 9223372036854775807, Mtime: 1}}}}}
	s := New(f, nil)
	for _, format := range []string{"", "string"} {
		w := httptest.NewRecorder()
		s.HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?identity_format="+format, nil))
		want := `"fs_id":9007199254740993`
		if format == "string" {
			want = `"fs_id":"9007199254740993"`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("format=%s: %s", format, w.Body.String())
		}
	}
}

func TestCloudListingRejectsInvalidIdentityFormatBeforeProviderCall(t *testing.T) {
	f := &fakeBackend{}
	s := New(f, nil)
	for _, query := range []string{"identity_format=bad", "identity_format=string&identity_format=string"} {
		w := httptest.NewRecorder()
		s.HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?"+query, nil))
		if w.Code != 400 || f.cloudCalls != 0 {
			t.Fatalf("query=%s status=%d calls=%d", query, w.Code, f.cloudCalls)
		}
	}
}
