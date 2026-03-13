# Clarion

AI 外呼语音引擎。基于 [Sonata](https://github.com/omeyang/Sonata) 核心库构建。

Sonata 提供与产品无关的实时语音对话能力（流式 ASR→LLM→TTS 管线、媒体状态机、WebRTC VAD、打断检测），Clarion 在此之上实现完整的电话外呼产品——FreeSWITCH ESL 集成、对话规则引擎、任务调度、后处理管线。

```
Sonata（引擎核心）
  ├── Clarion（AI 外呼）           ← 你在这里
  ├── AI 外语培训 APP（规划中）
  └── AI 儿童故事对讲机（规划中）
```

## 系统架构

三进程通过 Redis 解耦，各自独立部署和扩缩：

```
                    ┌─────────────────────────────────────┐
                    │           API Server                │
                    │  (REST API / 任务管理 / 通话查询)    │
                    └──────────────┬──────────────────────┘
                                   │ Redis Queue
                    ┌──────────────▼──────────────────────┐
                    │          Call Worker                 │
                    │  ┌─────────────────────────────┐    │
                    │  │  Session (per call)          │    │
                    │  │  ESL ←→ 媒体FSM ←→ 对话引擎  │    │
                    │  │  ASR ←→ LLM ←→ TTS (流式)   │    │
                    │  └─────────────────────────────┘    │
                    │  FreeSWITCH ESL + WebSocket Audio   │
                    └──────────────┬──────────────────────┘
                                   │ Redis Stream
                    ┌──────────────▼──────────────────────┐
                    │     Post-Processing Worker          │
                    │  (持久化 / 摘要 / 商机提取 / 通知)   │
                    └─────────────────────────────────────┘
```

| 进程 | 入口 | 职责 |
|------|------|------|
| **API Server** | `cmd/clarion/` | HTTP REST API，任务管理，通话状态和结果查询 |
| **Call Worker** | `cmd/worker/` | 消费任务队列，ESL 控制 FreeSWITCH 发起呼叫，运行实时对话 |
| **Post-Processing Worker** | `cmd/postprocessor/` | 消费通话完成事件，持久化记录，LLM 摘要，商机提取，通知 |

## 核心能力

- **实时语音对话** — 流式 ASR→LLM→TTS 管线，端到端延迟目标 < 1.2s
- **双状态机设计** — Media FSM（控制何时说话）与 Dialogue FSM（控制说什么）独立运行，通过 Session 协调
- **WebRTC VAD** — 基于 WebRTC 的人声检测，抗噪能力强于纯能量阈值
- **Barge-in 打断** — 用户说话时自动打断 AI 回复，实现自然对话
- **AMD 检测** — 答录机/留言机检测，避免浪费通话时长
- **对话引擎** — 规则驱动的 FSM + LLM 智能回复，支持意图识别、字段收集、线索分级（A/B/C/D）
- **推测执行** — ASR 文本稳定时提前触发 LLM，进一步压缩延迟
- **混合模式** — 支持 Omni-Realtime 模型（如 Qwen3-Omni）与传统管线的混合切换
- **安全防护** — Token/时间预算、回复格式校验、内容安全过滤、跑题检测
- **韧性容错** — 熔断器、指数退避重试、降级回退（TTS 超时 → 预合成音频 → 礼貌挂断）
- **任务调度** — 自动重试失败任务，可配置重试策略

## 项目结构

```
clarion/
├── cmd/
│   ├── clarion/              API Server 入口
│   ├── worker/               Call Worker 入口
│   ├── postprocessor/        Post-Processing Worker 入口
│   └── audiotest/            端到端语音管道测试工具
├── internal/
│   ├── api/                  HTTP API 层（路由 + Handler + Schema）
│   ├── service/              业务服务层
│   ├── model/                数据模型（struct + SQL）
│   ├── store/                存储层（PostgreSQL + Redis + OSS）
│   ├── config/               TOML 配置加载
│   ├── call/                 呼叫工作进程
│   │   ├── session*.go       单通电话会话编排（音频/对话/ESL/TTS/填充/混合/推测）
│   │   ├── worker.go         Worker 主循环
│   │   ├── esl.go            FreeSWITCH ESL 客户端
│   │   ├── amd.go            语音信箱检测
│   │   ├── jitter.go         抖动缓冲
│   │   └── netquality.go     网络质量监测
│   ├── engine/               引擎核心
│   │   ├── media/            媒体状态机（Sonata FSM 集成）
│   │   ├── dialogue/         对话 FSM + 对话引擎
│   │   └── rules/            规则引擎（意图→动作映射）
│   ├── provider/             AI Provider
│   │   ├── asr/              通义听悟 ASR（WebSocket 流式）
│   │   ├── llm/              DeepSeek LLM（SSE 流式）
│   │   ├── tts/              DashScope TTS（WebSocket 流式 + 连接池）
│   │   ├── realtime/         Qwen3-Omni Realtime（混合模式）
│   │   └── strategy/         异步对话策略
│   ├── guard/                对话安全防护
│   │   ├── budget.go         Token/时间预算控制
│   │   ├── response.go       回复格式校验
│   │   ├── content.go        内容安全过滤
│   │   ├── offtopic.go       跑题检测
│   │   └── output.go         输出安全校验
│   ├── resilience/           韧性容错
│   │   ├── breaker.go        熔断器
│   │   ├── retry.go          重试（指数退避）
│   │   └── fallback.go       降级回退
│   ├── scheduler/            任务调度与重试
│   ├── observe/              可观测性（指标采集）
│   ├── notify/               通知渠道（飞书等）
│   ├── postprocess/          后处理逻辑
│   ├── precompile/           TTS 预编译（常用话术缓存）
│   └── simulate/             通话模拟（测试用）
├── design/                   设计文档（00-07 共 8 篇）
├── deploy/                   部署配置（本地 / 腾讯云）
├── migrations/               SQL 迁移文件
├── web/                      前端管理界面（React + Ant Design）
├── Taskfile.yml              构建/测试/部署任务
└── Dockerfile                多阶段构建
```

## 快速开始

### 前置要求

- Go 1.25+
- PostgreSQL 15+
- Redis 7+
- FreeSWITCH 1.10+（需安装 mod_audio_fork）
- [Task](https://taskfile.dev/)（任务运行器）

### 本地开发

```bash
# 1. 启动基础设施（PG + Redis + FreeSWITCH）
task local-up

# 2. 数据库迁移
task migrate-up

# 3. 配置
cp deploy/local/clarion-local.toml clarion.toml
# 编辑 clarion.toml，填入 ASR/LLM/TTS 的 API Key

# 4. 启动三个进程（各开一个终端）
go run ./cmd/clarion -c clarion.toml          # API Server
go run ./cmd/worker -c clarion.toml           # Call Worker
go run ./cmd/postprocessor -c clarion.toml    # Post-Processor
```

### 构建

```bash
task build    # → bin/clarion, bin/clarion-worker, bin/clarion-postprocessor
```

### 测试

```bash
task test                       # 竞态检测 + 覆盖率
task lint                       # golangci-lint 全量检查
task bench                      # 基准测试

# 集成测试（需 .env 中的 API Key）
task integration-test           # 全部 Provider
task integration-test-llm       # 仅 LLM
task integration-test-tts       # 仅 TTS
task integration-test-asr       # 仅 ASR
task integration-test-pipeline  # 完整管道（LLM→TTS→ASR）
```

### 端到端语音测试（无需 FreeSWITCH）

```bash
task gen-test-audio       # TTS 生成测试音频
task pipeline-test        # WAV → ASR → LLM → TTS → WAV
```

## 与 Sonata 的关系

[Sonata](https://github.com/omeyang/Sonata) 是产品无关的实时语音对话引擎核心库，提供：

- 流式 ASR→LLM→TTS 管线编排
- 表驱动媒体状态机（预置电话 / APP 两套状态转换）
- WebRTC VAD 人声检测
- Barge-in 打断检测
- Transport 和 dialogue.Engine 两个核心抽象接口

Clarion 作为 Sonata 的第一个产品，实现了 Transport（FreeSWITCH ESL 音频桥接）和 dialogue.Engine（外呼对话引擎），并在此基础上构建了完整的外呼产品能力：

| Sonata 提供 | Clarion 实现 |
|-------------|-------------|
| `Transport` 接口 | ESL + WebSocket 音频桥接 |
| `dialogue.Engine` 接口 | 规则驱动的对话引擎 |
| `media.PhoneTransitions()` | 电话媒体状态机集成 |
| `ASRProvider` / `LLMProvider` / `TTSProvider` | 通义听悟 / DeepSeek / DashScope 实现 |
| — | 任务调度 + REST API + 后处理管线 |
| — | AMD 检测、安全防护、韧性容错 |
| — | 推测执行、混合模式 |

## 技术栈

| 领域 | 选型 | 理由 |
|------|------|------|
| 语言 | Go 1.25+ | goroutine + channel 天然匹配实时音频管线 |
| 核心引擎 | [Sonata](https://github.com/omeyang/Sonata) | 产品无关的语音对话引擎 |
| HTTP | `net/http`（Go 1.22+ 路由） | 标准库，无框架 |
| 数据库 | PostgreSQL (`pgx/v5`) | 社区最快的 PG 驱动 |
| 缓存/队列 | Redis (`go-redis/v9`) | 任务队列 + 事件流 + 会话状态 |
| 配置 | TOML (`koanf/v2`) | 显式类型，支持注释，环境变量覆盖 |
| 日志 | `log/slog` | Go 官方结构化日志 |
| 电话 | FreeSWITCH + mod_audio_fork | ESL 控制 + WebSocket 音频流 |
| ASR | 通义听悟 | WebSocket 流式识别 |
| LLM | DeepSeek | SSE 流式生成 |
| TTS | DashScope CosyVoice | WebSocket 流式合成 + 连接池 |
| Realtime | Qwen3-Omni-Flash | 混合模式（可选） |
| Lint | golangci-lint v2（严格模式） | 零 issue 才能合入 |

## 设计文档

`design/` 目录包含 8 篇设计文档，覆盖从工程原则到部署运维的完整设计：

| 文档 | 内容 |
|------|------|
| `00-architecture.md` | **架构设计总纲** — 工程原则（项目灵魂）、技术栈选型、项目结构 |
| `01-goals-and-constraints.md` | 目标与约束 |
| `02-architecture-overview.md` | 架构概览 |
| `03-realtime-session-runtime.md` | 实时会话运行时 |
| `04-communication-and-media.md` | 通信与媒体 |
| `05-dialogue-engine-and-rules.md` | 对话引擎与规则 |
| `06-data-model-and-storage.md` | 数据模型与存储 |
| `07-deployment-and-ops.md` | 部署与运维 |

## 许可证

本项目采用 [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE) 许可证。

这意味着：

- 你可以自由使用、修改和分发本软件
- 修改后的版本必须同样以 AGPL-3.0 开源
- **通过网络提供服务也需要公开源代码** — 这是 AGPL 与 GPL 的关键区别

详见 [LICENSE](LICENSE) 文件。
