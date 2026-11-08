package foldersync

import (
	"path"
	"sort"
	"strings"
)

func index(entries []Entry) map[string]*Entry {
	m := map[string]*Entry{}
	for i := range entries {
		m[entries[i].Path] = &entries[i]
	}
	return m
}
func sameContent(a, b *Entry) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Directory == b.Directory && (a.Directory || a.Size == b.Size && a.SHA256 != "" && a.SHA256 == b.SHA256)
}
func same(a, b *Entry) bool { return sameContent(a, b) && (a == nil || a.Path == b.Path) }
func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}

// Plan compares two observations to their last jointly verified baseline. It
// never infers deletion from an incomplete scan (the caller cannot pass one).
func Plan(mode string, base, now Snapshot) []Step {
	bl, br, l, r := index(base.Local), index(base.Remote), index(now.Local), index(now.Remote)
	usedL, usedR := map[string]bool{}, map[string]bool{}
	var ops []Step
	blocked := []string{}
	add := func(action, p, from, reason string, a, b *Entry) {
		ops = append(ops, Step{action, p, from, a, b, reason})
	}
	// A directory deletion conflicts with new/edited descendants on the other
	// side, even when the directory's own metadata did not change.
	for p, b := range bl {
		if !b.Directory || br[p] == nil {
			continue
		}
		if (l[p] == nil) == (r[p] == nil) {
			continue
		}
		var current, prior map[string]*Entry
		if l[p] == nil {
			current, prior = r, br
		} else {
			current, prior = l, bl
		}
		changed := false
		for q, e := range current {
			if q != p && under(q, p) && !same(e, prior[q]) {
				changed = true
			}
		}
		if changed {
			blocked = append(blocked, p)
			add("conflict", p, "", "目录删除与另一端子项变化冲突", l[p], r[p])
		}
	}
	isBlocked := func(p string) bool {
		for _, b := range blocked {
			if under(p, b) {
				return true
			}
		}
		return false
	}
	find := func(old *Entry, current map[string]*Entry) *Entry {
		if old == nil {
			return nil
		}
		var found *Entry
		for _, e := range current {
			if e.ID != "" && e.ID == old.ID {
				if found != nil {
					return current[old.Path]
				}
				found = e
			}
		}
		if found != nil {
			return found
		}
		return current[old.Path]
	}
	// Files retain identity through renames. Directories are expanded into mkdir,
	// child moves and empty-directory removal so child changes stay explicit.
	keys := []string{}
	for p := range bl {
		if br[p] != nil {
			keys = append(keys, p)
		}
	}
	sort.Strings(keys)
	for _, p := range keys {
		if isBlocked(p) {
			continue
		}
		a, b := bl[p], br[p]
		var x, y *Entry
		if a.Directory {
			x, y = l[p], r[p]
		} else {
			x, y = find(a, l), find(b, r)
		}
		if x != nil {
			usedL[x.Path] = true
		}
		if y != nil {
			usedR[y.Path] = true
		}
		if same(x, y) {
			continue
		}
		lc, rc := !same(a, x), !same(b, y)
		if mode == "pull" {
			if !rc {
				if lc {
					add("conflict", p, "", "本地已偏离共同基线，单向模式保留且不上传", x, y)
				}
				continue
			}
			if lc {
				add("conflict", p, "", "远端变化与本地偏离基线冲突", x, y)
				continue
			}
			if y == nil {
				add("local_delete", x.Path, "", "远端删除", x, nil)
			} else if y.Directory {
				add("local_mkdir", y.Path, "", "远端新增目录", x, y)
			} else if sameContent(x, y) {
				add("local_move", y.Path, x.Path, "远端移动或改名", x, y)
			} else {
				add("download", y.Path, "", "远端内容变化", x, y)
			}
			continue
		}
		if x == nil || y == nil {
			if lc && rc {
				add("conflict", p, "", "删除与另一端变化冲突", x, y)
			} else if x == nil {
				add("remote_delete", y.Path, "", "本地删除", nil, y)
			} else {
				add("local_delete", x.Path, "", "远端删除", x, nil)
			}
			continue
		}
		if x.Directory != y.Directory {
			add("conflict", p, "", "文件与目录类型冲突", x, y)
			continue
		}
		if lc && rc {
			// A prior directory move can already have changed paths on both
			// sides. Merge a one-sided content edit only at the same new path.
			if x.Path == y.Path && sameContent(a, x) && !sameContent(b, y) {
				add("download", y.Path, "", "远端内容变化", x, y)
			} else if x.Path == y.Path && !sameContent(a, x) && sameContent(b, y) {
				add("upload", x.Path, "", "本地内容变化", x, y)
			} else {
				add("conflict", p, "", "双端变化冲突", x, y)
			}
		} else if lc {
			if sameContent(x, y) {
				add("remote_move", x.Path, y.Path, "本地移动或改名", x, y)
			} else {
				add("upload", x.Path, "", "本地内容变化", x, y)
			}
		} else if rc {
			if sameContent(x, y) {
				add("local_move", y.Path, x.Path, "远端移动或改名", x, y)
			} else {
				add("download", y.Path, "", "远端内容变化", x, y)
			}
		}
	}
	all := map[string]bool{}
	for p := range l {
		if !usedL[p] {
			all[p] = true
		}
	}
	for p := range r {
		if !usedR[p] {
			all[p] = true
		}
	}
	keys = nil
	for p := range all {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		if isBlocked(p) {
			continue
		}
		x, y := l[p], r[p]
		if usedL[p] {
			x = nil
		}
		if usedR[p] {
			y = nil
		}
		if same(x, y) {
			continue
		}
		if x != nil && y != nil {
			add("conflict", p, "", "首次同路径内容不同", x, y)
		} else if y != nil {
			action := "download"
			if y.Directory {
				action = "local_mkdir"
			}
			add(action, p, "", "远端新增", nil, y)
		} else if x != nil && mode == "both" {
			action := "upload"
			if x.Directory {
				action = "remote_mkdir"
			}
			add(action, p, "", "本地新增", x, nil)
		}
	}
	// Moving into an occupied path is a conflict, not an overwrite or a rename
	// cycle. Removals never free a destination implicitly in the same run.
	for i := range ops {
		op := &ops[i]
		if op.Action == "local_move" && l[op.Path] != nil || op.Action == "remote_move" && r[op.Path] != nil {
			op.Action = "conflict"
			op.Reason = "移动目标已占用"
		}
	}
	priority := func(a string) int {
		switch a {
		case "conflict":
			return 0
		case "local_mkdir", "remote_mkdir":
			return 1
		case "local_delete", "remote_delete":
			return 3
		}
		return 2
	}
	sort.SliceStable(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if priority(a.Action) != priority(b.Action) {
			return priority(a.Action) < priority(b.Action)
		}
		if priority(a.Action) == 3 {
			if strings.Count(a.Path, "/") != strings.Count(b.Path, "/") {
				return strings.Count(a.Path, "/") > strings.Count(b.Path, "/")
			}
		} else if priority(a.Action) == 1 {
			if strings.Count(a.Path, "/") != strings.Count(b.Path, "/") {
				return strings.Count(a.Path, "/") < strings.Count(b.Path, "/")
			}
		}
		return path.Clean(a.Path) < path.Clean(b.Path)
	})
	return ops
}
