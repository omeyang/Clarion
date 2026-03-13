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
| **生态成熟** | net/http、database/sql、go-redis 均为生产级库 |
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

### 2.1 面向接口（Interface-Oriented）

- **所有模块间交互通过 Go interface 定义**，不直接依赖具体实现。
- ASR / LLM / TTS Provider、存储层（PostgreSQL / Redis / OSS）、通知渠道均为 interface。
- 接口定义在**使用方**所在的包中（Go 惯用法），而非实现方。
- 接口尽量小：1-3 个方法的接口优于 10 个方法的大接口。
- 编译期检查接口实现：`var _ ASRProvider = (*QwenASR)(nil)`。

```go
// 好：小接口，使用方定义
type AudioSender interface {
    SendAudio(chunk []byte) error
}

// 坏：大而全的接口，实现方定义
type ESLClient interface {
    Connect() error
    Originate(...) error
    SendAudio(...) error
    Kill(...) error
    // ... 10 more methods
}
```

### 2.2 封装（Encapsulation）

- **`internal/` 强制封装**，所有业务代码在 `internal/` 下，Go 编译器禁止外部包导入。
- 每个包只暴露必要的公开类型，实现细节用小写字母开头保持私有。
- 构造函数模式：`New*()` 返回接口类型，隐藏实现结构体。
- 状态机内部状态不直接暴露，通过方法查询：`fsm.State()` 而非 `fsm.state`。
- 配置结构体的敏感字段实现 `fmt.Stringer`，避免日志泄露。

### 2.3 Go 惯用风格（Idiomatic Go）

- **错误即值**：`if err != nil` 显式处理，不使用 panic 做流程控制。
- **零值可用**：结构体零值应有合理的默认行为。
- **组合优于继承**：使用嵌入（embedding）和接口组合，不模拟类层次。
- **显式优于隐式**：不用 init()，不用全局变量，依赖通过构造函数注入。
- **命名规范**：
  - 包名：单个小写单词（`call`、`engine`、`store`）
  - 接口名：行为动词（`Reader`、`Provider`、`Sender`）
  - 错误变量：`ErrNotFound`、`ErrTimeout`
  - Context 作为第一个参数：`func Foo(ctx context.Context, ...) error`
- **代码组织**：一个包一个职责，避免 `utils`、`helpers`、`common` 万能包。

### 2.4 统一配置（Unified Configuration）

- **单一配置文件 `clarion.toml`**，消除多种配置格式混用。
- TOML 格式：显式类型、支持注释、嵌套清晰、Go 有一流解析库。
- 环境变量仅用于覆盖敏感信息（API Key、DSN），命名规则 `CLARION_{SECTION}_{KEY}`。
- 配置结构体在 `internal/config/` 统一定义，启动时一次性加载校验，运行时不可变。
- 配置变更需重启生效（无热加载），简单可预测。

### 2.5 可观测性（Observability）

- **结构化日志（slog）**：所有日志必须结构化，携带 `session_id`、`task_id`、`component` 等上下文字段。
- **关键延迟埋点**：ASR 延迟、LLM 首 token 延迟、TTS 首包延迟、端到端延迟，每轮对话记录。
- **通话事件流**：`call_events` 表记录全部媒体事件（语音开始/结束、打断、超时），精确到毫秒。
- **运行时指标**：通过 `expvar` 或 Prometheus 暴露在途通话数、错误率、goroutine 数、内存使用。
- **pprof 端点**：开发和预发布环境开放 `/debug/pprof/`，支持 CPU / Heap / Goroutine 分析。
- **trace 支持**：预留 `trace_id` 字段贯穿全链路，后续可接入 OpenTelemetry。
- **告警条件**：结构化日志 + 指标可直接对接 Alertmanager 或 Loki 告警规则。

### 2.6 可维护性（Maintainability）

- **代码即文档**：清晰的命名和小函数比注释更重要，注释只解释 why 不解释 what。
- **一个包一个职责**：`call/` 只管通话、`engine/` 只管状态机、`store/` 只管数据访问。
- **依赖方向单一**：`api → service → store`，禁止循环依赖，Go 编译器强制保证。
- **golangci-lint 强制守护**：CI 中必须通过，severity 全部为 error，lint 不过不合入。
- **nolint 纪律**（详见 2.15 节）：
  - 非测试代码中 **禁止使用 `//nolint`**，必须修复 lint 问题本身。
  - **禁止通过修改 golangci-lint 配置来绕过**，配置变更需 code review 审批。
  - 测试代码中非必要不得绕过，确需绕过时必须指明具体 linter 并写明理由。
