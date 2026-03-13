package call

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
)

func TestWorker_SetProviders(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	require.Nil(t, w.asr)
	require.Nil(t, w.llm)
	require.Nil(t, w.tts)

	w.SetProviders(nil, nil, nil)

	// SetProviders 应不 panic，即使传入 nil。
	assert.Nil(t, w.asr)
}

func TestWorker_HandleAudioWS_MissingSessionID(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/audio", nil)
	rec := httptest.NewRecorder()

	w.handleAudioWS(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing session_id")
}

func TestWorker_HandleAudioWS_SessionNotFound(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/audio?session_id=nonexistent", nil)
	rec := httptest.NewRecorder()

	w.handleAudioWS(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "session not found")
}

func TestWorker_HandleAudioWS_SessionExists(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	// 注册一个模拟 audioLink（handleAudioWS 查找 audioLinks 而非 sessions）。
	w.mu.Lock()
	w.audioLinks["test-session-ws"] = &audioLink{
		in:  make(chan []byte, 1),
		out: make(chan []byte, 1),
	}
	w.mu.Unlock()

	// 使用 httptest.Server 进行真实 WebSocket 升级测试。
	srv := httptest.NewServer(http.HandlerFunc(w.handleAudioWS))
	defer srv.Close()

	// 构造 WebSocket URL。
	wsURL := "ws" + srv.URL[4:] + "?session_id=test-session-ws"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err, "WebSocket 连接应成功")
	defer conn.CloseNow()

	// 验证连接可正常关闭。
	conn.Close(websocket.StatusNormalClosure, "test done")
}

func TestWorker_StartWSServer(t *testing.T) {
	cfg := config.Defaults()
	cfg.FreeSWITCH.AudioWSAddr = "127.0.0.1:0"
	w := NewWorker(cfg, nil, testLogger())

	// startWSServer 不应 panic。
	w.startWSServer()

	// 清理。
	if w.wsServer != nil {
		_ = w.wsServer.Close()
	}
}

func TestWorker_ShutdownWSServer_Nil(t *testing.T) {
	cfg := defaultWorkerConfig()
	w := NewWorker(cfg, nil, testLogger())

	// wsServer 为 nil 时不应 panic。
	w.shutdownWSServer()
}
