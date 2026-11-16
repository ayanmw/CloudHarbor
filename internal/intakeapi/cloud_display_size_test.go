package intakeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type displaySizeBackend struct {
	*fakeBackend
	calls int
	err   error
	desc  bool
	token string
}

func (b *displaySizeBackend) ListCloudDisplaySize(_ context.Context, dir string, offset, limit int, desc bool, token string) (downloader.CloudListing, error) {
	b.calls++
	b.desc = desc
	b.token = token
	total := 1
	return downloader.CloudListing{Directory: dir, Offset: offset, Total: &total, Order: &baiduapi.DirectoryOrder{Field: "size", Descending: desc}, DisplaySize: &downloader.CloudDisplaySizeInfo{SnapshotID: strings.Repeat("a", 32), CacheGeneration: 9007199254740993}, Entries: []downloader.CloudEntry{{ID: "9007199254740993", FsID: 9007199254740993, Path: "/docs/a", Name: "a", Size: 9007199254740993, ProviderSize: 9007199254740993}}}, b.err
}
func TestDisplaySizeHTTPUsesExactIdentityAndRejectsInvalidRequests(t *testing.T) {
	b := &displaySizeBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	w := httptest.NewRecorder()
	s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?dir=%2Fdocs&desc=1&identity_format=string", nil))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || !b.desc || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"fs_id":"9007199254740993"`) || !strings.Contains(string(body["display_size"]), `"cache_generation":"9007199254740993"`) {
		t.Fatalf("response %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"order=type", "desc=2", "desc=0&desc=1", "snapshot=bad", "snapshot=" + strings.Repeat("A", 32), "limit=101", "unknown=1", "identity_format=number", "offset=1&expected_boundary=invalid"} {
		before := b.calls
		w = httptest.NewRecorder()
		s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?"+query, nil))
		if w.Code != 400 || b.calls != before {
			t.Fatalf("invalid %s: %d calls=%d", query, w.Code, b.calls)
		}
	}
	before := b.calls
	w = httptest.NewRecorder()
	s.HandleCloudDisplaySize(w, httptest.NewRequest("POST", "/api/cloud-display-size", nil))
	if w.Code != 405 || b.calls != before {
		t.Fatal("non-read request reached backend")
	}
	b.err = downloader.ErrCloudDisplaySizeSnapshotChanged
	w = httptest.NewRecorder()
	s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?snapshot="+strings.Repeat("a", 32), nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "display_size_snapshot_changed") {
		t.Fatalf("missing snapshot conflict %d %s", w.Code, w.Body.String())
	}
	b.err = errors.New("access_token=private")
	w = httptest.NewRecorder()
	s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size", nil))
	if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("private error exposed")
	}
}

func TestDisplaySizeHTTPBindsContinuationToSnapshotAndRawIdentityBoundary(t *testing.T) {
	b := &displaySizeBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	entry := downloader.CloudPageBoundary{FsID: 9007199254740993, Path: "/docs/a", Name: "a", Size: 9007199254740993}
	for _, matching := range []bool{true, false} {
		fingerprint := entry.FingerprintForListing("/docs", baiduapi.DirectoryOrder{Field: "size"})
		if !matching {
			fingerprint = strings.Repeat("0", 64)
		}
		raw, _ := json.Marshal(map[string]string{"fingerprint": fingerprint})
		q := url.Values{"dir": {"/docs"}, "offset": {"1"}, "snapshot": {strings.Repeat("a", 32)}, "expected_boundary": {string(raw)}}
		w := httptest.NewRecorder()
		s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?"+q.Encode(), nil))
		want := 200
		if !matching {
			want = 409
		}
		if w.Code != want || b.token != strings.Repeat("a", 32) {
			t.Fatalf("boundary match=%v response=%d %s", matching, w.Code, w.Body.String())
		}
	}
	before := b.calls
	w := httptest.NewRecorder()
	s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?offset=1", nil))
	if w.Code != 409 || b.calls != before {
		t.Fatal("continuation without snapshot reached backend")
	}
}

func (b *displaySizeBackend) ListCloudDisplayOrdered(ctx context.Context, dir string, offset, limit int, order baiduapi.DirectoryOrder, mixed bool, token string) (downloader.CloudListing, error) {
	result, err := b.ListCloudDisplaySize(ctx, dir, offset, limit, order.Descending, token)
	result.Order = &order
	result.DisplaySize.Mixed = mixed
	return result, err
}
func TestDisplaySizeHTTPAcceptsAllOrdersAndExplicitMixedMode(t *testing.T) {
	b := &displaySizeBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	for _, field := range []string{"name", "time", "size"} {
		for _, mixed := range []string{"0", "1"} {
			w := httptest.NewRecorder()
			s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?order="+field+"&mixed="+mixed, nil))
			var result struct {
				Order baiduapi.DirectoryOrder         `json:"order"`
				Info  downloader.CloudDisplaySizeInfo `json:"display_size"`
			}
			json.Unmarshal(w.Body.Bytes(), &result)
			if w.Code != 200 || result.Order.Field != field || result.Info.Mixed != (mixed == "1") {
				t.Fatalf("%s/%s: %d %s", field, mixed, w.Code, w.Body.String())
			}
		}
	}
	for _, query := range []string{"mixed=", "mixed=true", "mixed=0&mixed=1"} {
		before := b.calls
		w := httptest.NewRecorder()
		s.HandleCloudDisplaySize(w, httptest.NewRequest("GET", "/api/cloud-display-size?"+query, nil))
		if w.Code != 400 || b.calls != before {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
}
