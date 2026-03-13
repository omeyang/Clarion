package guard

import (
	"sync"
	"testing"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/stretchr/testify/assert"
)

func TestOffTopicAction_String(t *testing.T) {
	tests := []struct {
		action OffTopicAction
		want   string
	}{
		{OffTopicOK, "OK"},
		{OffTopicConverge, "CONVERGE"},
		{OffTopicEnd, "END"},
		{OffTopicAction(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.action.String())
	}
}

func TestNewOffTopicTracker_Defaults(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{})
	assert.Equal(t, defaultConvergeAfter, tr.convergeAfter)
	assert.Equal(t, defaultConvergeAfter+2, tr.endAfter) // 默认 endAfter = defaultEndAfter = 4
	assert.True(t, tr.offTopicIntents[engine.IntentUnknown])
}

func TestNewOffTopicTracker_CustomConfig(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{
		ConvergeAfter:   3,
		EndAfter:        6,
		OffTopicIntents: []engine.Intent{engine.IntentUnknown, engine.IntentBusy},
	})
	assert.Equal(t, 3, tr.convergeAfter)
	assert.Equal(t, 6, tr.endAfter)
	assert.True(t, tr.offTopicIntents[engine.IntentUnknown])
	assert.True(t, tr.offTopicIntents[engine.IntentBusy])
	assert.False(t, tr.offTopicIntents[engine.IntentContinue])
}

func TestNewOffTopicTracker_EndMustExceedConverge(t *testing.T) {
	// endAfter <= convergeAfter 时自动修正。
	tr := NewOffTopicTracker(OffTopicConfig{
		ConvergeAfter: 5,
		EndAfter:      3,
	})
	assert.Equal(t, 5, tr.convergeAfter)
	assert.Equal(t, 7, tr.endAfter) // converge + 2
}

func TestOffTopicTracker_NormalIntentsKeepOK(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{})
	normalIntents := []engine.Intent{
		engine.IntentContinue, engine.IntentInterested,
		engine.IntentConfirm, engine.IntentReject,
	}
	for _, intent := range normalIntents {
		action := tr.Record(intent)
		assert.Equal(t, OffTopicOK, action, "正常意图 %q 应返回 OK", intent)
	}
	assert.Equal(t, 0, tr.Consecutive())
}

func TestOffTopicTracker_ConvergeAfterThreshold(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})

	// 第 1 轮离题：OK。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 1, tr.Consecutive())

	// 第 2 轮离题：收束。
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 2, tr.Consecutive())

	// 第 3 轮离题：仍然收束。
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 3, tr.Consecutive())
}

func TestOffTopicTracker_EndAfterThreshold(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})

	for range 3 {
		tr.Record(engine.IntentUnknown)
	}
	// 第 4 轮离题：结束。
	assert.Equal(t, OffTopicEnd, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 4, tr.Consecutive())
}

func TestOffTopicTracker_ResetOnNormalIntent(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})

	// 连续 2 轮离题达到收束。
	tr.Record(engine.IntentUnknown)
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))

	// 一轮正常意图重置计数。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentContinue))
	assert.Equal(t, 0, tr.Consecutive())

	// 重新开始计数。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 1, tr.Consecutive())
}

func TestOffTopicTracker_CheckDoesNotModifyState(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})
	tr.Record(engine.IntentUnknown)

	// Check 不应改变计数。
	assert.Equal(t, OffTopicOK, tr.Check())
	assert.Equal(t, 1, tr.Consecutive())

	// 再 Record 一次，应变为收束。
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))
}

func TestOffTopicTracker_Reset(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})
	tr.Record(engine.IntentUnknown)
	tr.Record(engine.IntentUnknown)
	assert.Equal(t, 2, tr.Consecutive())

	tr.Reset()
	assert.Equal(t, 0, tr.Consecutive())
	assert.Equal(t, OffTopicOK, tr.Check())
}

func TestOffTopicTracker_CustomOffTopicIntents(t *testing.T) {
	// 将 busy 也作为离题意图。
	tr := NewOffTopicTracker(OffTopicConfig{
		ConvergeAfter:   2,
		EndAfter:        4,
		OffTopicIntents: []engine.Intent{engine.IntentUnknown, engine.IntentBusy},
	})

	// busy 计为离题。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentBusy))
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))
	assert.Equal(t, 2, tr.Consecutive())

	// interested 不是离题，重置计数。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentInterested))
	assert.Equal(t, 0, tr.Consecutive())
}

func TestOffTopicTracker_ConcurrentAccess(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 100, EndAfter: 200})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				tr.Record(engine.IntentUnknown)
				tr.Check()
				tr.Consecutive()
			}
		}()
	}
	wg.Wait()
	// 500 轮离题，应为结束状态。
	assert.Equal(t, OffTopicEnd, tr.Check())
}

func TestOffTopicTracker_ExactBoundary(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 2, EndAfter: 4})

	// convergeAfter-1 轮仍为 OK。
	assert.Equal(t, OffTopicOK, tr.Record(engine.IntentUnknown))

	// 恰好 convergeAfter 轮为 Converge。
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))

	// endAfter-1 轮仍为 Converge。
	assert.Equal(t, OffTopicConverge, tr.Record(engine.IntentUnknown))

	// 恰好 endAfter 轮为 End。
	assert.Equal(t, OffTopicEnd, tr.Record(engine.IntentUnknown))
}

func TestOffTopicTracker_EndPersistsAfterThreshold(t *testing.T) {
	tr := NewOffTopicTracker(OffTopicConfig{ConvergeAfter: 1, EndAfter: 3})

	tr.Record(engine.IntentUnknown)
	tr.Record(engine.IntentUnknown)
	assert.Equal(t, OffTopicEnd, tr.Record(engine.IntentUnknown))

	// 超过阈值后继续离题仍为 End。
	assert.Equal(t, OffTopicEnd, tr.Record(engine.IntentUnknown))
	assert.Equal(t, OffTopicEnd, tr.Record(engine.IntentUnknown))
}
