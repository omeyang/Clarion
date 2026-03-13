package call

import (
	"context"
	"time"
)

// RealtimeVoice 是实时音频对话接口。
// 单个 WebSocket 连接同时承载 Audio-in 和 Audio-out，
// 替代传统 ASR→LLM→TTS 三段串行管线。
//
// 实现者需保证并发安全：FeedAudio 可能与 AudioOut/Transcripts 消费同时发生。
type RealtimeVoice interface {
	// Connect 建立实时语音会话。
	Connect(ctx context.Context, cfg RealtimeVoiceConfig) error

	// FeedAudio 向模型发送用户音频帧（PCM 16kHz 16bit mono）。
	FeedAudio(ctx context.Context, frame []byte) error

	// AudioOut 返回模型生成的音频帧通道（PCM，采样率由配置决定）。
	AudioOut() <-chan []byte

	// Transcripts 返回模型生成的文本转录通道（用于业务决策和通话记录）。
	Transcripts() <-chan TranscriptEvent

	// UpdateInstructions 动态更新模型的系统指令（注入 Smart LLM 决策）。
	UpdateInstructions(ctx context.Context, instructions string) error

	// Interrupt 中断当前模型回复（barge-in 场景）。
	Interrupt(ctx context.Context) error

	// Close 关闭实时会话并释放资源。
	Close() error
}

// RealtimeVoiceConfig 持有实时语音会话的配置。
type RealtimeVoiceConfig struct {
	Model             string
	Voice             string
	Instructions      string
	InputSampleRate   int // 输入音频采样率，默认 16000。
	OutputSampleRate  int // 输出音频采样率，默认 24000。
	VADEnabled        bool
	VADThreshold      float64
	SilenceDurationMs int
}

// TranscriptEvent 是来自实时模型的文本事件。
type TranscriptEvent struct {
	Role      string    // "user" 或 "assistant"。
	Text      string    // 转录文本。
	IsFinal   bool      // 是否为最终结果。
	Timestamp time.Time // 事件时间戳。
}
