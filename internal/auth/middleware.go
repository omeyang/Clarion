package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/omeyang/xkit/pkg/context/xtenant"
)

// Middleware 验证 JWT 并将租户信息注入 context。
// 有 token → 验证，注入 Claims + xtenant。
// 无 token → 放行（由下游 RequireTenant 决定是否拒绝）。
// token 无效 → 立即 401。
func Middleware(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := issuer.Verify(tokenStr)
			if err != nil {
				slog.WarnContext(r.Context(), "JWT 验证失败",
					slog.String("error", err.Error()),
					slog.String("remote", r.RemoteAddr),
				)
				writeError(w, http.StatusUnauthorized, "无效或过期的 token")
				return
			}

			ctx := WithClaims(r.Context(), claims)
			ctx, _ = xtenant.WithTenantID(ctx, claims.TenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireTenant 要求请求必须携带有效的租户 JWT。
// 用于包装业务 API 路由组。
func RequireTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || claims.TenantID == "" {
			writeError(w, http.StatusUnauthorized, "需要认证")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractBearerToken 从 Authorization 头提取 Bearer token。
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
