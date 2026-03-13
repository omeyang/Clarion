package ebpf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
	"unsafe"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

// tcpEvent 是内核 BPF 程序输出的 TCP 事件。
// 与 BPF C 程序中的 struct tcp_event 布局一致。
type tcpEvent struct {
	SAddr   [4]byte // 源 IPv4 地址。
	DAddr   [4]byte // 目标 IPv4 地址。
	SPort   uint16  // 源端口。
	DPort   uint16  // 目标端口。
	SRtt    uint32  // 平滑 RTT（微秒）。
	Retrans uint32  // 累计重传次数。
}

// tcpStats 汇聚单个目标 IP 的 TCP 质量指标。
type tcpStats struct {
	rttSamples []float64 // RTT 采样值（毫秒）。
	retrans    int       // 累计重传次数。
}

// TCPTracer 通过 tracepoint 追踪 TCP 连接延迟和重传。
type TCPTracer struct {
	logger *slog.Logger

	mu    sync.Mutex
	stats map[string]*tcpStats // key: 目标 IP。

	eventsMap *ciliumebpf.Map
	prog      *ciliumebpf.Program
	tpLink    link.Link
	reader    *ringbuf.Reader
	stopCh    chan struct{}
	stopped   chan struct{}
}

// newTCPTracer 创建 TCP 延迟追踪器。
func newTCPTracer(logger *slog.Logger) *TCPTracer {
	return &TCPTracer{
		logger:  logger,
		stats:   make(map[string]*tcpStats),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Name 返回探针名称。
func (t *TCPTracer) Name() string { return "tcp_tracer" }

// Start 加载 BPF 程序并挂载 tcp:tcp_probe tracepoint。
func (t *TCPTracer) Start(ctx context.Context) error {
	// 创建 ringbuf map 用于内核→用户空间数据传输。
	var err error
	t.eventsMap, err = ciliumebpf.NewMap(&ciliumebpf.MapSpec{
		Type:       ciliumebpf.RingBuf,
		MaxEntries: 1 << 16, // 64KB
	})
	if err != nil {
		return fmt.Errorf("tcptracer: 创建 ringbuf: %w", err)
	}

	// 创建 BPF 程序。
	// 最小的 tcp_probe handler：占位实现，返回 0。
	// 生产环境应使用 bpf2go 从 .bpf.c 编译生成完整的 TCP 字段读取逻辑。
	spec := &ciliumebpf.ProgramSpec{
		Name:         "tcp_probe_handler",
		Type:         ciliumebpf.TracePoint,
		License:      "GPL",
		Instructions: buildTCPProbeInstructions(),
	}

	t.prog, err = ciliumebpf.NewProgram(spec)
	if err != nil {
		_ = t.eventsMap.Close()
		return fmt.Errorf("tcptracer: 加载 BPF 程序: %w", err)
	}

	// 挂载到 tcp:tcp_probe tracepoint。
	t.tpLink, err = link.Tracepoint("tcp", "tcp_probe", t.prog, nil)
	if err != nil {
		_ = t.prog.Close()
		_ = t.eventsMap.Close()
		return fmt.Errorf("tcptracer: 挂载 tracepoint: %w", err)
	}

	// 创建 ringbuf reader。
	t.reader, err = ringbuf.NewReader(t.eventsMap)
	if err != nil {
		_ = t.tpLink.Close()
		_ = t.prog.Close()
		_ = t.eventsMap.Close()
		return fmt.Errorf("tcptracer: 创建 ringbuf reader: %w", err)
	}

	go t.readLoop()

	t.logger.Info("tcptracer: TCP 探针已启动")
	return nil
}

// Stop 卸载 BPF 程序并释放资源。
func (t *TCPTracer) Stop() error {
	close(t.stopCh)

	var errs []error

	if t.reader != nil {
		if err := t.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 ringbuf reader: %w", err))
		}
	}

	// 等待 readLoop 退出。
	<-t.stopped

	if t.tpLink != nil {
		if err := t.tpLink.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 tracepoint link: %w", err))
		}
	}
	if t.prog != nil {
		if err := t.prog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 BPF 程序: %w", err))
		}
	}
	if t.eventsMap != nil {
		if err := t.eventsMap.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 ringbuf map: %w", err))
		}
	}

	t.logger.Info("tcptracer: TCP 探针已停止")
	return errors.Join(errs...)
}

