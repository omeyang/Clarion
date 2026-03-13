# 架构设计总纲

本文档是 Clarion 系统的顶层设计纲要。**第 2 章「工程原则」是整个项目的灵魂**，所有后续章节的技术决策都必须回溯到这些原则。

---

## 1. 为什么选择 Go

### 1.1 核心优势

| 维度 | Go 方案 |
|------|---------|
| **并发模型** | goroutine + channel 天然匹配实时音频流水线，无需 async/await 关键字污染 |
| **部署** | 单二进制（~15MB），静态链接，交叉编译，Docker 镜像 < 30MB |
| **性能** | 无 GIL，真正的多核并行；goroutine 调度开销 ~2KB |
| **类型安全** | 编译期接口检查，无运行时类型错误 |
| **生态成熟** | net/http、pgx/v5、go-redis/v9 均为生产级库 |
| **工具链统一** | go test / go vet / golangci-lint / pprof 一站式 |

### 1.2 为什么不是 Rust / Zig

| 语言 | 不选的理由 |
|------|-----------|
| **Rust** | 学习曲线陡峭（借用检查器），开发速度慢 2-3 倍，AI/云 SDK 生态不如 Go 完善 |
| **Zig** | 太新，生态不成熟，生产环境案例不足 |

Go 在**开发效率**与**运行时性能**之间取得了最佳平衡，且有大量实时音视频系统（LiveKit、Pion WebRTC）的成功案例。

---

## 2. 工程原则

> **这些原则是项目的最高纲领。每一行代码、每一个技术决策、每一次 code review 都以此为标尺。**

### 2.1 实时优先（Realtime-First）

- 所有架构决策在「实时通话体验」和「系统复杂度」之间发生冲突时，**优先保障实时体验**。
- 端到端延迟目标 **< 1.2s**（用户说完到 AI 开始播放回复）。
- 延迟预算明确分配：ASR 识别延迟、LLM 首 token 延迟、TTS 首包延迟，各环节有独立指标和熔断阈值。
- 非实时需求（报表、分析、通知）不得占用通话链路资源。
- 热路径零分配：音频帧处理、状态机转移等热路径代码尽量避免堆分配（`sync.Pool` / 栈分配）。
- 预合成音频（`internal/precompile/`）：开场白、确认词、结束语预合成为音频文件，消除 TTS 延迟。
- 推测执行（`session_speculative.go`）：ASR 中间结果稳定时提前启动 LLM 推理，命中时节省一轮延迟。

### 2.2 流式贯穿（Streaming-Through）

- ASR 输出实时文本流 → LLM 接收文本并流式生成回复 → TTS 接收文本流并流式合成音频。
- 各环节之间通过 Go channel 流水线串联，不等待完整结果。
- **句级分割**：LLM 流式输出按句子切分送入 TTS，而非等待整段回复完成，降低首包延迟。
- **填充音频**（`session_filler.go`）：在 LLM 思考期间播放自然过渡音（如"嗯"、"好的"），避免死寂。
- 批处理模式仅用于通话结束后的摘要生成、数据落库等后处理任务。
- **双管线模式**：
  - Classic：ASR（通义听悟）→ LLM（DeepSeek）→ TTS（CosyVoice），channel 串联。
  - Hybrid（`session_hybrid.go`）：Qwen3-Omni-Flash-Realtime 端到端语音 + Smart LLM 异步分析，低延迟与高质量兼顾。

### 2.3 热冷分离（Hot-Cold Separation）

- **热路径**（通话进行中）：只包含 ASR → LLM → TTS 核心对话循环，以及必要的状态维护。
- **冷路径**（通话结束后）：摘要生成、商机提取、PostgreSQL 落库、企业微信通知、录音归档等操作异步执行。
- 冷路径通过 Redis Stream 事件驱动，由独立的 Post-Processing Worker（`cmd/postprocessor/`）消费。
- 热路径中的任何操作都必须在毫秒级完成，不允许阻塞式 I/O 或重计算。
- 冷路径任务失败不影响通话体验，可重试。

