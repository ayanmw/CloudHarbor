package baiduapi

import (
	"context"
	"errors"
	"strconv"
	"time"
)

type UploadReceiptVerification struct {
	Receipt       UploadReceipt      `json:"receipt"`
	CurrentScope  UploadAccountScope `json:"current_scope"`
	RemoteMtime   int64              `json:"remote_mtime,string"`
	ObservedAtUTC time.Time          `json:"observed_at_utc"`
}

// VerifyUploadReceipt issues one metadata GET for the saved fsid. It never
// recreates paths, uploads data, searches by name, or substitutes another file.
func (c *Client) VerifyUploadReceipt(ctx context.Context, original UploadAccountScope, receipt UploadReceipt) (UploadReceiptVerification, error) {
	var empty UploadReceiptVerification
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	current, err := c.uploadAccountScope(ctx)
	if err != nil {
		return empty, err
	}
	if current == nil || !original.Valid() || original.AccountFingerprint != current.AccountFingerprint || original.AppKeyFingerprint != current.AppKeyFingerprint || !receipt.Valid() {
		return empty, errors.New("upload receipt verification requires matching account/application and saved identity")
	}
	fsid, _ := strconv.ParseInt(receipt.FsID, 10, 64)
	info, err := c.WithContext(ctx).GetFileInfo(fsid)
	if err != nil {
		return empty, errors.Join(ErrUploadUnconfirmed, err)
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if info.Path != receipt.Path || strconv.FormatInt(info.Size, 10) != receipt.Size || info.IsDir != 0 {
		return empty, ErrUploadUnconfirmed
	}
	return UploadReceiptVerification{Receipt: receipt, CurrentScope: *current, RemoteMtime: info.Mtime, ObservedAtUTC: time.Now().UTC()}, nil
}
