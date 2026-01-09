#!/bin/bash
#
# 打包生产部署包
#
# 使用方法:
#   ./scripts/package-deploy.sh
#   ./scripts/package-deploy.sh --version v1.0.0 --registry harbor.io/mxsec
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 默认值
VERSION="${VERSION:-v1.0.0}"
REGISTRY="${REGISTRY:-}"
OUTPUT_DIR="${PROJECT_ROOT}/dist/deploy"

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            VERSION="$2"
            shift 2
            ;;
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        *)
            echo "未知参数: $1"
            exit 1
            ;;
    esac
done

PACKAGE_NAME="mxsec-platform-${VERSION}"
PACKAGE_DIR="${OUTPUT_DIR}/${PACKAGE_NAME}"

echo "========================================"
echo "打包生产部署包"
echo "版本: $VERSION"
echo "输出: ${OUTPUT_DIR}/${PACKAGE_NAME}.tar.gz"
echo "========================================"

# 清理并创建目录
rm -rf "$PACKAGE_DIR"
mkdir -p "$PACKAGE_DIR"/{config,certs,certs/ssl}

# 复制文件
cp "$PROJECT_ROOT/deploy/production/deploy.sh" "$PACKAGE_DIR/"
cp "$PROJECT_ROOT/deploy/production/init.sql" "$PACKAGE_DIR/"
cp "$PROJECT_ROOT/deploy/production/README.md" "$PACKAGE_DIR/"
cp "$PROJECT_ROOT/deploy/production/config/"* "$PACKAGE_DIR/config/"

# 生成 docker-compose.yml（使用预构建镜像）
if [ -n "$REGISTRY" ]; then
    IMAGE_PREFIX="${REGISTRY}/"
else
    IMAGE_PREFIX=""
fi

cat > "$PACKAGE_DIR/docker-compose.yml" << EOF
version: '3.8'

