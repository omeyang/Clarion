package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/omeyang/clarion/internal/api/schema"
)

// writeJSON 将 v 序列化为 JSON 并写入 HTTP 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// WriteHeader 已调用，无法再更改状态码，只能记录日志。
		slog.Error("JSON 编码响应失败", slog.String("error", err.Error()))
	}
}

// writeError 写入标准错误响应。
func writeError(w http.ResponseWriter, status int, msg, details string) {
	writeJSON(w, status, schema.ErrorResponse{Error: msg, Details: details})
}

// pathID 从请求路径中解析整型 ID 参数，解析失败时写入 400 响应并返回 false。
func pathID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(param), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+param, err.Error())
		return 0, false
	}
	return id, true
}

// defaultJSON 当 data 为 nil 时返回空 JSON 对象，避免数据库写入 NULL。
func defaultJSON(data json.RawMessage) json.RawMessage {
	if data == nil {
		return json.RawMessage(`{}`)
	}
	return data
}
