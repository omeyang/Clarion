// Package auth 提供 JWT 签发/验证和 HTTP 认证中间件。
package auth

import "context"

type ctxKey struct{}

// WithClaims 将已验证的 Claims 注入 context。
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// ClaimsFromContext 从 context 中获取 Claims，不存在时返回 nil。
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(ctxKey{}).(*Claims)
	return c
}
