// Package directoryanalysis compares recorded observations without performing
// filesystem or provider operations. A report is never deletion authorization.
package directoryanalysis

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"math/big"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Entry struct {
	Path      string     `json:"path"`
	ID        string     `json:"id,omitempty"`
	Directory bool       `json:"directory"`
	Size      string     `json:"size"`
	SHA256    string     `json:"sha256,omitempty"`
	Observed  *time.Time `json:"observed_at_utc"`
	Implicit  bool       `json:"implied_parent,omitempty"`
}
type Input struct {
	Label    string  `json:"label"`
	Root     string  `json:"root"`
	Complete bool    `json:"complete"`
	Entries  []Entry `json:"entries"`
}
type Summary struct {
	Files           string `json:"files"`
	Directories     string `json:"directories"`
	Bytes           string `json:"bytes"`
	Unverified      string `json:"unverified_files"`
	Complete        bool   `json:"listing_complete"`
	ContentComplete bool   `json:"content_complete"`
	TreeDigest      string `json:"tree_digest,omitempty"`
	ContentDigest   string `json:"content_digest,omitempty"`
}
type Row struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Left  *Entry `json:"left"`
	Right *Entry `json:"right"`
}
type Member struct {
	Side  string `json:"side"`
	Entry Entry  `json:"entry"`
}
type Group struct {
	Key                 string   `json:"key"`
	SHA256              string   `json:"sha256"`
	Size                string   `json:"size"`
	RedundantBytesLeft  string   `json:"redundant_bytes_left"`
	RedundantBytesRight string   `json:"redundant_bytes_right"`
	Members             []Member `json:"members"`
	CapacityBasis       string   `json:"capacity_basis"`
}
type Report struct {
	Version  string  `json:"version"`
	Relation string  `json:"relation"`
	Left     Summary `json:"left"`
	Right    Summary `json:"right"`
	Rows     []Row   `json:"rows"`
	Groups   []Group `json:"groups"`
}
type side struct {
	entries map[string]*Entry
	summary Summary
	keys    []string
}

func verified(e *Entry) bool {
	digest, err := hex.DecodeString(e.SHA256)
	return err == nil && len(digest) == sha256.Size
}
func contentKey(e *Entry) string { return strings.ToLower(e.SHA256) + ":" + e.Size }
func field(h hash.Hash, text string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len([]byte(text))))
	h.Write(length[:])
	h.Write([]byte(text))
}
func prepare(input Input) (side, error) {
	result := side{entries: map[string]*Entry{}}
	folded := map[string]bool{}
	for _, entry := range input.Entries {
		if entry.Path == "" || entry.Path == "." || entry.Path == ".." || strings.HasPrefix(entry.Path, "/") || strings.HasPrefix(entry.Path, "../") || path.Clean(entry.Path) != entry.Path || strings.ContainsAny(entry.Path, "\\\x00") {
			return result, fmt.Errorf("目录分析包含无效相对路径")
		}
		fold := strings.ToLower(entry.Path)
		if folded[fold] {
			return result, fmt.Errorf("目录分析存在重复或大小写冲突路径：%s", entry.Path)
		}
		folded[fold] = true
		size, err := strconv.ParseInt(entry.Size, 10, 64)
		if err != nil || size < 0 || strconv.FormatInt(size, 10) != entry.Size {
			return result, fmt.Errorf("目录分析大小证据无效")
		}
		if entry.Directory {
			entry.Size = "0"
		}
		entry.SHA256 = strings.ToLower(entry.SHA256)
		copy := entry
		result.entries[entry.Path] = &copy
	}
	for _, entry := range input.Entries {
		for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
			if existing := result.entries[parent]; existing != nil {
				if !existing.Directory {
					return result, fmt.Errorf("目录分析父路径被文件占用：%s", parent)
				}
			} else {
				fold := strings.ToLower(parent)
				if folded[fold] {
					return result, fmt.Errorf("目录分析父路径存在大小写冲突：%s", parent)
				}
				folded[fold] = true
				result.entries[parent] = &Entry{Path: parent, Directory: true, Size: "0", Implicit: true}
			}
		}
	}
	for p := range result.entries {
		result.keys = append(result.keys, p)
	}
	sort.Strings(result.keys)
	bytes := new(big.Int)
	files, dirs, missing := 0, 0, 0
	tree := sha256.New()
	contents := []string{}
	field(tree, "directory-tree-v1")
	for _, p := range result.keys {
		e := result.entries[p]
		field(tree, p)
		if e.Directory {
			dirs++
			field(tree, "directory")
			continue
		}
		files++
		size, _ := new(big.Int).SetString(e.Size, 10)
		bytes.Add(bytes, size)
		field(tree, "file")
		field(tree, e.Size)
		field(tree, e.SHA256)
		if !verified(e) {
			missing++
		} else {
			contents = append(contents, contentKey(e))
		}
	}
	result.summary = Summary{Files: strconv.Itoa(files), Directories: strconv.Itoa(dirs), Bytes: bytes.String(), Unverified: strconv.Itoa(missing), Complete: input.Complete, ContentComplete: input.Complete && missing == 0}
	if result.summary.ContentComplete {
		result.summary.TreeDigest = hex.EncodeToString(tree.Sum(nil))
		sort.Strings(contents)
		digest := sha256.New()
		field(digest, "directory-content-multiset-v1")
		for _, key := range contents {
			field(digest, key)
		}
		result.summary.ContentDigest = hex.EncodeToString(digest.Sum(nil))
	}
	return result, nil
}

