package baiduapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
	"time"
)

// UploadPrecreateIntent is recorded before a request that may complete a rapid
// upload. It has no uploadid: the provider has not returned a session yet.
type UploadPrecreateIntent struct {
	RemotePath            string              `json:"remote_path"`
	Size                  int64               `json:"size,string"`
	LocalSHA256           string              `json:"local_sha256"`
	BlockMD5              []string            `json:"block_md5"`
	CredentialFingerprint string              `json:"credential_fingerprint"`
	AppKeyFingerprint     string              `json:"app_key_fingerprint"`
	AccountScope          *UploadAccountScope `json:"account_scope,omitempty"`
	ObservedAtUTC         time.Time           `json:"observed_at_utc"`
}

func (p UploadPrecreateIntent) Valid() bool {
	if p.RemotePath == "/" || !strings.HasPrefix(p.RemotePath, "/") || path.Clean(p.RemotePath) != p.RemotePath || strings.ContainsAny(p.RemotePath, "\\\x00") || p.Size < 0 || p.Size > int64(UploadBlockSize)*10000 || !sessionHex(p.LocalSHA256, 32) || !sessionHex(p.CredentialFingerprint, 32) || !sessionHex(p.AppKeyFingerprint, 32) || p.ObservedAtUTC.IsZero() || p.ObservedAtUTC.Year() < 1 || p.ObservedAtUTC.Year() > 9999 || len(p.BlockMD5) != max(1, int((p.Size+UploadBlockSize-1)/UploadBlockSize)) {
		return false
	}
	if p.AccountScope != nil && (!p.AccountScope.Valid() || p.AccountScope.CredentialFingerprint != p.CredentialFingerprint || p.AccountScope.AppKeyFingerprint != p.AppKeyFingerprint) {
		return false
	}
	for _, h := range p.BlockMD5 {
		if !sessionHex(h, 16) {
			return false
		}
	}
	return true
}
func (p UploadPrecreateIntent) Digest() string {
	p.ObservedAtUTC = p.ObservedAtUTC.UTC()
	if p.AccountScope != nil {
		scope := *p.AccountScope
		scope.CheckedAtUTC = scope.CheckedAtUTC.UTC()
		p.AccountScope = &scope
	}
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RapidReceipt uses PlanDigest to identify the durable precreate intent, not a
// multipart session. Its timestamp records local observation of the reply.
type UploadPrecreateCheckpoint struct {
	Intent       *UploadPrecreateIntent `json:"intent,omitempty"`
	RapidReceipt *UploadCreateReceipt   `json:"rapid_receipt,omitempty"`
}
type uploadPrecreateObserverKey struct{}

func WithUploadPrecreateObserver(ctx context.Context, observer func(UploadPrecreateCheckpoint) error) context.Context {
	return context.WithValue(ctx, uploadPrecreateObserverKey{}, observer)
}
func observeUploadPrecreate(ctx context.Context, c UploadPrecreateCheckpoint) error {
	if observer, ok := ctx.Value(uploadPrecreateObserverKey{}).(func(UploadPrecreateCheckpoint) error); ok && observer != nil {
		return observer(c)
	}
	return nil
}
