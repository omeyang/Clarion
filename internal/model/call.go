package model

import (
	"encoding/json"
	"time"

	"github.com/omeyang/clarion/internal/engine"
)

// Call 表示单条通话记录。
type Call struct {
	ID                 int64             `json:"id"`
	TenantID           string            `json:"tenant_id"`
	ContactID          int64             `json:"contact_id"`
	TaskID             int64             `json:"task_id"`
	TemplateSnapshotID *int64            `json:"template_snapshot_id"`
	SessionID          string            `json:"session_id"`
	Status             engine.CallStatus `json:"status"`
	AnswerType         engine.AnswerType `json:"answer_type"`
	Duration           int               `json:"duration"`
	RecordURL          string            `json:"record_url"`
	Transcript         string            `json:"transcript"`
	ExtractedFields    json.RawMessage   `json:"extracted_fields"`
	ResultGrade        engine.Grade      `json:"result_grade"`
	NextAction         string            `json:"next_action"`
	RuleTrace          json.RawMessage   `json:"rule_trace"`
	AISummary          string            `json:"ai_summary"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// DialogueTurn 表示通话对话中的单个轮次。
type DialogueTurn struct {
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

// CallEvent 表示带毫秒时间戳的媒体层事件。
type CallEvent struct {
	ID           int64            `json:"id"`
	CallID       int64            `json:"call_id"`
	EventType    engine.EventType `json:"event_type"`
	TimestampMs  int64            `json:"timestamp_ms"`
	MetadataJSON json.RawMessage  `json:"metadata_json"`
	CreatedAt    time.Time        `json:"created_at"`
}
