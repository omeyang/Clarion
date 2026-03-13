package call

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
)

// unexpectedCauses 是判定为意外中断的挂断原因集合。
// 这些原因表示通话并非用户主动挂断，而是网络或系统故障导致。
var unexpectedCauses = map[string]bool{
	"audio_closed":             true, // WebSocket 异常关闭，无 CHANNEL_HANGUP。
	"DESTINATION_OUT_OF_ORDER": true, // 目标端点故障。
	"MEDIA_TIMEOUT":            true, // RTP 媒体流超时。
	"RECOVERY_ON_TIMER_EXPIRE": true, // 信令恢复超时。
	"LOSE_RACE":                true, // 竞态条件导致断线。
}

// isUnexpectedDisconnect 判断挂断原因是否为意外中断。
// 只有通话已接听（answered=true）时，异常挂断原因才视为意外中断。
func (s *Session) isUnexpectedDisconnect(cause string) bool {
	if !s.answered {
		return false
	}
	return unexpectedCauses[cause]
}

// SessionSnapshot 持有通话意外中断时的最小上下文。
// 用于中断后快速恢复：重拨时从 Redis 加载快照，恢复对话状态。
type SessionSnapshot struct {
	CallID          int64             `json:"call_id"`
	ContactID       int64             `json:"contact_id"`
	TaskID          int64             `json:"task_id"`
	Phone           string            `json:"phone"`
	Gateway         string            `json:"gateway"`
	CallerID        string            `json:"caller_id"`
	DialogueState   string            `json:"dialogue_state"`
	Turns           []dialogue.Turn   `json:"turns"`
	CollectedFields map[string]string `json:"collected_fields"`
	InterruptCause  string            `json:"interrupt_cause"`
	CreatedAt       time.Time         `json:"created_at"`
}

// maxSnapshotTurns 快照中保留的最近轮次数上限。
const maxSnapshotTurns = 6

// SnapshotStore 持久化和加载会话快照。
type SnapshotStore interface {
	// Save 保存会话快照，ttl 为过期时间。
	Save(ctx context.Context, snap *SessionSnapshot, ttl time.Duration) error
	// Load 按 callID 加载会话快照。未找到时返回 nil, nil。
	Load(ctx context.Context, callID int64) (*SessionSnapshot, error)
	// Delete 删除会话快照。
	Delete(ctx context.Context, callID int64) error
}

// RedisSnapshotStore 基于 Redis 的快照存储。
type RedisSnapshotStore struct {
	client *redis.Client
	prefix string
}

// NewRedisSnapshotStore 创建 Redis 快照存储。
// prefix 示例："clarion:session"，实际键为 "clarion:session:snapshot:{callID}"。
func NewRedisSnapshotStore(client *redis.Client, prefix string) *RedisSnapshotStore {
	return &RedisSnapshotStore{client: client, prefix: prefix}
}

func (r *RedisSnapshotStore) snapshotKey(callID int64) string {
	return fmt.Sprintf("%s:snapshot:%d", r.prefix, callID)
}

// Save 将快照序列化为 JSON 存入 Redis，设置 TTL。
func (r *RedisSnapshotStore) Save(ctx context.Context, snap *SessionSnapshot, ttl time.Duration) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("序列化会话快照: %w", err)
	}
	key := r.snapshotKey(snap.CallID)
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("保存会话快照到 Redis: %w", err)
	}
	return nil
}

// Load 从 Redis 加载快照。键不存在时返回 nil, nil。
func (r *RedisSnapshotStore) Load(ctx context.Context, callID int64) (*SessionSnapshot, error) {
	key := r.snapshotKey(callID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("加载会话快照: %w", err)
	}
	var snap SessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("反序列化会话快照: %w", err)
	}
	return &snap, nil
}

// Delete 从 Redis 删除快照。
func (r *RedisSnapshotStore) Delete(ctx context.Context, callID int64) error {
	key := r.snapshotKey(callID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("删除会话快照: %w", err)
	}
	return nil
}

// buildSnapshot 从当前会话状态构建快照。
func (s *Session) buildSnapshot(cause string) *SessionSnapshot {
	snap := &SessionSnapshot{
		CallID:         s.cfg.CallID,
		ContactID:      s.cfg.ContactID,
		TaskID:         s.cfg.TaskID,
		Phone:          s.cfg.Phone,
		Gateway:        s.cfg.Gateway,
		CallerID:       s.cfg.CallerID,
		InterruptCause: cause,
		CreatedAt:      time.Now(),
	}

	if s.cfg.DialogueEngine != nil {
		snap.DialogueState = s.cfg.DialogueEngine.State().String()
		snap.CollectedFields = s.collectFields()
		snap.Turns = s.recentSnapshotTurns()
	}

	return snap
}

// collectFields 从对话引擎安全地获取已收集字段的副本。
func (s *Session) collectFields() map[string]string {
	result := s.cfg.DialogueEngine.Result(engine.CallInterrupted)
	if result.CollectedFields == nil {
		return make(map[string]string)
	}
	// 复制一份，避免共享底层 map。
	fields := make(map[string]string, len(result.CollectedFields))
	maps.Copy(fields, result.CollectedFields)
	return fields
}

// recentSnapshotTurns 返回最近几轮对话（用于快照）。
func (s *Session) recentSnapshotTurns() []dialogue.Turn {
	turns := s.cfg.DialogueEngine.Turns()
	if len(turns) <= maxSnapshotTurns {
		dst := make([]dialogue.Turn, len(turns))
		copy(dst, turns)
		return dst
	}
	dst := make([]dialogue.Turn, maxSnapshotTurns)
	copy(dst, turns[len(turns)-maxSnapshotTurns:])
	return dst
}
