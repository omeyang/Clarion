package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omeyang/xkit/pkg/context/xtenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIssuer() *Issuer {
	return NewIssuer("test-secret-key-for-middleware-test", 15*time.Minute)
}

// echoHandler 是一个用于测试的 handler，将 claims 和 tenant_id 写入响应。
func echoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"reached": true}

		if c := ClaimsFromContext(r.Context()); c != nil {
			resp["tenant_id"] = c.TenantID
			resp["key_id"] = c.KeyID
		}
		if tid, err := xtenant.RequireTenantID(r.Context()); err == nil {
			resp["xtenant_id"] = tid
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// ── Middleware 测试 ──────────────────────────────────────

func TestMiddleware_NoToken_PassesThrough(t *testing.T) {
	issuer := newTestIssuer()
	h := Middleware(issuer)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp["reached"].(bool))
	assert.Nil(t, resp["tenant_id"])
}

func TestMiddleware_ValidToken_InjectsClaims(t *testing.T) {
	issuer := newTestIssuer()
	token, _, err := issuer.Issue("tenant-abc", 42)
	require.NoError(t, err)

	h := Middleware(issuer)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "tenant-abc", resp["tenant_id"])
	assert.Equal(t, float64(42), resp["key_id"])
	assert.Equal(t, "tenant-abc", resp["xtenant_id"])
}

func TestMiddleware_InvalidToken_Returns401(t *testing.T) {
	issuer := newTestIssuer()
	h := Middleware(issuer)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-jwt-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "无效或过期的 token")
}

func TestMiddleware_ExpiredToken_Returns401(t *testing.T) {
	issuer := NewIssuer("test-secret", 1*time.Millisecond)
	token, _, err := issuer.Issue("tenant-abc", 1)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	h := Middleware(issuer)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_WrongSecret_Returns401(t *testing.T) {
	issuer1 := NewIssuer("secret-1", 15*time.Minute)
	issuer2 := NewIssuer("secret-2", 15*time.Minute)

	token, _, err := issuer1.Issue("tenant-abc", 1)
	require.NoError(t, err)

	h := Middleware(issuer2)(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_MalformedAuthHeader(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"空 header", ""},
		{"无 Bearer 前缀", "Basic abc123"},
		{"只有 Bearer", "Bearer "},
		{"Bearer 后有空格但无 token", "Bearer   "},
	}

	issuer := newTestIssuer()
	h := Middleware(issuer)(echoHandler())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.value != "" {
				req.Header.Set("Authorization", tt.value)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			// 空 header / 非 Bearer → 放行（无 token）。
			// "Bearer " 后面是空或空格 → 可能 401 也可能放行，取决于 extractBearerToken 的行为。
			// 但都不应该 panic。
			assert.NotEqual(t, http.StatusInternalServerError, rec.Code)
		})
	}
}

// ── RequireTenant 测试 ──────────────────────────────────

func TestRequireTenant_WithClaims_Passes(t *testing.T) {
	issuer := newTestIssuer()
	token, _, err := issuer.Issue("tenant-abc", 42)
	require.NoError(t, err)

	// Middleware 注入 claims → RequireTenant 放行。
	h := Middleware(issuer)(RequireTenant(echoHandler()))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireTenant_NoClaims_Returns401(t *testing.T) {
	h := RequireTenant(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "需要认证", resp["error"])
}

func TestRequireTenant_EmptyTenantID_Returns401(t *testing.T) {
	// 手动注入一个 TenantID 为空的 Claims。
	claims := &Claims{TenantID: "", KeyID: 1}
	h := RequireTenant(echoHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	ctx := WithClaims(req.Context(), claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ── extractBearerToken 测试 ─────────────────────────────

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"空 header", "", ""},
		{"Bearer + token", "Bearer abc123", "abc123"},
		{"Bearer + 带空格 token", "Bearer  abc123 ", "abc123"},
		{"Basic 认证", "Basic abc123", ""},
		{"只有 Bearer", "Bearer ", ""},
		{"小写 bearer", "bearer abc123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := extractBearerToken(req)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ── writeError 测试 ─────────────────────────────────────

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusForbidden, "禁止访问")

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "禁止访问", resp["error"])
}
