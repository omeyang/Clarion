package model

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/engine"
)

// CallTask 表示批量外呼任务。
type CallTask struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	ScenarioTemplateID int64             `json:"scenario_template_id"`
	TemplateSnapshotID *int64            `json:"template_snapshot_id"`
	ContactFilter      json.RawMessage   `json:"contact_filter"`
	ScheduleConfig     json.RawMessage   `json:"schedule_config"`
	DailyLimit         int               `json:"daily_limit"`
	MaxConcurrent      int               `json:"max_concurrent"`
	Status             engine.TaskStatus `json:"status"`
	TotalContacts      int               `json:"total_contacts"`
	CompletedContacts  int               `json:"completed_contacts"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}
