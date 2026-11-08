package uploads

import (
	"errors"
	"path"
	"sort"
)

// RestoreDirectoryIsolation bridges durable upload uncertainty to namespace
// admission without rewriting a factual confirmed provider-operation receipt.
// Startup must keep readiness false until this method succeeds.
func (m *Manager) RestoreDirectoryIsolation(isolate func(string) error) error {
	if isolate == nil {
		return errors.New("缺少目录隔离恢复入口")
	}
	rows, err := m.db.Query(`SELECT b.target,d.relative_path FROM upload_directories d JOIN batches b ON b.id=d.batch WHERE d.state IN ('unknown','sending')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	paths := map[string]bool{}
	for rows.Next() {
		var target, relative string
		if err = rows.Scan(&target, &relative); err != nil {
			return err
		}
		if !validPath(target) {
			return errors.New("目录隔离目标无效")
		}
		if _, err = manifestDirectories(nil, []string{relative}); err != nil {
			return err
		}
		paths[path.Join(target, relative)] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	ordered := make([]string, 0, len(paths))
	for remote := range paths {
		ordered = append(ordered, remote)
	}
	sort.Strings(ordered)
	for _, remote := range ordered {
		if err = isolate(remote); err != nil {
			return err
		}
	}
	return nil
}