- **迁移文件版本化**：SQL 迁移文件 `001_xxx.up.sql` / `001_xxx.down.sql`，可回滚。
- **Git commit 规范**：每个 commit 做一件事，描述 why 而非 what。

### 2.7 可测试性（Testability）

- **测试先行**：每个模块先写测试再写实现，测试覆盖率目标 > 80%，核心引擎 > 90%。
- **表驱动测试**：Go 标准模式，一组输入/期望输出，新增 case 只需加一行。
- **接口注入**：所有外部依赖通过接口注入，测试时替换为 mock/stub。
- **基准测试**：热路径必须有 `Benchmark*` 函数，CI 中跟踪性能回归。
- **模糊测试**：协议解析（ESL）、音频帧处理使用 `testing.F` 发现边界问题。
- **集成测试**：`testcontainers-go` 启动真实 PostgreSQL + Redis 容器，不 mock 数据库。
- **httptest**：API 端点使用 `net/http/httptest` 黑盒测试，验证请求/响应契约。
- **-race flag**：CI 中所有测试启用 `-race` 检测数据竞争。

### 2.8 灵活性（Flexibility）

- **Provider 可插拔**：新增 ASR / LLM / TTS 供应商只需实现 interface，无需改动引擎。
- **领域适配**：业务逻辑通过场景模板配置驱动，不硬编码行业特性。
- **状态机可配置**：业务状态和转移规则来自模板配置，非代码中的硬编码。
- **通知渠道可扩展**：通知接口抽象，当前实现企业微信，后续可扩展钉钉/飞书/短信。
- **存储层可替换**：OSS 接口抽象，可从阿里云 OSS 切换到 MinIO（S3 兼容）无需改上层。

### 2.9 鲁棒性（Robustness）

- **永不 panic**：所有可恢复错误通过 `error` 返回，HTTP handler 有 recover 中间件兜底。
- **超时无处不在**：所有外部调用（ASR / LLM / TTS / DB / Redis / ESL）必须携带 `context.Context` 超时。
- **降级链**：TTS 超时 → 预合成音频 → 模板话术 → 礼貌挂断，逐级降级。
- **重连机制**：ESL 断开自动重连（指数退避）；Redis / PostgreSQL 连接池自动恢复。
- **幂等写入**：后处理写入（turns / events / call record）全部幂等，重复消费不产生副作用。
- **优雅关闭**：收到 SIGTERM 后停止接新任务，等待在途通话完成（最长 `max_duration_sec`），然后退出。
- **资源上限**：并发通话数硬性上限、goroutine 数监控、内存使用告警，防止资源耗尽。
- **Admission Control**：宁可少打电话，也不让接通的电话体验差。

### 2.10 可扩展性（Extensibility）

- **水平扩展**：Call Worker 无共享状态（会话状态在 goroutine 内存中），可多实例部署。
- **后处理扩展**：Redis Stream 消费组模式，增加消费者实例即可提升处理吞吐。
- **新 Provider 接入**：实现 interface + 注册工厂函数，不修改已有代码（开闭原则）。
- **新通知渠道**：实现 `Notifier` interface，在配置中指定即可启用。
- **新场景适配**：通过模板配置定义新行业场景，零代码改动。

### 2.11 成熟可靠的依赖（Proven Dependencies）

- **优先标准库**：`net/http`、`database/sql`、`encoding/json`、`log/slog`、`text/template`、`testing`。
- **社区首选**：只选 GitHub Star 高、维护活跃、被大量项目依赖的库。
- **最小引入**：每个依赖都要有明确理由，能用标准库解决的绝不引入第三方。
- **零 CGO**：纯 Go 实现，保证静态链接和交叉编译能力。
- **明确排除**：不用 Web 框架（Gin/Echo）、不用 ORM（GORM）、不用 DI 框架（Wire/Fx）、不用重量级配置库（Viper）。

### 2.12 性能优先，尤其时延（Performance-First, Especially Latency）

