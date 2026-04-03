package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 单元测试 ────────────────────────────────────────────

func TestGenerateAPIKey(t *testing.T) {
	fullKey, prefix, err := GenerateAPIKey(KeyPrefixLive)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(fullKey, KeyPrefixLive))
	assert.Len(t, fullKey, len(KeyPrefixLive)+32)
	assert.True(t, strings.HasPrefix(fullKey, prefix))
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)
	for range 100 {
		key, _, err := GenerateAPIKey(KeyPrefixLive)
		require.NoError(t, err)
		assert.False(t, keys[key], "生成了重复的 key")
		keys[key] = true
	}
}

func TestGenerateAPIKey_DisplayPrefix(t *testing.T) {
	fullKey, prefix, err := GenerateAPIKey(KeyPrefixLive)
	require.NoError(t, err)

	// 展示前缀 = prefix + 随机串前 2 位。
	assert.Len(t, prefix, len(KeyPrefixLive)+2)
	assert.Equal(t, fullKey[:len(KeyPrefixLive)+2], prefix)
}

func TestGenerateAPIKey_Base62Only(t *testing.T) {
	for range 50 {
		fullKey, _, err := GenerateAPIKey(KeyPrefixLive)
		require.NoError(t, err)

		randomPart := fullKey[len(KeyPrefixLive):]
		for _, c := range randomPart {
			assert.True(t,
				(c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'),
				"字符 %c 不在 base62 范围内", c)
		}
	}
}

func TestGenerateAPIKey_CustomPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{"生产前缀", "ck_live_"},
		{"测试前缀", "ck_test_"},
		{"空前缀", ""},
		{"长前缀", "very_long_prefix_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullKey, displayPrefix, err := GenerateAPIKey(tt.prefix)
			require.NoError(t, err)

			assert.True(t, strings.HasPrefix(fullKey, tt.prefix))
			assert.Len(t, fullKey, len(tt.prefix)+32)
			assert.Len(t, displayPrefix, len(tt.prefix)+2)
		})
	}
}

func TestHashAPIKey(t *testing.T) {
	hash1 := HashAPIKey("ck_live_abc123")
	hash2 := HashAPIKey("ck_live_abc123")
	hash3 := HashAPIKey("ck_live_different")

	assert.Equal(t, hash1, hash2, "相同 key 的哈希应一致")
	assert.NotEqual(t, hash1, hash3, "不同 key 的哈希应不同")
	assert.Len(t, hash1, 64, "SHA-256 hex 应为 64 字符")
}

func TestHashAPIKey_Empty(t *testing.T) {
	hash := HashAPIKey("")
	assert.Len(t, hash, 64, "空字符串的 SHA-256 也应为 64 字符")
}

// ── 基准测试 ────────────────────────────────────────────

func BenchmarkGenerateAPIKey(b *testing.B) {
	for b.Loop() {
		_, _, _ = GenerateAPIKey(KeyPrefixLive)
	}
}

func BenchmarkHashAPIKey(b *testing.B) {
	key := "ck_live_7Kd9mRqP4xYzN2wL5vBn8jF1hG3t"
	b.ResetTimer()
	for b.Loop() {
		_ = HashAPIKey(key)
	}
}

// ── 模糊测试 ────────────────────────────────────────────

func FuzzGenerateAPIKey(f *testing.F) {
	f.Add("ck_live_")
	f.Add("ck_test_")
	f.Add("")
	f.Add("x")

	f.Fuzz(func(t *testing.T, prefix string) {
		// 过滤过长的前缀，避免内存问题。
		if len(prefix) > 100 {
			return
		}

		fullKey, displayPrefix, err := GenerateAPIKey(prefix)
		require.NoError(t, err)

		assert.True(t, strings.HasPrefix(fullKey, prefix))
		assert.Len(t, fullKey, len(prefix)+32)
		assert.Len(t, displayPrefix, len(prefix)+2)
	})
}

func FuzzHashAPIKey(f *testing.F) {
	f.Add("ck_live_abc123")
	f.Add("")
	f.Add("ck_test_xyz")

	f.Fuzz(func(t *testing.T, key string) {
		hash := HashAPIKey(key)
		assert.Len(t, hash, 64)
		// 确定性：相同输入应有相同输出。
		assert.Equal(t, hash, HashAPIKey(key))
	})
}
