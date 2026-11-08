package baiduapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
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

func (c *Client) uploadRequest(ctx context.Context, address, contentType string, body io.Reader, observe func(time.Time, time.Duration) error) (value uploadResponse, resultErr error) {
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
	if payload, ok := body.(*uploadPayloadReader); ok {
		req.ContentLength = payload.contentLength
	}
	client := http.Client{Timeout: 5 * time.Minute, Transport: metadataHTTPClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	if observe != nil {
		defer func() { resultErr = errors.Join(resultErr, observe(started, time.Since(started))) }()
	}
	resp, err := client.Do(req)
	if err != nil {
		return value, fmt.Errorf("%w: %v", ErrUploadUnconfirmed, requestError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 || resp.StatusCode >= 500 || resp.StatusCode == 408 {
		return value, ErrUploadUnconfirmed
	}
	if json.Unmarshal(raw, &value) != nil {
		return value, ErrUploadUnconfirmed
	}
	// PCS part replies use MD5 without XPan's errno. Restrict this exception to
	// superfile2; the caller still verifies the exact expected part digest.
	pcsPart := req.URL.Host == "d.pcs.baidu.com" && req.URL.Path == "/rest/2.0/pcs/superfile2" && req.URL.Query().Get("method") == "upload"
	if value.Errno == nil && pcsPart && resp.StatusCode == http.StatusOK && value.ErrorCode == 0 {
		digest, decodeErr := hex.DecodeString(value.MD5)
		if decodeErr == nil && len(digest) == md5.Size {
			return value, nil
		}
	}
	if value.Errno == nil && value.ErrorCode != 0 {
		return value, &APIError{Operation: "upload", Status: resp.StatusCode, Code: value.ErrorCode, tokenFingerprint: c.Fingerprint()}
	}
	if value.Errno == nil {
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
	return c.UploadLocalFileMeasured(ctx, local, remote, progress, nil)
}

// UploadLocalFileMeasured reports only actual part requests; callbacks must persist observations before merge.
func (c *Client) UploadLocalFileMeasured(ctx context.Context, local, remote string, progress func(string, int64) error, observe func(UploadTransferSample) error) (UploadReceipt, error) {
	var receipt UploadReceipt
	accountScope, scopeErr := c.uploadAccountScope(ctx)
	if scopeErr != nil {
		return receipt, scopeErr
	}
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
	localHash := sha256.New()
	for {
		if err = progress("hashing", 0); err != nil {
			return receipt, err
		}
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		n, readErr := io.ReadFull(f, buf)
		if n > 0 || len(hashes) == 0 {
			_, _ = localHash.Write(buf[:n])
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
	intent := UploadPrecreateIntent{RemotePath: remote, Size: info.Size(), LocalSHA256: hex.EncodeToString(localHash.Sum(nil)), BlockMD5: append([]string(nil), hashes...), CredentialFingerprint: c.Fingerprint(), AppKeyFingerprint: c.ApplicationFingerprint(), AccountScope: accountScope, ObservedAtUTC: time.Now().UTC()}
	if !intent.Valid() {
		return receipt, errors.New("invalid upload precreate intent")
	}
	if err = observeUploadPrecreate(ctx, UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
		return receipt, err
	}
	pre, err := c.uploadRequest(ctx, endpoint+query.Encode(), "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()), nil)
	if err != nil {
		return receipt, err
	}
	// A rapid-upload reply is not itself sufficient identity evidence. Verify its
	// returned file identity instead of treating missing receipt fields as success.
	if pre.ReturnType == 2 {
		if pre.FsID <= 0 || pre.Path == "" || path.Dir(pre.Path) != path.Dir(remote) || pre.Size != info.Size() {
			return receipt, ErrUploadUnconfirmed
		}
		receipt = UploadReceipt{pre.Path, strconv.FormatInt(pre.FsID, 10), strconv.FormatInt(pre.Size, 10)}
		rapid := UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: receipt, ObservedAtUTC: time.Now().UTC()}
		if !rapid.Valid() {
			return receipt, ErrUploadUnconfirmed
		}
		if err = observeUploadPrecreate(ctx, UploadPrecreateCheckpoint{RapidReceipt: &rapid}); err != nil {
			return receipt, errors.Join(ErrUploadUnconfirmed, err)
		}
		return receipt, nil
	}
	// An omitted/null list is not evidence that every part already exists.
	if pre.ReturnType != 1 || pre.UploadID == "" || pre.BlockList == nil {
		return receipt, ErrUploadUnconfirmed
	}
	seen := map[int]bool{}
	// Validate the complete provider plan before sending any part. Existing server
	// blocks are deliberately excluded from this attempt's remaining payload.
	for _, part := range pre.BlockList {
		if part < 0 || part >= len(hashes) || seen[part] {
			return receipt, ErrUploadUnconfirmed
		}
		seen[part] = true
	}
	appHash := sha256.Sum256([]byte(c.AppKey))
	plan := UploadSessionPlan{PrecreateDigest: intent.Digest(), CreateAdmissionVersion: 1, PartAdmissionVersion: 1, UploadID: pre.UploadID, RemotePath: remote, Size: info.Size(), BlockSize: UploadBlockSize, LocalSHA256: hex.EncodeToString(localHash.Sum(nil)), BlockMD5: append([]string(nil), hashes...), NeededParts: append([]int{}, pre.BlockList...), CredentialFingerprint: c.Fingerprint(), AppKeyFingerprint: hex.EncodeToString(appHash[:]), ObservedAtUTC: time.Now().UTC(), AccountScope: accountScope}
	if err = observeUploadSession(ctx, UploadSessionCheckpoint{Plan: &plan}); err != nil {
		return receipt, err
	}
	return c.uploadSessionParts(ctx, f, plan, pre.BlockList, 0, progress, observe)
}

// Both new and resumed uploads retain the same open staging handle through parts
// and merge. The caller must persist its appropriate plan/admission beforehand.
func (c *Client) uploadSessionParts(ctx context.Context, f *os.File, plan UploadSessionPlan, parts []int, previous int64, progress func(string, int64) error, observe func(UploadTransferSample) error) (UploadReceipt, error) {
	var receipt UploadReceipt
	var err error
	planDigest := plan.Digest()
	var planned int64
	for _, part := range parts {
		planned += min(int64(UploadBlockSize), plan.Size-int64(part)*UploadBlockSize)
	}
	blocks, _ := json.Marshal(plan.BlockMD5)
	form := url.Values{"path": {plan.RemotePath}, "size": {strconv.FormatInt(plan.Size, 10)}, "isdir": {"0"}, "autoinit": {"1"}, "rtype": {"1"}, "block_list": {string(blocks)}}
	endpoint := "https://pan.baidu.com/rest/2.0/xpan/file?"
	query := url.Values{"access_token": {c.AccessToken}}
	sent := previous
	for _, part := range parts {
		if err = progress("uploading", sent); err != nil {
			return receipt, err
		}
		offset := int64(part) * UploadBlockSize
		length := min(int64(UploadBlockSize), plan.Size-offset)
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		field, e := writer.CreateFormFile("file", "part")
		if e != nil {
			return receipt, e
		}
		payloadStart := int64(body.Len())
		if _, e = io.Copy(field, io.NewSectionReader(f, offset, length)); e != nil {
			return receipt, e
		}
		if e = writer.Close(); e != nil {
			return receipt, e
		}
		q := url.Values{"method": {"upload"}, "type": {"tmpfile"}, "access_token": {c.AccessToken}, "path": {plan.RemotePath}, "uploadid": {plan.UploadID}, "partseq": {strconv.Itoa(part)}}
		payload := newUploadPayloadReader(&body, payloadStart, length)
		if err = observeUploadSession(ctx, UploadSessionCheckpoint{Admission: &UploadPartAdmission{PlanDigest: planDigest, Index: part, MD5: plan.BlockMD5[part], ObservedAtUTC: time.Now().UTC()}}); err != nil {
			return receipt, err
		}
		var sample *UploadTransferSample
		result, e := c.uploadRequest(ctx, "https://d.pcs.baidu.com/rest/2.0/pcs/superfile2?"+q.Encode(), writer.FormDataContentType(), payload, func(started time.Time, elapsed time.Duration) error {
			sample = &UploadTransferSample{PlannedPayloadBytes: &planned, PlannedPartCount: len(parts), PartPayloadBytes: length, PartIndex: part, PayloadBytesRead: payload.payloadRead.Load(), ActiveNanos: int64(elapsed), StartedAtUTC: started.UTC(), FinishedAtUTC: time.Now().UTC()}
			return nil
		})
		if e == nil && result.MD5 != plan.BlockMD5[part] {
			e = errors.New("上传分片校验不一致")
		}
		if sample != nil && observe != nil {
			sample.PartConfirmed = e == nil
			e = errors.Join(e, observe(*sample))
		}
		if e != nil {
			return receipt, e
		}
		if err = observeUploadSession(ctx, UploadSessionCheckpoint{Part: &UploadPartReceipt{PlanDigest: planDigest, Index: part, MD5: result.MD5, ObservedAtUTC: time.Now().UTC()}}); err != nil {
			return receipt, err
		}
		sent += length
	}
	if err = progress("finalizing", sent); err != nil {
		return receipt, err
	}
	if err = observeUploadSession(ctx, UploadSessionCheckpoint{CreateAdmission: &UploadCreateAdmission{PlanDigest: planDigest, ObservedAtUTC: time.Now().UTC()}}); err != nil {
		return receipt, err
	}
	query.Set("method", "create")
	form.Set("uploadid", plan.UploadID)
	result, err := c.uploadRequest(ctx, endpoint+query.Encode(), "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()), nil)
	if err != nil {
		return receipt, err
	}
	if result.FsID <= 0 || result.Path == "" || path.Dir(result.Path) != path.Dir(plan.RemotePath) || result.Size != plan.Size {
		return receipt, ErrUploadUnconfirmed
	}
	receipt = UploadReceipt{result.Path, strconv.FormatInt(result.FsID, 10), strconv.FormatInt(result.Size, 10)}
	observed := UploadCreateReceipt{PlanDigest: planDigest, Receipt: receipt, ObservedAtUTC: time.Now().UTC()}
	if !observed.Valid() {
		return receipt, ErrUploadUnconfirmed
	}
	if err = observeUploadSession(ctx, UploadSessionCheckpoint{CreateReceipt: &observed}); err != nil {
		return receipt, errors.Join(ErrUploadUnconfirmed, err)
	}
	return receipt, nil
}
