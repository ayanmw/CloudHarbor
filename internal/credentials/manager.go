// Package credentials owns persisted OAuth credentials, account binding and
// renewal. Public status values never contain bearer or refresh tokens.
package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/oauth"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type Store interface {
	LoadCredentials() ([]byte, error)
	SaveCredentials([]byte) error
	BindAccount(string) error
}
type Provider interface {
	Refresh(context.Context, string, string, string) (*oauth.TokenResponse, error)
	Account(context.Context, string, string) (string, error)
}
type BaiduProvider struct{ RequestPacer baiduapi.RequestPacer }

func (BaiduProvider) Refresh(ctx context.Context, key, secret, refresh string) (*oauth.TokenResponse, error) {
	return oauth.RefreshTokenContext(ctx, key, secret, refresh)
}
func (p BaiduProvider) Account(ctx context.Context, access, key string) (string, error) {
	return baiduapi.NewClient(access, key).WithRequestPacer(p.RequestPacer).WithContext(ctx).UserID()
}

type Options struct{ AppKey, SecretKey, AccessToken, RefreshToken string }
type Record struct {
	Version      int       `json:"version"`
	AppKey       string    `json:"app_key"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	IssuedAt     time.Time `json:"issued_at,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Generation   int64     `json:"generation"`
}
type Status struct {
	State        string     `json:"state"`
	Ready        bool       `json:"ready"`
	HasRefresh   bool       `json:"has_refresh_token"`
	CanRefresh   bool       `json:"can_refresh"`
	AccountBound bool       `json:"account_bound"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	NextAttempt  *time.Time `json:"next_attempt,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Update is an in-process callback, not an HTTP response DTO.
type Update struct {
	Ready       bool
	AccessToken string
	Fingerprint string
	ExpiresAt   time.Time
}
type operationError struct {
	code  string
	cause error
}

func (e *operationError) Error() string { return "credential operation failed: " + e.code }
func (e *operationError) Unwrap() error { return e.cause }

var ErrNotConfigured = errors.New("Baidu credentials not configured")
var ErrRenewalUnavailable = errors.New("Baidu credentials require authorization")
var ErrClosed = errors.New("credential manager closed")

type Manager struct {
	store                                  Store
	options                                Options
	provider                               Provider
	hook                                   func(Update)
	gate                                   chan struct{}
	wake                                   chan struct{}
	emitMu                                 sync.Mutex
	mu                                     sync.RWMutex
	record                                 Record
	active                                 string
	activeExpiry                           time.Time
	state, lastCode                        string
	ready, force, blocked, closed, running bool
	nextAttempt, updated                   time.Time
	failures                               int
	lastErr                                error
	pendingSave, pendingVerify             *Record
	runCtx                                 context.Context
}

func Open(store Store, options Options, provider Provider, hook func(Update)) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("credential store required")
	}
	if provider == nil {
		provider = BaiduProvider{}
	}
	m := &Manager{store: store, options: options, provider: provider, hook: hook, gate: make(chan struct{}, 1), wake: make(chan struct{}, 1), state: "unconfigured", updated: time.Now()}
	m.gate <- struct{}{}
	body, err := store.LoadCredentials()
	if err != nil {
		return nil, &operationError{code: "storage_error", cause: err}
	}
	if body != nil {
		if json.Unmarshal(body, &m.record) != nil || !validRecord(m.record) {
			return nil, fmt.Errorf("invalid saved credential record")
		}
		if m.record.AppKey != "" && options.AppKey != "" && m.record.AppKey != options.AppKey {
			return nil, fmt.Errorf("credential store belongs to a different Baidu application")
		}
		if m.options.AppKey == "" {
			m.options.AppKey = m.record.AppKey
		}
		if m.record.AccountID != "" {
			if err = store.BindAccount(m.record.AccountID); err != nil {
				return nil, &operationError{code: "account_binding_error", cause: err}
			}
		}
	} else {
		m.record = Record{Version: 1, AppKey: options.AppKey, AccessToken: options.AccessToken, RefreshToken: options.RefreshToken}
		if !validRecord(m.record) && (options.AccessToken != "" || options.RefreshToken != "") {
			return nil, fmt.Errorf("invalid bootstrap credentials")
		}
	}
	if m.record.AccessToken != "" || m.record.RefreshToken != "" {
		m.state = "verification_required"
	}
	m.emit()
	return m, nil
}
func validRecord(record Record) bool {
	if record.Version != 1 || (record.AccessToken == "" && record.RefreshToken == "") || len(record.AccessToken) > 16384 || len(record.RefreshToken) > 16384 || record.Generation < 0 || record.Generation > 1<<60 {
		return false
	}
	if !record.ExpiresAt.IsZero() {
		ttl := record.ExpiresAt.Sub(record.IssuedAt)
		if record.IssuedAt.IsZero() || ttl <= 0 || ttl > 366*24*time.Hour {
			return false
		}
	}
	return true
}
func fingerprint(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ready := m.ready && (m.activeExpiry.IsZero() || time.Now().Before(m.activeExpiry))
	status := Status{State: m.state, Ready: ready, HasRefresh: m.record.RefreshToken != "", CanRefresh: m.canRefresh(), AccountBound: m.record.AccountID != "", LastError: m.lastCode, UpdatedAt: m.updated}
	if !m.record.ExpiresAt.IsZero() {
		value := m.record.ExpiresAt
		status.ExpiresAt = &value
	}
	if !m.nextAttempt.IsZero() {
		value := m.nextAttempt
		status.NextAttempt = &value
	}
	return status
}
func (m *Manager) canRefresh() bool {
	return m.options.AppKey != "" && m.options.SecretKey != "" && m.record.RefreshToken != ""
}
func (m *Manager) emit() {
	if m.hook == nil {
		return
	}
	// Capture the latest state after serialization. A delayed rejection callback
	// cannot overwrite a newer successful application in the downloader.
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.mu.RLock()
	update := Update{Ready: m.ready && (m.activeExpiry.IsZero() || time.Now().Before(m.activeExpiry)), AccessToken: m.active, Fingerprint: fingerprint(m.active), ExpiresAt: m.activeExpiry}
	m.mu.RUnlock()
	m.hook(update)
}
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.gate:
	}
	m.mu.RLock()
	closed := m.closed
	runCtx := m.runCtx
	m.mu.RUnlock()
	if closed {
		m.release()
		return ErrClosed
	}
	if runCtx != nil && runCtx.Err() != nil {
		m.release()
		return runCtx.Err()
	}
	return nil
}
func (m *Manager) release() { m.gate <- struct{}{} }

