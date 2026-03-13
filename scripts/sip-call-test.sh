#!/usr/bin/env bash
#
# SIP 软电话测试脚本
#
# 用法：
#   ./scripts/sip-call-test.sh [用户号码]
#
# 示例：
#   ./scripts/sip-call-test.sh 1000    # 呼叫注册用户 1000（Linphone）
#
# 前置条件：
#   1. EXT_IP=34.172.12.126 podman-compose up -d (deploy/local/)
#   2. Call Worker 正在运行
#   3. SIP 软电话已注册到 FreeSWITCH

set -euo pipefail

USER="${1:-1000}"
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
QUEUE_KEY="${QUEUE_KEY:-clarion:task_queue}"
EVENT_STREAM="${EVENT_STREAM:-clarion:call_completed}"

# 获取 FreeSWITCH 内部 IP（用作 SIP domain）
FS_DOMAIN="${FS_DOMAIN:-}"
if [ -z "${FS_DOMAIN}" ]; then
    # 从 FreeSWITCH 容器获取 IP
    FS_DOMAIN=$(podman exec deploy_local_fs_1 hostname -i 2>/dev/null || \
                podman exec local-fs-1 hostname -i 2>/dev/null || \
                podman exec local_fs_1 hostname -i 2>/dev/null || \
                echo "")
    if [ -z "${FS_DOMAIN}" ]; then
        echo "警告：无法自动获取 FreeSWITCH 容器 IP"
        echo "请手动设置：FS_DOMAIN=<容器IP> $0 ${USER}"
        echo ""
        echo "获取方法：podman exec <fs容器名> hostname -i"
        exit 1
    fi
fi

CALL_ID=$(date +%s)
TASK_JSON=$(cat <<EOF
{
  "call_id": ${CALL_ID},
  "contact_id": 1,
  "task_id": 1,
  "phone": "${USER}",
  "gateway": "local",
  "caller_id": "${FS_DOMAIN}",
  "template_id": 1
}
EOF
)

echo "=== Clarion SIP 软电话测试 ==="
echo ""
echo "目标用户: ${USER}"
echo "FreeSWITCH Domain: ${FS_DOMAIN}"
echo ""
echo "测试任务："
echo "${TASK_JSON}" | python3 -m json.tool 2>/dev/null || echo "${TASK_JSON}"
echo ""

# 检查 Redis
if ! redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" ping > /dev/null 2>&1; then
    echo "错误：无法连接 Redis (${REDIS_HOST}:${REDIS_PORT})"
    exit 1
fi

# 检查 FreeSWITCH ESL
if ! nc -z 127.0.0.1 8021 2>/dev/null; then
    echo "错误：FreeSWITCH ESL 端口 8021 未响应"
    exit 1
fi

# 检查用户注册状态
echo "检查用户 ${USER} 注册状态..."
REG_STATUS=$(redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" ping > /dev/null 2>&1 && \
    echo "Redis OK" || echo "Redis FAIL")
echo "  ${REG_STATUS}"
echo ""

echo "推送呼叫任务..."
redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" LPUSH "${QUEUE_KEY}" "${TASK_JSON}" > /dev/null

echo "任务已推送 (call_id: ${CALL_ID})"
echo ""
echo ">>> 请在 Linphone 上接听来电！ <<<"
echo ""
echo "等待呼叫结果..."

# 监听完成事件
TIMEOUT=120
START=$(date +%s)

while true; do
    ELAPSED=$(( $(date +%s) - START ))
    if [ "${ELAPSED}" -ge "${TIMEOUT}" ]; then
        echo ""
        echo "超时：${TIMEOUT} 秒内未收到结果"
        exit 1
    fi

    RESULT=$(redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
        XREAD COUNT 1 BLOCK 2000 STREAMS "${EVENT_STREAM}" '$' 2>/dev/null || true)

    if [ -n "${RESULT}" ]; then
        echo ""
        echo "=== 呼叫结果 ==="
        echo "${RESULT}"
        echo ""
        echo "SIP 测试完成！"
        break
    fi

    printf "."
done
