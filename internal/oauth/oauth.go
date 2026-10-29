package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	DeviceCodeURL = "https://openapi.baidu.com/oauth/2.0/device/code"
	TokenURL      = "https://openapi.baidu.com/oauth/2.0/token"
)

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	QRCodeURL       string `json:"qrcode_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

var ErrInvalidResponse = errors.New("invalid OAuth response")
var ErrConfiguration = errors.New("missing OAuth configuration")

type ProviderError struct {
	Code   string
	Status int
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("OAuth error: %s (HTTP %d)", e.Code, e.Status)
}

type networkError struct{ cause error }

func (e *networkError) Error() string { return "OAuth network request failed" }
func (e *networkError) Unwrap() error { return e.cause }
func IsTransient(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var provider *ProviderError
	if errors.As(err, &provider) {
		return provider.Status == 429 || provider.Status >= 500 || provider.Code == "temporarily_unavailable" || provider.Code == "server_error"
	}
	var network *networkError
	return errors.As(err, &network)
}
func safeCode(code string) string {
	switch code {
	case "authorization_pending", "slow_down", "invalid_grant", "expired_token", "invalid_client", "access_denied", "invalid_request", "unsupported_grant_type", "invalid_scope", "temporarily_unavailable", "server_error":
		return code
	}
	return "provider_error"
}

var httpClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

// Retain the GET protocol used by the repository's Baidu SDK samples. Errors
// deliberately omit request URLs, raw bodies and provider error descriptions.
func request(ctx context.Context, address string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"?"+params.Encode(), nil)
	if err != nil {
		return &networkError{cause: err}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return &networkError{cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return &ProviderError{Code: "http_error", Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return &networkError{cause: err}
	}
	if len(body) > 64*1024 {
		return ErrInvalidResponse
	}
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ErrInvalidResponse
	}
	if envelope.Error != "" {
		return &ProviderError{Code: safeCode(envelope.Error), Status: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		return &ProviderError{Code: "http_error", Status: resp.StatusCode}
	}
	if json.Unmarshal(body, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}

func GetDeviceCode(appKey string) (*DeviceCodeResponse, error) {
	return GetDeviceCodeContext(context.Background(), appKey)
}
func GetDeviceCodeContext(ctx context.Context, appKey string) (*DeviceCodeResponse, error) {
	if appKey == "" {
		return nil, ErrConfiguration
	}
	params := url.Values{"response_type": {"device_code"}, "client_id": {appKey}, "scope": {"basic,netdisk"}}
	var result DeviceCodeResponse
	if err := request(ctx, DeviceCodeURL, params, &result); err != nil {
		return nil, err
	}
	if result.DeviceCode == "" || len(result.DeviceCode) > 4096 || result.UserCode == "" || result.Interval < 1 || result.Interval > 60 || result.ExpiresIn < 1 || result.ExpiresIn > 3600 {
		return nil, ErrInvalidResponse
	}
	return &result, nil
}
func validToken(token *TokenResponse) bool {
	return token.AccessToken != "" && len(token.AccessToken) <= 16384 && len(token.RefreshToken) <= 16384 && token.ExpiresIn > 0 && token.ExpiresIn <= 366*24*60*60
}
func PollForToken(appKey, secretKey, deviceCode string, interval, expiresIn int) (*TokenResponse, error) {
	return PollForTokenContext(context.Background(), appKey, secretKey, deviceCode, interval, expiresIn)
}
func PollForTokenContext(ctx context.Context, appKey, secretKey, deviceCode string, interval, expiresIn int) (*TokenResponse, error) {
	if appKey == "" || secretKey == "" || deviceCode == "" {
		return nil, ErrConfiguration
	}
	if interval < 1 || interval > 60 || expiresIn < 1 || expiresIn > 3600 {
		return nil, fmt.Errorf("invalid device polling parameters")
	}
	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(expiresIn)*time.Second)
	defer cancel()
	delay := time.Duration(interval) * time.Second
	for {
		var result TokenResponse
		err := request(pollCtx, TokenURL, url.Values{"grant_type": {"device_token"}, "code": {deviceCode}, "client_id": {appKey}, "client_secret": {secretKey}}, &result)
		if err == nil {
			if !validToken(&result) {
				return nil, ErrInvalidResponse
			}
			return &result, nil
		}
		if pollCtx.Err() != nil {
			return nil, pollCtx.Err()
		}
		var provider *ProviderError
		if errors.As(err, &provider) {
			if provider.Code == "slow_down" {
				delay += 5 * time.Second
			} else if provider.Code != "authorization_pending" && !IsTransient(err) {
				return nil, err
			}
		} else if !IsTransient(err) {
			return nil, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return nil, pollCtx.Err()
		case <-timer.C:
		}
	}
}
func RefreshToken(appKey, secretKey, refreshToken string) (*TokenResponse, error) {
	return RefreshTokenContext(context.Background(), appKey, secretKey, refreshToken)
}
func RefreshTokenContext(ctx context.Context, appKey, secretKey, refreshToken string) (*TokenResponse, error) {
	if appKey == "" || secretKey == "" || refreshToken == "" {
		return nil, ErrConfiguration
	}
	var result TokenResponse
	err := request(ctx, TokenURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {appKey}, "client_secret": {secretKey}}, &result)
	if err != nil {
		return nil, err
	}
	if !validToken(&result) {
		return nil, ErrInvalidResponse
	}
	return &result, nil
}
