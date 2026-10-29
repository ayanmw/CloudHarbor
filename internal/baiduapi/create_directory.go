package baiduapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var ErrCreateUnconfirmed = errors.New("directory creation result is unconfirmed")

// CreateDirectory uses the provider's xpan file create endpoint. It never
// retries because a lost response may follow a successful creation.
func (c *Client) CreateDirectory(directory string) error {
	query := url.Values{"method": {"create"}, "access_token": {c.AccessToken}}
	form := url.Values{"path": {directory}, "isdir": {"1"}, "size": {"0"}}
	req, err := http.NewRequestWithContext(c.context(), http.MethodPost, "https://pan.baidu.com/rest/2.0/xpan/file?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return requestError(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: metadataHTTPClient.Timeout, Transport: metadataHTTPClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCreateUnconfirmed, requestError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil || len(body) > 64*1024 {
		return ErrCreateUnconfirmed
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Operation: "create_directory", Status: resp.StatusCode, tokenFingerprint: c.Fingerprint()}
	}
	var result struct {
		Errno     *int `json:"errno"`
		ErrorCode int  `json:"error_code"`
	}
	if json.Unmarshal(body, &result) != nil || result.Errno == nil {
		return ErrCreateUnconfirmed
	}
	if result.ErrorCode != 0 {
		return &APIError{Operation: "create_directory", Status: resp.StatusCode, Code: result.ErrorCode, tokenFingerprint: c.Fingerprint()}
	}
	if *result.Errno != 0 {
		return &APIError{Operation: "create_directory", Status: resp.StatusCode, Code: *result.Errno, tokenFingerprint: c.Fingerprint(), businessErrno: true}
	}
	return nil
}