// ConfigureApp serializes first-time application-key activation with every
// credential operation. persist runs while the credential gate is held so no
// refresh or account verification can observe a partial key transition.
func (m *Manager) ConfigureApp(ctx context.Context, appKey, secretKey string, persist func() error) error {
	if strings.TrimSpace(appKey) == "" || strings.TrimSpace(secretKey) == "" || len(appKey) > 4096 || len(secretKey) > 4096 || persist == nil {
		return fmt.Errorf("invalid Baidu application config")
	}
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	m.mu.RLock()
	currentKey := m.options.AppKey
	boundKey := m.record.AppKey
	m.mu.RUnlock()
	if (currentKey != "" && currentKey != appKey) || (boundKey != "" && boundKey != appKey) {
		return fmt.Errorf("credential store belongs to a different Baidu application")
	}
	if err := persist(); err != nil {
		return err
	}
	m.mu.Lock()
	m.options.AppKey = appKey
	m.options.SecretKey = secretKey
	if m.record.AppKey == "" {
		m.record.AppKey = appKey
	}
	m.updated = time.Now()
	m.mu.Unlock()
	return nil
}

// Wake schedules credential work after all related runtime components have
// completed their application-key switch.
func (m *Manager) Wake() { m.signal() }

func (m *Manager) failure(err error, code string, keepOld, retry, block bool) error {
	failure := &operationError{code: code, cause: err}
	m.mu.Lock()
	// Failure may preserve existing admission, never grant it again. In
	// particular, a refresh network failure cannot revive a rejected token.
	m.ready = keepOld && m.ready && m.active != "" && (m.activeExpiry.IsZero() || time.Now().Before(m.activeExpiry))
	m.state = code
	m.lastCode = code
	m.lastErr = failure
	m.blocked = block
	m.updated = time.Now()
	m.nextAttempt = time.Time{}
	if retry {
		m.failures++
		delay := 5 * time.Second
		for i := 1; i < m.failures && delay < 5*time.Minute; i++ {
			delay *= 2
		}
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		m.nextAttempt = time.Now().Add(delay)
	}
	m.mu.Unlock()
	m.emit()
	// Manual HTTP operations can change the deadline while Run is sleeping.
	m.signal()
	return failure
}
func transient(err error) bool {
	return oauth.IsTransient(err) || baiduapi.IsRetryable(err) || errors.Is(err, context.DeadlineExceeded)
}
func (m *Manager) save(record Record) error {
	body, err := json.Marshal(record)
	if err == nil {
		err = m.store.SaveCredentials(body)
	}
	if err != nil {
		copy := record
		m.mu.Lock()
		m.pendingSave = &copy
		m.mu.Unlock()
		return m.failure(err, "storage_error", false, true, false)
	}
	m.mu.Lock()
	m.record = record
	m.pendingSave = nil
	m.ready = false
	m.state = "verification_required"
	m.updated = time.Now()
	m.mu.Unlock()
	m.emit()
	m.signal()
	return nil
}
func (m *Manager) publish(record Record) {
	m.mu.Lock()
	m.record = record
	m.active = record.AccessToken
	m.activeExpiry = record.ExpiresAt
	m.ready = true
	m.force = false
	m.blocked = false
	m.failures = 0
	m.nextAttempt = time.Time{}
	m.pendingSave = nil
	m.pendingVerify = nil
	m.lastCode = ""
	m.lastErr = nil
	m.state = "ready"
	m.updated = time.Now()
	if record.ExpiresAt.IsZero() {
		m.state = "expiry_unknown"
	}
	m.mu.Unlock()
	m.emit()
	m.signal()
}
func (m *Manager) verify(ctx context.Context, record Record, manual bool) error {
	id, err := m.provider.Account(ctx, record.AccessToken, m.options.AppKey)
	if err != nil {
		// Cancelling one operation must not disable subsequent renewal. A rotated
		// pair is already durable and can be verified by the next Ensure call.
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return ctx.Err()
		}
		if baiduapi.IsAuthenticationError(err) && !manual {
			m.mu.Lock()
			m.force = true
			canRefresh := m.canRefresh()
			m.mu.Unlock()
			if canRefresh {
				return m.failure(err, "token_rejected", false, true, false)
			}
		}
		if manual && transient(err) {
			copy := record
			m.mu.Lock()
			m.pendingVerify = &copy
			m.mu.Unlock()
		}
		return m.failure(err, "account_verification_failed", manual, transient(err), !manual && !transient(err))
	}
	if record.AccountID != "" && record.AccountID != id {
		return m.failure(taskstore.ErrAccountMismatch, "account_mismatch", manual, false, !manual)
	}
	if err = m.store.BindAccount(id); err != nil {
		if errors.Is(err, taskstore.ErrAccountMismatch) {
			return m.failure(err, "account_mismatch", manual, false, !manual)
		}
		if manual {
			copy := record
			m.mu.Lock()
			m.pendingVerify = &copy
			m.mu.Unlock()
		}
		return m.failure(err, "storage_error", false, true, false)
	}
	record.AccountID = id
	record.AppKey = m.options.AppKey
	if err = m.save(record); err != nil {
		return err
	}
	m.publish(record)
	return nil
}
func (m *Manager) newRecord(token *oauth.TokenResponse) (Record, error) {
	if token == nil || token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 || token.ExpiresIn <= 0 || token.ExpiresIn > 366*24*60*60 {
		return Record{}, oauth.ErrInvalidResponse
	}
	m.mu.RLock()
	previous := m.record
	m.mu.RUnlock()
	now := time.Now()
	return Record{Version: 1, AppKey: m.options.AppKey, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, IssuedAt: now, ExpiresAt: now.Add(time.Duration(token.ExpiresIn) * time.Second), Generation: previous.Generation + 1, AccountID: previous.AccountID}, nil
}
func (m *Manager) Apply(ctx context.Context, token *oauth.TokenResponse) error {
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	record, err := m.newRecord(token)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.pendingVerify = nil
	m.mu.Unlock()
	return m.verify(ctx, record, true)
}
func (m *Manager) refresh(ctx context.Context) error {
	m.mu.RLock()
	record := m.record
	canRefresh := m.canRefresh()
	m.mu.RUnlock()
	if !canRefresh {
		return m.failure(ErrRenewalUnavailable, "reauthorization_required", true, false, true)
	}
	token, err := m.provider.Refresh(ctx, m.options.AppKey, m.options.SecretKey, record.RefreshToken)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return ctx.Err()
		}
		retry := transient(err)
		if retry {
			// A manual refresh can fail before the normal renewal window. Keep
			// the intent as well as its deadline so Ensure actually retries it.
			m.mu.Lock()
			m.force = true
			m.mu.Unlock()
		}
		return m.failure(err, "refresh_failed", true, retry, !retry)
	}
	candidate, err := m.newRecord(token)
	if err != nil {
		return m.failure(err, "invalid_response", false, false, true)
	}
	if candidate.RefreshToken == "" {
		candidate.RefreshToken = record.RefreshToken
	}
	// A refresh may rotate the refresh token. Save the pair before a separate
	// account request, otherwise a transient lookup failure could lose it.
	m.mu.Lock()
	m.ready = false
	m.mu.Unlock()
	m.emit()
	if err = m.save(candidate); err != nil {
		return err
	}
	return m.verify(ctx, candidate, false)
}
func (m *Manager) Refresh(ctx context.Context) error {
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	return m.refresh(ctx)
}
func lead(record Record) time.Duration {
	ttl := record.ExpiresAt.Sub(record.IssuedAt)
	value := ttl / 10
	if value > 5*time.Minute {
		return 5 * time.Minute
	}
	return value
}
func (m *Manager) Ensure(ctx context.Context) error {
	if err := m.acquire(ctx); err != nil {
		return err
	}
	defer m.release()
	m.mu.Lock()
	expiredActive := !m.activeExpiry.IsZero() && !time.Now().Before(m.activeExpiry)
	changed := m.ready && expiredActive
	if changed {
		m.ready = false
	}
	record := m.record
	force, blocked, ready := m.force, m.blocked, m.ready
	next, lastErr := m.nextAttempt, m.lastErr
	pendingSave, pendingVerify := m.pendingSave, m.pendingVerify
	canRefresh := m.canRefresh()
	m.mu.Unlock()
	if changed {
		m.emit()
	}
	if blocked {
		return lastErr
	}
	if time.Now().Before(next) {
		return lastErr
	}
	if pendingSave != nil {
		if err := m.save(*pendingSave); err != nil {
			return err
		}
		return m.verify(ctx, *pendingSave, false)
	}
	if pendingVerify != nil {
		return m.verify(ctx, *pendingVerify, true)
	}
	if record.AccessToken == "" && record.RefreshToken == "" {
		return m.failure(ErrNotConfigured, "unconfigured", false, false, true)
	}
	expired := !record.ExpiresAt.IsZero() && !time.Now().Before(record.ExpiresAt)
	due := !record.ExpiresAt.IsZero() && !time.Now().Before(record.ExpiresAt.Add(-lead(record)))
	needRefresh := force || record.AccessToken == "" || due || (record.ExpiresAt.IsZero() && canRefresh)
	if needRefresh && canRefresh {
		return m.refresh(ctx)
	}
	if force || expired || record.AccessToken == "" {
		return m.failure(ErrRenewalUnavailable, "reauthorization_required", false, false, true)
	}
	if ready {
		return nil
	}
	return m.verify(ctx, record, false)
}

