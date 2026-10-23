package baiduapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ErrRangeIgnored means the server returned a full body for a resume request.
// The caller must restart from zero instead of appending that body.
var ErrRangeIgnored = errors.New("server ignored Range; restart required")

// net/http wraps failures in url.Error, whose Error string includes credentials
// in the request URL. Preserve the cause without putting that URL in task/logs.
func requestError(err error) error {
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		return fmt.Errorf("%s: %w", requestErr.Op, requestErr.Err)
	}
	return err
}

// Client 百度网盘 API 客户端
type Client struct {
	AccessToken string
	AppKey      string
	ctx         context.Context
	idleTimeout time.Duration
}

// Copies keep each worker's cancellation scope independent from token updates.
func (c *Client) WithContext(ctx context.Context) *Client { copy := *c; copy.ctx = ctx; return &copy }
func (c *Client) context() context.Context {
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

var metadataHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (c *Client) get(address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(c.context(), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	return metadataHTTPClient.Do(req)
}

type APIError struct {
	Operation        string
	Status           int
	Code             int
	tokenFingerprint string
	// Preserve the JSON namespace: xpan errno=111 means async work is running,
	// whereas OAuth error_code=111 means the access token has expired.
	businessErrno bool
}

// Fingerprint identifies the credential used by a request without exposing it.
func (c *Client) Fingerprint() string {
	if c.AccessToken == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(c.AccessToken))
	return hex.EncodeToString(sum[:])
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s failed: HTTP=%d code=%d", e.Operation, e.Status, e.Code)
}
func IsRetryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == 429 || api.Status >= 500 || api.Code == 31034
	}
	var network net.Error
	return errors.As(err, &network)
}

// FileInfo 文件信息
type FileInfo struct {
	FsID           int64  `json:"fs_id"`
	Path           string `json:"path"`
	ServerFilename string `json:"server_filename"`
	Size           int64  `json:"size"`
	IsDir          int    `json:"isdir"` // 1=目录，0=文件
	Mtime          int64  `json:"server_mtime"`
	MD5            string `json:"md5"` // 添加MD5字段
}

// ListResponse 列目录响应
type ListResponse struct {
	Errno int        `json:"errno"`
	List  []FileInfo `json:"list"`
}

// NewClient 创建客户端
func NewClient(accessToken, appKey string) *Client {
	return &Client{
		AccessToken: accessToken,
		AppKey:      appKey,
	}
}

// ListDir 列出指定目录内容
// dir: 网盘路径，如 /NAS_Download_Queue
func (c *Client) ListDir(dir string) ([]FileInfo, error) {
	const limit = 1000
	const responseLimit = 16 * 1024 * 1024
	files := make([]FileInfo, 0)
	seen := make(map[int64]bool)
	for start := 0; ; start += limit {
		params := url.Values{}
		params.Set("method", "list")
		params.Set("access_token", c.AccessToken)
		params.Set("dir", dir)
		params.Set("order", "name")
		params.Set("desc", "0")
		params.Set("start", fmt.Sprint(start))
		params.Set("limit", fmt.Sprint(limit))
		resp, err := c.get("https://pan.baidu.com/rest/2.0/xpan/file?" + params.Encode())
		if err != nil {
			return nil, fmt.Errorf("列目录请求失败: %w", requestError(err))
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, &APIError{tokenFingerprint: c.Fingerprint(), Operation: "list", Status: resp.StatusCode}
		}
		if len(body) > responseLimit {
			return nil, fmt.Errorf("目录响应超过大小限制")
		}
		var result struct {
			Errno     *int            `json:"errno"`
			ErrorCode int             `json:"error_code"`
			List      json.RawMessage `json:"list"`
		}
		if err = json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("解析目录响应失败: %w", err)
		}
		if result.ErrorCode != 0 {
			return nil, &APIError{tokenFingerprint: c.Fingerprint(), Operation: "list", Status: resp.StatusCode, Code: result.ErrorCode}
		}
		if result.Errno == nil {
			return nil, fmt.Errorf("目录响应缺失 errno")
		}
		if *result.Errno != 0 {
			return nil, &APIError{tokenFingerprint: c.Fingerprint(), Operation: "list", Status: resp.StatusCode, Code: *result.Errno, businessErrno: true}
		}
		if len(result.List) == 0 || result.List[0] != '[' {
			return nil, fmt.Errorf("目录响应缺失文件列表")
		}
		var page []FileInfo
		if err = json.Unmarshal(result.List, &page); err != nil {
			return nil, err
		}
		for _, file := range page {
			if seen[file.FsID] {
				return nil, fmt.Errorf("分页目录发生重复，等待下一次扫描: fs_id=%d", file.FsID)
			}
			seen[file.FsID] = true
			files = append(files, file)
		}
		if len(page) < limit {
			return files, nil
		}
	}
}

