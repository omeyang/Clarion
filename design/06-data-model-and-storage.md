# 06 - 数据模型与存储设计

---

## 1. PostgreSQL 数据模型

### 1.1 实体关系

![ER图](./diagrams/06-01-er-diagram.mmd)

```
SCENARIO_TEMPLATES ──1:N──▶ TEMPLATE_SNAPSHOTS
SCENARIO_TEMPLATES ──1:N──▶ CALL_TASKS
TEMPLATE_SNAPSHOTS ──1:N──▶ CALL_TASKS
TEMPLATE_SNAPSHOTS ──1:N──▶ CALLS
CONTACTS           ──1:N──▶ CALLS
CALL_TASKS         ──1:N──▶ CALLS
CALLS              ──1:N──▶ DIALOGUE_TURNS
CALLS              ──1:N──▶ CALL_EVENTS
CALLS              ──1:N──▶ OPPORTUNITIES
```

### 1.2 contacts — 联系人

对应 `internal/model/contact.go` 的 `Contact` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `phone_masked` | `string` | 脱敏手机号（展示用，如 `138****1234`） |
| `phone_hash` | `string` | 手机号哈希值（去重和查询） |
| `source` | `string` | 来源渠道 |
| `profile_json` | `json.RawMessage` | 联系人画像（JSONB，行业差异字段存于此） |
| `current_status` | `string` | 当前状态（new / contacted / interested / rejected） |
| `do_not_call` | `bool` | 是否禁止拨打 |
| `created_at` | `time.Time` | 创建时间 |
| `updated_at` | `time.Time` | 更新时间 |

### 1.3 scenario_templates — 场景模板

对应 `internal/model/template.go` 的 `ScenarioTemplate` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `name` | `string` | 模板名称 |
| `domain` | `string` | 所属行业领域 |
| `opening_script` | `string` | 开场白脚本 |
| `state_machine_config` | `json.RawMessage` | 状态机配置（JSONB） |
| `extraction_schema` | `json.RawMessage` | 信息提取 Schema（JSONB） |
| `grading_rules` | `json.RawMessage` | 评级规则（JSONB） |
| `prompt_templates` | `json.RawMessage` | 提示词模板集合（JSONB） |
| `notification_config` | `json.RawMessage` | 通知配置（JSONB） |
| `call_protection_config` | `json.RawMessage` | 通话保护参数（JSONB） |
| `precompiled_audios` | `json.RawMessage` | 预合成音频文件列表（JSONB） |
| `status` | `engine.TemplateStatus` | 模板状态（draft / active / published / archived） |
| `version` | `int` | 版本号 |
| `created_at` | `time.Time` | 创建时间 |
| `updated_at` | `time.Time` | 更新时间 |

### 1.4 template_snapshots — 运行时快照

对应 `internal/model/template.go` 的 `TemplateSnapshot` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `template_id` | `int64` (FK) | 关联场景模板 |
| `snapshot_data` | `json.RawMessage` | 模板完整数据快照（JSONB） |
| `created_at` | `time.Time` | 创建时间 |

不可变约束：快照一旦创建，不允许 UPDATE 或 DELETE。

### 1.5 call_tasks — 外呼任务

对应 `internal/model/task.go` 的 `CallTask` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `name` | `string` | 任务名称 |
| `scenario_template_id` | `int64` (FK) | 关联场景模板 |
| `template_snapshot_id` | `*int64` (FK) | 关联模板快照（可空，任务创建时生成） |
| `contact_filter` | `json.RawMessage` | 联系人筛选条件（JSONB） |
| `schedule_config` | `json.RawMessage` | 调度配置（JSONB） |
| `daily_limit` | `int` | 每日外呼上限 |
| `max_concurrent` | `int` | 最大并发通话数 |
| `status` | `engine.TaskStatus` | 任务状态（draft / pending / running / paused / completed / cancelled） |
| `total_contacts` | `int` | 总联系人数 |
| `completed_contacts` | `int` | 已完成联系人数 |
| `created_at` | `time.Time` | 创建时间 |
| `updated_at` | `time.Time` | 更新时间 |

