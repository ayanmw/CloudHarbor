package oauth

import (
	"encoding/json"
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

// GetDeviceCode 获取设备码
func GetDeviceCode(appKey string) (*DeviceCodeResponse, error) {
	params := url.Values{}
	params.Set("response_type", "device_code")
	params.Set("client_id", appKey)
	params.Set("scope", "basic,netdisk")

	fullURL := DeviceCodeURL + "?" + params.Encode()
	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w, body=%s", err, string(body))
	}

	if result.DeviceCode == "" {
		return nil, fmt.Errorf("获取设备码失败: %s", string(body))
	}

	return &result, nil
}

// PollForToken 轮询获取 Token
func PollForToken(appKey, secretKey, deviceCode string, interval, expiresIn int) (*TokenResponse, error) {
	if interval < 1 {
		interval = 5
	}

	timeout := time.After(time.Duration(expiresIn) * time.Second)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("授权超时")
		case <-ticker.C:
			params := url.Values{}
			params.Set("grant_type", "device_token")
			params.Set("code", deviceCode)
			params.Set("client_id", appKey)
			params.Set("client_secret", secretKey)

			fullURL := TokenURL + "?" + params.Encode()
			resp, err := http.Get(fullURL)
			if err != nil {
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var result TokenResponse
			if err := json.Unmarshal(body, &result); err != nil {
				continue
			}

			// 检查错误
			if result.Error != "" {
				if result.Error == "authorization_pending" {
					// 用户还没授权，继续等待
					continue
				} else if result.Error == "slow_down" {
					// 轮询太快，延长间隔
					ticker.Reset(time.Duration(interval+5) * time.Second)
					continue
				} else {
					return nil, fmt.Errorf("授权失败: %s - %s", result.Error, result.ErrorDesc)
				}
			}

			// 成功获取 token
			if result.AccessToken != "" {
				return &result, nil
			}
		}
	}
}

// RefreshToken 刷新 Access Token
func RefreshToken(appKey, secretKey, refreshToken string) (*TokenResponse, error) {
	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", refreshToken)
	params.Set("client_id", appKey)
	params.Set("client_secret", secretKey)

	fullURL := TokenURL + "?" + params.Encode()
	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result TokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w, body=%s", err, string(body))
	}

	if result.Error != "" {
		return nil, fmt.Errorf("刷新 Token 失败: %s - %s", result.Error, result.ErrorDesc)
	}

	return &result, nil
}
