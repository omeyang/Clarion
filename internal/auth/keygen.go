package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// base62 字符集。
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// KeyPrefixLive 是生产环境 API Key 前缀。
const KeyPrefixLive = "ck_live_"

// GenerateAPIKey 生成一个 API Key，格式为 prefix + 32 字节 base62 随机串。
// 返回完整 key 和用于展示的前 8 字符前缀。
func GenerateAPIKey(prefix string) (fullKey, displayPrefix string, err error) {
	const randomLen = 32

	b := make([]byte, randomLen)
	max := big.NewInt(int64(len(base62Chars)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", "", fmt.Errorf("generate random byte: %w", err)
		}
		b[i] = base62Chars[n.Int64()]
	}

	fullKey = prefix + string(b)
	// 展示前缀：prefix 的前几位 + 随机串的前 2 位。
	displayPrefix = fullKey[:len(prefix)+2]
	return fullKey, displayPrefix, nil
}