### 1.6 calls — 通话记录

对应 `internal/model/call.go` 的 `Call` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `contact_id` | `int64` (FK) | 关联联系人 |
| `task_id` | `int64` (FK) | 关联外呼任务 |
| `template_snapshot_id` | `*int64` (FK) | 关联模板快照 |
| `session_id` | `string` | 会话唯一标识 |
| `status` | `engine.CallStatus` | 通话状态（pending / dialing / ringing / in_progress / completed / failed / no_answer / busy / voicemail / interrupted） |
| `answer_type` | `engine.AnswerType` | 接听类型（human / voicemail / ivr / unknown） |
| `duration` | `int` | 通话时长（秒） |
| `record_url` | `string` | 录音文件地址 |
| `transcript` | `string` | 完整转写文本 |
| `extracted_fields` | `json.RawMessage` | 提取的结构化信息（JSONB） |
| `result_grade` | `engine.Grade` | 结果评级（A/B/C/D/X） |
| `next_action` | `string` | 后续动作 |
| `rule_trace` | `json.RawMessage` | 规则执行轨迹（JSONB） |
| `ai_summary` | `string` | AI 生成的通话摘要 |
| `created_at` | `time.Time` | 创建时间 |
| `updated_at` | `time.Time` | 更新时间 |

### 1.7 dialogue_turns — 对话轮次

对应 `internal/model/call.go` 的 `DialogueTurn` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `call_id` | `int64` (FK) | 关联通话 |
| `turn_number` | `int` | 轮次序号 |
| `speaker` | `string` | 说话方（user / bot） |
| `content` | `string` | 说话内容 |
| `state_before` | `string` | 本轮开始前的对话状态 |
| `state_after` | `string` | 本轮结束后的对话状态 |
| `asr_latency_ms` | `int` | ASR 识别耗时（毫秒） |
| `llm_latency_ms` | `int` | LLM 推理耗时（毫秒） |
| `tts_latency_ms` | `int` | TTS 合成耗时（毫秒） |
| `asr_confidence` | `float32` | ASR 置信度（0.0 ~ 1.0） |
| `is_interrupted` | `bool` | 本轮是否被打断 |
| `created_at` | `time.Time` | 创建时间 |

写入幂等性：`ON CONFLICT (call_id, turn_number) DO NOTHING`。

### 1.8 call_events — 通话事件日志

对应 `internal/model/call.go` 的 `CallEvent` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `id` | `int64` | 主键 |
| `call_id` | `int64` (FK) | 关联通话 |
| `event_type` | `engine.EventType` | 事件类型 |
| `timestamp_ms` | `int64` | 事件发生的毫秒级时间戳 |
| `metadata_json` | `json.RawMessage` | 事件元数据（JSONB） |
| `created_at` | `time.Time` | 创建时间 |

**event_type 枚举值**（Sonata 通用 + Clarion 专有）：

| 来源 | 事件类型 | 说明 |
|------|----------|------|
| Sonata | `user_speech_start` | 用户开始说话 |
| Sonata | `user_speech_end` | 用户结束说话 |
| Sonata | `bot_speak_start` | 机器人开始说话 |
| Sonata | `bot_speak_end` | 机器人结束说话 |
| Sonata | `barge_in` | 用户打断机器人 |
| Sonata | `silence_timeout` | 静默超时 |
| Sonata | `asr_error` | ASR 识别错误 |
| Sonata | `llm_timeout` | LLM 推理超时 |
| Sonata | `tts_error` | TTS 合成错误 |
| Clarion | `hangup_by_user` | 用户挂断 |
| Clarion | `hangup_by_system` | 系统挂断 |
| Clarion | `amd_result` | 答录机检测结果 |
| Clarion | `poor_network_detected` | 网络质量差 |
| Clarion | `audio_gap_detected` | 音频间断 |
| Clarion | `low_volume_detected` | 低音量 |
| Clarion | `unexpected_disconnect` | 意外中断 |

