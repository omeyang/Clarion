package call

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/engine/dialogue"
	"github.com/omeyang/clarion/internal/engine/media"
	"github.com/omeyang/clarion/internal/engine/rules"
)

// --- isUnexpectedDisconnect 测试 ---

func TestSession_IsUnexpectedDisconnect(t *testing.T) {
	tests := []struct {
		name     string
		cause    string
		answered bool
		want     bool
	}{
		{"已接听+audio_closed", "audio_closed", true, true},
		{"已接听+MEDIA_TIMEOUT", "MEDIA_TIMEOUT", true, true},
		{"已接听+DESTINATION_OUT_OF_ORDER", "DESTINATION_OUT_OF_ORDER", true, true},
		{"已接听+RECOVERY_ON_TIMER_EXPIRE", "RECOVERY_ON_TIMER_EXPIRE", true, true},
		{"已接听+LOSE_RACE", "LOSE_RACE", true, true},
		{"已接听+NORMAL_CLEARING（正常挂断）", "NORMAL_CLEARING", true, false},
		{"已接听+NO_ANSWER", "NO_ANSWER", true, false},
		{"未接听+audio_closed", "audio_closed", false, false},
		{"未接听+MEDIA_TIMEOUT", "MEDIA_TIMEOUT", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestSession(engine.MediaBotSpeaking)
			s.answered = tt.answered
			got := s.isUnexpectedDisconnect(tt.cause)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- handleHangup 意外中断检测测试 ---

func TestSession_HandleHangup_UnexpectedDisconnect(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.status = engine.CallInProgress
	s.cfg.SnapshotStore = store
	s.cfg.SnapshotTTL = 5 * time.Minute

	s.handleHangup("audio_closed")

	assert.Equal(t, engine.CallInterrupted, s.status)

	// 应记录意外中断事件。
	s.mu.Lock()
	foundDisconnect := false
	for _, e := range s.events {
		if e.EventType == engine.EventUnexpectedDisconnect {
			foundDisconnect = true
			assert.Equal(t, "audio_closed", e.Metadata["cause"])
		}
	}
	s.mu.Unlock()
	assert.True(t, foundDisconnect, "应记录 unexpected_disconnect 事件")

	// 应保存快照。
	snap := store.get(s.cfg.CallID)
	require.NotNil(t, snap, "应保存会话快照")
	assert.Equal(t, s.cfg.CallID, snap.CallID)
	assert.Equal(t, "audio_closed", snap.InterruptCause)
}

func TestSession_HandleHangup_NormalNoSnapshot(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.status = engine.CallInProgress
	s.cfg.SnapshotStore = store

	s.handleHangup("NORMAL_CLEARING")

	assert.Equal(t, engine.CallCompleted, s.status)

	// 正常挂断不应保存快照。
	snap := store.get(s.cfg.CallID)
	assert.Nil(t, snap, "正常挂断不应保存快照")
}

func TestSession_HandleHangup_NoSnapshotStore(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.status = engine.CallInProgress
	// SnapshotStore 为 nil，不应 panic。

	s.handleHangup("audio_closed")

	assert.Equal(t, engine.CallInterrupted, s.status)
}

// --- SessionSnapshot 序列化测试 ---

func TestSessionSnapshot_JSON(t *testing.T) {
	snap := &SessionSnapshot{
		CallID:        123,
		ContactID:     456,
		TaskID:        789,
		Phone:         "13800138000",
		Gateway:       "pstn",
		CallerID:      "10001",
		DialogueState: "OPENING",
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
			{Number: 2, Speaker: "user", Content: "你好"},
		},
		CollectedFields: map[string]string{"name": "张三"},
		InterruptCause:  "audio_closed",
		CreatedAt:       time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var got SessionSnapshot
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, snap.CallID, got.CallID)
	assert.Equal(t, snap.Phone, got.Phone)
	assert.Equal(t, snap.DialogueState, got.DialogueState)
	assert.Equal(t, snap.InterruptCause, got.InterruptCause)
	assert.Len(t, got.Turns, 2)
	assert.Equal(t, "张三", got.CollectedFields["name"])
}

// --- buildSnapshot 测试 ---

func TestSession_BuildSnapshot(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	s.cfg.CallID = 100
	s.cfg.ContactID = 200
	s.cfg.TaskID = 300
	s.cfg.Phone = "13800138000"
	s.cfg.Gateway = "pstn"
	s.cfg.CallerID = "10001"
	s.answered = true

	snap := s.buildSnapshot("MEDIA_TIMEOUT")

	assert.Equal(t, int64(100), snap.CallID)
	assert.Equal(t, int64(200), snap.ContactID)
	assert.Equal(t, int64(300), snap.TaskID)
	assert.Equal(t, "13800138000", snap.Phone)
	assert.Equal(t, "MEDIA_TIMEOUT", snap.InterruptCause)
	assert.False(t, snap.CreatedAt.IsZero())
}

// --- recentSnapshotTurns 测试 ---

func TestSession_RecentSnapshotTurns_UnderLimit(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	// 无对话引擎时 buildSnapshot 的 Turns 为 nil。
	snap := s.buildSnapshot("audio_closed")
	assert.Nil(t, snap.Turns)
}

// --- saveSnapshot 测试 ---

func TestSession_SaveSnapshot_DefaultTTL(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.cfg.SnapshotStore = store
	// 不设置 SnapshotTTL，应使用默认值。

	s.saveSnapshot("audio_closed")

	snap := store.get(s.cfg.CallID)
	require.NotNil(t, snap)
	assert.Equal(t, defaultSnapshotTTL, store.lastTTL)
}

func TestSession_SaveSnapshot_CustomTTL(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.cfg.SnapshotStore = store
	s.cfg.SnapshotTTL = 3 * time.Minute

	s.saveSnapshot("audio_closed")

	assert.Equal(t, 3*time.Minute, store.lastTTL)
}

// --- memSnapshotStore 内存实现（测试用）---

// memSnapshotStore 是 SnapshotStore 的内存实现，用于单元测试。
type memSnapshotStore struct {
	mu      sync.Mutex
	store   map[int64]*SessionSnapshot
	lastTTL time.Duration
}

func (m *memSnapshotStore) Save(_ context.Context, snap *SessionSnapshot, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		m.store = make(map[int64]*SessionSnapshot)
	}
	m.store[snap.CallID] = snap
	m.lastTTL = ttl
	return nil
}

func (m *memSnapshotStore) Load(_ context.Context, callID int64) (*SessionSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return nil, nil
	}
	return m.store[callID], nil
}

