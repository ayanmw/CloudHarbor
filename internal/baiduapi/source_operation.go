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

// Only fixed reason codes and validated numeric task IDs survive the response;
// raw provider bodies and credential-bearing URLs are never persisted.
type SourceOutcomeError struct {
	Reason     string
	HTTPStatus int
	TaskID     string
	cause      error
}

func (e *SourceOutcomeError) Error() string {
	return fmt.Sprintf("source operation result is unconfirmed: reason=%s status=%d", e.Reason, e.HTTPStatus)
}
func (e *SourceOutcomeError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrSourceOperationUnconfirmed}
	}
	return []error{ErrSourceOperationUnconfirmed, e.cause}
}
func sourceOutcome(reason string, status int, taskID string, cause error) error {
	return &SourceOutcomeError{Reason: reason, HTTPStatus: status, TaskID: taskID, cause: cause}
}

// The scheduler must not use a preserved transient cause as permission to
// replay a mutation; SourceOutcomeError always unwraps to the unknown sentinel.
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
		return sourceOutcome("transport_error", 0, "", requestError(err))
	}
	defer resp.Body.Close()
	apiError := func(code int, business bool) error {
		return &APIError{Operation: operation, Status: resp.StatusCode, Code: code, tokenFingerprint: c.Fingerprint(), businessErrno: business}
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 {
			return apiError(0, false)
		}
		return sourceOutcome("http_status_unconfirmed", resp.StatusCode, "", apiError(0, false))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return sourceOutcome("response_read_failed", resp.StatusCode, "", err)
	}
	if len(body) > 64*1024 {
		return sourceOutcome("response_too_large", resp.StatusCode, "", nil)
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
		return sourceOutcome("invalid_response_json", resp.StatusCode, "", nil)
	}
	if len(result.TaskID) > 0 && string(result.TaskID) != "0" && string(result.TaskID) != "null" {
		id := strings.Trim(string(result.TaskID), "\"")
		if len(id) > 64 || strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			id = ""
		}
		return sourceOutcome("async_task", resp.StatusCode, id, nil)
	}
	if result.Errno != nil && *result.Errno == 111 {
		return sourceOutcome("async_running", resp.StatusCode, "", nil)
	}
	for _, item := range result.Info {
		if item.Errno != nil && *item.Errno == 111 {
			return sourceOutcome("async_running", resp.StatusCode, "", nil)
		}
	}
	if result.ErrorCode != 0 {
		return apiError(result.ErrorCode, false)
	}
	if result.Errno == nil {
		return sourceOutcome("missing_result_code", resp.StatusCode, "", nil)
	}
	if *result.Errno != 0 {
		return apiError(*result.Errno, true)
	}
	if len(result.Info) != 1 {
		return sourceOutcome("missing_item_receipt", resp.StatusCode, "", nil)
	}
	if result.Info[0].Errno == nil {
		return sourceOutcome("missing_item_result", resp.StatusCode, "", nil)
	}
	if *result.Info[0].Errno != 0 {
		return apiError(*result.Info[0].Errno, true)
	}
	return nil
}
