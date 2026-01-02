#!/bin/bash
#
# Matrix Cloud Security Platform - 生产环境部署脚本
#
# 使用方法:
#   ./deploy.sh              # 交互式部署
#   ./deploy.sh start        # 启动服务
#   ./deploy.sh stop         # 停止服务
#   ./deploy.sh restart      # 重启服务
#   ./deploy.sh status       # 查看状态
#   ./deploy.sh logs         # 查看日志
#   ./deploy.sh backup       # 备份数据
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${BLUE}[STEP]${NC} $1"; }

# ============================================================
# 环境检测
# ============================================================
check_os() {
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        if [ -f /etc/os-release ]; then
            . /etc/os-release
            OS_NAME=$ID
            OS_VERSION=$VERSION_ID
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        OS_NAME="macos"
        OS_VERSION=$(sw_vers -productVersion)
    else
        log_error "不支持的操作系统: $OSTYPE"
        exit 1
    fi
    log_info "操作系统: $OS_NAME $OS_VERSION"
}

check_docker() {
    if ! command -v docker &> /dev/null; then
        log_error "Docker 未安装"
        log_info "安装方法: https://docs.docker.com/engine/install/"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        log_error "Docker 服务未运行或无权限"
        log_info "请运行: sudo systemctl start docker"
        exit 1
    fi

    DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
    log_info "Docker 版本: $DOCKER_VERSION"
}

check_docker_compose() {
    if docker compose version &> /dev/null; then
        COMPOSE_CMD="docker compose"
        COMPOSE_VERSION=$(docker compose version --short 2>/dev/null || echo "unknown")
    elif command -v docker-compose &> /dev/null; then
        COMPOSE_CMD="docker-compose"
        COMPOSE_VERSION=$(docker-compose version --short 2>/dev/null || echo "unknown")
    else
        log_error "Docker Compose 未安装"
        exit 1
    fi
    log_info "Docker Compose 版本: $COMPOSE_VERSION"
}

check_ports() {
    local ports=("${GRPC_PORT:-6751}" "${HTTP_PORT:-80}")
    for port in "${ports[@]}"; do
        if netstat -tuln 2>/dev/null | grep -q ":$port " || ss -tuln 2>/dev/null | grep -q ":$port "; then
            log_warn "端口 $port 已被占用"
        fi
    done
}

# ============================================================
# 初始化配置
# ============================================================
init_env() {
    if [ -f "$ENV_FILE" ]; then
        log_info "配置文件已存在: $ENV_FILE"
        read -p "是否重新配置? (y/N): " confirm
        if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
            return
        fi
    fi

    log_step "配置环境变量..."

    # 数据库密码
    read -sp "MySQL Root 密码: " MYSQL_ROOT_PASSWORD
    echo
    if [ -z "$MYSQL_ROOT_PASSWORD" ]; then
        MYSQL_ROOT_PASSWORD=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9' | head -c 24)
        log_info "已自动生成 Root 密码"
    fi

    read -sp "MySQL 应用密码: " MYSQL_PASSWORD
    echo
    if [ -z "$MYSQL_PASSWORD" ]; then
        MYSQL_PASSWORD=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9' | head -c 24)
        log_info "已自动生成应用密码"
    fi

    # 数据目录
    read -p "数据存储目录 [/data/mxsec]: " DATA_DIR
    DATA_DIR="${DATA_DIR:-/data/mxsec}"

    # 服务器IP
    DEFAULT_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")
    read -p "服务器 IP [$DEFAULT_IP]: " SERVER_IP
    SERVER_IP="${SERVER_IP:-$DEFAULT_IP}"

    # 端口
    read -p "gRPC 端口 [6751]: " GRPC_PORT
    GRPC_PORT="${GRPC_PORT:-6751}"

    read -p "HTTP 端口 [80]: " HTTP_PORT
    HTTP_PORT="${HTTP_PORT:-80}"

    read -p "Manager API 端口 [8080]: " MANAGER_PORT
    MANAGER_PORT="${MANAGER_PORT:-8080}"

    # 版本
    read -p "部署版本 [v1.0.0]: " VERSION
    VERSION="${VERSION:-v1.0.0}"

    # 写入配置
    cat > "$ENV_FILE" << EOF
# Matrix Cloud Security Platform 配置
# 生成时间: $(date '+%Y-%m-%d %H:%M:%S')

# 数据库
MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD
MYSQL_PASSWORD=$MYSQL_PASSWORD
MYSQL_DATABASE=mxsec
MYSQL_USER=mxsec_user

# 数据目录
DATA_DIR=$DATA_DIR

# 网络
SERVER_IP=$SERVER_IP
GRPC_PORT=$GRPC_PORT
HTTP_PORT=$HTTP_PORT
HTTPS_PORT=443
MANAGER_PORT=$MANAGER_PORT

# 版本
VERSION=$VERSION
TZ=Asia/Shanghai
EOF

    chmod 600 "$ENV_FILE"
    log_info "配置已保存: $ENV_FILE"
}

