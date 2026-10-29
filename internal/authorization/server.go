package authorization

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"web_browser_baidu_onekey_upload/internal/credentials"
	"web_browser_baidu_onekey_upload/internal/oauth"
)

type Manager interface {
	Apply(context.Context, *oauth.TokenResponse) error
	Refresh(context.Context) error
	Status() credentials.Status
}
type Provider interface {
	Device(context.Context, string) (*oauth.DeviceCodeResponse, error)
	Poll(context.Context, string, string, string, int, int) (*oauth.TokenResponse, error)
}
type baiduProvider struct{}

func (baiduProvider) Device(ctx context.Context, key string) (*oauth.DeviceCodeResponse, error) {
	return oauth.GetDeviceCodeContext(ctx, key)
}
func (baiduProvider) Poll(ctx context.Context, key, secret, code string, interval, expires int) (*oauth.TokenResponse, error) {
	return oauth.PollForTokenContext(ctx, key, secret, code, interval, expires)
}

type session struct {
	code     string
	key      string
	secret   string
	interval int
	deadline time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	polling  bool
}
type Server struct {
	ctx         context.Context
	key, secret string
	manager     Manager
	provider    Provider
	mu          sync.Mutex
	generation  uint64
	session     *session
}

func New(ctx context.Context, key, secret string, manager Manager, provider Provider) *Server {
	if provider == nil {
		provider = baiduProvider{}
	}
	return &Server{ctx: ctx, key: key, secret: secret, manager: manager, provider: provider}
}

func (s *Server) ConfigureApp(key, secret string) error {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(secret) == "" || len(key) > 4096 || len(secret) > 4096 {
		return fmt.Errorf("invalid Baidu application config")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	if s.session != nil {
		s.session.cancel()
		s.session = nil
	}
	s.key, s.secret = key, secret
	return nil
}
func postOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	respond(w, s.manager.Status())
}
func (s *Server) HandleDeviceCode(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	s.mu.Lock()
	key, secret := s.key, s.secret
	s.generation++
	generation := s.generation
	if s.session != nil {
		s.session.cancel()
		s.session = nil
	}
	s.mu.Unlock()
	if key == "" || secret == "" {
		http.Error(w, "BAIDU_APP_KEY / BAIDU_SECRET_KEY 未配置", 400)
		return
	}
	device, err := s.provider.Device(r.Context(), key)
	if err != nil {
		http.Error(w, "device authorization request failed", 502)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.ctx.Err() != nil || r.Context().Err() != nil {
		http.Error(w, "device session replaced or cancelled", 409)
		return
	}
	ctx, cancel := context.WithDeadline(s.ctx, time.Now().Add(time.Duration(device.ExpiresIn)*time.Second))
	s.session = &session{code: device.DeviceCode, key: key, secret: secret, interval: device.Interval, deadline: time.Now().Add(time.Duration(device.ExpiresIn) * time.Second), ctx: ctx, cancel: cancel}
	respond(w, device)
}
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	var request struct {
		DeviceCode string `json:"device_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid device authorization request", 400)
		return
	}
	s.mu.Lock()
	current := s.session
	if current == nil || current.code != request.DeviceCode || current.polling || !time.Now().Before(current.deadline) {
		s.mu.Unlock()
		http.Error(w, "device session missing, busy or expired", 409)
		return
	}
	current.polling = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.session == current {
			current.polling = false
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(current.ctx, cancel)
	defer stop()
	remaining := int(time.Until(current.deadline) / time.Second)
	if remaining < 1 {
		remaining = 1
	}
	token, err := s.provider.Poll(ctx, current.key, current.secret, current.code, current.interval, remaining)
	if err != nil {
		http.Error(w, "device authorization incomplete, cancelled or failed", 502)
		return
	}
	if current.ctx.Err() != nil || ctx.Err() != nil {
		http.Error(w, "device session cancelled", 409)
		return
	}
	if err = s.manager.Apply(ctx, token); err != nil {
		http.Error(w, "authorization could not be verified and saved; check credential status", 502)
		return
	}
	s.mu.Lock()
	if s.session == current {
		s.session = nil
		current.cancel()
	}
	s.mu.Unlock()
	respond(w, s.manager.Status())
}
func (s *Server) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	if err := s.manager.Refresh(r.Context()); err != nil {
		http.Error(w, "credential refresh failed; check credential status", 502)
		return
	}
	respond(w, s.manager.Status())
}
