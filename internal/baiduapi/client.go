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
	req, err := http.NewRequest("GET", dlink, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "pan.baidu.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("下载文件请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("下载文件失败 status=%d", resp.StatusCode)
	}

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
