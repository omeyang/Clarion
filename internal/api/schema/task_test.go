package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestTaskFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	snapshotID := int64(99)

	m := &model.CallTask{
		ID:                 1,
		Name:               "春季营销外呼",
		ScenarioTemplateID: 10,
		TemplateSnapshotID: &snapshotID,
		ContactFilter:      json.RawMessage(`{"source":"crm"}`),
		ScheduleConfig:     json.RawMessage(`{"start":"09:00","end":"18:00"}`),
		DailyLimit:         500,
		MaxConcurrent:      10,
		Status:             engine.TaskRunning,
		TotalContacts:      1000,
		CompletedContacts:  250,
		CreatedAt:          now,
		UpdatedAt:          now.Add(2 * time.Hour),
	}

	got := TaskFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.Name, got.Name)
	assert.Equal(t, m.ScenarioTemplateID, got.ScenarioTemplateID)
	assert.Equal(t, m.TemplateSnapshotID, got.TemplateSnapshotID)
	assert.JSONEq(t, string(m.ContactFilter), string(got.ContactFilter))
	assert.JSONEq(t, string(m.ScheduleConfig), string(got.ScheduleConfig))
	assert.Equal(t, m.DailyLimit, got.DailyLimit)
	assert.Equal(t, m.MaxConcurrent, got.MaxConcurrent)
	assert.Equal(t, string(m.Status), got.Status)
	assert.Equal(t, m.TotalContacts, got.TotalContacts)
	assert.Equal(t, m.CompletedContacts, got.CompletedContacts)
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
	assert.Equal(t, m.UpdatedAt, got.UpdatedAt)
}

func TestTaskFromModel_NilSnapshotID(t *testing.T) {
	t.Parallel()

	m := &model.CallTask{
		ID:                 2,
		TemplateSnapshotID: nil,
		Status:             engine.TaskDraft,
	}

	got := TaskFromModel(m)
	assert.Nil(t, got.TemplateSnapshotID)
}

func TestTasksFromModels(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	tasks := []model.CallTask{
		{ID: 1, Name: "任务一", Status: engine.TaskDraft, CreatedAt: now},
		{ID: 2, Name: "任务二", Status: engine.TaskRunning, CreatedAt: now},
		{ID: 3, Name: "任务三", Status: engine.TaskCompleted, CreatedAt: now},
	}

	got := TasksFromModels(tasks)

	assert.Len(t, got, 3)
	for i, task := range tasks {
		assert.Equal(t, task.ID, got[i].ID)
		assert.Equal(t, task.Name, got[i].Name)
		assert.Equal(t, string(task.Status), got[i].Status)
	}
}

func TestTasksFromModels_Empty(t *testing.T) {
	t.Parallel()

	got := TasksFromModels([]model.CallTask{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}
