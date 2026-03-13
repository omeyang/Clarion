// Package realtime 实现实时全模态语音提供者。
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const defaultOmniURL = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"

// OmniConfig 配置 Omni 实时语音提供者。
type OmniConfig struct {
	APIKey string
	WSURL  string // 覆盖 WebSocket 端点（用于测试）。
	Logger *slog.Logger
}

// Omni 通过 WebSocket 使用 Qwen3-Omni-Flash-Realtime 实现实时语音对话。
// 单个连接同时处理音频输入/输出和文本转录，替代 ASR+LLM+TTS 三段管线。
type Omni struct {
	apiKey string
	wsURL  string
	logger *slog.Logger

	conn        *websocket.Conn
	audioOut    chan []byte
	transcripts chan TranscriptEvent
	done        chan struct{}
	cancelRd    context.CancelFunc

	closeOnce sync.Once
}

// TranscriptEvent 是来自实时模型的文本事件。
type TranscriptEvent struct {
	Role      string
	Text      string
	IsFinal   bool
	Timestamp time.Time
}

// ConnectConfig 持有实时语音会话的配置。
type ConnectConfig struct {
	Model             string
	Voice             string
	Instructions      string
	InputSampleRate   int
	OutputSampleRate  int
	VADEnabled        bool
	VADThreshold      float64
	SilenceDurationMs int
}

// NewOmni 创建新的 Omni 实时语音提供者。
func NewOmni(cfg OmniConfig) *Omni {
	wsURL := cfg.WSURL
	if wsURL == "" {
		wsURL = defaultOmniURL
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Omni{
		apiKey: cfg.APIKey,
		wsURL:  wsURL,
		logger: logger,
	}
}

// ── OpenAI Realtime 兼容协议消息 ──────────────────────────────

type wsMsg struct {
	EventID string `json:"event_id,omitempty"`
	Type    string `json:"type"`

	Session  *sessionCfg  `json:"session,omitempty"`
	Item     *convItem    `json:"item,omitempty"`
	Response *responseCfg `json:"response,omitempty"`
	Audio    string       `json:"audio,omitempty"`
	Delta    string       `json:"delta,omitempty"`
	Text     string       `json:"text,omitempty"`
}

type sessionCfg struct {
	Modalities       []string       `json:"modalities"`
	Voice            string         `json:"voice,omitempty"`
	InputAudioFormat string         `json:"input_audio_format,omitempty"`
	OutputAudioFmt   string         `json:"output_audio_format,omitempty"`
	Instructions     string         `json:"instructions,omitempty"`
	TurnDetection    *turnDetection `json:"turn_detection"`
}

type turnDetection struct {
	Type              string  `json:"type"`
	Threshold         float64 `json:"threshold"`
	SilenceDurationMs int     `json:"silence_duration_ms"`
}

type convItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []convContent `json:"content"`
}

type convContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type responseCfg struct {
	Modalities []string `json:"modalities,omitempty"`
}

// Connect 建立实时语音会话。
func (o *Omni) Connect(ctx context.Context, cfg ConnectConfig) error {
	model := cfg.Model
	if model == "" {
		model = "qwen3-omni-flash-realtime"
	}

	headers := map[string][]string{
		"Authorization": {"Bearer " + o.apiKey},
		"OpenAI-Beta":   {"realtime=v1"},
	}

	dialURL := o.wsURL + "?model=" + model
	conn, resp, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				o.logger.Warn("omni: close dial response body", "error", cerr)
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("omni: dial websocket: %w", err)
	}

	// 放大读取限制（音频数据较大）。
	conn.SetReadLimit(10 * 1024 * 1024)

	// 构建 session.update。
	voice := cfg.Voice
	if voice == "" {
		voice = "Cherry"
	}

	var td *turnDetection
	if cfg.VADEnabled {
		threshold := cfg.VADThreshold
		if threshold == 0 {
			threshold = 0.5
		}
		silenceMs := cfg.SilenceDurationMs
		if silenceMs == 0 {
			silenceMs = 500
		}
		td = &turnDetection{
			Type:              "server_vad",
			Threshold:         threshold,
			SilenceDurationMs: silenceMs,
		}
	}

	update := wsMsg{
		Type: "session.update",
		Session: &sessionCfg{
			Modalities:       []string{"text", "audio"},
			Voice:            voice,
			InputAudioFormat: "pcm",
			OutputAudioFmt:   "pcm",
			Instructions:     cfg.Instructions,
			TurnDetection:    td,
		},
	}

	if err := writeJSON(ctx, conn, update); err != nil {
		if closeErr := conn.Close(websocket.StatusInternalError, "session.update failed"); closeErr != nil {
			o.logger.Warn("omni: close after session.update error", "error", closeErr)
		}
		return fmt.Errorf("omni: send session.update: %w", err)
	}

	o.conn = conn
	o.audioOut = make(chan []byte, 128)
	o.transcripts = make(chan TranscriptEvent, 64)
	o.done = make(chan struct{})

	rdCtx, rdCancel := context.WithCancel(context.Background())
	o.cancelRd = rdCancel

	go o.readLoop(rdCtx)

	o.logger.Info("omni: 已连接", "model", model, "voice", voice)
	return nil
}

