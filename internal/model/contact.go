// Package model 定义与数据库表结构对应的数据类型。
package model

import (
	"encoding/json"
	"time"
)

// Contact 表示外呼目标联系人。
type Contact struct {
	ID            int64           `json:"id"`
	TenantID      string          `json:"tenant_id"`
	PhoneMasked   string          `json:"phone_masked"`
	PhoneHash     string          `json:"phone_hash"`
	Source        string          `json:"source"`
	ProfileJSON   json.RawMessage `json:"profile_json"`
	CurrentStatus string          `json:"current_status"`
	DoNotCall     bool            `json:"do_not_call"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}