### 2.4 故障友好（Fault-Tolerant）

- 任何 AI 组件超时或失败不应导致通话卡死，必须有降级路径。
- **熔断器**（`resilience.Breaker`）：三态（Closed → Open → HalfOpen），每个 Provider 独立熔断。连续 N 次失败后断路，冷却期后探测恢复。ASR 有专用熔断器。
- **降级链**：
  - ASR 超时 → 播放"没听清，请再说一次"
  - LLM 超时 → 规则引擎兜底回复（`internal/engine/rules/`）
  - TTS 超时 → 预合成音频降级（`internal/precompile/`）
  - 连续 N 次错误 → 礼貌挂断
- **预算降级**（`guard.CallBudget`）：Token 消耗或通话时长接近预算上限时，主动缩短回复、跳过非必要阶段。
- **填充音频兜底**（`session_filler.go`）：LLM 延迟过高时自动播放过渡音，维持通话自然感。
- **重试与降级**（`resilience.Retry` + `resilience.Fallback`）：可重试的临时错误自动重试（指数退避），不可重试的永久错误走降级链。
- **会话快照恢复**（`call/snapshot.go`）：通话中间状态可序列化，意外中断后支持恢复。
- **优雅关闭**：收到 SIGTERM 后停止接新任务，等待在途通话完成（最长 `max_duration_sec`），然后退出。

### 2.5 幂等安全（Idempotent-Safe）

- 同一号码在同一任务中不会被重复呼叫，除非显式标记为「需要重试」。
- 通知推送带唯一标识，接收端可去重，避免同一通话结果重复通知。
- 后处理写入（turns / events / call record）全部幂等，重复消费不产生副作用。
- 任务调度器（`internal/scheduler/`）基于 Asynq（Redis-based）实现，具备去重和防重机制。
- 重试调度（`scheduler/retrysched.go`）对失败通话按策略重新入队，不会重复已完成的任务。

### 2.6 接口抽象（Interface-Oriented）

- **所有模块间交互通过 Go interface 定义**，不直接依赖具体实现。
- 核心接口定义在 Sonata 引擎库（`engine/aiface`）中：
  - `aiface.ASRProvider` — 创建 ASR 流式识别会话
  - `aiface.LLMProvider` — 流式文本生成
  - `aiface.TTSProvider` — 流式语音合成
  - `aiface.DialogEngine` — 业务对话逻辑注入
  - `engine.Transport` — 音频 I/O 抽象
- Clarion 中通过**类型别名**引用 Sonata 接口（`internal/provider/asr.go`、`llm.go`、`tts.go`），确保类型一致性，同时实现在子包中：
  - `provider/asr/qwen.go` — 通义听悟 WebSocket 流式 ASR
  - `provider/llm/deepseek.go` — DeepSeek SSE 流式 LLM
  - `provider/tts/dashscope.go` — DashScope CosyVoice WebSocket 流式 TTS + 连接池（`pool.go`）
  - `provider/realtime/omni.go` — Qwen3-Omni-Flash-Realtime 端到端语音
  - `provider/strategy/smart.go` — 混合模式下的异步对话策略
- 接口定义在**使用方**所在的包中（Go 惯用法），尽量小（1-3 个方法）。
- 编译期检查接口实现：`var _ ASRProvider = (*QwenASR)(nil)`。
- 新增 Provider 只需实现 interface + 注册，不修改已有代码（开闭原则）。

### 2.7 可观测（Observable）

- **结构化日志（slog）**：所有日志必须结构化 JSON，携带 `session_id`、`task_id`、`component` 等上下文字段。
- **OTel 度量**（`internal/observe/metrics.go`）：基于 OpenTelemetry SDK，暴露以下指标：
  - 延迟直方图：ASR 延迟、LLM 首 token 延迟、TTS 首包延迟、轮次端到端延迟
  - 计数器：打断次数、静默超时、Provider 错误、通话完成、填充音播放、推测执行命中/未中
  - 网络质量：音频间隙、弱网事件、低音量事件、抖动分布、丢包率
