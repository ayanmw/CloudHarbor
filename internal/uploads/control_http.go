package uploads

import (
	"database/sql"
	"errors"
	"net/http"
	"path"
)

func (m *Manager) HandleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	batch, item := r.PathValue("batch"), r.PathValue("item")
	if !validID(batch) || !validID(item) || r.URL.RawQuery != "" {
		http.Error(w, "上传控制请求无效", 400)
		return
	}
	action := path.Base(r.URL.Path)
	var state string
	var err error
	switch action {
	case "pause":
		state, err = m.Pause(batch, item)
	case "resume":
		err = m.Resume(batch, item)
		state = "queued"
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), 409)
		}
		return
	}
	write(w, map[string]string{"batch_id": batch, "item_id": item, "state": state})
}
