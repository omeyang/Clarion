# Clarion 本地开发部署指南

## 架构概览

```
Linphone (SIP 软电话)
    │  SIP/RTP (UDP)
    ▼
FreeSWITCH (容器, host 网络模式)
    │  WebSocket (音频帧)          │  ESL (TCP:8021, 控制命令)
    ▼                              ▼
Call Worker (本机进程)
    │           │           │
    ▼           ▼           ▼
  ASR(通义)   LLM(DeepSeek)  TTS(DashScope)
```

通话流程：Worker 通过 ESL 发起呼叫 → Linphone 接听 → FreeSWITCH 通过 WebSocket 转发音频 →
Worker 将音频送 ASR 识别 → LLM 生成回复 → TTS 合成语音 → 写 WAV 文件 → ESL 命令 FreeSWITCH 播放。

## 前置条件

- Go 1.25+、GCC（WebRTC VAD 需要 CGO）
- podman + podman-compose
- Linphone（手机 SIP 客户端）

## 目录结构

```
deploy/local/
├── clarion-local.toml       # Worker 配置文件
├── docker-compose.yml       # PG + Redis + FreeSWITCH
├── freeswitch/conf/         # FreeSWITCH 配置（SIP 用户、拨号计划等）
└── README.md                # 本文件

.env                          # API Key（不提交 git）
```

## 快速启动

### 1. 启动基础服务

```bash
cd deploy/local
EXT_IP=<你的公网IP> podman-compose up -d

# 验证服务状态
podman-compose ps
podman exec local_fs_1 fs_cli -x "show registrations"   # 查看 SIP 注册
```

> **EXT_IP** 必须设为服务器公网 IP，否则 Linphone 通过 NAT 无法收到 RTP 音频。

### 2. 配置 API Key

项目根目录创建 `.env`：

```bash
CLARION_LLM_API_KEY=sk-your-deepseek-key
CLARION_ASR_API_KEY=sk-your-dashscope-key
CLARION_TTS_API_KEY=sk-your-dashscope-key
```

### 3. 编译并启动 Worker

```bash
# 在项目根目录
go build -o worker ./cmd/worker/
export $(cat .env | xargs)
./worker -c deploy/local/clarion-local.toml
```

### 4. 注册 Linphone

在 Linphone 中添加 SIP 账号：
- 用户名：`1000`
- 密码：`1234`
- 域名/代理：`<你的公网IP>:5060`
- 传输协议：UDP

验证注册：
```bash
podman exec local_fs_1 fs_cli -x "show registrations"
```

### 5. 发起测试呼叫

```bash
podman exec local_redis_1 redis-cli LPUSH clarion:task_queue \
  '{"call_id":1,"contact_id":1,"task_id":1,"phone":"1000","gateway":"local","caller_id":"8888","template_id":1}'
```

Linphone 应收到来电。

## 配置说明

所有配置在 `clarion-local.toml`，环境变量 `CLARION_{SECTION}_{KEY}` 可覆盖。

### AI 服务

| 配置项 | 说明 | 环境变量 |
|--------|------|----------|
| `asr.api_key` | 通义 ASR（语音识别） | `CLARION_ASR_API_KEY` |
| `llm.api_key` | DeepSeek LLM（对话生成） | `CLARION_LLM_API_KEY` |
| `tts.api_key` | DashScope TTS（语音合成） | `CLARION_TTS_API_KEY` |
| `llm.model` | LLM 模型 | 默认 `deepseek-chat` |
| `tts.voice` | TTS 音色 | 默认 `longanyang` |

### FreeSWITCH

| 配置项 | 说明 |
|--------|------|
| `freeswitch.esl_host` | ESL 连接地址 |
| `freeswitch.esl_port` | ESL 端口（默认 8021） |
| `freeswitch.audio_ws_addr` | Worker 的 WebSocket 监听地址 |
| `freeswitch.audio_ws_host` | FreeSWITCH 连接 Worker WS 的地址 |
| `freeswitch.sip_domain` | FreeSWITCH 的 SIP 域名（`local_ip_v4`） |

