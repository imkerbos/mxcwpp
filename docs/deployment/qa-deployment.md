# QA 环境部署指南

本文档描述如何构建和部署各模块到 QA 测试环境。

---

## 1. 构建概述

| 模块 | 构建命令 | 输出位置 |
|------|---------|---------|
| Agent | `make build-agent` | `dist/agent/` |
| Server (Manager + AgentCenter) | `make build-server` | `dist/server/` |
| 前端 UI | `cd ui && npm run build` | `ui/dist/` |
| Agent RPM/DEB 包 | `make package-agent` | `dist/packages/` |
| Server RPM/DEB 包 | `make package-server` | `dist/packages/` |

---

## 2. Agent 构建与部署

### 2.1 构建 Agent 二进制

```bash
# 基本构建（使用默认配置）
make build-agent

# 指定 Server 地址（推荐用于 QA 环境）
make build-agent SERVER_HOST=qa-server.example.com:6751 VERSION=1.0.0-qa

# 指定目标架构
make build-agent SERVER_HOST=qa-server.example.com:6751 GOARCH=amd64 GOOS=linux
```

构建产物：
- `dist/agent/mxsec-agent` - Agent 主程序
- `dist/agent/plugins/baseline` - 基线检查插件

### 2.2 打包 Agent (RPM/DEB)

```bash
# 打包为 RPM (CentOS/Rocky Linux)
make package-agent SERVER_HOST=qa-server.example.com:6751 VERSION=1.0.0-qa DISTRO=rocky9

# 打包为 DEB (Debian/Ubuntu)
make package-agent SERVER_HOST=qa-server.example.com:6751 VERSION=1.0.0-qa DISTRO=debian12

# 支持的发行版
# RPM: centos7, centos8, centos9, rocky8, rocky9, el7, el8, el9
# DEB: debian10, debian11, debian12, ubuntu20, ubuntu22
```

构建产物：
- `dist/packages/mxsec-agent-1.0.0-qa.el9.x86_64.rpm`
- `dist/packages/mxsec-agent_1.0.0-qa_amd64.deb`

### 2.3 部署 Agent 到 QA 主机

**方式 1: 使用安装包（推荐）**

```bash
# 上传安装包到 QA 主机
scp dist/packages/mxsec-agent-1.0.0-qa.el9.x86_64.rpm user@qa-host:/tmp/

# SSH 登录 QA 主机并安装
ssh user@qa-host
sudo rpm -ivh /tmp/mxsec-agent-1.0.0-qa.el9.x86_64.rpm

# 启动服务
sudo systemctl enable mxsec-agent
sudo systemctl start mxsec-agent

# 验证
sudo systemctl status mxsec-agent
sudo journalctl -u mxsec-agent -f
```

**方式 2: 直接复制二进制**

```bash
# 上传二进制文件
scp -r dist/agent/* user@qa-host:/tmp/mxsec-agent/

# SSH 登录并部署
ssh user@qa-host

# 安装
sudo mkdir -p /opt/mxsec-agent/plugins
sudo cp /tmp/mxsec-agent/mxsec-agent /opt/mxsec-agent/
sudo cp /tmp/mxsec-agent/plugins/* /opt/mxsec-agent/plugins/
sudo chmod +x /opt/mxsec-agent/mxsec-agent
sudo chmod +x /opt/mxsec-agent/plugins/*

# 创建 systemd service
sudo cp /tmp/mxsec-agent/mxsec-agent.service /etc/systemd/system/
# 或从项目复制
# scp deploy/systemd/mxsec-agent.service user@qa-host:/etc/systemd/system/

# 启动
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agent
sudo systemctl start mxsec-agent
```

---

## 3. Server 构建与部署

### 3.1 构建 Server 二进制

```bash
# 构建 Server (Manager + AgentCenter)
make build-server
```

构建产物：
- `dist/server/manager` - HTTP API Server
- `dist/server/agentcenter` - gRPC Server

### 3.2 打包 Server (RPM/DEB)

```bash
# 打包为 RPM
make package-server VERSION=1.0.0-qa DISTRO=rocky9

# 打包为 DEB
make package-server VERSION=1.0.0-qa DISTRO=debian12
```

### 3.3 部署 Server 到 QA 环境

**前置条件**：
- MySQL 8.0+ 已安装并运行
- 已创建数据库和用户

