# Clarion 腾讯云部署指南

## 服务器信息

| 项目 | 值 |
|------|------|
| 公网 IP | 82.156.218.133 |
| 内网 IP | 10.2.24.10 |
| OS | Rocky Linux 9.6 |
| 配置 | 4 核 / 3.6G 内存 / 40G 磁盘 |
| 容器 | Podman 5.6.0 + podman-compose 1.5.0 |

## 架构概览

```
iPhone Linphone (SIP 软电话)
    │  SIP/RTP (UDP, 公网 82.156.218.133:5060)
    ▼
FreeSWITCH (容器, host 网络模式)
    │  WebSocket (音频帧)          │  ESL (TCP:8021, 控制命令)
    ▼                              ▼
Call Worker (容器, host 网络模式)
    │           │           │
    ▼           ▼           ▼
  ASR(通义)   LLM(DeepSeek)  TTS(DashScope)
    │
    ▼
API Server (容器, host 网络模式, :8000)
    │
    ▼
PostgreSQL + Redis (容器, 仅本机访问)
```

## 目录结构

```
/opt/clarion/                  # 远程部署目录
├── bin/
│   ├── clarion                # API Server 二进制
│   ├── clarion-worker         # Call Worker 二进制
│   └── clarion-postprocessor  # Post-Processing Worker 二进制
├── docker-compose.yml         # Podman Compose 编排
├── clarion.toml               # 服务配置
├── .env                       # API Key（不提交 git）
├── migrations/                # 数据库迁移脚本
└── freeswitch/conf/           # FreeSWITCH 配置
```

## 前置条件

### 开发机（编译用）

- Go 1.25+、GCC（WebRTC VAD 需要 CGO）
- 与远程服务器同架构（均为 Linux amd64，直接编译即可）
- SSH 免密登录已配置：`ssh-copy-id root@82.156.218.133`

### 腾讯云服务器

- Podman + podman-compose（已预装）
- 安全组/防火墙放通以下端口：

| 端口 | 协议 | 用途 |
|------|------|------|
| 5060 | UDP | SIP 信令 |
| 16384-32768 | UDP | RTP 音频流 |
| 8000 | TCP | API Server（可选，按需开放） |

## 快速部署

### 一键部署

```bash
cd deploy/tencent
./deploy.sh
```

脚本自动完成：编译 → rsync 同步 → 重启服务。

### 手动部署（逐步）

#### 1. 编译二进制

```bash
# 在项目根目录（开发机与服务器同架构，直接编译）
go build -ldflags="-s -w" -o bin/clarion ./cmd/clarion
go build -ldflags="-s -w" -o bin/clarion-worker ./cmd/worker
go build -ldflags="-s -w" -o bin/clarion-postprocessor ./cmd/postprocessor
```

#### 2. 同步到远程

```bash
REMOTE="root@82.156.218.133"

ssh $REMOTE "mkdir -p /opt/clarion/{bin,migrations,freeswitch}"

# 二进制
rsync -avz bin/clarion bin/clarion-worker bin/clarion-postprocessor $REMOTE:/opt/clarion/bin/

# 配置
rsync -avz deploy/tencent/docker-compose.yml deploy/tencent/clarion.toml $REMOTE:/opt/clarion/
rsync -avz .env $REMOTE:/opt/clarion/.env
rsync -avz migrations/ $REMOTE:/opt/clarion/migrations/
rsync -avz deploy/local/freeswitch/conf/ $REMOTE:/opt/clarion/freeswitch/conf/
```

#### 3. 启动服务

```bash
ssh $REMOTE "cd /opt/clarion && podman-compose up -d"
```

#### 4. 验证

```bash
# 服务状态
ssh $REMOTE "cd /opt/clarion && podman-compose ps"

# API 健康检查
ssh $REMOTE "curl -s http://127.0.0.1:8000/healthz"
# 预期：{"status":"ok"}

# FreeSWITCH SIP 注册
ssh $REMOTE "podman exec clarion_fs_1 fs_cli -x 'sofia status profile internal reg'"
```

## 配置 .env

项目根目录的 `.env` 文件包含 AI 服务 API Key，部署时自动同步到远程：

```bash
CLARION_LLM_API_KEY=sk-your-deepseek-key
CLARION_ASR_API_KEY=sk-your-dashscope-key
CLARION_TTS_API_KEY=sk-your-dashscope-key
```

> **注意**：Worker 容器使用 `debian:bookworm-slim`，不含 CA 证书。
> docker-compose.yml 已将宿主机证书挂载进容器：
> `/etc/pki/tls/certs/ca-bundle.crt → /etc/ssl/certs/ca-certificates.crt`

## Linphone 配置（iPhone）

| 配置项 | 值 |
|--------|------|
| 用户名 | `1000`（可选 1001-1003） |
| 密码 | `1234` |
| 域名 | `82.156.218.133` |
| 传输 | UDP |

配置后在 FreeSWITCH 确认注册：

```bash
ssh root@82.156.218.133 \
  "podman exec clarion_fs_1 fs_cli -x 'sofia status profile internal reg'"
```

