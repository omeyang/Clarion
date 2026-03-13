package tts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/omeyang/clarion/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDashScope_Defaults(t *testing.T) {
	d := NewDashScope("test-key")
	assert.Equal(t, "test-key", d.apiKey)
	assert.Equal(t, defaultDashScopeTTSURL, d.wsURL)
	assert.NotNil(t, d.logger)
}

func TestNewDashScope_Options(t *testing.T) {
	d := NewDashScope("test-key", WithDashScopeWSURL("ws://localhost:9999"))
	assert.Equal(t, "ws://localhost:9999", d.wsURL)
}

// mockTTSServer 模拟 DashScope TTS WebSocket 服务端协议。
func mockTTSServer(t *testing.T, audioData []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("accept error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}

			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			switch msg.Header.Action {
			case "run-task":
				// 返回 task-started 事件。
				started := wsEnvelope{
					Header: wsHeader{
						TaskID: msg.Header.TaskID,
						Event:  "task-started",
					},
				}
				sdata, _ := json.Marshal(started)
				if err := conn.Write(r.Context(), websocket.MessageText, sdata); err != nil {
					return
				}

			case "continue-task":
				// 返回音频二进制帧。
				if err := conn.Write(r.Context(), websocket.MessageBinary, audioData); err != nil {
					return
				}

			case "finish-task":
				// 返回 task-finished 事件。
				finished := wsEnvelope{
					Header: wsHeader{
						TaskID: msg.Header.TaskID,
						Event:  "task-finished",
					},
				}
				fdata, _ := json.Marshal(finished)
				if err := conn.Write(r.Context(), websocket.MessageText, fdata); err != nil {
					return
				}
				return
			}
		}
	}))
}

func TestDashScope_SynthesizeStream(t *testing.T) {
	audioData := []byte("fake-pcm-audio-data")
	srv := mockTTSServer(t, audioData)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key", WithDashScopeWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 2)
	textCh <- "Hello"
	textCh <- "World"
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{
		Model:      "cosyvoice-v3-flash",
		Voice:      "longanyang",
		SampleRate: 16000,
	})
	require.NoError(t, err)

	var chunks [][]byte
	for chunk := range audioCh {
		chunks = append(chunks, chunk)
	}

	// 每段文本对应一个音频片段。
	require.GreaterOrEqual(t, len(chunks), 2)
	assert.Equal(t, audioData, chunks[0])
}

func TestDashScope_Synthesize(t *testing.T) {
	audioData := []byte("batch-audio-data")
	srv := mockTTSServer(t, audioData)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key", WithDashScopeWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	audio, err := d.Synthesize(ctx, "Hello world", provider.TTSConfig{})
	require.NoError(t, err)
	assert.Equal(t, audioData, audio)
}

func TestDashScope_Cancel(t *testing.T) {
	// Cancel 无活跃合成时不应 panic。
	d := NewDashScope("test-key")
	err := d.Cancel()
	assert.NoError(t, err)
}

func TestDashScope_Cancel_ActiveSynthesis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// 读取消息但不响应音频，保持连接。
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Header.Action == "run-task" {
				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key", WithDashScopeWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用不关闭的文本通道。
	textCh := make(chan string)
	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	// 等待 goroutine 启动。
	time.Sleep(50 * time.Millisecond)
	err = d.Cancel()
	assert.NoError(t, err)

	// 音频通道应最终关闭。
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-audioCh:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for audio channel to close after cancel")
		}
	}
}

func TestWithDashScopeLogger(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDashScope("key", WithDashScopeLogger(l))
	assert.Equal(t, l, d.logger)
}

