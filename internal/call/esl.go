package call

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omeyang/clarion/internal/config"
)

// ESLEvent 表示解析后的 FreeSWITCH Event Socket 事件。
type ESLEvent struct {
	Name    string
	Headers map[string]string
	Body    string
}

// Header 按名称返回头部值，不存在时返回空字符串。
func (e ESLEvent) Header(name string) string {
	return e.Headers[name]
}

// UUID 返回 Unique-ID 头部，用于标识通话腿。
func (e ESLEvent) UUID() string {
	return e.Headers["Unique-ID"]
}

// eslResponse 是 readLoop 传递给 sendRaw 的命令响应。
type eslResponse struct {
	headers map[string]string
	body    string
	err     error
}

// ESLClient 是 FreeSWITCH Event Socket Library 客户端。
// 通过 TCP 连接、认证、发送命令和分发事件。
//
// 架构：所有从 TCP 连接的读取都集中在 readLoop goroutine 中。
// sendRaw 只负责写入命令，然后通过 replyCh 等待 readLoop 传回响应。
// 这避免了 sendRaw 和 readLoop 并发读取同一个 bufio.Reader 的竞争。
type ESLClient struct {
	cfg    config.FreeSWITCH
	logger *slog.Logger

	writeMu sync.Mutex // 保护写入操作
	conn    net.Conn
	reader  *bufio.Reader

	// replyCh 用于 readLoop 将命令响应传递给 sendRaw。
	// sendRaw 写入命令后，从此 channel 接收响应。
	replyCh chan eslResponse

	events chan ESLEvent
	done   chan struct{}

	closeMu sync.Mutex
	closed  bool
}

// NewESLClient 创建 ESL 客户端。调用 Connect 建立连接。
func NewESLClient(cfg config.FreeSWITCH, logger *slog.Logger) *ESLClient {
	return &ESLClient{
		cfg:     cfg,
		logger:  logger,
		replyCh: make(chan eslResponse, 1),
		events:  make(chan ESLEvent, 256),
		done:    make(chan struct{}),
	}
}

// Connect 建立到 FreeSWITCH ESL 的 TCP 连接并认证。
// 启动后台 goroutine 读取事件。
func (c *ESLClient) Connect(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", c.cfg.ESLHost, c.cfg.ESLPort)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial ESL %s: %w", addr, err)
	}

	c.conn = conn
	// FreeSWITCH 事件可能很大（含完整 SIP 头），默认 4096 不够
	c.reader = bufio.NewReaderSize(conn, 65536)

	// 如果后续握手失败，关闭连接。
	success := false
	defer func() {
		if !success {
			if closeErr := conn.Close(); closeErr != nil {
				c.logger.Warn("close conn after handshake failure", slog.String("error", closeErr.Error()))
			}
		}
	}()

	if err := c.authenticate(); err != nil {
		return err
	}
	if err := c.subscribeEvents(); err != nil {
		return err
	}

	success = true
	c.logger.Info("ESL connected", slog.String("addr", addr))

	// 从此刻起所有读取都由 readLoop 负责
	go c.readLoop()

	return nil
}

// authenticate 完成 ESL 认证握手。
// readLoop 尚未启动，直接从 TCP 连接读取。
func (c *ESLClient) authenticate() error {
	// FreeSWITCH 在连接时发送 "Content-Type: auth/request"。
	if _, _, err := c.readPacket(); err != nil {
		return fmt.Errorf("read auth request: %w", err)
	}

	if _, err := fmt.Fprintf(c.conn, "auth %s\n\n", c.cfg.ESLPassword); err != nil {
		return fmt.Errorf("auth write: %w", err)
	}

	headers, body, err := c.readPacket()
	if err != nil {
		return fmt.Errorf("auth read: %w", err)
	}

	replyText := headers["Reply-Text"]
	if replyText == "" {
		replyText = body
	}
	if !strings.Contains(replyText, "+OK") {
		return fmt.Errorf("auth failed: %s", replyText)
	}

	return nil
}

// subscribeEvents 订阅 FreeSWITCH 事件。
// 只订阅呼叫相关事件，避免 HEARTBEAT、RE_SCHEDULE 等无关事件淹没通道。
// readLoop 尚未启动，直接从 TCP 连接读取。
func (c *ESLClient) subscribeEvents() error {
	// 只订阅呼叫流程必需的事件类型。
	events := strings.Join([]string{
		"CHANNEL_CREATE",
		"CHANNEL_PROGRESS",
		"CHANNEL_ANSWER",
		"CHANNEL_HANGUP",
		"CHANNEL_HANGUP_COMPLETE",
		"CHANNEL_DESTROY",
		"CHANNEL_STATE",
		"CHANNEL_CALLSTATE",
		"BACKGROUND_JOB",
		"PLAYBACK_START",
		"PLAYBACK_STOP",
		"DETECTED_SPEECH",
		"CUSTOM",
		"CODEC",
	}, " ")
	if _, err := fmt.Fprintf(c.conn, "event plain %s\n\n", events); err != nil {
		return fmt.Errorf("subscribe write: %w", err)
	}

	headers, body, err := c.readPacket()
	if err != nil {
		return fmt.Errorf("subscribe read: %w", err)
	}

	reply := headers["Reply-Text"]
	if reply == "" {
		reply = body
	}
	if !strings.Contains(reply, "+OK") {
		c.logger.Warn("event subscribe reply", slog.String("reply", reply))
	}

	return nil
}

