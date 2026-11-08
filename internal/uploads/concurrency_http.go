package uploads

import (
	"encoding/json"
	"io"
	"net/http"
)

func (m *Manager) HandleConcurrency(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		write(w, m.ConcurrencyStatus())
	case http.MethodPost:
		var input struct {
			Limit *int `json:"limit"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Limit == nil || (*input.Limit != 1 && *input.Limit != 2) {
			http.Error(w, "上传并发只能设置为1或2", 400)
			return
		}
		if err := m.SetConcurrency(*input.Limit); err != nil {
			http.Error(w, "并发设置未保存，请刷新核对后再操作", 409)
			return
		}
		write(w, m.ConcurrencyStatus())
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