// Snapshot 返回各目标 IP 的 RTT P50/P95/P99 和重传统计。
func (t *TCPTracer) Snapshot() map[string]TCPSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string]TCPSnapshot, len(t.stats))
	for ip, s := range t.stats {
		if len(s.rttSamples) == 0 {
			continue
		}
		result[ip] = TCPSnapshot{
			RTTP50Ms:  percentile(s.rttSamples, 0.50),
			RTTP95Ms:  percentile(s.rttSamples, 0.95),
			RTTP99Ms:  percentile(s.rttSamples, 0.99),
			Retrans:   s.retrans,
			SampleCnt: len(s.rttSamples),
		}
	}
	return result
}

// TCPSnapshot 是单个目标 IP 的 TCP 质量快照。
type TCPSnapshot struct {
	RTTP50Ms  float64 `json:"rtt_p50_ms"`
	RTTP95Ms  float64 `json:"rtt_p95_ms"`
	RTTP99Ms  float64 `json:"rtt_p99_ms"`
	Retrans   int     `json:"retrans"`
	SampleCnt int     `json:"sample_count"`
}

// readLoop 从 ringbuf 读取 TCP 事件并汇聚统计。
func (t *TCPTracer) readLoop() {
	defer close(t.stopped)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		record, err := t.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			t.logger.Warn("tcptracer: 读取事件失败", slog.String("error", err.Error()))
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if len(record.RawSample) < int(unsafe.Sizeof(tcpEvent{})) {
			continue
		}

		var evt tcpEvent
		evt.SAddr = [4]byte(record.RawSample[0:4])
		evt.DAddr = [4]byte(record.RawSample[4:8])
		evt.SPort = binary.LittleEndian.Uint16(record.RawSample[8:10])
		evt.DPort = binary.LittleEndian.Uint16(record.RawSample[10:12])
		evt.SRtt = binary.LittleEndian.Uint32(record.RawSample[12:16])
		evt.Retrans = binary.LittleEndian.Uint32(record.RawSample[16:20])

		t.recordEvent(evt)
	}
}

// recordEvent 将 TCP 事件汇入统计。
func (t *TCPTracer) recordEvent(evt tcpEvent) {
	dstIP := net.IP(evt.DAddr[:]).String()
	rttMs := float64(evt.SRtt) / 1000.0 // 微秒 → 毫秒。

	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.stats[dstIP]
	if s == nil {
		s = &tcpStats{}
		t.stats[dstIP] = s
	}

	// 滑动窗口保留最近 1000 个 RTT 样本。
	const maxSamples = 1000
	if len(s.rttSamples) >= maxSamples {
		s.rttSamples = s.rttSamples[1:]
	}
	s.rttSamples = append(s.rttSamples, rttMs)
	s.retrans = int(evt.Retrans)
}

// percentile 计算近似百分位值。
func percentile(samples []float64, p float64) float64 {
	n := len(samples)
	if n == 0 {
		return 0
	}

	sorted := make([]float64, n)
	copy(sorted, samples)
	sortFloat64s(sorted)

	idx := int(p * float64(n-1))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// sortFloat64s 对 float64 切片排序（插入排序，适用于滑动窗口规模）。
func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

// buildTCPProbeInstructions 生成 tcp_probe tracepoint 的最小 BPF 指令。
// 占位实现：直接返回 0。
// 生产环境应使用 bpf2go 从 .bpf.c 编译生成完整的 TCP 字段读取逻辑。
func buildTCPProbeInstructions() asm.Instructions {
	return asm.Instructions{
		asm.LoadImm(asm.R0, 0, asm.DWord),
		asm.Return(),
	}
}
