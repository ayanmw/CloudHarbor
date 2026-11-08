package foldersync

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

type SubtreeCheckEvidence struct {
	Version              string `json:"version"`
	Root                 string `json:"root"`
	SourceSnapshotSHA256 string `json:"source_snapshot_sha256"`
	LocalExpectedSHA256  string `json:"local_expected_sha256"`
	LocalObservedSHA256  string `json:"local_observed_sha256"`
	RemoteExpectedSHA256 string `json:"remote_expected_sha256"`
	RemoteObservedSHA256 string `json:"remote_observed_sha256"`
	LocalExpectedItems   string `json:"local_expected_items"`
	LocalObservedItems   string `json:"local_observed_items"`
	RemoteExpectedItems  string `json:"remote_expected_items"`
	RemoteObservedItems  string `json:"remote_observed_items"`
}

// Observed-at timestamps are retained in the source/current snapshots, not
// included in equality: a fresh observation must have a new observation time.
// Path, stable ID, type, size, content and provider/filesystem mtime remain bound.
func subtreeIdentity(entries []Entry, root string) (string, string) {
	type identity struct {
		Path, ID                            string
		Directory                           bool
		Size, Mtime, MtimeUnit, SHA256, MD5 string
	}
	values := []identity{}
	for _, e := range entries {
		if root == "" || under(e.Path, root) {
			values = append(values, identity{e.Path, e.ID, e.Directory, strconv.FormatInt(e.Size, 10), strconv.FormatInt(e.Mtime, 10), e.MtimeUnit, strings.ToLower(e.SHA256), strings.ToLower(e.MD5)})
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Path < values[j].Path })
	digest := sha256.Sum256([]byte("subtree-identity-v1\n" + encoded(values)))
	return hex.EncodeToString(digest[:]), strconv.Itoa(len(values))
}

func subtreeCheckEvidence(decision Resolution, current Snapshot) *SubtreeCheckEvidence {
	expected := Snapshot{Local: []Entry{}, Remote: []Entry{}}
	for _, item := range decision.SubtreeReview.Items {
		if item.Local != nil {
			expected.Local = append(expected.Local, *item.Local)
		}
		if item.Remote != nil {
			expected.Remote = append(expected.Remote, *item.Remote)
		}
	}
	root := decision.Conflict.Step.Path
	proof := &SubtreeCheckEvidence{Version: "subtree-identity-v1", Root: root, SourceSnapshotSHA256: decision.SnapshotSHA256}
	proof.LocalExpectedSHA256, proof.LocalExpectedItems = subtreeIdentity(expected.Local, root)
	proof.LocalObservedSHA256, proof.LocalObservedItems = subtreeIdentity(current.Local, root)
	proof.RemoteExpectedSHA256, proof.RemoteExpectedItems = subtreeIdentity(expected.Remote, root)
	proof.RemoteObservedSHA256, proof.RemoteObservedItems = subtreeIdentity(current.Remote, root)
	return proof
}