- **端到端延迟目标 < 1.2s**：从用户说完话到 AI 开始播放回复。
- **流式管道**：ASR → LLM → TTS 通过 channel 串联，各环节流水线并行，不等待完整结果。
- **热路径零分配**：音频帧处理、状态机转移等热路径代码尽量避免堆分配（`sync.Pool` / 栈分配）。
- **基准测试守护**：所有热路径必须有 `Benchmark*` 函数，CI 中跟踪回归。
- **pprof 可随时分析**：CPU / Heap / Goroutine / Trace 四维分析能力。
- **预合成音频**：开场白、确认词、结束语预合成为音频文件，消除 TTS 延迟。
- **连接复用**：ESL 长连接、HTTP/2 连接池、Redis Pipeline / 连接池，减少建连开销。
- **内存效率**：goroutine ~2KB 栈起步，单通话目标 < 5MB，单实例支撑 50+ 路并发。

### 2.13 完善的错误处理（Comprehensive Error Handling）

- **错误分类**：区分临时错误（可重试）与永久错误（需人工干预），通过自定义错误类型标识。
- **错误上下文**：`fmt.Errorf("send audio to ESL: %w", err)` 逐层包装，保留完整调用链。
- **错误不吞没**：每个 `if err != nil` 都必须处理（返回、记录、或降级），golangci-lint `errcheck` 强制检查。
- **超时处理**：
  - ASR 超时 → 播放"没听清，请再说一次"
  - LLM 超时 → 规则引擎兜底回复
  - TTS 超时 → 预合成音频降级
  - 连续 N 次错误 → 礼貌挂断
- **恐慌恢复**：HTTP handler 和 goroutine 入口都有 `defer recover()` 兜底，记录堆栈后继续服务。
- **错误指标**：ASR / LLM / TTS 错误率实时统计，超过阈值触发 Admission Control 降速。

```go
// 错误类型分类
var (
    ErrTemporary = errors.New("temporary")  // 可重试
    ErrPermanent = errors.New("permanent")  // 不可重试
)

type ProviderError struct {
    Provider string        // "asr", "llm", "tts"
    Op       string        // "connect", "stream", "parse"
    Err      error         // 原始错误
    Retry    bool          // 是否可重试
}

func (e *ProviderError) Error() string {
    return fmt.Sprintf("%s.%s: %v", e.Provider, e.Op, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }
```

### 2.14 原则优先级

当原则之间发生冲突时，按以下优先级决策：

```
鲁棒性 > 性能（时延） > 可测试性 > 面向接口 > 可维护性 > 灵活性 > 可扩展性
```

**解释**：
- 系统**不能崩**比**快**更重要 — 一通卡死的电话比延迟多 200ms 的影响严重得多。
- **能测**比**写得漂亮**更重要 — 没有测试覆盖的"优雅代码"是定时炸弹。
- **接口清晰**比**灵活可扩展**更重要 — 过度设计的抽象比硬编码更难维护。

### 2.15 Lint 纪律（golangci-lint 强制规范）

> **这不是建议，是硬性规定。**

#### 核心铁律

| 规则 | 适用范围 | 说明 |
|------|----------|------|
| **禁止 `//nolint`** | 非测试代码 | 必须修复 lint 问题本身，不允许用 nolint 绕过 |
| **禁止修改 lint 配置绕过** | 全部代码 | `.golangci.yml` 变更需 code review 审批，不允许通过降低标准来"修复" lint 错误 |
| **测试代码中非必要不得绕过** | 测试代码 | 确需绕过时，必须 `//nolint:具体linter // 理由说明` |
| **severity 全部 error** | CI | lint 不通过 = 构建失败，不允许降级为 warning |

#### 配置文件

使用 golangci-lint v2.11+，配置文件为 `.golangci.yml`（完整参考配置见 `design/.golangci.reference.yml`）。

#### 启用的 linter 分类

