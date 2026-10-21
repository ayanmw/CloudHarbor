package taskstore

import (
	"path/filepath"
	"testing"
)

func TestIntakeJournalIsIndependentAndRejectsUnsafeIDs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := "0123456789abcdef0123456789abcdef"
	if err = s.SaveIntake(id, []byte(`{"state":"sending"}`)); err != nil {
		t.Fatal(err)
	}
	records, err := s.LoadIntakes()
	if err != nil || string(records[id]) != `{"state":"sending"}` {
		t.Fatal("intake journal lost")
	}
	jobs, err := s.Load()
	if err != nil || len(jobs) != 0 {
		t.Fatal("intake was loaded as download job")
	}
	if s.SaveIntake("../credentials", []byte(`{}`)) == nil {
		t.Fatal("unsafe intake filename accepted")
	}
}
