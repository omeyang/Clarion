# 07 - 部署与运维

## 1. 技术选型

### 基础技术栈

| 组件 | 选型 | 说明 |
|---|---|---|
| 语言 | Go 1.25+ | 标准库优先，无 Web 框架 |
| HTTP 路由 | Go 1.22+ `net/http` | 标准库路由，无第三方框架 |
| 数据库 | PostgreSQL（pgx/v5） | 核心业务数据 |
| 缓存/队列 | Redis（go-redis/v9） | Asynq 任务队列、Stream 事件流、会话快照 |
| 任务队列 | Asynq | 基于 Redis 的任务队列框架 |
| 通信 | FreeSWITCH | SIP/呼叫控制，ESL + mod_audio_fork |
| 配置 | TOML（koanf/v2） | 单一配置文件 `clarion.toml`，环境变量覆盖 |
| 日志 | log/slog | 结构化 JSON 日志 |
| Lint | golangci-lint v2 | 严格模式，0 issues |
| 构建 | go-task（Taskfile.yml） | 统一构建/测试/lint 命令 |
| 通知 | 企业微信机器人 | 人工跟进推送 |

### AI 能力栈

| 能力 | 云端实现 | 本地备选（sherpa-onnx） | 说明 |
|---|---|---|---|
| ASR | 通义听悟（WebSocket 流式） | Paraformer（CGO 绑定） | 支持 RacingASR 竞速模式 |
| TTS | DashScope CosyVoice（WebSocket + 连接池） | VITS（CGO 绑定） | 支持 TieredTTS 分层模式 |
| LLM | DeepSeek（SSE 流式） | — | 同步/流式双接口 |
| VAD | — | Silero（CGO 绑定） | 本地 VAD |
| Realtime | Qwen3-Omni-Flash-Realtime | — | 混合管线模式 |

### 技术取舍说明

**为什么 Go：**

- goroutine + channel 天然适合实时音频流水线，无需 async/await 关键字污染
- 单二进制部署，Docker 镜像 < 30MB
- 编译期类型安全，接口检查在编译时完成（`var _ ASRProvider = (*QwenASR)(nil)`）
- 内置 pprof/trace 性能分析，内置 testing/benchmark 框架
- 无 GIL，真正的多核并行处理

**为什么用 Asynq 而非裸 Redis List：**

- Asynq 提供任务重试、超时、优先级、唯一性等开箱即用的能力
- 内置监控 dashboard，方便排查任务状态
- 基于 Redis，与现有基础设施统一

**为什么有 sherpa-onnx 本地推理：**

- RacingASR：本地 Paraformer 与云端通义听悟竞速，降低 ASR 延迟
- TieredTTS：短文本（填充语、确认词）走本地 VITS 极低延迟合成，长文本走云端
- 离线场景：网络不稳定时可降级到纯本地推理
- CGO 绑定，无需额外部署服务

**为什么 FreeSWITCH 现在就上：**

- 外呼系统核心是通话控制，FreeSWITCH 是事实标准
- mod_audio_fork 支持实时音频流转发，与流式管道天然匹配
- ESL 提供完整的通话控制能力
- 延迟引入会导致后期架构返工

---

## 2. 部署架构

![部署架构](./images/07-01-deployment.png)

> Mermaid 源文件：[diagrams/07-01-deployment.mmd](./diagrams/07-01-deployment.mmd)

### 单机部署方案

整体部署在 **1 台 Linux 服务器** 上。

**容器化服务（Podman Compose）：**

- PostgreSQL
- Redis
- FreeSWITCH（可选容器化或主机部署）

**Go 二进制（直接运行）：**

| 二进制 | 入口 | 职责 |
|---|---|---|
| `bin/clarion` | `cmd/clarion/` | API Server（HTTP 管理面） |
| `bin/clarion-worker` | `cmd/worker/` | Call Worker（实时通话） |
| `bin/clarion-postprocessor` | `cmd/postprocessor/` | Post-Processor（异步后处理） |

**构建命令：**

```bash
task build    # 构建全部三个二进制到 bin/ 目录
```

**外部依赖：**

- 通义听悟 ASR API（WebSocket）
- DeepSeek LLM API（SSE）
- DashScope TTS API（WebSocket）
- 企业微信（通知推送）
- 电话线路（SIP trunk）

### 配置管理

**TOML 配置文件：** `clarion.toml`

通过 koanf/v2 加载，支持环境变量覆盖，格式为 `CLARION_{SECTION}_{KEY}`。

关键配置段：

- `[server]` — HTTP 监听地址、端口
- `[worker]` — 并发数、Asynq 配置
- `[database]` — PostgreSQL 连接
- `[redis]` — Redis 连接
- `[freeswitch]` — ESL 地址、WebSocket 端口
- `[asr]` — ASR Provider 选择及参数
- `[llm]` — LLM Provider 选择及参数
- `[tts]` — TTS Provider 选择及参数
- `[pipeline]` — 管线模式（classic / hybrid）

**默认 Provider 配置：**

