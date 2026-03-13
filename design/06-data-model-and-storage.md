# 06 数据模型与存储设计

---

## 1. 数据模型原则

在设计数据模型时，遵循以下核心原则：

| # | 原则 | 说明 |
|---|------|------|
| 1 | **先少后多** | 初期只建必要的表和字段，随业务验证后再扩展，避免过度设计 |
| 2 | **只保留能支撑闭环的数据** | 每个字段都应服务于某个明确的业务流程或度量指标，无法关联到具体用途的字段不入库 |
| 3 | **核心表承载通用结构，领域差异通过 JSON Schema 表达** | 主表字段保持通用性，不同行业（房产、金融、教育等）的差异化字段放入 `profile_json`、`extracted_fields` 等 JSONB 列中，通过 JSON Schema 进行校验 |
| 4 | **避免把房产专属字段写死在主表** | 如"意向楼盘"、"预算区间"等行业特有字段，绝不出现在核心表的固定列中 |
| 5 | **通话事件独立记录** | 媒体层事件（语音开始/结束、打断、超时等）与对话内容分离，写入独立的 `call_events` 表，便于调试、度量和回放 |
| 6 | **模板快照机制** | 每通通话绑定一个不可变的模板快照（`template_snapshot_id`），即使模板后续修改，也能精确还原当时的对话逻辑和提示词 |

---

## 2. 核心实体关系

![ER图](./images/06-01-er-diagram.png)

> 对应 Mermaid 源文件：`docs/diagrams/06-01-er-diagram.mmd`

### 实体关系概览

```
SCENARIO_TEMPLATES ──1:N──▶ TEMPLATE_SNAPSHOTS
SCENARIO_TEMPLATES ──1:N──▶ CALL_TASKS
TEMPLATE_SNAPSHOTS ──1:N──▶ CALL_TASKS
TEMPLATE_SNAPSHOTS ──1:N──▶ CALLS
CONTACTS           ──1:N──▶ CALLS
CALL_TASKS         ──1:N──▶ CALLS
CALLS              ──1:N──▶ DIALOGUE_TURNS
CALLS              ──1:N──▶ CALL_EVENTS
```

### 2.1 contacts — 联系人

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `phone_masked` | string | 脱敏后的手机号（展示用，如 `138****1234`） |
| `phone_hash` | string | 手机号哈希值（用于去重和查询） |
| `source` | string | 来源渠道 |
| `profile_json` | jsonb | 联系人画像（行业差异字段存于此） |
| `current_status` | string | 当前状态（如 new / contacted / interested / rejected） |
| `do_not_call` | boolean | 是否禁止拨打 |
| `created_at` | datetime | 创建时间 |

### 2.2 scenario_templates — 场景模板

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `name` | string | 模板名称 |
| `domain` | string | 所属行业领域 |
| `opening_script` | text | 开场白脚本 |
| `state_machine_config` | jsonb | 状态机配置 |
| `extraction_schema` | jsonb | 信息提取 Schema |
| `grading_rules` | jsonb | 评级规则 |
| `prompt_templates` | jsonb | 提示词模板集合 |
| `notification_config` | jsonb | 通知配置 |
| `call_protection_config` | jsonb | 通话保护参数（超时阈值、降级策略等） |
| `precompiled_audios` | jsonb | 预合成音频文件列表 |
| `status` | string | 模板状态（draft / active / archived） |
| `version` | int | 版本号 |
| `created_at` | datetime | 创建时间 |

### 2.3 template_snapshots — 运行时快照
| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `template_id` | bigint (FK) | 关联场景模板 |
| `snapshot_data` | jsonb | 模板完整数据快照 |
| `created_at` | datetime | 创建时间 |

> **不可变约束**：快照记录一旦创建，不允许 UPDATE 或 DELETE。应用层和数据库层（触发器）双重保障。

### 2.4 call_tasks — 外呼任务

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `name` | string | 任务名称 |
| `scenario_template_id` | bigint (FK) | 关联场景模板 |
| `template_snapshot_id` | bigint (FK) | 关联模板快照（任务创建时生成） |
| `contact_filter` | jsonb | 联系人筛选条件 |
| `schedule_config` | jsonb | 调度配置（时间窗口、重试策略等） |
| `daily_limit` | int | 每日外呼上限 |
| `max_concurrent` | int | 最大并发通话数 |
| `status` | string | 任务状态（pending / running / paused / completed） |
| `created_at` | datetime | 创建时间 |

