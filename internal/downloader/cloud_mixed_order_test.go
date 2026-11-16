package downloader

import (
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestCloudMixedOrderAllFieldsAndDirections(t *testing.T) {
	for _, field := range []string{"name", "size", "time"} {
		for _, desc := range []bool{false, true} {
			for _, mixed := range []bool{false, true} {
				entries := []CloudEntry{{FsID: 1, Name: "a-file", Size: 40, Mtime: 20}, {FsID: 2, Name: "b-dir", Directory: true, Size: 10, SizeComplete: true, Mtime: 40}, {FsID: 3, Name: "c-file", Size: 20, Mtime: 10}, {FsID: 4, Name: "d-dir", Directory: true, Size: 30, SizeComplete: true, Mtime: 30}}
				sortCloudEntries(entries, baiduapi.DirectoryOrder{Field: field, Descending: desc}, mixed)
				expected := map[string][][]int64{
					"name": {{1, 2, 3, 4}, {4, 3, 2, 1}, {2, 4, 1, 3}, {4, 2, 3, 1}},
					"size": {{2, 3, 4, 1}, {1, 4, 3, 2}, {2, 4, 3, 1}, {4, 2, 1, 3}},
					"time": {{3, 1, 4, 2}, {2, 4, 1, 3}, {4, 2, 3, 1}, {2, 4, 1, 3}},
				}
				index := 0
				if desc {
					index++
				}
				if !mixed {
					index += 2
				}
				want := expected[field][index]
				for i, id := range want {
					if entries[i].FsID != id {
						t.Fatalf("%s desc=%v mixed=%v: %+v", field, desc, mixed, entries)
					}
				}
			}
		}
	}
}

func TestCloudGroupedSizeKeepsUnknownLastWithinDirectoryGroupAndTiesAscending(t *testing.T) {
	for _, desc := range []bool{false, true} {
		entries := []CloudEntry{{FsID: 1, Name: "z-file", Size: 0}, {FsID: 2, Name: "unknown", Directory: true}, {FsID: 3, Name: "b-dir", Directory: true, SizeComplete: true}, {FsID: 4, Name: "a-dir", Directory: true, SizeComplete: true}}
		sortCloudEntries(entries, baiduapi.DirectoryOrder{Field: "size", Descending: desc}, false)
		for i, id := range []int64{4, 3, 2, 1} {
			if entries[i].FsID != id {
				t.Fatalf("desc=%v: %+v", desc, entries)
			}
		}
	}
}
