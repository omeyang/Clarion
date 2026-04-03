package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 单元测试 ────────────────────────────────────────────

func TestIssuer_IssueAndVerify(t *testing.T) {
	issuer := NewIssuer("test-secret-key-32-chars-minimum", 15*time.Minute)

	token, exp, err := issuer.Issue("tenant-abc", 42)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), exp, 2*time.Second)

	claims, err := issuer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "tenant-abc", claims.TenantID)
	assert.Equal(t, int64(42), claims.KeyID)
}

func TestIssuer_VerifyExpired(t *testing.T) {
	issuer := NewIssuer("test-secret", 1*time.Millisecond)

	token, _, err := issuer.Issue("tenant-abc", 1)
	require.NoError(t, err)

	// 等待过期。
	time.Sleep(10 * time.Millisecond)

	_, err = issuer.Verify(token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is expired")
}

func TestIssuer_VerifyInvalidToken(t *testing.T) {
	issuer := NewIssuer("test-secret", 15*time.Minute)

	_, err := issuer.Verify("not-a-valid-jwt")
	assert.Error(t, err)
}

func TestIssuer_VerifyWrongSecret(t *testing.T) {
	issuer1 := NewIssuer("secret-1", 15*time.Minute)
	issuer2 := NewIssuer("secret-2", 15*time.Minute)

	token, _, err := issuer1.Issue("tenant-abc", 1)
	require.NoError(t, err)

	_, err = issuer2.Verify(token)
	assert.Error(t, err)
}

func TestIssuer_VerifyEmptyToken(t *testing.T) {
	issuer := NewIssuer("test-secret", 15*time.Minute)

	_, err := issuer.Verify("")
	assert.Error(t, err)
}

func TestIssuer_Issue_ClaimsRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		tenant string
		keyID  int64
	}{
		{"正常 UUID", "0192d5e8-7a3b-7def-9c1a-1234567890ab", 1},
		{"默认租户 UUID", "00000000-0000-0000-0000-000000000000", 0},
		{"长 tenant ID", "a-very-long-tenant-identifier-string-for-testing", 9999},
		{"最大 keyID", "tenant-max", 1<<62 - 1},
	}

	issuer := NewIssuer("test-secret-roundtrip", 15*time.Minute)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, _, err := issuer.Issue(tt.tenant, tt.keyID)
			require.NoError(t, err)

			claims, err := issuer.Verify(token)
			require.NoError(t, err)
			assert.Equal(t, tt.tenant, claims.TenantID)
			assert.Equal(t, tt.keyID, claims.KeyID)
		})
	}
}

func TestIssuer_Issue_ExpiresAtMatchesTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{"1 秒", 1 * time.Second},
		{"15 分钟", 15 * time.Minute},
		{"1 小时", 1 * time.Hour},
		{"24 小时", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := NewIssuer("test-secret", tt.ttl)
			before := time.Now()
			_, exp, err := issuer.Issue("tenant", 1)
			require.NoError(t, err)

			expected := before.Add(tt.ttl)
			assert.WithinDuration(t, expected, exp, 2*time.Second)
		})
	}
}

func TestNewIssuer(t *testing.T) {
	issuer := NewIssuer("my-secret", 30*time.Minute)
	assert.NotNil(t, issuer)
	assert.Equal(t, []byte("my-secret"), issuer.secret)
	assert.Equal(t, 30*time.Minute, issuer.ttl)
}

// ── 基准测试 ────────────────────────────────────────────

func BenchmarkIssuer_Issue(b *testing.B) {
	issuer := NewIssuer("benchmark-secret-key-32-chars-ok", 15*time.Minute)
	b.ResetTimer()

	for b.Loop() {
		_, _, _ = issuer.Issue("tenant-bench", 42)
	}
}

func BenchmarkIssuer_Verify(b *testing.B) {
	issuer := NewIssuer("benchmark-secret-key-32-chars-ok", 15*time.Minute)
	token, _, _ := issuer.Issue("tenant-bench", 42)
	b.ResetTimer()

	for b.Loop() {
		_, _ = issuer.Verify(token)
	}
}

func BenchmarkIssuer_IssueAndVerify(b *testing.B) {
	issuer := NewIssuer("benchmark-secret-key-32-chars-ok", 15*time.Minute)
	b.ResetTimer()

	for b.Loop() {
		token, _, _ := issuer.Issue("tenant-bench", 42)
		_, _ = issuer.Verify(token)
	}
}

// ── 模糊测试 ────────────────────────────────────────────

func FuzzIssuer_Verify(f *testing.F) {
	// 种子语料：合法 token、空串、随机串。
	issuer := NewIssuer("fuzz-secret", 15*time.Minute)
	validToken, _, _ := issuer.Issue("tenant-fuzz", 1)

	f.Add(validToken)
	f.Add("")
	f.Add("not-a-jwt")
	f.Add("eyJhbGciOiJIUzI1NiJ9.e30.ZRrHA1JJJW8opB1Qfp7QDv")
	f.Add("a.b.c")
	f.Add("........")

	f.Fuzz(func(t *testing.T, tokenStr string) {
		// 模糊测试验证 Verify 不会 panic。
		_, _ = issuer.Verify(tokenStr)
	})
}

func FuzzIssuer_Issue(f *testing.F) {
	f.Add("tenant-1", int64(1))
	f.Add("", int64(0))
	f.Add("0192d5e8-7a3b-7def-9c1a-1234567890ab", int64(9999))
	f.Add("a-very-long-string-for-tenant-id-testing-purposes-here", int64(-1))

	issuer := NewIssuer("fuzz-secret-issue", 15*time.Minute)

	f.Fuzz(func(t *testing.T, tenantID string, keyID int64) {
		token, exp, err := issuer.Issue(tenantID, keyID)
		if err != nil {
			return
		}
		// 签发成功则必须能验证。
		assert.NotEmpty(t, token)
		assert.False(t, exp.IsZero())

		claims, err := issuer.Verify(token)
		require.NoError(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, keyID, claims.KeyID)
	})
}
