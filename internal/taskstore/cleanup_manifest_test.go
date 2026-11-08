package taskstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func cleanupManifestFixture() CleanupBatchManifest {
	keep := CleanupIdentity{FsID: 9007199254740995, Path: "/保留.bin", Name: "保留.bin", Size: math.MaxInt64, Mtime: 123, MD5: strings.Repeat("a", 32)}
	return CleanupBatchManifest{ID: strings.Repeat("a", 32), AccountFingerprint: strings.Repeat("b", 64), ApplicationFingerprint: strings.Repeat("c", 64), CacheNamespace: strings.Repeat("d", 64), CreatedAtUTC: time.Date(2026, 11, 6, 0, 0, 0, 123456789, time.UTC), Items: []CleanupBatchItem{{Kind: "duplicate", Expected: CleanupIdentity{FsID: 9007199254740993, Path: "/待删.bin", Name: "待删.bin", Size: math.MaxInt64, Mtime: 123}, Retained: &keep, ExpectedSHA256: strings.Repeat("e", 64), PreviewObservedAtUTC: time.Date(2026, 11, 5, 23, 59, 0, 987654321, time.UTC)}}}
}

func TestCleanupManifestEncodesExactIdentitiesAndFreezesPayload(t *testing.T) {
	m := cleanupManifestFixture()
	body, digest, err := encodeCleanupBatchManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"fs_id":"9007199254740993"`)) || !bytes.Contains(body, []byte(`"size":"9223372036854775807"`)) {
		t.Fatalf("identity precision lost: %s", body)
	}
	if bytes.Contains(body, []byte("created_at_utc")) {
		t.Fatal("local receive clock must be persisted separately from idempotent content")
	}
	sum := sha256.Sum256(body)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("payload digest mismatch")
	}
	copy := append([]byte(nil), body...)
	m.Items[0].Expected.Path = "/changed"
	m.Items[0].Retained.Path = "/changed-keep"
	if !bytes.Equal(body, copy) {
		t.Fatal("caller changed frozen payload")
	}
	var decoded struct {
		Items []CleanupBatchItem `json:"items"`
	}
	if err = json.Unmarshal(body, &decoded); err != nil || len(decoded.Items) != 1 || decoded.Items[0].Expected.FsID != 9007199254740993 || decoded.Items[0].Retained.Size != math.MaxInt64 {
		t.Fatalf("payload roundtrip: %+v err=%v", decoded, err)
	}
}

func TestCleanupManifestReceiveClockDoesNotChangeContentButPreviewClockDoes(t *testing.T) {
	one := cleanupManifestFixture()
	body, digest, err := encodeCleanupBatchManifest(one)
	if err != nil {
		t.Fatal(err)
	}
	two := cleanupManifestFixture()
	two.CreatedAtUTC = two.CreatedAtUTC.Add(time.Hour)
	two.Items[0].PreviewObservedAtUTC = two.Items[0].PreviewObservedAtUTC.In(time.FixedZone("offset", 8*3600))
	body2, digest2, err := encodeCleanupBatchManifest(two)
	if err != nil || digest != digest2 || !bytes.Equal(body, body2) {
		t.Fatalf("receive/zone changed idempotency err=%v", err)
	}
	if two.Items[0].PreviewObservedAtUTC.Location() == time.UTC {
		t.Fatal("encoder mutated caller's preview timestamp")
	}
	two.Items[0].PreviewObservedAtUTC = two.Items[0].PreviewObservedAtUTC.Add(time.Nanosecond)
	_, digest2, err = encodeCleanupBatchManifest(two)
	if err != nil || digest == digest2 {
		t.Fatalf("preview evidence was discarded err=%v", err)
	}
}

func TestCleanupManifestRejectsMalformedDeletionContracts(t *testing.T) {
	cases := map[string]func(*CleanupBatchManifest){
		"batch_id":         func(m *CleanupBatchManifest) { m.ID = "invalid" },
		"account":          func(m *CleanupBatchManifest) { m.AccountFingerprint = "" },
		"application":      func(m *CleanupBatchManifest) { m.ApplicationFingerprint = "invalid" },
		"namespace":        func(m *CleanupBatchManifest) { m.CacheNamespace = "" },
		"receive_time":     func(m *CleanupBatchManifest) { m.CreatedAtUTC = time.Time{} },
		"preview_time":     func(m *CleanupBatchManifest) { m.Items[0].PreviewObservedAtUTC = time.Time{} },
		"kind":             func(m *CleanupBatchManifest) { m.Items[0].Kind = "other" },
		"root":             func(m *CleanupBatchManifest) { m.Items[0].Expected.Path = "/" },
		"identity":         func(m *CleanupBatchManifest) { m.Items[0].Expected.FsID = 0 },
		"size":             func(m *CleanupBatchManifest) { m.Items[0].Expected.Size = -1 },
		"mtime":            func(m *CleanupBatchManifest) { m.Items[0].Expected.Mtime = -1 },
		"name":             func(m *CleanupBatchManifest) { m.Items[0].Expected.Name = "different" },
		"path_control":     func(m *CleanupBatchManifest) { m.Items[0].Expected.Path = "/bad\n"; m.Items[0].Expected.Name = "bad\n" },
		"parent_segment":   func(m *CleanupBatchManifest) { m.Items[0].Expected.Path = "/a/../待删.bin" },
		"missing_keep":     func(m *CleanupBatchManifest) { m.Items[0].Retained = nil },
		"keep_directory":   func(m *CleanupBatchManifest) { m.Items[0].Retained.Directory = true },
		"source_directory": func(m *CleanupBatchManifest) { m.Items[0].Expected.Directory = true },
		"size_difference":  func(m *CleanupBatchManifest) { m.Items[0].Retained.Size-- },
		"invalid_sha":      func(m *CleanupBatchManifest) { m.Items[0].ExpectedSHA256 = "invalid" },
		"empty_file": func(m *CleanupBatchManifest) {
			m.Items[0].Kind = "empty"
			m.Items[0].Retained = nil
			m.Items[0].ExpectedSHA256 = ""
		},
		"empty_with_keep": func(m *CleanupBatchManifest) { m.Items[0].Kind = "empty"; m.Items[0].Expected.Directory = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := cleanupManifestFixture()
			change(&m)
			if _, _, err := encodeCleanupBatchManifest(m); err == nil {
				t.Fatal("malformed deletion manifest accepted")
			}
		})
	}
}

func TestCleanupManifestRejectsDeletionAndRetainedIdentityConflicts(t *testing.T) {
	for _, kind := range []string{"duplicate_id", "duplicate_path", "keep_deleted_id", "keep_deleted_path", "keep_changed_identity", "keep_changed_digest"} {
		t.Run(kind, func(t *testing.T) {
			m := cleanupManifestFixture()
			other := m.Items[0]
			other.Expected.FsID = 11
			other.Expected.Path = "/other"
			other.Expected.Name = "other"
			keep := *other.Retained
			other.Retained = &keep
			switch kind {
			case "duplicate_id":
				other.Expected.FsID = m.Items[0].Expected.FsID
			case "duplicate_path":
				other.Expected.Path = m.Items[0].Expected.Path
				other.Expected.Name = m.Items[0].Expected.Name
			case "keep_deleted_id":
				other.Expected.FsID = m.Items[0].Retained.FsID
			case "keep_deleted_path":
				other.Expected.Path = m.Items[0].Retained.Path
				other.Expected.Name = m.Items[0].Retained.Name
			case "keep_changed_identity":
				other.Retained.Mtime++
			case "keep_changed_digest":
				other.ExpectedSHA256 = strings.Repeat("f", 64)
			}
			m.Items = append(m.Items, other)
			if _, _, err := encodeCleanupBatchManifest(m); err == nil {
				t.Fatal("conflicting deletion manifest accepted")
			}
		})
	}
}

func TestCleanupManifestSupportsSharedKeepAndEmptyDirectoryButBindsOrder(t *testing.T) {
	m := cleanupManifestFixture()
	other := m.Items[0]
	other.Expected.FsID = 11
	other.Expected.Path = "/other"
	other.Expected.Name = "other"
	m.Items = append(m.Items, other, CleanupBatchItem{Kind: "empty", Expected: CleanupIdentity{FsID: 12, Path: "/empty", Name: "empty", Directory: true}, PreviewObservedAtUTC: m.CreatedAtUTC})
	_, digest, err := encodeCleanupBatchManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	m.Items[0], m.Items[1] = m.Items[1], m.Items[0]
	_, reordered, err := encodeCleanupBatchManifest(m)
	if err != nil || digest == reordered {
		t.Fatalf("ordered manifest not bound err=%v", err)
	}
}

func TestCleanupManifestBoundsItemsAndPayloadWithoutTruncation(t *testing.T) {
	m := cleanupManifestFixture()
	m.Items = nil
	if _, _, err := encodeCleanupBatchManifest(m); err == nil {
		t.Fatal("empty manifest accepted")
	}
	for i := 0; i < CleanupBatchItemLimit; i++ {
		name := fmt.Sprint(i)
		m.Items = append(m.Items, CleanupBatchItem{Kind: "empty", Expected: CleanupIdentity{FsID: int64(i + 1), Path: "/" + name, Name: name, Directory: true}, PreviewObservedAtUTC: m.CreatedAtUTC})
	}
	if _, _, err := encodeCleanupBatchManifest(m); err != nil {
		t.Fatalf("bounded manifest rejected: %v", err)
	}
	m.Items = append(m.Items, m.Items[0])
	if _, _, err := encodeCleanupBatchManifest(m); err == nil {
		t.Fatal("oversized item count accepted")
	}
	m.Items = m.Items[:CleanupBatchItemLimit]
	for i := range m.Items {
		name := strings.Repeat("a", 1100) + fmt.Sprint(i)
		m.Items[i].Expected.Path = "/" + name
		m.Items[i].Expected.Name = name
	}
	if _, _, err := encodeCleanupBatchManifest(m); err == nil {
		t.Fatal("oversized payload accepted")
	}
}
