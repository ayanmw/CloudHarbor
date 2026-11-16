package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func privateName(name string) bool {
	// Windows aliases case variants to the same private files; keep the public
	// namespace consistent on all supported platforms as well.
	name = strings.ToLower(name)
	return name == ".nas-downloader" || name == ".env" || name == ".git" || name == ".nas-sync-history"
}
func validRelative(p string) error {
	if p == "" {
		return nil
	}
	if strings.ContainsAny(p, "\\:\x00\r\n") || path.IsAbs(p) || path.Clean(p) != p {
		return ErrUnsafe
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." || part == "" || privateName(part) || strings.TrimRight(part, ". ") != part {
			return ErrUnsafe
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return ErrUnsafe
		}
	}
	return nil
}
func overlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		r, e := filepath.Rel(pair[0], pair[1])
		if e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r) {
			return true
		}
	}
	return false
}
func checkAbsoluteComponents(p string) error {
	for {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || reparse(info) {
			return ErrUnsafe
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}
func components(r *os.Root, p string) error {
	if err := validRelative(p); err != nil {
		return err
	}
	current := ""
	for _, part := range strings.Split(p, "/") {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		info, err := r.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || reparse(info) {
			return ErrUnsafe
		}
	}
	return nil
}
func openChecked(r *os.Root, p string) (*os.File, os.FileInfo, error) {
	if err := components(r, p); err != nil {
		return nil, nil, err
	}
	name := p
	if name == "" {
		name = "."
	}
	f, err := r.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, ErrUnsafe
	}
	if err = components(r, p); err != nil {
		f.Close()
		return nil, nil, err
	}
	current, err := r.Stat(name)
	if err != nil || !os.SameFile(info, current) {
		f.Close()
		return nil, nil, ErrChanged
	}
	return f, info, nil
}
func (m *Manager) scan(ctx context.Context, c Config, p string) (Listing, error) {
	r, err := m.openRoot(c)
	if err != nil {
		return Listing{}, err
	}
	defer r.Close()
	f, before, err := openChecked(r, p)
	if err != nil {
		return Listing{}, err
	}
	defer f.Close()
	if !before.IsDir() {
		return Listing{}, ErrUnsafe
	}
	l := Listing{Path: p, RootRevision: c.Revision, Entries: []Entry{}}
	count := 0
	for {
		batch, readErr := f.ReadDir(256)
		for _, item := range batch {
			if err = ctx.Err(); err != nil {
				return Listing{}, err
			}
			count++
			if count > 100000 {
				return Listing{}, errors.New("当前目录超过100000项，本版不发布不完整观察")
			}
			child := path.Join(p, item.Name())
			if validRelative(child) != nil {
				l.Excluded++
				continue
			}
			initial, err := r.Lstat(child)
			if err != nil {
				return Listing{}, err
			}
			if initial.Mode()&os.ModeSymlink != 0 || reparse(initial) || (!initial.IsDir() && !initial.Mode().IsRegular()) {
				l.Excluded++
				continue
			}
			h, info, err := openChecked(r, child)
			if err != nil {
				return Listing{}, err
			}
			id, err := identity(h)
			h.Close()
			if err != nil {
				return Listing{}, err
			}
			size := info.Size()
			if info.IsDir() {
				size = 0
			}
			l.Entries = append(l.Entries, Entry{Name: item.Name(), Path: child, ID: id, Directory: info.IsDir(), Size: size, ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano), Revision: observationRevision(c, child, id, info)})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return Listing{}, readErr
		}
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		return Listing{}, ErrChanged
	}
	check, err := m.openRoot(c)
	if err != nil {
		return Listing{}, err
	}
	check.Close()
	l.ObservedAt = utcNow()
	return l, nil
}
