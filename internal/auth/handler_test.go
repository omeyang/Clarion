package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── mock 实现 ───────────────────────────────────────────

type mockAPIKeyStore struct {
	getByHashFn     func(ctx context.Context, hash string) (*KeyRecord, error)
	touchLastUsedFn func(ctx context.Context, id int64) error
}

func (m *mockAPIKeyStore) GetByHash(ctx context.Context, hash string) (*KeyRecord, error) {
	if m.getByHashFn != nil {
		return m.getByHashFn(ctx, hash)
	}
	return nil, errors.New("not found")
}

func (m *mockAPIKeyStore) TouchLastUsed(ctx context.Context, id int64) error {
	if m.touchLastUsedFn != nil {
		return m.touchLastUsedFn(ctx, id)
	}
	return nil
}

type mockTenantStore struct {
	getByIDFn func(ctx context.Context, id string) (*TenantRecord, error)
}

func (m *mockTenantStore) GetByID(ctx context.Context, id string) (*TenantRecord, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, errors.New("not found")
}

func newTestHandler(keys APIKeyStore, tenants TenantStore) *Handler {
	issuer := NewIssuer("test-handler-secret-key", 15*time.Minute)
	logger := slog.Default()
	return NewHandler(issuer, keys, tenants, logger)
}

// ── HandleToken 测试 ────────────────────────────────────

func TestHandleToken_Success(t *testing.T) {
	apiKey, _, err := GenerateAPIKey(KeyPrefixLive)
	require.NoError(t, err)
	keyHash := HashAPIKey(apiKey)

	keys := &mockAPIKeyStore{
		getByHashFn: func(_ context.Context, hash string) (*KeyRecord, error) {
			if hash == keyHash {
				return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "active"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(_ context.Context, id string) (*TenantRecord, error) {
			if id == "tenant-123" {
				return &TenantRecord{ID: "tenant-123", Status: "active"}, nil
			}
			return nil, errors.New("not found")
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: apiKey})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp tokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "tenant-123", resp.TenantID)
	assert.NotEmpty(t, resp.ExpiresAt)

	// 验证签发的 token 可被 issuer 解析。
	issuer := NewIssuer("test-handler-secret-key", 15*time.Minute)
	claims, err := issuer.Verify(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, "tenant-123", claims.TenantID)
	assert.Equal(t, int64(1), claims.KeyID)
}

func TestHandleToken_EmptyAPIKey(t *testing.T) {
	h := newTestHandler(&mockAPIKeyStore{}, &mockTenantStore{})

	body, _ := json.Marshal(map[string]string{"api_key": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorMessage(t, rec, "api_key 不能为空")
}

func TestHandleToken_MalformedJSON(t *testing.T) {
	h := newTestHandler(&mockAPIKeyStore{}, &mockTenantStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", strings.NewReader("{invalid}"))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorMessage(t, rec, "请求体格式错误")
}

func TestHandleToken_KeyNotFound(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return nil, errors.New("not found")
		},
	}

	h := newTestHandler(keys, &mockTenantStore{})
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorMessage(t, rec, "无效的 API Key")
}

func TestHandleToken_KeyRevoked(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "revoked"}, nil
		},
	}

	h := newTestHandler(keys, &mockTenantStore{})
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_revokedkey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assertErrorMessage(t, rec, "API Key 已吊销")
}

func TestHandleToken_TenantSuspended(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "active"}, nil
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(context.Context, string) (*TenantRecord, error) {
			return &TenantRecord{ID: "tenant-123", Status: "suspended"}, nil
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_somekey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assertErrorMessage(t, rec, "租户已暂停")
}

func TestHandleToken_TenantQueryError(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "active"}, nil
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(context.Context, string) (*TenantRecord, error) {
			return nil, errors.New("database connection lost")
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_somekey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleToken_TouchLastUsed_AsyncCall(t *testing.T) {
	var touchCalled atomic.Bool

	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 99, TenantID: "tenant-123", Status: "active"}, nil
		},
		touchLastUsedFn: func(_ context.Context, id int64) error {
			assert.Equal(t, int64(99), id)
			touchCalled.Store(true)
			return nil
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(context.Context, string) (*TenantRecord, error) {
			return &TenantRecord{ID: "tenant-123", Status: "active"}, nil
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_somekey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// 异步调用，等待一小段时间。
	require.Eventually(t, func() bool {
		return touchCalled.Load()
	}, 1*time.Second, 10*time.Millisecond, "TouchLastUsed 应被异步调用")
}

func TestHandleToken_TouchLastUsed_ErrorDoesNotAffectResponse(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "active"}, nil
		},
		touchLastUsedFn: func(context.Context, int64) error {
			return errors.New("redis timeout")
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(context.Context, string) (*TenantRecord, error) {
			return &TenantRecord{ID: "tenant-123", Status: "active"}, nil
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_somekey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	// TouchLastUsed 失败不影响响应。
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleToken_ResponseFormat(t *testing.T) {
	keys := &mockAPIKeyStore{
		getByHashFn: func(context.Context, string) (*KeyRecord, error) {
			return &KeyRecord{ID: 1, TenantID: "tenant-123", Status: "active"}, nil
		},
	}
	tenants := &mockTenantStore{
		getByIDFn: func(context.Context, string) (*TenantRecord, error) {
			return &TenantRecord{ID: "tenant-123", Status: "active"}, nil
		},
	}

	h := newTestHandler(keys, tenants)
	body, _ := json.Marshal(tokenRequest{APIKey: "ck_live_somekey"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleToken(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp, "token")
	assert.Contains(t, resp, "expires_at")
	assert.Contains(t, resp, "tenant_id")

	// expires_at 应为 RFC3339 格式。
	_, err := time.Parse(time.RFC3339, resp["expires_at"].(string))
	assert.NoError(t, err, "expires_at 应为 RFC3339 格式")
}

// ── HashAPIKey 测试 ─────────────────────────────────────

func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "ck_live_TestKey12345678901234567890"
	h1 := HashAPIKey(key)
	h2 := HashAPIKey(key)
	assert.Equal(t, h1, h2)
}

func TestHashAPIKey_DifferentKeys_DifferentHashes(t *testing.T) {
	h1 := HashAPIKey("ck_live_key1")
	h2 := HashAPIKey("ck_live_key2")
	assert.NotEqual(t, h1, h2)
}

func TestHashAPIKey_Length(t *testing.T) {
	h := HashAPIKey("any-key")
	assert.Len(t, h, 64, "SHA-256 hex 编码应为 64 字符")
}

// ── 辅助函数 ────────────────────────────────────────────

func assertErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, expected, resp["error"])
}
