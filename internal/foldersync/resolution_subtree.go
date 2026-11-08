package foldersync

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

type SubtreeImpact struct {
	Path               string `json:"path"`
	Local              *Entry `json:"local"`
	Remote             *Entry `json:"remote"`
	LocalAction        string `json:"local_action"`
	RemoteAction       string `json:"remote_action"`
	LocalTarget        string `json:"local_target,omitempty"`
	RemoteTarget       string `json:"remote_target,omitempty"`
	RestoreLocalTarget string `json:"restore_local_target,omitempty"`
}
type SubtreeReview struct {
	PlanVersion   string          `json:"plan_version,omitempty"`
	PlanStepCount string          `json:"plan_step_count,omitempty"`
	Paged         bool            `json:"paged,omitempty"`
	ItemCount     string          `json:"item_count,omitempty"`
	Version       string          `json:"version"`
	Root          string          `json:"root"`
	LocalFiles    string          `json:"local_files"`
	RemoteFiles   string          `json:"remote_files"`
	LocalBytes    string          `json:"local_bytes"`
	RemoteBytes   string          `json:"remote_bytes"`
	Note          string          `json:"note"`
	Items         []SubtreeImpact `json:"items"`
}

// This is a semantic impact inventory, not an executable ordering. Type swaps
// and nonempty directories still require ordered backup and child-step plans.
func BuildSubtreeReview(mode, choice, root, copyRoot string, snapshot Snapshot) (*SubtreeReview, error) {
	if !safeRelative(root) || (mode != "pull" && mode != "both") || snapshot.Local == nil || snapshot.Remote == nil {
		return nil, fmt.Errorf("目录影响范围或完整快照无效")
	}
	switch choice {
	case "keep_local", "keep_remote", "keep_both", "skip":
	default:
		return nil, fmt.Errorf("目录处理方案无效")
	}
	local, remote := map[string]*Entry{}, map[string]*Entry{}
	paths := map[string]bool{}
	totals := []*big.Int{new(big.Int), new(big.Int)}
	counts := []int{0, 0}
	for side, all := range [][]Entry{snapshot.Local, snapshot.Remote} {
		target := local
		if side == 1 {
			target = remote
		}
		seen := map[string]bool{}
		for i := range all {
			entry := &all[i]
			if !under(entry.Path, root) {
				continue
			}
			key := strings.ToLower(entry.Path)
			if !safeRelative(entry.Path) || entry.ID == "" || entry.Size < 0 || seen[key] {
				return nil, fmt.Errorf("目录影响快照路径或身份无效")
			}
			seen[key] = true
			target[entry.Path] = entry
			paths[entry.Path] = true
			if !entry.Directory {
				counts[side]++
				totals[side].Add(totals[side], big.NewInt(entry.Size))
			}
		}
		if len(target) > 0 && (target[root] == nil || !target[root].Directory && len(target) > 1) {
			return nil, fmt.Errorf("子项缺少目录根证据，不能据此推断删除")
		}
	}
	review := &SubtreeReview{Version: "subtree-review-v1", Root: root, LocalFiles: strconv.Itoa(counts[0]), RemoteFiles: strconv.Itoa(counts[1]), LocalBytes: totals[0].String(), RemoteBytes: totals[1].String(), Note: "只读子树影响清单，尚非可执行步骤；目录替换与移除必须先保存旧内容并展开子项核对。", Items: []SubtreeImpact{}}
	keys := []string{}
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	bothRoots := local[root] != nil && remote[root] != nil
	if choice == "keep_both" && bothRoots && (!safeRelative(copyRoot) || under(copyRoot, root) || under(root, copyRoot)) {
		return nil, fmt.Errorf("固定副本目录无效或与原范围重叠")
	}
	if choice == "keep_both" && !bothRoots {
		review.Note += " 缺失的一端不是空版本，不创建虚构副本；单向模式不上传本地内容。"
	}
	action := func(want, current *Entry, transfer string) string {
		if want == nil {
			if current == nil {
				return "keep"
			}
			if current.Directory {
				return "remove_directory_after_children"
			}
			return "backup_then_remove"
		}
		if current == nil {
			if want.Directory {
				return "mkdir"
			}
			return transfer
		}
		if want.Directory != current.Directory {
			return "backup_then_replace_type"
		}
		if sameContent(want, current) {
			return "keep"
		}
		return "backup_then_" + transfer
	}
	for _, path := range keys {
		l, r := local[path], remote[path]
		item := SubtreeImpact{Path: path, Local: l, Remote: r, LocalAction: "keep", RemoteAction: "keep"}
		if l != nil {
			item.LocalTarget = path
		}
		if r != nil {
			item.RemoteTarget = path
		}
		switch {
		case choice == "skip" || choice == "keep_local" && mode == "pull":
		case choice == "keep_remote" || choice == "keep_both" && !bothRoots && remote[root] != nil:
			item.LocalAction = action(r, l, "download")
			item.LocalTarget = ""
			if r != nil {
				item.LocalTarget = path
			}
		case choice == "keep_local" || choice == "keep_both" && !bothRoots && mode == "both":
			item.RemoteAction = action(l, r, "upload")
			item.RemoteTarget = ""
			if l != nil {
				item.RemoteTarget = path
			}
		case choice == "keep_both" && bothRoots:
			if l != nil {
				target := copyRoot + strings.TrimPrefix(path, root)
				item.LocalTarget = target
				item.LocalAction = "preserve_local_copy"
				if mode == "both" {
					item.RemoteAction = "upload_copy"
					if l.Directory {
						item.RemoteAction = "create_copy_directory"
					}
					item.RemoteTarget = target
				}
			}
			if r != nil {
				item.RestoreLocalTarget = path
				if l != nil {
					item.LocalAction = "preserve_copy_then_restore_remote"
				} else {
					item.LocalAction = "restore_remote"
					item.LocalTarget = path
				}
			}
		}
		review.Items = append(review.Items, item)
	}
	return review, nil
}
