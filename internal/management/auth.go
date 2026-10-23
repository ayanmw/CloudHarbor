// Package management protects the local administrative surface, independently
// of the Baidu OAuth credentials.
package management

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Gate struct {
	password [32]byte
	origin   *url.URL
}

type originContextKey struct{}

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
		// Browser Origin serialization removes default 443 and leading zeroes.
		// The proxy review reproduced legal requests rejected without this step.
		u.Host = strings.TrimSuffix(u.Host, ":"+port)
		if n != 443 {
			u.Host += ":" + strconv.Itoa(n)
		}
	}
	g.origin = u
	return g, nil
}

func New(password string) (*Gate, error) {
	if len(password) < 16 || len(password) > 4096 {
		return nil, fmt.Errorf("ADMIN_PASSWORD must contain 16..4096 bytes")
	}
	return &Gate{password: sha256.Sum256([]byte(password))}, nil
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

func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		token := ""
		if user, password, ok := r.BasicAuth(); ok && user == "admin" {
			token = password
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		sum := sha256.Sum256([]byte(token))
		if token == "" || subtle.ConstantTimeCompare(sum[:], g.password[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Baidu NAS management", charset="UTF-8"`)
			http.Error(w, "administrator authentication required", http.StatusUnauthorized)
			return
		}
		// Only server configuration supplies this context. Forwarded headers
		// cannot alter the shared gate/WebSocket origin rule.
		if g.origin != nil {
			r = r.WithContext(context.WithValue(r.Context(), originContextKey{}, g.origin))
		}
		if !CheckOrigin(r) {
			http.Error(w, "cross-origin management request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
