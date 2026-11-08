package foldersync

import (
	"fmt"
	"strings"
)

// Verify only the declared namespace/content transition. A fresh snapshot is
// not automatically a new trusted baseline: unrelated edits must be rejected.
func verifySubtreeTransition(before, after Snapshot, step Step) error {
	local := strings.HasPrefix(step.Action, "local_") || step.Action == "download"
	if !local && step.Action != "upload" && step.Action != "remote_mkdir" && step.Action != "remote_archive" {
		return fmt.Errorf("不支持的子树步骤")
	}
	for _, pair := range []struct {
		old, now []Entry
		affected bool
	}{{before.Local, after.Local, local}, {before.Remote, after.Remote, !local}} {
		old, now := index(pair.old), index(pair.now)
		expected := map[string]*Entry{}
		for p, e := range old {
			v := *e
			expected[p] = &v
		}
		changed := []string{step.Path}
		if pair.affected {
			switch step.Action {
			case "local_archive", "remote_archive":
				source := step.Local
				if !local {
					source = step.Remote
				}
				if source == nil || old[step.Path] == nil || source.ID != old[step.Path].ID {
					return fmt.Errorf("归档来源身份不符")
				}
				for p := range expected {
					if under(p, step.Path) {
						delete(expected, p)
					}
				}
			case "local_move":
				if old[step.From] == nil || step.Local == nil || old[step.From].ID != step.Local.ID {
					return fmt.Errorf("副本移动缺少来源")
				}
				changed = append(changed, step.From)
				for p := range old {
					if under(p, step.Path) {
						return fmt.Errorf("副本目标已占用")
					}
				}
				for p, e := range old {
					if under(p, step.From) {
						v := *e
						v.Path = step.Path + strings.TrimPrefix(p, step.From)
						expected[v.Path] = &v
						delete(expected, p)
					}
				}
			case "download", "upload", "local_mkdir", "remote_mkdir":
				source := step.Remote
				if !local {
					source = step.Local
				}
				got := now[step.Path]
				if old[step.Path] != nil || source == nil || got == nil || !sameContent(source, got) || got.ID == "" {
					return fmt.Errorf("新建或传输结果与步骤不符")
				}
				if (step.Action == "download" || step.Action == "upload") && got.Directory || (step.Action == "local_mkdir" || step.Action == "remote_mkdir") && !got.Directory {
					return fmt.Errorf("新项目类型不符")
				}
				v := *got
				expected[step.Path] = &v
			}
		}
		if len(expected) != len(now) {
			return fmt.Errorf("存在步骤外项目增删")
		}
		for p, want := range expected {
			got := now[p]
			if got == nil {
				return fmt.Errorf("步骤外项目缺失")
			}
			w, g := *want, *got
			// Creating/removing a child updates its ancestor directories' mtimes.
			// Their stable IDs and all other fields remain bound to the prior snapshot.
			if pair.affected && w.Directory && g.Directory {
				for _, pChanged := range changed {
					if strings.HasPrefix(pChanged, p+"/") {
						w.Mtime = g.Mtime
						break
					}
				}
			}
			wh, _ := subtreeIdentity([]Entry{w}, p)
			gh, _ := subtreeIdentity([]Entry{g}, p)
			if wh != gh {
				return fmt.Errorf("步骤外身份、内容或元数据变化：%s", p)
			}
		}
	}
	return nil
}
