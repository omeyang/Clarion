// Package guard 实现对话安全防护，包括输入过滤、输出校验和成本预算控制。
package guard

import (
	"sync"
	"time"
	"unicode/utf8"
)

// BudgetAction 表示预算检查后的建议操作。
type BudgetAction int

// 预算操作枚举。
const (
	// BudgetOK 预算充裕，正常处理。
	BudgetOK BudgetAction = iota
	// BudgetDegrade 预算紧张，降级为模板回复。
	BudgetDegrade
	// BudgetEnd 预算耗尽，礼貌结束通话。
	BudgetEnd
)

func (a BudgetAction) String() string {
	names := [...]string{"OK", "DEGRADE", "END"}
	if int(a) < len(names) {
		return names[a]
	}
	return "UNKNOWN"
}

// BudgetConfig 成本预算配置。
type BudgetConfig struct {
	// MaxTokens 整通电话 token 上限（输入+输出合计）。0 表示不限制。
	MaxTokens int `toml:"max_tokens" koanf:"max_tokens"`
	// MaxTurns 最大对话轮次。0 表示不限制。
	MaxTurns int `toml:"max_turns" koanf:"max_turns"`
	// MaxDuration 最长通话时长。0 表示不限制。
	MaxDuration time.Duration `toml:"max_duration" koanf:"max_duration"`
	// DegradeThreshold 降级阈值比例（0-1）。
	// 当任一维度使用量达到此比例时，切换为模板回复。
	// 默认 0.8（使用 80% 后降级）。
	DegradeThreshold float64 `toml:"degrade_threshold" koanf:"degrade_threshold"`
}

// CallBudget 跟踪单通电话的资源消耗，在预算紧张时触发降级或结束。
type CallBudget struct {
	mu        sync.Mutex
	config    BudgetConfig
	startTime time.Time

	usedTokens int
	usedTurns  int
}

// NewCallBudget 创建通话预算跟踪器。
func NewCallBudget(cfg BudgetConfig) *CallBudget {
	threshold := cfg.DegradeThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.8
	}
	cfg.DegradeThreshold = threshold

	return &CallBudget{
		config:    cfg,
		startTime: time.Now(),
	}
}

// RecordTokens 记录本轮消耗的 token 数。
func (b *CallBudget) RecordTokens(n int) {
	b.mu.Lock()
	b.usedTokens += n
	b.mu.Unlock()
}

// RecordTurn 记录一轮对话完成。
func (b *CallBudget) RecordTurn() {
	b.mu.Lock()
	b.usedTurns++
	b.mu.Unlock()
}

// Check 检查当前预算状态并返回建议操作。
func (b *CallBudget) Check() BudgetAction {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isExhausted() {
		return BudgetEnd
	}
	if b.isDegraded() {
		return BudgetDegrade
	}
	return BudgetOK
}

// UsedTokens 返回已使用的 token 数。
func (b *CallBudget) UsedTokens() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usedTokens
}

// UsedTurns 返回已使用的轮次数。
func (b *CallBudget) UsedTurns() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usedTurns
}

// Elapsed 返回通话已用时长。
func (b *CallBudget) Elapsed() time.Duration {
	return time.Since(b.startTime)
}

// isExhausted 检查任一维度是否耗尽（须持锁调用）。
func (b *CallBudget) isExhausted() bool {
	if b.config.MaxTokens > 0 && b.usedTokens >= b.config.MaxTokens {
		return true
	}
	if b.config.MaxTurns > 0 && b.usedTurns >= b.config.MaxTurns {
		return true
	}
	if b.config.MaxDuration > 0 && time.Since(b.startTime) >= b.config.MaxDuration {
		return true
	}
	return false
}

// isDegraded 检查任一维度是否达到降级阈值（须持锁调用）。
func (b *CallBudget) isDegraded() bool {
	threshold := b.config.DegradeThreshold

	if b.config.MaxTokens > 0 {
		ratio := float64(b.usedTokens) / float64(b.config.MaxTokens)
		if ratio >= threshold {
			return true
		}
	}
	if b.config.MaxTurns > 0 {
		ratio := float64(b.usedTurns) / float64(b.config.MaxTurns)
		if ratio >= threshold {
			return true
		}
	}
	if b.config.MaxDuration > 0 {
		ratio := float64(time.Since(b.startTime)) / float64(b.config.MaxDuration)
		if ratio >= threshold {
			return true
		}
	}
	return false
}

// EstimateTokens 根据文本长度估算 token 数。
// 中文约每 1.5 个字符一个 token，英文约每 4 个字符一个 token。
// 此为粗略估算，实际 token 数取决于模型分词器。
func EstimateTokens(text string) int {
	runeCount := utf8.RuneCountInString(text)
	if runeCount == 0 {
		return 0
	}
	// 中文场景为主，按 1.5 字符/token 估算，向上取整。
	return (runeCount*2 + 2) / 3
}
