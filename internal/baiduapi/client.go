package baiduapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

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
	Mtime          int64  `json:"mtime"`
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
	apiURL := "https://pan.baidu.com/rest/2.0/xpan/file"
	params := url.Values{}
	params.Set("method", "list")
	params.Set("access_token", c.AccessToken)
	params.Set("dir", dir)
	params.Set("order", "time")
	params.Set("desc", "1")
	params.Set("limit", "1000")

	fullURL := apiURL + "?" + params.Encode()
	fmt.Printf("[BaiduAPI] 列目录请求: dir=%s, url=%s\n", dir, fullURL)
	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("列目录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[BaiduAPI] 列目录响应: %s\n", string(body))
	var result ListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析列目录响应失败: %w, body=%s", err, string(body))
	}

	if result.Errno != 0 {
		return nil, fmt.Errorf("列目录失败 errno=%d, body=%s", result.Errno, string(body))
	}

	return result.List, nil
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
		return "", fmt.Errorf("获取下载链接请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[BaiduAPI] 获取下载链接响应: %s\n", string(body))

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

	req, err := http.NewRequest("GET", dlink, nil)
	if err != nil {
		return nil, 0, err
	}

	// 设置必要的请求头（模拟百度网盘官方客户端）
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://pan.baidu.com/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	// 添加额外的请求头以避免403错误
	req.Header.Set("Host", "d.pcs.baidu.com")

	fmt.Printf("[BaiduAPI] 开始下载文件（已添加 access_token）\n")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", err)
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
	// 在 dlink 中添加 access_token 参数
	if !strings.Contains(dlink, "access_token=") {
		separator := "&"
		if !strings.Contains(dlink, "?") {
			separator = "?"
		}
		dlink = dlink + separator + "access_token=" + c.AccessToken
	}

	req, err := http.NewRequest("GET", dlink, nil)
	if err != nil {
		return nil, 0, err
	}

	// 设置 Range 头部支持断点续传
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	// 设置必要的请求头（模拟百度网盘官方客户端）
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://pan.baidu.com/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	// 添加额外的请求头以避免403错误
	req.Header.Set("Host", "d.pcs.baidu.com")

	fmt.Printf("[BaiduAPI] 开始下载文件（支持断点续传，偏移量: %d）\n", startOffset)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		// 读取错误响应体
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("下载文件失败 status=%d, body=%s", resp.StatusCode, string(body))
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
		return fmt.Errorf("重命名请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Errno int `json:"errno"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析重命名响应失败: %w, body=%s", err, string(body))
	}

	if result.Errno != 0 {
		return fmt.Errorf("重命名失败 errno=%d, body=%s", result.Errno, string(body))
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
		return fmt.Errorf("删除请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Errno int `json:"errno"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析删除响应失败: %w, body=%s", err, string(body))
	}

	if result.Errno != 0 {
		return fmt.Errorf("删除失败 errno=%d, body=%s", result.Errno, string(body))
	}

	return nil
}
