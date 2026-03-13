package schema

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/model"
)

// CreateTemplateRequest 是创建模板的请求体。
type CreateTemplateRequest struct {
	Name                 string          `json:"name"`
	Domain               string          `json:"domain"`
	OpeningScript        string          `json:"opening_script"`
	StateMachineConfig   json.RawMessage `json:"state_machine_config"`
	ExtractionSchema     json.RawMessage `json:"extraction_schema"`
	GradingRules         json.RawMessage `json:"grading_rules"`
	PromptTemplates      json.RawMessage `json:"prompt_templates"`
	NotificationConfig   json.RawMessage `json:"notification_config"`
	CallProtectionConfig json.RawMessage `json:"call_protection_config"`
	PrecompiledAudios    json.RawMessage `json:"precompiled_audios"`
}

// UpdateTemplateRequest 是更新模板的请求体。
type UpdateTemplateRequest = CreateTemplateRequest

// TemplateResponse 是场景模板的 API 表示。
type TemplateResponse struct {
	ID                   int64           `json:"id"`
	Name                 string          `json:"name"`
	Domain               string          `json:"domain"`
	OpeningScript        string          `json:"opening_script"`
	StateMachineConfig   json.RawMessage `json:"state_machine_config"`
	ExtractionSchema     json.RawMessage `json:"extraction_schema"`
	GradingRules         json.RawMessage `json:"grading_rules"`
	PromptTemplates      json.RawMessage `json:"prompt_templates"`
	NotificationConfig   json.RawMessage `json:"notification_config"`
	CallProtectionConfig json.RawMessage `json:"call_protection_config"`
	PrecompiledAudios    json.RawMessage `json:"precompiled_audios"`
	Status               string          `json:"status"`
	Version              int             `json:"version"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// TemplateFromModel 将 model.ScenarioTemplate 转换为 TemplateResponse。
func TemplateFromModel(t *model.ScenarioTemplate) TemplateResponse {
	return TemplateResponse{
		ID:                   t.ID,
		Name:                 t.Name,
		Domain:               t.Domain,
		OpeningScript:        t.OpeningScript,
		StateMachineConfig:   t.StateMachineConfig,
		ExtractionSchema:     t.ExtractionSchema,
		GradingRules:         t.GradingRules,
		PromptTemplates:      t.PromptTemplates,
		NotificationConfig:   t.NotificationConfig,
		CallProtectionConfig: t.CallProtectionConfig,
		PrecompiledAudios:    t.PrecompiledAudios,
		Status:               string(t.Status),
		Version:              t.Version,
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
	}
}

// TemplatesFromModels 将 model.ScenarioTemplate 切片批量转换。
func TemplatesFromModels(templates []model.ScenarioTemplate) []TemplateResponse {
	result := make([]TemplateResponse, len(templates))
	for i := range templates {
		result[i] = TemplateFromModel(&templates[i])
	}
	return result
}

// SnapshotResponse 是模板快照的 API 表示。
type SnapshotResponse struct {
	ID           int64           `json:"id"`
	TemplateID   int64           `json:"template_id"`
	SnapshotData json.RawMessage `json:"snapshot_data"`
	CreatedAt    time.Time       `json:"created_at"`
}

// SnapshotFromModel 将 model.TemplateSnapshot 转换为 SnapshotResponse。
func SnapshotFromModel(s *model.TemplateSnapshot) SnapshotResponse {
	return SnapshotResponse{
		ID:           s.ID,
		TemplateID:   s.TemplateID,
		SnapshotData: s.SnapshotData,
		CreatedAt:    s.CreatedAt,
	}
}

// UpdateTemplateStatusRequest 是变更模板状态的请求体。
type UpdateTemplateStatusRequest struct {
	Status string `json:"status"`
}