- **eBPF 探针**（`internal/observe/ebpf/`，可选）：
  - `tcptracer.go` — TCP 连接延迟追踪（tracepoint/tcp）
  - `schedlat.go` — Go 调度延迟采集（sched:sched_switch）
  - 需要 Linux kernel >= 5.8（BTF），CAP_BPF + CAP_PERFMON 权限
- **网络质量监控**（`call/netquality.go`）：实时统计抖动、丢包率、低音量比例，触发事件告警。
- **通话事件流**：`call_events` 表记录全部媒体事件（语音开始/结束、打断、超时、AMD 结果、网络异常），精确到毫秒。
- **pprof 端点**：开发和预发布环境开放 `/debug/pprof/`，支持 CPU / Heap / Goroutine / Trace 分析。

---

## 3. 技术栈选型

> 选型标准：**优先标准库 → 社区首选 → 最小依赖 → 尽量零 CGO**。

### 3.1 核心依赖

| 领域 | 库 | 理由 |
|------|-----|------|
| **HTTP 路由** | `net/http` (Go 1.22+) | 官方标准库，已支持 `GET /path/{id}` 模式匹配 |
| **数据库** | `jackc/pgx/v5` | 最快的 PostgreSQL 驱动，原生连接池 |
| **Redis** | `redis/go-redis/v9` | 官方推荐，类型安全 API，内置连接池 |
| **任务队列** | `hibiken/asynq` | Redis-based 任务队列，支持重试、去重、优先级 |
| **WebSocket** | `coder/websocket` | 符合标准的 WebSocket 实现 |
| **配置** | `knadh/koanf/v2` + TOML | 轻量配置库，多源加载（struct defaults → TOML → 环境变量） |
| **日志** | `log/slog` | Go 1.21+ 官方结构化日志 |
| **度量** | `go.opentelemetry.io/otel` | OpenTelemetry 标准，指标 + 追踪 |
| **eBPF** | `cilium/ebpf` | 内核级观测，TCP/调度延迟采集（可选） |
| **CLI** | `urfave/cli/v3` | 轻量 CLI 框架 |
| **验证** | `go-playground/validator/v10` | 社区标准，结构体标签验证 |
| **测试** | `testing` + `stretchr/testify` | 官方 + 社区最流行的断言库 |
| **Lint** | `golangci-lint v2` | 集成 50+ linter 的元工具，严格模式 |
| **音频处理** | Sonata `engine/pcm` | PCM 重采样、能量计算、WAV 编码、WebRTC VAD |
| **UUID** | `google/uuid` | Google 官方实现 |

### 3.2 明确不用的库

| 库 | 不用的理由 |
|-----|-----------|
| Gin / Echo / Fiber | Go 1.22+ 标准库路由已足够 |
| GORM / sqlx | ORM 在 Go 中是反模式，pgx 原生 API 更清晰可控 |
| Viper | 过度设计，koanf 更轻量 |
| Zap / Zerolog | slog 是官方标准，够用 |
| Wire / Fx | 构造函数显式注入，不需要 DI 框架 |

---

## 4. 配置格式统一：TOML

### 4.1 为什么选 TOML

| 格式 | 问题 |
|------|------|
| **YAML** | 缩进敏感，隐式类型转换（Norway problem），安全隐患 |
| **JSON** | 不支持注释，手写体验差 |
| **TOML** | 显式类型、支持注释、嵌套清晰、Go 一流支持 |

### 4.2 配置管理规则

