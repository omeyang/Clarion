package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestTemplateFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	m := &model.ScenarioTemplate{
		ID:                   1,
		Name:                 "催收场景模板",
		Domain:               "collection",
		OpeningScript:        "您好，这里是XX公司",
		StateMachineConfig:   json.RawMessage(`{"states":["opening","closing"]}`),
		ExtractionSchema:     json.RawMessage(`{"fields":["name","amount"]}`),
		GradingRules:         json.RawMessage(`{"A":"paid","D":"refused"}`),
		PromptTemplates:      json.RawMessage(`{"opening":"你好"}`),
		NotificationConfig:   json.RawMessage(`{"webhook":"https://example.com"}`),
		CallProtectionConfig: json.RawMessage(`{"max_duration":300}`),
		PrecompiledAudios:    json.RawMessage(`["audio1.mp3"]`),
		Status:               engine.TemplatePublished,
		Version:              3,
		CreatedAt:            now,
		UpdatedAt:            now.Add(24 * time.Hour),
	}

	got := TemplateFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.Name, got.Name)
	assert.Equal(t, m.Domain, got.Domain)
	assert.Equal(t, m.OpeningScript, got.OpeningScript)
	assert.JSONEq(t, string(m.StateMachineConfig), string(got.StateMachineConfig))
	assert.JSONEq(t, string(m.ExtractionSchema), string(got.ExtractionSchema))
	assert.JSONEq(t, string(m.GradingRules), string(got.GradingRules))
	assert.JSONEq(t, string(m.PromptTemplates), string(got.PromptTemplates))
	assert.JSONEq(t, string(m.NotificationConfig), string(got.NotificationConfig))
	assert.JSONEq(t, string(m.CallProtectionConfig), string(got.CallProtectionConfig))
	assert.JSONEq(t, string(m.PrecompiledAudios), string(got.PrecompiledAudios))
	assert.Equal(t, string(m.Status), got.Status)
	assert.Equal(t, m.Version, got.Version)
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
	assert.Equal(t, m.UpdatedAt, got.UpdatedAt)
}

func TestTemplateFromModel_NilJSON字段(t *testing.T) {
	t.Parallel()

	m := &model.ScenarioTemplate{
		ID:     2,
		Status: engine.TemplateDraft,
	}

	got := TemplateFromModel(m)

	assert.Nil(t, got.StateMachineConfig)
	assert.Nil(t, got.ExtractionSchema)
	assert.Nil(t, got.GradingRules)
	assert.Nil(t, got.PromptTemplates)
	assert.Nil(t, got.NotificationConfig)
	assert.Nil(t, got.CallProtectionConfig)
	assert.Nil(t, got.PrecompiledAudios)
}

func TestTemplatesFromModels(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	templates := []model.ScenarioTemplate{
		{ID: 1, Name: "模板一", Status: engine.TemplateDraft, Version: 1, CreatedAt: now},
		{ID: 2, Name: "模板二", Status: engine.TemplateActive, Version: 2, CreatedAt: now},
		{ID: 3, Name: "模板三", Status: engine.TemplateArchived, Version: 5, CreatedAt: now},
	}

	got := TemplatesFromModels(templates)

	assert.Len(t, got, 3)
	for i, tmpl := range templates {
		assert.Equal(t, tmpl.ID, got[i].ID)
		assert.Equal(t, tmpl.Name, got[i].Name)
		assert.Equal(t, string(tmpl.Status), got[i].Status)
		assert.Equal(t, tmpl.Version, got[i].Version)
	}
}

func TestTemplatesFromModels_Empty(t *testing.T) {
	t.Parallel()

	got := TemplatesFromModels([]model.ScenarioTemplate{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestSnapshotFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	m := &model.TemplateSnapshot{
		ID:           1,
		TemplateID:   10,
		SnapshotData: json.RawMessage(`{"name":"快照数据","version":3}`),
		CreatedAt:    now,
	}

	got := SnapshotFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.TemplateID, got.TemplateID)
	assert.JSONEq(t, string(m.SnapshotData), string(got.SnapshotData))
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
}

func TestSnapshotFromModel_NilSnapshotData(t *testing.T) {
	t.Parallel()

	m := &model.TemplateSnapshot{
		ID:           2,
		TemplateID:   20,
		SnapshotData: nil,
	}

	got := SnapshotFromModel(m)
	assert.Nil(t, got.SnapshotData)
}
