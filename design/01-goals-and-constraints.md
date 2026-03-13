# 目标与约束

---

## 1. 项目目标

- **定义 AI 外呼系统的整体架构**，为后续设计、开发与迭代提供统一参照。
- 确立**通用内核 + 领域适配**的架构思路：核心通话引擎（Sonata）与业务逻辑（Clarion）解耦，系统不绑定单一行业。
- 明确**当前第一落地场景为房产线索初筛**，以此驱动 MVP 的功能范围和优先级。
- 承认当前的现实条件：**单人开发、成本敏感、快速验证**，架构选型和工程决策均以此为出发点。
- **预留开源到商业的切换路径**：Sonata 核心引擎开源，商业能力以插件或独立模块形式叠加。

### 不包含

- 未经验证的收益承诺或 ROI 结论。
- 大厂式平台设计（多区域部署、万级并发、企业级权限体系等）。
- 与当前阶段无关的远期规划细节。

---

## 2. 当前阶段目标

### 2.1 最小可运行闭环

MVP 阶段需要跑通的完整闭环：

| # | 能力 | 说明 |
|---|------|------|
| 1 | 外呼发起 | Asynq 任务队列取号 → FreeSWITCH ESL 发起 SIP 呼叫 |
| 2 | 实时语音交互 | ASR（通义听悟）→ LLM（DeepSeek）→ TTS（CosyVoice），全链路流式 |
| 3 | 话术引导 | 基于预设话术模板（房产场景），Dialogue FSM 引导对话完成信息收集 |
| 4 | 意向判定 | 通话过程中规则引擎实时评估 + 结束后 LLM 精判，分级（A/B/C/D/X） |
| 5 | 通话记录 | 录音存储、ASR 转写文本留存、轮次级事件日志 |
| 6 | 结果通知 | 高意向线索通过企业微信实时通知业务人员 |
| 7 | 任务管理 | HTTP API：批量导入号码、设定外呼时段、查看任务进度 |
| 8 | 基础看板 | 接通率、平均通话时长、意向分布等关键指标（OTel 度量） |

### 2.2 需要验证的 5 个核心问题

1. **延迟体验**：ASR → LLM → TTS 全链路延迟能否控制在 < 1.2 秒？推测执行和填充音频能否有效掩盖延迟？
2. **对话质量**：LLM 在房产初筛场景下，能否稳定完成多轮引导并准确提取关键信息？Guard 防护链能否有效拦截跑题和不安全回复？
3. **成本模型**：单通电话的综合成本（通信 + ASR + LLM + TTS）是否在商业可行区间？混合模式（Omni Realtime）是否能降低成本？
4. **通用性验证**：核心引擎在切换到非房产场景（如教育、保险）时，需要改动多少？Sonata 抽象层是否足够？
5. **稳定性底线**：在 AI 组件异常时，熔断器 + 降级链 + 预算控制能否确保优雅降级而非通话卡死？

### 2.3 房产是首个业务模板，不是系统边界

房产线索初筛被选为第一落地场景，原因是：

- 场景结构化程度高（预算、区域、户型、时间），适合 AI 引导式对话。
- 行业对外呼接受度相对较高，合规风险可控。
- 意向判定标准明确，验证闭环短。

架构设计时，房产相关的逻辑（话术模板、意向判定规则、字段提取 schema）作为**业务模板层**存在，通过 Dialogue FSM + 规则引擎配置驱动，与核心通话引擎严格分离。系统边界是「AI 外呼引擎」，而非「房产外呼工具」。

---

## 3. 当前约束与前提

### 3.1 开发前提

- **单人全栈开发**，无专职前端、运维或测试人员。
- 技术选型基于 **Go 1.25+ 生态**，利用其在并发、部署、性能方面的优势。
- **标准库优先**：`net/http`（Go 1.22+ 路由）、`log/slog`、`testing`、`encoding/json`、`text/template`。
- 核心引擎能力提取到独立 Go module **Sonata**（`engine.Session`、`engine/aiface`、`engine/mediafsm`、`engine/pcm`），Clarion 通过 `go.mod` 依赖。
- 配置格式统一使用 **TOML**（`clarion.toml`），通过 koanf/v2 加载，环境变量覆盖格式 `CLARION_{SECTION}_{KEY}`。
- 代码组织以模块清晰、可独立替换为原则，不追求微服务粒度。
- **golangci-lint v2 严格模式**，lint 不通过 = 构建失败，非测试代码禁止 `//nolint`。
- CI/CD 从简，但必须有自动化测试（`-race` + 覆盖率）和 lint 流程。

### 3.2 通信前提

