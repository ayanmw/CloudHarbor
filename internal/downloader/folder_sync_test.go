package downloader

import (
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestFolderSyncScopeIncludesApplicationWithStableCacheNamespace(t *testing.T) {
	a := baiduapi.NewClient("token", "app-a").WithCloudCacheNamespace("stable-cache")
	b := baiduapi.NewClient("token", "app-b").WithCloudCacheNamespace("stable-cache")
	if syncAccountScope(a) == syncAccountScope(b) {
		t.Fatal("application change retained synchronization scope")
	}
	renewed := baiduapi.NewClient("renewed-token", "app-a").WithCloudCacheNamespace("stable-cache")
	if syncAccountScope(a) != syncAccountScope(renewed) {
		t.Fatal("same-account renewal unnecessarily invalidated synchronization scope")
	}
}
