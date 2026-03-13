package call

import (
	"context"

	"github.com/omeyang/clarion/internal/engine"
)

// DialogueStrategy 是异步业务决策接口。
// 在 hybrid 模式下，Omni 模型负责快速应答，
// DialogueStrategy 在后台分析对话内容并输出 Decision 指导 Omni 行为。
//
// Analyze 调用可能较慢（1-2s），调用方应在独立 goroutine 中执行。
type DialogueStrategy interface {
	// Analyze 分析用户输入和助手回复，返回业务决策。
	Analyze(ctx context.Context, input StrategyInput) (*Decision, error)
}

// StrategyInput 是传递给策略分析器的输入。
type StrategyInput struct {
	UserText      string            // 用户最新一轮输入。
	AssistantText string            // 助手最新一轮回复。
	TurnNumber    int               // 当前轮次编号。
	CurrentFields map[string]string // 已采集的业务字段。
}

// Decision 是策略分析器的输出。
type Decision struct {
	Intent          engine.Intent     // 用户意图。
	ExtractedFields map[string]string // 本轮新提取的字段。
	Instructions    string            // 注入给 RealtimeVoice 的更新指令。
	ShouldEnd       bool              // 是否应结束通话。
	Grade           engine.Grade      // 客户评级（可能每轮更新）。
}