- 使用 **SIP 中继**接入运营商线路，通过 **FreeSWITCH** 处理信令与媒体。
- 音频桥接通过 FreeSWITCH **mod_audio_fork** WebSocket 接口，实现双向 PCM 流传输。
- 初期支持**低并发**（1-5 路同时通话），架构支持水平扩展至 50+ 路。
- ESL 客户端（`internal/call/esl.go`）负责呼叫控制：发起、挂断、事件监听。
- AMD 检测（`internal/call/amd.go`）：区分真人接听、语音信箱、IVR。
- 抖动缓冲（`internal/call/jitter.go`）和网络质量监控（`internal/call/netquality.go`）保障音频质量。

### 3.3 AI 能力前提

#### ASR（语音识别）

- **当前实现**：通义听悟 Qwen ASR（`internal/provider/asr/qwen.go`），WebSocket 流式识别。
- 模型：`qwen3-asr-flash-realtime`，采样率 16kHz。
- 云端方案成熟度高，开箱即用，延迟可控。
- **备用方案**：本地 Paraformer（sherpa-onnx），用于成本优化阶段切换。

#### LLM（大语言模型）

- **当前实现**：DeepSeek（`internal/provider/llm/deepseek.go`），SSE 流式调用。
- DeepSeek 在中文场景下性价比高，响应质量满足初筛对话需求。
- 通过 prompt 工程实现话术引导和信息提取，初期不做微调。
- **Guard 防护链**（`internal/guard/`）对 LLM 输出进行多层校验：
  - `CallBudget` — Token/时间预算控制，防止超支
  - `ResponseValidator` — 回复格式校验
  - `ContentChecker` — 内容安全过滤
  - `OffTopicTracker` — 跑题检测与干预
  - `OutputChecker` — 输出安全最终校验
  - `InputFilter` — 用户输入过滤
  - `DecisionValidator` — 对话决策校验

#### TTS（语音合成）

- **当前实现**：DashScope CosyVoice（`internal/provider/tts/dashscope.go`），WebSocket 流式合成。
- 模型：`cosyvoice-v3.5-plus`，音色 `longanyang`。
- **连接池**（`internal/provider/tts/pool.go`）：预建立 WebSocket 连接，消除建连开销。
- **预合成缓存**（`internal/precompile/`）：开场白等固定话术预合成，零延迟播放。
- **备用方案**：本地 VITS（sherpa-onnx），用于成本优化或定制音色。

#### 混合管线模式（Hybrid Pipeline）

- **Omni Realtime**（`internal/provider/realtime/omni.go`）：Qwen3-Omni-Flash-Realtime 端到端语音模型，直接输入音频、输出音频，跳过 ASR+TTS 环节，极低延迟。
- **Smart Strategy**（`internal/provider/strategy/smart.go`）：混合模式下异步运行高质量 LLM 分析（意图提取、信息整理），与 Omni 实时回复并行。
- 混合模式通过 `session_hybrid.go` 编排，Classic 和 Hybrid 可按配置切换。

### 3.4 韧性前提

系统内建多层韧性机制（`internal/resilience/`）：

| 机制 | 实现 | 说明 |
|------|------|------|
| **熔断器** | `resilience.Breaker` | 三态（Closed/Open/HalfOpen），每个 Provider 独立，连续失败后断路 |
| **重试** | `resilience.Retry` | 指数退避，区分临时/永久错误 |
| **降级回退** | `resilience.Fallback` | 主路径失败时切换到备用路径 |
| **预算控制** | `guard.CallBudget` | Token/时间预算接近上限时主动缩短回复 |
| **会话快照** | `call/snapshot.go` | 通话中间状态可序列化，支持意外中断恢复 |
| **重试调度** | `scheduler/retrysched.go` | 失败通话按策略重新入队 |

### 3.5 抽象层对照

| 抽象层 | 接口 | 当前实现 | 可替换为 |
|--------|------|----------|----------|
| ASR | `aiface.ASRProvider` | 通义听悟 Qwen（`provider/asr/qwen.go`） | 本地 Paraformer（sherpa-onnx）/ Google STT |
| LLM | `aiface.LLMProvider` | DeepSeek（`provider/llm/deepseek.go`） | OpenAI / Claude / 本地模型 |
| TTS | `aiface.TTSProvider` | DashScope CosyVoice（`provider/tts/dashscope.go`） | 本地 VITS（sherpa-onnx）/ Azure TTS |
| 端到端语音 | Realtime Provider | Qwen3-Omni-Flash-Realtime（`provider/realtime/omni.go`） | GPT-4o Realtime / Gemini |
| 对话引擎 | `aiface.DialogEngine` | Clarion Dialogue FSM + Rules | 自定义实现 |
| 音频传输 | `engine.Transport` | FreeSWITCH ESL + mod_audio_fork | WebRTC / SIP direct |
| 通信层 | ESL 客户端 | FreeSWITCH（`call/esl.go`） | Asterisk / Otel |
| 业务模板 | Dialogue FSM + Rules | 房产初筛模板 | 教育/保险/通用模板 |
| 通知渠道 | Notifier | 企业微信（`notify/wechat.go`） | 钉钉 / 飞书 / 短信 |
| 任务队列 | Asynq Client | Redis（`scheduler/client.go`） | RabbitMQ / NATS |
| 存储层 | Store | PostgreSQL pgx/v5 + Redis go-redis/v9 | CockroachDB / 其他 |