func TestDashScope_SynthesizeStream_DialError(t *testing.T) {
	// 连接不存在的地址应返回错误。
	d := NewDashScope("key", WithDashScopeWSURL("ws://127.0.0.1:1"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	textCh := make(chan string)
	_, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial websocket")
}

func TestDashScope_Synthesize_NoAudio(t *testing.T) {
	// 服务端不返回任何音频数据时应返回 "no audio produced" 错误。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Header.Action {
			case "run-task":
				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)
			case "finish-task":
				// 直接完成，不发送音频。
				finished := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-finished"},
				}
				fdata, _ := json.Marshal(finished)
				conn.Write(r.Context(), websocket.MessageText, fdata)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("key", WithDashScopeWSURL(wsURL))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := d.Synthesize(ctx, "hello", provider.TTSConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no audio produced")
}

func TestHandleTextMessage_InvalidJSON(t *testing.T) {
	d := NewDashScope("key", WithDashScopeLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	// 非法 JSON 不应终止循环。
	assert.False(t, d.handleTextMessage([]byte("not-json")))
}

func TestHandleTextMessage_Events(t *testing.T) {
	d := NewDashScope("key", WithDashScopeLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	tests := []struct {
		name     string
		event    string
		wantStop bool
	}{
		{"task-started 继续", "task-started", false},
		{"result-generated 继续", "result-generated", false},
		{"task-finished 停止", "task-finished", true},
		{"task-failed 停止", "task-failed", true},
		{"未知事件继续", "unknown-event", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := wsEnvelope{Header: wsHeader{Event: tt.event, TaskID: "t1"}}
			data, _ := json.Marshal(msg)
			assert.Equal(t, tt.wantStop, d.handleTextMessage(data))
		})
	}
}

func TestDashScope_SynthesizeStream_EmptyText(t *testing.T) {
	// 空文本应被跳过，不发送 continue-task。
	audioData := []byte("audio")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Header.Action {
			case "run-task":
				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)
			case "continue-task":
				// 如果收到空文本的 continue-task 则测试失败。
				assert.NotEmpty(t, msg.Payload.Input.Text, "不应发送空文本")
				conn.Write(r.Context(), websocket.MessageBinary, audioData)
			case "finish-task":
				finished := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-finished"},
				}
				fdata, _ := json.Marshal(finished)
				conn.Write(r.Context(), websocket.MessageText, fdata)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("key", WithDashScopeWSURL(wsURL))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 3)
	textCh <- ""     // 空文本，应跳过。
	textCh <- "real" // 有内容，应发送。
	textCh <- ""     // 空文本，应跳过。
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	var chunks int
	for range audioCh {
		chunks++
	}
	assert.Equal(t, 1, chunks, "只有一段非空文本对应音频")
}

func TestDashScope_SynthesizeStream_ServerError(t *testing.T) {
	// 服务端返回 task-failed 时应正常关闭。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Header.Action {
			case "run-task":
				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)
			case "continue-task":
				// 返回 task-failed 而非音频。
				failed := wsEnvelope{
					Header: wsHeader{
						TaskID:       msg.Header.TaskID,
						Event:        "task-failed",
						ErrorCode:    "QUOTA_EXCEEDED",
						ErrorMessage: "quota exceeded",
					},
				}
				fdata, _ := json.Marshal(failed)
				conn.Write(r.Context(), websocket.MessageText, fdata)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("key", WithDashScopeWSURL(wsURL))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 1)
	textCh <- "hello"

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	// audioCh 应最终关闭。
	for range audioCh {
	}
}

func TestDashScope_SynthesizeStream_SendContinueTaskError(t *testing.T) {
	// 服务端在收到 continue-task 前关闭连接，触发 sendContinueTask write 错误。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Header.Action == "run-task" {
				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)
				// run-task 后立即关闭，使 continue-task 写入失败。
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("key", WithDashScopeWSURL(wsURL),
		WithDashScopeLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 1)
	textCh <- "hello"

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	// audioCh 应最终关闭。
	for range audioCh {
	}
}

func TestDashScope_Warmup(t *testing.T) {
	// 模拟 WebSocket 服务端，接受连接后等待关闭。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// 等待客户端关闭。
		conn.Read(r.Context())
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key", WithDashScopeWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := d.Warmup(ctx)
	assert.NoError(t, err)
}

