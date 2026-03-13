package asr

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

func TestNewQwen_Defaults(t *testing.T) {
	q := NewQwen("test-key")
	assert.Equal(t, "test-key", q.apiKey)
	assert.Equal(t, defaultDashScopeASRURL, q.wsURL)
	assert.NotNil(t, q.logger)
}

func TestNewQwen_Options(t *testing.T) {
	q := NewQwen("test-key", WithQwenWSURL("ws://localhost:9999"))
	assert.Equal(t, "ws://localhost:9999", q.wsURL)
}

func TestQwen_StartStreamAndReceiveEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("accept error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// Read session.update message.
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Logf("read session update: %v", err)
			return
		}
		var msg wsMessage
		require.NoError(t, json.Unmarshal(data, &msg))
		assert.Equal(t, "session.update", msg.Type)

		// Send a partial result.
		partial := wsMessage{
			Type: "conversation.item.input_audio_transcription.text",
			Text: "你",
		}
		pdata, _ := json.Marshal(partial)
		if err := conn.Write(r.Context(), websocket.MessageText, pdata); err != nil {
			return
		}

		// Send a final result.
		final := wsMessage{
			Type:       "conversation.item.input_audio_transcription.completed",
			Transcript: "你好",
			Confidence: 0.95,
		}
		fdata, _ := json.Marshal(final)
		if err := conn.Write(r.Context(), websocket.MessageText, fdata); err != nil {
			return
		}

		// Wait a bit then close.
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("test-key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{
		Model:      "qwen3-asr-flash-realtime",
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err)
	defer stream.Close()

	events := stream.Events()

	// Expect partial event.
	select {
	case evt := <-events:
		assert.Equal(t, "你", evt.Text)
		assert.False(t, evt.IsFinal)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for partial event")
	}

	// Expect final event.
	select {
	case evt := <-events:
		assert.Equal(t, "你好", evt.Text)
		assert.True(t, evt.IsFinal)
		assert.InDelta(t, 0.95, evt.Confidence, 0.01)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for final event")
	}
}

func TestQwen_FeedAudio(t *testing.T) {
	received := make(chan wsMessage, 10)

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
			var msg wsMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			received <- msg
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("test-key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{
		SampleRate: 16000,
		Language:   "zh",
	})
	require.NoError(t, err)
	defer stream.Close()

	// Wait for session.update to arrive.
	select {
	case msg := <-received:
		assert.Equal(t, "session.update", msg.Type)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for session update")
	}

	// Feed audio chunk.
	err = stream.FeedAudio(ctx, []byte{0x01, 0x02, 0x03})
	require.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, "input_audio_buffer.append", msg.Type)
		assert.NotEmpty(t, msg.Audio)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for audio message")
	}
}

func TestNewQwen_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQwen("key", WithQwenLogger(logger))
	assert.Equal(t, logger, q.logger)
}

