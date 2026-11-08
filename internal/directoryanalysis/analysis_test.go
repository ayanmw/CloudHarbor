package directoryanalysis

import (
	"strings"
	"testing"
)

func item(p, h string) Entry { return Entry{Path: p, Size: "3", SHA256: strings.Repeat(h, 64)} }
func TestCompareDirectoryEvidence(t *testing.T) {
	a, b := item("a", "a"), item("b", "a")
	for _, tc := range []struct {
		name        string
		left, right Input
		relation    string
	}{
		{"identical", Input{Complete: true, Entries: []Entry{a}}, Input{Complete: true, Entries: []Entry{a}}, "identical_tree"},
		{"different_structure", Input{Complete: true, Entries: []Entry{a}}, Input{Complete: true, Entries: []Entry{b}}, "same_content_different_structure"},
		{"multiplicity_is_not_set_equality", Input{Complete: true, Entries: []Entry{a, b}}, Input{Complete: true, Entries: []Entry{a}}, "different"},
		{"missing_coverage", Input{Entries: []Entry{a}}, Input{Complete: true, Entries: []Entry{a}}, "incomplete"},
		{"missing_hash", Input{Complete: true, Entries: []Entry{{Path: "a", Size: "3"}}}, Input{Complete: true, Entries: []Entry{a}}, "incomplete"},
		{"empty_directory_matters", Input{Complete: true, Entries: []Entry{{Path: "empty", Directory: true, Size: "0"}}}, Input{Complete: true}, "same_content_different_structure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Compare(tc.left, tc.right)
			if err != nil || r.Relation != tc.relation {
				t.Fatalf("relation=%s err=%v", r.Relation, err)
			}
		})
	}
}
func TestRowsAndSavingsDoNotTreatSyncCopiesAsWaste(t *testing.T) {
	a := item("a", "a")
	b := item("b", "a")
	r, err := Compare(Input{Complete: true, Entries: []Entry{a, b}}, Input{Complete: true, Entries: []Entry{a}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0].RedundantBytesLeft != "3" || r.Groups[0].RedundantBytesRight != "0" {
		t.Fatalf("wrong savings %+v", r.Groups)
	}
	if len(r.Rows) != 2 || r.Rows[0].Kind != "same" || r.Rows[1].Kind != "same_content_elsewhere" {
		t.Fatalf("rows %+v", r.Rows)
	}
}
func TestAmbiguousPathsFailBeforeIssuingAConclusion(t *testing.T) {
	for _, entries := range [][]Entry{{item("../escape", "a")}, {item("a", "a"), item("A", "a")}, {{Path: "a", Directory: true, Size: "0"}, item("a/child", "a"), item("a", "b")}} {
		if _, err := Compare(Input{Complete: true, Entries: entries}, Input{Complete: true}); err == nil {
			t.Fatal("ambiguous paths accepted")
		}
	}
}

func TestKnownHardLinksAreNotCountedAsIndependentCopies(t *testing.T) {
	a, b := item("a", "a"), item("b", "a")
	a.ID = "same-file"
	b.ID = "same-file"
	r, err := Compare(Input{Complete: true, Entries: []Entry{a, b}}, Input{Complete: true})
	if err != nil || len(r.Groups) != 1 || r.Groups[0].RedundantBytesLeft != "0" {
		t.Fatalf("hard link capacity %+v %v", r.Groups, err)
	}
	b.ID = "other-file"
	r, err = Compare(Input{Complete: true, Entries: []Entry{a, b}}, Input{Complete: true})
	if err != nil || r.Groups[0].RedundantBytesLeft != "3" {
		t.Fatal("independent copies not counted")
	}
}
