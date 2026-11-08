package intakeapi

import (
	"context"
	"net/http"
	"time"
)

type cleanupRevisionBackend interface {
	CloudCleanupRevision(context.Context) (string, error)
}

func (s *Server) HandleCloudCleanupRevision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "GET") {
		return
	}
	b, ok := s.backend.(cleanupRevisionBackend)
	if !ok {
		http.Error(w, "当前服务不支持清理变化观察。", 503)
		return
	}
	revision, err := b.CloudCleanupRevision(r.Context())
	if err != nil {
		cleanupHTTPFailure(w, err, cleanupReadFailureMessage)
		return
	}
	jsonResponse(w, map[string]any{"revision": revision, "observed_at_utc": time.Now().UTC()})
}