---

## 4. 系统能力总览

### 4.1 三进程架构

```
┌──────────────┐     Asynq (Redis)     ┌───────────────┐
│  API Server  │ ──────────────────── → │  Call Worker   │
│ cmd/clarion  │                        │  cmd/worker    │
│              │                        │                │
│ · 模板 CRUD  │                        │ · ESL 呼叫控制 │
│ · 任务管理   │                        │ · ASR→LLM→TTS │
│ · 联系人管理 │                        │ · Media FSM    │
│ · 通话查询   │                        │ · Dialogue FSM │
│ · net/http   │                        │ · Guard 防护   │
└──────────────┘                        │ · 韧性机制     │
                                        └───────┬───────┘
                                                │ Redis Stream
                                        ┌───────▼──────────┐
                                        │ Post-Processor    │
                                        │ cmd/postprocessor │
                                        │                   │
                                        │ · LLM 摘要生成    │
                                        │ · 商机提取        │
                                        │ · PostgreSQL 落库 │
                                        │ · 企业微信通知    │
                                        └───────────────────┘
```

### 4.2 双状态机

核心设计是 **Media FSM**（控制何时说话）与 **Dialogue FSM**（控制说什么）的分离：

- **Media FSM**（`internal/engine/media/`，基于 Sonata `mediafsm.PhoneTransitions()`）：
  ```
  IDLE → DIALING → RINGING → AMD_DETECTING → BOT_SPEAKING ↔ USER_SPEAKING → HANGUP → POST_PROCESSING
  ```
  含打断（BargeIn）、静默超时（SilenceTimeout）、处理中（Processing）等中间状态。

- **Dialogue FSM**（`internal/engine/dialogue/`）：
  ```
  Opening → Qualification → InformationGathering → ObjectionHandling → NextAction → MarkForFollowup → Closing
  ```
  由规则引擎（`internal/engine/rules/`）根据用户意图和已收集信息驱动转换。

两个 FSM 独立运行，通过 Session（`internal/call/session.go`）协调。

### 4.3 Guard 防护链

对话安全防护（`internal/guard/`）对 LLM 的输入和输出进行多层校验：

```
用户输入 → InputFilter → LLM → ResponseValidator → ContentChecker → OutputChecker → 播放
                                                          ↑
                                    OffTopicTracker ──────┘
                                    CallBudget ───────────┘
                                    DecisionValidator ────┘
```

确保 AI 回复安全、合规、不跑题、不超预算。

### 4.4 可观测体系

| 层级 | 工具 | 覆盖范围 |
|------|------|----------|
| 应用层 | OTel 度量（`observe/metrics.go`） | 延迟直方图、错误计数、网络质量 |
| 应用层 | slog 结构化日志 | 全链路事件追踪 |
| 应用层 | pprof | CPU / Heap / Goroutine 分析 |
| 内核层 | eBPF 探针（`observe/ebpf/`，可选） | TCP 延迟、Go 调度延迟 |
| 网络层 | 网络质量监控（`call/netquality.go`） | 抖动、丢包、低音量 |

---

## 5. 设计原则速查

以下原则的完整阐述见 `00-architecture.md` 第 2 章。

| # | 原则 | 一句话 |
|---|------|--------|
| 1 | 实时优先 | 端到端延迟 < 1.2s，所有决策以通话体验为先 |
| 2 | 流式贯穿 | ASR→LLM→TTS channel 串联，句级分割，填充音频，双管线模式 |
| 3 | 热冷分离 | 通话中只做最小闭环，摘要/落库/通知 Redis Stream 异步后处理 |
| 4 | 故障友好 | 熔断器 + 降级链 + 预算控制 + 填充音频 + 会话快照，永不卡死 |
| 5 | 幂等安全 | Asynq 去重 + 幂等写入 + 通知去重 |
| 6 | 接口抽象 | Sonata aiface 接口 + Clarion 类型别名，Provider 可插拔 |
| 7 | 可观测 | OTel 度量 + eBPF 探针 + slog 结构化日志 + 网络质量监控 |