写入幂等性：`ON CONFLICT (call_id, event_type, timestamp_ms) DO NOTHING`。

### 1.9 opportunities — 商机记录

对应 `internal/postprocess/opportunity.go` 的 `Opportunity` 结构体。

| 字段 | Go 类型 | 说明 |
|------|---------|------|
| `call_id` | `int64` (FK, Unique) | 关联通话（每通通话最多一条商机） |
| `contact_id` | `int64` (FK) | 关联联系人 |
| `task_id` | `int64` (FK) | 关联任务 |
| `score` | `int` | 综合意向分（0-100） |
| `intent_type` | `string` | 意向类型（interested / not_interested / needs_info / callback / unknown） |
| `budget_signal` | `string` | 预算信号（has_budget / no_budget / not_mentioned） |
| `timeline_signal` | `string` | 时间线信号（urgent / soon / later / not_mentioned） |
| `contact_role` | `string` | 联系人角色（decision_maker / user / receptionist / unknown） |
| `pain_points` | `[]string` | 痛点列表（JSONB） |
| `followup_action` | `string` | 跟进动作（follow_up / abandon / transfer_human / schedule_callback） |
| `followup_date` | `*time.Time` | 跟进日期 |
| `needs_human_review` | `bool` | 是否需要人工复核 |
| `created_at` | `time.Time` | 创建时间 |
| `updated_at` | `time.Time` | 更新时间 |

写入幂等性：`ON CONFLICT (call_id) DO UPDATE`，同一通话的商机只保留最新版本。

---

## 2. Redis 数据结构

### 2.1 任务队列（Asynq）

外呼任务派发使用 Asynq 队列（基于 Redis），不是原始 Redis List/Stream。Asynq 提供任务调度、重试、超时等能力。

### 2.2 事件流

通话完成事件通过 Redis Stream 发布，供后处理 Worker 消费。

**Stream Key**：`clarion:call_completed`

**载荷结构**（`postprocess.CallCompletionEvent`）：

```go
type CallCompletionEvent struct {
    CallID          int64                  // 通话 ID
    ContactID       int64                  // 联系人 ID
    TaskID          int64                  // 任务 ID
    Grade           engine.Grade           // 评级
    CollectedFields map[string]string      // 已收集字段
    Turns           []dialogue.Turn        // 对话轮次
    Events          []engine.RecordedEvent // 媒体事件
    Summary         string                 // 摘要（可选，后处理时生成）
    NextAction      string                 // 后续动作
    DurationSec     int                    // 通话时长（秒）
    ShouldNotify    bool                   // 是否发送通知
    ContactName     string                 // 联系人姓名
    ContactPhone    string                 // 联系人电话
}
```

消费方式：XReadGroup + 消费组，保证每条事件至少被处理一次（At-Least-Once），处理完成后 XAck。

### 2.3 会话快照

通话进行中的会话状态快照存储在 Redis 中，用于通话恢复。

**Key 格式**：`clarion:session:{call_id}`

**存储方式**：Redis String，包含序列化的 `SnapshotData`（对话状态、轮次、已收集字段）

**TTL**：通话最大时长 + 缓冲时间

恢复流程：从 Redis 读取快照 → `RestoreFromSnapshot` 还原对话引擎 → 使用恢复开场白继续对话

---

## 3. 后处理流程

### 3.1 架构

后处理由独立进程 `cmd/postprocessor` 运行，消费 Redis Stream 中的通话完成事件。核心组件：

