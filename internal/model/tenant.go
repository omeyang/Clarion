package model

import (
	"encoding/json"
	"time"
)

// Tenant 表示平台租户。
type Tenant struct {
	ID             string          `json:"id"`    // UUID v7
	Slug           string          `json:"slug"`  // 人类可读标识
	Name           string          `json:"name"`
	ContactPerson  string          `json:"contact_person"`
	ContactPhone   string          `json:"contact_phone"`
	Status         string          `json:"status"` // active / suspended
	DailyCallLimit int             `json:"daily_call_limit"`
	MaxConcurrent  int             `json:"max_concurrent"`
	Settings       json.RawMessage `json:"settings"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// APIKey 表示租户的 API 访问凭据（不含密钥明文）。
type APIKey struct {
	ID         int64      `json:"id"`
	TenantID   string     `json:"tenant_id"`
	KeyPrefix  string     `json:"key_prefix"`  // 前 8 字符，用于展示
	KeyHash    string     `json:"-"`           // SHA-256 哈希，不序列化
	Name       string     `json:"name"`
	Status     string     `json:"status"` // active / revoked
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