### 2.5 calls — 通话记录

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `contact_id` | bigint (FK) | 关联联系人 |
| `task_id` | bigint (FK) | 关联外呼任务 |
| `template_snapshot_id` | bigint (FK) | 关联模板快照（用于精确回放） |
| `session_id` | string | 会话唯一标识 |
| `status` | string | 通话状态 |
| `answer_type` | string | 接听类型（human / voicemail / ivr / unknown） |
| `duration` | int | 通话时长（秒） |
| `record_url` | string | 录音文件地址 |
| `transcript` | text | 完整转写文本 |
| `extracted_fields` | jsonb | 提取的结构化信息 |
| `result_grade` | string | 结果评级 |
| `next_action` | string | 后续动作 |
| `rule_trace` | jsonb | 规则执行轨迹 |
| `ai_summary` | text | AI 生成的通话摘要 |
| `created_at` | datetime | 创建时间 |

### 2.6 dialogue_turns — 对话轮次

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `call_id` | bigint (FK) | 关联通话 |
| `turn_number` | int | 轮次序号 |
| `speaker` | string | 说话方（user / bot） |
| `content` | text | 说话内容 |
| `state_before` | string | 本轮开始前的业务状态 |
| `state_after` | string | 本轮结束后的业务状态 |
| `asr_latency_ms` | int | ASR 识别耗时（毫秒） |
| `llm_latency_ms` | int | LLM 推理耗时（毫秒） |
| `tts_latency_ms` | int | TTS 合成耗时（毫秒） |
| `asr_confidence` | float | ASR 置信度（0.0 ~ 1.0） |
| `is_interrupted` | boolean | 本轮是否被打断 |
| `created_at` | datetime | 创建时间 |

### 2.7 call_events — 通话事件日志
| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | bigint (PK) | 主键 |
| `call_id` | bigint (FK) | 关联通话 |
| `event_type` | string | 事件类型 |
| `timestamp_ms` | bigint | 事件发生的毫秒级时间戳 |
| `metadata_json` | jsonb | 事件元数据 |
| `created_at` | datetime | 创建时间 |

**event_type 枚举值：**

| 事件类型 | 说明 |
|----------|------|
| `user_speech_start` | 用户开始说话 |
| `user_speech_end` | 用户结束说话 |
| `bot_speak_start` | 机器人开始说话 |
| `bot_speak_end` | 机器人结束说话 |
| `barge_in` | 用户打断机器人 |
| `silence_timeout` | 静默超时 |
| `asr_error` | ASR 识别错误 |
| `llm_timeout` | LLM 推理超时 |
| `tts_error` | TTS 合成错误 |
| `hangup_by_user` | 用户挂断 |
| `hangup_by_system` | 系统挂断 |
| `amd_result` | 答录机检测结果 |

---

## 3. 实体说明

### 3.1 已有字段说明

- **phone_masked / phone_hash**：手机号不明文存储。`phone_masked` 用于界面展示，`phone_hash` 用于唯一性判断和查询。真实号码仅在外呼执行时从加密存储中获取。
- **profile_json**：以 JSONB 存储联系人画像。不同行业的差异化属性（如房产的意向楼盘、教育的年级等）通过 JSON Schema 校验，避免主表字段膨胀。
- **state_machine_config**：定义对话状态机的完整配置，包含状态列表、转移条件和各状态对应的行为。
- **extraction_schema**：定义本场景需要提取的结构化字段及其类型约束。
- **grading_rules**：通话结束后的自动评级规则（如 A/B/C/D 级客户）。
- **prompt_templates**：各状态节点对应的 LLM 提示词模板。
- **notification_config**：通话结束后的通知规则（触发条件、通知渠道、通知内容模板）。
- **contact_filter**：任务关联的联系人筛选条件（如来源、状态、标签等），以 JSON 表达。
- **schedule_config**：调度策略配置，包括时间窗口、重试间隔、重试次数上限等。
- **rule_trace**：记录评级和后续动作的规则匹配过程，用于结果可解释性。

### 3.2 补充字段说明

#### template_snapshot_id（快照绑定）

每通通话绑定一个不可变的模板快照。当任务创建或模板更新时，系统自动生成新快照。通话记录通过 `template_snapshot_id` 指向创建时刻的模板全量数据，即使模板后续被修改或删除，也能精确回放当时的对话逻辑、提示词和评级规则。

#### answer_type（接听类型）

通话接通后，通过 AMD（Answering Machine Detection）识别对端类型：

| 值 | 说明 |
|----|------|
| `human` | 真人接听 |
| `voicemail` | 语音信箱 |
| `ivr` | IVR 自动应答 |
| `unknown` | 无法判断 |

不同接听类型会触发不同的处理流程：真人进入正常对话，语音信箱可留言或挂断，IVR 尝试按键转人工。

#### call_protection_config（通话保护参数）

在场景模板中配置通话保护策略，包括：

- 单轮 ASR 超时阈值
- LLM 推理超时阈值
- TTS 合成超时阈值
- 连续错误次数上限
- 降级话术配置
- 最大通话时长

