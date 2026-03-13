// Package llm 实现 LLM 服务提供者。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sonataprovider "github.com/omeyang/Sonata/engine/aiface"

	"github.com/omeyang/clarion/internal/provider"
)

// 编译时接口检查：DeepSeek 实现 Warmer 接口。
var _ sonataprovider.Warmer = (*DeepSeek)(nil)

// 编译时接口检查。
var _ provider.LLMProvider = (*DeepSeek)(nil)

// DeepSeek 使用 DeepSeek API（OpenAI 兼容）实现 provider.LLMProvider。
type DeepSeek struct {
	apiKey  string
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// DeepSeekOption 配置 DeepSeek 提供者。
type DeepSeekOption func(*DeepSeek)

// WithHTTPClient 设置自定义 HTTP 客户端（用于测试）。
func WithHTTPClient(c *http.Client) DeepSeekOption {
	return func(d *DeepSeek) { d.client = c }
}

// WithLogger 设置自定义日志记录器。
func WithLogger(l *slog.Logger) DeepSeekOption {
	return func(d *DeepSeek) { d.logger = l }
}

// newPooledHTTPClient 创建配置了连接池的 HTTP 客户端。
// 连接池保活可避免每轮 LLM 调用重建 TCP+TLS 连接（节省约 200ms）。
func newPooledHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          10,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// NewDeepSeek 创建新的 DeepSeek LLM 提供者。
func NewDeepSeek(apiKey, baseURL string, opts ...DeepSeekOption) *DeepSeek {
	d := &DeepSeek{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  newPooledHTTPClient(),
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// chatRequest 是 chat completions API 的请求体。
type chatRequest struct {
	Model       string             `json:"model"`
	Messages    []provider.Message `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream"`
}

// chatResponse 是 chat completions API 的非流式响应。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// sseChunk 是来自流式 API 的单个 SSE 数据块。
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// GenerateStream 通过 SSE 流式返回响应 token 通道。
func (d *DeepSeek) GenerateStream(ctx context.Context, messages []provider.Message, cfg provider.LLMConfig) (<-chan string, error) {
	reqBody := chatRequest{
		Model:       d.model(cfg),
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
		Stream:      true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	var cancel context.CancelFunc
	if cfg.TimeoutMs > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
	}

	resp, err := d.doStreamRequest(ctx, body)
	if err != nil {
		cancelIfSet(cancel)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer d.closeBody(resp)
		cancelIfSet(cancel)
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deepseek: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan string, 32)
	go d.readSSEStream(ctx, cancel, resp, ch)

	return ch, nil
}

// doStreamRequest 创建并发送 SSE 流式 HTTP 请求。
func (d *DeepSeek) doStreamRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("deepseek: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek: send request: %w", err)
	}
	return resp, nil
}

// readSSEStream 从响应体读取 SSE 事件并将 token 发送到 ch。
func (d *DeepSeek) readSSEStream(ctx context.Context, cancel context.CancelFunc, resp *http.Response, ch chan<- string) {
	defer close(ch)
	defer d.closeBody(resp)
	if cancel != nil {
		defer cancel()
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		if done := d.emitSSEToken(ctx, data, ch); done {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		d.logger.Warn("deepseek: scanner error", "error", err)
	}
}

// emitSSEToken 解析单个 SSE 数据负载并将 token 发送到 ch。
// 当上下文结束且调用方应停止时返回 true。
func (d *DeepSeek) emitSSEToken(ctx context.Context, data string, ch chan<- string) bool {
	var chunk sseChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		d.logger.Warn("deepseek: unmarshal SSE chunk", "error", err)
		return false
	}
	if len(chunk.Choices) == 0 {
		return false
	}
	content := chunk.Choices[0].Delta.Content
	if content == "" {
		return false
	}
	select {
	case ch <- content:
		return false
	case <-ctx.Done():
		return true
	}
}

// closeBody 关闭响应体，出错时记录警告。
func (d *DeepSeek) closeBody(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		d.logger.Warn("deepseek: close response body", "error", err)
	}
}

// cancelIfSet 在 cancel 非 nil 时调用它。
func cancelIfSet(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
}

// Generate 返回完整响应（非流式）。
func (d *DeepSeek) Generate(ctx context.Context, messages []provider.Message, cfg provider.LLMConfig) (string, error) {
	reqBody := chatRequest{
		Model:       d.model(cfg),
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
		Stream:      false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("deepseek: marshal request: %w", err)
	}

	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("deepseek: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepseek: send request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			d.logger.Warn("deepseek: close response body", "error", closeErr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("deepseek: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deepseek: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("deepseek: unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("deepseek: no choices in response")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// Warmup 预热 HTTP 连接池，提前完成 TCP+TLS 握手。
// 发送 HEAD 请求到 baseURL 建立持久连接，后续 LLM 调用可复用。
func (d *DeepSeek) Warmup(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, d.baseURL+"/chat/completions", nil)
	if err != nil {
		return fmt.Errorf("deepseek warmup: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("deepseek warmup: send request: %w", err)
	}
	defer func() {
		// 读空并关闭 body，确保连接可被复用。
		_, _ = io.Copy(io.Discard, resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			d.logger.Warn("deepseek warmup: close body", "error", closeErr)
		}
	}()

	d.logger.Debug("deepseek: 连接池预热完成", slog.Int("status", resp.StatusCode))
	return nil
}

// model 返回 cfg 中的模型名，若为空则返回默认值。
func (d *DeepSeek) model(cfg provider.LLMConfig) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	return "deepseek-chat"
}