func (m *memSnapshotStore) Delete(_ context.Context, callID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		delete(m.store, callID)
	}
	return nil
}

func (m *memSnapshotStore) get(callID int64) *SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return nil
	}
	return m.store[callID]
}

// --- handleHangup 与 SessionResult 集成测试 ---

func TestSession_HandleHangup_InterruptedResult(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.status = engine.CallInProgress
	s.cfg.SnapshotStore = store

	s.handleHangup("MEDIA_TIMEOUT")

	result := s.buildResult(s.status)
	assert.Equal(t, engine.CallInterrupted, result.Status)
}

// --- 并发安全测试 ---

func TestSession_SaveSnapshot_Concurrent(t *testing.T) {
	store := &memSnapshotStore{}
	s := newTestSession(engine.MediaBotSpeaking)
	s.answered = true
	s.cfg.SnapshotStore = store

	done := make(chan struct{})
	for range 10 {
		go func() {
			s.saveSnapshot("audio_closed")
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}

	// 所有 goroutine 都应成功保存（最后一个覆盖前面的）。
	snap := store.get(s.cfg.CallID)
	require.NotNil(t, snap)
}

// --- memSnapshotStore Load/Delete 测试 ---

func TestMemSnapshotStore_LoadDelete(t *testing.T) {
	store := &memSnapshotStore{}
	ctx := context.Background()

	// 空存储加载返回 nil。
	snap, err := store.Load(ctx, 999)
	require.NoError(t, err)
	assert.Nil(t, snap)

	// 保存后可加载。
	require.NoError(t, store.Save(ctx, &SessionSnapshot{CallID: 1, Phone: "123"}, time.Minute))
	snap, err = store.Load(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, "123", snap.Phone)

	// 删除后不可加载。
	require.NoError(t, store.Delete(ctx, 1))
	snap, err = store.Load(ctx, 1)
	require.NoError(t, err)
	assert.Nil(t, snap)
}

// --- eventLoop 中 audio_closed 触发中断流程测试 ---

func TestSession_EventLoop_AudioClosedInterrupt(t *testing.T) {
	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)
	store := &memSnapshotStore{}

	s := NewSession(SessionConfig{
		CallID:        500,
		SessionID:     "test-interrupt",
		Phone:         "13800138000",
		Protection:    defaultProtection(),
		AMDConfig:     defaultAMDCfg(),
		Logger:        testLogger(),
		AudioIn:       audioIn,
		AudioOut:      audioOut,
		SnapshotStore: store,
		SnapshotTTL:   5 * time.Minute,
	})

	// 模拟已接听状态。
	s.mfsm = media.NewFSM(engine.MediaBotSpeaking)
	s.answered = true
	s.status = engine.CallInProgress
	s.startTime = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.ctx = ctx

	// 关闭 audioIn 触发 audio_closed。
	close(audioIn)

	err := s.eventLoop(ctx)
	require.NoError(t, err)

	assert.Equal(t, engine.CallInterrupted, s.status)

	// 应保存快照。
	snap := store.get(500)
	require.NotNil(t, snap, "audio_closed 应触发快照保存")
	assert.Equal(t, "audio_closed", snap.InterruptCause)
}

