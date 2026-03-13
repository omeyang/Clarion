package call

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/engine"
)

// mockSpeechDetector 是用于测试的 SpeechDetector 模拟实现。
type mockSpeechDetector struct {
	result        bool
	err           error
	calls         int
	lastFrameSize int // 记录最近一次收到的帧字节数
}

func (m *mockSpeechDetector) IsSpeech(frame []byte) (bool, error) {
	m.calls++
	m.lastFrameSize = len(frame)
	return m.result, m.err
}

func TestWorker_SetSpeechDetector(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	require.Nil(t, w.speechDetector)

	detector := &mockSpeechDetector{result: true}
	w.SetSpeechDetector(detector)

	assert.Equal(t, detector, w.speechDetector)
}

func TestWorker_SetSpeechDetector_Nil(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	// 传入 nil 不应 panic。
	w.SetSpeechDetector(nil)
	assert.Nil(t, w.speechDetector)
}

func TestWorker_BuildSessionConfig_InjectsSpeechDetector(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	detector := &mockSpeechDetector{result: true}
	w.SetSpeechDetector(detector)

	audioIn := make(chan []byte, 1)
	audioOut := make(chan []byte, 1)
	task := Task{
		CallID:   1,
		Phone:    "13800138000",
		Gateway:  "pstn",
		CallerID: "10001",
	}

	sessionCfg := w.buildSessionConfig(task, "test-session", audioIn, audioOut)

	assert.Equal(t, detector, sessionCfg.SpeechDetector,
		"buildSessionConfig 应将 Worker 的 speechDetector 注入到 SessionConfig")
}

func TestWorker_BuildSessionConfig_NilSpeechDetector(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	audioIn := make(chan []byte, 1)
	audioOut := make(chan []byte, 1)
	task := Task{CallID: 1, Phone: "10086"}

	sessionCfg := w.buildSessionConfig(task, "test-session", audioIn, audioOut)

	assert.Nil(t, sessionCfg.SpeechDetector,
		"未设置 speechDetector 时 SessionConfig.SpeechDetector 应为 nil")
}

func TestSession_IsSpeechFrame_WithDetector(t *testing.T) {
	s := newTestSession(engine.MediaWaitingUser)

	detector := &mockSpeechDetector{result: true}
	s.speechDetector = detector

	frame := makeSilentFrame()
	got := s.isSpeechFrame(frame)

	assert.True(t, got, "SpeechDetector 返回 true 时应判定为语音")
	assert.Equal(t, 1, detector.calls, "应调用 SpeechDetector.IsSpeech")
}

func TestSession_IsSpeechFrame_DetectorReturnsFalse(t *testing.T) {
	s := newTestSession(engine.MediaWaitingUser)

	detector := &mockSpeechDetector{result: false}
	s.speechDetector = detector

	frame := makeLoudFrame()
	got := s.isSpeechFrame(frame)

	assert.False(t, got, "SpeechDetector 返回 false 时应判定为非语音")
	assert.Equal(t, 1, detector.calls)
}

func TestSession_IsSpeechFrame_DetectorError_FallbackToEnergy(t *testing.T) {
	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.AMDConfig.EnergyThresholdDBFS = -35.0

	detector := &mockSpeechDetector{
		result: false,
		err:    assert.AnError,
	}
	s.speechDetector = detector

	// 高能量帧 + detector 出错 → 退回能量检测 → 判定为语音。
	frame := makeLoudFrame()
	got := s.isSpeechFrame(frame)

	assert.True(t, got, "SpeechDetector 出错时应退回能量阈值检测")
	assert.Equal(t, 1, detector.calls)
}

func TestSession_IsSpeechFrame_NoDetector_FallbackToEnergy(t *testing.T) {
	s := newTestSession(engine.MediaWaitingUser)
	s.cfg.AMDConfig.EnergyThresholdDBFS = -35.0
	s.speechDetector = nil

	// 无 detector 时退回能量检测。
	loudFrame := makeLoudFrame()
	assert.True(t, s.isSpeechFrame(loudFrame), "高能量帧应判定为语音")

	silentFrame := makeSilentFrame()
	assert.False(t, s.isSpeechFrame(silentFrame), "静默帧应判定为非语音")
}

func TestSession_IsSpeechFrame_ResamplesTo16kHz(t *testing.T) {
	s := newTestSession(engine.MediaWaitingUser)

	detector := &mockSpeechDetector{result: true}
	s.speechDetector = detector

	// 8kHz 20ms 帧：160 采样 × 2 字节 = 320 字节。
	frame8k := makeSilentFrame()
	require.Equal(t, testFrameSamples*2, len(frame8k))

	s.isSpeechFrame(frame8k)

	// 重采样后应变为 16kHz：320 采样 × 2 字节 = 640 字节。
	assert.Equal(t, testFrameSamples*2*2, detector.lastFrameSize,
		"isSpeechFrame 应将 8kHz 帧重采样为 16kHz 后传入 SpeechDetector")
}
