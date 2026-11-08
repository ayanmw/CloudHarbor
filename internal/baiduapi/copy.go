package baiduapi

import "errors"

var ErrCopyUnconfirmed = errors.New("copy response does not confirm synchronous completion")
var ErrCopyAsync = errors.New("provider accepted an unexpected asynchronous copy")

type AsyncCopyError struct{ TaskID string }

func (e *AsyncCopyError) Error() string { return ErrCopyAsync.Error() }
func (e *AsyncCopyError) Unwrap() error { return ErrCopyAsync }

// CopyFile requires a synchronous single-item receipt and refuses overwrite.
// A missing reply never causes an implicit retry; the caller must retain evidence.
func (c *Client) CopyFile(source, destination, name string) error {
	return c.transferFile("copy", source, destination, name)
}
