#!/bin/bash

# Docker 开发环境启动脚本
# 用于在 Docker 中运行后端和前端

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 项目根目录
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Matrix Cloud Security Platform${NC}"
echo -e "${GREEN}  Docker 开发环境启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查Docker
echo -e "${YELLOW}[1/4] 检查Docker...${NC}"
if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: 未找到 Docker，请先安装 Docker${NC}"
    exit 1
fi
echo "  ✓ Docker: $(docker --version)"

if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo -e "${RED}错误: 未找到 docker-compose${NC}"
    exit 1
fi
echo "  ✓ docker-compose: 已安装"

# 检查Node.js（用于构建Docker镜像）
echo ""
echo -e "${YELLOW}[2/4] 检查Node.js（用于构建Docker镜像）...${NC}"
if ! command -v node &> /dev/null; then
    echo -e "${YELLOW}  警告: 未找到 Node.js，Docker 构建时可能需要更长时间${NC}"
else
    echo "  ✓ Node.js: $(node --version)"
fi

# 检查证书
echo ""
echo -e "${YELLOW}[3/4] 检查mTLS证书...${NC}"
if [ ! -f "deploy/dev/certs/ca.crt" ]; then
    echo -e "${YELLOW}  证书文件不存在，正在生成...${NC}"
    make certs || {
        echo -e "${RED}  错误: 证书生成失败${NC}"
        exit 1
    }
    echo "  ✓ 证书已生成"
else
    echo "  ✓ 证书文件存在"
fi

# UI依赖会在Docker构建时安装，这里不需要检查
echo ""
echo -e "${YELLOW}[4/4] 准备Docker环境...${NC}"
echo "  ✓ UI依赖将在Docker构建时安装"

# 检查宿主机MySQL连接
echo ""
echo -e "${YELLOW}[额外检查] 检查宿主机MySQL连接...${NC}"
if command -v mysql &> /dev/null; then
    if mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e "SELECT 1;" 2>/dev/null; then
        echo -e "${GREEN}  ✓ 宿主机MySQL连接成功${NC}"
        # 检查数据库是否存在
        if ! mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e "USE mxsec;" 2>/dev/null; then
            echo "创建数据库..."
            mysql -h 127.0.0.1 -P 3306 -u root -p123456 -e "CREATE DATABASE IF NOT EXISTS mxsec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;" 2>/dev/null
            echo -e "${GREEN}  ✓ 数据库已创建${NC}"
        else
            echo -e "${GREEN}  ✓ 数据库已存在${NC}"
        fi
    else
        echo -e "${RED}  ✗ 无法连接到宿主机MySQL (127.0.0.1:3306, root/123456)${NC}"
        echo -e "${RED}  请确保MySQL已启动，并且root密码为123456${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}  警告: 未找到mysql客户端，跳过MySQL检查${NC}"
    echo -e "${YELLOW}  请确保MySQL已启动 (127.0.0.1:3306, root/123456)${NC}"
fi

# 启动Docker服务
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  启动Docker服务...${NC}"
echo -e "${GREEN}========================================${NC}"
cd deploy/dev

# 启动Manager（前台运行，可以看到日志）
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  启动Manager服务（Docker）...${NC}"
echo -e "${GREEN}========================================${NC}"

# 清理函数
cleanup() {
    echo ""
    echo -e "${YELLOW}正在停止服务...${NC}"
    cd "$PROJECT_ROOT/deploy/dev"
    docker-compose -f docker-compose.dev.yml stop agentcenter manager ui agent
    echo -e "${GREEN}服务已停止${NC}"
    exit 0
}

# 注册清理函数
trap cleanup SIGINT SIGTERM

# 启动AgentCenter、Manager、UI和Agent服务（前台运行，可以看到日志）
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  启动AgentCenter、Manager、UI和Agent服务（Docker）...${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "  服务将在前台运行，日志会实时显示"
echo -e "  按 ${YELLOW}Ctrl+C${NC} 停止服务"
echo ""

# 先启动 AgentCenter（后台运行）
echo -e "${YELLOW}先启动 AgentCenter 服务（后台）...${NC}"
docker-compose -f docker-compose.dev.yml up -d --build agentcenter

# 等待 AgentCenter 启动
echo -e "${YELLOW}等待 AgentCenter 启动...${NC}"
sleep 5

# 启动 Agent（后台运行，使用 Rocky Linux 9）
echo -e "${YELLOW}启动 Agent 服务（后台，Rocky Linux 9）...${NC}"
docker-compose -f docker-compose.dev.yml up -d --build agent

# 使用docker-compose启动Manager和UI服务（前台运行）
docker-compose -f docker-compose.dev.yml up --build manager ui
