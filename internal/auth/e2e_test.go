package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/auth"
	"github.com/omeyang/xkit/pkg/context/xtenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── mock 实现（黑盒测试） ────────────────────────────────

type stubAPIKeyStore struct {
	keys map[string]*auth.KeyRecord // key_hash → record
}

func (s *stubAPIKeyStore) GetByHash(_ context.Context, hash string) (*auth.KeyRecord, error) {
	if rec, ok := s.keys[hash]; ok {
		return rec, nil
	}
	return nil, errors.New("not found")
}

func (s *stubAPIKeyStore) TouchLastUsed(context.Context, int64) error {
	return nil
}

type stubTenantStore struct {
	tenants map[string]*auth.TenantRecord // id → record
}

func (s *stubTenantStore) GetByID(_ context.Context, id string) (*auth.TenantRecord, error) {
	if rec, ok := s.tenants[id]; ok {
		return rec, nil
	}
	return nil, errors.New("not found")
}

// testSecret 返回测试用 JWT 密钥。
func testSecret() string {
	return "e2e-test-jwt-" + time.Now().Format("20060102150405")
}

// ── 端到端集成测试 ──────────────────────────────────────

// TestE2E_FullAuthFlow 验证完整的认证流程：
// 生成 API Key → 换取 JWT → 访问业务端点 → 验证租户上下文。
func TestE2E_FullAuthFlow(t *testing.T) {
	secret := testSecret()
	const tenantID = "0192d5e8-7a3b-7def-9c1a-1234567890ab"

	// 1. 生成 API Key。
	fullKey, _, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
	require.NoError(t, err)

	keyHash := auth.HashAPIKey(fullKey)

	// 2. 构造 store 数据。
	keyStore := &stubAPIKeyStore{
		keys: map[string]*auth.KeyRecord{
			keyHash: {ID: 1, TenantID: tenantID, Status: "active"},
		},
	}
	tenantStore := &stubTenantStore{
		tenants: map[string]*auth.TenantRecord{
			tenantID: {ID: tenantID, Status: "active"},
		},
	}

	issuer := auth.NewIssuer(secret, 15*time.Minute)
	handler := auth.NewHandler(issuer, keyStore, tenantStore, slog.Default())

	// 3. 构造路由：token 端点 + 受保护业务端点。
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/token", handler.HandleToken)

	// 业务端点返回从 context 中获取的 tenant_id。
	businessHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid, terr := xtenant.RequireTenantID(r.Context())
		if terr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tenant_id": tid})
	})
	mux.Handle("GET /api/v1/test", auth.RequireTenant(businessHandler))

	// 全局认证中间件。
	var h http.Handler = mux
	h = auth.Middleware(issuer)(h)

	server := httptest.NewServer(h)
	defer server.Close()
	client := server.Client()

	// ── Step 1: Token 端点不需要 JWT ──
	t.Run("获取 token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"api_key": fullKey})
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/auth/token", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var tokenResp map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&tokenResp))
		assert.NotEmpty(t, tokenResp["token"])
		assert.Equal(t, tenantID, tokenResp["tenant_id"])

		// ── Step 2: 用 JWT 访问业务端点 ──
		t.Run("用 token 访问业务端点", func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/test", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+tokenResp["token"].(string))

			resp2, err := client.Do(req)
			require.NoError(t, err)
			defer resp2.Body.Close()

			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			var bizResp map[string]string
			require.NoError(t, json.NewDecoder(resp2.Body).Decode(&bizResp))
			assert.Equal(t, tenantID, bizResp["tenant_id"])
		})
	})

	// ── Step 3: 无 token 访问业务端点 → 401 ──
	t.Run("无 token 访问业务端点", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/test", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	// ── Step 4: 无效 token 访问业务端点 → 401 ──
	t.Run("无效 token 访问业务端点", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/test", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer invalid-token")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

// TestE2E_MultiTenantIsolation 验证多租户数据隔离：
// 租户 A 的 token 不能用于租户 B 的上下文。
func TestE2E_MultiTenantIsolation(t *testing.T) {
	secret := testSecret()
	const tenantA = "tenant-aaaa-aaaa"
	const tenantB = "tenant-bbbb-bbbb"

	keyA, _, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
	require.NoError(t, err)
	keyB, _, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
	require.NoError(t, err)

	keyStore := &stubAPIKeyStore{
		keys: map[string]*auth.KeyRecord{
			auth.HashAPIKey(keyA): {ID: 1, TenantID: tenantA, Status: "active"},
			auth.HashAPIKey(keyB): {ID: 2, TenantID: tenantB, Status: "active"},
		},
	}
	tenantStore := &stubTenantStore{
		tenants: map[string]*auth.TenantRecord{
			tenantA: {ID: tenantA, Status: "active"},
			tenantB: {ID: tenantB, Status: "active"},
		},
	}

	issuer := auth.NewIssuer(secret, 15*time.Minute)
	handler := auth.NewHandler(issuer, keyStore, tenantStore, slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/token", handler.HandleToken)
	mux.Handle("GET /api/v1/whoami", auth.RequireTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid, _ := xtenant.RequireTenantID(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tenant_id": tid})
	})))

	var h http.Handler = mux
	h = auth.Middleware(issuer)(h)

	server := httptest.NewServer(h)
	defer server.Close()
	client := server.Client()

	// 获取两个租户的 token。
	getToken := func(apiKey string) string {
		body, _ := json.Marshal(map[string]string{"api_key": apiKey})
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/auth/token", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		var tokenResp map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&tokenResp))
		return tokenResp["token"].(string)
	}

	tokenA := getToken(keyA)
	tokenB := getToken(keyB)

	// 验证 token A → 返回 tenant A。
	whoami := func(token string) string {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/whoami", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		return result["tenant_id"]
	}

	assert.Equal(t, tenantA, whoami(tokenA))
	assert.Equal(t, tenantB, whoami(tokenB))
	assert.NotEqual(t, whoami(tokenA), whoami(tokenB))
}