| 类别 | linter | 为什么启用 |
|------|--------|-----------|
| **正确性** | errcheck, govet, staticcheck, unused, ineffassign | 基础正确性保证 |
| **错误处理** | nilerr, errorlint, wrapcheck | 确保 error 被正确处理和包装（2.13 节要求） |
| **Bug 预防** | bodyclose, sqlclosecheck, rowserrcheck, noctx, contextcheck | 资源泄露和 context 传播检查 |
| **代码质量** | revive, goconst, gocyclo, funlen, nestif, unconvert, unparam | 控制复杂度，保持代码简洁 |
| **安全** | gosec | OWASP 常见安全问题检查 |
| **风格** | goimports, whitespace, godot, errname, exhaustive | 统一风格，减少 review 中的格式争论 |
| **nolint 管控** | nolintlint | 强制执行 nolint 纪律 |
| **性能** | perfsprint, intrange, usestdlibvars | 提醒性能更优的写法 |

#### nolintlint 配置

```yaml
nolintlint:
  require-explanation: true    # 必须写明理由
  require-specific: true       # 必须指定具体 linter（禁止 //nolint 全局忽略）
  allow-unused: false          # 不允许无效的 nolint 指令
```

#### 测试代码中的放宽规则

测试文件（`_test.go`）中以下 linter 被排除（因为表驱动测试和测试辅助函数的特性）：

`funlen`、`gocyclo`、`wrapcheck`、`ireturn`、`errcheck`、`bodyclose`、`goconst`、`dupword`

但 **nolintlint 不被排除** — 测试代码中如果使用 nolint，仍然必须指明 linter 和理由。

#### 复杂度阈值

| 指标 | 阈值 | 说明 |
|------|------|------|
| 圈复杂度 (gocyclo) | 15 | 超过需拆分函数 |
| 函数行数 (funlen) | 80 行 / 50 语句 | 注释不计入 |
| 嵌套深度 (nestif) | 4 层 | 超过需提取子函数或提前返回 |
| 裸 return (nakedret) | 10 行 | 超过此长度的函数禁止裸 return |

#### 违反 lint 纪律的处理

1. **CI 自动阻断**：lint 不通过的 PR 无法合入。
2. **不接受 "先 nolint 后修" 的承诺**：要么修复，要么不提交。
3. **发现绕过行为**：如果在 review 中发现通过修改配置或其他方式绕过 lint，视为 **代码质量事故**，必须回滚。

---

## 3. 技术栈选型

> 选型标准详见 2.11 节「成熟可靠的依赖」。

### 3.1 选型原则

1. **优先官方标准库** — 能用 `net/http`、`database/sql`、`encoding/json` 就不引入第三方
2. **社区首选库** — 只选 Star 数高、维护活跃、被大量项目依赖的库
3. **最小依赖** — 每个依赖都要有明确理由，避免依赖膨胀
4. **零 CGO** — 尽量纯 Go 实现，保证交叉编译和静态链接

### 3.2 核心依赖

| 领域 | 库 | 理由 |
|------|-----|------|
| **HTTP 路由** | `net/http` (Go 1.22+ 内置路由) | 官方标准库，Go 1.22 已支持 `GET /path/{id}` 模式匹配 |
| **数据库** | `jackc/pgx/v5` + `jmoiron/sqlx` | pgx 是最快的 PostgreSQL 驱动，sqlx 提供命名参数和结构体扫描 |
| **数据库迁移** | `golang-migrate/migrate` | 社区标准，支持嵌入 SQL 文件 |
| **Redis** | `redis/go-redis/v9` | 官方推荐，类型安全 API，内置连接池 |
| **WebSocket** | `coder/websocket` (nhooyr) | 符合标准的 WebSocket 实现，比 gorilla 更现代 |
| **配置** | `knadh/koanf/v2` + TOML parser | 轻量配置库，支持多源加载（struct defaults → TOML → 环境变量），类型安全 |
| **日志** | `log/slog` | Go 1.21+ 官方结构化日志 |
| **HTTP 客户端** | `net/http` | 标准库，配合 SSE 手动解析即可 |
| **JSON** | `encoding/json` | 标准库；高性能场景可选 `bytedance/sonic` |
| **验证** | `go-playground/validator/v10` | 社区标准，结构体标签验证 |
| **测试** | `testing` + `stretchr/testify` | 官方 + 社区最流行的断言库 |
| **Lint** | `golangci-lint` | 集成 50+ linter 的元工具 |
| **音频处理** | 纯 Go 实现 | PCM 重采样用线性插值，无需 CGO |
| **模板引擎** | `text/template` | 官方标准库 |
| **UUID** | `google/uuid` | Google 官方实现 |
| **OSS** | `aliyun/aliyun-oss-go-sdk` | 阿里云官方 SDK |
| **DashScope** | HTTP + SSE 直连 | 阿里云 DashScope 是 HTTP API，直接用 net/http 调用 |

