package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
)

// APIKeyStore 定义 token handler 所需的 API Key 数据访问接口。
type APIKeyStore interface {
	GetByHash(ctx context.Context, hash string) (*KeyRecord, error)
	TouchLastUsed(ctx context.Context, id int64) error
}

// TenantStore 定义 token handler 所需的租户数据访问接口。
type TenantStore interface {
	GetByID(ctx context.Context, id string) (*TenantRecord, error)
}

// KeyRecord 是 API Key 查询结果。
type KeyRecord struct {
	ID       int64
	TenantID string
	Status   string
}

// TenantRecord 是租户查询结果。
type TenantRecord struct {
	ID     string
	Status string
}

// Handler 处理认证相关的 HTTP 请求。
type Handler struct {
	issuer  *Issuer
	keys    APIKeyStore
	tenants TenantStore
	logger  *slog.Logger
}

// NewHandler 创建认证处理器。
func NewHandler(issuer *Issuer, keys APIKeyStore, tenants TenantStore, logger *slog.Logger) *Handler {
	return &Handler{
		issuer:  issuer,
		keys:    keys,
		tenants: tenants,
		logger:  logger,
	}
}

type tokenRequest struct {
	APIKey string `json:"api_key"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	TenantID  string `json:"tenant_id"`
}

// HandleToken 处理 API Key → JWT 的换取请求（POST /api/v1/auth/token）。
func (h *Handler) HandleToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "api_key 不能为空")
		return
	}

	// 1. 查找 API Key。
	keyHash := hashAPIKey(req.APIKey)
	key, err := h.keys.GetByHash(r.Context(), keyHash)
	if err != nil {
		h.logger.WarnContext(r.Context(), "API Key 查询失败",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusUnauthorized, "无效的 API Key")
		return
	}
	if key.Status != "active" {
		writeError(w, http.StatusUnauthorized, "API Key 已吊销")
		return
	}

	// 2. 检查租户状态。
	tenant, err := h.tenants.GetByID(r.Context(), key.TenantID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "查询租户失败",
			slog.String("tenant_id", key.TenantID),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "内部错误")
		return
	}
	if tenant.Status != "active" {
		writeError(w, http.StatusForbidden, "租户已暂停")
		return
	}

	// 3. 签发 JWT。
	token, expiresAt, err := h.issuer.Issue(key.TenantID, key.ID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "签发 token 失败",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "签发 token 失败")
		return
	}

	// 4. 异步更新 last_used_at（不阻塞响应）。
	go func() {
		if err := h.keys.TouchLastUsed(context.Background(), key.ID); err != nil {
			h.logger.Warn("更新 API Key 使用时间失败",
				slog.Int64("key_id", key.ID),
				slog.String("error", err.Error()),
			)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokenResponse{
		Token:     token,
		ExpiresAt: expiresAt.Format("2006-01-02T15:04:05Z07:00"),
		TenantID:  key.TenantID,
	})
}

// HashAPIKey 对 API Key 做 SHA-256 哈希。导出用于 CLI 工具。
func HashAPIKey(key string) string {
	return hashAPIKey(key)
}

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
