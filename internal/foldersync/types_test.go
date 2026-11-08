package foldersync

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEntryEvidencePreservesExactIntegersAndReadsEarlyNumericRecords(t *testing.T) {
	entry := Entry{Path: "a", ID: "file-id", Size: 9007199254740993, Mtime: 1793949641612345678, MtimeUnit: "unix_nanoseconds"}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"size":"9007199254740993"`) || !strings.Contains(string(raw), `"mtime":"1793949641612345678"`) {
		t.Fatalf("precision lost: %s", raw)
	}
	var decoded Entry
	if err = json.Unmarshal(raw, &decoded); err != nil || decoded.Size != entry.Size || decoded.Mtime != entry.Mtime {
		t.Fatalf("roundtrip %+v %v", decoded, err)
	}
	if err = json.Unmarshal([]byte(`{"path":"a","id":"file-id","size":9007199254740993,"mtime":1793949641612345678}`), &decoded); err != nil || decoded.Size != entry.Size || decoded.Mtime != entry.Mtime {
		t.Fatalf("early numeric evidence lost %+v %v", decoded, err)
	}
}
