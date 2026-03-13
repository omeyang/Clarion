package scheduler

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
)

// Client 封装 Asynq 客户端，用于入队外呼任务。
type Client struct {
	c     *asynq.Client
	queue string
}

// NewClient 创建新的调度客户端。
func NewClient(redisOpt asynq.RedisClientOpt, queue string) *Client {
	return &Client{
		c:     asynq.NewClient(redisOpt),
		queue: queue,
	}
}

// EnqueueOutboundCall 将外呼任务加入队列。
// 默认设置 MaxRetry(0)，由业务层控制重试策略。
func (c *Client) EnqueueOutboundCall(ctx context.Context, p OutboundCallPayload, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	task, err := NewOutboundCallTask(p)
	if err != nil {
		return nil, fmt.Errorf("create outbound call task: %w", err)
	}

	defaults := []asynq.Option{
		asynq.Queue(c.queue),
		asynq.MaxRetry(0),
	}
	opts = append(defaults, opts...)

	info, err := c.c.EnqueueContext(ctx, task, opts...)
	if err != nil {
		return nil, fmt.Errorf("enqueue outbound call: %w", err)
	}
	return info, nil
}

// Close 关闭调度客户端。
func (c *Client) Close() error {
	if err := c.c.Close(); err != nil {
		return fmt.Errorf("close asynq client: %w", err)
	}
	return nil
}
