package ebpf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

// SchedLatencyProbe 通过 uprobe 观测 Go 调度延迟。
// 挂载 runtime.execute 测量 goroutine 从可运行到实际运行的等待时间。
// 正常情况 < 100μs，> 1ms 说明 GOMAXPROCS 不够或 GC 压力大，
// 对实时音频帧处理（20ms 间隔）影响显著。
type SchedLatencyProbe struct {
	logger *slog.Logger

	mu      sync.Mutex
	samples []float64 // 调度延迟采样值（微秒）。
	jitters int       // 延迟 > 1ms 的次数。

	eventsMap *ciliumebpf.Map
	prog      *ciliumebpf.Program
	uprobe    link.Link
	reader    *ringbuf.Reader
	stopCh    chan struct{}
	stopped   chan struct{}
}

// newSchedLatencyProbe 创建 Go 调度延迟探针。
func newSchedLatencyProbe(logger *slog.Logger) *SchedLatencyProbe {
	return &SchedLatencyProbe{
		logger:  logger,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Name 返回探针名称。
func (s *SchedLatencyProbe) Name() string { return "sched_latency" }

// Start 加载 BPF 程序并挂载 uprobe。
func (s *SchedLatencyProbe) Start(ctx context.Context) error {
	var err error

	// 创建 ringbuf map。
	s.eventsMap, err = ciliumebpf.NewMap(&ciliumebpf.MapSpec{
		Type:       ciliumebpf.RingBuf,
		MaxEntries: 1 << 14, // 16KB
	})
	if err != nil {
		return fmt.Errorf("schedlat: 创建 ringbuf: %w", err)
	}

	// 占位 BPF 程序。
	// 生产环境应使用 bpf2go 生成挂载 runtime.execute 的 uprobe 程序，
	// 读取 goroutine 的 readyTime 字段计算调度延迟。
	spec := &ciliumebpf.ProgramSpec{
		Name:    "sched_lat_probe",
		Type:    ciliumebpf.Kprobe, // uprobe 与 kprobe 共用程序类型。
		License: "GPL",
		Instructions: asm.Instructions{
			asm.LoadImm(asm.R0, 0, asm.DWord),
			asm.Return(),
		},
	}

	s.prog, err = ciliumebpf.NewProgram(spec)
	if err != nil {
		_ = s.eventsMap.Close()
		return fmt.Errorf("schedlat: 加载 BPF 程序: %w", err)
	}

	// 挂载 uprobe 到当前进程的 runtime.execute。
	exe, err := link.OpenExecutable("/proc/self/exe")
	if err != nil {
		_ = s.prog.Close()
		_ = s.eventsMap.Close()
		return fmt.Errorf("schedlat: 打开可执行文件: %w", err)
	}

	s.uprobe, err = exe.Uprobe("runtime.execute", s.prog, nil)
	if err != nil {
		_ = s.prog.Close()
		_ = s.eventsMap.Close()
		return fmt.Errorf("schedlat: 挂载 uprobe: %w", err)
	}

	// 创建 ringbuf reader。
	s.reader, err = ringbuf.NewReader(s.eventsMap)
	if err != nil {
		_ = s.uprobe.Close()
		_ = s.prog.Close()
		_ = s.eventsMap.Close()
		return fmt.Errorf("schedlat: 创建 ringbuf reader: %w", err)
	}

	go s.readLoop()

	s.logger.Info("schedlat: Go 调度延迟探针已启动")
	return nil
}

// Stop 卸载探针并释放资源。
func (s *SchedLatencyProbe) Stop() error {
	close(s.stopCh)

	var errs []error
	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 reader: %w", err))
		}
	}
	<-s.stopped

	if s.uprobe != nil {
		if err := s.uprobe.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 uprobe: %w", err))
		}
	}
	if s.prog != nil {
		if err := s.prog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 BPF 程序: %w", err))
		}
	}
	if s.eventsMap != nil {
		if err := s.eventsMap.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 map: %w", err))
		}
	}

	s.logger.Info("schedlat: Go 调度延迟探针已停止")
	return errors.Join(errs...)
}

// Snapshot 返回调度延迟的统计快照。
func (s *SchedLatencyProbe) Snapshot() SchedSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.samples)
	if n == 0 {
		return SchedSnapshot{}
	}

	return SchedSnapshot{
		P50Us:     percentile(s.samples, 0.50),
		P95Us:     percentile(s.samples, 0.95),
		P99Us:     percentile(s.samples, 0.99),
		Jitters:   s.jitters,
		SampleCnt: n,
	}
}

// SchedSnapshot 是调度延迟统计快照。
type SchedSnapshot struct {
	P50Us     float64 `json:"p50_us"`       // P50 调度延迟（微秒）。
	P95Us     float64 `json:"p95_us"`       // P95 调度延迟（微秒）。
	P99Us     float64 `json:"p99_us"`       // P99 调度延迟（微秒）。
	Jitters   int     `json:"jitters"`      // 延迟 > 1ms 的次数。
	SampleCnt int     `json:"sample_count"` // 采样总数。
}

// readLoop 从 ringbuf 读取调度延迟事件。
func (s *SchedLatencyProbe) readLoop() {
	defer close(s.stopped)

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		record, err := s.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			s.logger.Warn("schedlat: 读取事件失败", slog.String("error", err.Error()))
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 事件格式：uint64 延迟纳秒。
		if len(record.RawSample) < 8 {
			continue
		}

		latNs := leUint64(record.RawSample[:8])
		latUs := float64(latNs) / 1000.0

		s.mu.Lock()
		const maxSamples = 1000
		if len(s.samples) >= maxSamples {
			s.samples = s.samples[1:]
		}
		s.samples = append(s.samples, latUs)
		if latUs > 1000 { // > 1ms
			s.jitters++
		}
		s.mu.Unlock()
	}
}

// leUint64 从小端字节中读取 uint64。
func leUint64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
