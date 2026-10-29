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

var ErrSourceOperationUnconfirmed = errors.New("source operation result is unconfirmed")

// Uncertainty preserves safe error causes for diagnostics, but the scheduler
// must not use a transient cause as permission to replay a source mutation.
func uncertainSource(cause error) error {
	if cause == nil {
		return ErrSourceOperationUnconfirmed
	}
	return fmt.Errorf("%w: %w", ErrSourceOperationUnconfirmed, cause)
}

func (c *Client) sourceOperation(operation string, item map[string]string) error {
	items, _ := json.Marshal([]map[string]string{item})
	query := url.Values{"method": {"filemanager"}, "opera": {operation}, "access_token": {c.AccessToken}}
	form := url.Values{"async": {"0"}, "filelist": {string(items)}}
	req, err := http.NewRequestWithContext(c.context(), "POST", "https://pan.baidu.com/rest/2.0/xpan/file?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return requestError(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: metadataHTTPClient.Timeout, Transport: metadataHTTPClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return uncertainSource(requestError(err))
	}
	defer resp.Body.Close()
	apiError := func(code int, business bool) error {
		return &APIError{Operation: operation, Status: resp.StatusCode, Code: code, tokenFingerprint: c.Fingerprint(), businessErrno: business}
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 {
			return apiError(0, false)
		}
		return uncertainSource(apiError(0, false))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return uncertainSource(err)
	}
	if len(body) > 64*1024 {
		return ErrSourceOperationUnconfirmed
	}
	var result struct {
		Errno     *int            `json:"errno"`
		ErrorCode int             `json:"error_code"`
		TaskID    json.RawMessage `json:"taskid"`
		Info      []struct {
			Errno *int `json:"errno"`
		} `json:"info"`
	}
	if json.Unmarshal(body, &result) != nil {
		return ErrSourceOperationUnconfirmed
	}
	if len(result.TaskID) > 0 && string(result.TaskID) != "0" && string(result.TaskID) != "null" {
		return ErrSourceOperationUnconfirmed
	}
	if result.Errno != nil && *result.Errno == 111 {
		return ErrSourceOperationUnconfirmed
	}
	for _, item := range result.Info {
		if item.Errno != nil && *item.Errno == 111 {
			return ErrSourceOperationUnconfirmed
		}
	}
	if result.ErrorCode != 0 {
		return apiError(result.ErrorCode, false)
	}
	if result.Errno == nil {
		return ErrSourceOperationUnconfirmed
	}
	if *result.Errno != 0 {
		return apiError(*result.Errno, true)
	}
	if len(result.Info) != 1 || result.Info[0].Errno == nil {
		return ErrSourceOperationUnconfirmed
	}
	if *result.Info[0].Errno != 0 {
		return apiError(*result.Info[0].Errno, true)
	}
	return nil
}