| 组件 | 实现文件 | 职责 |
|------|----------|------|
| `Worker` | `internal/postprocess/worker.go` | 消费循环、消息分发 |
| `Summarizer` | `internal/postprocess/summarizer.go` | LLM 摘要生成 |
| `OpportunityExtractor` | `internal/postprocess/opportunity.go` | 商机提取 |
| `Writer` | `internal/postprocess/writer.go` | PostgreSQL 持久化 |

### 3.2 处理步骤

Worker 每条消息的处理流程：

1. **反序列化**——从 Redis Stream 消息中解析 `CallCompletionEvent`
2. **摘要生成**——若事件中无摘要且 Summarizer 可用，调用 LLM 生成结构化摘要
3. **商机提取**——若 OpportunityExtractor 已配置，从通话数据中提取商机信息
4. **结果持久化**——通过 Writer 写入 PostgreSQL：
   - `WriteCallResult` — 更新 calls 表（状态、评级、摘要、字段等）
   - `WriteTurns` — 插入 dialogue_turns（批量，pgx Batch）
   - `WriteEvents` — 插入 call_events（批量，pgx Batch）
   - `WriteOpportunity` — 插入/更新 opportunities
5. **发送通知**——若 `ShouldNotify == true` 且 Notifier 已配置，推送跟进通知

### 3.3 商机提取

`OpportunityExtractor` 支持两种模式：

| 模式 | 触发条件 | 说明 |
|------|----------|------|
| **LLM 提取** | LLM 可用 | 调用 LLM 分析对话记录，输出结构化商机（score/intent_type/budget_signal 等） |
| **规则回退** | LLM 不可用或调用失败 | 基于评级映射（A→85分/interested，B→60分/needs_info，C→30分，D→10分） |

LLM 提取使用低温度（0.1）确保输出稳定，响应 JSON 经过枚举校验和分数裁剪。

### 3.4 幂等性保证

所有写入操作都是幂等的：

| 操作 | 幂等机制 |
|------|----------|
| `WriteCallResult` | `WHERE result_grade IS NULL OR result_grade = ''` 条件更新 |
| `WriteTurns` | `ON CONFLICT (call_id, turn_number) DO NOTHING` |
| `WriteEvents` | `ON CONFLICT (call_id, event_type, timestamp_ms) DO NOTHING` |
| `WriteOpportunity` | `ON CONFLICT (call_id) DO UPDATE` |

### 3.5 通知

通知通过 `notify.Notifier` 接口发送，载荷为 `FollowUpNotification`：

```go
type FollowUpNotification struct {
    CallID          int64
    ContactName     string
    ContactPhone    string
    Grade           string
    Summary         string
    CollectedFields map[string]string
    NextAction      string
}
```

---

## 4. Store 层

### 4.1 PostgreSQL

`internal/store/postgres.go` — 基于 `pgx/v5` 的连接池封装。

Store 按实体拆分为独立文件：

| 文件 | 职责 |
|------|------|
| `call_store.go` | 通话记录、轮次、事件的 CRUD |
| `contact_store.go` | 联系人的 CRUD |
| `task_store.go` | 外呼任务的 CRUD |
| `template_store.go` | 场景模板和快照的 CRUD |

### 4.2 Redis

`internal/store/redis.go` — 基于 `go-redis/v9` 的客户端封装（`RDS` 结构体）。

---

## 附录

### A. 代码索引

| 组件 | 文件路径 |
|------|----------|
| Contact 模型 | `internal/model/contact.go` |
| Call/DialogueTurn/CallEvent 模型 | `internal/model/call.go` |
| CallTask 模型 | `internal/model/task.go` |
| ScenarioTemplate/TemplateSnapshot 模型 | `internal/model/template.go` |
| PostgreSQL 连接 | `internal/store/postgres.go` |
| Redis 连接 | `internal/store/redis.go` |
| 后处理 Worker | `internal/postprocess/worker.go` |
| 摘要生成 | `internal/postprocess/summarizer.go` |
| 商机提取 | `internal/postprocess/opportunity.go` |
| 结果写入 | `internal/postprocess/writer.go` |
