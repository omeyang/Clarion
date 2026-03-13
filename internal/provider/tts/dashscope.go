// Package tts 实现 TTS（文本转语音）服务提供者。
package tts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
	"github.com/gofrs/uuid/v5"
	sonataprovider "github.com/omeyang/Sonata/engine/aiface"

	"github.com/omeyang/clarion/internal/provider"
)

// 编译时接口检查。
var (
	_ provider.TTSProvider  = (*DashScope)(nil)
	_ sonataprovider.Warmer = (*DashScope)(nil)
)

const defaultDashScopeTTSURL = "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"

// DashScope 通过 WebSocket 使用 DashScope CosyVoice 实现 provider.TTSProvider。
type DashScope struct {
	apiKey   string
	wsURL    string
	logger   *slog.Logger
	poolSize int
	pool     *wsPool

	mu       sync.Mutex
	cancelFn context.CancelFunc
}

// DashScopeOption 配置 DashScope TTS 提供者。
type DashScopeOption func(*DashScope)

// WithDashScopeLogger 设置自定义日志记录器。
func WithDashScopeLogger(l *slog.Logger) DashScopeOption {
	return func(d *DashScope) { d.logger = l }
}

// WithDashScopeWSURL 覆盖 WebSocket 端点（用于测试）。
func WithDashScopeWSURL(url string) DashScopeOption {
	return func(d *DashScope) { d.wsURL = url }
}

// WithDashScopePoolSize 设置 WebSocket 连接池大小。
// 启用后每次合成优先从池中取预建连接，减少建连延迟约 100ms。
// 设为 0 禁用连接池（默认行为）。
func WithDashScopePoolSize(size int) DashScopeOption {
	return func(d *DashScope) { d.poolSize = size }
}

