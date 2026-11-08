package baiduapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strconv"
	"strings"
	"time"
)

// UploadSessionPlan is private evidence, not authority to reuse a remote session.
// CredentialFingerprint identifies a token, not a stable Baidu account identity.
type UploadSessionPlan struct {
	PrecreateDigest string `json:"precreate_digest,omitempty"`
	// Zero preserves historical digests but cannot prove a missing create was never admitted.
	CreateAdmissionVersion int                 `json:"create_admission_version,omitempty"`
	PartAdmissionVersion   int                 `json:"part_admission_version"`
	UploadID               string              `json:"upload_id"`
	RemotePath             string              `json:"remote_path"`
	Size                   int64               `json:"size,string"`
	BlockSize              int64               `json:"block_size,string"`
	LocalSHA256            string              `json:"local_sha256"`
	BlockMD5               []string            `json:"block_md5"`
	NeededParts            []int               `json:"needed_parts"`
	CredentialFingerprint  string              `json:"credential_fingerprint"`
	AppKeyFingerprint      string              `json:"app_key_fingerprint"`
	ObservedAtUTC          time.Time           `json:"observed_at_utc"`
	AccountScope           *UploadAccountScope `json:"account_scope,omitempty"`
}
type UploadPartReceipt struct {
	PlanDigest    string    `json:"plan_digest"`
	Index         int       `json:"index"`
	MD5           string    `json:"md5"`
	ObservedAtUTC time.Time `json:"observed_at_utc"`
}

// UploadPartAdmission binds a planned request to its expected MD5. It is local
// intent before pacing/network admission, not proof that bytes reached Baidu.
type UploadPartAdmission UploadPartReceipt

// UploadCreateAdmission is local intent before pacing/network, not proof of delivery.
type UploadCreateAdmission struct {
	PlanDigest    string    `json:"plan_digest"`
	ObservedAtUTC time.Time `json:"observed_at_utc"`
}

// UploadCreateReceipt preserves a provider reply; independent metadata verification
// is still required before declaring the upload complete.
type UploadCreateReceipt struct {
	PlanDigest    string        `json:"plan_digest"`
	Receipt       UploadReceipt `json:"receipt"`
	ObservedAtUTC time.Time     `json:"observed_at_utc"`
}

func (a UploadCreateAdmission) Valid() bool {
	return sessionHex(a.PlanDigest, 32) && !a.ObservedAtUTC.IsZero() && a.ObservedAtUTC.Year() >= 1 && a.ObservedAtUTC.Year() <= 9999
}
func (r UploadCreateReceipt) Valid() bool {
	return (UploadCreateAdmission{r.PlanDigest, r.ObservedAtUTC}).Valid() && r.Receipt.Valid()
}
func (r UploadReceipt) Valid() bool {
	id, e := strconv.ParseInt(r.FsID, 10, 64)
	size, se := strconv.ParseInt(r.Size, 10, 64)
	return e == nil && id > 0 && strconv.FormatInt(id, 10) == r.FsID && se == nil && size >= 0 && strconv.FormatInt(size, 10) == r.Size && strings.HasPrefix(r.Path, "/") && r.Path != "/" && path.Clean(r.Path) == r.Path && !strings.ContainsAny(r.Path, "\\\x00")
}

type UploadSessionCheckpoint struct {
	CreateAdmission *UploadCreateAdmission `json:"create_admission,omitempty"`
	CreateReceipt   *UploadCreateReceipt   `json:"create_receipt,omitempty"`
	Plan            *UploadSessionPlan     `json:"plan,omitempty"`
	Part            *UploadPartReceipt     `json:"part,omitempty"`
	Admission       *UploadPartAdmission   `json:"part_admission,omitempty"`
}
type uploadSessionObserverKey struct{}

func WithUploadSessionObserver(ctx context.Context, observe func(UploadSessionCheckpoint) error) context.Context {
	return context.WithValue(ctx, uploadSessionObserverKey{}, observe)
}
func observeUploadSession(ctx context.Context, checkpoint UploadSessionCheckpoint) error {
	if observe, ok := ctx.Value(uploadSessionObserverKey{}).(func(UploadSessionCheckpoint) error); ok && observe != nil {
		return observe(checkpoint)
	}
	return nil
}
func sessionHex(value string, bytes int) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == bytes && value == strings.ToLower(value)
}
func (p UploadSessionPlan) Valid() bool {
	if p.PrecreateDigest != "" && !sessionHex(p.PrecreateDigest, 32) {
		return false
	}
	if p.AccountScope != nil && (!p.AccountScope.Valid() || p.AccountScope.CredentialFingerprint != p.CredentialFingerprint || p.AccountScope.AppKeyFingerprint != p.AppKeyFingerprint) {
		return false
	}
	if p.UploadID == "" || len(p.UploadID) > 8192 || strings.IndexFunc(p.UploadID, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || !strings.HasPrefix(p.RemotePath, "/") || p.RemotePath == "/" || path.Clean(p.RemotePath) != p.RemotePath || strings.ContainsAny(p.RemotePath, "\\\x00") || p.Size < 0 || p.Size > int64(UploadBlockSize)*10000 || p.BlockSize != UploadBlockSize || !sessionHex(p.LocalSHA256, 32) || !sessionHex(p.CredentialFingerprint, 32) || !sessionHex(p.AppKeyFingerprint, 32) || p.ObservedAtUTC.IsZero() {
		return false
	}
	if p.CreateAdmissionVersion < 0 || p.CreateAdmissionVersion > 1 || p.PartAdmissionVersion != 1 || p.NeededParts == nil || p.ObservedAtUTC.Year() < 1 || p.ObservedAtUTC.Year() > 9999 || len(p.BlockMD5) != max(1, int((p.Size+UploadBlockSize-1)/UploadBlockSize)) {
		return false
	}
	for _, hash := range p.BlockMD5 {
		if !sessionHex(hash, 16) {
			return false
		}
	}
	seen := map[int]bool{}
	for _, index := range p.NeededParts {
		if index < 0 || index >= len(p.BlockMD5) || seen[index] {
			return false
		}
		seen[index] = true
	}
	return true
}
func (p UploadSessionPlan) Digest() string {
	p.ObservedAtUTC = p.ObservedAtUTC.UTC()
	if p.AccountScope != nil {
		scope := *p.AccountScope
		scope.CheckedAtUTC = scope.CheckedAtUTC.UTC()
		p.AccountScope = &scope
	}
	body, _ := json.Marshal(p)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func (p UploadPartReceipt) Valid() bool {
	return sessionHex(p.PlanDigest, 32) && p.Index >= 0 && p.Index < 10000 && sessionHex(p.MD5, 16) && !p.ObservedAtUTC.IsZero() && p.ObservedAtUTC.Year() >= 1 && p.ObservedAtUTC.Year() <= 9999
}
