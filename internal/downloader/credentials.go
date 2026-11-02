package downloader

import (
	"fmt"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// CloudAPIRequestPacer shares Baidu quota pacing with OAuth account checks.
func (d *Downloader) CloudAPIRequestPacer() baiduapi.RequestPacer {
	return d.cloudAPIPacer
}

func (d *Downloader) LoadCredentials() ([]byte, error) {
	if d.queue.store == nil {
		return nil, fmt.Errorf("persistent credential store unavailable")
	}
	return d.queue.store.LoadCredentials()
}
func (d *Downloader) SaveCredentials(body []byte) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent credential store unavailable")
	}
	return d.queue.store.SaveCredentials(body)
}
func (d *Downloader) BindAccount(id string) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent account store unavailable")
	}
	return d.queue.store.BindAccount(id)
}

func (d *Downloader) AdministratorKey(configured string) (string, error) {
	if d.queue.store == nil {
		return "", fmt.Errorf("persistent administrator store unavailable")
	}
	return d.queue.store.AdministratorKey(configured)
}

func (d *Downloader) LoadBaiduAppConfig() (string, string, string, string, error) {
	if d.queue.store == nil {
		return "", "", "", "", fmt.Errorf("persistent Baidu application store unavailable")
	}
	return d.queue.store.LoadBaiduAppConfig()
}

func (d *Downloader) SaveBaiduAppConfigIfEmpty(appID, appKey, secretKey, signKey string) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent Baidu application store unavailable")
	}
	return d.queue.store.SaveBaiduAppConfigIfEmpty(appID, appKey, secretKey, signKey)
}

func (d *Downloader) CreateAdminSession(hash string, createdAt time.Time) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent administrator session store unavailable")
	}
	return d.queue.store.CreateAdminSession(hash, createdAt)
}

func (d *Downloader) AdminSessionActive(hash string) (bool, error) {
	if d.queue.store == nil {
		return false, fmt.Errorf("persistent administrator session store unavailable")
	}
	return d.queue.store.AdminSessionActive(hash)
}

func (d *Downloader) RevokeAdminSession(hash string, revokedAt time.Time) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent administrator session store unavailable")
	}
	return d.queue.store.RevokeAdminSession(hash, revokedAt)
}

func (d *Downloader) RevokeAllAdminSessions(revokedAt time.Time) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent administrator session store unavailable")
	}
	return d.queue.store.RevokeAllAdminSessions(revokedAt)
}