// Close 关闭 ESL 连接并停止事件读取器。
func (c *ESLClient) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	close(c.done)

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("close ESL connection: %w", err)
		}
	}
	return nil
}

// SendCommand 向 FreeSWITCH 发送同步 api 命令并返回响应体。
func (c *ESLClient) SendCommand(ctx context.Context, cmd string) (string, error) {
	return c.sendRaw(ctx, "api "+cmd)
}

// Originate 通过指定网关发起外呼。
// 成功时返回 Job-UUID。
func (c *ESLClient) Originate(ctx context.Context, gateway, callerID, callee, sessionID string) (string, error) {
	cmd := fmt.Sprintf(
		"bgapi originate {origination_caller_id_number=%s,clarion_session_id=%s}sofia/gateway/%s/%s &park()",
		callerID, sessionID, gateway, callee,
	)
	reply, err := c.sendRaw(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("originate: %w", err)
	}

	// bgapi 返回 "+OK Job-UUID: <uuid>"
	if strings.Contains(reply, "+OK") {
		parts := strings.SplitN(reply, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("originate failed: %s", reply)
}

// OriginateLoopback 通过 loopback 端点发起内部呼叫（用于本地测试）。
// extension 是拨号计划中的扩展名，context 通常为 "default"。
func (c *ESLClient) OriginateLoopback(ctx context.Context, extension, dialplanContext, sessionID string) (string, error) {
	cmd := fmt.Sprintf(
		"bgapi originate {clarion_session_id=%s}loopback/%s/%s &park()",
		sessionID, extension, dialplanContext,
	)
	reply, err := c.sendRaw(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("originate loopback: %w", err)
	}

	if strings.Contains(reply, "+OK") {
		parts := strings.SplitN(reply, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("originate loopback failed: %s", reply)
}

// OriginateUser 呼叫已注册的本地 SIP 用户（用于 SIP 软电话测试）。
// user 是注册用户名（如 "1000"），sipDomain 是 FreeSWITCH 的 SIP 域名。
// callerID 是主叫号码显示，sessionID 用于在事件中标识该通话。
func (c *ESLClient) OriginateUser(ctx context.Context, user, sipDomain, callerID, sessionID string) (string, error) {
	cmd := fmt.Sprintf(
		"bgapi originate {origination_caller_id_number=%s,clarion_session_id=%s}user/%s@%s &park()",
		callerID, sessionID, user, sipDomain,
	)
	c.logger.Info("ESL originate user 命令", slog.String("cmd", cmd))
	reply, err := c.sendRaw(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("originate user: %w", err)
	}

	if strings.Contains(reply, "+OK") {
		parts := strings.SplitN(reply, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("originate user failed: %s", reply)
}

// AudioForkStart 开始将通话音频流式传输到 WebSocket URL。
func (c *ESLClient) AudioForkStart(ctx context.Context, uuid, wsURL string) error {
	cmd := fmt.Sprintf("uuid_audio_fork %s start %s mono 8000", uuid, wsURL)
	reply, err := c.SendCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("audio_fork start: %w", err)
	}
	if strings.Contains(reply, "-ERR") {
		return fmt.Errorf("audio_fork start: %s", reply)
	}
	return nil
}

// AudioForkStop 停止通话的音频流。
func (c *ESLClient) AudioForkStop(ctx context.Context, uuid string) error {
	cmd := fmt.Sprintf("uuid_audio_fork %s stop", uuid)
	_, err := c.SendCommand(ctx, cmd)
	return err
}

// Break 中断通道上的播放。
func (c *ESLClient) Break(ctx context.Context, uuid string) error {
	cmd := fmt.Sprintf("uuid_break %s all", uuid)
	_, err := c.SendCommand(ctx, cmd)
	return err
}

// Kill 以给定原因终止通话。
func (c *ESLClient) Kill(ctx context.Context, uuid, cause string) error {
	cmd := fmt.Sprintf("uuid_kill %s %s", uuid, cause)
	_, err := c.SendCommand(ctx, cmd)
	return err
}

// Events 返回 ESL 事件的只读通道。
func (c *ESLClient) Events() <-chan ESLEvent {
	return c.events
}

// sendRaw 写入 ESL 命令，然后等待 readLoop 通过 replyCh 传回响应。
func (c *ESLClient) sendRaw(ctx context.Context, cmd string) (string, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return "", errors.New("ESL not connected")
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return "", fmt.Errorf("set write deadline: %w", err)
		}
		defer func() {
			if err := c.conn.SetWriteDeadline(time.Time{}); err != nil {
				c.logger.Warn("clear write deadline", slog.String("error", err.Error()))
			}
		}()
	}

	if _, err := fmt.Fprintf(c.conn, "%s\n\n", cmd); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	// 等待 readLoop 通过 replyCh 传回命令响应
	select {
	case resp := <-c.replyCh:
		return resp.body, resp.err
	case <-c.done:
		return "", errors.New("ESL connection closed")
	case <-ctx.Done():
		return "", fmt.Errorf("wait ESL reply: %w", ctx.Err())
	}
}

// readPacket 从 reader 读取一个完整的 ESL 数据包（头部 + 可选正文）。
// 仅在 readLoop 启动前由 Connect 调用。
func (c *ESLClient) readPacket() (headers map[string]string, body string, err error) {
	headers, err = c.readHeaders()
	if err != nil {
		return nil, "", err
	}

	contentLength := 0
	if cl, ok := headers["Content-Length"]; ok {
		if n, parseErr := strconv.Atoi(cl); parseErr == nil {
			contentLength = n
		}
	}

	if contentLength > 0 {
		buf := make([]byte, contentLength)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return headers, "", fmt.Errorf("read body: %w", err)
		}
		body = string(buf)
	}

	return headers, body, nil
}

// readHeaders 读取 ESL 头部直到空行。
func (c *ESLClient) readHeaders() (map[string]string, error) {
	headers := make(map[string]string)

	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header line: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			headers[parts[0]] = parts[1]
		}
	}

	return headers, nil
}

