package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithClaims_AndClaimsFromContext(t *testing.T) {
	claims := &Claims{TenantID: "tenant-abc", KeyID: 42}
	ctx := WithClaims(context.Background(), claims)

	got := ClaimsFromContext(ctx)
	assert.Equal(t, claims, got)
	assert.Equal(t, "tenant-abc", got.TenantID)
	assert.Equal(t, int64(42), got.KeyID)
}

func TestClaimsFromContext_Empty(t *testing.T) {
	got := ClaimsFromContext(context.Background())
	assert.Nil(t, got)
}

func TestClaimsFromContext_WrongType(t *testing.T) {
	// 确保不同类型的 value 不会导致 panic。
	ctx := context.WithValue(context.Background(), ctxKey{}, "not-claims")
	got := ClaimsFromContext(ctx)
	assert.Nil(t, got)
}

func TestWithClaims_Overwrite(t *testing.T) {
	c1 := &Claims{TenantID: "tenant-1", KeyID: 1}
	c2 := &Claims{TenantID: "tenant-2", KeyID: 2}

	ctx := WithClaims(context.Background(), c1)
	ctx = WithClaims(ctx, c2)

	got := ClaimsFromContext(ctx)
	assert.Equal(t, "tenant-2", got.TenantID)
}

func TestWithClaims_NilClaims(t *testing.T) {
	ctx := WithClaims(context.Background(), nil)
	got := ClaimsFromContext(ctx)
	assert.Nil(t, got)
}
