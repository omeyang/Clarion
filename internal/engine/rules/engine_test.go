package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omeyang/clarion/internal/engine"
)

func testConfig() TemplateConfig {
	return TemplateConfig{
		RequiredFields: []string{"name", "age", "budget"},
		MaxObjections:  2,
		MaxTurns:       20,
		GradingRules: GradingRules{
			AIntents:        []engine.Intent{engine.IntentInterested, engine.IntentSchedule},
			AMinFields:      3,
			BMinFields:      1,
			BMinTurns:       3,
			RejectIntents:   []engine.Intent{engine.IntentReject, engine.IntentNotInterested},
			InvalidStatuses: []engine.CallStatus{engine.CallNoAnswer, engine.CallVoicemail, engine.CallFailed},
		},
		Templates: map[string]string{
			"OPENING":               "您好，我是XX公司的客服小李。",
			"CLOSING":               "感谢您的时间，祝您生活愉快，再见！",
			"INFORMATION_GATHERING": "请问您的预算大概是多少？",
			"OBJECTION_HANDLING":    "理解您的想法，不知道什么时候方便聊几分钟？",
		},
	}
}

func testContext() *engine.DialogueContext {
	return &engine.DialogueContext{
		CurrentState:    engine.DialogueQualification,
		CollectedFields: make(map[string]string),
		RequiredFields:  []string{"name", "age", "budget"},
		MaxObjections:   2,
		MaxTurns:        20,
	}
}

func TestEngine_Evaluate_MergesExtractedFields(t *testing.T) {
	eng := NewEngine(testConfig())
	ctx := testContext()

	llmOut := LLMOutput{
		Intent:          engine.IntentContinue,
		ExtractedFields: map[string]string{"name": "张先生", "age": "35"},
		SuggestedReply:  "好的，请问预算多少？",
		Confidence:      0.9,
	}

	eng.Evaluate(llmOut, ctx)

	assert.Equal(t, "张先生", ctx.CollectedFields["name"])
	assert.Equal(t, "35", ctx.CollectedFields["age"])
}

func TestEngine_Evaluate_TracksObjections(t *testing.T) {
	eng := NewEngine(testConfig())
	ctx := testContext()

	llmOut := LLMOutput{
		Intent:     engine.IntentBusy,
		Confidence: 0.8,
	}

	eng.Evaluate(llmOut, ctx)
	assert.Equal(t, 1, ctx.ObjectionCount)

	eng.Evaluate(llmOut, ctx)
	assert.Equal(t, 2, ctx.ObjectionCount)
}

func TestEngine_Evaluate_TurnLimit(t *testing.T) {
	eng := NewEngine(testConfig())
	ctx := testContext()
	ctx.TurnCount = 19

	llmOut := LLMOutput{
		Intent:     engine.IntentContinue,
		Confidence: 0.9,
	}

	decision := eng.Evaluate(llmOut, ctx)
	assert.True(t, decision.ShouldEnd)
	assert.Equal(t, engine.DialogueClosing, decision.NextState)
}

func TestEngine_Evaluate_ReplyStrategy(t *testing.T) {
	tests := []struct {
		name       string
		state      engine.DialogueState
		intent     engine.Intent
		confidence float64
		want       ReplyStrategy
	}{
		{"opening with suggested reply uses LLM", engine.DialogueOpening, engine.IntentContinue, 0.9, ReplyLLM},
		{"closing uses template", engine.DialogueClosing, engine.IntentContinue, 0.9, ReplyTemplate},
		{"confirm with suggested reply uses LLM", engine.DialogueQualification, engine.IntentConfirm, 0.9, ReplyLLM},
		{"low confidence uses template", engine.DialogueQualification, engine.IntentContinue, 0.3, ReplyTemplate},
		{"normal uses LLM", engine.DialogueQualification, engine.IntentContinue, 0.9, ReplyLLM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := NewEngine(testConfig())
			ctx := testContext()
			ctx.CurrentState = tt.state

			llmOut := LLMOutput{
				Intent:         tt.intent,
				SuggestedReply: "test reply",
				Confidence:     tt.confidence,
			}

			decision := eng.Evaluate(llmOut, ctx)
			assert.Equal(t, tt.want, decision.ReplyStrategy)
		})
	}
}

func TestEngine_GradeCall(t *testing.T) {
	tests := []struct {
		name       string
		intent     engine.Intent
		collected  map[string]string
		turnCount  int
		objections int
		callStatus engine.CallStatus
		want       engine.Grade
	}{
		{
			name:       "grade X for no answer",
			callStatus: engine.CallNoAnswer,
			want:       engine.GradeX,
		},
		{
			name:       "grade X for voicemail",
			callStatus: engine.CallVoicemail,
			want:       engine.GradeX,
		},
		{
			name:       "grade D for reject",
			intent:     engine.IntentReject,
			callStatus: engine.CallCompleted,
			want:       engine.GradeD,
		},
		{
			name:       "grade A for interested with enough fields",
			intent:     engine.IntentInterested,
			collected:  map[string]string{"name": "张", "age": "35", "budget": "500万"},
			callStatus: engine.CallCompleted,
			want:       engine.GradeA,
		},
		{
			name:       "grade B for some engagement",
			intent:     engine.IntentContinue,
			collected:  map[string]string{"name": "张"},
			turnCount:  5,
			callStatus: engine.CallCompleted,
			want:       engine.GradeB,
		},
		{
			name:       "grade C for hesitation",
			intent:     engine.IntentHesitate,
			callStatus: engine.CallCompleted,
			want:       engine.GradeC,
		},
		{
			name:       "grade C for objection with engagement",
			intent:     engine.IntentContinue,
			objections: 1,
			callStatus: engine.CallCompleted,
			want:       engine.GradeC,
		},
		{
			name:       "grade D default",
			intent:     engine.IntentUnknown,
			callStatus: engine.CallCompleted,
			want:       engine.GradeD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := NewEngine(testConfig())
			ctx := &engine.DialogueContext{
				Intent:          tt.intent,
				CollectedFields: tt.collected,
				ObjectionCount:  tt.objections,
				TurnCount:       tt.turnCount,
			}
			if ctx.CollectedFields == nil {
				ctx.CollectedFields = make(map[string]string)
			}

			got := eng.GradeCall(ctx, tt.callStatus)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReplyStrategy_String(t *testing.T) {
	assert.Equal(t, "TEMPLATE", ReplyTemplate.String())
	assert.Equal(t, "LLM", ReplyLLM.String())
	assert.Equal(t, "PRECOMPILED", ReplyPrecompiled.String())
}
