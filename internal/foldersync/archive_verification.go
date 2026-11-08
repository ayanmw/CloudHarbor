package foldersync

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// RemoteArchiveVerification binds a complete fresh archive observation to the
// original relative paths. The caller persists it as a new execution event.
type ArchiveMtimeChange struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Before string `json:"before_mtime"`
	After  string `json:"after_mtime"`
	Unit   string `json:"mtime_unit"`
}
type RemoteArchiveVerification struct {
	ExpectedContentIdentitySHA256 string               `json:"expected_content_identity_sha256"`
	ObservedContentIdentitySHA256 string               `json:"observed_content_identity_sha256"`
	MtimeChanges                  []ArchiveMtimeChange `json:"mtime_changes"`

	Version        string    `json:"version"`
	Root           string    `json:"root"`
	ArchivePath    string    `json:"archive_path"`
	ExpectedSHA256 string    `json:"expected_sha256"`
	ObservedSHA256 string    `json:"observed_sha256"`
	ExpectedItems  string    `json:"expected_items"`
	ObservedItems  string    `json:"observed_items"`
	VerifiedAt     time.Time `json:"verified_at_utc"`
	Entries        []Entry   `json:"entries"`
}

// SubtreeFingerprint excludes observation times, retaining object identity,
// paths, type, size, content hashes and modification-time evidence.
// An empty root fingerprints the entire observation.
func SubtreeFingerprint(entries []Entry, root string) (string, string) {
	return subtreeIdentity(entries, root)
}

// Archive movement may retimestamp every descendant. This separate comparison
// excludes only mtime values, retaining paths/IDs/types/content/units. Full
// fingerprints and exact time differences are still preserved independently.
func ArchiveContentIdentity(entries []Entry, root string) (string, string) {
	copy := append([]Entry{}, entries...)
	for i := range copy {
		copy[i].Mtime = 0
	}
	hash, count := subtreeIdentity(copy, root)
	return fmt.Sprintf("%x", sha256.Sum256([]byte("archive-content-identity-v1\n"+hash))), count
}
func ArchiveMtimeChanges(expected, observed []Entry, root string) []ArchiveMtimeChange {
	before := index(expected)
	changes := []ArchiveMtimeChange{}
	for _, e := range observed {
		if !under(e.Path, root) {
			continue
		}
		old := before[e.Path]
		if old != nil && old.ID == e.ID && old.Mtime != e.Mtime {
			changes = append(changes, ArchiveMtimeChange{e.Path, e.ID, strconv.FormatInt(old.Mtime, 10), strconv.FormatInt(e.Mtime, 10), e.MtimeUnit})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}