```bash
# 1. 上传二进制和配置
scp dist/server/manager dist/server/agentcenter user@qa-server:/tmp/
scp configs/server.yaml.example user@qa-server:/tmp/server.yaml
scp -r deploy/systemd/mxsec-*.service user@qa-server:/tmp/

# 2. SSH 登录 QA Server
ssh user@qa-server

# 3. 安装
sudo mkdir -p /opt/mxsec-platform
sudo mkdir -p /etc/mxsec-platform
sudo mkdir -p /var/log/mxsec-platform

sudo cp /tmp/manager /opt/mxsec-platform/
sudo cp /tmp/agentcenter /opt/mxsec-platform/
sudo chmod +x /opt/mxsec-platform/*

# 4. 配置
sudo cp /tmp/server.yaml /etc/mxsec-platform/server.yaml
sudo vi /etc/mxsec-platform/server.yaml  # 修改数据库连接等配置

# 5. 生成证书（如果需要）
# 在开发机上生成后复制
make certs
scp certs/* user@qa-server:/etc/mxsec-platform/certs/

# 6. 安装 systemd 服务
sudo cp /tmp/mxsec-manager.service /etc/systemd/system/
sudo cp /tmp/mxsec-agentcenter.service /etc/systemd/system/

# 7. 启动服务
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agentcenter mxsec-manager
sudo systemctl start mxsec-agentcenter mxsec-manager

# 8. 验证
sudo systemctl status mxsec-agentcenter
sudo systemctl status mxsec-manager
curl http://localhost:8080/health
```

---

## 4. 前端 UI 构建与部署

### 4.1 构建前端

```bash
cd ui

# 安装依赖
npm install

# 构建生产版本
npm run build

# 构建产物在 ui/dist/ 目录
```

### 4.2 部署前端

**方式 1: 使用 Nginx 静态托管**

```bash
# 上传构建产物
scp -r ui/dist/* user@qa-server:/var/www/mxsec-ui/

# 配置 Nginx
sudo tee /etc/nginx/conf.d/mxsec-ui.conf > /dev/null <<'EOF'
server {
    listen 80;
    server_name qa.mxsec.example.com;

    root /var/www/mxsec-ui;
    index index.html;

    # 前端路由支持
    location / {
        try_files $uri $uri/ /index.html;
    }

    # API 代理
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
EOF

sudo nginx -t
sudo systemctl reload nginx
```

**方式 2: 集成到 Manager 服务**

如果 Manager 已配置静态文件服务，可以直接将前端文件放到指定目录：

```bash
scp -r ui/dist/* user@qa-server:/opt/mxsec-platform/ui/
```

---

## 5. 一键部署脚本

### 5.1 完整构建脚本

```bash
#!/bin/bash
# scripts/build-qa.sh

set -e

QA_SERVER=${QA_SERVER:-"qa-server.example.com"}
QA_GRPC_PORT=${QA_GRPC_PORT:-"6751"}
VERSION=${VERSION:-"1.0.0-qa"}

echo "=== Building for QA Environment ==="
echo "Server: ${QA_SERVER}:${QA_GRPC_PORT}"
echo "Version: ${VERSION}"

# 构建 Agent
echo ">>> Building Agent..."
make build-agent SERVER_HOST=${QA_SERVER}:${QA_GRPC_PORT} VERSION=${VERSION}

# 构建 Server
echo ">>> Building Server..."
make build-server

# 构建前端
echo ">>> Building UI..."
cd ui && npm install && npm run build && cd ..

# 打包（可选）
echo ">>> Packaging..."
make package-agent SERVER_HOST=${QA_SERVER}:${QA_GRPC_PORT} VERSION=${VERSION} DISTRO=rocky9
make package-server VERSION=${VERSION} DISTRO=rocky9

echo "=== Build Complete ==="
echo "Agent: dist/agent/"
echo "Server: dist/server/"
echo "UI: ui/dist/"
echo "Packages: dist/packages/"
```

### 5.2 部署脚本

```bash
#!/bin/bash
# scripts/deploy-qa.sh

set -e

QA_SERVER=${QA_SERVER:-"qa-server.example.com"}
QA_USER=${QA_USER:-"root"}

echo "=== Deploying to QA Server: ${QA_SERVER} ==="

# 停止服务
echo ">>> Stopping services..."
ssh ${QA_USER}@${QA_SERVER} "systemctl stop mxsec-manager mxsec-agentcenter || true"

# 上传文件
echo ">>> Uploading binaries..."
scp dist/server/manager dist/server/agentcenter ${QA_USER}@${QA_SERVER}:/opt/mxsec-platform/

# 上传前端
echo ">>> Uploading UI..."
ssh ${QA_USER}@${QA_SERVER} "mkdir -p /var/www/mxsec-ui"
scp -r ui/dist/* ${QA_USER}@${QA_SERVER}:/var/www/mxsec-ui/

# 启动服务
echo ">>> Starting services..."
ssh ${QA_USER}@${QA_SERVER} "systemctl start mxsec-agentcenter mxsec-manager"

# 验证
echo ">>> Verifying..."
ssh ${QA_USER}@${QA_SERVER} "systemctl status mxsec-agentcenter --no-pager"
ssh ${QA_USER}@${QA_SERVER} "systemctl status mxsec-manager --no-pager"
ssh ${QA_USER}@${QA_SERVER} "curl -s http://localhost:8080/health"

echo "=== Deployment Complete ==="
```

