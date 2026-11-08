package foldersync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const privateDirectory = ".nas-sync-history"

func (m *Manager) exclusions() []string {
	paths := m.forbidden
	if m.resolveForbidden != nil {
		paths = m.resolveForbidden()
	}
	result := []string{}
	for _, p := range paths {
		if absolute, err := filepath.Abs(p); err == nil {
			result = append(result, absolute)
		}
	}
	return result
}

func safeRelative(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.TrimRight(part, ". ") != part || strings.ContainsAny(part, "<>\"|?*\r\n") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
	}
	return p != "" && p != "." && p != ".." && !strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "\\:\x00") && filepath.ToSlash(filepath.Clean(p)) == p && !strings.HasPrefix(p, "../") && strings.Split(p, "/")[0] != privateDirectory
}
func localPath(root, p string) (string, error) {
	if !safeRelative(p) {
		return "", fmt.Errorf("同步相对路径无效")
	}
	target := filepath.Join(root, filepath.FromSlash(p))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("同步路径越界")
	}
	current := root
	parts := strings.Split(p, "/")
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return "", fmt.Errorf("同步不允许链接或重解析点")
		}
	}
	return target, nil
}
func localEntry(ctx context.Context, root, p string) (*Entry, error) {
	target, err := localPath(root, p)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("同步不支持此文件类型")
	}
	id, err := fileIdentity(target, info)
	if err != nil {
		return nil, err
	}
	e := &Entry{Path: p, ID: id, Directory: info.IsDir(), Size: info.Size(), Mtime: info.ModTime().UnixNano(), MtimeUnit: "unix_nanoseconds", Observed: time.Now().UTC()}
	if e.Directory {
		e.Size = 0
		return e, nil
	}
	f, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("本地文件在读取期间变化")
	}
	e.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return e, nil
}
func ScanLocal(ctx context.Context, root string) ([]Entry, error) {
	result := []Entry{}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == privateDirectory && d.IsDir() {
			return filepath.SkipDir
		}
		if !safeRelative(rel) {
			return fmt.Errorf("同步包含不兼容路径：%s", rel)
		}
		fold := strings.ToLower(rel)
		if seen[fold] {
			return fmt.Errorf("同步存在大小写冲突：%s", rel)
		}
		seen[fold] = true
		e, err := localEntry(ctx, root, rel)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("本地扫描期间项目消失")
		}
		result = append(result, *e)
		return nil
	})
	return result, err
}

func rootIdentity(root string) (string, error) {
	for current := filepath.Clean(root); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return "", fmt.Errorf("本地根目录的祖先包含链接")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
		return "", fmt.Errorf("请选择真实本地目录，不能使用链接")
	}
	return fileIdentity(root, info)
}
func overlap(a, b string) bool {
	a = strings.ToLower(filepath.Clean(a))
	b = strings.ToLower(filepath.Clean(b))
	inside := func(root, candidate string) bool {
		rel, err := filepath.Rel(root, candidate)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	return inside(a, b) || inside(b, a)
}