func TestQwen_StartStream_DialFailure(t *testing.T) {
	// 连接一个不存在的地址，期望 dial 失败。
	q := NewQwen("key", WithQwenWSURL("ws://127.0.0.1:1"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := q.StartStream(ctx, provider.ASRConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qwen asr: dial websocket")
}

func TestQwen_StartStream_Defaults(t *testing.T) {
	// 验证 Model、Language、SampleRate 为空时使用默认值。
	msgCh := make(chan wsMessage, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var msg wsMessage
		_ = json.Unmarshal(data, &msg)
		msgCh <- msg

		// 保持连接直到客户端关闭。
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{})
	require.NoError(t, err)
	defer stream.Close()

	// 通过 channel 等待服务端收到消息，避免数据竞争。
	var receivedMsg wsMessage
	select {
	case receivedMsg = <-msgCh:
	case <-ctx.Done():
		t.Fatal("等待服务端消息超时")
	}

	assert.Equal(t, "session.update", receivedMsg.Type)
	assert.Equal(t, "zh", receivedMsg.Session.InputAudioTranscription.Language)
	assert.Equal(t, 16000, receivedMsg.Session.SampleRate)
}

func TestQwenStream_FeedAudio_AfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{Language: "zh"})
	require.NoError(t, err)

	require.NoError(t, stream.Close())

	err = stream.FeedAudio(ctx, []byte{0x01})
	require.Error(t, err)
	assert.Equal(t, "qwen asr: stream closed", err.Error())
}

func TestQwenStream_ReadLoop_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// 读取 session.update。
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}

		// 发送畸形 JSON，readLoop 应跳过并继续。
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("{invalid json"))

		// 发送有效的最终结果，验证 readLoop 继续工作。
		final := wsMessage{
			Type:       "conversation.item.input_audio_transcription.completed",
			Transcript: "恢复正常",
			Confidence: 0.9,
		}
		data, _ := json.Marshal(final)
		_ = conn.Write(r.Context(), websocket.MessageText, data)

		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{Language: "zh"})
	require.NoError(t, err)
	defer stream.Close()

	// 畸形 JSON 被跳过后应收到正常事件。
	select {
	case evt := <-stream.Events():
		assert.Equal(t, "恢复正常", evt.Text)
		assert.True(t, evt.IsFinal)
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待事件")
	}
}

func TestQwenStream_ReadLoop_AllEventTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// 读取 session.update。
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}

		// 依次发送各种事件类型，覆盖 readLoop 中所有 switch 分支。
		messages := []wsMessage{
			{Type: "session.created"},
			{Type: "input_audio_buffer.speech_started"},
			{Type: "input_audio_buffer.speech_stopped"},
			{Type: "error"},
			// 空文本的转录事件（不应产生 ASREvent）。
			{Type: "conversation.item.input_audio_transcription.text", Text: ""},
			{Type: "conversation.item.input_audio_transcription.completed", Transcript: ""},
			// 有文本的最终结果（产生 ASREvent）。
			{Type: "conversation.item.input_audio_transcription.completed", Transcript: "测试完成", Confidence: 0.88},
		}

		for _, msg := range messages {
			data, _ := json.Marshal(msg)
			if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{Language: "zh"})
	require.NoError(t, err)
	defer stream.Close()

	// session.created、speech_started/stopped、error 不产生 ASREvent。
	// 空文本的转录事件也不产生 ASREvent。
	// 只有最后一个有文本的 completed 事件应被接收到。
	select {
	case evt := <-stream.Events():
		assert.Equal(t, "测试完成", evt.Text)
		assert.True(t, evt.IsFinal)
		assert.InDelta(t, 0.88, evt.Confidence, 0.01)
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待事件")
	}
}

func TestQwenStream_ReadLoop_UnexpectedClose(t *testing.T) {
	// 服务端在发送一条消息后意外关闭，验证 readLoop 的读取错误处理。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		// 读取 session.update。
		if _, _, err := conn.Read(r.Context()); err != nil {
			conn.Close(websocket.StatusInternalError, "")
			return
		}

		// 意外关闭连接，触发 readLoop 的读取错误路径。
		conn.Close(websocket.StatusInternalError, "unexpected")
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{Language: "zh"})
	require.NoError(t, err)
	defer stream.Close()

	// events 通道应在 readLoop 退出后被关闭。
	select {
	case _, ok := <-stream.Events():
		assert.False(t, ok, "events 通道应已关闭")
	case <-time.After(3 * time.Second):
		t.Fatal("超时等待 events 通道关闭")
	}
}

func TestQwenStream_CloseIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// Read session.update then continuously read (will see the close frame).
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	q := NewQwen("test-key", WithQwenWSURL(wsURL))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.StartStream(ctx, provider.ASRConfig{Language: "zh"})
	require.NoError(t, err)

	// Close twice should not panic.
	err = stream.Close()
	assert.NoError(t, err)
	err = stream.Close()
	assert.NoError(t, err)
}
