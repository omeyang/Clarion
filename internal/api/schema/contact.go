package schema

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/model"
)

// CreateContactRequest 是创建联系人的请求体。
type CreateContactRequest struct {
	PhoneMasked string          `json:"phone_masked"`
	PhoneHash   string          `json:"phone_hash"`
	Source      string          `json:"source"`
	ProfileJSON json.RawMessage `json:"profile_json"`
}

// BulkCreateContactRequest 是批量创建联系人的请求体。
type BulkCreateContactRequest struct {
	Contacts []CreateContactRequest `json:"contacts"`
}

// ContactResponse 是联系人的 API 表示。
type ContactResponse struct {
	ID            int64           `json:"id"`
	PhoneMasked   string          `json:"phone_masked"`
	PhoneHash     string          `json:"phone_hash"`
	Source        string          `json:"source"`
	ProfileJSON   json.RawMessage `json:"profile_json"`
	CurrentStatus string          `json:"current_status"`
	DoNotCall     bool            `json:"do_not_call"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ContactFromModel 将 model.Contact 转换为 ContactResponse。
func ContactFromModel(c *model.Contact) ContactResponse {
	return ContactResponse{
		ID:            c.ID,
		PhoneMasked:   c.PhoneMasked,
		PhoneHash:     c.PhoneHash,
		Source:        c.Source,
		ProfileJSON:   c.ProfileJSON,
		CurrentStatus: c.CurrentStatus,
		DoNotCall:     c.DoNotCall,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

// ContactsFromModels 将 model.Contact 切片批量转换为 ContactResponse。
func ContactsFromModels(contacts []model.Contact) []ContactResponse {
	result := make([]ContactResponse, len(contacts))
	for i := range contacts {
		result[i] = ContactFromModel(&contacts[i])
	}
	return result
}

// UpdateContactStatusRequest 是更新联系人状态的请求体。
type UpdateContactStatusRequest struct {
	Status string `json:"status"`
}