### 3.3 明确不用的库

| 库 | 不用的理由 |
|-----|-----------|
| Gin / Echo / Fiber | Go 1.22+ 标准库路由已足够，不需要框架 |
| GORM | ORM 在 Go 中是反模式，sqlx + 手写 SQL 更清晰可控 |
| Viper | 过度设计，koanf 更轻量更符合需求 |
| Zap / Zerolog | slog 是官方标准，够用 |
| Wire / Fx | 依赖注入在 Go 中不推荐，构造函数显式注入 |

---

## 4. 配置格式统一：TOML

> 设计原则详见 2.4 节「统一配置」。

### 4.1 为什么选 TOML

| 格式 | 问题 |
|------|------|
| **INI** | 无类型系统，无嵌套，历史遗留格式 |
| **YAML** | 缩进敏感，隐式类型转换（Norway problem: `NO` → `false`），安全隐患 |
| **JSON** | 不支持注释，手写体验差 |
| **TOML** | 显式类型、支持注释、嵌套清晰、Go 有一流支持 |

### 4.2 配置文件结构

系统只有一个配置文件 `clarion.toml`，按模块分段：

```toml
# clarion.toml — Clarion AI 外呼系统配置

[server]
addr = ":8000"
debug = false
log_level = "info"

[database]
dsn = "postgres://clarion:clarion@localhost:5432/clarion?sslmode=disable"
max_open_conns = 20
max_idle_conns = 5

[redis]
addr = "localhost:6379"
db = 0
task_queue_key = "clarion:task_queue"
event_stream_key = "clarion:call_completed"
session_prefix = "clarion:session"

[asr]
provider = "qwen"
api_key = ""
model = "qwen3-asr-flash-realtime"
sample_rate = 16000

[llm]
provider = "deepseek"
api_key = ""
base_url = "https://api.deepseek.com"
model = "deepseek-chat"
max_tokens = 512
temperature = 0.7
timeout_ms = 5000

[tts]
provider = "dashscope"
api_key = ""
model = "cosyvoice-v3.5-plus"
voice = "longanyang"
sample_rate = 16000

[freeswitch]
esl_host = "127.0.0.1"
esl_port = 8021
esl_password = "ClueCon"
audio_ws_addr = ":8765"

[call_protection]
max_duration_sec = 300
max_silence_sec = 15
ring_timeout_sec = 30
first_silence_timeout_sec = 6
max_asr_retries = 2
max_consecutive_errors = 3
max_turns = 20

[amd]
enabled = true
detection_window_ms = 3000
continuous_speech_threshold_ms = 4000
human_pause_threshold_ms = 300
energy_threshold_dbfs = -35.0

[oss]
enabled = false
endpoint = ""
bucket = "clarion-recordings"
access_key_id = ""
access_key_secret = ""

[worker]
max_concurrent_calls = 5
```

### 4.3 敏感配置的处理

API Key 等敏感信息支持环境变量覆盖：

```go
// 环境变量优先级高于 TOML 文件
// 命名规则: CLARION_{SECTION}_{KEY}
// 例: CLARION_LLM_API_KEY, CLARION_DATABASE_DSN
```

### 4.4 配置加载优先级

```
1. 默认值（代码中定义）
2. clarion.toml 文件
3. 环境变量覆盖（CLARION_ 前缀）
4. 命令行参数（--config 指定配置文件路径）
```

---

## 5. 项目结构

