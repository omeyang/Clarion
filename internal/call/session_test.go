package call

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/observe"
)

func defaultProtection() config.CallProtection {
	return config.CallProtection{
		MaxDurationSec:         10,
		MaxSilenceSec:          3,
		RingTimeoutSec:         5,
		FirstSilenceTimeoutSec: 2,
		MaxASRRetries:          2,
		MaxConsecutiveErrors:   3,
		MaxTurns:               20,
	}
}

func defaultAMDCfg() config.AMD {
	return config.AMD{
		Enabled:                     false, // Disable AMD to simplify tests.
		DetectionWindowMs:           3000,
		ContinuousSpeechThresholdMs: 4000,
		HumanPauseThresholdMs:       300,
		EnergyThresholdDBFS:         -35.0,
	}
}

func TestSession_NewSession(t *testing.T) {
	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	session := NewSession(SessionConfig{
		CallID:     1,
		SessionID:  "test-session-1",
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		Protection: defaultProtection(),
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
	})

	require.NotNil(t, session)
}

func TestSession_RunWithClosedAudioIn(t *testing.T) {
	// When AudioIn is closed immediately, session should complete.
	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	session := NewSession(SessionConfig{
		CallID:     2,
		SessionID:  "test-session-2",
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		Protection: defaultProtection(),
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
	})

	// Close audio immediately to trigger quick exit from event loop.
	close(audioIn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := session.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(2), result.CallID)
	assert.Equal(t, "test-session-2", result.SessionID)
}

func TestSession_RunWithTimeout(t *testing.T) {
	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	cfg := defaultProtection()
	cfg.MaxDurationSec = 1 // 1 second max duration.

	session := NewSession(SessionConfig{
		CallID:     3,
		SessionID:  "test-session-3",
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		Protection: cfg,
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
	})

	ctx := context.Background()

	// Run in goroutine.
	done := make(chan struct{})
	var result *SessionResult
	var runErr error

	go func() {
		result, runErr = session.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Session ended due to max duration timeout.
		assert.Error(t, runErr) // context deadline exceeded
		require.NotNil(t, result)
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete within expected time")
	}
}

func TestSession_EventRecording(t *testing.T) {
	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	session := NewSession(SessionConfig{
		CallID:     4,
		SessionID:  "test-session-4",
		Phone:      "13800138000",
		Gateway:    "pstn",
		CallerID:   "10001",
		Protection: defaultProtection(),
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
	})

	close(audioIn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _ := session.Run(ctx)
	require.NotNil(t, result)

	// Should have at least the initial event (bot_speak_start from dial).
	assert.Greater(t, len(result.Events), 0)
}

func TestSessionResult_JSON(t *testing.T) {
	result := &SessionResult{
		CallID:     100,
		SessionID:  "test-json",
		Status:     engine.CallCompleted,
		AnswerType: engine.AnswerHuman,
		Duration:   45,
		Grade:      engine.GradeB,
		Fields:     map[string]string{"name": "test"},
	}

	data, err := SessionResultJSON(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"call_id":100`)
	assert.Contains(t, string(data), `"status":"completed"`)
	assert.Contains(t, string(data), `"answer_type":"human"`)
}

func TestSessionResult_JSON_包含网络质量(t *testing.T) {
	t.Parallel()
	result := &SessionResult{
		CallID:    200,
		SessionID: "test-nq-json",
		Status:    engine.CallCompleted,
		NetQuality: &NetworkQualitySnapshot{
			JitterAvgMs:   5.5,
			LossRate:      0.02,
			GapCount:      3,
			LowVolumeRate: 0.1,
			FrameCount:    500,
		},
	}

	data, err := SessionResultJSON(result)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"net_quality"`)
	assert.Contains(t, string(data), `"jitter_avg_ms"`)
}

func TestSessionResult_JSON_无网络质量时省略(t *testing.T) {
	t.Parallel()
	result := &SessionResult{
		CallID:    201,
		SessionID: "test-no-nq",
		Status:    engine.CallCompleted,
	}

	data, err := SessionResultJSON(result)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"net_quality"`)
}

func TestSession_BuildResult_包含网络质量快照(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaWaitingUser)
	s.startTime = time.Now()

	// 模拟一些音频帧到达。
	frame := makeLoudFrame()
	now := time.Now()
	for i := range 10 {
		s.netQuality.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
	}

	result := s.buildResult(engine.CallCompleted)
	require.NotNil(t, result.NetQuality, "结果应包含网络质量快照")
	assert.Equal(t, 10, result.NetQuality.FrameCount)
}

func TestSession_BuildResult_无Metrics不panic(t *testing.T) {
	t.Parallel()
	s := newTestSession(engine.MediaWaitingUser)
	s.startTime = time.Now()
	s.metrics = nil

	result := s.buildResult(engine.CallCompleted)
	require.NotNil(t, result.NetQuality)
}

func TestSession_BuildResult_有Metrics上报抖动(t *testing.T) {
	t.Parallel()

	m, err := observe.NewCallMetrics()
	require.NoError(t, err)

	s := newTestSession(engine.MediaWaitingUser)
	s.startTime = time.Now()
	s.metrics = m

	// 注入一些有抖动的帧数据。
	frame := makeLoudFrame()
	now := time.Now()
	for i := range 10 {
		s.netQuality.RecordFrame(frame, now.Add(time.Duration(i)*20*time.Millisecond))
	}

	result := s.buildResult(engine.CallCompleted)
	require.NotNil(t, result.NetQuality)
	// OTel 度量在测试中使用 noop exporter，确保不 panic 即可。
}

func TestSession_RecordNetworkQuality_上报OTel度量(t *testing.T) {
	t.Parallel()

	m, err := observe.NewCallMetrics()
	require.NoError(t, err)

	s := newTestSession(engine.MediaWaitingUser)
	s.startTime = time.Now()
	s.metrics = m

	frame := makeLoudFrame()
	// recordNetworkQuality 应正常工作，不 panic。
	s.recordNetworkQuality(frame)
}

func TestSession_RecordNetworkQuality_无Metrics不panic(t *testing.T) {
	t.Parallel()

	s := newTestSession(engine.MediaWaitingUser)
	s.startTime = time.Now()
	s.metrics = nil

	frame := makeLoudFrame()
	s.recordNetworkQuality(frame)
}

func TestSession_NewSession_初始化网络质量监控(t *testing.T) {
	t.Parallel()

	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	session := NewSession(SessionConfig{
		CallID:     10,
		SessionID:  "test-nq-init",
		Phone:      "13800138000",
		Protection: defaultProtection(),
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
	})

	require.NotNil(t, session.netQuality, "NewSession 应初始化 netQuality")
	assert.Nil(t, session.metrics, "未注入 Metrics 时应为 nil")
}

func TestSession_NewSession_注入Metrics(t *testing.T) {
	t.Parallel()

	m, err := observe.NewCallMetrics()
	require.NoError(t, err)

	audioIn := make(chan []byte, 10)
	audioOut := make(chan []byte, 10)

	session := NewSession(SessionConfig{
		CallID:     11,
		SessionID:  "test-metrics-inject",
		Phone:      "13800138000",
		Protection: defaultProtection(),
		AMDConfig:  defaultAMDCfg(),
		Logger:     testLogger(),
		AudioIn:    audioIn,
		AudioOut:   audioOut,
		Metrics:    m,
	})

	require.NotNil(t, session.metrics, "注入 Metrics 后应可使用")
}
