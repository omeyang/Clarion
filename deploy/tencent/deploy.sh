#!/bin/bash
# Clarion 腾讯云部署脚本
#
# 用法：
#   ./deploy.sh          # 首次部署或更新
#   ./deploy.sh restart   # 重启服务

set -euo pipefail

REMOTE="root@82.156.218.133"
REMOTE_DIR="/opt/clarion"
PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEPLOY_DIR="$PROJECT_ROOT/deploy/tencent"

echo "=== 编译 Go 二进制文件 ==="
cd "$PROJECT_ROOT"
go build -ldflags="-s -w" -o bin/clarion ./cmd/clarion
go build -ldflags="-s -w" -o bin/clarion-worker ./cmd/worker
go build -ldflags="-s -w" -o bin/clarion-postprocessor ./cmd/postprocessor

echo "=== 同步文件到远程服务器 ==="
ssh "$REMOTE" "mkdir -p $REMOTE_DIR/{bin,lib,migrations,freeswitch}"

# 二进制文件
rsync -avz --progress bin/clarion bin/clarion-worker bin/clarion-postprocessor "$REMOTE:$REMOTE_DIR/bin/"

# 配置文件
rsync -avz "$DEPLOY_DIR/docker-compose.yml" "$REMOTE:$REMOTE_DIR/"
rsync -avz "$DEPLOY_DIR/clarion.toml" "$REMOTE:$REMOTE_DIR/"

# 数据库迁移（源目录为 schema/）
rsync -avz "$PROJECT_ROOT/schema/" "$REMOTE:$REMOTE_DIR/migrations/"

# FreeSWITCH 配置
rsync -avz "$PROJECT_ROOT/deploy/local/freeswitch/conf/" "$REMOTE:$REMOTE_DIR/freeswitch/conf/"

# sherpa-onnx 共享库（worker CGO 依赖）
SHERPA_LIB="$(go env GOPATH)/pkg/mod/github.com/k2-fsa/sherpa-onnx-go-linux@v1.12.29/lib/x86_64-unknown-linux-gnu"
if [ -d "$SHERPA_LIB" ]; then
    rsync -avz "$SHERPA_LIB/" "$REMOTE:$REMOTE_DIR/lib/"
fi

# .env 文件（如果本地有的话）
if [ -f "$PROJECT_ROOT/.env" ]; then
    rsync -avz "$PROJECT_ROOT/.env" "$REMOTE:$REMOTE_DIR/.env"
fi

echo "=== 重启服务 ==="
ssh "$REMOTE" "cd $REMOTE_DIR && podman-compose down 2>/dev/null; podman-compose up -d"

echo "=== 检查服务状态 ==="
sleep 3
ssh "$REMOTE" "cd $REMOTE_DIR && podman-compose ps"

echo "=== 部署完成 ==="
echo "API Server: http://82.156.218.133:8000"
