package schema

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/model"
)

// CallResponse 是通话记录的 API 表示。
type CallResponse struct {
	ID                 int64           `json:"id"`
	ContactID          int64           `json:"contact_id"`
	TaskID             int64           `json:"task_id"`
	TemplateSnapshotID *int64          `json:"template_snapshot_id"`
	SessionID          string          `json:"session_id"`
	Status             string          `json:"status"`
	AnswerType         string          `json:"answer_type"`
	Duration           int             `json:"duration"`
	RecordURL          string          `json:"record_url"`
	Transcript         string          `json:"transcript"`
	ExtractedFields    json.RawMessage `json:"extracted_fields"`
	ResultGrade        string          `json:"result_grade"`
	NextAction         string          `json:"next_action"`
	RuleTrace          json.RawMessage `json:"rule_trace"`
	AISummary          string          `json:"ai_summary"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// CallFromModel 将 model.Call 转换为 CallResponse。
func CallFromModel(c *model.Call) CallResponse {
	return CallResponse{
		ID:                 c.ID,
		ContactID:          c.ContactID,
		TaskID:             c.TaskID,
		TemplateSnapshotID: c.TemplateSnapshotID,
		SessionID:          c.SessionID,
		Status:             string(c.Status),
		AnswerType:         string(c.AnswerType),
		Duration:           c.Duration,
		RecordURL:          c.RecordURL,
		Transcript:         c.Transcript,
		ExtractedFields:    c.ExtractedFields,
		ResultGrade:        string(c.ResultGrade),
		NextAction:         c.NextAction,
		RuleTrace:          c.RuleTrace,
		AISummary:          c.AISummary,
		CreatedAt:          c.CreatedAt,
		UpdatedAt:          c.UpdatedAt,
	}
}

// CallsFromModels 将 model.Call 切片批量转换。
func CallsFromModels(calls []model.Call) []CallResponse {
	result := make([]CallResponse, len(calls))
	for i := range calls {
		result[i] = CallFromModel(&calls[i])
	}
	return result
}

// TurnResponse 是对话轮次的 API 表示。
type TurnResponse struct {
	ID            int64     `json:"id"`
	CallID        int64     `json:"call_id"`
	TurnNumber    int       `json:"turn_number"`
	Speaker       string    `json:"speaker"`
	Content       string    `json:"content"`
	StateBefore   string    `json:"state_before"`
	StateAfter    string    `json:"state_after"`
	ASRLatencyMs  int       `json:"asr_latency_ms"`
	LLMLatencyMs  int       `json:"llm_latency_ms"`
	TTSLatencyMs  int       `json:"tts_latency_ms"`
	ASRConfidence float32   `json:"asr_confidence"`
	IsInterrupted bool      `json:"is_interrupted"`
	CreatedAt     time.Time `json:"created_at"`
}

// TurnFromModel 将 model.DialogueTurn 转换为 TurnResponse。
func TurnFromModel(t *model.DialogueTurn) TurnResponse {
	return TurnResponse{
		ID:            t.ID,
		CallID:        t.CallID,
		TurnNumber:    t.TurnNumber,
		Speaker:       t.Speaker,
		Content:       t.Content,
		StateBefore:   t.StateBefore,
		StateAfter:    t.StateAfter,
		ASRLatencyMs:  t.ASRLatencyMs,
		LLMLatencyMs:  t.LLMLatencyMs,
		TTSLatencyMs:  t.TTSLatencyMs,
		ASRConfidence: t.ASRConfidence,
		IsInterrupted: t.IsInterrupted,
		CreatedAt:     t.CreatedAt,
	}
}

// TurnsFromModels 将 model.DialogueTurn 切片批量转换。
func TurnsFromModels(turns []model.DialogueTurn) []TurnResponse {
	result := make([]TurnResponse, len(turns))
	for i := range turns {
		result[i] = TurnFromModel(&turns[i])
	}
	return result
}

// CallDetailResponse 包含通话记录及其对话轮次。
type CallDetailResponse struct {
	Call  CallResponse   `json:"call"`
	Turns []TurnResponse `json:"turns"`
}
