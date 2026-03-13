// Package ebpf 提供基于 eBPF 的内核级观测能力。
//
// 通过 tracepoint 和 uprobe 采集 TCP 延迟、Go 调度延迟等内核层指标，
// 与应用层 pprof/benchmark 工具形成互补，覆盖 Go 运行时看不到的问题。
//
// 本包为可选功能，通过配置 [observe.ebpf] 控制开关。
// 需要 Linux kernel ≥ 5.8（BTF 支持）和 CAP_BPF + CAP_PERFMON 权限。
package ebpf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
)

// Config 是 eBPF 观测的配置。
type Config struct {
	// Enabled 是否启用 eBPF 观测（默认 false，需要 CAP_BPF）。
	Enabled bool `toml:"enabled" koanf:"enabled"`
	// TCPTrace 是否启用 TCP 延迟追踪。
	TCPTrace bool `toml:"tcp_trace" koanf:"tcp_trace"`
	// SchedLatency 是否启用 Go 调度延迟观测（实验性）。
	SchedLatency bool `toml:"sched_latency" koanf:"sched_latency"`
}

// ProbeManager 管理 eBPF 程序的加载和卸载生命周期。
type ProbeManager struct {
	cfg    Config
	logger *slog.Logger
	probes []probe
}

// probe 是单个 eBPF 探针的接口。
type probe interface {
	Start(ctx context.Context) error
	Stop() error
	Name() string
}

// NewProbeManager 创建 eBPF 探针管理器。
// 如果内核不支持 BTF 或权限不足，返回错误。
func NewProbeManager(cfg Config, logger *slog.Logger) (*ProbeManager, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	// 检测内核 BTF 支持。
	if !hasBTFSupport() {
		return nil, errors.New("ebpf: 内核不支持 BTF（需要 Linux ≥ 5.8）")
	}

	// 检测 BPF 权限。
	if !hasBPFPermission() {
		return nil, errors.New("ebpf: 权限不足（需要 CAP_BPF + CAP_PERFMON 或 root）")
	}

	pm := &ProbeManager{
		cfg:    cfg,
		logger: logger,
	}

	if cfg.TCPTrace {
		pm.probes = append(pm.probes, newTCPTracer(logger))
	}
	if cfg.SchedLatency {
		pm.probes = append(pm.probes, newSchedLatencyProbe(logger))
	}

	return pm, nil
}

// Start 加载并启动所有配置的 eBPF 探针。
func (pm *ProbeManager) Start(ctx context.Context) error {
	if pm == nil {
		return nil
	}

	var started []probe
	for _, p := range pm.probes {
		pm.logger.Info("ebpf: 启动探针", slog.String("name", p.Name()))
		if err := p.Start(ctx); err != nil {
			// 回滚已启动的探针。
			for _, s := range started {
				if stopErr := s.Stop(); stopErr != nil {
					pm.logger.Warn("ebpf: 回滚停止探针失败",
						slog.String("name", s.Name()),
						slog.String("error", stopErr.Error()))
				}
			}
			return fmt.Errorf("ebpf: 启动探针 %s 失败: %w", p.Name(), err)
		}
		started = append(started, p)
	}

	pm.logger.Info("ebpf: 所有探针已启动", slog.Int("count", len(pm.probes)))
	return nil
}

// Stop 卸载所有 eBPF 探针并释放资源。
func (pm *ProbeManager) Stop() error {
	if pm == nil {
		return nil
	}

	var errs []error
	for _, p := range pm.probes {
		pm.logger.Info("ebpf: 停止探针", slog.String("name", p.Name()))
		if err := p.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("停止探针 %s: %w", p.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// hasBTFSupport 检测内核是否支持 BTF。
func hasBTFSupport() bool {
	_, err := btf.LoadKernelSpec()
	return err == nil
}

// hasBPFPermission 检测当前进程是否有 BPF 权限。
// 尝试创建最小 BPF 程序来验证。
func hasBPFPermission() bool {
	// root 用户直接通过。
	if os.Geteuid() == 0 {
		return true
	}

	// 尝试创建最小的 BPF 程序来验证权限。
	spec := &ciliumebpf.ProgramSpec{
		Type: ciliumebpf.SocketFilter,
		Instructions: asm.Instructions{
			asm.LoadImm(asm.R0, 0, asm.DWord),
			asm.Return(),
		},
		License: "GPL",
	}
	prog, err := ciliumebpf.NewProgram(spec)
	if err != nil {
		return false
	}
	_ = prog.Close()
	return true
}
