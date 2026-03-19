#!/bin/bash

# Docker 开发环境启动脚本
# 工作目录: deploy/ (自动合并 docker-compose.override.yml 实现热重载)

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$PROJECT_ROOT/deploy"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Matrix Cloud Security Platform${NC}"
echo -e "${GREEN}  Docker 开发环境启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# [1/4] 检查 Docker
echo -e "${YELLOW}[1/4] 检查 Docker...${NC}"
if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: 未找到 Docker，请先安装 Docker${NC}"
    exit 1
fi
echo "  ✓ Docker: $(docker --version)"

if ! docker compose version &> /dev/null && ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}错误: 未找到 docker compose${NC}"
    exit 1
fi
echo "  ✓ docker compose: 已安装"

# [2/4] 准备 .env
echo ""
echo -e "${YELLOW}[2/4] 准备环境配置...${NC}"
if [ ! -f "$DEPLOY_DIR/.env" ]; then
    cp "$DEPLOY_DIR/.env.example" "$DEPLOY_DIR/.env"
    echo "  ✓ 已从 .env.example 创建 .env（可按需修改）"
else
    echo "  ✓ .env 已存在"
fi

# [3/4] 从模板生成 server.yaml
echo ""
echo -e "${YELLOW}[3/4] 生成 server.yaml...${NC}"
source "$DEPLOY_DIR/.env"
cp "$DEPLOY_DIR/config/server.yaml.tpl" "$DEPLOY_DIR/config/server.yaml"

PLUGINS_URL="${PLUGINS_BASE_URL:-}"
sed -i.bak \
    -e "s|__MYSQL_HOST__|${MYSQL_HOST:-mysql}|g" \
    -e "s|__MYSQL_PORT__|${MYSQL_PORT:-3306}|g" \
    -e "s|__MYSQL_USER__|${MYSQL_USER:-mxsec_user}|g" \
    -e "s|__MYSQL_PASSWORD__|${MYSQL_PASSWORD:-123456}|g" \
    -e "s|__MYSQL_DATABASE__|${MYSQL_DATABASE:-mxsec}|g" \
    -e "s|__DB_MAX_IDLE_CONNS__|${DB_MAX_IDLE_CONNS:-20}|g" \
    -e "s|__DB_MAX_OPEN_CONNS__|${DB_MAX_OPEN_CONNS:-200}|g" \
    -e "s|__DB_CONN_MAX_LIFETIME__|${DB_CONN_MAX_LIFETIME:-1h}|g" \
    -e "s|__REDIS_ADDR__|${REDIS_ADDR:-redis:6379}|g" \
    -e "s|__REDIS_PASSWORD__|${REDIS_PASSWORD:-}|g" \
    -e "s|__REDIS_DB__|${REDIS_DB:-0}|g" \
    -e "s|__REDIS_POOL_SIZE__|${REDIS_POOL_SIZE:-100}|g" \
    -e "s|__KAFKA_ENABLED__|${KAFKA_ENABLED:-false}|g" \
    -e "s|__KAFKA_BROKERS__|${KAFKA_BROKERS:-kafka:9092}|g" \
    -e "s|__KAFKA_TOPIC_PREFIX__|${KAFKA_TOPIC_PREFIX:-}|g" \
    -e "s|__CLICKHOUSE_ENABLED__|${CLICKHOUSE_ENABLED:-false}|g" \
    -e "s|__CLICKHOUSE_ADDR__|${CLICKHOUSE_ADDR:-clickhouse:9000}|g" \
    -e "s|__CLICKHOUSE_DATABASE__|${CLICKHOUSE_DATABASE:-mxsec}|g" \
    -e "s|__CLICKHOUSE_USER__|${CLICKHOUSE_USER:-default}|g" \
    -e "s|__CLICKHOUSE_PASSWORD__|${CLICKHOUSE_PASSWORD:-}|g" \
    -e "s|__LOG_LEVEL__|${LOG_LEVEL:-debug}|g" \
    -e "s|__LOG_FORMAT__|${LOG_FORMAT:-console}|g" \
    -e "s|__LOG_MAX_AGE__|${LOG_MAX_AGE:-7}|g" \
    -e "s|__HEARTBEAT_INTERVAL__|${HEARTBEAT_INTERVAL:-60}|g" \
    -e "s|__PLUGINS_DIR__|${PLUGINS_DIR:-/opt/mxsec-platform/plugins}|g" \
    -e "s|__PLUGINS_BASE_URL__|${PLUGINS_URL}|g" \
    -e "s|__JWT_SECRET__|${JWT_SECRET:-dev-secret-change-in-production}|g" \
    "$DEPLOY_DIR/config/server.yaml"
rm -f "$DEPLOY_DIR/config/server.yaml.bak"
echo "  ✓ server.yaml 已生成"

# [4/4] 检查证书
echo ""
echo -e "${YELLOW}[4/4] 检查 mTLS 证书...${NC}"
if [ ! -f "$DEPLOY_DIR/certs/ca.crt" ]; then
    echo -e "${YELLOW}  证书文件不存在，正在生成...${NC}"
    cd "$PROJECT_ROOT" && make certs || {
        echo -e "${RED}  错误: 证书生成失败${NC}"
        exit 1
    }
    echo "  ✓ 证书已生成"
else
    echo "  ✓ 证书文件存在"
fi

# 启动服务
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  启动 Docker 服务 (dev 模式)...${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "  按 ${YELLOW}Ctrl+C${NC} 停止服务"
echo ""

cd "$DEPLOY_DIR"

# 清理函数
cleanup() {
    echo ""
    echo -e "${YELLOW}正在停止服务...${NC}"
    cd "$DEPLOY_DIR"
    docker compose down
    echo -e "${GREEN}服务已停止${NC}"
    exit 0
}
trap cleanup SIGINT SIGTERM

# docker compose up 自动合并 docker-compose.override.yml（dev 热重载层）
docker compose up --build
