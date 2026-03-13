#!/usr/bin/env bash
#
# 回环测试脚本
#
# 用法：
#   ./scripts/loopback-test.sh
#
# 前置条件：
#   1. docker compose up -d (deploy/local/)
#   2. Call Worker 正在运行 (go run ./cmd/worker -c deploy/local/clarion-local.toml)
#   3. 测试音频已就位 (deploy/local/test-audio/user-speech.wav)
#
# 此脚本通过 Redis 推送一个测试任务到任务队列，
# Call Worker 会从队列取出任务并发起回环呼叫。

set -euo pipefail

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
QUEUE_KEY="${QUEUE_KEY:-clarion:task_queue}"
EVENT_STREAM="${EVENT_STREAM:-clarion:call_completed}"

# 生成测试任务 JSON
CALL_ID=$(date +%s)
TASK_JSON=$(cat <<EOF
{
  "call_id": ${CALL_ID},
  "contact_id": 1,
  "task_id": 1,
  "phone": "loopback_test",
  "gateway": "",
  "caller_id": "10000",
  "template_id": 1
}
EOF
)

echo "=== Clarion 回环测试 ==="
echo ""
echo "测试任务："
echo "${TASK_JSON}" | python3 -m json.tool 2>/dev/null || echo "${TASK_JSON}"
echo ""

# 检查 Redis 连接
if ! redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" ping > /dev/null 2>&1; then
    echo "错误：无法连接 Redis (${REDIS_HOST}:${REDIS_PORT})"
    echo "请确认 docker compose up -d 已执行"
    exit 1
fi

# 检查 FreeSWITCH ESL 连接
if ! nc -z 127.0.0.1 8021 2>/dev/null; then
    echo "警告：FreeSWITCH ESL 端口 8021 未响应"
    echo "请确认 FreeSWITCH 容器已启动"
fi

echo "推送测试任务到 Redis 队列..."
redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" LPUSH "${QUEUE_KEY}" "${TASK_JSON}" > /dev/null

echo "任务已推送 (call_id: ${CALL_ID})"
echo ""
echo "等待呼叫结果..."
echo "(Call Worker 日志: go run ./cmd/worker -c deploy/local/clarion-local.toml)"
echo ""

# 监听完成事件（最多等待 60 秒）
TIMEOUT=60
START=$(date +%s)

while true; do
    ELAPSED=$(( $(date +%s) - START ))
    if [ "${ELAPSED}" -ge "${TIMEOUT}" ]; then
        echo "超时：${TIMEOUT} 秒内未收到呼叫结果"
        exit 1
    fi

    # 尝试从 Redis Stream 读取结果
    RESULT=$(redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
        XREAD COUNT 1 BLOCK 2000 STREAMS "${EVENT_STREAM}" '$' 2>/dev/null || true)

    if [ -n "${RESULT}" ]; then
        echo "=== 呼叫结果 ==="
        echo "${RESULT}"
        echo ""
        echo "回环测试完成！"
        break
    fi

    printf "."
done
