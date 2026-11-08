package taskstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"time"
	"unicode"
	"unicode/utf8"
)

const CleanupBatchItemLimit = 1000
const cleanupBatchManifestByteLimit = 1 << 20

type CleanupIdentity struct {
	FsID      int64  `json:"fs_id,string"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size,string"`
	Mtime     int64  `json:"provider_mtime,string"`
	MD5       string `json:"content_md5,omitempty"`
}

type CleanupBatchItem struct {
	Kind                 string           `json:"kind"`
	Expected             CleanupIdentity  `json:"expected"`
	Retained             *CleanupIdentity `json:"retained,omitempty"`
	ExpectedSHA256       string           `json:"expected_sha256,omitempty"`
	PreviewObservedAtUTC time.Time        `json:"client_preview_observed_at_utc"`
}

type CleanupBatchManifest struct {
	ID                     string             `json:"batch_id"`
	AccountFingerprint     string             `json:"account_fingerprint"`
	ApplicationFingerprint string             `json:"application_fingerprint"`
	CacheNamespace         string             `json:"cache_namespace"`
	CreatedAtUTC           time.Time          `json:"created_at_utc"`
	Items                  []CleanupBatchItem `json:"items"`
}

func validCleanupHex(value string, byteLength int) bool {
	if len(value) != byteLength*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validCleanupIdentity(item CleanupIdentity) bool {
	if item.FsID <= 0 || item.Size < 0 || item.Mtime < 0 || item.Path == "/" || !validInventoryPath(item.Path) || len(item.Path) > 2048 || !utf8.ValidString(item.Path) || item.Name != path.Base(item.Path) || len(item.MD5) > 128 {
		return false
	}
	for _, char := range item.Path {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

// This validates a frozen request, not permission to delete. The worker must
// still verify current account/app readiness and each remote identity/content.
// Creation time belongs in the durable batch row: retrying an identical request
// must not produce a different content digest merely because it arrived later.
func encodeCleanupBatchManifest(m CleanupBatchManifest) ([]byte, string, error) {
	if !validCleanupHex(m.ID, 16) || !validCleanupHex(m.AccountFingerprint, 32) || !validCleanupHex(m.ApplicationFingerprint, 32) || !validCleanupHex(m.CacheNamespace, 32) || m.CreatedAtUTC.IsZero() {
		return nil, "", fmt.Errorf("invalid cleanup batch identity or local receive time")
	}
	if len(m.Items) == 0 || len(m.Items) > CleanupBatchItemLimit {
		return nil, "", fmt.Errorf("cleanup batch must contain 1 to %d items", CleanupBatchItemLimit)
	}
	deletedIDs := make(map[int64]bool, len(m.Items))
	deletedPaths := make(map[string]bool, len(m.Items))
	for index, item := range m.Items {
		if !validCleanupIdentity(item.Expected) || item.PreviewObservedAtUTC.IsZero() || deletedIDs[item.Expected.FsID] || deletedPaths[item.Expected.Path] {
			return nil, "", fmt.Errorf("invalid or repeated cleanup identity at item %d", index+1)
		}
		switch item.Kind {
		case "empty":
			if !item.Expected.Directory || item.Retained != nil || item.ExpectedSHA256 != "" {
				return nil, "", fmt.Errorf("invalid empty-directory contract at item %d", index+1)
			}
		case "duplicate":
			if item.Expected.Directory || item.Retained == nil || !validCleanupIdentity(*item.Retained) || item.Retained.Directory || item.Expected.Size != item.Retained.Size || !validCleanupHex(item.ExpectedSHA256, 32) {
				return nil, "", fmt.Errorf("invalid duplicate-content contract at item %d", index+1)
			}
		default:
			return nil, "", fmt.Errorf("invalid cleanup kind at item %d", index+1)
		}
		deletedIDs[item.Expected.FsID], deletedPaths[item.Expected.Path] = true, true
	}
	type retainedContract struct {
		Identity CleanupIdentity
		SHA256   string
	}
	keptIDs := make(map[int64]retainedContract)
	keptPaths := make(map[string]retainedContract)
	for index, item := range m.Items {
		if item.Retained == nil {
			continue
		}
		keep := *item.Retained
		if deletedIDs[keep.FsID] || deletedPaths[keep.Path] {
			return nil, "", fmt.Errorf("retained copy also selected for deletion at item %d", index+1)
		}
		contract := retainedContract{Identity: keep, SHA256: item.ExpectedSHA256}
		if existing, ok := keptIDs[keep.FsID]; ok && existing != contract {
			return nil, "", fmt.Errorf("retained identity has conflicting observations at item %d", index+1)
		}
		if existing, ok := keptPaths[keep.Path]; ok && existing != contract {
			return nil, "", fmt.Errorf("retained path has conflicting observations at item %d", index+1)
		}
		keptIDs[keep.FsID], keptPaths[keep.Path] = contract, contract
	}
	items := append([]CleanupBatchItem(nil), m.Items...)
	for index := range items {
		items[index].PreviewObservedAtUTC = items[index].PreviewObservedAtUTC.UTC()
	}
	payload := struct {
		Version                int                `json:"version"`
		ID                     string             `json:"batch_id"`
		AccountFingerprint     string             `json:"account_fingerprint"`
		ApplicationFingerprint string             `json:"application_fingerprint"`
		CacheNamespace         string             `json:"cache_namespace"`
		Items                  []CleanupBatchItem `json:"items"`
	}{1, m.ID, m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace, items}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	if len(body) > cleanupBatchManifestByteLimit {
		return nil, "", fmt.Errorf("cleanup batch manifest exceeds %d bytes", cleanupBatchManifestByteLimit)
	}
	digest := sha256.Sum256(body)
	return body, hex.EncodeToString(digest[:]), nil
}

// Validate client selection independently of backend-owned account scope/time.
func ValidateCleanupBatchSelection(id string, items []CleanupBatchItem) error {
	const scope = "0000000000000000000000000000000000000000000000000000000000000000"
	_, _, err := encodeCleanupBatchManifest(CleanupBatchManifest{ID: id, AccountFingerprint: scope, ApplicationFingerprint: scope, CacheNamespace: scope, CreatedAtUTC: time.Unix(1, 0).UTC(), Items: items})
	return err
}
