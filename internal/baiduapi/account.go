package baiduapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func IsAuthenticationError(err error) bool {
	_, ok := AuthenticationFingerprint(err)
	return ok
}

// Walk joined errors too: a folder's first child failure may be unrelated.
func AuthenticationFingerprint(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if api, ok := err.(*APIError); ok && (api.Status == http.StatusUnauthorized || api.Code == 110 || api.Code == 111) {
		return api.tokenFingerprint, true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if fp, found := AuthenticationFingerprint(child); found {
				return fp, true
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return AuthenticationFingerprint(wrapped.Unwrap())
	}
	return "", false
}

// The repository SDK declares uinfo.uk as an account identifier. Parse it as
// an integer/string without passing through float64 and losing large IDs.
func (c *Client) UserID() (string, error) {
	params := url.Values{"method": {"uinfo"}, "access_token": {c.AccessToken}}
	resp, err := c.get("https://pan.baidu.com/rest/2.0/xpan/nas?" + params.Encode())
	if err != nil {
		return "", requestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "uinfo", Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return "", err
	}
	if len(body) > 64*1024 {
		return "", fmt.Errorf("uinfo response too large")
	}
	var result struct {
		Errno     *int        `json:"errno"`
		ErrorCode int         `json:"error_code"`
		UK        json.Number `json:"uk"`
	}
	if json.Unmarshal(body, &result) != nil {
		return "", fmt.Errorf("invalid uinfo response")
	}
	if result.ErrorCode != 0 {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "uinfo", Status: resp.StatusCode, Code: result.ErrorCode}
	}
	if result.Errno == nil {
		return "", fmt.Errorf("uinfo response missing errno")
	}
	if *result.Errno != 0 {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "uinfo", Status: resp.StatusCode, Code: *result.Errno}
	}
	id, err := result.UK.Int64()
	if err != nil || id <= 0 {
		return "", fmt.Errorf("uinfo response missing valid account")
	}
	return strconv.FormatInt(id, 10), nil
}
