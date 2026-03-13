package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// HealthChecker 检查依赖服务的健康状态。
type HealthChecker interface {
	// Ping 验证连接是否正常，返回错误表示不健康。
	Ping(ctx context.Context) error
}

// HealthHandler 处理健康检查请求。
type HealthHandler struct {
	checks map[string]HealthChecker
	logger *slog.Logger
}

// NewHealthHandler 创建健康检查处理器。
func NewHealthHandler(logger *slog.Logger) *HealthHandler {
	return &HealthHandler{
		checks: make(map[string]HealthChecker),
		logger: logger,
	}
}

// Register 注册一个健康检查项。
func (h *HealthHandler) Register(name string, checker HealthChecker) {
	h.checks[name] = checker
}

// ServeHTTP 处理 GET /health 请求。
// 返回 200 表示所有依赖健康，503 表示至少一个不健康。
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result := make(map[string]string, len(h.checks))
	healthy := true

	for name, checker := range h.checks {
		if err := checker.Ping(ctx); err != nil {
			result[name] = "unhealthy: " + err.Error()
			healthy = false
			h.logger.Warn("健康检查失败", slog.String("name", name), slog.String("error", err.Error()))
		} else {
			result[name] = "healthy"
		}
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": map[bool]string{true: "healthy", false: "unhealthy"}[healthy],
		"checks": result,
	})
}