// GetDownloadLink 获取文件下载链接
// 改用 PCS 接口直接构造下载链接
func (c *Client) GetDownloadLink(fsid int64) (string, error) {
	// 方法1: 尝试使用 xpan multimedia 接口
	apiURL := "https://pan.baidu.com/rest/2.0/xpan/multimedia"
	params := url.Values{}
	params.Set("method", "filemetas")
	params.Set("access_token", c.AccessToken)
	params.Set("fsids", fmt.Sprintf("[%d]", fsid))
	params.Set("dlink", "1")

	fullURL := apiURL + "?" + params.Encode()
	fmt.Printf("[BaiduAPI] 获取下载链接请求(multimedia): fsid=%d\n", fsid)
	resp, err := c.get(fullURL)
	if err != nil {
		return "", fmt.Errorf("获取下载链接请求失败: %w", requestError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "filemetas", Status: resp.StatusCode}
	}

	const responseLimit = 1024 * 1024
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if readErr != nil {
		return "", readErr
	}
	if len(body) > responseLimit {
		return "", fmt.Errorf("下载链接响应超过大小限制")
	}
	// Temporary download URLs must not be written into logs.

	var result struct {
		Errno     int    `json:"errno"`
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
		List      []struct {
			Dlink string `json:"dlink"`
		} `json:"list"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析下载链接响应失败: %w", err)
	}

	// 检查错误
	if result.ErrorCode != 0 {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "filemetas", Status: resp.StatusCode, Code: result.ErrorCode}
	}

	if result.Errno != 0 {
		return "", &APIError{tokenFingerprint: c.Fingerprint(), Operation: "filemetas", Status: resp.StatusCode, Code: result.Errno, businessErrno: true}
	}

	if len(result.List) == 0 || result.List[0].Dlink == "" {
		return "", fmt.Errorf("下载链接为空")
	}

	return result.List[0].Dlink, nil
}

// DownloadFile 下载文件内容
func (c *Client) DownloadFile(dlink string) (io.ReadCloser, int64, error) {
	return c.DownloadFileWithRange(dlink, 0)
}

// DownloadFileWithRange 支持断点续传的下载文件内容
func (c *Client) DownloadFileWithRange(dlink string, startOffset int64) (io.ReadCloser, int64, error) {
	if startOffset < 0 {
		return nil, 0, fmt.Errorf("invalid negative offset")
	}
	ctx, cancel := context.WithCancelCause(c.context())
	timeout := c.idleTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	timer := time.AfterFunc(timeout, func() { cancel(errDownloadIdleTimeout) })
	accepted := false
	defer func() {
		if !accepted {
			timer.Stop()
			cancel(nil)
		}
	}()
	// The local HTTP regression found that substring checks confuse paths and
	// similarly named parameters with the credential; raw concatenation also
	// puts it after fragments and changes tokens containing query delimiters.
	link, err := url.Parse(dlink)
	if err != nil {
		return nil, 0, requestError(err)
	}
	if _, present := link.Query()["access_token"]; !present {
		if link.RawQuery != "" {
			link.RawQuery += "&"
		}
		// Append only the new parameter: re-encoding the existing query can
		// change provider signatures or its spelling of percent escapes.
		link.RawQuery += url.Values{"access_token": {c.AccessToken}}.Encode()
	}
	dlink = link.String()

	// 创建支持跳转的客户端
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// 允许跳转，但保持必要的头部
			req.Header.Set("User-Agent", "pan.baidu.com")
			if startOffset > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", dlink, nil)
	if err != nil {
		return nil, 0, requestError(err)
	}

	// 设置 Range 头部支持断点续传
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	// 设置必要的请求头（根据百度官方文档要求）
	req.Header.Set("User-Agent", "pan.baidu.com")
	req.Header.Set("Host", "d.pcs.baidu.com")

	fmt.Printf("[BaiduAPI] 开始下载文件（支持断点续传，偏移量: %d）\n", startOffset)
	resp, err := client.Do(req)
	if err != nil {
		if context.Cause(ctx) == errDownloadIdleTimeout {
			return nil, 0, errDownloadIdleTimeout
		}
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", requestError(err))
	}
	resp.Body = &idleBody{ReadCloser: resp.Body, ctx: ctx, cancel: cancel, timer: timer, timeout: timeout}
	accepted = true

	// 检查响应状态码
	if startOffset > 0 && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, 0, ErrRangeIgnored
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		_ = body
		return nil, 0, &APIError{tokenFingerprint: c.Fingerprint(), Operation: "download", Status: resp.StatusCode}
	}
	// 接受 206 前验证完整后缀，不能仅根据状态码追加，否则会损坏本地文件。
	if resp.StatusCode == http.StatusPartialContent {
		var start, end, total int64
		value := resp.Header.Get("Content-Range")
		n, parseErr := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total)
		if parseErr != nil || n != 3 || value != fmt.Sprintf("bytes %d-%d/%d", start, end, total) ||
			start != startOffset || end < start || total <= end || end != total-1 ||
			(resp.ContentLength >= 0 && resp.ContentLength != end-start+1) {
			resp.Body.Close()
			return nil, 0, fmt.Errorf("invalid Content-Range %q for offset %d", value, startOffset)
		}
		return resp.Body, total - start, nil
	}

	fmt.Printf("[BaiduAPI] 下载响应成功，Content-Length=%d, StatusCode=%d\n", resp.ContentLength, resp.StatusCode)
	return resp.Body, resp.ContentLength, nil
}

// RenameFile requests a strictly confirmed synchronous rename.
func (c *Client) RenameFile(path, newName string) error {
	return c.sourceOperation("rename", map[string]string{"path": path, "newname": newName})
}

// DeleteFile requests a strictly confirmed synchronous delete.
func (c *Client) DeleteFile(path string) error {
	return c.sourceOperation("delete", map[string]string{"path": path})
}