func TestDashScope_Warmup_DialError(t *testing.T) {
	d := NewDashScope("key", WithDashScopeWSURL("ws://127.0.0.1:1"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := d.Warmup(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warmup")
}

func TestDashScope_SynthesizeStream_WithPool(t *testing.T) {
	audioData := []byte("pool-audio")
	srv := mockTTSServer(t, audioData)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key",
		WithDashScopeWSURL(wsURL),
		WithDashScopePoolSize(2),
	)
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 预热填充池。
	require.NoError(t, d.Warmup(ctx))
	require.Equal(t, 2, d.pool.Len(), "预热后池中应有 2 个连接")

	// 第一次合成使用池中连接。
	textCh := make(chan string, 1)
	textCh <- "Hello"
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	var chunks int
	for range audioCh {
		chunks++
	}
	assert.GreaterOrEqual(t, chunks, 1)
}

func TestDashScope_Close(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Read(r.Context())
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key",
		WithDashScopeWSURL(wsURL),
		WithDashScopePoolSize(2),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, d.Warmup(ctx))
	assert.Equal(t, 2, d.pool.Len())

	// Close 应清理池中连接。
	require.NoError(t, d.Close())
	assert.Equal(t, 0, d.pool.Len())
}

func TestDashScope_Close_NoPool(t *testing.T) {
	// 未启用连接池时 Close 不应 panic。
	d := NewDashScope("test-key")
	assert.NoError(t, d.Close())
}

func TestDashScope_Warmup_WithPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Read(r.Context())
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key",
		WithDashScopeWSURL(wsURL),
		WithDashScopePoolSize(3),
	)
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, d.Warmup(ctx))
	assert.Equal(t, 3, d.pool.Len(), "预热后池中应有 3 个连接")
}

func TestWithDashScopePoolSize(t *testing.T) {
	d := NewDashScope("key", WithDashScopePoolSize(5))
	require.NotNil(t, d.pool, "poolSize > 0 时应创建连接池")
	assert.Equal(t, 5, cap(d.pool.conns))
	d.Close()
}

func TestWithDashScopePoolSize_Zero(t *testing.T) {
	d := NewDashScope("key", WithDashScopePoolSize(0))
	assert.Nil(t, d.pool, "poolSize = 0 时不应创建连接池")
}

func TestDashScope_SynthesizeStream_PoolEmptyFallback(t *testing.T) {
	// 启用连接池但池为空时，应回退新建连接正常合成。
	audioData := []byte("fallback-audio")
	srv := mockTTSServer(t, audioData)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key",
		WithDashScopeWSURL(wsURL),
		WithDashScopePoolSize(2),
	)
	defer d.Close()

	// 不预热，池为空。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 1)
	textCh <- "Hello"
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	var chunks int
	for range audioCh {
		chunks++
	}
	assert.GreaterOrEqual(t, chunks, 1, "池为空时应回退新建连接正常合成")
}

func TestDashScope_SynthesizeStream_DefaultConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg wsEnvelope
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			switch msg.Header.Action {
			case "run-task":
				// 验证默认参数。
				assert.Equal(t, "cosyvoice-v3-flash", msg.Payload.Model)
				assert.NotNil(t, msg.Payload.Parameters)
				assert.Equal(t, "longanyang", msg.Payload.Parameters.Voice)
				assert.Equal(t, 16000, msg.Payload.Parameters.SampleRate)

				started := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-started"},
				}
				sdata, _ := json.Marshal(started)
				conn.Write(r.Context(), websocket.MessageText, sdata)

			case "continue-task":
				conn.Write(r.Context(), websocket.MessageBinary, []byte("audio"))

			case "finish-task":
				finished := wsEnvelope{
					Header: wsHeader{TaskID: msg.Header.TaskID, Event: "task-finished"},
				}
				fdata, _ := json.Marshal(finished)
				conn.Write(r.Context(), websocket.MessageText, fdata)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	d := NewDashScope("test-key", WithDashScopeWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	textCh := make(chan string, 1)
	textCh <- "Test"
	close(textCh)

	audioCh, err := d.SynthesizeStream(ctx, textCh, provider.TTSConfig{})
	require.NoError(t, err)

	for range audioCh {
		// 消费完毕。
	}
}
