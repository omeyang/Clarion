package guard

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetAction_String(t *testing.T) {
	tests := []struct {
		action BudgetAction
		want   string
	}{
		{BudgetOK, "OK"},
		{BudgetDegrade, "DEGRADE"},
		{BudgetEnd, "END"},
		{BudgetAction(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.action.String())
	}
}

func TestNewCallBudget_DefaultThreshold(t *testing.T) {
	b := NewCallBudget(BudgetConfig{MaxTokens: 100})
	assert.InDelta(t, 0.8, b.config.DegradeThreshold, 0.001)
}

func TestNewCallBudget_CustomThreshold(t *testing.T) {
	b := NewCallBudget(BudgetConfig{MaxTokens: 100, DegradeThreshold: 0.7})
	assert.InDelta(t, 0.7, b.config.DegradeThreshold, 0.001)
}

func TestNewCallBudget_InvalidThreshold(t *testing.T) {
	// 零值和负值应回退到默认 0.8。
	b := NewCallBudget(BudgetConfig{MaxTokens: 100, DegradeThreshold: 0})
	assert.InDelta(t, 0.8, b.config.DegradeThreshold, 0.001)

	b2 := NewCallBudget(BudgetConfig{MaxTokens: 100, DegradeThreshold: -0.5})
	assert.InDelta(t, 0.8, b2.config.DegradeThreshold, 0.001)
}

func TestCallBudget_Check_NoLimits(t *testing.T) {
	// 无限制时始终返回 OK。
	b := NewCallBudget(BudgetConfig{})
	b.RecordTokens(999999)
	b.RecordTurn()
	assert.Equal(t, BudgetOK, b.Check())
}

func TestCallBudget_Check_TokenLimit(t *testing.T) {
	b := NewCallBudget(BudgetConfig{
		MaxTokens:        100,
		DegradeThreshold: 0.8,
	})

	// 初始状态：OK。
	assert.Equal(t, BudgetOK, b.Check())

	// 使用 79 个 token：仍然 OK。
	b.RecordTokens(79)
	assert.Equal(t, BudgetOK, b.Check())

	// 使用 80 个 token（80%）：降级。
	b.RecordTokens(1)
	assert.Equal(t, BudgetDegrade, b.Check())

	// 使用 100 个 token：结束。
	b.RecordTokens(20)
	assert.Equal(t, BudgetEnd, b.Check())
}

func TestCallBudget_Check_TurnLimit(t *testing.T) {
	b := NewCallBudget(BudgetConfig{
		MaxTurns:         10,
		DegradeThreshold: 0.8,
	})

	// 7 轮：OK。
	for range 7 {
		b.RecordTurn()
	}
	assert.Equal(t, BudgetOK, b.Check())

	// 第 8 轮（80%）：降级。
	b.RecordTurn()
	assert.Equal(t, BudgetDegrade, b.Check())

	// 10 轮：结束。
	b.RecordTurn()
	b.RecordTurn()
	assert.Equal(t, BudgetEnd, b.Check())
}

func TestCallBudget_Check_DurationLimit(t *testing.T) {
	b := NewCallBudget(BudgetConfig{
		MaxDuration:      100 * time.Millisecond,
		DegradeThreshold: 0.5,
	})

	// 刚开始：OK。
	assert.Equal(t, BudgetOK, b.Check())

	// 等待超过降级阈值（50ms）。
	time.Sleep(55 * time.Millisecond)
	assert.Equal(t, BudgetDegrade, b.Check())

	// 等待超过最大时长。
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, BudgetEnd, b.Check())
}

func TestCallBudget_RecordTokens_And_UsedTokens(t *testing.T) {
	b := NewCallBudget(BudgetConfig{MaxTokens: 1000})
	b.RecordTokens(100)
	b.RecordTokens(200)
	assert.Equal(t, 300, b.UsedTokens())
}

func TestCallBudget_RecordTurn_And_UsedTurns(t *testing.T) {
	b := NewCallBudget(BudgetConfig{MaxTurns: 20})
	b.RecordTurn()
	b.RecordTurn()
	b.RecordTurn()
	assert.Equal(t, 3, b.UsedTurns())
}

func TestCallBudget_Elapsed(t *testing.T) {
	b := NewCallBudget(BudgetConfig{})
	time.Sleep(10 * time.Millisecond)
	assert.GreaterOrEqual(t, b.Elapsed(), 10*time.Millisecond)
}

func TestCallBudget_ConcurrentAccess(t *testing.T) {
	b := NewCallBudget(BudgetConfig{
		MaxTokens: 100000,
		MaxTurns:  10000,
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				b.RecordTokens(1)
				b.RecordTurn()
				b.Check()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1000, b.UsedTokens())
	assert.Equal(t, 1000, b.UsedTurns())
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"空字符串", "", 0},
		{"短中文", "你好", 2},         // 2 * 2 / 3 = 1.33 → 2 (ceiling)
		{"中等中文", "好的我了解了", 4},    // 6 * 2 / 3 = 4
		{"英文", "hello world", 8}, // 11 * 2 / 3 = 7.33 → 8 (ceiling)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			require.Greater(t, got+1, 0, "token 数应为正数或零")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCallBudget_MultiDimension(t *testing.T) {
	// 多维度同时限制时，任一维度触发即生效。
	b := NewCallBudget(BudgetConfig{
		MaxTokens:        1000,
		MaxTurns:         5,
		DegradeThreshold: 0.8,
	})

	// 轮次到 4 轮（80%），即使 token 充裕也应降级。
	for range 4 {
		b.RecordTurn()
	}
	b.RecordTokens(100) // 仅 10% token。
	assert.Equal(t, BudgetDegrade, b.Check())
}
