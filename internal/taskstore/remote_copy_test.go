package taskstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCopyObservationRequiresMatchingConfirmedCopy(t *testing.T) {
	store, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := CloudInventoryEntry{FsID: 91, Path: "/source/a", Name: "a", Size: 7}
	target := CloudInventoryEntry{FsID: 9007199254740995, Path: "/archive/b", Name: "b", Size: 7}
	now := time.Now().UTC()
	id := strings.Repeat("a", 32)
	if err = store.CreateRemoteOperation(RemoteOperation{ID: id, AccountFingerprint: "account", Kind: "copy", ObjectID: 91, SourcePath: source.Path, DestinationPath: target.Path, Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordRemoteCopyObservation(id, source, target, now, now); err == nil {
		t.Fatal("prepared copy claimed observed completion")
	}
	if err = store.UpdateRemoteOperation(id, "confirmed", json.RawMessage(`{"provider_response":"synchronous_success"}`), now); err != nil {
		t.Fatal(err)
	}
	wrong := target
	wrong.Path = "/wrong/b"
	if err = store.RecordRemoteCopyObservation(id, source, wrong, now, now); err == nil {
		t.Fatal("wrong destination accepted")
	}
	wrong = target
	wrong.FsID = source.FsID
	if err = store.RecordRemoteCopyObservation(id, source, wrong, now, now); err == nil {
		t.Fatal("source id accepted as copy id")
	}
	if err = store.RecordRemoteCopyObservation(id, source, target, now, now); err != nil {
		t.Fatal(err)
	}
	events, _, _, err := store.RemoteOperationEventPage(id, RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || !strings.Contains(string(events[0].Evidence), `"target_fs_id":"9007199254740995"`) || events[0].ResponseObservedAtUTC == nil {
		t.Fatalf("unexpected copy evidence: %+v", events)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || string(operations[0].Evidence) != `{"provider_response":"synchronous_success"}` {
		t.Fatalf("observation overwrote receipt: %+v err=%v", operations, err)
	}
}