// NewDashScope 创建新的 DashScope TTS 提供者。
func NewDashScope(apiKey string, opts ...DashScopeOption) *DashScope {
	d := &DashScope{
		apiKey: apiKey,
		wsURL:  defaultDashScopeTTSURL,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.poolSize > 0 {
		d.pool = newWSPool(d.dialWS, d.poolSize, d.logger)
	}
	return d
}

// ── DashScope WebSocket 协议消息结构 ─────────────────────────

// wsEnvelope 是 DashScope WebSocket 协议的通用消息信封。
type wsEnvelope struct {
	Header  wsHeader  `json:"header"`
	Payload wsPayload `json:"payload"`
}

// wsHeader 是消息头部。
type wsHeader struct {
	// 客户端请求字段。
	Action    string `json:"action,omitempty"`
	TaskID    string `json:"task_id"`
	Streaming string `json:"streaming,omitempty"`

	// 服务端响应字段。
	Event        string            `json:"event,omitempty"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// wsPayload 是消息载荷。
type wsPayload struct {
	TaskGroup  string          `json:"task_group,omitempty"`
	Task       string          `json:"task,omitempty"`
	Function   string          `json:"function,omitempty"`
	Model      string          `json:"model,omitempty"`
	Parameters *ttsParameters  `json:"parameters,omitempty"`
	Input      *ttsInput       `json:"input"`
	Output     json.RawMessage `json:"output,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
}

// ttsParameters 是合成参数。
type ttsParameters struct {
	TextType   string `json:"text_type"`
	Voice      string `json:"voice"`
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Volume     int    `json:"volume"`
	Rate       int    `json:"rate"`
	Pitch      int    `json:"pitch"`
}

// ttsInput 是输入载荷。
type ttsInput struct {
	Text string `json:"text,omitempty"`
}

// ── 核心实现 ─────────────────────────────────────────────────

// SynthesizeStream 接收文本通道并返回音频片段通道。
func (d *DashScope) SynthesizeStream(ctx context.Context, textCh <-chan string, cfg provider.TTSConfig) (<-chan []byte, error) {
	ctx, cancel := context.WithCancel(ctx)

	d.mu.Lock()
	d.cancelFn = cancel
	d.mu.Unlock()

	conn, taskID, err := d.acquireAndInit(ctx, cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	audioCh := make(chan []byte, 64)
	go d.streamLoop(ctx, cancel, conn, textCh, audioCh, taskID)

	return audioCh, nil
}

// acquireAndInit 获取连接并发送 run-task 初始化合成任务。
// 池中连接可能已失效（服务端超时关闭），此时自动回退为新建连接重试。
func (d *DashScope) acquireAndInit(ctx context.Context, cfg provider.TTSConfig) (*websocket.Conn, string, error) {
	conn, err := d.acquireConn(ctx)
	if err != nil {
		return nil, "", err
	}

	taskID := uuid.Must(uuid.NewV4()).String()
	if err := d.sendRunTask(ctx, conn, taskID, cfg); err != nil {
		d.closeConnQuietly(conn, "run-task failed")
		// 池中连接失效时回退新建连接；非池连接则直接返回错误。
		if d.pool == nil {
			return nil, "", err
		}
		d.logger.Debug("dashscope tts: 池中连接失效，回退新建连接")
		return d.dialAndInit(ctx, cfg)
	}

	return conn, taskID, nil
}

// dialAndInit 新建连接并初始化合成任务（无重试）。
func (d *DashScope) dialAndInit(ctx context.Context, cfg provider.TTSConfig) (*websocket.Conn, string, error) {
	conn, err := d.dialWS(ctx)
	if err != nil {
		return nil, "", err
	}
	taskID := uuid.Must(uuid.NewV4()).String()
	if err := d.sendRunTask(ctx, conn, taskID, cfg); err != nil {
		d.closeConnQuietly(conn, "run-task retry failed")
		return nil, "", err
	}
	return conn, taskID, nil
}

// closeConnQuietly 关闭连接，出错仅记录警告。
func (d *DashScope) closeConnQuietly(conn *websocket.Conn, reason string) {
	if closeErr := conn.Close(websocket.StatusInternalError, reason); closeErr != nil {
		d.logger.Warn("dashscope tts: close conn", "reason", reason, "error", closeErr)
	}
}

// acquireConn 获取 WebSocket 连接。优先使用池中预建连接，池空时直接建连。
func (d *DashScope) acquireConn(ctx context.Context) (*websocket.Conn, error) {
	if d.pool != nil {
		if conn := d.pool.Get(); conn != nil {
			d.logger.Debug("dashscope tts: 使用池中预建连接")
			return conn, nil
		}
	}
	return d.dialWS(ctx)
}

// dialWS 建立新的 WebSocket 连接。
// 同时作为连接池的 dialFunc，统一所有建连逻辑。
func (d *DashScope) dialWS(ctx context.Context) (*websocket.Conn, error) {
	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + d.apiKey}

	conn, resp, err := websocket.Dial(ctx, d.wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		// 读空 body 确保底层连接可复用。
		_, _ = io.Copy(io.Discard, resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			d.logger.Warn("dashscope tts: close dial response body", "error", cerr)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dashscope tts: dial websocket: %w", err)
	}
	return conn, nil
}

// streamLoop 在单个 goroutine 中输入文本并收集音频。
func (d *DashScope) streamLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	textCh <-chan string,
	audioCh chan<- []byte,
	taskID string,
) {
	defer close(audioCh)
	defer cancel()
	defer func() {
		if err := conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
			d.logger.Warn("dashscope tts: close websocket", "error", err)
		}
		// 连接用完后异步补充池中连接。
		if d.pool != nil {
			d.pool.Replenish()
		}
	}()

	readDone := make(chan struct{})
	go d.readAudioLoop(ctx, conn, audioCh, readDone)

	d.writeTextLoop(ctx, conn, textCh, taskID, readDone)
}

// sendRunTask 发送 run-task 消息初始化合成任务。
func (d *DashScope) sendRunTask(ctx context.Context, conn *websocket.Conn, taskID string, cfg provider.TTSConfig) error {
	model := cfg.Model
	if model == "" {
		model = "cosyvoice-v3-flash"
	}
	voice := cfg.Voice
	if voice == "" {
		voice = "longanyang"
	}
	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 16000
	}

	msg := wsEnvelope{
		Header: wsHeader{
			Action:    "run-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: wsPayload{
			TaskGroup: "audio",
			Task:      "tts",
			Function:  "SpeechSynthesizer",
			Model:     model,
			Parameters: &ttsParameters{
				TextType:   "PlainText",
				Voice:      voice,
				Format:     "pcm",
				SampleRate: sampleRate,
				Volume:     50,
				Rate:       1,
				Pitch:      1,
			},
			Input: &ttsInput{},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("dashscope tts: marshal run-task: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("dashscope tts: send run-task: %w", err)
	}

	d.logger.Debug("dashscope tts: sent run-task", "task_id", taskID)
	return nil
}

// readAudioLoop 从 WebSocket 读取消息并将音频转发到 audioCh。
func (d *DashScope) readAudioLoop(ctx context.Context, conn *websocket.Conn, audioCh chan<- []byte, done chan<- struct{}) {
	defer close(done)
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				d.logger.Warn("dashscope tts: read error", "error", err)
			}
			return
		}

		if typ == websocket.MessageBinary {
			select {
			case audioCh <- data:
			case <-ctx.Done():
				return
			}
			continue
		}

		if d.handleTextMessage(data) {
			return
		}
	}
}

