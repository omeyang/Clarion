package tts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoWSServer 创建一个简单的 WebSocket 服务端，接受连接后等待关闭。
func echoWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// 等待客户端关闭。
		conn.Read(r.Context())
	}))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testDialFn 返回一个连接到指定 WebSocket URL 的 dialFunc。
func testDialFn(wsURL string) dialFunc {
	return func(ctx context.Context) (*websocket.Conn, error) {
		conn, resp, err := websocket.Dial(ctx, wsURL, nil)
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("test dial: %w", err)
		}
		return conn, nil
	}
}

func TestWSPool_FillAndGet(t *testing.T) {
	srv := echoWSServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := newWSPool(testDialFn(wsURL), 3, discardLogger())
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool.Fill(ctx)
	assert.Equal(t, 3, pool.Len(), "填充后池中应有 3 个连接")

	// 逐个取出。
	conn1 := pool.Get()
	require.NotNil(t, conn1, "第 1 个连接不应为 nil")
	assert.Equal(t, 2, pool.Len())

	conn2 := pool.Get()
	require.NotNil(t, conn2, "第 2 个连接不应为 nil")
	assert.Equal(t, 1, pool.Len())

	conn3 := pool.Get()
	require.NotNil(t, conn3, "第 3 个连接不应为 nil")
	assert.Equal(t, 0, pool.Len())

	// 池空时返回 nil。
	assert.Nil(t, pool.Get(), "池空时应返回 nil")

	// 清理连接。
	conn1.Close(websocket.StatusNormalClosure, "test")
	conn2.Close(websocket.StatusNormalClosure, "test")
	conn3.Close(websocket.StatusNormalClosure, "test")
}

func TestWSPool_GetEmpty(t *testing.T) {
	failDial := func(_ context.Context) (*websocket.Conn, error) {
		return nil, errors.New("should not dial")
	}
	pool := newWSPool(failDial, 2, discardLogger())
	defer pool.Close()

	assert.Nil(t, pool.Get(), "空池应返回 nil")
}

func TestWSPool_Replenish(t *testing.T) {
	srv := echoWSServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := newWSPool(testDialFn(wsURL), 2, discardLogger())
	defer pool.Close()

	pool.Replenish()

	// 等待异步补充完成。
	var conn *websocket.Conn
	require.Eventually(t, func() bool {
		conn = pool.Get()
		return conn != nil
	}, 3*time.Second, 50*time.Millisecond, "补充后应有可用连接")

	conn.Close(websocket.StatusNormalClosure, "test")
}

func TestWSPool_ReplenishAfterClose(t *testing.T) {
	srv := echoWSServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := newWSPool(testDialFn(wsURL), 2, discardLogger())

	pool.Close()
	// Close 后补充不应 panic。
	pool.Replenish()
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, pool.Len())
}

func TestWSPool_Close(t *testing.T) {
	srv := echoWSServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := newWSPool(testDialFn(wsURL), 2, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool.Fill(ctx)
	assert.Equal(t, 2, pool.Len())

	pool.Close()
	assert.Equal(t, 0, pool.Len())

	// 重复 Close 不应 panic。
	pool.Close()
}

func TestWSPool_DialError(t *testing.T) {
	failDial := func(ctx context.Context) (*websocket.Conn, error) {
		return nil, errors.New("dial failed")
	}
	pool := newWSPool(failDial, 2, discardLogger())
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 填充失败后池应为空。
	pool.Fill(ctx)
	assert.Equal(t, 0, pool.Len())
}

func TestWSPool_DefaultSize(t *testing.T) {
	failDial := func(_ context.Context) (*websocket.Conn, error) {
		return nil, errors.New("should not dial")
	}
	pool := newWSPool(failDial, 0, discardLogger())
	defer pool.Close()

	// size <= 0 时默认为 2。
	assert.Equal(t, 2, cap(pool.conns))
}
