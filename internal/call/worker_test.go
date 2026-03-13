package call

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/scheduler"
)

func defaultWorkerConfig() config.Config {
	cfg := config.Defaults()
	cfg.Worker.MaxConcurrentCalls = 3
	return cfg
}

func TestNewWorker(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	require.NotNil(t, w)
	assert.Equal(t, 3, w.maxCalls)
	assert.Equal(t, 0, w.ActiveCalls())
}

func TestWorker_ActiveCalls(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	assert.Equal(t, 0, w.ActiveCalls())

	w.activeCalls.Add(1)
	assert.Equal(t, 1, w.ActiveCalls())

	w.activeCalls.Add(2)
	assert.Equal(t, 3, w.ActiveCalls())

	w.activeCalls.Add(-3)
	assert.Equal(t, 0, w.ActiveCalls())
}

func TestTask_JSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Task
	}{
		{
			name:  "full task",
			input: `{"call_id":1,"contact_id":2,"task_id":3,"phone":"13800138000","gateway":"pstn","caller_id":"10001","template_id":5}`,
			want: Task{
				CallID:     1,
				ContactID:  2,
				TaskID:     3,
				Phone:      "13800138000",
				Gateway:    "pstn",
				CallerID:   "10001",
				TemplateID: 5,
			},
		},
		{
			name:  "minimal task",
			input: `{"call_id":100,"phone":"10086"}`,
			want: Task{
				CallID: 100,
				Phone:  "10086",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var task Task
			err := json.Unmarshal([]byte(tt.input), &task)
			require.NoError(t, err)
			assert.Equal(t, tt.want, task)
		})
	}
}

func TestTask_JSONRoundTrip(t *testing.T) {
	task := Task{
		CallID:     42,
		ContactID:  100,
		TaskID:     7,
		Phone:      "13900139000",
		Gateway:    "sip-trunk",
		CallerID:   "88888888",
		TemplateID: 3,
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var decoded Task
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, task, decoded)
}

func TestWorker_HandleOutboundCall_InvalidPayload(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	task := asynq.NewTask(scheduler.TaskTypeOutboundCall, []byte("invalid json"))
	err := w.HandleOutboundCall(context.Background(), task)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse outbound call payload")
}

// --- 恢复呼叫测试 ---

func TestWorker_AttachRecoverySnapshot_NoStore(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	sessionCfg := SessionConfig{}
	// snapshotStore 为 nil 时不应 panic。
	w.attachRecoverySnapshot(context.Background(), &sessionCfg, 123)

	assert.Nil(t, sessionCfg.RestoredSnapshot)
}

func TestWorker_AttachRecoverySnapshot_ZeroCallID(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())
	w.snapshotStore = &memSnapshotStore{}

	sessionCfg := SessionConfig{}
	// RecoveryFromCallID 为 0 时不做任何操作。
	w.attachRecoverySnapshot(context.Background(), &sessionCfg, 0)

	assert.Nil(t, sessionCfg.RestoredSnapshot)
}

func TestWorker_AttachRecoverySnapshot_Found(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())
	store := &memSnapshotStore{}
	w.snapshotStore = store

	ctx := context.Background()
	snap := &SessionSnapshot{
		CallID:        100,
		Phone:         "13800138000",
		DialogueState: "QUALIFICATION",
	}
	require.NoError(t, store.Save(ctx, snap, time.Minute))

	sessionCfg := SessionConfig{}
	w.attachRecoverySnapshot(ctx, &sessionCfg, 100)

	require.NotNil(t, sessionCfg.RestoredSnapshot)
	assert.Equal(t, int64(100), sessionCfg.RestoredSnapshot.CallID)
	assert.Equal(t, "QUALIFICATION", sessionCfg.RestoredSnapshot.DialogueState)

	// 快照应被删除。
	loaded := store.get(100)
	assert.Nil(t, loaded, "使用后的快照应被删除")
}

func TestWorker_AttachRecoverySnapshot_Expired(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())
	store := &memSnapshotStore{}
	w.snapshotStore = store

	// 不保存快照，模拟已过期。
	sessionCfg := SessionConfig{}
	w.attachRecoverySnapshot(context.Background(), &sessionCfg, 999)

	assert.Nil(t, sessionCfg.RestoredSnapshot, "过期快照应返回 nil")
}

func TestWorker_ScheduleRecoveryIfNeeded_NotInterrupted(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	result := &SessionResult{Status: engine.CallCompleted}
	task := Task{CallID: 1}

	// 正常完成不应触发恢复调度（不 panic）。
	w.scheduleRecoveryIfNeeded(context.Background(), result, task)
}

func TestWorker_ScheduleRecoveryIfNeeded_NoClient(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	result := &SessionResult{Status: engine.CallInterrupted}
	task := Task{CallID: 1}

	// 无调度客户端时不应 panic。
	w.scheduleRecoveryIfNeeded(context.Background(), result, task)
}

func TestWorker_ScheduleRecoveryIfNeeded_AlreadyRecovery(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	result := &SessionResult{Status: engine.CallInterrupted}
	task := Task{CallID: 1, RecoveryFromCallID: 99}

	// 已经是恢复呼叫的不应再次入队。
	w.scheduleRecoveryIfNeeded(context.Background(), result, task)
}

func TestWorker_CloseSchedulerClient_Nil(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	// schedulerClient 为 nil 时不应 panic。
	w.closeSchedulerClient()
}

func TestTask_RecoveryFromCallID_JSON(t *testing.T) {
	task := Task{
		CallID:             42,
		Phone:              "13900139000",
		RecoveryFromCallID: 100,
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var decoded Task
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, int64(100), decoded.RecoveryFromCallID)
}

func TestTask_RecoveryFromCallID_OmittedWhenZero(t *testing.T) {
	task := Task{CallID: 42, Phone: "13900139000"}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "recovery_from_call_id")
}
