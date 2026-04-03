package call

import (
	"log/slog"
	"sync"
)

// eslDispatcher 从全局 ESL 事件通道读取事件，按 session_id 路由到各 Session 的专属通道。
// 无人认领的事件直接丢弃，确保全局通道永远不会堵塞。
type eslDispatcher struct {
	mu       sync.RWMutex
	sessions map[string]chan<- ESLEvent // sessionID → 专属事件通道
	logger   *slog.Logger
}

// newESLDispatcher 创建事件分发器。
func newESLDispatcher(logger *slog.Logger) *eslDispatcher {
	return &eslDispatcher{
		sessions: make(map[string]chan<- ESLEvent),
		logger:   logger,
	}
}

// register 注册 Session 的事件通道。返回只读通道供 Session 消费。
func (d *eslDispatcher) register(sessionID string) <-chan ESLEvent {
	ch := make(chan ESLEvent, 64)
	d.mu.Lock()
	d.sessions[sessionID] = ch
	d.mu.Unlock()
	return ch
}

// unregister 注销 Session 并关闭其事件通道。
func (d *eslDispatcher) unregister(sessionID string) {
	d.mu.Lock()
	if ch, ok := d.sessions[sessionID]; ok {
		close(ch)
		delete(d.sessions, sessionID)
	}
	d.mu.Unlock()
}

// run 持续从 ESL 事件通道消费并分发。阻塞直到 eslEvents 关闭。
func (d *eslDispatcher) run(eslEvents <-chan ESLEvent) {
	for event := range eslEvents {
		sessionID := event.Header("variable_clarion_session_id")

		if sessionID != "" {
			// 有 session_id：精确路由到对应 Session。
			d.mu.RLock()
			ch, ok := d.sessions[sessionID]
			d.mu.RUnlock()
			if ok {
				d.trySend(ch, sessionID, event)
			}
			continue
		}

		// 无 session_id（如 BACKGROUND_JOB、早期 CHANNEL 事件）：广播给所有 Session。
		// 每个 Session 内部的 isMyESLEvent 会做二次过滤。
		d.mu.RLock()
		for sid, ch := range d.sessions {
			d.trySend(ch, sid, event)
		}
		d.mu.RUnlock()
	}
}

// trySend 非阻塞发送事件到 Session 通道。
func (d *eslDispatcher) trySend(ch chan<- ESLEvent, sessionID string, event ESLEvent) {
	select {
	case ch <- event:
	default:
		d.logger.Warn("session ESL event channel full, dropping",
			slog.String("session_id", sessionID),
			slog.String("event", event.Name))
	}
}