# ============================================================
# 初始化目录和证书
# ============================================================
init_dirs() {
    source "$ENV_FILE"

    log_step "创建数据目录..."
    sudo mkdir -p "$DATA_DIR"/{mysql,logs/{agentcenter,manager,nginx},plugins}
    sudo chown -R $(id -u):$(id -g) "$DATA_DIR" 2>/dev/null || true
    log_info "数据目录: $DATA_DIR"
}

init_certs() {
    if [ -f "$SCRIPT_DIR/certs/ca.crt" ]; then
        log_info "证书已存在"
        return
    fi

    log_step "生成 mTLS 证书..."
    mkdir -p "$SCRIPT_DIR/certs/ssl"

    cd "$PROJECT_ROOT"
    if [ -f "./scripts/generate-certs.sh" ]; then
        ./scripts/generate-certs.sh
        cp -r certs/* "$SCRIPT_DIR/certs/"
    else
        # 手动生成证书
        cd "$SCRIPT_DIR/certs"

        # CA
        openssl genrsa -out ca.key 4096
        openssl req -new -x509 -days 3650 -key ca.key -out ca.crt -subj "/CN=MxSec CA"

        # Server
        openssl genrsa -out server.key 2048
        openssl req -new -key server.key -out server.csr -subj "/CN=mxsec-server"
        openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt
        rm -f server.csr

        # Agent
        openssl genrsa -out agent.key 2048
        openssl req -new -key agent.key -out agent.csr -subj "/CN=mxsec-agent"
        openssl x509 -req -days 365 -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out agent.crt
        rm -f agent.csr
    fi

    log_info "证书生成完成"
}

init_config() {
    source "$ENV_FILE"

    log_step "更新配置文件..."

    # 更新 server.yaml
    sed -i.bak "s/MYSQL_PASSWORD_PLACEHOLDER/$MYSQL_PASSWORD/g" "$SCRIPT_DIR/config/server.yaml"
    sed -i.bak "s|PLUGINS_BASE_URL_PLACEHOLDER|http://$SERVER_IP:$HTTP_PORT/api/v1/plugins/download|g" "$SCRIPT_DIR/config/server.yaml"
    rm -f "$SCRIPT_DIR/config/server.yaml.bak"

    log_info "配置更新完成"
}

# ============================================================
# Docker Compose 操作
# ============================================================
dc() {
    cd "$SCRIPT_DIR"
    $COMPOSE_CMD --env-file "$ENV_FILE" "$@"
}

build() {
    log_step "构建镜像..."
    dc build
}

start() {
    if [ ! -f "$ENV_FILE" ]; then
        log_error "请先运行 ./deploy.sh 初始化环境"
        exit 1
    fi

    log_step "启动服务..."
    dc up -d

    log_info "等待服务就绪..."
    sleep 10

    status

    source "$ENV_FILE"
    echo ""
    log_info "部署完成!"
    log_info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    log_info "Web 控制台: http://$SERVER_IP:$HTTP_PORT"
    log_info "AgentCenter: $SERVER_IP:$GRPC_PORT"
    log_info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

stop() {
    log_step "停止服务..."
    dc down
    log_info "服务已停止"
}

restart() {
    log_step "重启服务..."
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
    BACKUP_FILE="$SCRIPT_DIR/backup_$(date +%Y%m%d_%H%M%S).sql"

    log_step "备份数据库..."
    dc exec -T mysql mysqldump -u root -p"$MYSQL_ROOT_PASSWORD" mxsec > "$BACKUP_FILE"

    log_info "备份完成: $BACKUP_FILE"
}

# ============================================================
# 主流程
# ============================================================
full_deploy() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Matrix Cloud Security Platform 生产环境部署"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    log_step "[1/7] 检测运行环境..."
    check_os
    check_docker
    check_docker_compose

    log_step "[2/7] 检测端口..."
    check_ports

    log_step "[3/7] 配置环境变量..."
    init_env

    log_step "[4/7] 初始化目录..."
    init_dirs

    log_step "[5/7] 生成证书..."
    init_certs

    log_step "[6/7] 更新配置..."
    init_config

    log_step "[7/7] 构建并启动服务..."
    build
    start
}

show_help() {
    echo "Matrix Cloud Security Platform 部署脚本"
    echo ""
    echo "用法: $0 [命令]"
    echo ""
    echo "命令:"
    echo "  (无参数)    交互式部署"
    echo "  start       启动服务"
    echo "  stop        停止服务"
    echo "  restart     重启服务"
    echo "  status      查看状态"
    echo "  logs        查看日志"
    echo "  backup      备份数据"
    echo "  build       构建镜像"
    echo "  help        显示帮助"
}

main() {
    check_docker
    check_docker_compose

    case "${1:-}" in
        "")
            full_deploy
            ;;
        start)
            start
            ;;
        stop)
            stop
            ;;
        restart)
            shift
            restart "$@"
            ;;
        status)
            status
            ;;
        logs)
            shift
            logs "$@"
            ;;
        backup)
            backup
            ;;
        build)
            build
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            log_error "未知命令: $1"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
