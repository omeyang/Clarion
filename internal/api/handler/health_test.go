package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// healthyChecker 始终返回健康状态的检查器。
type healthyChecker struct{}

func (healthyChecker) Ping(_ context.Context) error { return nil }

// unhealthyChecker 始终返回不健康状态的检查器。
type unhealthyChecker struct{}

func (unhealthyChecker) Ping(_ context.Context) error { return errors.New("connection refused") }

func TestHealthHandler_AllHealthy(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler(slog.Default())
	h.Register("db", healthyChecker{})
	h.Register("cache", healthyChecker{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "healthy", body["status"])

	// 验证每个检查项都返回健康。
	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "healthy", checks["db"])
	assert.Equal(t, "healthy", checks["cache"])
}

func TestHealthHandler_OneUnhealthy(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler(slog.Default())
	h.Register("db", healthyChecker{})
	h.Register("cache", unhealthyChecker{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "unhealthy", body["status"])

	// 验证失败的检查项被列出。
	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "healthy", checks["db"])
	assert.Contains(t, checks["cache"], "unhealthy:")
}

func TestHealthHandler_NoChecks(t *testing.T) {
	t.Parallel()

	// 未注册任何检查项时，应返回 200 和健康状态。
	h := NewHealthHandler(slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "healthy", body["status"])
}
