package web

import (
	"net/http"
)

// PageHandler 返回嵌入的前端页面（暂时占位，后续用 main.go 的 embed 实现或迁移 html）
func PageHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
