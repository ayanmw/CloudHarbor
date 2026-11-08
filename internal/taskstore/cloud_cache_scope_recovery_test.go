package taskstore

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteRecoveryInvalidatesPreparedCacheNamespaceAfterCredentialRotation(t *testing.T) {
	for _, status := range []string{"prepared", "unknown", "confirmed"} {
		t.Run(status, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			s, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			if err = s.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			account, _ := s.BoundAccountFingerprint()
			old, next, app := cacheScopeTestFingerprint("old"), cacheScopeTestFingerprint("next"), cacheScopeTestFingerprint("app")
			now := time.Now().UTC()
			namespace, err := s.ResolveCloudCacheScope(account, app, old, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ResolveCloudCacheScope(account, app, next, now); err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{namespace, next} {
				if err = s.UpsertCloudInventoryPage(scope, "seed", "/", nil, now, nil); err != nil {
					t.Fatal(err)
				}
			}
			evidence, _ := json.Marshal(map[string]string{"cache_namespace": namespace})
			op := RemoteOperation{ID: strings.Repeat("a", 32), AccountFingerprint: next, Kind: "delete", ObjectID: 1, SourcePath: "/file.bin", Status: "prepared", CreatedAtUTC: now, Evidence: evidence}
			if err = s.CreateRemoteOperation(op); err != nil {
				t.Fatal(err)
			}
			if status != "prepared" {
				if err = s.UpdateRemoteOperation(op.ID, status, json.RawMessage(`{"response":"recorded"}`), now); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ scope, status string }{{namespace, "stale"}, {next, "complete"}} {
				var actual string
				if err = s.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint=? AND path='/'`, tc.scope).Scan(&actual); err != nil || actual != tc.status {
					t.Fatalf("scope recovery status=%s want=%s err=%v", actual, tc.status, err)
				}
			}
			operations, err := s.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].AccountFingerprint != next {
				t.Fatalf("credential evidence changed: %+v err=%v", operations, err)
			}
		})
	}
}

func TestRemoteRecoveryRejectsInvalidExplicitCacheNamespaceWithoutPartialRecovery(t *testing.T) {
	for _, raw := range []string{`null`, `"malformed"`, `"` + strings.Repeat("b", 64) + `"`} {
		t.Run(raw, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			evidence := json.RawMessage(`{"cache_namespace":` + raw + `}`)
			op := RemoteOperation{ID: strings.Repeat("c", 32), AccountFingerprint: cacheScopeTestFingerprint("credential"), Kind: "delete", ObjectID: 1, SourcePath: "/file", Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: evidence}
			if err = s.CreateRemoteOperation(op); err != nil {
				t.Fatal(err)
			}
			if err = s.recoverRemoteOperations(); err == nil {
				t.Fatal("invalid explicit namespace accepted")
			}
			operations, err := s.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].Status != "prepared" {
				t.Fatalf("partial recovery: %+v err=%v", operations, err)
			}
			var events int
			if err = s.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=?`, op.ID).Scan(&events); err != nil || events != 1 {
				t.Fatalf("partial recovery events=%d err=%v", events, err)
			}
		})
	}
}

func TestRemoteRecoveryRejectsDamagedAssociationWithoutPublishingRecovery(t *testing.T) {
	for _, damage := range []string{"missing_fields", "wrong_account", "wrong_app", "bad_credential", "zero_time"} {
		t.Run(damage, func(t *testing.T) {

			s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			account, _ := s.BoundAccountFingerprint()
			now := time.Now().UTC()
			namespace, err := s.ResolveCloudCacheScope(account, cacheScopeTestFingerprint("app"), cacheScopeTestFingerprint("old"), now)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.UpsertCloudInventoryPage(namespace, "seed", "/", nil, now, nil); err != nil {
				t.Fatal(err)
			}
			evidence, _ := json.Marshal(map[string]string{"cache_namespace": namespace})
			op := RemoteOperation{ID: strings.Repeat("d", 32), AccountFingerprint: cacheScopeTestFingerprint("new"), Kind: "delete", ObjectID: 1, SourcePath: "/file", Status: "prepared", CreatedAtUTC: now, Evidence: evidence}
			if err = s.CreateRemoteOperation(op); err != nil {
				t.Fatal(err)
			}
			record := cloudCacheScopeRecord{AccountFingerprint: account, ApplicationFingerprint: cacheScopeTestFingerprint("app"), Namespace: namespace, FirstCredential: cacheScopeTestFingerprint("old"), CreatedAtUTC: now}
			switch damage {
			case "missing_fields":
				record = cloudCacheScopeRecord{Namespace: namespace}
			case "wrong_account":
				record.AccountFingerprint = cacheScopeTestFingerprint("other-account")
			case "wrong_app":
				record.ApplicationFingerprint = cacheScopeTestFingerprint("other-app")
			case "bad_credential":
				record.FirstCredential = "invalid"
			case "zero_time":
				record.CreatedAtUTC = time.Time{}
			}
			damaged, _ := json.Marshal(record)
			if _, err = s.db.Exec(`UPDATE store_meta SET value=? WHERE key LIKE 'cloud_cache_scope_v1:%'`, string(damaged)); err != nil {
				t.Fatal(err)
			}
			if err = s.recoverRemoteOperations(); err == nil {
				t.Fatal("damaged association accepted for recovery")
			}
			operations, err := s.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].Status != "prepared" {
				t.Fatalf("partial operation recovery: %+v err=%v", operations, err)
			}
			var status string
			if err = s.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint=? AND path='/'`, namespace).Scan(&status); err != nil || status != "complete" {
				t.Fatalf("partial cache recovery status=%s err=%v", status, err)
			}
		})
	}
}