### 通话保护

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `call_protection.max_duration_sec` | 最大通话时长 | 120 |
| `call_protection.max_silence_sec` | 最大静默时间 | 15 |
| `call_protection.max_turns` | 最大对话轮次 | 10 |
| `call_protection.first_silence_timeout_sec` | 首次静默超时 | 6 |

### AMD（答录机检测）

| 配置项 | 说明 |
|--------|------|
| `amd.enabled` | 是否启用（SIP 测试建议关闭） |
| `amd.energy_threshold_dbfs` | 能量阈值（VAD 退回方案时使用） |

## 调试

### 查看 Worker 日志

Worker 输出 JSON 格式日志到 stdout。筛选关键信息：

```bash
# 只看 INFO 及以上级别
./worker -c deploy/local/clarion-local.toml 2>&1 | grep -E '"level":"(INFO|WARN|ERROR)"'

# 跟踪特定通话
./worker -c deploy/local/clarion-local.toml 2>&1 | grep 'call-123'
```

日志级别在配置文件 `[server] log_level` 中设置，支持 `debug`、`info`、`warn`、`error`。

### 查看 FreeSWITCH 日志

```bash
# 进入 FreeSWITCH CLI
podman exec -it local_fs_1 fs_cli

# 常用命令
show registrations           # 查看 SIP 注册
show channels                # 查看活跃通话
sofia status profile internal  # SIP 配置状态
sofia loglevel all 7         # 开启详细 SIP 日志
```

### 手动发起 FreeSWITCH 呼叫（绕过 Worker）

```bash
podman exec local_fs_1 fs_cli -x \
  "originate user/1000@10.128.0.10 &playback(/usr/local/freeswitch/sounds/en/us/callie/ivr/ivr-welcome_to_freeswitch.wav)"
```

### 常见问题

**USER_NOT_REGISTERED**
- Linphone 注册过期或 NAT 端口变化
- 解决：重新打开 Linphone，确认注册成功后再发起呼叫
- 验证：`podman exec local_fs_1 fs_cli -x "show registrations"` 应显示 1 total

**听不到声音**
- 检查 `EXT_IP` 是否设为公网 IP
- 检查 FreeSWITCH 音频 fork 是否启动（日志中搜 `audio fork 已启动`）
- 检查 TTS 临时文件是否生成：`ls /tmp/clarion-audio/`

**TTS 合成慢（5-10秒）**
- DashScope API 的正常延迟，尤其长句子
- 可在 `[llm]` 中调低 `max_tokens` 让 LLM 生成更短的回复

**Barge-in 误触发**
- 当前使用 WebRTC VAD 检测人声，`VeryAggressive` 模式
- 如仍有问题，可在代码中将 `VADVeryAggressive` 改为更低级别
- 能量阈值 `amd.energy_threshold_dbfs` 仅在 VAD 不可用时生效

## 重启流程

```bash
# 1. 重启基础服务
cd deploy/local
podman-compose down && EXT_IP=<公网IP> podman-compose up -d

# 2. 重新编译并启动 Worker
cd /path/to/clarion
go build -o worker ./cmd/worker/
export $(cat .env | xargs)
./worker -c deploy/local/clarion-local.toml

# 3. 快速重启 Worker（不重启容器）
kill $(pgrep -f './worker') 2>/dev/null
export $(cat .env | xargs) && ./worker -c deploy/local/clarion-local.toml &
```

## Redis 队列操作

```bash
# 推送呼叫任务
podman exec local_redis_1 redis-cli LPUSH clarion:task_queue \
  '{"call_id":1,"contact_id":1,"task_id":1,"phone":"1000","gateway":"local","caller_id":"8888","template_id":1}'

# 查看队列长度
podman exec local_redis_1 redis-cli LLEN clarion:task_queue

# 查看完成事件流
podman exec local_redis_1 redis-cli XRANGE clarion:call_completed - +

# 清空队列
podman exec local_redis_1 redis-cli DEL clarion:task_queue
```
