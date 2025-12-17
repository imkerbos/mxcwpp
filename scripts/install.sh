#!/bin/bash

# Matrix Cloud Security Platform Agent 一键安装脚本
# 使用方法: curl -sS http://SERVER_IP:8080/agent/install.sh | bash

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 默认配置（可通过环境变量覆盖）
# SERVER_HOST 用于下载安装包，应该指向 Manager HTTP 服务器（例如：10.0.0.1:8080）
# BLS_SERVER_HOST 用于 Agent 连接，应该指向 AgentCenter gRPC 服务器（例如：10.0.0.1:6751）
# BLS_BUSINESS_LINE 业务线标识（可选，如果设置，Agent 安装后会自动绑定到该业务线）
# 如果未设置，将从脚本中自动替换（由 Manager API 动态替换）
SERVER_HOST="${BLS_HTTP_SERVER:-localhost:8080}"
AGENT_SERVER_HOST="${BLS_SERVER_HOST:-localhost:6751}"
BUSINESS_LINE="${BLS_BUSINESS_LINE:-}"
ARCH="${BLS_ARCH:-$(uname -m)}"
OS_TYPE="${BLS_OS_TYPE:-}"

# 检测操作系统类型
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS_TYPE="$ID"
        OS_VERSION="$VERSION_ID"
    elif [ -f /etc/redhat-release ]; then
        OS_TYPE="rhel"
    elif [ -f /etc/debian_version ]; then
        OS_TYPE="debian"
    else
        echo -e "${RED}Error: Unsupported operating system${NC}"
        exit 1
    fi
}

# 检测架构
detect_arch() {
    case "$(uname -m)" in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac
}

# 确定包管理器
determine_package_manager() {
    if command -v yum &> /dev/null; then
        PKG_MANAGER="yum"
        PKG_TYPE="rpm"
    elif command -v dnf &> /dev/null; then
        PKG_MANAGER="dnf"
        PKG_TYPE="rpm"
    elif command -v apt-get &> /dev/null; then
        PKG_MANAGER="apt-get"
        PKG_TYPE="deb"
    else
        echo -e "${RED}Error: No supported package manager found${NC}"
        exit 1
    fi
}

# 下载安装包
download_package() {
    echo -e "${GREEN}Downloading agent package...${NC}"
    
    # 构建下载 URL
    # SERVER_HOST 在脚本中会被替换为实际的 HTTP 服务器地址
    DOWNLOAD_URL="http://${SERVER_HOST}/api/v1/agent/download/${PKG_TYPE}/${ARCH}"
    
    TEMP_DIR=$(mktemp -d)
    PACKAGE_FILE="${TEMP_DIR}/mxsec-agent.${PKG_TYPE}"
    
    if command -v curl &> /dev/null; then
        curl -f -L -o "$PACKAGE_FILE" "$DOWNLOAD_URL"
    elif command -v wget &> /dev/null; then
        wget -O "$PACKAGE_FILE" "$DOWNLOAD_URL"
    else
        echo -e "${RED}Error: curl or wget is required${NC}"
        exit 1
    fi
    
    echo "$PACKAGE_FILE"
}

# 安装包
install_package() {
    PACKAGE_FILE="$1"
    
    echo -e "${GREEN}Installing agent...${NC}"
    
    if [ "$PKG_TYPE" = "rpm" ]; then
        if [ "$PKG_MANAGER" = "yum" ]; then
            yum install -y "$PACKAGE_FILE"
        else
            dnf install -y "$PACKAGE_FILE"
        fi
    else
        apt-get update
        apt-get install -y "$PACKAGE_FILE"
    fi
    
    rm -f "$PACKAGE_FILE"
    rmdir "$(dirname "$PACKAGE_FILE")"
}

# 配置业务线环境变量（如果提供了）
configure_business_line() {
    if [ -n "$BUSINESS_LINE" ]; then
        echo -e "${GREEN}Configuring business line: ${BUSINESS_LINE}${NC}"
        
        # 创建 systemd override 目录
        OVERRIDE_DIR="/etc/systemd/system/mxsec-agent.service.d"
        mkdir -p "$OVERRIDE_DIR"
        
        # 创建 override 配置文件
        OVERRIDE_FILE="$OVERRIDE_DIR/business-line.conf"
        cat > "$OVERRIDE_FILE" <<EOF
[Service]
Environment="BLS_BUSINESS_LINE=${BUSINESS_LINE}"
EOF
        
        echo -e "${GREEN}Business line configured in ${OVERRIDE_FILE}${NC}"
    fi
}

# 启动服务
start_service() {
    echo -e "${GREEN}Starting agent service...${NC}"
    
    # 配置业务线（如果提供了）
    configure_business_line
    
    systemctl daemon-reload
    systemctl enable mxsec-agent
    systemctl start mxsec-agent
    
    # 等待服务启动
    sleep 2
    
    if systemctl is-active --quiet mxsec-agent; then
        echo -e "${GREEN}Agent started successfully!${NC}"
        echo -e "${GREEN}Status: $(systemctl status mxsec-agent --no-pager -l | head -n 3)${NC}"
    else
        echo -e "${YELLOW}Warning: Agent service may not have started properly${NC}"
        echo -e "${YELLOW}Check logs: journalctl -u mxsec-agent${NC}"
    fi
}

# 主流程
main() {
    echo -e "${GREEN}=== Matrix Cloud Security Platform Agent Installer ===${NC}"
    echo ""
    
    # 检查 root 权限
    if [ "$EUID" -ne 0 ]; then
        echo -e "${RED}Error: This script must be run as root${NC}"
        exit 1
    fi
    
    # 检测系统信息
    detect_os
    detect_arch
    determine_package_manager
    
    echo -e "${GREEN}Detected: ${OS_TYPE} (${ARCH})${NC}"
    echo -e "${GREEN}HTTP Server: ${SERVER_HOST}${NC}"
    echo -e "${GREEN}Agent Server: ${AGENT_SERVER_HOST}${NC}"
    echo ""
    
    # 下载并安装
    PACKAGE_FILE=$(download_package)
    install_package "$PACKAGE_FILE"
    
    # 启动服务
    start_service
    
    echo ""
    echo -e "${GREEN}Installation completed!${NC}"
    echo -e "${GREEN}Agent will connect to server and download configuration automatically.${NC}"
}

# 执行主流程
main
