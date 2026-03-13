package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestCallFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	snapshotID := int64(42)

	m := &model.Call{
		ID:                 1,
		ContactID:          10,
		TaskID:             20,
		TemplateSnapshotID: &snapshotID,
		SessionID:          "sess-001",
		Status:             engine.CallCompleted,
		AnswerType:         engine.AnswerHuman,
		Duration:           120,
		RecordURL:          "https://example.com/record.mp3",
		Transcript:         "你好，请问有什么需要帮助的吗？",
		ExtractedFields:    json.RawMessage(`{"name":"张三"}`),
		ResultGrade:        engine.GradeA,
		NextAction:         "follow_up",
		RuleTrace:          json.RawMessage(`["rule1","rule2"]`),
		AISummary:          "客户表示感兴趣",
		CreatedAt:          now,
		UpdatedAt:          now.Add(time.Minute),
	}

	got := CallFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.ContactID, got.ContactID)
	assert.Equal(t, m.TaskID, got.TaskID)
	assert.Equal(t, m.TemplateSnapshotID, got.TemplateSnapshotID)
	assert.Equal(t, m.SessionID, got.SessionID)
	assert.Equal(t, string(m.Status), got.Status)
	assert.Equal(t, string(m.AnswerType), got.AnswerType)
	assert.Equal(t, m.Duration, got.Duration)
	assert.Equal(t, m.RecordURL, got.RecordURL)
	assert.Equal(t, m.Transcript, got.Transcript)
	assert.JSONEq(t, string(m.ExtractedFields), string(got.ExtractedFields))
	assert.Equal(t, string(m.ResultGrade), got.ResultGrade)
	assert.Equal(t, m.NextAction, got.NextAction)
	assert.JSONEq(t, string(m.RuleTrace), string(got.RuleTrace))
	assert.Equal(t, m.AISummary, got.AISummary)
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
	assert.Equal(t, m.UpdatedAt, got.UpdatedAt)
}

func TestCallFromModel_NilSnapshotID(t *testing.T) {
	t.Parallel()

	m := &model.Call{
		ID:                 1,
		TemplateSnapshotID: nil,
		Status:             engine.CallPending,
		AnswerType:         engine.AnswerUnknown,
		ResultGrade:        engine.GradeX,
	}

	got := CallFromModel(m)
	assert.Nil(t, got.TemplateSnapshotID)
}

func TestCallsFromModels(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	calls := []model.Call{
		{ID: 1, Status: engine.CallPending, AnswerType: engine.AnswerUnknown, ResultGrade: engine.GradeX, CreatedAt: now},
		{ID: 2, Status: engine.CallCompleted, AnswerType: engine.AnswerHuman, ResultGrade: engine.GradeA, CreatedAt: now},
		{ID: 3, Status: engine.CallFailed, AnswerType: engine.AnswerVoicemail, ResultGrade: engine.GradeD, CreatedAt: now},
	}

	got := CallsFromModels(calls)

	assert.Len(t, got, 3)
	for i, c := range calls {
		assert.Equal(t, c.ID, got[i].ID)
		assert.Equal(t, string(c.Status), got[i].Status)
	}
}

func TestCallsFromModels_Empty(t *testing.T) {
	t.Parallel()

	got := CallsFromModels([]model.Call{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestTurnFromModel(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	m := &model.DialogueTurn{
		ID:            100,
		CallID:        1,
		TurnNumber:    3,
		Speaker:       "bot",
		Content:       "请问您方便吗？",
		StateBefore:   "OPENING",
		StateAfter:    "QUALIFICATION",
		ASRLatencyMs:  150,
		LLMLatencyMs:  300,
		TTSLatencyMs:  80,
		ASRConfidence: 0.95,
		IsInterrupted: true,
		CreatedAt:     now,
	}

	got := TurnFromModel(m)

	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, m.CallID, got.CallID)
	assert.Equal(t, m.TurnNumber, got.TurnNumber)
	assert.Equal(t, m.Speaker, got.Speaker)
	assert.Equal(t, m.Content, got.Content)
	assert.Equal(t, m.StateBefore, got.StateBefore)
	assert.Equal(t, m.StateAfter, got.StateAfter)
	assert.Equal(t, m.ASRLatencyMs, got.ASRLatencyMs)
	assert.Equal(t, m.LLMLatencyMs, got.LLMLatencyMs)
	assert.Equal(t, m.TTSLatencyMs, got.TTSLatencyMs)
	assert.InDelta(t, m.ASRConfidence, got.ASRConfidence, 0.001)
	assert.Equal(t, m.IsInterrupted, got.IsInterrupted)
	assert.Equal(t, m.CreatedAt, got.CreatedAt)
}

func TestTurnsFromModels(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	turns := []model.DialogueTurn{
		{ID: 1, CallID: 10, TurnNumber: 1, Speaker: "bot", CreatedAt: now},
		{ID: 2, CallID: 10, TurnNumber: 2, Speaker: "user", CreatedAt: now},
	}

	got := TurnsFromModels(turns)

	assert.Len(t, got, 2)
	for i, turn := range turns {
		assert.Equal(t, turn.ID, got[i].ID)
		assert.Equal(t, turn.Speaker, got[i].Speaker)
		assert.Equal(t, turn.TurnNumber, got[i].TurnNumber)
	}
}

func TestTurnsFromModels_Empty(t *testing.T) {
	t.Parallel()

	got := TurnsFromModels([]model.DialogueTurn{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}
