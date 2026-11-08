package baiduapi

import (
	"context"
	"strings"
	"testing"
)

func TestCloudCacheNamespaceDoesNotChangeCredentialOrApplicationIdentity(t *testing.T) {
	original := NewClient("credential", "application")
	namespace := strings.Repeat("a", 64)
	copy := original.WithCloudCacheNamespace(namespace).WithContext(context.Background()).WithRequestPacer(nil)
	if copy.CloudCacheNamespace() != namespace || copy.Fingerprint() != original.Fingerprint() || copy.ApplicationFingerprint() != original.ApplicationFingerprint() {
		t.Fatal("cache scope changed authentication identity or was lost by client copy")
	}
	if original.CloudCacheNamespace() != original.Fingerprint() {
		t.Fatal("copy mutated original legacy scope")
	}
}
