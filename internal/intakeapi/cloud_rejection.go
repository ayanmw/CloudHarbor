package intakeapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

// A mutation without a journal ID has not reached the provider write in
// MutateCloud. Only classify known admission errors; never infer this from an
// arbitrary HTTP status or a lost/malformed provider receipt.
func writeCloudMutationRejection(w http.ResponseWriter, err error) bool {
	code, message, status := "", "", http.StatusConflict
	switch {
	case errors.Is(err, downloader.ErrIntakeInvalid):
		code, message, status = "invalid_request", "请求参数或路径无效，本次未执行。请刷新目录并重新选择。", http.StatusBadRequest
	case errors.Is(err, downloader.ErrIntakeStale):
		code, message = "identity_changed", "网盘项目身份或账号状态已变化，本次未执行。请刷新目录并重新核对。"
	case errors.Is(err, downloader.ErrIntakeConflict):
		code, message = "conflict", "项目或目标状态存在冲突，本次未执行。请刷新目录，检查同名项、任务维护或待核对记录。"
	case errors.Is(err, downloader.ErrAuthenticationWaiting):
		code, message = "authentication_waiting", "账号授权尚未就绪，本次未执行。请先完成授权或续期，再刷新目录。"
	default:
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// These messages are fixed UI text, not raw provider errors or credentials.
	_ = json.NewEncoder(w).Encode(map[string]string{"outcome": "not_started", "code": code, "message": message})
	return true
}
