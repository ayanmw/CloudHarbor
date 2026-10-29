package taskstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrBaiduAppConfigExists = errors.New("Baidu application configuration already exists")

type baiduAppConfig struct {
	Version   int    `json:"version"`
	AppID     string `json:"app_id"`
	AppKey    string `json:"app_key"`
	SecretKey string `json:"secret_key"`
	SignKey   string `json:"sign_key"`
}

func (s *Store) LoadBaiduAppConfig() (string, string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", "", "", "", os.ErrClosed
	}
	body, err := s.readPrivate("baidu-app.json", 16*1024)
	if err != nil {
		return "", "", "", "", err
	}
	if body == nil {
		return "", "", "", "", nil
	}
	var record baiduAppConfig
	if json.Unmarshal(body, &record) != nil || record.Version != 1 || strings.TrimSpace(record.AppID) == "" || strings.TrimSpace(record.AppKey) == "" || strings.TrimSpace(record.SecretKey) == "" || strings.TrimSpace(record.SignKey) == "" {
		return "", "", "", "", fmt.Errorf("invalid Baidu application config record")
	}
	return record.AppID, record.AppKey, record.SecretKey, record.SignKey, nil
}

func (s *Store) SaveBaiduAppConfigIfEmpty(appID, appKey, secretKey, signKey string) error {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appKey) == "" || strings.TrimSpace(secretKey) == "" || strings.TrimSpace(signKey) == "" || len(appID) > 4096 || len(appKey) > 4096 || len(secretKey) > 4096 || len(signKey) > 4096 {
		return fmt.Errorf("invalid Baidu application config")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	name := filepath.Join(s.dir, "baidu-app.json")
	if err := safePath(name); err != nil {
		return err
	}
	if _, err := os.Lstat(name); err == nil {
		return ErrBaiduAppConfigExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, err := json.Marshal(baiduAppConfig{Version: 1, AppID: appID, AppKey: appKey, SecretKey: secretKey, SignKey: signKey})
	if err != nil {
		return err
	}
	return atomicWrite(name, body)
}