services:
  mysql:
    image: mysql:8.0
    container_name: mxsec-mysql
    restart: always
    environment:
      MYSQL_ROOT_PASSWORD: \${MYSQL_ROOT_PASSWORD}
      MYSQL_DATABASE: \${MYSQL_DATABASE:-mxsec}
      MYSQL_USER: \${MYSQL_USER:-mxsec_user}
      MYSQL_PASSWORD: \${MYSQL_PASSWORD}
      TZ: \${TZ:-Asia/Shanghai}
    volumes:
      - \${DATA_DIR}/mysql:/var/lib/mysql
      - ./init.sql:/docker-entrypoint-initdb.d/init.sql:ro
    command: >
      --character-set-server=utf8mb4
      --collation-server=utf8mb4_unicode_ci
      --max_connections=500
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-u", "root", "-p\${MYSQL_ROOT_PASSWORD}"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    networks:
      - mxsec-net
    deploy:
      resources:
        limits:
          memory: 4G

  agentcenter:
    image: ${IMAGE_PREFIX}mxsec-agentcenter:\${VERSION:-${VERSION}}
    container_name: mxsec-agentcenter
    restart: always
    depends_on:
      mysql:
        condition: service_healthy
    ports:
      - "\${GRPC_PORT:-6751}:6751"
    volumes:
      - ./config/server.yaml:/etc/mxsec-platform/server.yaml:ro
      - ./certs:/etc/mxsec-platform/certs:ro
      - \${DATA_DIR}/logs/agentcenter:/var/log/mxsec-platform
      - \${DATA_DIR}/plugins:/opt/mxsec-platform/plugins
    environment:
      TZ: \${TZ:-Asia/Shanghai}
    healthcheck:
      test: ["CMD-SHELL", "nc -z localhost 6751 || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 60s
    networks:
      - mxsec-net
    deploy:
      resources:
        limits:
          memory: 4G

  manager:
    image: ${IMAGE_PREFIX}mxsec-manager:\${VERSION:-${VERSION}}
    container_name: mxsec-manager
    restart: always
    depends_on:
      mysql:
        condition: service_healthy
      agentcenter:
        condition: service_healthy
    expose:
      - "8080"
    volumes:
      - ./config/server.yaml:/etc/mxsec-platform/server.yaml:ro
      - \${DATA_DIR}/logs/manager:/var/log/mxsec-platform
      - \${DATA_DIR}/plugins:/opt/mxsec-platform/plugins:ro
    environment:
      TZ: \${TZ:-Asia/Shanghai}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 60s
    networks:
      - mxsec-net
    deploy:
      resources:
        limits:
          memory: 4G

  ui:
    image: ${IMAGE_PREFIX}mxsec-ui:\${VERSION:-${VERSION}}
    container_name: mxsec-ui
    restart: always
    depends_on:
      manager:
        condition: service_healthy
    ports:
      - "\${HTTP_PORT:-80}:80"
      - "\${HTTPS_PORT:-443}:443"
    volumes:
      - ./config/nginx.conf:/etc/nginx/conf.d/default.conf:ro
      - ./certs/ssl:/etc/nginx/ssl:ro
      - \${DATA_DIR}/logs/nginx:/var/log/nginx
    environment:
      TZ: \${TZ:-Asia/Shanghai}
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - mxsec-net

networks:
  mxsec-net:
    driver: bridge
EOF

# 更新 deploy.sh（移除构建步骤）
cat > "$PACKAGE_DIR/deploy.sh" << 'DEPLOY_SCRIPT'
#!/bin/bash
#
# Matrix Cloud Security Platform - 生产环境部署
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${BLUE}[STEP]${NC} $1"; }

check_docker() {
    if ! command -v docker &> /dev/null; then
        log_error "Docker 未安装"
        exit 1
    fi
    if ! docker info &> /dev/null; then
        log_error "Docker 服务未运行"
        exit 1
    fi
    log_info "Docker: $(docker version --format '{{.Server.Version}}')"
}

check_compose() {
    if docker compose version &> /dev/null; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose &> /dev/null; then
        COMPOSE_CMD="docker-compose"
    else
        log_error "Docker Compose 未安装"
        exit 1
    fi
    log_info "Compose: $($COMPOSE_CMD version --short 2>/dev/null || echo 'ok')"
}

dc() {
    cd "$SCRIPT_DIR"
    $COMPOSE_CMD --env-file "$ENV_FILE" "$@"
}

init_env() {
    if [ -f "$ENV_FILE" ]; then
        log_info "配置已存在: $ENV_FILE"
        read -p "重新配置? (y/N): " confirm
        [[ ! "$confirm" =~ ^[Yy]$ ]] && return
    fi

    log_step "配置环境..."

    read -sp "MySQL Root 密码 (回车自动生成): " MYSQL_ROOT_PASSWORD
    echo
    [ -z "$MYSQL_ROOT_PASSWORD" ] && MYSQL_ROOT_PASSWORD=$(openssl rand -hex 16)

    read -sp "MySQL 应用密码 (回车自动生成): " MYSQL_PASSWORD
    echo
    [ -z "$MYSQL_PASSWORD" ] && MYSQL_PASSWORD=$(openssl rand -hex 16)

    read -p "数据目录 [/data/mxsec]: " DATA_DIR
    DATA_DIR="${DATA_DIR:-/data/mxsec}"

    DEFAULT_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")
    read -p "服务器 IP [$DEFAULT_IP]: " SERVER_IP
    SERVER_IP="${SERVER_IP:-$DEFAULT_IP}"

    read -p "gRPC 端口 [6751]: " GRPC_PORT
    GRPC_PORT="${GRPC_PORT:-6751}"

    read -p "HTTP 端口 [80]: " HTTP_PORT
    HTTP_PORT="${HTTP_PORT:-80}"

    cat > "$ENV_FILE" << EOF
MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD
MYSQL_PASSWORD=$MYSQL_PASSWORD
MYSQL_DATABASE=mxsec
MYSQL_USER=mxsec_user
DATA_DIR=$DATA_DIR
SERVER_IP=$SERVER_IP
GRPC_PORT=$GRPC_PORT
HTTP_PORT=$HTTP_PORT
HTTPS_PORT=443
TZ=Asia/Shanghai
EOF
    chmod 600 "$ENV_FILE"
    log_info "配置已保存"
}

init_dirs() {
    source "$ENV_FILE"
    log_step "创建目录..."
    sudo mkdir -p "$DATA_DIR"/{mysql,logs/{agentcenter,manager,nginx},plugins}
    sudo chown -R $(id -u):$(id -g) "$DATA_DIR" 2>/dev/null || true
}

init_certs() {
    [ -f "$SCRIPT_DIR/certs/ca.crt" ] && { log_info "证书已存在"; return; }

    log_step "生成证书..."
    cd "$SCRIPT_DIR/certs"

    openssl genrsa -out ca.key 4096 2>/dev/null
    openssl req -new -x509 -days 3650 -key ca.key -out ca.crt -subj "/CN=MxSec CA" 2>/dev/null

    openssl genrsa -out server.key 2048 2>/dev/null
    openssl req -new -key server.key -out server.csr -subj "/CN=mxsec-server" 2>/dev/null
    openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt 2>/dev/null
    rm -f server.csr ca.srl

    openssl genrsa -out agent.key 2048 2>/dev/null
    openssl req -new -key agent.key -out agent.csr -subj "/CN=mxsec-agent" 2>/dev/null
    openssl x509 -req -days 365 -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out agent.crt 2>/dev/null
    rm -f agent.csr ca.srl

    log_info "证书生成完成"
}

init_config() {
    source "$ENV_FILE"
    log_step "更新配置..."

    sed -i.bak "s/MYSQL_PASSWORD_PLACEHOLDER/$MYSQL_PASSWORD/g" "$SCRIPT_DIR/config/server.yaml" 2>/dev/null || \
    sed -i '' "s/MYSQL_PASSWORD_PLACEHOLDER/$MYSQL_PASSWORD/g" "$SCRIPT_DIR/config/server.yaml"

    sed -i.bak "s|PLUGINS_BASE_URL_PLACEHOLDER|http://$SERVER_IP:$HTTP_PORT/api/v1/plugins/download|g" "$SCRIPT_DIR/config/server.yaml" 2>/dev/null || \
    sed -i '' "s|PLUGINS_BASE_URL_PLACEHOLDER|http://$SERVER_IP:$HTTP_PORT/api/v1/plugins/download|g" "$SCRIPT_DIR/config/server.yaml"

    rm -f "$SCRIPT_DIR/config/server.yaml.bak"
}

start() {
    [ ! -f "$ENV_FILE" ] && { log_error "请先运行 ./deploy.sh 初始化"; exit 1; }

    log_step "启动服务..."
    dc up -d

    sleep 10
    status

    source "$ENV_FILE"
    echo ""
    log_info "========================================"
    log_info "部署完成!"
    log_info "Web: http://$SERVER_IP:$HTTP_PORT"
    log_info "gRPC: $SERVER_IP:$GRPC_PORT"
    log_info "========================================"
}

stop() {
    log_step "停止服务..."
    dc down
}

restart() {
    dc restart "$@"
}

status() {
    dc ps
}

logs() {
    dc logs -f "$@"
}

backup() {
    source "$ENV_FILE"
    BACKUP="$SCRIPT_DIR/backup_$(date +%Y%m%d_%H%M%S).sql"
    log_step "备份数据库..."
    dc exec -T mysql mysqldump -u root -p"$MYSQL_ROOT_PASSWORD" mxsec > "$BACKUP"
    log_info "备份: $BACKUP"
}

full_deploy() {
    echo ""
    echo "=========================================="
    echo "  Matrix Cloud Security Platform 部署"
    echo "=========================================="
    echo ""

    log_step "[1/5] 检测环境..."
    check_docker
    check_compose

    log_step "[2/5] 配置..."
    init_env

    log_step "[3/5] 初始化目录..."
    init_dirs

    log_step "[4/5] 生成证书..."
    init_certs

    log_step "[5/5] 启动服务..."
    init_config
    start
}

case "${1:-}" in
    "") full_deploy ;;
    start) start ;;
    stop) stop ;;
    restart) shift; restart "$@" ;;
    status) status ;;
    logs) shift; logs "$@" ;;
    backup) backup ;;
    help|--help|-h)
        echo "用法: $0 [命令]"
        echo "命令: start, stop, restart, status, logs, backup"
        ;;
    *) log_error "未知: $1"; exit 1 ;;
esac
DEPLOY_SCRIPT

chmod +x "$PACKAGE_DIR/deploy.sh"

# 打包
cd "$OUTPUT_DIR"
tar -czf "${PACKAGE_NAME}.tar.gz" "$PACKAGE_NAME"

echo ""
echo "========================================"
echo "打包完成!"
echo ""
echo "部署包: ${OUTPUT_DIR}/${PACKAGE_NAME}.tar.gz"
echo "大小: $(du -h "${PACKAGE_NAME}.tar.gz" | cut -f1)"
echo ""
echo "部署步骤:"
echo "  1. 上传到服务器"
echo "  2. tar -xzf ${PACKAGE_NAME}.tar.gz"
echo "  3. cd ${PACKAGE_NAME}"
echo "  4. ./deploy.sh"
echo "========================================"