```
ASR    = qwen      （通义听悟）
LLM    = deepseek
TTS    = dashscope （CosyVoice）
管线   = classic
```

### 本地推理部署（可选）

启用 sherpa-onnx 本地推理需要：

- 下载对应的 ONNX 模型文件
- 在 `clarion.toml` 中配置模型路径
- 构建时启用 CGO（`CGO_ENABLED=1`）

可用的本地推理能力：

| 能力 | 模型 | 用途 |
|---|---|---|
| ASR | Paraformer | RacingASR 竞速、离线降级 |
| TTS | VITS | TieredTTS 短文本合成 |
| VAD | Silero | 本地语音活动检测 |
| Speaker | 说话人嵌入 | 说话人识别 |

### 当前阶段不做

- Kubernetes
- 多机高可用
- 服务网格
- 微服务拆分

> 单人维护阶段，复杂度是最大敌人。单机方案足以支撑 MVP 验证。

---

## 3. 模块划分

![模块划分](./images/07-02-modules.png)

> Mermaid 源文件：[diagrams/07-02-modules.mmd](./diagrams/07-02-modules.mmd)

### API Server 模块（bin/clarion）

| Go 包 | 职责 |
|---|---|
| `api/router.go` | HTTP 路由注册（Go 1.22+ 标准库） |
| `api/handler/` | HTTP Handler，请求解析与响应 |
| `api/schema/` | 请求/响应 JSON Schema |
| `api/middleware.go` | 中间件：日志、Recovery |
| `service/` | 业务逻辑层 |
| `store/` | 数据访问层（pgx/v5） |

### Call Worker 模块（bin/clarion-worker）

| Go 包 | 职责 |
|---|---|
| `call/worker.go` | Asynq Handler、WebSocket Server |
| `call/session.go` | Session 编排主体 |
| `call/esl/` | FreeSWITCH ESL 连接与控制 |
| `call/amd/` | 应答机检测（Answering Machine Detection） |
| `engine/media/fsm.go` | Media FSM — 媒体状态机 |
| `engine/dialogue/` | Dialogue FSM — 对话状态机与引擎 |
| `engine/rules/` | 规则引擎 |
| `guard/` | 安全防护链（预算、校验、内容安全、跑题检测） |
| `resilience/` | 韧性模块（熔断、重试、降级） |
| `precompile/` | 预编译（话术模板预处理） |
| `observe/` | 可观测性（指标采集） |

### Post-Processor 模块（bin/clarion-postprocessor）

| Go 包 | 职责 |
|---|---|
| `postprocess/worker.go` | Redis Stream 事件消费 |
| `postprocess/summary.go` | LLM 摘要生成 |
| `postprocess/opportunity.go` | 商机提取 |
| `postprocess/writer.go` | PostgreSQL 结果写入 |
| `notify/` | 企业微信通知推送 |

### Provider 模块（共享）

| Go 包 | 实现 | 说明 |
|---|---|---|
| `provider/asr/qwen.go` | 通义听悟 | WebSocket 流式 ASR |
| `provider/llm/deepseek.go` | DeepSeek | SSE 流式 LLM |
| `provider/tts/dashscope.go` | DashScope CosyVoice | WebSocket 流式 TTS |
| `provider/tts/pool.go` | 连接池 | TTS WebSocket 连接复用 |
| `provider/realtime/omni.go` | Qwen3-Omni-Flash-Realtime | 混合管线端到端模型 |
| `provider/strategy/smart.go` | Smart Strategy | 异步业务分析策略 |

### Sonata 核心库（独立模块）

| Go 包 | 职责 |
|---|---|
| `engine/session.go` | Session 核心抽象 |
| `engine/transport.go` | Transport 接口 |
| `engine/aiface/` | ASR/LLM/TTS/DialogEngine 接口定义 |
| `engine/mediafsm/` | 媒体状态机 |
| `engine/pcm/` | 音频处理 |
| `sherpa/asr.go` | Paraformer 本地 ASR |
| `sherpa/tts.go` | VITS 本地 TTS |
| `sherpa/vad.go` | Silero VAD |
| `sherpa/raceasr.go` | 云端/本地 ASR 竞速 |
| `sherpa/tieredtts.go` | 分层 TTS |
| `sherpa/speaker.go` | 说话人嵌入 |

---

## 4. 可观测性

### 结构化日志

使用 Go 标准库 `log/slog`，输出结构化 JSON 日志。每通通话通过 `call_id` 贯穿全链路，支持日志关联查询。

### OTel 指标

通过 `observe/` 包采集 OpenTelemetry 指标：

| 指标 | 说明 |
|---|---|
| 通话时长 | 每通通话的总时长分布 |
| ASR/LLM/TTS 延迟 | 各 Provider 的响应延迟 |
| 首音频延迟 | 从用户说完到 AI 开始回复的延迟 |
| 抖动（jitter） | 音频流的抖动情况 |
| 丢包率（loss rate） | 音频包丢失比例 |
| 通话成功率 | 接通/完成/异常的比例 |

