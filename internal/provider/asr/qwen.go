// Package asr 实现 ASR（自动语音识别）服务提供者。
package asr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
	"github.com/omeyang/clarion/internal/provider"
)

// 编译时接口检查。
var (
	_ provider.ASRProvider = (*Qwen)(nil)
	_ provider.ASRStream   = (*qwenStream)(nil)
)

const defaultDashScopeASRURL = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"

// Qwen 通过 WebSocket 使用 DashScope Qwen3-ASR 实现 provider.ASRProvider。
type Qwen struct {
	apiKey string
	wsURL  string
	logger *slog.Logger
}

// QwenOption 配置 Qwen ASR 提供者。
type QwenOption func(*Qwen)

// WithQwenLogger 设置自定义日志记录器。
func WithQwenLogger(l *slog.Logger) QwenOption {
	return func(q *Qwen) { q.logger = l }
}

// WithQwenWSURL 覆盖 WebSocket 端点（用于测试）。
func WithQwenWSURL(url string) QwenOption {
	return func(q *Qwen) { q.wsURL = url }
}

// NewQwen 创建新的 Qwen ASR 提供者。
func NewQwen(apiKey string, opts ...QwenOption) *Qwen {
	q := &Qwen{
		apiKey: apiKey,
		wsURL:  defaultDashScopeASRURL,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// ── OpenAI Realtime 兼容协议消息结构 ─────────────────────────

// wsMessage 是 DashScope Realtime API 兼容的 WebSocket JSON 消息。
type wsMessage struct {
	EventID string `json:"event_id,omitempty"`
	Type    string `json:"type"`

	// 用于 session.update。
	Session *sessionConfig `json:"session,omitempty"`

	// 用于 input_audio_buffer.append。
	Audio string `json:"audio,omitempty"`

	// 用于转录结果（服务端响应）。
	Text       string  `json:"text,omitempty"`
	Transcript string  `json:"transcript,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type sessionConfig struct {
	Modalities              []string          `json:"modalities"`
	InputAudioFormat        string            `json:"input_audio_format"`
	SampleRate              int               `json:"sample_rate"`
	InputAudioTranscription *transcriptionCfg `json:"input_audio_transcription,omitempty"`
	TurnDetection           *turnDetectionCfg `json:"turn_detection,omitempty"`
}

type transcriptionCfg struct {
	Language string `json:"language"`
}

type turnDetectionCfg struct {
	Type              string  `json:"type"`
	Threshold         float64 `json:"threshold"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
}

// dialURL 根据配置构建 WebSocket 连接地址。
func (q *Qwen) dialURL(model string) string {
	if q.wsURL == defaultDashScopeASRURL {
		return q.wsURL + "?model=" + model
	}
	return q.wsURL
}

// StartStream 通过 WebSocket 打开新的识别流。
func (q *Qwen) StartStream(ctx context.Context, cfg provider.ASRConfig) (provider.ASRStream, error) {
	model := cfg.Model
	if model == "" {
		model = "qwen3-asr-flash-realtime"
	}

	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + q.apiKey}
	headers["OpenAI-Beta"] = []string{"realtime=v1"}

	conn, resp, err := websocket.Dial(ctx, q.dialURL(model), &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				q.logger.Warn("qwen asr: close dial response body", "error", cerr)
			}
		}()
	}
	if err != nil {
		return nil, fmt.Errorf("qwen asr: dial websocket: %w", err)
	}

	// 发送 session.update 配置 ASR 模式。
	lang := cfg.Language
	if lang == "" {
		lang = "zh"
	}
	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 16000
	}

	update := wsMessage{
		Type: "session.update",
		Session: &sessionConfig{
			Modalities:       []string{"text"},
			InputAudioFormat: "pcm",
			SampleRate:       sampleRate,
			InputAudioTranscription: &transcriptionCfg{
				Language: lang,
			},
			TurnDetection: &turnDetectionCfg{
				Type:              "server_vad",
				Threshold:         0.0,
				SilenceDurationMs: 400,
			},
		},
	}

	data, err := json.Marshal(update)
	if err != nil {
		if closeErr := conn.Close(websocket.StatusInternalError, "marshal error"); closeErr != nil {
			q.logger.Warn("qwen asr: close after marshal error", "error", closeErr)
		}
		return nil, fmt.Errorf("qwen asr: marshal session update: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		if closeErr := conn.Close(websocket.StatusInternalError, "write error"); closeErr != nil {
			q.logger.Warn("qwen asr: close after write error", "error", closeErr)
		}
		return nil, fmt.Errorf("qwen asr: send session update: %w", err)
	}

	rdCtx, rdCancel := context.WithCancel(context.Background())

	stream := &qwenStream{
		conn:     conn,
		events:   make(chan provider.ASREvent, 64),
		done:     make(chan struct{}),
		cancelRd: rdCancel,
		logger:   q.logger,
	}

	go stream.readLoop(rdCtx)

	return stream, nil
}

// qwenStream 实现 provider.ASRStream。
type qwenStream struct {
	conn     *websocket.Conn
	events   chan provider.ASREvent
	done     chan struct{}
	cancelRd context.CancelFunc
	logger   *slog.Logger

	closeOnce sync.Once
}

// FeedAudio 向识别流发送音频片段。
func (s *qwenStream) FeedAudio(ctx context.Context, chunk []byte) error {
	select {
	case <-s.done:
		return errors.New("qwen asr: stream closed")
	default:
	}

	msg := wsMessage{
		Type:  "input_audio_buffer.append",
		Audio: base64.StdEncoding.EncodeToString(chunk),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("qwen asr: marshal audio: %w", err)
	}
	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("qwen asr: send audio: %w", err)
	}
	return nil
}

// Events 返回接收 ASR 事件的通道。
func (s *qwenStream) Events() <-chan provider.ASREvent {
	return s.events
}

// Close 终止识别流。
func (s *qwenStream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		s.cancelRd()
		if closeErr := s.conn.Close(websocket.StatusNormalClosure, "close"); closeErr != nil {
			err = fmt.Errorf("close ASR stream: %w", closeErr)
		}
	})
	return err
}

// readLoop 从 WebSocket 读取消息并发出 ASR 事件。
func (s *qwenStream) readLoop(ctx context.Context) {
	defer close(s.events)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		_, data, err := s.conn.Read(ctx)
		if err != nil {
			select {
			case <-s.done:
				// 预期的关闭。
			default:
				s.logger.Warn("qwen asr: read error", "error", err)
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			s.logger.Warn("qwen asr: unmarshal message", "error", err)
			continue
		}

		switch msg.Type {
		case "session.created":
			s.logger.Debug("qwen asr: session created")
		case "conversation.item.input_audio_transcription.text":
			if msg.Text != "" {
				s.emit(provider.ASREvent{
					Text:    msg.Text,
					IsFinal: false,
				})
			}
		case "conversation.item.input_audio_transcription.completed":
			if msg.Transcript != "" {
				s.emit(provider.ASREvent{
					Text:       msg.Transcript,
					IsFinal:    true,
					Confidence: msg.Confidence,
				})
			}
		case "input_audio_buffer.speech_started":
			s.logger.Debug("qwen asr: speech started")
		case "input_audio_buffer.speech_stopped":
			s.logger.Debug("qwen asr: speech stopped")
		case "error":
			s.logger.Error("qwen asr: server error", "message", string(data))
		}
	}
}

func (s *qwenStream) emit(evt provider.ASREvent) {
	select {
	case s.events <- evt:
	case <-s.done:
	}
}