- **单一配置文件 `clarion.toml`**，按模块分段（`[server]`、`[database]`、`[redis]`、`[asr]`、`[llm]`、`[tts]`、`[freeswitch]` 等）。
- 配置结构体在 `internal/config/` 统一定义，启动时一次性加载校验，运行时不可变。
- 环境变量仅用于覆盖敏感信息（API Key、DSN），命名规则 `CLARION_{SECTION}_{KEY}`。
- 配置加载优先级：代码默认值 → TOML 文件 → 环境变量 → 命令行参数。
- 配置变更需重启生效（无热加载），简单可预测。

---

## 5. 项目结构

```
clarion/
├── cmd/
│   ├── clarion/          # API Server 入口
│   │   └── main.go
│   ├── worker/           # Call Worker 入口
│   │   └── main.go
│   └── postprocessor/    # Post-Processing Worker 入口
│       └── main.go
├── internal/
│   ├── config/           # TOML 配置加载（koanf/v2）
│   ├── api/              # HTTP 路由与处理器
│   │   ├── handler/      # 模板、任务、联系人、通话处理器
│   │   └── schema/       # 请求/响应结构体
│   ├── model/            # 数据库模型（struct + SQL）
│   ├── service/          # 业务逻辑层
│   ├── store/            # 数据访问层（PostgreSQL pgx + Redis go-redis）
│   ├── engine/           # 核心引擎
│   │   ├── types.go      # MediaState/DialogueState/Intent/Grade 等枚举
│   │   ├── context.go    # 对话上下文
│   │   ├── media/        # 媒体 FSM（Sonata mediafsm 封装）
│   │   ├── dialogue/     # 业务对话 FSM + 对话引擎
│   │   └── rules/        # 规则引擎
│   ├── provider/         # AI Provider 接口（Sonata 类型别名）+ 实现
│   │   ├── asr.go/llm.go/tts.go  # Sonata 类型别名
│   │   ├── asr/          # 通义听悟（WebSocket 流式）
│   │   ├── llm/          # DeepSeek（SSE 流式）
│   │   ├── tts/          # DashScope CosyVoice（WebSocket + 连接池）
│   │   ├── realtime/     # Qwen3-Omni-Flash-Realtime（混合模式）
│   │   └── strategy/     # Smart LLM 异步策略（混合模式）
│   ├── call/             # Call Worker 核心
│   │   ├── worker.go     # Asynq 任务消费主循环
│   │   ├── session.go    # 单通电话会话编排
│   │   ├── session_audio.go   # 音频收发
│   │   ├── session_dialogue.go # 对话流程
│   │   ├── session_esl.go     # FreeSWITCH ESL 控制
│   │   ├── session_tts.go     # TTS 合成与播放
│   │   ├── session_filler.go  # 填充音频
│   │   ├── session_hybrid.go  # Omni-Realtime 混合模式
│   │   ├── session_speculative.go # 推测执行
│   │   ├── esl.go        # FreeSWITCH ESL 客户端
│   │   ├── amd.go        # 语音信箱/IVR 检测
│   │   ├── jitter.go     # 抖动缓冲
│   │   ├── netquality.go # 网络质量监控
│   │   ├── snapshot.go   # 会话快照/恢复
│   │   └── adapter.go    # Sonata Transport 适配
│   ├── guard/            # 对话安全防护
│   │   ├── budget.go     # Token/时间预算控制
│   │   ├── response.go   # 回复格式校验
│   │   ├── content.go    # 内容安全过滤
│   │   ├── offtopic.go   # 跑题检测
│   │   ├── output.go     # 输出安全校验
│   │   ├── filter.go     # 输入过滤
│   │   └── validator.go  # 决策校验
│   ├── resilience/       # 韧性模式
│   │   ├── breaker.go    # 熔断器（Closed/Open/HalfOpen 三态）
│   │   ├── retry.go      # 重试（指数退避）
│   │   └── fallback.go   # 降级回退
│   ├── observe/          # 可观测性
│   │   ├── metrics.go    # OTel 度量指标
│   │   └── ebpf/         # eBPF 探针（TCP/调度延迟）
│   ├── scheduler/        # 任务调度（Asynq）
│   │   ├── client.go     # Asynq 客户端
│   │   ├── task.go       # 任务类型定义
│   │   └── retrysched.go # 重试调度
│   ├── postprocess/      # Post-Processing Worker
│   ├── notify/           # 通知服务（企业微信）
│   ├── precompile/       # TTS 预合成音频缓存
│   └── simulate/         # 文本模拟模式
├── migrations/           # SQL 迁移文件
├── clarion.toml          # 配置文件
├── go.mod                # Go 1.25+
├── Taskfile.yml          # 构建、测试、lint 命令
├── Dockerfile            # 多阶段构建
└── design/               # 设计文档
```

