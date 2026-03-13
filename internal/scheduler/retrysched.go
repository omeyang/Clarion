package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// Enqueuer 定义外呼任务入队能力。
// Client 实现此接口；测试中可替换为桩实现。
type Enqueuer interface {
	EnqueueOutboundCall(ctx context.Context, p OutboundCallPayload, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// 编译期校验 Client 满足 Enqueuer。
var _ Enqueuer = (*Client)(nil)

// RetryScheduler 组合重试评估和任务入队，提供统一的重试调度。
type RetryScheduler struct {
	evaluator *RetryEvaluator
	enqueuer  Enqueuer
	logger    *slog.Logger
}

// NewRetryScheduler 创建重试调度器。
func NewRetryScheduler(evaluator *RetryEvaluator, enqueuer Enqueuer, logger *slog.Logger) *RetryScheduler {
	return &RetryScheduler{
		evaluator: evaluator,
		enqueuer:  enqueuer,
		logger:    logger,
	}
}

// uniqueTTL 是幂等去重键的过期时间。
// 设为 24 小时，覆盖最长可能的退避间隔（跨周末约 2-3 天），
// 同时避免永久占用 Redis 内存。
const uniqueTTL = 24 * time.Hour

// ScheduleRetry 根据呼叫结果评估是否需要重试，如需要则入队延迟任务。
// attemptNo 从 1 开始，表示已完成的尝试次数。
// 入队时自动将 payload.AttemptNo 设为 attemptNo+1，并附加幂等去重键。
// 返回重试决策；入队失败时同时返回错误。
func (s *RetryScheduler) ScheduleRetry(
	ctx context.Context,
	result CallResult,
	attemptNo int,
	payload OutboundCallPayload,
) (RetryDecision, error) {
	decision := s.evaluator.Evaluate(result, attemptNo)

	if !decision.ShouldRetry {
		s.logger.Info("重试评估：不重试",
			slog.Int64("call_id", payload.CallID),
			slog.String("result", string(result)),
			slog.String("reason", decision.Reason),
		)
		return decision, nil
	}

	// 递增尝试次数，构建下一次入队载荷。
	retryPayload := payload
	retryPayload.AttemptNo = attemptNo + 1

	_, err := s.enqueuer.EnqueueOutboundCall(ctx, retryPayload,
		asynq.ProcessIn(decision.Delay),
		asynq.Unique(uniqueTTL),
	)
	if err != nil {
		return decision, fmt.Errorf("入队重试任务: %w", err)
	}

	s.logger.Info("重试评估：已入队",
		slog.Int64("call_id", payload.CallID),
		slog.String("result", string(result)),
		slog.Int("attempt", attemptNo),
		slog.Int("next_attempt", retryPayload.AttemptNo),
		slog.Duration("delay", decision.Delay),
	)
	return decision, nil
}
