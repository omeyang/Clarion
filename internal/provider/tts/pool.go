package tts

import (
	"context"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
)

// dialFunc 是建立 WebSocket 连接的函数签名。
// 由 DashScope.dialWS 提供，避免 pool 与 dial 逻辑耦合。
type dialFunc func(ctx context.Context) (*websocket.Conn, error)

// wsPool 管理预建的 WebSocket 连接，减少每次合成的建连延迟（约 100ms）。
// 池中连接已完成 DNS/TCP/TLS 握手，取出后可直接发送 run-task 开始合成。
type wsPool struct {
	dialFn dialFunc
	logger *slog.Logger
	conns  chan *websocket.Conn

	once sync.Once
	done chan struct{}
}

// newWSPool 创建指定容量的 WebSocket 连接池。
// dialFn 由调用方提供，负责建立新的 WebSocket 连接。
func newWSPool(dialFn dialFunc, size int, logger *slog.Logger) *wsPool {
	if size <= 0 {
		size = 2
	}
	return &wsPool{
		dialFn: dialFn,
		logger: logger,
		conns:  make(chan *websocket.Conn, size),
		done:   make(chan struct{}),
	}
}

// Get 从池中获取预建连接。池为空时返回 nil，调用方应自行建连。
func (p *wsPool) Get() *websocket.Conn {
	select {
	case conn := <-p.conns:
		return conn
	default:
		return nil
	}
}

// Replenish 异步补充一个连接到池中。
// 连接用完后调用此方法，确保池中始终有可用连接。
func (p *wsPool) Replenish() {
	go func() {
		select {
		case <-p.done:
			return
		default:
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// 池已关闭时取消建连。
		go func() {
			select {
			case <-p.done:
				cancel()
			case <-ctx.Done():
			}
		}()

		conn, err := p.dialFn(ctx)
		if err != nil {
			p.logger.Debug("wsPool: 补充连接失败", "error", err)
			return
		}

		select {
		case p.conns <- conn:
		case <-p.done:
			p.closeConn(conn, "pool closed")
		default:
			// 池已满，关闭多余连接。
			p.closeConn(conn, "pool full")
		}
	}()
}

// Fill 同步填充连接池到容量上限。
func (p *wsPool) Fill(ctx context.Context) {
	cap := cap(p.conns)
	for range cap {
		conn, err := p.dialFn(ctx)
		if err != nil {
			p.logger.Warn("wsPool: 预填充连接失败", "error", err)
			continue
		}
		select {
		case p.conns <- conn:
		default:
			p.closeConn(conn, "pool full")
			return
		}
	}
}

// Len 返回池中当前空闲连接数。
func (p *wsPool) Len() int {
	return len(p.conns)
}

// Close 关闭池中所有空闲连接并阻止后续补充。
func (p *wsPool) Close() {
	p.once.Do(func() {
		close(p.done)
		for {
			select {
			case conn := <-p.conns:
				p.closeConn(conn, "pool closing")
			default:
				return
			}
		}
	})
}

// closeConn 关闭连接并记录错误日志。
func (p *wsPool) closeConn(conn *websocket.Conn, reason string) {
	if err := conn.Close(websocket.StatusNormalClosure, reason); err != nil {
		p.logger.Debug("wsPool: close conn", "reason", reason, "error", err)
	}
}