> **重要**：每次 FreeSWITCH 重启后，Linphone 需要重新注册（断开再连接）。

## 发起测试呼叫

### 1. 创建测试模板（首次）

```bash
ssh root@82.156.218.133 'curl -s -X POST http://127.0.0.1:8000/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '"'"'{
    "name": "测试模板",
    "domain": "test",
    "opening_script": "你好，我是 Clarion 智能语音助手。",
    "state_machine_config": {},
    "extraction_schema": {},
    "grading_rules": {},
    "prompt_templates": {"system": "你是一个友善的AI语音助手，请用简短的中文回答用户的问题。"},
    "notification_config": {},
    "call_protection_config": {},
    "precompiled_audios": {}
  }'"'"''
```

### 2. 推送呼叫任务

```bash
ssh root@82.156.218.133 'podman exec clarion_redis_1 redis-cli LPUSH clarion:task_queue \
  '"'"'{"call_id":1,"contact_id":1,"task_id":1,"phone":"1000","gateway":"local","caller_id":"8888","template_id":1}'"'"''
```

Linphone 应收到来电，接听后可与 AI 对话。

### 3. 查看通话日志

```bash
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose logs --tail 50 worker"
```

## 日常运维

### 更新代码并重新部署

```bash
cd deploy/tencent && ./deploy.sh
```

### 只重启服务（不重新编译）

```bash
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose down && podman-compose up -d"
```

### 只重启 Worker

```bash
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose restart worker"
```

### 查看日志

```bash
# 所有服务
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose logs -f"

# 单个服务
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose logs -f worker"
ssh root@82.156.218.133 "cd /opt/clarion && podman-compose logs -f clarion"
```

### Redis 队列操作

```bash
# 查看队列长度
ssh root@82.156.218.133 "podman exec clarion_redis_1 redis-cli LLEN clarion:task_queue"

# 查看完成事件流
ssh root@82.156.218.133 "podman exec clarion_redis_1 redis-cli XRANGE clarion:call_completed - +"

# 清空队列
ssh root@82.156.218.133 "podman exec clarion_redis_1 redis-cli DEL clarion:task_queue"
```

### FreeSWITCH 调试

```bash
# 查看 SIP 注册
ssh root@82.156.218.133 "podman exec clarion_fs_1 fs_cli -x 'show registrations'"

# 查看活跃通话
ssh root@82.156.218.133 "podman exec clarion_fs_1 fs_cli -x 'show channels'"

# 挂断所有通话
ssh root@82.156.218.133 "podman exec clarion_fs_1 fs_cli -x 'hupall'"

# 手动呼叫（绕过 Worker，测试 SIP 连通性）
ssh root@82.156.218.133 "podman exec clarion_fs_1 fs_cli -x \
  'originate user/1000@10.2.24.10 &playback(/usr/local/freeswitch/sounds/en/us/callie/ivr/ivr-welcome_to_freeswitch.wav)'"
```

## 常见问题

### 容器内 TLS 报错 `x509: certificate signed by unknown authority`

Worker/Clarion 容器（debian:bookworm-slim）缺少 CA 证书，导致无法连接阿里云/DeepSeek API。

**解决**：docker-compose.yml 已挂载宿主机证书（Rocky Linux 路径）：
```yaml
- /etc/pki/tls/certs/ca-bundle.crt:/etc/ssl/certs/ca-certificates.crt:ro
```

如果宿主机 OS 不同，调整源路径：
- Debian/Ubuntu: `/etc/ssl/certs/ca-certificates.crt`
- CentOS/Rocky: `/etc/pki/tls/certs/ca-bundle.crt`
- Alpine: `/etc/ssl/certs/ca-certificates.crt`

### Worker 启动后立即退出（ESL connection refused）

FreeSWITCH 启动慢（约 3-5 秒），Worker 连接 ESL 8021 失败后退出。
`restart: unless-stopped` 会自动重启，通常 2-3 次后成功连接。

### Linphone 收不到来电（USER_NOT_REGISTERED）

- Linphone 注册过期或 FreeSWITCH 重启后失效
- 解决：在 Linphone 中断开再重新连接
- 验证：`show registrations` 应显示 `Total items returned: 1`

### 呼叫秒回 USER_BUSY

上一个通话通道未释放。先挂断所有通道再重试：
```bash
ssh root@82.156.218.133 "podman exec clarion_fs_1 fs_cli -x 'hupall'"
```

### 接通后听不到声音

排查顺序：
1. 检查 Worker 日志中 TTS/ASR 是否有 `ERROR`（通常是 TLS 证书问题）
2. 检查 `audio fork 已启动` 日志是否出现
3. 检查 `/tmp/clarion-audio/` 是否有 TTS 生成的 WAV 文件
4. 确认腾讯云安全组放通了 UDP 16384-32768（RTP 端口）

### API Server 返回 404

确认 docker-compose.yml 中 clarion 的 command 包含 `serve` 子命令：
```yaml
command: ["clarion", "-c", "/etc/clarion/clarion.toml", "serve"]
```
