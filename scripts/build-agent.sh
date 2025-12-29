#!/bin/bash
# Agent 构建脚本
# 支持本地构建和 Docker 环境构建

set -e

# 获取脚本所在目录，切换到项目根目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 如果在 Docker 中且 /workspace 存在，使用 /workspace
if [ -d "/workspace" ]; then
    cd /workspace
else
    cd "$PROJECT_ROOT"
fi

# 设置默认值
SERVER_HOST=${SERVER_HOST:-localhost:6751}
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# 版本：环境变量 > VERSION 文件 > 默认值
if [ -n "${VERSION:-}" ]; then
    : # 使用环境变量
elif [ -f "VERSION" ]; then
    VERSION=$(cat VERSION | tr -d '[:space:]')
elif [ -f "/workspace/VERSION" ]; then
    VERSION=$(cat /workspace/VERSION | tr -d '[:space:]')
else
    VERSION="dev"
fi
ARCH="${GOARCH:-amd64}"
OS="${GOOS:-linux}"

# 输出目录
DIST_DIR="dist/agent"
mkdir -p "$DIST_DIR"

echo "=== Building Agent ==="
echo "Server: $SERVER_HOST"
echo "Version: $VERSION"
echo "OS/Arch: $OS/$ARCH"
echo ""

# 构建 Agent
CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -ldflags "\
  -X main.serverHost=$SERVER_HOST \
  -X main.buildVersion=$VERSION \
  -X main.buildTime=$BUILD_TIME \
  -s -w" \
  -o "$DIST_DIR/mxsec-agent-$OS-$ARCH" ./cmd/agent

echo ""
echo "=== Build Complete ==="
echo "Agent: $DIST_DIR/mxsec-agent-$OS-$ARCH"
ls -lh "$DIST_DIR/mxsec-agent-$OS-$ARCH"

# 如果在 Docker/Air 环境中，复制到 tmp/agent 供 Air 使用
if [ -d "/workspace" ]; then
    mkdir -p /workspace/tmp
    cp "$DIST_DIR/mxsec-agent-$OS-$ARCH" /workspace/tmp/agent
    echo "Copied to /workspace/tmp/agent for Air hot-reload"
fi