func Compare(left, right Input) (Report, error) {
	report := Report{Version: "directory-comparison-v1", Rows: []Row{}, Groups: []Group{}}
	l, err := prepare(left)
	if err != nil {
		return report, err
	}
	r, err := prepare(right)
	if err != nil {
		return report, err
	}
	report.Left, report.Right = l.summary, r.summary
	switch {
	case !l.summary.ContentComplete || !r.summary.ContentComplete:
		report.Relation = "incomplete"
	case l.summary.TreeDigest == r.summary.TreeDigest:
		report.Relation = "identical_tree"
	case l.summary.ContentDigest == r.summary.ContentDigest:
		report.Relation = "same_content_different_structure"
	default:
		report.Relation = "different"
	}
	groups := map[string]*Group{}
	sideContent := map[string]map[string]bool{"left": {}, "right": {}}
	for name, s := range map[string]side{"left": l, "right": r} {
		for _, p := range s.keys {
			e := s.entries[p]
			if e.Directory || !verified(e) {
				continue
			}
			key := contentKey(e)
			sideContent[name][key] = true
			g := groups[key]
			if g == nil {
				g = &Group{Key: key, SHA256: e.SHA256, Size: e.Size, Members: []Member{}}
				groups[key] = g
			}
			g.Members = append(g.Members, Member{name, *e})
		}
	}
	paths := map[string]bool{}
	for p := range l.entries {
		paths[p] = true
	}
	for p := range r.entries {
		paths[p] = true
	}
	keys := []string{}
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		a, b := l.entries[p], r.entries[p]
		kind := ""
		switch {
		case a != nil && b != nil:
			if a.Directory != b.Directory {
				kind = "type_conflict"
			} else if a.Directory {
				kind = "same_directory"
			} else if !verified(a) || !verified(b) {
				kind = "unverified"
			} else if contentKey(a) == contentKey(b) {
				kind = "same"
			} else {
				kind = "content_conflict"
			}
		case a != nil:
			kind = "only_left"
			if !right.Complete {
				kind = "right_not_fully_observed"
			} else if !a.Directory && verified(a) && sideContent["right"][contentKey(a)] {
				kind = "same_content_elsewhere"
			}
		case b != nil:
			kind = "only_right"
			if !left.Complete {
				kind = "left_not_fully_observed"
			} else if !b.Directory && verified(b) && sideContent["left"][contentKey(b)] {
				kind = "same_content_elsewhere"
			}
		}
		report.Rows = append(report.Rows, Row{kind, p, a, b})
	}
	groupKeys := []string{}
	for key, g := range groups {
		if len(g.Members) > 1 {
			groupKeys = append(groupKeys, key)
		}
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		g := groups[key]
		sort.Slice(g.Members, func(i, j int) bool {
			if g.Members[i].Side != g.Members[j].Side {
				return g.Members[i].Side < g.Members[j].Side
			}
			return g.Members[i].Entry.Path < g.Members[j].Entry.Path
		})
		counts := map[string]int{}
		identities := map[string]map[string]bool{"left": {}, "right": {}}
		for _, m := range g.Members {
			key := "id:" + m.Entry.ID
			if m.Entry.ID == "" {
				key = "path:" + m.Entry.Path
			}
			if !identities[m.Side][key] {
				identities[m.Side][key] = true
				counts[m.Side]++
			}
		}
		saved := func(count int) string {
			if count < 2 {
				return "0"
			}
			size, _ := new(big.Int).SetString(g.Size, 10)
			return size.Mul(size, big.NewInt(int64(count-1))).String()
		}
		g.RedundantBytesLeft = saved(counts["left"])
		g.RedundantBytesRight = saved(counts["right"])
		g.CapacityBasis = "identity_deduplicated_logical_bytes_upper_bound"
		report.Groups = append(report.Groups, *g)
	}
	return report, nil
}
