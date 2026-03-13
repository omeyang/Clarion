// Package scheduler 封装基于 Asynq 的异步任务调度。
//
// 提供外呼任务的类型定义、载荷序列化和 Asynq 客户端封装。
// 替代之前基于 Redis BRPOP 的轮询消费模式，支持延迟任务、
// 唯一任务和监控面板（Asynqmon）。
package scheduler

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/omeyang/clarion/internal/config"
)

// TaskTypeOutboundCall 是外呼任务类型标识。
const TaskTypeOutboundCall = "outbound:call"

// OutboundCallPayload 是外呼任务的有效载荷。
type OutboundCallPayload struct {
	CallID     int64  `json:"call_id"`
	ContactID  int64  `json:"contact_id"`
	TaskID     int64  `json:"task_id"`
	Phone      string `json:"phone"`
	Gateway    string `json:"gateway"`
	CallerID   string `json:"caller_id"`
	TemplateID int64  `json:"template_id"`
	// AttemptNo 当前尝试次数，从 1 开始。首次呼叫为 1，首次重试为 2。
	AttemptNo int `json:"attempt_no"`
	// RecoveryFromCallID 非零时表示这是一次恢复呼叫，值为被中断通话的 CallID。
	// Worker 收到此字段后会从 Redis 加载会话快照，恢复对话状态。
	RecoveryFromCallID int64 `json:"recovery_from_call_id,omitempty"`
}

// UniqueKey 返回幂等调度键，用于 Asynq 去重。
// 格式：outbound:call:{task_id}:{contact_id}:{attempt_no}。
func (p OutboundCallPayload) UniqueKey() string {
	return fmt.Sprintf("outbound:call:%d:%d:%d", p.TaskID, p.ContactID, p.AttemptNo)
}

// NewOutboundCallTask 创建新的外呼 Asynq 任务。
func NewOutboundCallTask(p OutboundCallPayload, opts ...asynq.Option) (*asynq.Task, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal outbound call payload: %w", err)
	}
	return asynq.NewTask(TaskTypeOutboundCall, data, opts...), nil
}

// ParseOutboundCallPayload 从 Asynq 任务中解析外呼载荷。
func ParseOutboundCallPayload(t *asynq.Task) (OutboundCallPayload, error) {
	var p OutboundCallPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return p, fmt.Errorf("unmarshal outbound call payload: %w", err)
	}
	return p, nil
}

// RedisOpt 根据配置构建 Asynq Redis 连接选项。
func RedisOpt(cfg config.Redis) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
}
