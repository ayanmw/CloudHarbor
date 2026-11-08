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

var ErrMoveUnconfirmed = errors.New("move response does not confirm synchronous completion")
var ErrMoveAsync = errors.New("provider accepted an unexpected asynchronous move")

type AsyncMoveError struct{ TaskID string }

func (e *AsyncMoveError) Error() string { return ErrMoveAsync.Error() }
func (e *AsyncMoveError) Unwrap() error { return ErrMoveAsync }

// MoveFile never retries. Even a lost reply may have changed the source.
func (c *Client) MoveFile(source, destination, name string) error {
	return c.transferFile("move", source, destination, name)
}

// transferFile shares the receipt checks for the two synchronous filemanager
// transfers. Neither copying nor moving may replay a POST after an uncertain reply.
func (c *Client) transferFile(operation, source, destination, name string) error {
	unconfirmed := ErrMoveUnconfirmed
	asyncError := func(id string) error { return &AsyncMoveError{TaskID: id} }
	if operation == "copy" {
		unconfirmed = ErrCopyUnconfirmed
		asyncError = func(id string) error { return &AsyncCopyError{TaskID: id} }
	}
	items, _ := json.Marshal([]map[string]string{{"path": source, "dest": destination, "newname": name, "ondup": "fail"}})
	query := url.Values{"method": {"filemanager"}, "opera": {operation}, "access_token": {c.AccessToken}}
	form := url.Values{"async": {"0"}, "ondup": {"fail"}, "filelist": {string(items)}}
	req, err := http.NewRequestWithContext(c.context(), "POST", "https://pan.baidu.com/rest/2.0/xpan/file?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return requestError(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// 307/308 redirects would replay a POST through GetBody. Moving a source
	// must never be repeated implicitly, even to another provider URL.
	client := &http.Client{Timeout: metadataHTTPClient.Timeout, Transport: metadataHTTPClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", operation, requestError(err))
	}
	defer resp.Body.Close()
	apiError := func(code int) error {
		return &APIError{Operation: operation, Status: resp.StatusCode, Code: code, tokenFingerprint: c.Fingerprint()}
	}
	if resp.StatusCode != 200 {
		return apiError(0)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return err
	}
	if len(body) > 64*1024 {
		return unconfirmed
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
		return unconfirmed
	}
	// A nonzero task ID prevents interpreting any accompanying error as proof
	// of no mutation. Its asynchronous work might still arrive later.
	if len(result.TaskID) > 0 && string(result.TaskID) != "0" && string(result.TaskID) != "null" {
		var number json.Number
		var id string
		if json.Unmarshal(result.TaskID, &number) == nil {
			if value, err := number.Int64(); err == nil && value > 0 {
				id = fmt.Sprint(value)
			}
		}
		return asyncError(id)
	}
	// As with taskid above, running work takes precedence over accompanying
	// failure fields: we cannot prove that the mutation did not happen.
	if result.Errno != nil && *result.Errno == 111 {
		return asyncError("")
	}
	for _, item := range result.Info {
		if item.Errno != nil && *item.Errno == 111 {
			return asyncError("")
		}
	}
	if result.ErrorCode != 0 {
		return apiError(result.ErrorCode)
	}
	if result.Errno == nil {
		return unconfirmed
	}
	if *result.Errno != 0 {
		return &APIError{Operation: operation, Status: resp.StatusCode, Code: *result.Errno, tokenFingerprint: c.Fingerprint(), businessErrno: true}
	}
	if len(result.Info) != 1 || result.Info[0].Errno == nil {
		return unconfirmed
	}
	if *result.Info[0].Errno != 0 {
		return &APIError{Operation: operation, Status: resp.StatusCode, Code: *result.Info[0].Errno, tokenFingerprint: c.Fingerprint(), businessErrno: true}
	}
	return nil
}
