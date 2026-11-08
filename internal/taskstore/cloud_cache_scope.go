package taskstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type cloudCacheScopeRecord struct {
	AccountFingerprint     string    `json:"account_fingerprint"`
	ApplicationFingerprint string    `json:"application_fingerprint"`
	Namespace              string    `json:"namespace"`
	FirstCredential        string    `json:"first_credential_fingerprint"`
	CreatedAtUTC           time.Time `json:"created_at_utc"`
}

func cloudCacheFingerprintValid(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ResolveCloudCacheScope preserves the first verified credential's namespace
// across renewals without rewriting inventory, hashes or append-only history.
// The caller must have verified that the credential belongs to this account and
// application. This association is not an authorization or a provider receipt.
func (s *Store) ResolveCloudCacheScope(account, application, credential string, observedAt time.Time) (string, error) {
	if !cloudCacheFingerprintValid(account) || !cloudCacheFingerprintValid(application) || !cloudCacheFingerprintValid(credential) || observedAt.IsZero() {
		return "", fmt.Errorf("invalid cloud cache scope identity or observation time")
	}
	bound, err := s.BoundAccountFingerprint()
	if err != nil {
		return "", err
	}
	if bound != account {
		return "", ErrAccountMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", os.ErrClosed
	}
	identityHash := sha256.Sum256([]byte("cloud-cache-scope-v1\x00" + account + "\x00" + application))
	identity := hex.EncodeToString(identityHash[:])
	key := "cloud_cache_scope_v1:" + identity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var body string
	err = tx.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key=?`, key).Scan(&body)
	if err == nil {
		var record cloudCacheScopeRecord
		if json.Unmarshal([]byte(body), &record) != nil || record.AccountFingerprint != account || record.ApplicationFingerprint != application || !cloudCacheFingerprintValid(record.Namespace) || !cloudCacheFingerprintValid(record.FirstCredential) || record.CreatedAtUTC.IsZero() {
			return "", fmt.Errorf("invalid persisted cloud cache scope association")
		}
		var owner string
		if err = tx.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key=?`, "cloud_cache_namespace_v1:"+record.Namespace).Scan(&owner); err != nil {
			return "", fmt.Errorf("cloud cache namespace ownership unavailable: %w", err)
		}
		if owner != identity {
			return "", fmt.Errorf("cloud cache namespace ownership mismatch")
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return record.Namespace, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var ownedNamespaces int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM store_meta WHERE key LIKE 'cloud_cache_namespace_v1:%' AND value=?`, identity).Scan(&ownedNamespaces); err != nil {
		return "", err
	}
	if ownedNamespaces != 0 {
		// Check by stable identity, not only the supplied token: a renewal must
		// not hide a lost association by creating a second namespace.
		return "", fmt.Errorf("cloud cache namespace has an owner without its association")
	}
	namespace := credential
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key=?`, "cloud_cache_namespace_v1:"+namespace).Scan(&owner)
	if err == nil && !cloudCacheFingerprintValid(owner) {
		return "", fmt.Errorf("invalid cloud cache namespace ownership")
	}
	if err == nil && owner != identity {
		// An application change must not inherit another application's cache even
		// if a caller supplies the same credential fingerprint.
		namespace = identity
		err = tx.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key=?`, "cloud_cache_namespace_v1:"+namespace).Scan(&owner)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil {
		// A valid transaction creates both keys. A surviving owner without its
		// association is inconsistent, so do not silently reconstruct evidence.
		return "", fmt.Errorf("cloud cache namespace has an owner without this association")
	}
	record := cloudCacheScopeRecord{AccountFingerprint: account, ApplicationFingerprint: application, Namespace: namespace, FirstCredential: credential, CreatedAtUTC: observedAt.UTC()}
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO store_meta(key,value) VALUES(?,?)`, key, string(encoded)); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO store_meta(key,value) VALUES(?,?)`, "cloud_cache_namespace_v1:"+namespace, identity); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return namespace, nil
}

// Prepared intent freezes the cache namespace independently of the sending
// credential. Old operations without this field retain their original scope.
func (s *Store) remoteOperationCacheNamespaceTx(tx *sql.Tx, operationID, credential string) (string, error) {
	var evidence []byte
	err := tx.QueryRow(`SELECT evidence_json FROM remote_operation_events WHERE operation_id=? AND status='prepared' ORDER BY sequence LIMIT 1`, operationID).Scan(&evidence)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(evidence, &fields) != nil {
		return credential, nil // Legacy evidence could be any valid JSON value.
	}
	raw, present := fields["cache_namespace"]
	if !present {
		return credential, nil
	}
	var namespace string
	if json.Unmarshal(raw, &namespace) != nil || !cloudCacheFingerprintValid(namespace) {
		return "", fmt.Errorf("invalid prepared cloud cache namespace")
	}
	if namespace == credential {
		return namespace, nil // Legacy/unbound clients use their exact token scope.
	}
	var owner, body string
	if err = tx.QueryRow(`SELECT value FROM store_meta WHERE key=?`, "cloud_cache_namespace_v1:"+namespace).Scan(&owner); err != nil {
		return "", fmt.Errorf("prepared cloud cache ownership unavailable: %w", err)
	}
	if !cloudCacheFingerprintValid(owner) {
		return "", fmt.Errorf("invalid prepared cloud cache owner")
	}
	if err = tx.QueryRow(`SELECT value FROM store_meta WHERE key=?`, "cloud_cache_scope_v1:"+owner).Scan(&body); err != nil {
		return "", fmt.Errorf("prepared cloud cache association unavailable: %w", err)
	}
	var record cloudCacheScopeRecord
	if json.Unmarshal([]byte(body), &record) != nil || record.Namespace != namespace || !cloudCacheFingerprintValid(record.AccountFingerprint) || !cloudCacheFingerprintValid(record.ApplicationFingerprint) || !cloudCacheFingerprintValid(record.FirstCredential) || record.CreatedAtUTC.IsZero() {
		return "", fmt.Errorf("prepared cloud cache association mismatch")
	}
	bound, err := s.boundAccountFingerprintLocked()
	if err != nil {
		return "", err
	}
	identityHash := sha256.Sum256([]byte("cloud-cache-scope-v1\x00" + record.AccountFingerprint + "\x00" + record.ApplicationFingerprint))
	if bound != record.AccountFingerprint || hex.EncodeToString(identityHash[:]) != owner {
		return "", fmt.Errorf("prepared cloud cache account or application ownership mismatch")
	}
	return namespace, nil
}
