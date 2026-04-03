package model

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/engine"
)

// ScenarioTemplate 定义对话流程配置。
type ScenarioTemplate struct {
	ID                   int64                 `json:"id"`
	TenantID             string                `json:"tenant_id"`
	Name                 string                `json:"name"`
	Domain               string                `json:"domain"`
	OpeningScript        string                `json:"opening_script"`
	StateMachineConfig   json.RawMessage       `json:"state_machine_config"`
	ExtractionSchema     json.RawMessage       `json:"extraction_schema"`
	GradingRules         json.RawMessage       `json:"grading_rules"`
	PromptTemplates      json.RawMessage       `json:"prompt_templates"`
	NotificationConfig   json.RawMessage       `json:"notification_config"`
	CallProtectionConfig json.RawMessage       `json:"call_protection_config"`
	PrecompiledAudios    json.RawMessage       `json:"precompiled_audios"`
	Status               engine.TemplateStatus `json:"status"`
	Version              int                   `json:"version"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

// TemplateSnapshot 是模板的不可变副本，用于运行时。
type TemplateSnapshot struct {
	ID           int64           `json:"id"`
	TemplateID   int64           `json:"template_id"`
	SnapshotData json.RawMessage `json:"snapshot_data"`
	CreatedAt    time.Time       `json:"created_at"`
}