```
clarion/
├── cmd/
│   ├── clarion/          # 主入口（API server）
│   │   └── main.go
│   ├── worker/           # Call Worker 入口
│   │   └── main.go
│   └── postprocessor/    # Post-Processing Worker 入口
│       └── main.go
├── internal/
│   ├── config/           # TOML 配置加载
│   │   └── config.go
│   ├── api/              # HTTP 路由与处理器
│   │   ├── router.go
│   │   ├── middleware.go
│   │   ├── handler/
│   │   │   ├── template.go
│   │   │   ├── task.go
│   │   │   ├── contact.go
│   │   │   └── call.go
│   │   └── schema/       # 请求/响应结构体
│   │       ├── template.go
│   │       ├── task.go
│   │       ├── contact.go
│   │       └── call.go
│   ├── model/            # 数据库模型（struct + SQL）
│   │   ├── contact.go
│   │   ├── template.go
│   │   ├── task.go
│   │   ├── call.go
│   │   └── queries.go
│   ├── store/            # 数据访问层
│   │   ├── postgres.go
│   │   ├── redis.go
│   │   └── oss.go
│   ├── engine/           # 核心引擎
│   │   ├── media/        # 媒体状态机
│   │   │   └── fsm.go
│   │   ├── dialogue/     # 业务状态机 + 对话引擎
│   │   │   ├── fsm.go
│   │   │   └── engine.go
│   │   └── rules/        # 规则引擎
│   │       └── engine.go
│   ├── provider/         # AI Provider 接口 + 实现
│   │   ├── asr.go        # ASR 接口定义
│   │   ├── llm.go        # LLM 接口定义
│   │   ├── tts.go        # TTS 接口定义
│   │   ├── asr/          # ASR 实现
│   │   │   ├── qwen.go
│   │   │   └── dashscope.go
│   │   ├── llm/          # LLM 实现
│   │   │   └── deepseek.go
│   │   └── tts/          # TTS 实现
│   │       ├── dashscope.go
│   │       └── voice_clone.go
│   ├── call/             # Call Worker
│   │   ├── worker.go     # 主循环
│   │   ├── session.go    # 单通电话会话
│   │   ├── esl.go        # FreeSWITCH ESL 客户端
│   │   ├── audio.go      # 音频桥接 + 重采样
│   │   └── amd.go        # 语音信箱检测
│   ├── postprocess/      # Post-Processing Worker
│   │   ├── worker.go
│   │   ├── summary.go
│   │   └── writer.go
│   ├── notify/           # 通知服务
│   │   └── wechat.go
│   └── precompile/       # 预合成音频
│       ├── synthesizer.go
│       └── cache.go
├── migrations/           # SQL 迁移文件
│   ├── 001_initial_schema.up.sql
│   └── 001_initial_schema.down.sql
├── scripts/              # 运维工具脚本
├── clarion.toml          # 配置文件
├── clarion.example.toml  # 配置示例
├── go.mod
├── go.sum
├── Taskfile.yml          # 构建、测试、lint 命令（go-task）
├── Dockerfile            # 多阶段构建
└── design/               # 设计文档（保留）
```

### 5.1 结构设计原则

- **`cmd/`** — 每个可执行文件一个目录，`main.go` 只做依赖组装和启动
- **`internal/`** — 全部业务代码在 internal 下，禁止外部导入
- **无 `pkg/`** — 当前阶段不需要对外暴露库
- **扁平优先** — 只在确实需要分组时创建子目录，避免过度嵌套
- **`model/` 与 `store/` 分离** — model 定义结构体，store 封装数据库操作

---

## 6. 接口设计（Go Interface）

> 设计原则详见 2.1 节「面向接口」。

### 6.1 ASR Provider

```go
// provider/asr.go

type ASREvent struct {
    Text       string
    IsFinal    bool
    Confidence float64
    LatencyMs  int
}

type ASRStream interface {
    // FeedAudio sends audio chunk to the recognition stream.
    FeedAudio(ctx context.Context, chunk []byte) error

    // Events returns a channel that receives ASR events.
    Events() <-chan ASREvent

    // Close terminates the recognition stream.
    Close() error
}

type ASRProvider interface {
    // StartStream opens a new recognition stream.
    StartStream(ctx context.Context, cfg ASRConfig) (ASRStream, error)
}
```

### 6.2 LLM Provider

```go
// provider/llm.go

type LLMProvider interface {
    // GenerateStream returns a channel of response tokens.
    GenerateStream(ctx context.Context, messages []Message, cfg LLMConfig) (<-chan string, error)
}
```

### 6.3 TTS Provider