---

## 6. QA 环境配置示例

### 6.1 server.yaml (QA 环境)

```yaml
# /etc/mxsec-platform/server.yaml

server:
  http:
    port: 8080
    host: "0.0.0.0"
  grpc:
    port: 6751
    host: "0.0.0.0"

database:
  type: "mysql"
  mysql:
    host: "qa-mysql.example.com"
    port: 3306
    user: "mxsec_qa"
    password: "qa_password"
    database: "mxsec_qa"
    max_idle_conns: 10
    max_open_conns: 50

log:
  level: "debug"  # QA 环境使用 debug 级别
  format: "json"
  file: "/var/log/mxsec-platform/server.log"

mtls:
  enabled: true
  ca_cert: "/etc/mxsec-platform/certs/ca.crt"
  server_cert: "/etc/mxsec-platform/certs/server.crt"
  server_key: "/etc/mxsec-platform/certs/server.key"
```

---

## 7. 验证清单

### 7.1 Server 验证

```bash
# 检查服务状态
systemctl status mxsec-agentcenter
systemctl status mxsec-manager

# 检查端口
netstat -tuln | grep -E '6751|8080'

# 健康检查
curl http://localhost:8080/health

# API 测试
curl http://localhost:8080/api/v1/hosts
curl http://localhost:8080/api/v1/policies

# 查看日志
journalctl -u mxsec-manager -f
journalctl -u mxsec-agentcenter -f
```

### 7.2 Agent 验证

```bash
# 检查服务状态
systemctl status mxsec-agent

# 检查 Agent ID
cat /var/lib/mxsec-agent/agent_id

# 检查连接
journalctl -u mxsec-agent -f | grep -i connect

# 在 Server 端确认 Agent 已注册
curl http://qa-server:8080/api/v1/hosts
```

### 7.3 前端验证

```bash
# 检查 Nginx 配置
nginx -t

# 访问前端页面
curl -I http://qa.mxsec.example.com

# 检查 API 代理
curl http://qa.mxsec.example.com/api/v1/health
```

---

## 8. 常见问题

### Q1: Agent 构建时如何指定 Server 地址？

```bash
# 通过环境变量
make build-agent SERVER_HOST=10.0.0.1:6751

# Server 地址会在编译时嵌入二进制，无需额外配置文件
```

### Q2: 如何查看构建时嵌入的配置？

```bash
# 运行 Agent 查看版本信息
./dist/agent/mxsec-agent --version
```

### Q3: 如何更新已部署的 Agent？

```bash
# 方式 1: 重新构建并部署
make build-agent SERVER_HOST=qa-server:6751 VERSION=1.0.1-qa
scp dist/agent/mxsec-agent user@qa-host:/opt/mxsec-agent/
ssh user@qa-host "systemctl restart mxsec-agent"

# 方式 2: 使用新安装包
make package-agent SERVER_HOST=qa-server:6751 VERSION=1.0.1-qa DISTRO=rocky9
scp dist/packages/*.rpm user@qa-host:/tmp/
ssh user@qa-host "rpm -Uvh /tmp/mxsec-agent-*.rpm && systemctl restart mxsec-agent"
```

### Q4: 如何回滚到之前的版本？

```bash
# 备份当前版本
ssh user@qa-server "cp /opt/mxsec-platform/manager /opt/mxsec-platform/manager.bak"

# 恢复旧版本
ssh user@qa-server "cp /opt/mxsec-platform/manager.bak /opt/mxsec-platform/manager"
ssh user@qa-server "systemctl restart mxsec-manager"
```

---

## 9. 参考文档

- [Server 部署指南](./server-deployment.md)
- [Agent 部署指南](./agent-deployment.md)
- [配置说明](./server-config.md)
- [Makefile 帮助](../../Makefile) - 运行 `make help` 查看所有命令
