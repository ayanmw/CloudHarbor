package baiduapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

var ErrUploadUnconfirmed = errors.New("上传提交结果未知，请核对远端，不要重复提交")

const UploadBlockSize = 4 * 1024 * 1024

// UploadReceipt records the actual destination, including a provider conflict rename.
type UploadReceipt struct {
	Path string `json:"path"`
	FsID string `json:"fs_id"`
	Size string `json:"size"`
}
type uploadResponse struct {
	Errno      *int   `json:"errno"`
	ErrorCode  int    `json:"error_code"`
	UploadID   string `json:"uploadid"`
	ReturnType int    `json:"return_type"`
	BlockList  []int  `json:"block_list"`
	MD5        string `json:"md5"`
	Path       string `json:"path"`
	FsID       int64  `json:"fs_id"`
	Size       int64  `json:"size"`
}

func (c *Client) uploadRequest(ctx context.Context, address, contentType string, body io.Reader) (uploadResponse, error) {
	var value uploadResponse
	if c.requestPacer != nil {
		if err := c.requestPacer.Wait(ctx); err != nil {
			return value, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, body)
	if err != nil {
		return value, requestError(err)
	}
	req.Header.Set("Content-Type", contentType)
	client := http.Client{Timeout: 5 * time.Minute, Transport: metadataHTTPClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return value, fmt.Errorf("%w: %v", ErrUploadUnconfirmed, requestError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 || resp.StatusCode >= 500 || resp.StatusCode == 408 {
		return value, ErrUploadUnconfirmed
	}
	if json.Unmarshal(raw, &value) != nil || value.Errno == nil {
		return value, ErrUploadUnconfirmed
	}
	if resp.StatusCode != 200 || *value.Errno != 0 || value.ErrorCode != 0 {
		code := *value.Errno
		if value.ErrorCode != 0 {
			code = value.ErrorCode
		}
		return value, &APIError{Operation: "upload", Status: resp.StatusCode, Code: code, tokenFingerprint: c.Fingerprint(), businessErrno: value.ErrorCode == 0}
	}
	return value, nil
}

// UploadLocalFile never repeats a mutating call. The caller durably records each
// phase before side effects; lost precreate/create replies can include a created file.
func (c *Client) UploadLocalFile(ctx context.Context, local, remote string, progress func(string, int64) error) (UploadReceipt, error) {
	var receipt UploadReceipt
	f, err := os.Open(local)
	if err != nil {
		return receipt, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return receipt, errors.New("上传暂存文件不可读")
	}
	if info.Size() > int64(UploadBlockSize)*10000 {
		return receipt, errors.New("单文件超过当前分片数量上限")
	}
	if err = progress("hashing", 0); err != nil {
		return receipt, err
	}
	buf := make([]byte, UploadBlockSize)
	hashes := []string{}
	for {
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		n, readErr := io.ReadFull(f, buf)
		if n > 0 || len(hashes) == 0 {
			sum := md5.Sum(buf[:n])
			hashes = append(hashes, hex.EncodeToString(sum[:]))
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return receipt, readErr
		}
	}
	blocks, _ := json.Marshal(hashes)
	form := url.Values{"path": {remote}, "size": {strconv.FormatInt(info.Size(), 10)}, "isdir": {"0"}, "autoinit": {"1"}, "rtype": {"1"}, "block_list": {string(blocks)}}
	endpoint := "https://pan.baidu.com/rest/2.0/xpan/file?"
	query := url.Values{"method": {"precreate"}, "access_token": {c.AccessToken}}
	if err = progress("submitting", 0); err != nil {
		return receipt, err
	}
	pre, err := c.uploadRequest(ctx, endpoint+query.Encode(), "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return receipt, err
	}
	// A rapid-upload reply is not itself sufficient identity evidence. Verify its
	// returned file identity instead of treating missing receipt fields as success.
	if pre.ReturnType == 2 {
		if pre.FsID <= 0 || pre.Path == "" || pre.Size != info.Size() {
			return receipt, ErrUploadUnconfirmed
		}
		return UploadReceipt{pre.Path, strconv.FormatInt(pre.FsID, 10), strconv.FormatInt(pre.Size, 10)}, nil
	}
	if pre.ReturnType != 1 || pre.UploadID == "" {
		return receipt, ErrUploadUnconfirmed
	}
	seen := map[int]bool{}
	var sent int64
	for _, part := range pre.BlockList {
		if part < 0 || part >= len(hashes) || seen[part] {
			return receipt, ErrUploadUnconfirmed
		}
		seen[part] = true
		if err = progress("uploading", sent); err != nil {
			return receipt, err
		}
		offset := int64(part) * UploadBlockSize
		length := min(int64(UploadBlockSize), info.Size()-offset)
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		field, e := writer.CreateFormFile("file", "part")
		if e != nil {
			return receipt, e
		}
		if _, e = io.Copy(field, io.NewSectionReader(f, offset, length)); e != nil {
			return receipt, e
		}
		if e = writer.Close(); e != nil {
			return receipt, e
		}
		q := url.Values{"method": {"upload"}, "type": {"tmpfile"}, "access_token": {c.AccessToken}, "path": {remote}, "uploadid": {pre.UploadID}, "partseq": {strconv.Itoa(part)}}
		result, e := c.uploadRequest(ctx, "https://d.pcs.baidu.com/rest/2.0/pcs/superfile2?"+q.Encode(), writer.FormDataContentType(), &body)
		if e != nil {
			return receipt, e
		}
		if result.MD5 != hashes[part] {
			return receipt, errors.New("上传分片校验不一致")
		}
		sent += length
	}
	if err = progress("finalizing", sent); err != nil {
		return receipt, err
	}
	query.Set("method", "create")
	form.Set("uploadid", pre.UploadID)
	result, err := c.uploadRequest(ctx, endpoint+query.Encode(), "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return receipt, err
	}
	if result.FsID <= 0 || result.Path == "" || result.Size != info.Size() {
		return receipt, ErrUploadUnconfirmed
	}
	return UploadReceipt{result.Path, strconv.FormatInt(result.FsID, 10), strconv.FormatInt(result.Size, 10)}, nil
}
