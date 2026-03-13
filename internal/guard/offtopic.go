package guard

import (
	"sync"

	"github.com/omeyang/clarion/internal/engine"
)

// OffTopicAction 表示离题检测后的建议操作。
type OffTopicAction int

// 离题操作枚举。
const (
	// OffTopicOK 对话正常，无需干预。
	OffTopicOK OffTopicAction = iota
	// OffTopicConverge 连续离题达到收束阈值，建议将对话拉回正轨。
	OffTopicConverge
	// OffTopicEnd 连续离题达到结束阈值，建议结束通话。
	OffTopicEnd
)

func (a OffTopicAction) String() string {
	names := [...]string{"OK", "CONVERGE", "END"}
	if int(a) < len(names) {
		return names[a]
	}
	return "UNKNOWN"
}

// OffTopicConfig 配置离题计数器。
type OffTopicConfig struct {
	// ConvergeAfter 连续离题达到此轮次后触发收束。0 使用默认值。
	ConvergeAfter int
	// EndAfter 连续离题达到此轮次后触发结束。0 使用默认值。
	EndAfter int
	// OffTopicIntents 被视为离题的意图集合。空列表使用默认值。
	OffTopicIntents []engine.Intent
}

// 默认阈值。
const (
	defaultConvergeAfter = 2
	defaultEndAfter      = 4
)

// defaultOffTopicIntents 默认被视为离题的意图。
// unknown 表示 LLM 无法识别意图，通常是用户聊到了业务范围外。
var defaultOffTopicIntents = []engine.Intent{
	engine.IntentUnknown,
}

// OffTopicTracker 跟踪连续离题轮次，触发收束或结束。
// 每轮对话结束后调用 Record 传入该轮意图，Check 返回建议操作。
// 一旦对话回到正轨（非离题意图），计数器自动重置。
type OffTopicTracker struct {
	mu              sync.Mutex
	convergeAfter   int
	endAfter        int
	offTopicIntents map[engine.Intent]bool
	consecutive     int
}

// NewOffTopicTracker 创建离题计数器。
func NewOffTopicTracker(cfg OffTopicConfig) *OffTopicTracker {
	converge := cfg.ConvergeAfter
	if converge <= 0 {
		converge = defaultConvergeAfter
	}
	end := cfg.EndAfter
	if end <= 0 {
		end = defaultEndAfter
	}
	// 确保结束阈值大于收束阈值。
	if end <= converge {
		end = converge + 2
	}

	intents := cfg.OffTopicIntents
	if len(intents) == 0 {
		intents = defaultOffTopicIntents
	}
	im := make(map[engine.Intent]bool, len(intents))
	for _, v := range intents {
		im[v] = true
	}

	return &OffTopicTracker{
		convergeAfter:   converge,
		endAfter:        end,
		offTopicIntents: im,
	}
}

// Record 记录本轮意图并返回当前建议操作。
// 离题意图累加计数，正常意图重置计数。
func (t *OffTopicTracker) Record(intent engine.Intent) OffTopicAction {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.offTopicIntents[intent] {
		t.consecutive++
	} else {
		t.consecutive = 0
	}

	return t.action()
}

// Check 返回当前建议操作（不修改状态）。
func (t *OffTopicTracker) Check() OffTopicAction {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.action()
}

// Consecutive 返回当前连续离题轮次数。
func (t *OffTopicTracker) Consecutive() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consecutive
}

// Reset 重置计数器。
func (t *OffTopicTracker) Reset() {
	t.mu.Lock()
	t.consecutive = 0
	t.mu.Unlock()
}

// action 根据当前计数返回操作（须持锁调用）。
func (t *OffTopicTracker) action() OffTopicAction {
	if t.consecutive >= t.endAfter {
		return OffTopicEnd
	}
	if t.consecutive >= t.convergeAfter {
		return OffTopicConverge
	}
	return OffTopicOK
}