这些参数由 Call Engine 在运行时读取并执行。

#### precompiled_audios（预合成音频）

场景模板中可配置需要预合成的音频列表，例如：

- 开场白
- 常见固定回复（"好的，感谢您的时间"）
- 降级话术

预合成音频在模板发布时生成并上传至对象存储，运行时直接播放，避免 TTS 延迟。

#### call_events（通话事件日志）

媒体事件独立于对话内容，记录通话过程中的所有底层事件。其用途包括：

- **调试**：还原通话过程中每个事件的时序
- **度量**：统计打断率、静默超时率、ASR 错误率等
- **回放**：结合录音和事件时间轴，可视化还原通话全过程
- **告警**：通过事件流实时检测异常模式（如连续 ASR 错误）

#### state_before / state_after（状态转移记录）

在 `dialogue_turns` 中记录每一轮对话前后的业务状态。例如某轮对话中用户表达了购买意向，状态可能从 `needs_analysis` 转移到 `interested`。这为状态机调试和对话流程优化提供了精确的数据支撑。

#### asr_confidence（ASR 置信度）

ASR 引擎返回的识别置信度（0.0 ~ 1.0）。用于：

- 数据质量评估：低置信度的转写结果需要人工复核
- 对话策略：置信度过低时可主动要求用户重复
- 模型优化：筛选低置信度样本用于 ASR 模型微调

#### is_interrupted（打断标记）

标记本轮对话是否被打断（即用户在机器人说话过程中插话）。用于：

- 分析打断率，评估话术节奏是否合理
- 区分正常轮次与被打断轮次的效果差异
- 优化 barge-in 检测灵敏度

---

## 4. Redis 用途

系统使用 Redis 承担多种运行时职责，与 PostgreSQL 形成互补：

### 4.1 会话状态

通话进行中的会话上下文存储在 Redis 中，包括当前状态机节点、已提取字段、对话历史摘要等。Key 格式：

```
session:{session_id}
```

TTL 设置为通话最大时长 + 缓冲时间（如 30 分钟），通话结束后由 Post-Processing Worker 持久化到 PostgreSQL。

### 4.2 临时上下文

对话过程中产生的临时数据（如 ASR 中间结果、LLM 上下文窗口等），不需要持久化，仅在通话生命周期内有效。

### 4.3 任务队列

外呼任务派发使用 Redis List 或 Stream 实现：

- Scheduler 将待拨打的联系人推入队列
- Dialer Worker 消费队列并发起呼叫
- 支持优先级队列（多个 List 按优先级消费）

### 4.4 事件流

通话完成事件通过 Redis Stream 发布，供下游 Post-Processing Worker 消费：

```
XADD call_completed * session_id {id} status {status} ...
```

消费组机制保证每条事件至少被处理一次。

### 4.5 分布式锁

用于需要互斥的操作，如：

- 模板发布（防止并发发布同一模板）
- 任务状态变更（防止并发修改）

### 4.6 去重与节流

- API 请求去重（幂等 key）
- 通知发送频率限制
- 外呼频率控制

### 4.7 在途通话计数
使用 Redis 原子计数器跟踪当前在途通话数，供 Admission Control（准入控制）使用：

```
INCR  inflight:calls          -- 发起呼叫时 +1
DECR  inflight:calls          -- 通话结束时 -1
INCR  inflight:task:{task_id} -- 任务级计数
DECR  inflight:task:{task_id}
```

当在途通话数达到 `max_concurrent` 限制时，Scheduler 暂停派发新任务。

### 4.8 联系人外呼去重锁
防止同一联系人在短时间内被重复拨打：

```
SET contact_lock:{contact_id}:{task_id} 1 NX EX 3600
```

- `NX`：仅在 key 不存在时设置（原子操作）
- `EX 3600`：1 小时后自动过期
- 拨打前检查锁是否存在，存在则跳过

---

## 5. 对象存储

系统使用对象存储（OSS）保存以下类型的文件：

| 类型 | 路径规则 | 说明 |
|------|----------|------|
| 通话录音 | `recordings/{yyyy}/{MM}/{dd}/{session_id}.wav` | 完整通话录音，保留周期按合规要求配置 |
| 导出文件 | `exports/{task_id}/{timestamp}.csv` | 任务数据导出文件（通话列表、统计报表等） |
| 预合成音频 | `precompiled/{template_id}/{version}/{audio_name}.wav` | 模板预合成音频文件 |

### 存储选型建议

- **初期**：使用云厂商 OSS（阿里云 OSS / 腾讯 COS / AWS S3），开箱即用，按量付费
- **后期**：如有私有化部署需求，可切换至 MinIO，接口兼容 S3 协议，业务代码无需修改