// FeedAudio 向模型发送用户音频帧。
func (o *Omni) FeedAudio(ctx context.Context, frame []byte) error {
	select {
	case <-o.done:
		return errors.New("omni: connection closed")
	default:
	}

	msg := wsMsg{
		Type:  "input_audio_buffer.append",
		Audio: base64.StdEncoding.EncodeToString(frame),
	}
	return writeJSON(ctx, o.conn, msg)
}

// AudioOut 返回模型生成的音频帧通道。
func (o *Omni) AudioOut() <-chan []byte {
	return o.audioOut
}

// Transcripts 返回模型生成的文本转录通道。
func (o *Omni) Transcripts() <-chan TranscriptEvent {
	return o.transcripts
}

// UpdateInstructions 动态更新模型的系统指令。
func (o *Omni) UpdateInstructions(ctx context.Context, instructions string) error {
	select {
	case <-o.done:
		return errors.New("omni: connection closed")
	default:
	}

	msg := wsMsg{
		Type: "session.update",
		Session: &sessionCfg{
			Instructions: instructions,
		},
	}
	return writeJSON(ctx, o.conn, msg)
}

// Interrupt 中断当前模型回复（barge-in 场景）。
func (o *Omni) Interrupt(ctx context.Context) error {
	select {
	case <-o.done:
		return errors.New("omni: connection closed")
	default:
	}

	msg := wsMsg{Type: "response.cancel"}
	return writeJSON(ctx, o.conn, msg)
}

// Close 关闭实时会话。
func (o *Omni) Close() error {
	var err error
	o.closeOnce.Do(func() {
		close(o.done)
		if o.cancelRd != nil {
			o.cancelRd()
		}
		if o.conn != nil {
			if closeErr := o.conn.Close(websocket.StatusNormalClosure, "close"); closeErr != nil {
				err = fmt.Errorf("omni: close websocket: %w", closeErr)
			}
		}
	})
	return err
}

// readLoop 从 WebSocket 读取消息并分发到相应通道。
func (o *Omni) readLoop(ctx context.Context) {
	defer close(o.audioOut)
	defer close(o.transcripts)

	for {
		select {
		case <-o.done:
			return
		default:
		}

		_, data, err := o.conn.Read(ctx)
		if err != nil {
			select {
			case <-o.done:
				// 预期的关闭。
			default:
				o.logger.Warn("omni: read error", "error", err)
			}
			return
		}

		var msg wsMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			o.logger.Warn("omni: unmarshal message", "error", err)
			continue
		}

		o.handleMessage(msg)
	}
}

// handleMessage 处理单条 WebSocket 消息。
func (o *Omni) handleMessage(msg wsMsg) {
	switch msg.Type {
	case "session.created", "session.updated":
		o.logger.Debug("omni: session event", "type", msg.Type)

	case "response.audio.delta":
		o.handleAudioDelta(msg.Delta)

	case "response.audio_transcript.delta":
		o.emitTranscript(TranscriptEvent{
			Role:      "assistant",
			Text:      msg.Delta,
			Timestamp: time.Now(),
		})

	case "response.audio_transcript.done":
		o.emitTranscript(TranscriptEvent{
			Role:      "assistant",
			Text:      msg.Text,
			IsFinal:   true,
			Timestamp: time.Now(),
		})

	case "conversation.item.input_audio_transcription.completed":
		o.emitTranscript(TranscriptEvent{
			Role:      "user",
			Text:      msg.Text,
			IsFinal:   true,
			Timestamp: time.Now(),
		})

	case "input_audio_buffer.speech_started":
		o.logger.Debug("omni: 用户开始说话")

	case "input_audio_buffer.speech_stopped":
		o.logger.Debug("omni: 用户停止说话")

	case "response.audio.done":
		o.logger.Debug("omni: 音频输出完成")

	case "response.done":
		o.logger.Debug("omni: 回复完成")

	case "error":
		o.logger.Error("omni: 服务端错误", "type", msg.Type)

	default:
		o.logger.Debug("omni: 未处理事件", "type", msg.Type)
	}
}

// handleAudioDelta 解码并转发音频片段。
func (o *Omni) handleAudioDelta(delta string) {
	if delta == "" {
		return
	}
	chunk, err := base64.StdEncoding.DecodeString(delta)
	if err != nil {
		o.logger.Warn("omni: decode audio delta", "error", err)
		return
	}
	select {
	case o.audioOut <- chunk:
	case <-o.done:
	}
}

// emitTranscript 将转录事件发送到通道。
func (o *Omni) emitTranscript(evt TranscriptEvent) {
	select {
	case o.transcripts <- evt:
	case <-o.done:
	}
}

// writeJSON 序列化并发送 JSON 消息。
func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