// Only a rejection from the active token can force renewal. Older workers may
// report a previous token's error after a newer token has already been applied.
func (m *Manager) NotifyRejected(tokenFingerprint string) {
	m.mu.Lock()
	if m.closed || tokenFingerprint == "" || tokenFingerprint != fingerprint(m.active) {
		m.mu.Unlock()
		return
	}
	if m.force && !m.ready {
		m.mu.Unlock()
		return
	}
	// A scheduled refresh retry may still allow the old token. An actual
	// rejection must revoke it immediately without bypassing existing backoff.
	if !m.force {
		m.nextAttempt = time.Time{}
	}
	m.force = true
	m.ready = false
	m.blocked = false
	m.state = "token_rejected"
	m.lastCode = "token_rejected"
	m.updated = time.Now()
	m.mu.Unlock()
	m.emit()
	m.signal()
}
func (m *Manager) waitDuration() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.nextAttempt.IsZero() {
		at := m.nextAttempt
		// Expiry revokes admission even while refresh remains in backoff.
		if m.ready && !m.activeExpiry.IsZero() && m.activeExpiry.Before(at) {
			at = m.activeExpiry
		}
		value := time.Until(at)
		if value < 100*time.Millisecond {
			return 100 * time.Millisecond
		}
		return value
	}
	if m.ready && !m.record.ExpiresAt.IsZero() {
		at := m.record.ExpiresAt
		if m.canRefresh() && !m.blocked {
			at = at.Add(-lead(m.record))
		}
		value := time.Until(at)
		if value < time.Second {
			return time.Second
		}
		return value
	}
	return time.Minute
}
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	if m.running || m.closed {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.runCtx = ctx
	m.mu.Unlock()
	defer func() {
		<-m.gate
		m.mu.Lock()
		m.closed = true
		m.ready = false
		m.state = "closed"
		m.mu.Unlock()
		m.emit()
		m.release()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		m.Ensure(ctx)
		timer := time.NewTimer(m.waitDuration())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
