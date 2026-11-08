package downloader

import (
	"sort"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// Unknown directory sizes remain last in both directions. The tie-break order
// never reverses with descending size, and integer comparisons avoid precision
// loss and subtraction overflow. ProviderSize stays untouched for remote checks.
func sortCloudEntriesByDisplaySize(entries []CloudEntry, descending bool) {
	sortCloudEntries(entries, baiduapi.DirectoryOrder{Field: "size", Descending: descending}, true)
}

// Grouping is a primary key, independent of direction. Each group uses the same order.
func sortCloudEntries(entries []CloudEntry, order baiduapi.DirectoryOrder, mixed bool) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !mixed && a.Directory != b.Directory {
			return a.Directory
		}
		if order.Field == "size" {
			aKnown, bKnown := !a.Directory || a.SizeComplete, !b.Directory || b.SizeComplete
			if aKnown != bKnown {
				return aKnown
			}
			if aKnown && a.Size != b.Size {
				if order.Descending {
					return a.Size > b.Size
				}
				return a.Size < b.Size
			}
		}
		if order.Field == "time" && a.Mtime != b.Mtime {
			if order.Descending {
				return a.Mtime > b.Mtime
			}
			return a.Mtime < b.Mtime
		}
		if a.Name != b.Name {
			if order.Field == "name" && order.Descending {
				return a.Name > b.Name
			}
			return a.Name < b.Name
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.FsID < b.FsID
	})
}
