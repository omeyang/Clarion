// Package api 提供 HTTP 路由、中间件和处理器注册。
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/omeyang/clarion/internal/api/handler"
	"github.com/omeyang/clarion/internal/service"
)

// Services 组合 API 层所需的全部服务依赖。
type Services struct {
	Contacts  *service.ContactSvc
	Templates *service.TemplateSvc
	Tasks     *service.TaskSvc
	Calls     *service.CallSvc
}

// Router 设置所有 HTTP 路由和中间件。
// 如果 services 为 nil，则只注册健康检查端点。
func Router(logger *slog.Logger, services *Services) http.Handler {
	return RouterWithHealth(logger, services, nil)
}

// RouterWithHealth 设置所有 HTTP 路由和中间件，支持注入 HealthHandler。
// 如果 health 为 nil，则使用默认的简单健康检查。
func RouterWithHealth(logger *slog.Logger, services *Services, health *handler.HealthHandler) http.Handler {
	mux := http.NewServeMux()

	// 健康检查端点。
	mux.HandleFunc("GET /healthz", handleHealthz)
	if health != nil {
		mux.Handle("GET /health", health)
	}

	if services != nil {
		// 注册 API v1 处理器。
		handler.NewContactHandler(services.Contacts).Register(mux)
		handler.NewTemplateHandler(services.Templates).Register(mux)
		handler.NewTaskHandler(services.Tasks).Register(mux)
		handler.NewCallHandler(services.Calls).Register(mux)
	} else {
		// 未配置服务时的兜底路由。
		mux.HandleFunc("GET /api/v1/", handleNotImplemented)
	}

	// 应用中间件链（顺序：最外层最先执行）。
	var h http.Handler = mux
	h = RequestIDMiddleware(h)
	h = CORSMiddleware(h)
	h = LoggingMiddleware(logger)(h)
	h = RecoveryMiddleware(logger)(h)

	return h
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// WriteHeader 已调用，无法再更改状态码，只能记录日志。
		slog.Error("JSON 编码响应失败", slog.String("error", err.Error()))
	}
}
