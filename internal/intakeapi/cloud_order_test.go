package intakeapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type orderedBackend struct {
	*fakeBackend
	orders []baiduapi.DirectoryOrder
}

func (b *orderedBackend) ListCloudOrdered(ctx context.Context, dir string, offset, limit int, order baiduapi.DirectoryOrder) (downloader.CloudListing, error) {
	b.orders = append(b.orders, order)
	result, err := b.fakeBackend.ListCloud(ctx, dir, offset, limit)
	result.Order = &order
	return result, err
}

func TestCloudOrderRejectsInvalidQueryBeforeBackend(t *testing.T) {
	for _, q := range []string{"order=type", "order=", "order=name&order=size", "desc=2", "desc=", "desc=0&desc=1"} {
		backend := &orderedBackend{fakeBackend: &fakeBackend{}}
		w := httptest.NewRecorder()
		New(backend, func() {}).HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?"+q, nil))
		if w.Code != 400 || len(backend.orders) != 0 || backend.cloudCalls != 0 {
			t.Fatalf("query=%s status=%d calls=%d", q, w.Code, backend.cloudCalls)
		}
	}
}

func TestCloudOrderReanchorKeepsSortAndRejectsDifferentSortBoundary(t *testing.T) {
	entry := downloader.CloudEntry{FsID: 5, Path: "/docs/a", Name: "a", ProviderSize: 10, Mtime: 20}
	order := baiduapi.DirectoryOrder{Field: "size", Descending: true}
	identity := downloader.CloudPageBoundary{FsID: entry.FsID, Path: entry.Path, Name: entry.Name, Size: entry.ProviderSize, ProviderMtime: entry.Mtime}
	for _, sameOrder := range []bool{false, true} {
		backend := &orderedBackend{fakeBackend: &fakeBackend{cloudListings: map[int]downloader.CloudListing{100: {Directory: "/docs", Offset: 100, Entries: []downloader.CloudEntry{entry}}, 0: {Directory: "/docs", Offset: 0, Entries: []downloader.CloudEntry{entry}}}}}
		fingerprint := identity.ComputeFingerprint()
		if sameOrder {
			fingerprint = identity.FingerprintForListing("/docs", order)
		}
		raw, _ := json.Marshal(map[string]string{"fingerprint": fingerprint})
		w := httptest.NewRecorder()
		New(backend, func() {}).HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?dir=%2Fdocs&offset=100&order=size&desc=1&expected_boundary="+url.QueryEscape(string(raw)), nil))
		var result downloader.CloudListing
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		wantCalls := 2
		if sameOrder {
			wantCalls = 1
		}
		if len(backend.orders) != wantCalls || result.BoundaryChanged == sameOrder || result.Order == nil || *result.Order != order {
			t.Fatalf("same=%v orders=%v result=%+v", sameOrder, backend.orders, result)
		}
		for _, got := range backend.orders {
			if got != order {
				t.Fatalf("sort lost on reanchor: %v", got)
			}
		}
	}
}
