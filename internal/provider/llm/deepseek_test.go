package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepSeek_GenerateStream(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		wantTokens []string
		wantErr    bool
	}{
		{
			name: "basic_streaming",
			response: sseLines(
				`{"choices":[{"delta":{"content":"Hello"}}]}`,
				`{"choices":[{"delta":{"content":" world"}}]}`,
				`{"choices":[{"delta":{"content":"!"}}]}`,
			),
			wantTokens: []string{"Hello", " world", "!"},
		},
		{
			name: "empty_delta_skipped",
			response: sseLines(
				`{"choices":[{"delta":{"content":"Hi"}}]}`,
				`{"choices":[{"delta":{"content":""}}]}`,
				`{"choices":[{"delta":{"content":"."}}]}`,
			),
			wantTokens: []string{"Hi", "."},
		},
		{
			name: "no_choices",
			response: sseLines(
				`{"choices":[]}`,
			),
			wantTokens: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/chat/completions", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, tt.response)
			}))
			defer srv.Close()

			ds := NewDeepSeek("test-key", srv.URL)
			ch, err := ds.GenerateStream(context.Background(), []provider.Message{
				{Role: "user", Content: "Hello"},
			}, provider.LLMConfig{Model: "deepseek-chat"})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var tokens []string
			for tok := range ch {
				tokens = append(tokens, tok)
			}
			assert.Equal(t, tt.wantTokens, tokens)
		})
	}
}

func TestDeepSeek_Generate(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantText   string
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   `{"choices":[{"message":{"content":"Hello, I'm DeepSeek."}}]}`,
			wantText:   "Hello, I'm DeepSeek.",
		},
		{
			name:       "no_choices",
			statusCode: http.StatusOK,
			response:   `{"choices":[]}`,
			wantErr:    true,
		},
		{
			name:       "server_error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error":"internal error"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/chat/completions", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.response)
			}))
			defer srv.Close()

			ds := NewDeepSeek("test-key", srv.URL)
			text, err := ds.Generate(context.Background(), []provider.Message{
				{Role: "user", Content: "Hello"},
			}, provider.LLMConfig{Model: "deepseek-chat"})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantText, text)
		})
	}
}

func TestDeepSeek_GenerateStream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write one token then hang.
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until request is cancelled.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ds := NewDeepSeek("test-key", srv.URL)
	ch, err := ds.GenerateStream(ctx, []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat"})
	require.NoError(t, err)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	// Should get at least "Hi" before context cancellation.
	assert.Contains(t, tokens, "Hi")
}

func TestDeepSeek_GenerateStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	_, err := ds.GenerateStream(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestDeepSeek_ModelDefault(t *testing.T) {
	ds := NewDeepSeek("key", "http://example.com")
	assert.Equal(t, "deepseek-chat", ds.model(provider.LLMConfig{}))
	assert.Equal(t, "custom-model", ds.model(provider.LLMConfig{Model: "custom-model"}))
}

func TestNewDeepSeek_Options(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	ds := NewDeepSeek("key", "http://example.com/", WithHTTPClient(client))
	assert.Equal(t, client, ds.client)
	assert.Equal(t, "http://example.com", ds.baseURL) // trailing slash trimmed
}

func TestNewDeepSeek_WithLogger(t *testing.T) {
	l := slog.Default()
	ds := NewDeepSeek("key", "http://example.com", WithLogger(l))
	assert.Equal(t, l, ds.logger)
}

func TestDeepSeek_GenerateStream_MalformedJSON(t *testing.T) {
	// 覆盖 emitSSEToken 中 JSON 反序列化失败的分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {invalid-json}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	ch, err := ds.GenerateStream(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat"})
	require.NoError(t, err)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	assert.Empty(t, tokens)
}

func TestDeepSeek_GenerateStream_HTTPErrorWithTimeout(t *testing.T) {
	// 覆盖 GenerateStream 中 HTTP 错误 + 超时场景下 cancelIfSet(cancel) 的非 nil 分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	_, err := ds.GenerateStream(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat", TimeoutMs: 5000})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestDeepSeek_GenerateStream_WithTimeout(t *testing.T) {
	// 覆盖 GenerateStream 成功路径中 TimeoutMs > 0 的分支以及 readSSEStream 中 cancel 非 nil 的 defer。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseLines(`{"choices":[{"delta":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	ch, err := ds.GenerateStream(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat", TimeoutMs: 5000})
	require.NoError(t, err)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	assert.Equal(t, []string{"ok"}, tokens)
}

func TestDeepSeek_Generate_WithTimeout(t *testing.T) {
	// 覆盖 Generate 中 TimeoutMs > 0 的分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Hi"}}]}`)
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	text, err := ds.Generate(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat", TimeoutMs: 5000})
	require.NoError(t, err)
	assert.Equal(t, "Hi", text)
}

func TestDeepSeek_Generate_InvalidJSON(t *testing.T) {
	// 覆盖 Generate 中 JSON 反序列化失败的分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `not-json`)
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	_, err := ds.Generate(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal response")
}

func TestDeepSeek_Generate_RequestError(t *testing.T) {
	// 覆盖 Generate 中 HTTP 请求发送失败的分支。
	ds := NewDeepSeek("test-key", "http://127.0.0.1:1") // 不可达地址
	_, err := ds.Generate(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
}

func TestDeepSeek_GenerateStream_RequestError(t *testing.T) {
	// 覆盖 GenerateStream 中 doStreamRequest 失败 + 有超时的 cancelIfSet 分支。
	ds := NewDeepSeek("test-key", "http://127.0.0.1:1")
	_, err := ds.GenerateStream(context.Background(), []provider.Message{
		{Role: "user", Content: "Hello"},
	}, provider.LLMConfig{Model: "deepseek-chat", TimeoutMs: 5000})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
}

func TestDeepSeek_Warmup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ds := NewDeepSeek("test-key", srv.URL)
	err := ds.Warmup(context.Background())
	require.NoError(t, err)
}

func TestDeepSeek_Warmup_ConnectionError(t *testing.T) {
	ds := NewDeepSeek("test-key", "http://127.0.0.1:1")
	err := ds.Warmup(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warmup")
}

func TestDeepSeek_PooledHTTPClient(t *testing.T) {
	// 验证默认客户端配置了连接池 Transport。
	ds := NewDeepSeek("key", "http://example.com")
	transport, ok := ds.client.Transport.(*http.Transport)
	require.True(t, ok, "默认 Transport 应为 *http.Transport")
	assert.Equal(t, 10, transport.MaxIdleConns)
	assert.Equal(t, 10, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
	assert.Equal(t, 30*time.Second, transport.ResponseHeaderTimeout)
	assert.True(t, transport.ForceAttemptHTTP2)
}

// sseLines builds an SSE response body from JSON data lines.
func sseLines(dataLines ...string) string {
	var b strings.Builder
	for _, d := range dataLines {
		b.WriteString("data: ")
		b.WriteString(d)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}