// TestE2E_KeyRevocationFlow 验证 API Key 吊销后无法获取新 token。
func TestE2E_KeyRevocationFlow(t *testing.T) {
	const tenantID = "tenant-revoke"

	fullKey, _, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
	require.NoError(t, err)

	keyHash := auth.HashAPIKey(fullKey)
	keyRecord := &auth.KeyRecord{ID: 1, TenantID: tenantID, Status: "active"}

	keyStore := &stubAPIKeyStore{
		keys: map[string]*auth.KeyRecord{keyHash: keyRecord},
	}
	tenantStore := &stubTenantStore{
		tenants: map[string]*auth.TenantRecord{
			tenantID: {ID: tenantID, Status: "active"},
		},
	}

	issuer := auth.NewIssuer(testSecret(), 15*time.Minute)
	handler := auth.NewHandler(issuer, keyStore, tenantStore, slog.Default())

	// 第一次换取 token 应成功。
	body, _ := json.Marshal(map[string]string{"api_key": fullKey})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleToken(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 模拟吊销。
	keyRecord.Status = "revoked"

	// 第二次换取应失败。
	body, _ = json.Marshal(map[string]string{"api_key": fullKey})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	handler.HandleToken(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestE2E_TenantSuspensionFlow 验证租户暂停后无法获取 token。
func TestE2E_TenantSuspensionFlow(t *testing.T) {
	const tenantID = "tenant-suspend"

	fullKey, _, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
	require.NoError(t, err)

	tenantRecord := &auth.TenantRecord{ID: tenantID, Status: "active"}

	keyStore := &stubAPIKeyStore{
		keys: map[string]*auth.KeyRecord{
			auth.HashAPIKey(fullKey): {ID: 1, TenantID: tenantID, Status: "active"},
		},
	}
	tenantStore := &stubTenantStore{
		tenants: map[string]*auth.TenantRecord{tenantID: tenantRecord},
	}

	issuer := auth.NewIssuer(testSecret(), 15*time.Minute)
	handler := auth.NewHandler(issuer, keyStore, tenantStore, slog.Default())

	// 活跃状态时换取 token 应成功。
	body, _ := json.Marshal(map[string]string{"api_key": fullKey})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleToken(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 模拟暂停。
	tenantRecord.Status = "suspended"

	// 暂停后换取应失败。
	body, _ = json.Marshal(map[string]string{"api_key": fullKey})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	handler.HandleToken(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