### eBPF 可观测性（可选）

高级可观测性能力，需要 `CAP_BPF` 权限：

- **TCP 追踪** — 监控与 AI Provider 的 WebSocket/SSE 连接延迟和重连
- **调度延迟** — 监控 goroutine 调度延迟，识别 CPU 争抢导致的音频卡顿
- **网络诊断** — 抓取特定连接的网络包，用于排查音频质量问题

### 性能分析

内置 Go pprof endpoint，支持运行时：

- CPU profiling
- 内存分配分析
- Goroutine 栈分析
- Block/Mutex profiling

---

## 5. 常用运维命令

### go-task 命令

通过 `Taskfile.yml` 统一管理构建和运维命令：

```bash
# 构建
task build                          # 构建全部二进制 → bin/

# 测试
task test                           # 竞态检测 + 覆盖率报告
task integration-test               # 全部 Provider 集成测试
task integration-test-llm           # 仅 LLM
task integration-test-tts           # 仅 TTS
task integration-test-asr           # 仅 ASR

# 本地开发环境
task local-up                       # 启动 PG + Redis + FreeSWITCH（Podman Compose）
task local-down                     # 停止
task schema-up                      # 应用数据库 schema 变更

# Lint
golangci-lint run ./...             # 全量 lint（必须 0 issues）
```

### 环境变量覆盖

配置优先级：环境变量 > clarion.toml > 默认值

```bash
# 示例：覆盖数据库连接
CLARION_DATABASE_HOST=localhost
CLARION_DATABASE_PORT=5432

# 示例：覆盖 Provider
CLARION_ASR_PROVIDER=qwen
CLARION_LLM_PROVIDER=deepseek
CLARION_TTS_PROVIDER=dashscope
```

---

## 6. 成本度量方法

### 已知成本项

| 成本项 | 计费方式 | 备注 |
|---|---|---|
| 服务器 | 月租 | 单机 Linux，按配置计费 |
| 通信测试 | 按通话时长 | SIP 线路费用 |
| DeepSeek API | 按 token 计费 | input/output 分别计价 |
| 通义听悟 ASR | 按识别时长计费 | WebSocket 实时语音识别 |
| DashScope TTS | 按合成字符数计费 | CosyVoice 流式语音合成 |

### 度量方法

每通通话完成后记录计量数据：

```
- asr_duration_ms:     ASR 调用总时长（毫秒）
- llm_input_tokens:    LLM 输入 token 数
- llm_output_tokens:   LLM 输出 token 数
- tts_characters:      TTS 合成字符数
- call_duration_ms:    通话总时长（毫秒）
```

**本地推理降本：**

- 启用 RacingASR 后，部分 ASR 请求由本地 Paraformer 处理，减少云端调用
- 启用 TieredTTS 后，短文本（填充语、确认词等）由本地 VITS 合成，减少 DashScope 调用量

---

## 7. 合规与风险控制

### 通话策略要求

| 要求 | 说明 |
|---|---|
| 开场说明身份 | 通话开始必须表明来电方身份和来意 |
| 拒绝时立即结束 | 用户明确拒绝后，礼貌结束通话 |
| 支持黑名单 | 维护不可拨打号码列表 |
| 不重复骚扰 | 同一号码在指定时间窗口内不重复拨打 |

### 安全防护

`internal/guard/` 包提供对话安全防护链：

| 模块 | 职责 |
|---|---|
| CallBudget | Token/时间预算控制，防止单通通话超限 |
| ResponseValidator | 回复格式校验 |
| ContentChecker | 内容安全过滤 |
| OffTopicTracker | 跑题检测 |
| OutputChecker | 输出安全校验 |

### 韧性设计

`internal/resilience/` 包提供容错原语：

- **熔断器** — Provider 连续失败时熔断，避免雪崩
- **重试** — 可配置的重试策略
- **降级回退** — Provider 不可用时降级到备选方案

### 风险与应对

| 风险 | 应对策略 |
|---|---|
| 云端 ASR/TTS 延迟波动 | RacingASR 竞速 + TieredTTS 分层 + 填充音频缓冲 |
| LLM 回复偏离预期 | guard 安全防护链 + 双状态机约束 |
| DeepSeek API 超时/限流 | resilience 熔断 + 填充话术 + 降级模板回复 |
| 重复外呼 | Asynq 唯一性 + 分布式锁 |
| 单人维护复杂度 | 三进程分离降低耦合，go-task 统一命令 |

---

## 8. 后续演进方向

### AI 服务演进

```
当前：云端 API（通义听悟 + DashScope + DeepSeek）+ sherpa-onnx 本地辅助
  ↓ 通话量增长
目标：更多场景使用本地推理，降低云端依赖和成本
```

### 工程化演进

- **链路追踪**：OTel trace 贯穿全链路
- **部署隔离**：开发/测试/生产环境分离
- **自动化测试**：基于文本模拟模式的回归测试套件
- **eBPF 深度可观测**：TCP 追踪、调度延迟分析