// --- 恢复呼叫开场白测试 ---

func TestSession_StartDialogue_RecoveryOpening(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	s.ctx = context.Background()

	// 创建对话引擎。
	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{
			"OPENING": "你好，我是AI助手。",
		},
	})
	require.NoError(t, err)
	s.cfg.DialogueEngine = eng

	// 设置恢复快照。
	s.cfg.RestoredSnapshot = &SessionSnapshot{
		CallID:        100,
		DialogueState: "QUALIFICATION",
		Turns: []dialogue.Turn{
			{Number: 1, Speaker: "bot", Content: "你好"},
			{Number: 2, Speaker: "user", Content: "你好"},
		},
		CollectedFields: map[string]string{"name": "张三"},
	}

	// startDialogue 应使用恢复开场白。
	s.startDialogue()

	// 验证对话引擎状态已恢复。
	assert.Equal(t, engine.DialogueQualification, eng.State())
	assert.Equal(t, "张三", eng.Result(engine.CallCompleted).CollectedFields["name"])
	assert.Len(t, eng.Turns(), 2)

	// 验证记录的事件中包含恢复开场白。
	s.mu.Lock()
	found := false
	for _, e := range s.events {
		if e.EventType == engine.EventBotSpeakStart {
			if strings.Contains(e.Metadata["text"], "断了") {
				found = true
			}
		}
	}
	s.mu.Unlock()
	assert.True(t, found, "应记录包含恢复开场白的事件")
}

func TestSession_StartDialogue_NormalOpening(t *testing.T) {
	s := newTestSession(engine.MediaBotSpeaking)
	s.ctx = context.Background()

	eng, err := dialogue.NewEngine(dialogue.EngineConfig{
		TemplateConfig: rules.TemplateConfig{MaxTurns: 20, MaxObjections: 3},
		PromptTemplates: map[string]string{
			"OPENING": "你好，我是AI助手。",
		},
	})
	require.NoError(t, err)
	s.cfg.DialogueEngine = eng

	// 不设置恢复快照，应使用正常开场白。
	s.startDialogue()

	s.mu.Lock()
	found := false
	for _, e := range s.events {
		if e.EventType == engine.EventBotSpeakStart {
			if e.Metadata["text"] == "你好，我是AI助手。" {
				found = true
			}
		}
	}
	s.mu.Unlock()
	assert.True(t, found, "应记录正常开场白事件")
}