```go
// provider/tts.go

type TTSProvider interface {
    // SynthesizeStream accepts a text channel and returns an audio chunk channel.
    SynthesizeStream(ctx context.Context, textCh <-chan string, cfg TTSConfig) (<-chan []byte, error)

    // Cancel aborts the current synthesis (barge-in scenario).
    Cancel() error
}
```

### 6.4 流式管道（Channel Pipeline）

Go 的 channel 天然适合流式管道串联：

```go
func (s *Session) streamingPipeline(ctx context.Context, asrText string) error {
    // LLM 流式生成 → channel
    tokenCh, err := s.llm.GenerateStream(ctx, s.buildMessages(asrText), s.llmCfg)
    if err != nil {
        return err
    }

    // TTS 流式合成 ← channel
    audioCh, err := s.tts.SynthesizeStream(ctx, tokenCh, s.ttsCfg)
    if err != nil {
        return err
    }

    // 播放音频 ← channel
    for chunk := range audioCh {
        if err := s.esl.SendAudio(chunk); err != nil {
            return err
        }
    }
    return nil
}
```

---

## 7. 测试策略

> 设计原则详见 2.7 节「可测试性」。

### 7.1 测试先行原则

> **每个模块实现前先写测试，测试覆盖率目标 > 80%。**

Go 的 `testing` 包天然支持：
- 单元测试（`_test.go`）
- 基准测试（`Benchmark`）
- 模糊测试（`Fuzz`）
- 并行测试（`t.Parallel()`）

### 7.2 测试分层

| 层 | 工具 | 说明 |
|----|------|------|
| **单元测试** | `testing` + `testify/assert` | 纯逻辑测试，无外部依赖 |
| **集成测试** | `testcontainers-go` | PostgreSQL + Redis 真实容器 |
| **基准测试** | `testing.B` | 音频重采样、状态机转移性能 |
| **API 测试** | `net/http/httptest` | HTTP 端点黑盒测试 |
| **模糊测试** | `testing.F` | ESL 协议解析、音频帧处理 |

### 7.3 测试基础设施

```go
// 表驱动测试 — Go 标准模式
func TestMediaFSM_Transition(t *testing.T) {
    tests := []struct {
        name    string
        from    MediaState
        event   MediaEvent
        want    MediaState
        wantErr bool
    }{
        {"idle to dialing", Idle, EvDial, Dialing, false},
        {"dialing to ringing", Dialing, EvRinging, Ringing, false},
        {"invalid transition", Idle, EvBargeIn, Idle, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            fsm := NewMediaFSM(tt.from)
            err := fsm.Handle(tt.event)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tt.want, fsm.State())
        })
    }
}
```

### 7.4 CI 集成

```yaml
# Taskfile.yml (go-task)
tasks:
  test:
    cmds:
      - go test -race -count=1 -coverprofile=coverage.out ./cmd/... ./internal/...
      - go tool cover -func=coverage.out

  lint:
    cmds:
      - golangci-lint run ./cmd/... ./internal/...

  bench:
    cmds:
      - go test -bench=. -benchmem ./internal/...
```

---

## 8. 性能目标与度量

> 设计原则详见 2.12 节「性能优先，尤其时延」。

### 8.1 性能目标

| 指标 | 目标值 | 度量方式 |
|------|--------|----------|
| 端到端延迟（用户说完 → AI 开始播放） | < 1.2s | 通话事件时间戳 |
| 单实例并发通话 | 50+ 路 | 压测 |
| 内存/通话 | < 5MB | pprof heap |
| API 响应 P99 | < 50ms | 中间件计时 |
| 二进制大小 | < 20MB | `ls -lh` |
| Docker 镜像大小 | < 30MB | `docker images` |
| 冷启动时间 | < 500ms | 日志时间戳 |

### 8.2 性能工具

| 工具 | 用途 |
|------|------|
| `go test -bench` | 微基准测试 |
| `pprof` (CPU/Heap/Goroutine) | 运行时性能分析 |
| `trace` | goroutine 调度可视化 |
| `expvar` / Prometheus | 运行时指标暴露 |
| `-race` flag | 数据竞争检测 |

### 8.3 关键路径基准测试

以下热路径必须有基准测试：

- 音频重采样（8kHz → 16kHz）
- 能量计算（dBFS）
- 媒体状态机转移
- 业务状态机转移
- 规则引擎匹配
- ESL 协议解析
- JSON 序列化/反序列化

