# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 常用命令

```bash
# 构建全部二进制
task build                          # → bin/clarion, bin/clarion-worker, bin/clarion-postprocessor

# 测试
go test ./...                       # 全量测试
go test ./internal/call/...         # 单包测试
go test -run TestSessionHangup ./internal/call/...  # 单个测试
go test -race -count=1 ./...        # 竞态检测（CI 标准）
task test                           # 竞态 + 覆盖率报告

# Lint
golangci-lint run ./...             # 全量 lint（必须 0 issues）

# 集成测试（需 .env 中的 API Key）
task integration-test               # 全部 provider
task integration-test-llm           # 仅 LLM
task integration-test-tts           # 仅 TTS
task integration-test-asr           # 仅 ASR

# 本地开发环境
task local-up                       # 启动 PG + Redis + FreeSWITCH
task local-down                     # 停止
task schema-up                      # 应用数据库 schema 变更
```

## Git 规范

- commit 信息使用**中文**，描述 why 而非 what
- commit 信息**禁止**包含 AI 相关信息（不加 Co-Authored-By 等 AI 标识）

## 代码规范

- 注释语言：**中文**（包括包文档、函数注释、行内注释）
- 非测试代码禁止 `//nolint`
- 函数不超 80 行，圈复杂度 ≤ 15，嵌套复杂度 ≤ 4
- 错误必须包装（`%w` 或 `errors.Join`）
- 禁止 `utils`、`helpers`、`common` 等万能包名
- 接口定义在**使用方**所在的包中，尽量小（1-3 个方法）
- 所有外部依赖通过接口注入，无全局变量，无 `init()`
- 编译期检查接口实现：`var _ ASRProvider = (*QwenASR)(nil)`

## 技术栈

- Go 1.25+，标准库优先
- PostgreSQL (pgx/v5) + Redis (go-redis/v9)
- 配置：TOML (koanf/v2)，环境变量覆盖格式 `CLARION_{SECTION}_{KEY}`
- 日志：log/slog（结构化 JSON）
- 测试：testing + testify
- Lint：golangci-lint v2（严格模式，配置见 `.golangci.yml`）
- HTTP 路由：Go 1.22+ 标准库 `net/http`，无框架

## 架构概览

Clarion 是 AI 外呼语音引擎，基于 [Sonata](https://github.com/omeyang/Sonata) 核心库构建。三进程架构：

```
API Server (cmd/clarion)  →  Redis Queue  →  Call Worker (cmd/worker)
                                                    ↓ Redis Stream
                                            Post-Processing Worker (cmd/postprocessor)
```

- **API Server**：HTTP REST API，任务管理和通话查询
- **Call Worker**：消费任务队列，通过 FreeSWITCH ESL 发起呼叫，运行实时 ASR→LLM→TTS 管线
- **Post-Processing Worker**：消费通话完成事件，持久化、摘要、商机提取、通知

### 双状态机

核心设计是 **Media FSM**（控制何时说话）与 **Dialogue FSM**（控制说什么）的分离：

- **Media FSM** (`internal/engine/media/fsm.go`)：IDLE → DIALING → RINGING → AMD → BOT_SPEAKING ↔ USER_SPEAKING → HANGUP
- **Dialogue FSM** (`internal/engine/dialogue/`)：Opening → Qualification → InformationGathering → ObjectionHandling → NextAction → Closing

两个 FSM 独立运行，通过 Session 协调。

### 会话管线

`internal/call/session.go` 编排单通电话，Session 被拆分为职责文件：
- `session_audio.go` — 音频收发
- `session_dialogue.go` — 对话流程
- `session_esl.go` — FreeSWITCH ESL 控制
- `session_tts.go` — TTS 合成与播放
- `session_filler.go` — 等待期间填充音频
- `session_hybrid.go` — Omni-Realtime 混合模式
- `session_speculative.go` — ASR 稳定时提前推测 LLM

### Provider 体系

Provider 接口是 Sonata 类型别名（`internal/provider/*.go`），实现在子包：
- `internal/provider/asr/` — 通义听悟（WebSocket 流式）
- `internal/provider/llm/` — DeepSeek（SSE 流式）
- `internal/provider/tts/` — DashScope（WebSocket 流式），含连接池 `pool.go`
- `internal/provider/realtime/` — Qwen3-Omni-Flash-Realtime（混合模式）
- `internal/provider/strategy/` — 异步对话策略（混合模式）

### 安全防护

`internal/guard/` 提供对话安全防护链：
- `CallBudget` — Token/时间预算控制
- `ResponseValidator` — 回复格式校验
- `ContentChecker` — 内容安全过滤
- `OffTopicTracker` — 跑题检测
- `OutputChecker` — 输出安全校验

### 韧性模块

`internal/resilience/` 提供容错原语：熔断器、重试、降级回退。

## 设计文档

`design/00-architecture.md` 是**项目灵魂**，第 2 章「工程原则」是最高纲领。其余设计文档 `design/01-07` 覆盖目标约束、架构概览、实时会话、通信媒体、对话引擎、数据模型、部署运维。
