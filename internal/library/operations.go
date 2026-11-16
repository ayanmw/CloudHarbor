package library

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
)

const privateArea = ".cloudharbor-library"
const MaxUploadBytes int64 = 50 << 30

var operationIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func sameRequest(a, b WriteRequest) bool { return a == b }
func rootName(p string) string {
	if p == "" {
		return "."
	}
	return p
}
func parentPath(p string) string {
	v := path.Dir(p)
	if v == "." {
		return ""
	}
	return v
}
func parentIdentity(r *os.Root, p string) (string, error) {
	f, i, e := openChecked(r, p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	if !i.IsDir() {
		return "", ErrUnsafe
	}
	return identity(f)
}
func checkedItem(r *os.Root, p string) (string, os.FileInfo, error) {
	f, i, e := openChecked(r, p)
	if e != nil {
		return "", nil, e
	}
	defer f.Close()
	id, e := identity(f)
	return id, i, e
}

// Internal paths never come from a client; only recorded IDs and fixed names.
func internalOpen(r *os.Root, p string) (*os.File, error) {
	current := ""
	for _, part := range strings.Split(p, "/") {
		current = path.Join(current, part)
		i, e := r.Lstat(current)
		if e != nil {
			return nil, e
		}
		if i.Mode()&os.ModeSymlink != 0 || reparse(i) {
			return nil, ErrUnsafe
		}
	}
	f, e := r.Open(p)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil || (!i.IsDir() && !i.Mode().IsRegular()) {
		f.Close()
		return nil, ErrUnsafe
	}
	at, e := r.Lstat(p)
	if e != nil || !os.SameFile(i, at) {
		f.Close()
		return nil, ErrChanged
	}
	return f, nil
}
func (m *Manager) privateRoot(r *os.Root, c Config) error {
	var owner string
	if e := m.db.QueryRow(`SELECT token FROM library_owner WHERE singleton=1`).Scan(&owner); e != nil {
		return e
	}
	marker, _ := json.Marshal(struct{ Root, Owner string }{c.RootID, owner})
	made := false
	if e := r.Mkdir(privateArea, 0700); e == nil {
		made = true
	} else if !errors.Is(e, os.ErrExist) {
		return e
	}
	if made {
		f, e := r.OpenFile(privateArea+"/owner.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(marker)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
	}
	f, e := internalOpen(r, privateArea+"/owner.json")
	if e != nil {
		return fmt.Errorf("私有区所有权无法核对: %w", e)
	}
	b, e := io.ReadAll(io.LimitReader(f, 4096))
	f.Close()
	if e != nil || !bytes.Equal(b, marker) {
		return ErrUnsafe
	}
	for _, name := range []string{"staging", "trash"} {
		p := privateArea + "/" + name
		if e = r.Mkdir(p, 0700); e != nil && !errors.Is(e, os.ErrExist) {
			return e
		}
		f, e = internalOpen(r, p)
		if e != nil {
			return e
		}
		i, e := f.Stat()
		f.Close()
		if e != nil || !i.IsDir() {
			return ErrUnsafe
		}
	}
	return syncParent(r, ".")
}
func (m *Manager) Write(ctx context.Context, req WriteRequest, body io.Reader) (op Operation, err error) {
	if !m.mu.TryLock() {
		return Operation{}, ErrBusy
	}
	defer m.mu.Unlock()
	if m.closed {
		return op, os.ErrClosed
	}
	if !operationIDPattern.MatchString(req.ID) {
		return op, ErrUnsafe
	}
	if prior, e := m.operation(req.ID); e == nil {
		if !sameRequest(prior.Request, req) {
			return op, ErrConflict
		}
		if prior.Status == "succeeded" {
			return prior, nil
		}
		if prior.Status == "unknown" || prior.Status == "prepared" {
			return prior, ErrUncertain
		}
		return prior, fmt.Errorf("此操作已失败: %s", prior.Error)
	} else if !errors.Is(e, os.ErrNotExist) {
		return op, e
	}
	if e := m.checkUncertain(); e != nil {
		return op, e
	}
	if e := ctx.Err(); e != nil {
		return op, e
	}
	c, e := m.config()
	if e != nil {
		return op, e
	}
	if req.RootRevision != c.Revision {
		return op, ErrConflict
	}
	r, e := m.openRoot(c)
	if e != nil {
		return op, e
	}
	defer r.Close()
	op = Operation{ID: req.ID, Request: req, Status: "prepared", StartedAt: utcNow(), RootID: c.RootID, RootPath: c.Root}
	source, target := req.Path, req.Target
	var sourceID string
	switch req.Kind {
	case "upload", "mkdir":
		if target == "" || validRelative(target) != nil || req.Path != "" || req.Revision != "" || req.TrashID != "" {
			return Operation{}, ErrUnsafe
		}
		if req.Kind == "upload" && (req.Size < 0 || req.Size > MaxUploadBytes) {
			return Operation{}, ErrUnsafe
		}
	case "rename", "move", "trash":
		if source == "" || validRelative(source) != nil || req.TrashID != "" {
			return Operation{}, ErrUnsafe
		}
		id, i, e := checkedItem(r, source)
		if e != nil {
			return Operation{}, e
		}
		if observationRevision(c, source, id, i) != req.Revision {
			return Operation{}, ErrChanged
		}
		sourceID = id
		op.ItemID = id
		op.OriginalParentID, e = parentIdentity(r, parentPath(source))
		if e != nil {
			return Operation{}, e
		}
		if req.Kind == "trash" {
			if target != "" || req.ParentID != "" {
				return Operation{}, ErrUnsafe
			}
			target = privateArea + "/trash/" + req.ID
		} else {
			if target == "" || validRelative(target) != nil || target == source || strings.HasPrefix(target, source+"/") {
				return Operation{}, ErrUnsafe
			}
			if req.Kind == "rename" && parentPath(source) != parentPath(target) {
				return Operation{}, ErrUnsafe
			}
		}
	case "restore":
		if req.Path != "" || req.Target != "" || req.ParentID != "" || req.Revision != "" || !operationIDPattern.MatchString(req.TrashID) {
			return Operation{}, ErrUnsafe
		}
		old, e := m.operation(req.TrashID)
		if e != nil {
			return Operation{}, e
		}
		if old.Request.Kind != "trash" || old.Status != "succeeded" || old.RestoredBy != "" || old.RootID != c.RootID {
			return Operation{}, ErrConflict
		}
		source = privateArea + "/trash/" + old.ID
		target = old.Request.Path
		sourceID = old.ItemID
		op.ItemID = sourceID
		op.OriginalParentID = old.OriginalParentID
		parent, e := parentIdentity(r, parentPath(target))
		if e != nil {
			return Operation{}, e
		}
		if parent != old.OriginalParentID {
			return Operation{}, ErrChanged
		}
	default:
		return Operation{}, ErrUnsafe
	}
	if req.Kind != "trash" && req.Kind != "restore" {
		pid, e := parentIdentity(r, parentPath(target))
		if e != nil {
			return Operation{}, e
		}
		if req.ParentID == "" || pid != req.ParentID {
			return Operation{}, ErrChanged
		}
	}
	if req.Kind != "trash" {
		if _, e = r.Lstat(target); e == nil {
			return Operation{}, os.ErrExist
		} else if !errors.Is(e, os.ErrNotExist) {
			return Operation{}, e
		}
	}
	op.ItemPath = req.Path
	if op.ItemPath == "" {
		op.ItemPath = target
	}
	if e = m.prepareOperation(op); e != nil {
		return Operation{}, e
	}
	stagePath, stageID := "", ""
	uncertain := false
	defer func() {
		if err != nil && !uncertain && stageID != "" {
			if f, e := internalOpen(r, stagePath); e == nil {
				id, ie := identity(f)
				f.Close()
				if ie == nil && id == stageID {
					if ce := r.Remove(stagePath); ce != nil {
						err = fmt.Errorf("%w; 临时上传清理失败: %v", err, ce)
					}
				}
			}
		}
		op.FinishedAt = utcNow()
		if err == nil {
			op.Status = "succeeded"
		} else {
			op.Status = "failed"
			op.Error = err.Error()
			if uncertain {
				op.Status = "unknown"
			}
		}
		if saveErr := m.finishOperation(op); saveErr != nil {
			op.Status = "unknown"
			op.Error = "操作结果无法持久保存"
			err = fmt.Errorf("%w: %v", ErrUncertain, saveErr)
		}
		log.Printf("[Library] at_utc=%s operation_id=%q kind=%q path=%q target=%q status=%q bytes=%d error=%q", op.FinishedAt, req.ID, req.Kind, source, target, op.Status, op.Bytes, op.Error)
	}()
	// Persist the intent before initializing our private area; a foreign area is never adopted.
	if e = m.privateRoot(r, c); e != nil {
		return op, e
	}
	if req.Kind == "restore" {
		f, e := internalOpen(r, source)
		if e != nil {
			return op, e
		}
		id, e := identity(f)
		f.Close()
		if e != nil || id != sourceID {
			return op, ErrChanged
		}
	}

	if req.Kind == "upload" {
		if body == nil {
			return op, errors.New("上传内容缺失")
		}
		stage := privateArea + "/staging/" + req.ID
		f, e := r.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return op, e
		}
		stagePath = stage
		stageID, e = identity(f)
		if e != nil {
			f.Close()
			return op, e
		}
		hash := sha256.New()
		op.Bytes, e = copyUpload(ctx, io.MultiWriter(f, hash), body, req.Size)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return op, e
		}
		op.SHA256 = hex.EncodeToString(hash.Sum(nil))
		source = stage
		f, e = internalOpen(r, stage)
		if e != nil {
			return op, e
		}
		sourceID, e = identity(f)
		if e != nil || sourceID != stageID {
			f.Close()
			return op, ErrChanged
		}
		// Re-read the synced staging bytes: the receipt describes the published file,
		// not merely bytes that once passed through the network stream.
		verify := sha256.New()
		_, e = copyUpload(ctx, verify, f, req.Size)
		f.Close()
		if e != nil {
			return op, e
		}
		if hex.EncodeToString(verify.Sum(nil)) != op.SHA256 {
			return op, ErrChanged
		}
	}
	if e = ctx.Err(); e != nil {
		return op, e
	}
	check, e := m.openRoot(c)
	if e != nil {
		return op, e
	}
	check.Close()
	expectedParent := req.ParentID
	if req.Kind == "trash" {
		expectedParent, e = rawDirectoryIdentity(r, parentPath(target))
		if e != nil {
			return op, e
		}
	}
	if req.Kind == "restore" {
		expectedParent = op.OriginalParentID
	}
	if req.Kind == "rename" || req.Kind == "move" || req.Kind == "trash" {
		id, i, e := checkedItem(r, req.Path)
		if e != nil {
			return op, e
		}
		if observationRevision(c, req.Path, id, i) != req.Revision {
			return op, ErrChanged
		}
	}
	if req.Kind == "mkdir" {
		uncertain = true
		err = mkdirChecked(r, target, expectedParent)
	} else {
		uncertain = true
		err = renameNoReplace(r, source, target, sourceID, expectedParent)
	}
	if err != nil {
		if errors.Is(err, os.ErrExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrInvalid) || errors.Is(err, errors.ErrUnsupported) || errors.Is(err, ErrBusy) || errors.Is(err, ErrChanged) || errors.Is(err, ErrUnsafe) {
			uncertain = false
		}
		return op, err
	}
	f, e := internalOpen(r, target)
	if e != nil {
		return op, e
	}
	op.ItemID, e = identity(f)
	f.Close()
	if e != nil {
		return op, e
	}
	if sourceID != "" && op.ItemID != sourceID {
		return op, ErrChanged
	}
	if e = syncParent(r, rootName(parentPath(target))); e != nil {
		return op, e
	}
	if source != "" && parentPath(source) != parentPath(target) {
		if e = syncParent(r, rootName(parentPath(source))); e != nil {
			return op, e
		}
	}
	uncertain = false
	return op, nil
}
func copyUpload(ctx context.Context, w io.Writer, r io.Reader, size int64) (int64, error) {
	var total int64
	buffer := make([]byte, 64*1024)
	reader := io.LimitReader(r, size+1)
	for {
		if e := ctx.Err(); e != nil {
			return total, e
		}
		n, e := reader.Read(buffer)
		if n > 0 {
			if total+int64(n) > size {
				return total, errors.New("上传内容超过声明大小")
			}
			written, we := w.Write(buffer[:n])
			total += int64(written)
			if we != nil {
				return total, we
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return total, e
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
	if total != size {
		return total, io.ErrUnexpectedEOF
	}
	return total, nil
}

func rawDirectoryIdentity(r *os.Root, p string) (string, error) {
	f, e := internalOpen(r, rootName(p))
	if e != nil {
		return "", e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !i.IsDir() {
		return "", ErrUnsafe
	}
	return identity(f)
}
func mkdirChecked(r *os.Root, target, expectedParent string) error {
	parent, e := r.OpenRoot(rootName(parentPath(target)))
	if e != nil {
		return e
	}
	defer parent.Close()
	f, e := parent.Open(".")
	if e != nil {
		return e
	}
	id, e := identity(f)
	f.Close()
	if e != nil || id != expectedParent {
		return ErrChanged
	}
	return parent.Mkdir(path.Base(target), 0755)
}
