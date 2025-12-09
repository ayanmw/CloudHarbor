package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

// 百度网盘 OAuth 设备授权流程工具
// 用于首次获取 Access Token

const (
	deviceCodeURL = "https://openapi.baidu.com/oauth/2.0/device/code"
	tokenURL      = "https://openapi.baidu.com/oauth/2.0/token"
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

func main() {
	appKey := os.Getenv("BAIDU_APP_KEY")
	secretKey := os.Getenv("BAIDU_SECRET_KEY")

	if appKey == "" || secretKey == "" {
		log.Fatal("请先设置环境变量: BAIDU_APP_KEY 和 BAIDU_SECRET_KEY")
	}

	fmt.Println("===== 百度网盘 Access Token 获取工具 =====\n")

	// 第一步：获取设备码
	fmt.Println("[1/3] 正在获取设备授权码...")
	deviceResp, err := getDeviceCode(appKey)
	if err != nil {
		log.Fatalf("获取设备码失败: %v", err)
	}

	fmt.Printf("\n[2/3] 请在浏览器中打开以下链接并授权:\n\n")
	fmt.Printf("    授权链接: %s\n", deviceResp.VerificationURL)
	fmt.Printf("    用户码: %s\n\n", deviceResp.UserCode)
	fmt.Printf("或者扫描二维码: %s\n\n", deviceResp.QRCodeURL)
	fmt.Printf("授权有效期: %d 秒\n", deviceResp.ExpiresIn)
	fmt.Printf("请在浏览器完成授权后，程序会自动继续...\n\n")

	// 第三步：轮询获取 token
	fmt.Println("[3/3] 等待授权完成...")
	token, err := pollForToken(appKey, secretKey, deviceResp.DeviceCode, deviceResp.Interval, deviceResp.ExpiresIn)
	if err != nil {
		log.Fatalf("获取 Token 失败: %v", err)
	}

	fmt.Printf("\n===== 授权成功！=====\n\n")
	fmt.Printf("Access Token:  %s\n", token.AccessToken)
	fmt.Printf("Refresh Token: %s\n", token.RefreshToken)
	fmt.Printf("有效期: %d 秒 (约 %d 天)\n\n", token.ExpiresIn, token.ExpiresIn/86400)

	fmt.Println("请将以上 Token 保存到 .env 文件中:")
	fmt.Printf("BAIDU_ACCESS_TOKEN=%s\n", token.AccessToken)
	fmt.Printf("BAIDU_REFRESH_TOKEN=%s\n", token.RefreshToken)
}

func getDeviceCode(appKey string) (*DeviceCodeResponse, error) {
	params := url.Values{}
	params.Set("response_type", "device_code")
	params.Set("client_id", appKey)
	params.Set("scope", "basic,netdisk")

	fullURL := deviceCodeURL + "?" + params.Encode()
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

func pollForToken(appKey, secretKey, deviceCode string, interval, expiresIn int) (*TokenResponse, error) {
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

			fullURL := tokenURL + "?" + params.Encode()
			resp, err := http.Get(fullURL)
			if err != nil {
				log.Printf("轮询请求失败: %v", err)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var result TokenResponse
			if err := json.Unmarshal(body, &result); err != nil {
				log.Printf("解析响应失败: %v", err)
				continue
			}

			// 检查错误
			if result.Error != "" {
				if result.Error == "authorization_pending" {
					// 用户还没授权，继续等待
					fmt.Print(".")
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