### 5.1 结构设计原则

- **`cmd/`** — 每个可执行文件一个目录，`main.go` 只做依赖组装和启动
- **`internal/`** — 全部业务代码在 internal 下，Go 编译器禁止外部导入
- **无 `pkg/`** — 通用能力提取到独立 module（Sonata），不在项目内暴露
- **扁平优先** — 只在确实需要分组时创建子目录，避免过度嵌套
- **Sonata 分离** — 核心引擎能力（Session、FSM、PCM、aiface 接口）在 Sonata 独立模块中，Clarion 通过类型别名引用

---

## 6. 面向接口（Go Interface）

### 6.1 Provider 接口体系

Provider 接口定义在 Sonata 核心库（`engine/aiface`），Clarion 通过类型别名引用：

```go
// internal/provider/asr.go
type ASRProvider = sonata.ASRProvider  // StartStream(ctx, cfg) → (ASRStream, error)
type ASRStream   = sonata.ASRStream   // FeedAudio / Events / Close
type ASREvent    = sonata.ASREvent    // Text, IsFinal, Confidence, LatencyMs

// internal/provider/llm.go
type LLMProvider = sonata.LLMProvider  // GenerateStream(ctx, messages, cfg) → (<-chan string, error)

// internal/provider/tts.go
type TTSProvider = sonata.TTSProvider  // SynthesizeStream(ctx, textCh, cfg) → (<-chan []byte, error) + Cancel()
```

### 6.2 业务注入接口

```go
// Sonata engine/aiface
type DialogEngine interface {
    // 业务逻辑注入点，Session 在适当时机调用
}

// engine.Transport — 音频 I/O 抽象
type Transport interface {
    // 音频收发，FreeSWITCH ESL 和 WebSocket 各有实现
}
```

### 6.3 Clarion 内部接口

```go
// internal/call — 接口定义在使用方
type SpeechDetector interface {
    IsSpeech(frame []byte) (bool, error)
}
```

接口设计遵循：使用方定义、尽量小（1-3 方法）、编译期检查实现。

---

## 7. 测试策略

### 7.1 测试分层

| 层 | 工具 | 说明 |
|----|------|------|
| **单元测试** | `testing` + `testify/assert` | 纯逻辑测试，无外部依赖 |
| **集成测试** | `testcontainers-go` | PostgreSQL + Redis 真实容器 |
| **Provider 集成** | `task integration-test-*` | 真实 ASR/LLM/TTS API（需 .env） |
| **基准测试** | `testing.B` | 音频重采样、状态机转移、TTS 连接池、抖动缓冲 |
| **模糊测试** | `testing.F` | ESL 协议解析、DeepSeek SSE 解析、TTS 帧处理、网络质量 |
| **API 测试** | `net/http/httptest` | HTTP 端点黑盒测试 |
| **竞态检测** | `-race` flag | CI 中所有测试启用 |

### 7.2 测试原则

- **测试先行**：每个模块先写测试再写实现，覆盖率目标 > 80%，核心引擎 > 90%。
- **表驱动测试**：Go 标准模式，一组输入/期望输出，新增 case 只需加一行。
- **接口注入**：所有外部依赖通过接口注入，测试时替换为 mock/stub。

### 7.3 CI 集成

