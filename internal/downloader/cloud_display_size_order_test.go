package downloader

import (
	"fmt"
	"math"
	"testing"
)

func TestDisplaySizeOrderSortsWholeListingWithNameAscendingTies(t *testing.T) {
	for _, descending := range []bool{false, true} {
		t.Run(fmt.Sprint(descending), func(t *testing.T) {
			entries := []CloudEntry{{FsID: 900, Name: "unknown-z", Path: "/unknown-z", Directory: true, Size: 0}, {FsID: 901, Name: "unknown-a", Path: "/unknown-a", Directory: true, Size: math.MaxInt64}, {FsID: 800, Name: "empty", Path: "/empty", Directory: true, SizeComplete: true, Size: 0}}
			for n := 319; n >= 0; n-- {
				entries = append(entries, CloudEntry{FsID: int64(n + 1), Name: fmt.Sprintf("same-%03d", n), Path: fmt.Sprintf("/same-%03d", n), Directory: n%2 == 0, SizeComplete: n%2 == 0, Size: 9007199254740993, ProviderSize: 17})
			}
			entries = append(entries, CloudEntry{FsID: 700, Name: "larger", Path: "/larger", Size: 9007199254740994})
			sortCloudEntriesByDisplaySize(entries, descending)
			if entries[len(entries)-2].Name != "unknown-a" || entries[len(entries)-1].Name != "unknown-z" {
				t.Fatal("unknown sizes not last in name order")
			}
			first := 1
			if descending {
				if entries[0].Name != "larger" {
					t.Fatal("integer order lost")
				}
			} else if entries[0].Name != "empty" {
				t.Fatal("known zero not first")
			}
			for n := 0; n < 320; n++ {
				entry := entries[first+n]
				if entry.Name != fmt.Sprintf("same-%03d", n) || entry.ProviderSize != 17 {
					t.Fatalf("tie or provider size changed at %d: %+v", n, entry)
				}
			}
		})
	}
}

func TestDisplaySizeOrderKeepsExtremeBytesAndIdentityTiesExact(t *testing.T) {
	for _, descending := range []bool{false, true} {
		entries := []CloudEntry{{FsID: 9007199254740994, Name: "same", Path: "/same", Size: math.MaxInt64}, {FsID: 9007199254740993, Name: "same", Path: "/same", Size: math.MaxInt64}, {FsID: 1, Name: "zero", Path: "/zero", Size: 0}}
		sortCloudEntriesByDisplaySize(entries, descending)
		start := 1
		if descending {
			start = 0
		}
		if entries[start].FsID != 9007199254740993 || entries[start+1].FsID != 9007199254740994 {
			t.Fatalf("identity precision lost: %+v", entries)
		}
	}
}
