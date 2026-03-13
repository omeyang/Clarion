# 02 - 总体架构概览

---

## 目录

1. [总体架构概览](#1-总体架构概览)
2. [三类进程架构](#2-三类进程架构)
3. [用户交互层](#3-用户交互层)
4. [领域配置与业务编排层](#4-领域配置与业务编排层)
5. [AI能力层 — 流式Provider架构](#5-ai能力层--流式provider架构)
6. [数据存储层](#6-数据存储层)

---

## 1. 总体架构概览

系统采用**分层架构**设计，将关注点清晰隔离，同时在关键路径上引入**进程分离**，确保实时音频处理不受管理面操作的影响。

### 1.1 六层架构

整体系统由以下六层组成：

| 层级 | 名称 | 核心职责 |
|------|------|----------|
| L1 | **用户交互层** | 管理后台（Web UI）+ 企业微信通知 |
| L2 | **领域配置与业务编排层** | 通用业务编排内核 + 领域适配组件 |
| L3 | **实时会话运行时** | Call Worker 进程、Media Proxy、流式管道 |
| L4 | **通信控制层** | FreeSWITCH — 外呼发起、SIP 管理、音频收发 |
| L5 | **AI 能力层** | ASR / LLM / TTS Provider（全部为流式接口） |
| L6 | **数据存储层** | PostgreSQL + Redis + OSS |

### 1.2 架构总览图

![总体架构](./images/02-01-overall-architecture.png)

> 图注：对应 Mermaid 源文件 `diagrams/02-01-overall-architecture.mmd`

### 1.3 各层交互要点

**L1 → L2（用户交互层 → 领域配置与业务编排层）**

- 管理后台通过 HTTP 管理面 API 完成任务创建、名单导入、模板管理、结果查询等操作。
- 企业微信通知双向集成：管理面主动推送高优联系人、任务完成、异常提醒等通知。

**L2 → L3（领域配置与业务编排层 → 实时会话运行时）**

- 管理面将外呼任务通过 Redis 任务队列派发给 Call Worker。
- Call Worker 取任务后，按照领域配置（场景模板、状态机配置、话术模板等）驱动实时会话。

**L3 → L4（实时会话运行时 → 通信控制层）**

- Call Worker 通过 WebSocket / ESL 与 FreeSWITCH 交互，控制外呼发起、媒体流收发。
- FreeSWITCH 负责底层 SIP 信令和 RTP 媒体处理，将音频帧传递给 Call Worker。

**L3 → L5（实时会话运行时 → AI 能力层）**

- Call Worker 内的流式管道将音频帧实时推送至 ASR Provider，获取语音识别的部分/最终结果。
- 识别结果送入 LLM Provider 进行流式推理，生成回复 token 流。
- Token 流实时送入 TTS Provider 进行流式语音合成，生成音频 chunk 回传给 FreeSWITCH 播放。

**L3 → L6（实时会话运行时 → 数据存储层）**

- 通话完成后，Call Worker 将完成事件推入 Redis 事件流。
- Post-Processing Worker 消费事件，将结果写入 PostgreSQL，录音上传至 OSS。

**L2 ↔ L6（领域配置与业务编排层 ↔ 数据存储层）**

- HTTP 管理面直接读写 PostgreSQL，完成联系人、任务、模板等数据的 CRUD。

### 1.4 关键设计原则

1. **实时与管理分离** — 实时音频处理（Call Worker）与管理面（HTTP Server）运行在不同进程中，避免 HTTP 请求抖动影响通话质量。
2. **流式优先** — AI 能力层全部采用流式接口，减少首字延迟，提升对话自然度。
3. **领域可配置** — 通过领域适配组件（场景模板、状态机配置、字段 Schema、分级规则、话术模板），同一套内核可适配不同外呼场景。
4. **异步后处理** — 通话完成后的摘要生成、录音上传、通知推送等重操作异步完成，不阻塞通话资源。
5. **Provider 可替换** — ASR / LLM / TTS 均通过统一的流式接口抽象，MVP 使用云服务，后续可无缝切换为本地部署方案。

---

## 2. 三类进程架构

系统拆分为**三类独立进程**，每类进程有不同的运行特征和资源需求。

### 2.1 进程架构总览

![进程架构](./images/02-02-process-architecture.png)

> 图注：对应 Mermaid 源文件 `diagrams/02-02-process-architecture.mmd`

### 2.2 HTTP 管理面

**定位：** 处理所有非实时的管理操作，面向运营人员和系统管理员。

**核心职责：**

- **任务 CRUD** — 创建、查询、暂停、恢复、取消外呼任务
- **模板管理** — 场景模板、话术模板、状态机配置的创建与维护
- **结果查询** — 通话记录、摘要、录音的查询与导出
- **名单导入** — Excel / CSV 名单上传与联系人管理

**运行特征：**

| 特性 | 说明 |
|------|------|
| 协议 | HTTP/HTTPS |
| 延迟要求 | 常规 Web 应用级别（百毫秒） |
| 并发模型 | Go net/http，goroutine per request |
| 实例数 | 单实例或多实例（负载均衡） |
| 重启影响 | 不影响进行中的通话 |

**关键点：** 管理面进程的重启、升级、慢查询等问题不会影响正在进行的实时通话，这是进程分离带来的核心收益。

### 2.3 Call Worker 进程（通话面）

**定位：** 处理实时音频通话，管理通话的完整生命周期。

**核心职责：**

- **FreeSWITCH WebSocket 连接** — 维护与 FreeSWITCH 的双向媒体通道
- **媒体状态机** — 管理通话中的媒体状态切换（振铃、接通、静音、挂断等）
- **流式管道编排** — 驱动 VAD → ASR → LLM → TTS 的实时流式管道
- **业务状态机** — 根据领域配置执行对话策略（开场白、信息收集、回答异议、结束语等）

**运行特征：**

| 特性 | 说明 |
|------|------|
| 延迟要求 | 严格实时（毫秒级） |
| 并发模型 | goroutine + channel |
| 实例数 | 可多实例，每实例处理 N 路并发通话 |
| 资源配置 | CPU 密先，需保证调度优先级 |
| 通信方式 | 通过 Redis 接收任务、发布事件 |

**扩缩容策略：**

- 每个 Call Worker 实例支持一定数量的并发通话（具体取决于硬件资源和 AI Provider 响应速度）。
- 需要更多并发通话时，水平扩展 Call Worker 实例。
- 每个实例独立连接 FreeSWITCH 和 AI Provider，无共享状态（会话状态在进程内存中）。

### 2.4 Post-Processing Worker（后处理面）

**定位：** 异步处理通话完成后的后续操作，不占用实时资源。

**核心职责：**

- **摘要生成** — 基于通话记录调用 LLM 生成结构化摘要
- **结果写入 PostgreSQL** — 将通话结果、摘要、标签等持久化到数据库
- **录音上传 OSS** — 将 FreeSWITCH 生成的录音文件上传到对象存储
- **企业微信通知** — 针对高优联系人或异常情况推送通知

**运行特征：**

| 特性 | 说明 |
|------|------|
| 延迟要求 | 无严格要求（秒级到分钟级可接受） |
| 并发模型 | 事件驱动，从 Redis 消费事件 |
| 实例数 | 单实例或多实例 |
| 重启影响 | 不影响通话，仅暂时延迟后处理 |
| 幂等性 | 所有操作必须幂等，支持重试 |

### 2.5 三类进程协作流程

1. **管理面** 创建外呼任务，将任务信息写入 PostgreSQL，同时将待外呼条目推入 Redis 任务队列。
2. **Call Worker** 从 Redis 任务队列中取出外呼条目，通过 FreeSWITCH 发起呼叫，驱动流式 AI 管道完成对话。
3. 通话完成后，**Call Worker** 将通话完成事件（含通话记录、录音路径等）推入 Redis 事件流。
4. **Post-Processing Worker** 从 Redis 事件流中消费事件，执行摘要生成、数据落库、录音上传、通知推送等操作。

### 2.6 进程间通信机制

三类进程之间通过 **Redis** 进行松耦合通信：

| 通信场景 | Redis 数据结构 | 说明 |
|----------|---------------|------|
| 任务派发 | List（BRPOP） | 管理面 LPUSH，Call Worker BRPOP |
| 通话完成事件 | Stream（XADD/XREADGROUP） | Call Worker XADD，后处理面 XREADGROUP |
| 任务状态同步 | Hash | Call Worker 更新，管理面查询 |
| 分布式锁 | String + SETNX | 防止重复外呼 |
| 限流控制 | Sorted Set / Token Bucket | 控制外呼并发速率 |

---

## 3. 用户交互层

### 3.1 管理后台

管理后台是运营人员操作系统的主要入口，提供 Web UI 界面。

**核心功能：**

- **名单管理** — 上传联系人名单（Excel/CSV），查看、编辑、删除联系人
- **任务管理** — 创建外呼任务，配置外呼时间段、并发数、重试策略
- **模板管理** — 创建和编辑场景模板、话术模板
- **结果查看** — 查看通话记录、收听录音、查看 AI 摘要
- **数据导出** — 导出通话结果为 Excel/CSV

### 3.2 企业微信通知

通过企业微信机器人或应用消息，向相关人员推送通知。

**通知场景：**

- **高优联系人推送** — 当通话中识别到高意向或紧急联系人时，实时推送给对应销售/客服
- **任务完成通知** — 外呼任务批次完成后，推送完成率、接通率等汇总数据
- **异常告警** — 系统异常（FreeSWITCH 断连、ASR 服务不可用等）实时告警

### 3.3 当前版本不做的功能

以下功能已识别但不在 MVP 范围内：

- 实时通话监听（Supervisor 功能）
- 通话中人工介入/转接
- 多租户与权限管理
- 移动端 APP
- 通话数据的 BI 分析看板
- A/B 测试（话术模板对比）

---

## 4. 领域配置与业务编排层

### 4.1 通用业务编排内核

业务编排内核运行在 **HTTP 管理面** 中，负责外呼任务的全生命周期管理。

**核心原则：**

1. **领域无关** — 内核不包含任何特定行业的业务逻辑，所有行业特性通过领域适配组件注入。
2. **配置驱动** — 外呼流程通过配置而非代码定义，运营人员可自行调整。
3. **状态可追溯** — 任务和通话的每次状态变更均有事件记录，支持回溯和审计。

**内核管理的实体：**

| 实体 | 说明 |
|------|------|
| 联系人（Contact） | 被叫信息，含电话、姓名、自定义字段 |
| 外呼任务（Task） | 一批联系人 + 一个场景模板 + 调度配置 |
| 外呼条目（TaskItem） | 任务中的每一条待外呼记录 |
| 通话记录（CallRecord） | 一次通话的完整信息 |
| 场景模板（ScenarioTemplate） | 定义对话流程的配置 |

**任务生命周期：**

```
DRAFT → PENDING → RUNNING → PAUSED → COMPLETED
                          ↘ CANCELLED
```

- `DRAFT` — 任务创建但未提交
- `PENDING` — 已提交，等待调度
- `RUNNING` — 正在执行外呼
- `PAUSED` — 手动暂停
- `COMPLETED` — 所有条目处理完成
- `CANCELLED` — 手动取消

### 4.2 领域适配组件

领域适配组件通过**配置**而非**代码**来定义特定场景的外呼行为，使系统可快速适配不同行业和场景。

**组件构成：**

#### 4.2.1 场景模板（Scenario Template）

定义一次外呼对话的完整流程，包括：

- **对话阶段** — 开场白、身份确认、信息传递、信息收集、异议处理、结束语
- **各阶段话术** — 每个阶段的 LLM 提示词模板
- **转移条件** — 阶段间的切换条件

#### 4.2.2 状态机配置

定义对话中的状态转移规则：

- **状态列表** — 对话可能处于的所有状态
- **触发条件** — 从一个状态到另一个状态的触发条件（用户意图、超时、特定关键词等）
- **动作绑定** — 进入/离开某状态时执行的动作

#### 4.2.3 字段 Schema

定义特定场景需要从对话中提取的结构化字段：

```json
{
  "scene": "insurance_renewal",
  "fields": [
    {"name": "intention", "type": "enum", "values": ["renew", "cancel", "undecided"]},
    {"name": "preferred_time", "type": "datetime", "required": false},
    {"name": "concern", "type": "text", "required": false}
  ]
}
```

#### 4.2.4 分级规则

定义联系人意向分级的规则：

- **分级维度** — 意向度、紧急度、匹配度等
- **评分规则** — 基于对话内容和提取字段的评分逻辑
- **阈值配置** — 各等级的分数阈值

#### 4.2.5 话术模板

为 LLM 提供的提示词模板，支持变量替换：

- 支持 Go text/template 模板语法
- 变量来源：联系人信息、对话历史、提取字段、系统配置
- 分阶段管理：每个对话阶段可配置不同的提示词

---

## 5. AI能力层 — 流式Provider架构

### 5.1 设计理念

AI 能力层采用**全链路流式处理**。

**为什么必须流式：**

| 指标 | 批次模式 | 流式模式 |
|------|----------|----------|
| ASR 延迟 | 等待说完 → 整句识别 | 边说边识别，实时出部分结果 |
| LLM 首 token 延迟 | 等待完整输入 → 完整输出 | 流式输入 → 流式输出 |
| TTS 首音频延迟 | 等待完整文本 → 完整合成 | 收到首批 token 即开始合成 |
| 端到端延迟 | 各环节延迟累加 | 各环节流水线并行，大幅降低 |
| 用户体感 | 长时间沉默后突然回复 | 自然、连贯的对话节奏 |

### 5.2 Provider 架构图

![Provider架构](./images/02-03-provider-architecture.png)

> 图注：对应 Mermaid 源文件 `diagrams/02-03-provider-architecture.mmd`

### 5.3 流式 Provider 接口定义

所有 Provider 必须实现统一的流式接口，Call Worker 通过这些接口与 AI 服务交互。

#### 5.3.1 ASR Provider 接口

```go
type ASRStream interface {
    FeedAudio(ctx context.Context, chunk []byte) error
    Events() <-chan ASREvent
    Close() error
}

type ASRProvider interface {
    StartStream(ctx context.Context, cfg ASRConfig) (ASRStream, error)
}
```

**关键设计点：**

- `feed_audio` 持续接收音频帧，无需等待用户说完。
- `on_partial` 提供中间结果，可用于实时显示或提前触发 LLM 推理。
- `on_endpoint` 检测到用户停顿时触发，是启动 LLM 回复的主要信号。

#### 5.3.2 LLM Provider 接口

```go
type LLMProvider interface {
    GenerateStream(ctx context.Context, messages []Message, cfg LLMConfig) (<-chan string, error)
}
```

**关键设计点：**

- 返回 `<-chan string`，每个 token 立即发送，无需等待完整回复。
- 配合 TTS 的流式合成，实现"边想边说"的效果。
- 支持 DeepSeek SSE 协议。

#### 5.3.3 TTS Provider 接口

```go
type TTSProvider interface {
    SynthesizeStream(ctx context.Context, textCh <-chan string, cfg TTSConfig) (<-chan []byte, error)
    Cancel() error
}
```

**关键设计点：**

- `synthesize_stream` 接收 token 流作为输入，不需要完整文本。
- 返回音频 chunk 流，每个 chunk 可立即送入 FreeSWITCH 播放。
- `cancel` 支持即时取消，应对用户打断场景。

### 5.4 MVP Provider 实现

| Provider | MVP 选型 | 备选方案 | 切换成本 |
|----------|----------|----------|----------|
| ASR | 阿里云实时语音识别 | FunASR（本地部署） | 低（统一接口） |
| LLM | DeepSeek（流式 SSE） | — | — |
| TTS | 阿里云流式语音合成 | CosyVoice（本地部署） | 低（统一接口） |

**MVP 选型理由：**

- **阿里云 ASR** — 实时识别延迟低、中文效果好、有成熟的 WebSocket 流式接口。
- **DeepSeek** — 中文推理能力强、支持 SSE 流式输出、性价比高。
- **阿里云 TTS** — 流式合成延迟低、音色自然、支持 SSML。

**后续演进：**

- 当通话量增长到一定规模时，可将 ASR 切换为本地部署的 FunASR 以降低成本。
- TTS 可切换为 CosyVoice 本地部署，支持更多音色定制。
- 切换过程只需实现新的 Provider 类并修改配置，不影响 Call Worker 和业务逻辑。

### 5.5 流式管道串联

在 Call Worker 中，三个 Provider 通过流式管道串联工作：

```
用户语音（音频帧流）
    ↓ feed_audio()
ASR Provider
    ↓ on_final() / on_endpoint()
用户文本（识别结果）
    ↓ 构造 messages
LLM Provider
    ↓ generate_stream() → token 流
AI 回复文本（token 流）
    ↓ synthesize_stream()
TTS Provider
    ↓ audio chunk 流
FreeSWITCH 播放
```

**流水线并行的关键：**

- LLM 产生第一个 token 时，TTS 就开始合成。
- TTS 产生第一个 audio chunk 时，FreeSWITCH 就开始播放。
- 整个链路的首音频延迟 = ASR端点延迟 + LLM首token延迟 + TTS首chunk延迟。
- 相比批次模式，端到端延迟可降低 50% 以上。

---

## 6. 数据存储层

### 6.1 存储技术选型

系统使用三种存储技术，各自承担不同的职责：

| 存储 | 技术 | 用途 |
|------|------|------|
| 关系数据库 | PostgreSQL | 结构化业务数据的持久存储 |
| 内存数据库 | Redis | 实时状态、队列、缓存、锁 |
| 对象存储 | OSS（阿里云） | 录音文件、导出文件等大文件 |

### 6.2 PostgreSQL

承担系统的核心业务数据存储，包括：

- **联系人数据** — 电话、姓名、自定义字段、来源批次
- **任务数据** — 任务配置、状态、调度信息
- **通话记录** — 通话时长、接通状态、对话文本、ASR 原始结果
- **模板数据** — 场景模板、话术模板、状态机配置
- **事件日志** — 系统事件、状态变更记录

> 详细的数据模型设计请参见文档 `06-数据模型设计`。

### 6.3 Redis

在系统中承担多种实时职责：

| 用途 | 数据结构 | 说明 |
|------|----------|------|
| 外呼任务队列 | List | 管理面派发，Call Worker 消费 |
| 通话完成事件流 | Stream | Call Worker 发布，后处理面消费 |
| 会话状态缓存 | Hash | 通话中的临时状态（对话历史等） |
| 分布式锁 | String + SETNX | 防止同一联系人被重复外呼 |
| 限流控制 | 多种 | 控制外呼并发速率和 API 调用频率 |
| 任务状态同步 | Hash | Call Worker 与管理面之间的状态同步 |

### 6.4 OSS（对象存储）

用于存储大文件：

- **通话录音** — FreeSWITCH 生成的录音文件，通话完成后由 Post-Processing Worker 上传
- **导出文件** — 管理后台导出的 Excel/CSV 结果文件
- **录音访问** — 通过预签名 URL 提供时效性访问，无需暴露存储凭证

### 6.5 数据流向总结

```
管理面写入:   PostgreSQL ← 联系人/任务/模板
任务派发:     Redis ← 外呼条目队列
实时通话:     Redis ← 会话状态（临时）
通话完成:     Redis ← 完成事件
后处理写入:   PostgreSQL ← 通话记录/摘要/结果
录音存储:     OSS ← 录音文件
```

> 详细的数据模型、表结构、索引策略请参见文档 `06-数据模型设计`。

---

## 附录

### A. 相关文档

| 文档编号 | 标题 | 说明 |
|----------|------|------|
| 01 | 需求与范围 | 项目背景、MVP 范围定义 |
| 03 | 实时会话运行时 | Call Worker 详细设计 |
| 04 | 通信控制层 | FreeSWITCH 集成方案 |
| 05 | 领域适配详细设计 | 场景模板、状态机配置规范 |
| 06 | 数据模型设计 | PostgreSQL 表结构、Redis 键设计 |

### B. Mermaid 源文件清单

| 文件 | 对应图片 | 说明 |
|------|----------|------|
| `diagrams/02-01-overall-architecture.mmd` | `images/02-01-overall-architecture.png` | 总体架构图 |
| `diagrams/02-02-process-architecture.mmd` | `images/02-02-process-architecture.png` | 三类进程架构图 |
| `diagrams/02-03-provider-architecture.mmd` | `images/02-03-provider-architecture.png` | AI Provider 架构图 |