```bash
task test              # go test -race -count=1 -coverprofile + 覆盖率报告
task lint              # golangci-lint run（严格模式，0 issues）
task integration-test  # 全部 Provider 集成测试
```

---

## 8. 性能目标

| 指标 | 目标值 | 度量方式 |
|------|--------|----------|
| 端到端延迟（用户说完 → AI 播放） | < 1.2s | OTel turnLatency 直方图 |
| 单实例并发通话 | 50+ 路 | 压测 |
| 内存/通话 | < 5MB | pprof heap |
| API 响应 P99 | < 50ms | 中间件计时 |
| 二进制大小 | < 20MB | `ls -lh` |
| Docker 镜像大小 | < 30MB | `docker images` |
| 冷启动时间 | < 500ms | 日志时间戳 |

---

## 9. Lint 纪律（golangci-lint v2 强制规范）

> **这不是建议，是硬性规定。**

### 核心铁律

| 规则 | 适用范围 | 说明 |
|------|----------|------|
| **禁止 `//nolint`** | 非测试代码 | 必须修复 lint 问题本身 |
| **禁止修改 lint 配置绕过** | 全部代码 | `.golangci.yml` 变更需 code review 审批 |
| **测试代码中非必要不得绕过** | 测试代码 | 确需绕过时，必须 `//nolint:具体linter // 理由说明` |
| **severity 全部 error** | CI | lint 不通过 = 构建失败 |

### 复杂度阈值

| 指标 | 阈值 |
|------|------|
| 圈复杂度 (gocyclo) | 15 |
| 函数行数 (funlen) | 80 行 / 50 语句 |
| 嵌套深度 (nestif) | 4 层 |

### 违反处理

1. CI 自动阻断：lint 不通过的 PR 无法合入。
2. 不接受"先 nolint 后修"的承诺：要么修复，要么不提交。
3. 发现绕过行为视为代码质量事故，必须回滚。

---

## 10. 代码规范

### 10.1 语言与风格

- **Go 1.25+**，充分利用最新语言特性。
- **注释语言：中文**（包括包文档、函数注释、行内注释）。
- **commit 信息：中文**，描述 why 而非 what，禁止包含 AI 标识。
- **错误即值**：`if err != nil` 显式处理，不使用 panic 做流程控制。
- **零值可用**：结构体零值应有合理的默认行为。
- **组合优于继承**：使用嵌入和接口组合，不模拟类层次。
- **显式优于隐式**：不用 `init()`，不用全局变量，依赖通过构造函数注入。

### 10.2 命名规范

- 包名：单个小写单词（`call`、`engine`、`store`），**禁止** `utils`、`helpers`、`common`。
- 接口名：行为动词（`Reader`、`Provider`、`Sender`），定义在**使用方**包中。
- 错误变量：`ErrNotFound`、`ErrTimeout`。
- Context 作为第一个参数：`func Foo(ctx context.Context, ...) error`。

### 10.3 错误处理

- 逐层包装：`fmt.Errorf("send audio to ESL: %w", err)`。
- 每个 `if err != nil` 都必须处理（返回、记录、或降级），`errcheck` 强制检查。
- 区分临时错误（可重试）与永久错误（需降级/挂断）。

### 10.4 依赖方向

```
api → service → store
         ↓
      engine ← call（通话时）
         ↓
      provider
```

禁止循环依赖，Go 编译器强制保证。

---

## 11. 原则优先级

当原则之间发生冲突时，按以下优先级决策：

```
鲁棒性（故障友好） > 性能（实时优先） > 可测试性 > 接口抽象 > 可观测 > 可维护性
```

- 系统**不能崩**比**快**更重要 — 一通卡死的电话比延迟多 200ms 的影响严重得多。
- **能测**比**写得漂亮**更重要 — 没有测试覆盖的"优雅代码"是定时炸弹。
- **接口清晰**比**灵活可扩展**更重要 — 过度设计的抽象比硬编码更难维护。
