package foldersync

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// CompileSubtreePlan produces immutable intended steps, not current observations.
// Archive actions preserve the entire old object before any type replacement.
// Their execution remains disabled until subtree guards and archival receipts
// are supported by both local and remote executors.
func CompileSubtreePlan(mode, choice, copyRoot string, review *SubtreeReview) ([]Step, error) {
	if review == nil || review.Paged || review.Version != "subtree-review-v1" {
		return nil, fmt.Errorf("需要完整固定子树证据")
	}
	snapshot := Snapshot{Local: []Entry{}, Remote: []Entry{}}
	for _, item := range review.Items {
		for side, e := range []*Entry{item.Local, item.Remote} {
			if e == nil {
				continue
			}
			if e.Path != item.Path {
				return nil, fmt.Errorf("子树来源路径不一致")
			}
			if !e.Directory {
				if len(e.SHA256) != 64 {
					return nil, fmt.Errorf("子树文件缺少完整SHA256")
				}
				if _, err := hex.DecodeString(e.SHA256); err != nil {
					return nil, fmt.Errorf("子树文件摘要无效")
				}
			}
			if side == 0 {
				snapshot.Local = append(snapshot.Local, *e)
			} else {
				snapshot.Remote = append(snapshot.Remote, *e)
			}
		}
	}
	if _, err := BuildSubtreeReview(mode, choice, review.Root, copyRoot, snapshot); err != nil {
		return nil, err
	}
	steps := []Step{}
	if choice == "skip" || choice == "keep_local" && mode == "pull" {
		return steps, nil
	}
	reason := "固定子树选择：" + choice
	depthLess := func(a, b string) bool {
		da, db := strings.Count(a, "/"), strings.Count(b, "/")
		if da != db {
			return da < db
		}
		return a < b
	}
	reconcile := func(wanted, destination []Entry, local bool) {
		want, dest := index(wanted), index(destination)
		candidates := []string{}
		for p, e := range dest {
			w := want[p]
			if w == nil || w.Directory != e.Directory || !e.Directory && !sameContent(w, e) {
				candidates = append(candidates, p)
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return depthLess(candidates[i], candidates[j]) })
		archived := []string{}
		covered := func(p string) bool {
			for _, root := range archived {
				if under(p, root) {
					return true
				}
			}
			return false
		}
		for _, p := range candidates {
			if covered(p) {
				continue
			}
			e := dest[p]
			s := Step{Path: p, Reason: reason}
			if local {
				s.Action = "local_archive"
				s.Local = e
			} else {
				s.Action = "remote_archive"
				s.Remote = e
			}
			steps = append(steps, s)
			archived = append(archived, p)
		}
		dirs, files := []string{}, []string{}
		for p, w := range want {
			d := dest[p]
			if d != nil && !covered(p) && sameContent(w, d) {
				continue
			}
			if w.Directory {
				dirs = append(dirs, p)
			} else {
				files = append(files, p)
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return depthLess(dirs[i], dirs[j]) })
		sort.Strings(files)
		for _, p := range append(dirs, files...) {
			w := want[p]
			s := Step{Path: p, Reason: reason}
			if local {
				s.Action = "download"
				if w.Directory {
					s.Action = "local_mkdir"
				}
				s.Remote = w
			} else {
				s.Action = "upload"
				if w.Directory {
					s.Action = "remote_mkdir"
				}
				s.Local = w
			}
			steps = append(steps, s)
		}
	}
	l, r := index(snapshot.Local)[review.Root], index(snapshot.Remote)[review.Root]
	switch {
	case choice == "keep_remote":
		reconcile(snapshot.Remote, snapshot.Local, true)
	case choice == "keep_local":
		reconcile(snapshot.Local, snapshot.Remote, false)
	case choice == "keep_both" && l != nil && r != nil:
		steps = append(steps, Step{Action: "local_move", Path: copyRoot, From: review.Root, Local: l, Reason: reason})
		reconcile(snapshot.Remote, nil, true)
		if mode == "both" {
			copies := make([]Entry, 0, len(snapshot.Local))
			for _, entry := range snapshot.Local {
				entry.Path = copyRoot + strings.TrimPrefix(entry.Path, review.Root)
				copies = append(copies, entry)
			}
			reconcile(copies, nil, false)
		}
	case choice == "keep_both" && r != nil:
		reconcile(snapshot.Remote, snapshot.Local, true)
	case choice == "keep_both" && l != nil && mode == "both":
		reconcile(snapshot.Local, snapshot.Remote, false)
	}
	return steps, nil
}
