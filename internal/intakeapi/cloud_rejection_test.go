package intakeapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

func TestCloudMutationExplicitNotStartedRejection(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   string
		status int
	}{
		{downloader.ErrIntakeInvalid, "invalid_request", 400}, {downloader.ErrIntakeStale, "identity_changed", 409}, {downloader.ErrIntakeConflict, "conflict", 409}, {downloader.ErrAuthenticationWaiting, "authentication_waiting", 409},
	} {
		t.Run(tc.code, func(t *testing.T) {
			backend := &fakeBackend{mutationErr: tc.err}
			w := httptest.NewRecorder()
			New(backend, nil).HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{}`)))
			var got struct{ Outcome, Code, Message string }
			err := json.Unmarshal(w.Body.Bytes(), &got)
			if w.Code != tc.status || err != nil || got.Outcome != "not_started" || got.Code != tc.code || !strings.Contains(got.Message, "未执行") || backend.mutationCalls != 1 {
				t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body.String(), err)
			}
		})
	}
}

func TestCloudMutationNeverLabelsIssuedOrUnclassifiedErrorsNotStarted(t *testing.T) {
	for _, status := range []string{"unknown", "failed", "confirmed", ""} {
		t.Run(status, func(t *testing.T) {
			result := downloader.CloudMutationResult{}
			err := error(downloader.ErrIntakeConflict)
			if status != "" {
				result.OperationID = strings.Repeat("a", 32)
				result.Status = status
			} else {
				err = errors.New("unclassified failure")
			}
			w := httptest.NewRecorder()
			New(&fakeBackend{mutationErr: err, mutationResult: result}, nil).HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{}`)))
			if strings.Contains(w.Body.String(), `"not_started"`) {
				t.Fatalf("issued or unknown request mislabeled: %s", w.Body.String())
			}
			if status != "" && !strings.Contains(w.Body.String(), result.OperationID) {
				t.Fatalf("lost operation receipt: %s", w.Body.String())
			}
		})
	}
}
