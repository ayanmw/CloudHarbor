// Package management protects the local administrative surface independently
// of Baidu OAuth credentials.
package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionCookieName = "nas_admin_session"

type SessionStore interface {
	CreateAdminSession(string, time.Time) error
	AdminSessionActive(string) (bool, error)
	RevokeAdminSession(string, time.Time) error
}

type Gate struct {
	password [32]byte
	origin   *url.URL
	sessions SessionStore

	loginMu  sync.Mutex
	failures map[string]loginFailure
}

type loginFailure struct {
	started time.Time
	count   int
}

type originContextKey struct{}
type sessionContextKey struct{}

func NewWithOrigin(password, origin string) (*Gate, error) {
	g, err := New(password)
	if err != nil || origin == "" {
		return g, err
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(origin, "#") || strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("PUBLIC_ORIGIN must be an HTTPS origin without credentials, path, query or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("PUBLIC_ORIGIN port must be 1..65535")
		}
		u.Host = strings.TrimSuffix(u.Host, ":"+port)
		if n != 443 {
			u.Host += ":" + strconv.Itoa(n)
		}
	}
	g.origin = u
	return g, nil
}

func New(password string) (*Gate, error) {
	if len(password) < 10 || len(password) > 4096 {
		return nil, fmt.Errorf("ADMIN_PASSWORD must contain 10..4096 bytes")
	}
	return &Gate{password: sha256.Sum256([]byte(password)), failures: make(map[string]loginFailure)}, nil
}

func (g *Gate) SetSessionStore(store SessionStore) { g.sessions = store }

func withExpectedOrigin(r *http.Request, origin *url.URL) *http.Request {
	if origin == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), originContextKey{}, origin))
}

// CheckOrigin also serves Gorilla's WebSocket upgrader. A non-browser client
// authenticated by the outer gate can omit Origin.
func CheckOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if expected, ok := r.Context().Value(originContextKey{}).(*url.URL); ok {
		scheme, host = expected.Scheme, expected.Host
	}
	return err == nil && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.Scheme == scheme && strings.EqualFold(u.Host, host)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func browserPage(path string) bool { return !strings.HasPrefix(path, "/api/") && path != "/ws" }

func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		r = withExpectedOrigin(r, g.origin)
		if r.URL.Path == "/login" || r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}
		token := ""
		if r.URL.Path == "/api/logout" {
			if cookie, err := r.Cookie(sessionCookieName); err == nil {
				token = cookie.Value
			}
		} else if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
			token = strings.TrimPrefix(value, "Bearer ")
			if g.validBearer(token) {
				if !CheckOrigin(r) {
					http.Error(w, "cross-origin management request rejected", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			token = ""
		} else if cookie, err := r.Cookie(sessionCookieName); err == nil {
			token = cookie.Value
		}
		hash, valid, storeErr := g.sessionHash(token)
		if storeErr != nil {
			http.Error(w, "administrator session store unavailable", http.StatusServiceUnavailable)
			return
		}
		if !valid {
			if browserPage(r.URL.Path) {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			} else {
				http.Error(w, "administrator authentication required", http.StatusUnauthorized)
			}
			return
		}
		if !CheckOrigin(r) {
			http.Error(w, "cross-origin management request rejected", http.StatusForbidden)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, hash))
		next.ServeHTTP(w, r)
	})
}

func (g *Gate) validBearer(token string) bool {
	if token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], g.password[:]) == 1
}

func (g *Gate) sessionHash(token string) (string, bool, error) {
	if token == "" || g.sessions == nil {
		return "", false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", false, nil
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	active, err := g.sessions.AdminSessionActive(hash)
	return hash, active, err
}

func (g *Gate) loginIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return ip
	}
	return r.RemoteAddr
}

func (g *Gate) allowLogin(ip string) bool {
	now := time.Now()
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	for address, failure := range g.failures {
		if now.Sub(failure.started) >= 15*time.Minute {
			delete(g.failures, address)
		}
	}
	failure, ok := g.failures[ip]
	if ok && failure.count >= 10 {
		return false
	}
	if !ok && len(g.failures) >= 1024 {
		return false
	}
	return true
}

func (g *Gate) recordLoginFailure(ip string) {
	now := time.Now()
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	failure := g.failures[ip]
	if failure.started.IsZero() || now.Sub(failure.started) >= 15*time.Minute {
		failure = loginFailure{started: now}
	}
	failure.count++
	g.failures[ip] = failure
}

func (g *Gate) HandleLogin(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = withExpectedOrigin(r, g.origin)
	if r.Header.Get("Origin") == "" || !CheckOrigin(r) {
		http.Error(w, "same-origin login required", http.StatusForbidden)
		return
	}
	ip := g.loginIP(r)
	if !g.allowLogin(ip) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	if g.sessions == nil {
		http.Error(w, "administrator session store unavailable", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(request.Password) < 10 || len(request.Password) > 4096 {
		g.recordLoginFailure(ip)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sum := sha256.Sum256([]byte(request.Password))
	if subtle.ConstantTimeCompare(sum[:], g.password[:]) != 1 {
		g.recordLoginFailure(ip)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(token))
	if err := g.sessions.CreateAdminSession(hex.EncodeToString(hash[:]), time.Now().UTC()); err != nil {
		http.Error(w, "login unavailable", http.StatusServiceUnavailable)
		return
	}
	g.loginMu.Lock()
	delete(g.failures, ip)
	g.loginMu.Unlock()
	// Keep a persistent browser cookie; the server-side session itself has no
	// expiry and is invalidated only by explicit revocation.
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", Expires: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), HttpOnly: true, Secure: r.TLS != nil || g.origin != nil, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (g *Gate) HandleLogout(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash, _ := r.Context().Value(sessionContextKey{}).(string)
	if hash == "" || g.sessions == nil {
		http.Error(w, "administrator session required", http.StatusUnauthorized)
		return
	}
	if err := g.sessions.RevokeAdminSession(hash, time.Now().UTC()); err != nil {
		http.Error(w, "logout unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil || g.origin != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0).UTC()})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
