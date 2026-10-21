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
		resp, err := http.Get("https://pan.baidu.com/rest/2.0/xpan/file?" + params.Encode())
		if err != nil {
			return nil, fmt.Errorf("列目录请求失败: %w", requestError(err))
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("列目录 HTTP %d", resp.StatusCode)
		}
		var result struct {
			Errno *int            `json:"errno"`
			List  json.RawMessage `json:"list"`
		}
		if err = json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("解析目录响应失败: %w", err)
		}
		if result.Errno == nil || *result.Errno != 0 {
			return nil, fmt.Errorf("目录响应失败 errno=%v", result.Errno)
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
	resp, err := http.Get(fullURL)
	if err != nil {
		return "", fmt.Errorf("获取下载链接请求失败: %w", requestError(err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
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
		return "", fmt.Errorf("解析下载链接响应失败: %w, body=%s", err, string(body))
	}

	// 检查错误
	if result.ErrorCode != 0 {
		return "", fmt.Errorf("获取下载链接失败 error_code=%d, error_msg=%s", result.ErrorCode, result.ErrorMsg)
	}

	if result.Errno != 0 {
		return "", fmt.Errorf("获取下载链接失败 errno=%d", result.Errno)
	}

	if len(result.List) == 0 || result.List[0].Dlink == "" {
		return "", fmt.Errorf("下载链接为空, body=%s", string(body))
	}

	return result.List[0].Dlink, nil
}

// DownloadFile 下载文件内容
func (c *Client) DownloadFile(dlink string) (io.ReadCloser, int64, error) {
	// 在 dlink 中添加 access_token 参数
	if !strings.Contains(dlink, "access_token=") {
		separator := "&"
		if !strings.Contains(dlink, "?") {
			separator = "?"
		}
		dlink = dlink + separator + "access_token=" + c.AccessToken
	}

	// 创建支持跳转的客户端
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 允许跳转，但保持必要的头部
			req.Header.Set("User-Agent", "pan.baidu.com")
			return nil
		},
	}

	req, err := http.NewRequest("GET", dlink, nil)
	if err != nil {
		return nil, 0, err
	}

	// 设置必要的请求头（根据百度官方文档要求）
	req.Header.Set("User-Agent", "pan.baidu.com")
	req.Header.Set("Host", "d.pcs.baidu.com")

	fmt.Printf("[BaiduAPI] 开始下载文件（已添加 access_token）\n")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", requestError(err))
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		// 读取错误响应体
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("下载文件失败 status=%d, body=%s", resp.StatusCode, string(body))
	}

	fmt.Printf("[BaiduAPI] 下载响应成功，Content-Length=%d\n", resp.ContentLength)
	return resp.Body, resp.ContentLength, nil
}

// DownloadFileWithRange 支持断点续传的下载文件内容
func (c *Client) DownloadFileWithRange(dlink string, startOffset int64) (io.ReadCloser, int64, error) {
	if startOffset < 0 {
		return nil, 0, fmt.Errorf("invalid negative offset")
	}
	// 在 dlink 中添加 access_token 参数
	if !strings.Contains(dlink, "access_token=") {
		separator := "&"
		if !strings.Contains(dlink, "?") {
			separator = "?"
		}
		dlink = dlink + separator + "access_token=" + c.AccessToken
	}

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

	req, err := http.NewRequest("GET", dlink, nil)
	if err != nil {
		return nil, 0, err
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
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", requestError(err))
	}

	// 检查响应状态码
	if startOffset > 0 && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, 0, ErrRangeIgnored
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, 0, fmt.Errorf("下载文件失败 status=%d, body=%s", resp.StatusCode, string(body))
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

// RenameFile 重命名文件
func (c *Client) RenameFile(path, newName string) error {
	apiURL := "https://pan.baidu.com/rest/2.0/xpan/file"
	params := url.Values{}
	params.Set("method", "filemanager")
	params.Set("access_token", c.AccessToken)
	params.Set("opera", "rename")

	opList := []map[string]string{
		{
			"path":    path,
			"newname": newName,
		},
	}
	opListJSON, _ := json.Marshal(opList)

	data := url.Values{}
	data.Set("async", "0")
	data.Set("filelist", string(opListJSON))

	req, err := http.NewRequest("POST", apiURL+"?"+params.Encode(), strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("重命名请求失败: %w", requestError(err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Errno *int `json:"errno"`
		Info  []struct {
			Errno int `json:"errno"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析重命名响应失败: %w, body=%s", err, string(body))
	}

	if resp.StatusCode != http.StatusOK || result.Errno == nil || *result.Errno != 0 {
		return fmt.Errorf("重命名请求失败 HTTP=%d", resp.StatusCode)
	}
	for _, item := range result.Info {
		if item.Errno != 0 {
			return fmt.Errorf("重命名项目失败 errno=%d", item.Errno)
		}
	}

	return nil
}

// DeleteFile 删除单个文件或空目录
func (c *Client) DeleteFile(path string) error {
	apiURL := "https://pan.baidu.com/rest/2.0/xpan/file"
	params := url.Values{}
	params.Set("method", "filemanager")
	params.Set("access_token", c.AccessToken)
	params.Set("opera", "delete")

	opList := []map[string]string{
		{
			"path": path,
		},
	}
	opListJSON, _ := json.Marshal(opList)

	data := url.Values{}
	data.Set("async", "0")
	data.Set("filelist", string(opListJSON))

	req, err := http.NewRequest("POST", apiURL+"?"+params.Encode(), strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("删除请求失败: %w", requestError(err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Errno *int `json:"errno"`
		Info  []struct {
			Errno int `json:"errno"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析删除响应失败: %w, body=%s", err, string(body))
	}

	if resp.StatusCode != http.StatusOK || result.Errno == nil || *result.Errno != 0 {
		return fmt.Errorf("删除请求失败 HTTP=%d", resp.StatusCode)
	}
	for _, item := range result.Info {
		if item.Errno != 0 {
			return fmt.Errorf("删除项目失败 errno=%d", item.Errno)
		}
	}

	return nil
}
