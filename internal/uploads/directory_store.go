package uploads

import (
	"errors"
	"path"
	"sort"
	"strings"
)

// Directory is a namespace operation, never a zero-byte upload or staging file.
type Directory struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	State       string `json:"state"`
	Updated     string `json:"updated_at_utc"`
	ActualPath  string `json:"actual_path"`
	OperationID string `json:"operation_id"`
	Error       string `json:"error"`
}

func (m *Manager) initDirectoryStore() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS upload_directories(
 id TEXT PRIMARY KEY,batch TEXT NOT NULL,relative_path TEXT NOT NULL,state TEXT NOT NULL,
 updated TEXT NOT NULL,actual_path TEXT NOT NULL DEFAULT '',operation_id TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',
 UNIQUE(batch,relative_path));
 CREATE INDEX IF NOT EXISTS upload_directories_batch ON upload_directories(batch);
 CREATE TABLE IF NOT EXISTS upload_directory_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 directory TEXT NOT NULL,state TEXT NOT NULL,recorded TEXT NOT NULL,evidence TEXT NOT NULL DEFAULT '');`)
	if err != nil {
		return err
	}
	return m.recoverDirectoryOperations()
}

// A restart cannot prove a namespace write completed. State and historical
// observation are committed together, preserving the original operation ID.
func (m *Manager) recoverDirectoryOperations() error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`INSERT INTO upload_directory_events(directory,state,recorded,evidence)
 SELECT id,'unknown',?,'restart_recovery: previous_state='||state||'; previous_updated_at_utc='||updated||'; action=isolated_unconfirmed_directory_write'
 FROM upload_directories WHERE state='sending'`, at); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE upload_directories SET state='unknown',updated=?,error='服务重启时目录操作尚未确认；未自动重发' WHERE state='sending'`, at); err != nil {
		return err
	}
	return tx.Commit()
}

func manifestDirectories(files []Input, directories []string) ([]string, error) {
	if len(files)+len(directories) == 0 || len(files)+len(directories) > 10000 {
		return nil, errors.New("上传清单须包含1至10000个文件或目录")
	}
	filePaths := make(map[string]bool, len(files))
	for _, file := range files {
		filePaths[file.Path] = true
	}
	seen := make(map[string]bool, len(directories))
	for _, relative := range directories {
		if relative == "" || relative == "." || relative == ".." || strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, "../") || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\\x00") {
			return nil, errors.New("目录相对路径无效")
		}
		for _, part := range strings.Split(relative, "/") {
			if len([]byte(part)) > 255 || strings.IndexFunc(part, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
				return nil, errors.New("目录名称无效")
			}
		}
		for candidate := relative; candidate != "."; candidate = path.Dir(candidate) {
			if filePaths[candidate] {
				return nil, errors.New("文件与目录路径冲突")
			}
		}
		seen[relative] = true
	}
	out := make([]string, 0, len(seen))
	for relative := range seen {
		out = append(out, relative)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if a != b {
			return a < b
		}
		return out[i] < out[j]
	})
	return out, nil
}

func (m *Manager) directories(batch string) ([]Directory, error) {
	rows, err := m.db.Query(`SELECT id,relative_path,state,updated,actual_path,operation_id,error FROM upload_directories WHERE batch=? ORDER BY length(relative_path)-length(replace(relative_path,'/','')),relative_path`, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Directory{}
	for rows.Next() {
		var d Directory
		if err = rows.Scan(&d.ID, &d.Path, &d.State, &d.Updated, &d.ActualPath, &d.OperationID, &d.Error); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
