package baiduapi

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"
)

// CheckedAtUTC is a local authorization-gate observation, not a timestamp
// supplied by Baidu. The caller must obtain the stable account from its binding.
type UploadAccountScope struct {
	AccountFingerprint    string    `json:"account_fingerprint"`
	CredentialFingerprint string    `json:"credential_fingerprint"`
	AppKeyFingerprint     string    `json:"app_key_fingerprint"`
	CheckedAtUTC          time.Time `json:"authorization_gate_checked_at_utc"`
}

func (s UploadAccountScope) Valid() bool {
	// An empty configured key supplies no application binding for session reuse.
	emptyApp := sha256.Sum256(nil)
	return sessionHex(s.AccountFingerprint, 32) && sessionHex(s.CredentialFingerprint, 32) && sessionHex(s.AppKeyFingerprint, 32) && s.AppKeyFingerprint != hex.EncodeToString(emptyApp[:]) && !s.CheckedAtUTC.IsZero() && s.CheckedAtUTC.Year() >= 1 && s.CheckedAtUTC.Year() <= 9999
}

// ApplicationFingerprint identifies the configured app key; it does not prove
// the remote token issuer or continued validity of an upload session.
func (c *Client) ApplicationFingerprint() string {
	sum := sha256.Sum256([]byte(c.AppKey))
	return hex.EncodeToString(sum[:])
}

type uploadAccountScopeKey struct{}

func WithUploadAccountScope(ctx context.Context, scope UploadAccountScope) context.Context {
	return context.WithValue(ctx, uploadAccountScopeKey{}, scope)
}
func (c *Client) uploadAccountScope(ctx context.Context) (*UploadAccountScope, error) {
	scope, ok := ctx.Value(uploadAccountScopeKey{}).(UploadAccountScope)
	if !ok {
		return nil, nil
	}
	if !scope.Valid() || scope.CredentialFingerprint != c.Fingerprint() || scope.AppKeyFingerprint != c.ApplicationFingerprint() {
		return nil, errors.New("upload account scope does not match the current client")
	}
	scope.CheckedAtUTC = scope.CheckedAtUTC.UTC()
	return &scope, nil
}

// This compares two admission scopes, without proving session freshness or
// authorizing a network operation. Token rotation is distinct from account change.
func ValidateUploadSessionScope(plan UploadSessionPlan, current UploadAccountScope) error {
	if !plan.Valid() || plan.AccountScope == nil || !current.Valid() || plan.AccountScope.AccountFingerprint != current.AccountFingerprint || plan.AccountScope.AppKeyFingerprint != current.AppKeyFingerprint {
		return errors.New("upload session account/application scope requires verification")
	}
	return nil
}

// The caller owns the open handle and must keep it for any later transfer. This
// read-only check is not authorization to reuse a session or reopen another file.
func verifyUploadSessionPayload(ctx context.Context, f *os.File, plan UploadSessionPlan, checkpoint func() error) error {
	mismatch := errors.New("staged upload content no longer matches the session manifest")
	if err := ctx.Err(); err != nil {
		return err
	}
	if !plan.Valid() {
		return mismatch
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != plan.Size {
		return mismatch
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	buffer := make([]byte, UploadBlockSize)
	index := 0
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if checkpoint != nil {
			if err = checkpoint(); err != nil {
				return err
			}
		}
		n, readErr := io.ReadFull(f, buffer)
		if n > 0 || index == 0 {
			if index >= len(plan.BlockMD5) {
				return mismatch
			}
			sum := md5.Sum(buffer[:n])
			if hex.EncodeToString(sum[:]) != plan.BlockMD5[index] {
				return mismatch
			}
			_, _ = hash.Write(buffer[:n])
			total += int64(n)
			index++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if total != plan.Size || index != len(plan.BlockMD5) || hex.EncodeToString(hash.Sum(nil)) != plan.LocalSHA256 {
		return mismatch
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}
