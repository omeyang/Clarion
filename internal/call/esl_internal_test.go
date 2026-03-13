package call

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/config"
)

// fakeESL 是模拟 FreeSWITCH ESL 的 TCP 服务器。
type fakeESL struct {
	listener net.Listener
	handler  func(conn net.Conn)
}

func newFakeESL(t *testing.T, handler func(conn net.Conn)) *fakeESL {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeESL{listener: ln, handler: handler}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handler(conn)
		}
	}()
	return f
}

func (f *fakeESL) addr() string {
	return f.listener.Addr().String()
}

func (f *fakeESL) close() {
	_ = f.listener.Close()
}

func eslAddr(addr string) (string, int) {
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}

// defaultFakeHandler 模拟 FreeSWITCH 的标准认证流程。
func defaultFakeHandler(password string) func(conn net.Conn) {
	return func(conn net.Conn) {
		defer conn.Close()

		_, _ = fmt.Fprintf(conn, "Content-Type: auth/request\n\n")

		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n')
		_, _ = reader.ReadString('\n')

		if strings.TrimSpace(line) == "auth "+password {
			_, _ = fmt.Fprintf(conn, "Reply-Text: +OK accepted\n\n")
		} else {
			_, _ = fmt.Fprintf(conn, "Reply-Text: -ERR invalid\n\n")
			return
		}

		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK event listener enabled plain\n\n")

		buf := make([]byte, 1)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
		}
	}
}

func TestESLClient_ConnectAndClose(t *testing.T) {
	fake := newFakeESL(t, defaultFakeHandler("TestPass"))
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "TestPass",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, client.Connect(ctx))

	// 关闭应成功。
	require.NoError(t, client.Close())
	// 重复关闭应幂等。
	require.NoError(t, client.Close())
}

func TestESLClient_ConnectAuthFailed(t *testing.T) {
	fake := newFakeESL(t, defaultFakeHandler("RightPassword"))
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "WrongPassword",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth failed")
}

func TestESLClient_ConnectDialError(t *testing.T) {
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: "127.0.0.1", ESLPort: 1, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial ESL")
}

func TestESLClient_ConnectReadAuthFailure(t *testing.T) {
	// 服务器立即关闭连接，模拟读取 auth/request 失败。
	fake := newFakeESL(t, func(conn net.Conn) {
		conn.Close()
	})
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read auth request")
}

func TestESLClient_ConnectSubscribeFailure(t *testing.T) {
	// 认证通过后关闭连接，模拟订阅事件失败。
	fake := newFakeESL(t, func(conn net.Conn) {
		defer conn.Close()
		_, _ = fmt.Fprintf(conn, "Content-Type: auth/request\n\n")
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK accepted\n\n")
		// 认证通过后立即关闭，导致订阅失败。
	})
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscribe read")
}

func TestESLClient_SendRaw_NotConnected(t *testing.T) {
	client := NewESLClient(config.FreeSWITCH{}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	_, err := client.sendRaw(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ESL not connected")
}

func TestESLClient_ReadLoop_Events(t *testing.T) {
	var connMu sync.Mutex
	var serverConn net.Conn

	handler := func(conn net.Conn) {
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()

		defer conn.Close()
		_, _ = fmt.Fprintf(conn, "Content-Type: auth/request\n\n")
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK accepted\n\n")
		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK event listener enabled plain\n\n")

		buf := make([]byte, 4096)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
		}
	}

	fake := newFakeESL(t, handler)
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	// 通过服务器端连接发送事件（仅 readLoop 在读取，无竞争）。
	connMu.Lock()
	eventBody := "Event-Name: CHANNEL_ANSWER\nUnique-ID: test-uuid-001\n"
	_, _ = fmt.Fprintf(serverConn, "Content-Type: text/event-plain\nContent-Length: %d\n\n%s",
		len(eventBody), eventBody)
	connMu.Unlock()

	select {
	case event := <-client.Events():
		assert.Equal(t, "CHANNEL_ANSWER", event.Name)
		assert.Equal(t, "test-uuid-001", event.UUID())
	case <-time.After(2 * time.Second):
		t.Fatal("未在超时内收到事件")
	}
}

func TestESLClient_ReadLoop_NonEventPlainSkipped(t *testing.T) {
	var connMu sync.Mutex
	var serverConn net.Conn

	handler := func(conn net.Conn) {
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()

		defer conn.Close()
		_, _ = fmt.Fprintf(conn, "Content-Type: auth/request\n\n")
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK accepted\n\n")
		_, _ = reader.ReadString('\n')
		_, _ = reader.ReadString('\n')
		_, _ = fmt.Fprintf(conn, "Reply-Text: +OK event listener enabled plain\n\n")

		buf := make([]byte, 4096)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
		}
	}

	fake := newFakeESL(t, handler)
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	connMu.Lock()
	// 发送非 event-plain 的内容（应被跳过）。
	_, _ = fmt.Fprintf(serverConn, "Content-Type: api/response\n\n")
	// 然后发送真实事件。
	eventBody := "Event-Name: CHANNEL_HANGUP\nUnique-ID: skip-test\n"
	_, _ = fmt.Fprintf(serverConn, "Content-Type: text/event-plain\nContent-Length: %d\n\n%s",
		len(eventBody), eventBody)
	connMu.Unlock()

	select {
	case event := <-client.Events():
		assert.Equal(t, "CHANNEL_HANGUP", event.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("未在超时内收到事件")
	}
}

func TestESLClient_ReadLoop_CloseStopsLoop(t *testing.T) {
	fake := newFakeESL(t, defaultFakeHandler("x"))
	defer fake.close()

	host, port := eslAddr(fake.addr())
	client := NewESLClient(config.FreeSWITCH{
		ESLHost: host, ESLPort: port, ESLPassword: "x",
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	require.NoError(t, client.Connect(context.Background()))
	require.NoError(t, client.Close())

	// 等待 readLoop 退出并关闭事件通道。
	select {
	case _, ok := <-client.Events():
		if ok {
			<-client.Events()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("事件通道未在超时内关闭")
	}
}
