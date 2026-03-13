package postprocess

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/provider"
)

func TestOpportunityExtractor_Extract_NilLLM(t *testing.T) {
	ext := NewOpportunityExtractor(nil, slog.Default())
	event := &CallCompletionEvent{
		CallID:    1,
		ContactID: 10,
		TaskID:    100,
		Grade:     engine.GradeA,
	}

	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)

	assert.Equal(t, int64(1), opp.CallID)
	assert.Equal(t, int64(10), opp.ContactID)
	assert.Equal(t, int64(100), opp.TaskID)
	assert.Equal(t, 85, opp.Score)
	assert.Equal(t, "interested", opp.IntentType)
	assert.Equal(t, "transfer_human", opp.FollowupAction)
	assert.True(t, opp.NeedsHumanReview)
}

func TestOpportunityExtractor_Extract_LLMSuccess(t *testing.T) {
	llmResp := llmOpportunity{
		Score:          75,
		IntentType:     "interested",
		BudgetSignal:   "has_budget",
		TimelineSignal: "soon",
		ContactRole:    "decision_maker",
		PainPoints:     []string{"效率低", "成本高"},
		FollowupAction: "follow_up",
		FollowupDate:   "2026-03-20",
	}
	respBytes, _ := json.Marshal(llmResp)

	ext := NewOpportunityExtractor(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return string(respBytes), nil
		},
	}, slog.Default())

	event := &CallCompletionEvent{
		CallID:    2,
		ContactID: 20,
		TaskID:    200,
		Grade:     engine.GradeA,
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
			{Number: 2, Speaker: "user", Content: "我们正在找方案"},
		},
		CollectedFields: map[string]string{"company": "测试公司"},
	}

	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)

	assert.Equal(t, 75, opp.Score)
	assert.Equal(t, "interested", opp.IntentType)
	assert.Equal(t, "has_budget", opp.BudgetSignal)
	assert.Equal(t, "soon", opp.TimelineSignal)
	assert.Equal(t, "decision_maker", opp.ContactRole)
	assert.Equal(t, []string{"效率低", "成本高"}, opp.PainPoints)
	assert.Equal(t, "follow_up", opp.FollowupAction)
	require.NotNil(t, opp.FollowupDate)
	assert.Equal(t, "2026-03-20", opp.FollowupDate.Format("2006-01-02"))
	assert.True(t, opp.NeedsHumanReview, "分数>=70 应标记人工复核")
}

func TestOpportunityExtractor_Extract_LLMError(t *testing.T) {
	ext := NewOpportunityExtractor(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return "", errors.New("llm timeout")
		},
	}, slog.Default())

	event := &CallCompletionEvent{
		CallID: 3,
		Grade:  engine.GradeB,
	}

	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, 60, opp.Score, "LLM 失败时应回退到规则评分")
	assert.Equal(t, "needs_info", opp.IntentType)
}

func TestOpportunityExtractor_Extract_InvalidJSON(t *testing.T) {
	ext := NewOpportunityExtractor(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return "这不是 JSON", nil
		},
	}, slog.Default())

	event := &CallCompletionEvent{
		CallID: 4,
		Grade:  engine.GradeC,
	}

	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, 30, opp.Score, "JSON 解析失败应回退到规则评分")
}

func TestOpportunityExtractor_Extract_LLMResponseWithExtraText(t *testing.T) {
	resp := `好的，以下是分析结果：
{"score": 50, "intent_type": "needs_info", "budget_signal": "not_mentioned", "timeline_signal": "later", "contact_role": "user", "pain_points": [], "followup_action": "follow_up", "followup_date": ""}
希望对你有帮助。`

	ext := NewOpportunityExtractor(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return resp, nil
		},
	}, slog.Default())

	event := &CallCompletionEvent{CallID: 5, Grade: engine.GradeB}
	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, 50, opp.Score)
	assert.Equal(t, "needs_info", opp.IntentType)
}