// handleTextMessage 处理文本 WebSocket 消息。当循环应停止时返回 true。
func (d *DashScope) handleTextMessage(data []byte) bool {
	var resp wsEnvelope
	if err := json.Unmarshal(data, &resp); err != nil {
		d.logger.Warn("dashscope tts: unmarshal response", "error", err)
		return false
	}

	switch resp.Header.Event {
	case "task-started":
		d.logger.Debug("dashscope tts: task started", "task_id", resp.Header.TaskID)
	case "result-generated":
		// 音频数据在二进制帧中，这里只是元数据通知。
		d.logger.Debug("dashscope tts: result generated")
	case "task-finished":
		d.logger.Debug("dashscope tts: task finished", "task_id", resp.Header.TaskID)
		return true
	case "task-failed":
		d.logger.Error("dashscope tts: task failed",
			"error_code", resp.Header.ErrorCode,
			"error_message", resp.Header.ErrorMessage)
		return true
	}
	return false
}

// writeTextLoop 从 textCh 读取文本并通过 continue-task 发送。
func (d *DashScope) writeTextLoop(ctx context.Context, conn *websocket.Conn, textCh <-chan string, taskID string, readDone <-chan struct{}) {
	for {
		select {
		case text, ok := <-textCh:
			if !ok {
				d.sendFinishTask(ctx, conn, taskID)
				<-readDone
				return
			}
			if text == "" {
				continue
			}
			if err := d.sendContinueTask(ctx, conn, taskID, text); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// sendContinueTask 发送 continue-task 消息传递文本。
func (d *DashScope) sendContinueTask(ctx context.Context, conn *websocket.Conn, taskID, text string) error {
	msg := wsEnvelope{
		Header: wsHeader{
			Action:    "continue-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: wsPayload{
			Input: &ttsInput{Text: text},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		d.logger.Warn("dashscope tts: marshal continue-task", "error", err)
		return nil
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		d.logger.Warn("dashscope tts: send continue-task", "error", err)
		return fmt.Errorf("dashscope tts: send continue-task: %w", err)
	}
	return nil
}

// sendFinishTask 发送 finish-task 消息通知服务端合成结束。
func (d *DashScope) sendFinishTask(ctx context.Context, conn *websocket.Conn, taskID string) {
	msg := wsEnvelope{
		Header: wsHeader{
			Action:    "finish-task",
			TaskID:    taskID,
			Streaming: "duplex",
		},
		Payload: wsPayload{
			Input: &ttsInput{},
		},
	}
	data, _ := json.Marshal(msg)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		d.logger.Warn("dashscope tts: send finish-task", "error", err)
	}
}

// Synthesize 从完整文本生成音频（批量，用于预编译）。
func (d *DashScope) Synthesize(ctx context.Context, text string, cfg provider.TTSConfig) ([]byte, error) {
	textCh := make(chan string, 1)
	textCh <- text
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, cfg)
	if err != nil {
		return nil, err
	}

	var result []byte
	for chunk := range audioCh {
		result = append(result, chunk...)
	}

	if len(result) == 0 {
		return nil, errors.New("dashscope tts: no audio produced")
	}

	return result, nil
}

// Warmup 预热 WebSocket 连接。
// 启用连接池时填充池中所有连接槽位；未启用时做单次 DNS/TLS 预热。
func (d *DashScope) Warmup(ctx context.Context) error {
	if d.pool != nil {
		d.pool.Fill(ctx)
		d.logger.Debug("dashscope tts: 连接池预热完成",
			slog.Int("size", d.pool.Len()))
		return nil
	}

	// 无连接池时做单次建连预热 DNS 缓存。
	conn, err := d.dialWS(ctx)
	if err != nil {
		return fmt.Errorf("dashscope tts warmup: %w", err)
	}
	if closeErr := conn.Close(websocket.StatusNormalClosure, "warmup"); closeErr != nil {
		d.logger.Warn("dashscope tts: warmup close", "error", closeErr)
	}

	d.logger.Debug("dashscope tts: 连接预热完成")
	return nil
}

// Close 关闭连接池中的空闲连接，释放资源。
// 未启用连接池时为空操作。
func (d *DashScope) Close() error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

// Cancel 中止当前合成（打断场景）。
func (d *DashScope) Cancel() error {
	d.mu.Lock()
	fn := d.cancelFn
	d.cancelFn = nil
	d.mu.Unlock()

	if fn != nil {
		fn()
	}
	return nil
}