### 预合成音频管理
预合成音频的生命周期：

1. 模板发布时，系统读取 `precompiled_audios` 配置
2. 调用 TTS 引擎批量合成音频文件
3. 上传至对象存储，路径包含 `template_id` 和 `version`
4. 运行时 Call Engine 按需拉取（支持本地缓存）
5. 模板新版本发布时生成新音频，旧版本音频在快照过期后清理

---

## 6. 幂等性与数据安全

### 6.1 防重复外呼

双重保障机制：

- **数据库层**：`calls` 表对 `(contact_id, task_id)` 添加唯一约束（同一任务同一联系人仅允许一条有效通话记录）
- **Redis 层**：拨打前设置去重锁（见 4.8 节），即使数据库写入有延迟，也能在内存层拦截重复请求

```sql
ALTER TABLE calls ADD CONSTRAINT uq_calls_contact_task
    UNIQUE (contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');
```

> 注意：使用 PostgreSQL 的部分唯一索引（Partial Unique Index），允许失败和未接通的记录存在多条（支持重试）。

### 6.2 通知幂等

每次通知发送携带幂等 key，防止因消息重复消费导致重复通知：

```
idempotent_key = sha256(call_id + notification_type + recipient)
```

通知服务在发送前检查该 key 是否已处理，已处理则跳过。

### 6.3 数据持久化优先级

通话过程中的关键节点数据采用"先 Redis 后 PostgreSQL"的写入策略：

```
通话事件 ──▶ Redis Stream (call_events_stream)
                    │
                    ▼
           Post-Processing Worker
                    │
                    ▼
           PostgreSQL (calls / dialogue_turns / call_events)
```

- **Redis Stream**：保证写入速度，不阻塞通话主流程
- **Post-Processing Worker**：异步消费并批量写入 PostgreSQL
- **At-Least-Once**：消费组 + ACK 机制保证数据不丢失
- **补偿机制**：定时任务扫描 Redis 中未 ACK 的消息，重新投递

### 6.4 模板快照不可变

模板快照遵循 Write-Once-Read-Many 原则：

- 应用层：快照相关 API 仅提供创建和查询接口，不提供更新和删除接口
- 数据库层：通过触发器拒绝 UPDATE 和 DELETE 操作

```sql
CREATE OR REPLACE FUNCTION prevent_snapshot_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'template_snapshots is immutable: UPDATE and DELETE are not allowed';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_snapshot_mutation
    BEFORE UPDATE OR DELETE ON template_snapshots
    FOR EACH ROW EXECUTE FUNCTION prevent_snapshot_mutation();
```

---

## 7. 索引建议

### 7.1 contacts 表

```sql
-- profile_json GIN 索引，支持 JSONB 查询（如按行业、标签筛选联系人）
CREATE INDEX idx_contacts_profile ON contacts USING GIN (profile_json);

-- phone_hash 唯一索引，支持去重查询
CREATE UNIQUE INDEX idx_contacts_phone_hash ON contacts (phone_hash);

-- source + status 复合索引，支持按来源和状态筛选
CREATE INDEX idx_contacts_source_status ON contacts (source, current_status);
```

### 7.2 calls 表

```sql
-- 任务 + 状态复合索引，支持任务维度的通话查询和统计
CREATE INDEX idx_calls_task_status ON calls (task_id, status);

-- 联系人索引，支持查询某联系人的通话历史
CREATE INDEX idx_calls_contact ON calls (contact_id);

-- 创建时间索引，支持时间范围查询和分页
CREATE INDEX idx_calls_created_at ON calls (created_at);

-- 部分唯一索引，防重复外呼（见 6.1 节）
CREATE UNIQUE INDEX uq_calls_contact_task ON calls (contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');
```

### 7.3 call_events 表

```sql
-- 通话 + 事件类型复合索引，支持按类型查询某通通话的事件
CREATE INDEX idx_call_events_call_type ON call_events (call_id, event_type);

-- 时间戳索引，支持事件时序分析
CREATE INDEX idx_call_events_timestamp ON call_events (call_id, timestamp_ms);
```

### 7.4 dialogue_turns 表

```sql
-- 通话 + 轮次复合索引，支持按顺序查询对话轮次
CREATE INDEX idx_dialogue_turns_call_turn ON dialogue_turns (call_id, turn_number);
```

### 7.5 其他建议

- **分区策略**：`calls` 和 `call_events` 表数据增长快，建议按 `created_at` 做按月 Range 分区
- **归档策略**：超过保留期限的历史数据归档至冷存储（对象存储中的 Parquet 文件），减轻在线库压力
- **索引维护**：定期执行 `REINDEX` 和 `VACUUM ANALYZE`，保持索引效率