// readLoop 是唯一从 TCP 连接读取数据的 goroutine。
// 它区分命令响应（api/command reply）和异步事件（text/event-plain），
// 将命令响应通过 replyCh 传给 sendRaw，将事件通过 events 通道分发。
func (c *ESLClient) readLoop() {
	defer close(c.events)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		headers, body, err := c.readPacket()
		if err != nil {
			c.closeMu.Lock()
			closed := c.closed
			c.closeMu.Unlock()
			if closed {
				return
			}
			c.logger.Error("ESL read error", slog.String("error", err.Error()))
			return
		}

		ct := headers["Content-Type"]

		c.logger.Debug("ESL packet received",
			slog.String("content_type", ct),
			slog.String("reply_text", headers["Reply-Text"]),
			slog.Int("body_len", len(body)))

		switch ct {
		case "text/event-plain":
			// 异步事件：解析并分发
			event := parseESLEvent(body)
			c.logger.Debug("ESL event parsed",
				slog.String("event_name", event.Name),
				slog.String("uuid", event.UUID()),
				slog.String("session_var", event.Header("variable_clarion_session_id")))
			// BACKGROUND_JOB 包含 originate 的实际结果，记录 body 以便调试
			if event.Name == "BACKGROUND_JOB" {
				c.logger.Info("BACKGROUND_JOB result",
					slog.String("job_uuid", event.Header("Job-UUID")),
					slog.String("body", event.Body))
			}
			select {
			case c.events <- event:
			default:
				c.logger.Warn("ESL event channel full, dropping event",
					slog.String("event", event.Name))
			}

		case "api/response", "command/reply":
			// 命令响应：传递给等待的 sendRaw
			replyText := headers["Reply-Text"]
			if body != "" {
				replyText = body
			}
			c.logger.Debug("ESL sending reply to channel",
				slog.String("reply", replyText[:min(len(replyText), 100)]))
			select {
			case c.replyCh <- eslResponse{headers: headers, body: replyText}:
			default:
				c.logger.Warn("ESL reply channel full, dropping response")
			}

		case "text/disconnect-notice":
			c.logger.Warn("ESL disconnect notice received")
			return

		default:
			// 未知类型，忽略
			c.logger.Debug("ESL unknown content type", slog.String("type", ct))
		}
	}
}

// parseESLEvent 将 ESL 纯文本事件解析为 ESLEvent。
func parseESLEvent(raw string) ESLEvent {
	event := ESLEvent{
		Headers: make(map[string]string),
	}

	// 通过双换行分割头部和正文。
	parts := strings.SplitN(raw, "\n\n", 2)
	headerSection := parts[0]
	if len(parts) == 2 {
		event.Body = parts[1]
	}

	for line := range strings.SplitSeq(headerSection, "\n") {
		line = strings.TrimRight(line, "\r")
		kv := strings.SplitN(line, ": ", 2)
		if len(kv) == 2 {
			event.Headers[kv[0]] = kv[1]
		}
	}

	event.Name = event.Headers["Event-Name"]

	return event
}

// ParseESLEventForTest 是 parseESLEvent 的导出封装，用于测试。
func ParseESLEventForTest(raw string) ESLEvent {
	return parseESLEvent(raw)
}

// FormatOriginateCmd 格式化 originate 命令字符串，用于测试。
func FormatOriginateCmd(gateway, callerID, callee string) string {
	return fmt.Sprintf(
		"bgapi originate {origination_caller_id_number=%s}sofia/gateway/%s/%s &park()",
		callerID, gateway, callee,
	)
}
