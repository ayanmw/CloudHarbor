package uploads

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
	"sync"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func (m *Manager) namespaceAdmissionGuard(item, attempt string) baiduapi.UploadNamespaceAdmissionGuard {
	return func(target, operation string) (func(error), error) {
		m.mu.Lock()
		fail := func(err error) (func(error), error) { m.mu.Unlock(); return nil, err }
		if !validID(item) || !validID(attempt) || !validID(operation) || m.activeAttempts[item].ID != attempt || m.activeAttempts[item].RunnerReturned {
			return fail(errors.New("namespace admission outside active upload attempt"))
		}
		if err := m.checkMutationAdmissionLocked(item); err != nil {
			return fail(err)
		}
		var root, relative, batch string
		if err := m.db.QueryRow(`SELECT b.target,i.relative_path,b.id FROM items i JOIN batches b ON b.id=i.batch WHERE i.id=?`, item).Scan(&root, &relative, &batch); err != nil {
			return fail(err)
		}
		parent := path.Dir(path.Join(root, relative))
		if !validPath(target) || target == "/" || (target != parent && !strings.HasPrefix(parent, target+"/")) {
			return fail(errors.New("namespace admission path is not an upload parent"))
		}
		at := now()
		evidence, _ := json.Marshal(map[string]string{"event": "upload_namespace_admission_v1", "evidence_type": "local_send_intent", "attempt_id": attempt, "batch_id": batch, "item_id": item, "operation_id": operation, "requested_path": target, "admitted_at_utc": at})
		if _, err := m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'namespace_admitted',sent,?,? FROM items WHERE id=?`, at, string(evidence), item); err != nil {
			return fail(m.evidencePersistenceFailureLocked(err))
		}
		var once sync.Once
		return func(persistErr error) {
			once.Do(func() { m.evidencePersistenceFailureLocked(persistErr); m.mu.Unlock() })
		}, nil
	}
}
