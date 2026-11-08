package taskstore

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"
)

func cacheScopeTestFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestCloudCacheScopePersistsVerifiedAssociationAcrossCredentialsAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	account, err := s.BoundAccountFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	app, old, next := cacheScopeTestFingerprint("app"), cacheScopeTestFingerprint("old"), cacheScopeTestFingerprint("next")
	now := time.Now().UTC()
	first, err := s.ResolveCloudCacheScope(account, app, old, now)
	if err != nil || first != old {
		t.Fatalf("initial legacy namespace=%q err=%v", first, err)
	}
	var originalRecord, renewedRecord string
	const scopeRecordSQL = `SELECT value FROM store_meta WHERE key LIKE 'cloud_cache_scope_v1:%'`
	if err = s.db.QueryRow(scopeRecordSQL).Scan(&originalRecord); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage(first, "known", "/", []CloudInventoryEntry{{FsID: 1, Path: "/kept", Name: "kept", Size: 9}}, now, nil); err != nil {
		t.Fatal(err)
	}
	second, err := s.ResolveCloudCacheScope(account, app, next, now.Add(time.Hour))
	if err != nil || second != first {
		t.Fatalf("renewal namespace=%q err=%v", second, err)
	}
	if err = s.db.QueryRow(scopeRecordSQL).Scan(&renewedRecord); err != nil || originalRecord != renewedRecord {
		t.Fatalf("renewal rewrote initial credential/time evidence: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.ResolveCloudCacheScope(account, app, next, now.Add(2*time.Hour))
	if err != nil || third != first {
		t.Fatalf("reopen namespace=%q err=%v", third, err)
	}
	rows, _, count, err := s.ListCloudInventory(third, "/", 0, 10)
	if err != nil || count != 1 || len(rows) != 1 || rows[0].Size != 9 {
		t.Fatalf("durable cache rows=%+v count=%d err=%v", rows, count, err)
	}
}

func TestCloudCacheScopeSeparatesApplicationsEvenForSameCredential(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	account, _ := s.BoundAccountFingerprint()
	token, now := cacheScopeTestFingerprint("shared"), time.Now().UTC()
	one, err := s.ResolveCloudCacheScope(account, cacheScopeTestFingerprint("app-one"), token, now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.ResolveCloudCacheScope(account, cacheScopeTestFingerprint("app-two"), token, now)
	if err != nil || one == two {
		t.Fatalf("applications share namespace: %q %q err=%v", one, two, err)
	}
}

func TestCloudCacheScopeRejectsUnboundMismatchedOrInvalidIdentity(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fp := cacheScopeTestFingerprint("value")
	if _, err = s.ResolveCloudCacheScope(fp, fp, fp, time.Now().UTC()); err == nil {
		t.Fatal("unbound scope accepted")
	}
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	account, _ := s.BoundAccountFingerprint()
	for _, tc := range []struct {
		account, app, credential string
		at                       time.Time
	}{
		{fp, fp, fp, time.Now().UTC()},
		{account, "", fp, time.Now().UTC()},
		{account, fp, "malformed", time.Now().UTC()},
		{account, fp, fp, time.Time{}},
	} {
		if _, err = s.ResolveCloudCacheScope(tc.account, tc.app, tc.credential, tc.at); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM store_meta WHERE key LIKE 'cloud_cache_scope_v1:%' OR key LIKE 'cloud_cache_namespace_v1:%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid input wrote mapping count=%d err=%v", count, err)
	}
}

func TestCloudCacheScopeRollsBackAssociationWhenOwnershipWriteFails(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	account, _ := s.BoundAccountFingerprint()
	if _, err = s.db.Exec(`CREATE TRIGGER test_reject_cache_owner BEFORE INSERT ON store_meta WHEN NEW.key LIKE 'cloud_cache_namespace_v1:%' BEGIN SELECT RAISE(ABORT,'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}
	fp := cacheScopeTestFingerprint("value")
	if _, err = s.ResolveCloudCacheScope(account, fp, fp, time.Now().UTC()); err == nil {
		t.Fatal("ownership write failure ignored")
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM store_meta WHERE key LIKE 'cloud_cache_scope_v1:%' OR key LIKE 'cloud_cache_namespace_v1:%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial mapping count=%d err=%v", count, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER test_reject_cache_owner`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveCloudCacheScope(account, fp, fp, time.Now().UTC()); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestCloudCacheScopeRejectsDamagedAssociationWithoutRepairingEvidence(t *testing.T) {
	for _, damage := range []string{"record", "missing_owner", "wrong_owner", "orphan_owner"} {
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
			fp, now := cacheScopeTestFingerprint("value"), time.Now().UTC()
			if _, err = s.ResolveCloudCacheScope(account, fp, fp, now); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "record":
				_, err = s.db.Exec(`UPDATE store_meta SET value='invalid' WHERE key LIKE 'cloud_cache_scope_v1:%'`)
			case "missing_owner":
				_, err = s.db.Exec(`DELETE FROM store_meta WHERE key LIKE 'cloud_cache_namespace_v1:%'`)
			case "wrong_owner":
				_, err = s.db.Exec(`UPDATE store_meta SET value='invalid' WHERE key LIKE 'cloud_cache_namespace_v1:%'`)
			case "orphan_owner":
				_, err = s.db.Exec(`DELETE FROM store_meta WHERE key LIKE 'cloud_cache_scope_v1:%'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			var before, after string
			const snapshotSQL = `SELECT coalesce(group_concat(key||'='||value,'|'),'') FROM (SELECT key,value FROM store_meta WHERE key LIKE 'cloud_cache_scope_v1:%' OR key LIKE 'cloud_cache_namespace_v1:%' ORDER BY key)`
			if err = s.db.QueryRow(snapshotSQL).Scan(&before); err != nil {
				t.Fatal(err)
			}
			for _, credential := range []string{fp, cacheScopeTestFingerprint("renewed-value")} {
				if _, err = s.ResolveCloudCacheScope(account, fp, credential, now.Add(time.Hour)); err == nil {
					t.Fatal("damaged association accepted with original or renewed credential")
				}
			}
			if err = s.db.QueryRow(snapshotSQL).Scan(&after); err != nil || before != after {
				t.Fatalf("damaged evidence changed err=%v", err)
			}
		})
	}
}