---

## 9. 部署方案

### 9.1 构建

```dockerfile
# Dockerfile — 多阶段构建
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion ./cmd/clarion
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion-worker ./cmd/worker
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clarion-postprocessor ./cmd/postprocessor

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /clarion /clarion-worker /clarion-postprocessor /usr/local/bin/
COPY clarion.example.toml /etc/clarion/clarion.toml
ENTRYPOINT ["clarion"]
```

### 9.2 运行方式

```bash
# 方式一：单二进制，子命令
clarion serve          # API server
clarion worker         # Call Worker
clarion postprocess    # Post-Processing Worker
clarion migrate up     # 数据库迁移
clarion simulate       # 文本模拟模式

# 方式二：独立二进制
clarion-worker
clarion-postprocessor
```

### 9.3 部署优势

| 维度 | 说明 |
|------|------|
| 部署依赖 | 无（静态二进制） |
| Docker 镜像 | ~25MB (alpine + binary) |
| 启动时间 | < 500ms |
| 内存（空载） | ~10MB |
| 进程模型 | 3 个 Go 二进制（或 1 个子命令） |

---

## 10. 开发路线

### 10.1 原则

- **设计文档先行**，业务逻辑和架构决策文档化
- **测试先行**，每个模块先有测试再写实现

### 10.2 阶段划分

#### Phase 1: 基础设施（1-2 周）

- [ ] 项目脚手架（go.mod、目录结构、Taskfile.yml）
- [ ] TOML 配置加载
- [ ] PostgreSQL 连接 + 迁移（golang-migrate）
- [ ] Redis 客户端封装
- [ ] 结构化日志（slog）
- [ ] 基础中间件（日志、恢复、CORS）

#### Phase 2: 核心引擎（2-3 周）

- [ ] 枚举类型和核心数据结构
- [ ] 媒体状态机（表驱动）+ 100% 测试覆盖
- [ ] 业务状态机 + 规则引擎 + 测试
- [ ] 对话引擎 orchestrator
- [ ] Provider 接口定义

#### Phase 3: AI Provider（1-2 周）

- [ ] DeepSeek LLM Provider（SSE 流式）
- [ ] DashScope ASR Provider（WebSocket 流式）
- [ ] DashScope TTS Provider（WebSocket 流式）
- [ ] Provider 集成测试

#### Phase 4: 管理面 API（1-2 周）

- [ ] 数据模型 + 查询
- [ ] 模板 CRUD + 发布 + 快照
- [ ] 任务 CRUD + 状态流转
- [ ] 联系人 CRUD + 批量导入
- [ ] 通话记录查询 API
- [ ] httptest 端点测试

#### Phase 5: Call Worker（2-3 周）

- [ ] ESL 客户端（TCP 异步读写）
- [ ] WebSocket 音频桥接 + 重采样
- [ ] AMD 检测
- [ ] CallSession 会话编排
- [ ] CallWorker 主循环（Redis 消费 + ESL 事件路由）
- [ ] 预合成音频缓存

#### Phase 6: Post-Processing Worker（1 周）

- [ ] Redis Stream 消费者
- [ ] LLM 摘要生成
- [ ] 结果持久化（幂等写入）
- [ ] OSS 录音上传
- [ ] 企业微信通知

#### Phase 7: 集成与部署（1 周）

- [ ] 文本模拟 CLI
- [ ] Docker 多阶段构建
- [ ] CI/CD（GitHub Actions）
- [ ] 端到端集成测试
- [ ] 性能基准测试

### 10.3 里程碑验收标准

| 里程碑 | 标准 |
|--------|------|
| Phase 2 完成 | `go test ./internal/engine/... -cover` 覆盖率 > 90% |
| Phase 4 完成 | 全部 API 端点 httptest 通过 |
| Phase 5 完成 | 文本模拟模式可完成完整对话流程 |
| Phase 7 完成 | 真实通话端到端延迟 < 1.2s |

---

## 11. 前端不变

Web 前端（React + Ant Design）保持不变，API 契约兼容：

- 前端 → `/api/v1/*` → Go HTTP Server
- 请求/响应 JSON 格式保持一致
- Swagger/OpenAPI 文档从 Go handler 注解生成（或手写 OpenAPI spec）
