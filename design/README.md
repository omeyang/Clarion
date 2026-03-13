# AI 外呼系统架构文档

> 实现语言：**Go** | 配置格式：**TOML** | 数据库：PostgreSQL + Redis

## 文档结构

本文档按关注点分离为以下部分，建议按顺序阅读：

| 文档 | 内容 | 核心读者 |
|---|---|---|
| [00-架构设计总纲](./00-architecture.md) | 为什么选 Go、工程原则、技术栈选型、配置统一、测试策略 | **所有人（首先阅读）** |
| [01-目标与约束](./01-goals-and-constraints.md) | 项目目标、当前阶段定义、约束条件、设计原则 | 所有人 |
| [02-总体架构](./02-architecture-overview.md) | 六层架构、模块边界、Go interface、领域适配设计 | 架构决策者 |
| [03-实时会话运行时](./03-realtime-session-runtime.md) | 流式管道、媒体状态机、VAD/打断/超时、goroutine + channel | 核心开发 |
| [04-通信与媒体桥接](./04-communication-and-media.md) | FreeSWITCH 集成、音频流路径、Call Worker 架构 | 核心开发 |
| [05-对话引擎与规则](./05-dialogue-engine-and-rules.md) | 业务状态机、规则引擎、LLM 职责边界、分级设计 | 业务开发 |
| [06-数据模型与存储](./06-data-model-and-storage.md) | 核心表设计、Redis 用途、对象存储、事件日志 | 后端开发 |
| [07-部署与运维](./07-deployment-and-ops.md) | 单二进制部署、技术选型、成本度量、合规与风险 | 运维/决策者 |

## 技术栈速览

| 组件 | 选型 |
|------|------|
| 语言 | Go 1.22+ |
| HTTP | net/http (标准库路由) |
| 数据库 | pgx/v5 + sqlx |
| 迁移 | golang-migrate |
| Redis | go-redis/v9 |
| WebSocket | coder/websocket |
| 配置 | TOML (knadh/koanf/v2) |
| 日志 | slog (标准库) |
| 测试 | testing + testify |
| Lint | golangci-lint |

## 图表说明

- 所有 Mermaid 源文件位于 `diagrams/` 目录
- 生成的图片位于 `images/` 目录
- 生成命令：`mmdc -i diagrams/xxx.mmd -o images/xxx.png -w 2400 -H 1600 --backgroundColor white`
