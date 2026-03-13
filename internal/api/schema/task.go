package schema

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/model"
)

// CreateTaskRequest 是创建外呼任务的请求体。
type CreateTaskRequest struct {
	Name               string          `json:"name"`
	ScenarioTemplateID int64           `json:"scenario_template_id"`
	ContactFilter      json.RawMessage `json:"contact_filter"`
	ScheduleConfig     json.RawMessage `json:"schedule_config"`
	DailyLimit         int             `json:"daily_limit"`
	MaxConcurrent      int             `json:"max_concurrent"`
}

// UpdateTaskRequest 是更新任务的请求体。
type UpdateTaskRequest struct {
	Name           string          `json:"name"`
	ContactFilter  json.RawMessage `json:"contact_filter"`
	ScheduleConfig json.RawMessage `json:"schedule_config"`
	DailyLimit     int             `json:"daily_limit"`
	MaxConcurrent  int             `json:"max_concurrent"`
	TotalContacts  int             `json:"total_contacts"`
}

// TaskResponse 是外呼任务的 API 表示。
type TaskResponse struct {
	ID                 int64           `json:"id"`
	Name               string          `json:"name"`
	ScenarioTemplateID int64           `json:"scenario_template_id"`
	TemplateSnapshotID *int64          `json:"template_snapshot_id"`
	ContactFilter      json.RawMessage `json:"contact_filter"`
	ScheduleConfig     json.RawMessage `json:"schedule_config"`
	DailyLimit         int             `json:"daily_limit"`
	MaxConcurrent      int             `json:"max_concurrent"`
	Status             string          `json:"status"`
	TotalContacts      int             `json:"total_contacts"`
	CompletedContacts  int             `json:"completed_contacts"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// TaskFromModel 将 model.CallTask 转换为 TaskResponse。
func TaskFromModel(t *model.CallTask) TaskResponse {
	return TaskResponse{
		ID:                 t.ID,
		Name:               t.Name,
		ScenarioTemplateID: t.ScenarioTemplateID,
		TemplateSnapshotID: t.TemplateSnapshotID,
		ContactFilter:      t.ContactFilter,
		ScheduleConfig:     t.ScheduleConfig,
		DailyLimit:         t.DailyLimit,
		MaxConcurrent:      t.MaxConcurrent,
		Status:             string(t.Status),
		TotalContacts:      t.TotalContacts,
		CompletedContacts:  t.CompletedContacts,
		CreatedAt:          t.CreatedAt,
		UpdatedAt:          t.UpdatedAt,
	}
}

// TasksFromModels 将 model.CallTask 切片批量转换。
func TasksFromModels(tasks []model.CallTask) []TaskResponse {
	result := make([]TaskResponse, len(tasks))
	for i := range tasks {
		result[i] = TaskFromModel(&tasks[i])
	}
	return result
}

// UpdateTaskStatusRequest 是变更任务状态的请求体。
type UpdateTaskStatusRequest struct {
	Status string `json:"status"`
}