func TestOpportunityExtractor_Extract_TransferHumanNeedsReview(t *testing.T) {
	resp := `{"score": 40, "intent_type": "callback", "budget_signal": "not_mentioned", "timeline_signal": "not_mentioned", "contact_role": "unknown", "pain_points": [], "followup_action": "transfer_human", "followup_date": ""}`

	ext := NewOpportunityExtractor(&mockLLM{
		generateFn: func(_ context.Context, _ []provider.Message, _ provider.LLMConfig) (string, error) {
			return resp, nil
		},
	}, slog.Default())

	event := &CallCompletionEvent{CallID: 6, Grade: engine.GradeC}
	opp, err := ext.Extract(context.Background(), event)
	require.NoError(t, err)
	assert.True(t, opp.NeedsHumanReview, "transfer_human 应标记人工复核")
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "纯 JSON",
			input: `{"score": 80}`,
			want:  `{"score": 80}`,
		},
		{
			name:  "前后有文本",
			input: "分析结果：\n{\"score\": 80}\n以上。",
			want:  `{"score": 80}`,
		},
		{
			name:  "无 JSON",
			input: "没有 JSON 内容",
			want:  "没有 JSON 内容",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClampScore(t *testing.T) {
	assert.Equal(t, 0, clampScore(-10))
	assert.Equal(t, 0, clampScore(0))
	assert.Equal(t, 50, clampScore(50))
	assert.Equal(t, 100, clampScore(100))
	assert.Equal(t, 100, clampScore(150))
}

func TestNormalizeEnum(t *testing.T) {
	assert.Equal(t, "interested", normalizeEnum("interested", validIntentTypes))
	assert.Equal(t, "unknown", normalizeEnum("invalid_value", validIntentTypes))
	assert.Equal(t, "not_mentioned", normalizeEnum("bad", validBudgetSignals))
}

func TestGradeToScore(t *testing.T) {
	tests := []struct {
		grade engine.Grade
		want  int
	}{
		{engine.GradeA, 85},
		{engine.GradeB, 60},
		{engine.GradeC, 30},
		{engine.GradeD, 10},
		{engine.GradeX, 0},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, gradeToScore(tt.grade))
	}
}

func TestGradeToIntentType(t *testing.T) {
	assert.Equal(t, "interested", gradeToIntentType(engine.GradeA))
	assert.Equal(t, "needs_info", gradeToIntentType(engine.GradeB))
	assert.Equal(t, "not_interested", gradeToIntentType(engine.GradeC))
	assert.Equal(t, "not_interested", gradeToIntentType(engine.GradeD))
	assert.Equal(t, "unknown", gradeToIntentType(engine.GradeX))
}

func TestGradeToFollowupAction(t *testing.T) {
	assert.Equal(t, "transfer_human", gradeToFollowupAction(engine.GradeA))
	assert.Equal(t, "follow_up", gradeToFollowupAction(engine.GradeB))
	assert.Equal(t, "follow_up", gradeToFollowupAction(engine.GradeC))
	assert.Equal(t, "abandon", gradeToFollowupAction(engine.GradeD))
	assert.Equal(t, "abandon", gradeToFollowupAction(engine.GradeX))
}

func TestBuildOpportunityPrompt(t *testing.T) {
	turns := []dialogue.Turn{
		{Speaker: "bot", Content: "你好"},
		{Speaker: "user", Content: "你好，我想了解一下"},
	}
	fields := map[string]string{"company": "ABC"}

	prompt := buildOpportunityPrompt(turns, fields, engine.GradeA)

	assert.Contains(t, prompt, "[bot] 你好")
	assert.Contains(t, prompt, "[user] 你好，我想了解一下")
	assert.Contains(t, prompt, "company: ABC")
	assert.Contains(t, prompt, "A")
}

func TestBuildOpportunityPrompt_NoFields(t *testing.T) {
	turns := []dialogue.Turn{{Speaker: "bot", Content: "hi"}}
	prompt := buildOpportunityPrompt(turns, nil, engine.GradeC)
	assert.Contains(t, prompt, "（无）")
}

func TestFillFromRules_AllGrades(t *testing.T) {
	ext := NewOpportunityExtractor(nil, slog.Default())

	tests := []struct {
		grade           engine.Grade
		wantScore       int
		wantIntent      string
		wantAction      string
		wantHumanReview bool
	}{
		{engine.GradeA, 85, "interested", "transfer_human", true},
		{engine.GradeB, 60, "needs_info", "follow_up", true},
		{engine.GradeC, 30, "not_interested", "follow_up", false},
		{engine.GradeD, 10, "not_interested", "abandon", false},
		{engine.GradeX, 0, "unknown", "abandon", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.grade), func(t *testing.T) {
			opp := &Opportunity{}
			event := &CallCompletionEvent{Grade: tt.grade}
			ext.fillFromRules(opp, event)

			assert.Equal(t, tt.wantScore, opp.Score)
			assert.Equal(t, tt.wantIntent, opp.IntentType)
			assert.Equal(t, tt.wantAction, opp.FollowupAction)
			assert.Equal(t, tt.wantHumanReview, opp.NeedsHumanReview)
			assert.Equal(t, "not_mentioned", opp.BudgetSignal)
			assert.Equal(t, "not_mentioned", opp.TimelineSignal)
			assert.Equal(t, "unknown", opp.ContactRole)
		})
	}
}
