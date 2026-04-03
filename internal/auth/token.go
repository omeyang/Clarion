package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken 表示 JWT 签名或格式无效。
var ErrInvalidToken = errors.New("invalid token")

// Claims 是 JWT 中携带的租户认证信息。
type Claims struct {
	TenantID string `json:"tid"`
	KeyID    int64  `json:"kid"`
	jwt.RegisteredClaims
}

// Issuer 负责签发和验证 JWT。
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// NewIssuer 创建 JWT 签发器。
func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Issue 签发 JWT，返回 token 字符串和过期时间。
func (i *Issuer) Issue(tenantID string, keyID int64) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)

	claims := Claims{
		TenantID: tenantID,
		KeyID:    keyID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Verify 验证 JWT 签名和过期时间，返回 Claims。
func (i *Issuer) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
