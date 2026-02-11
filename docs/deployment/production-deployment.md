# 生产环境部署方案

> 本文档提供 Matrix Cloud Security Platform 完整的生产环境部署方案。

---

## 1. 部署架构概览

```
                    ┌─────────────────────────────────────────────────────────┐
                    │                    生产环境架构                          │
                    └─────────────────────────────────────────────────────────┘

┌──────────────┐          ┌─────────────────────────────────────────────────────┐
│   用户浏览器  │──────────│                    负载均衡 / Nginx                   │
└──────────────┘          └─────────────────────────────────────────────────────┘
                                    │                           │
                          ┌─────────▼─────────┐       ┌─────────▼─────────┐
                          │   UI (前端静态)    │       │  Manager (HTTP)   │
                          │   Nginx / CDN     │       │    端口: 8080     │
                          │   端口: 80/443    │       │                   │
                          └───────────────────┘       └─────────┬─────────┘
                                                                │
                                                      ┌─────────▼─────────┐
                                                      │     MySQL 8.0     │
                                                      │    端口: 3306     │
                                                      └─────────▲─────────┘
                                                                │
┌──────────────────────────────────────────────────────────────┼────────────────┐
│                         Agent 主机集群                        │                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │                │
│  │   Agent 1   │  │   Agent 2   │  │   Agent N   │           │                │
│  │  + Baseline │  │  + Baseline │  │  + Baseline │           │                │
│  │  + Collect  │  │  + Collect  │  │  + Collect  │           │                │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘           │                │
│         │                │                │                   │                │
│         └────────────────┼────────────────┘                   │                │
│                          │                                    │                │
│                ┌─────────▼─────────┐                          │                │
│                │  AgentCenter      │──────────────────────────┘                │
│                │  (gRPC + mTLS)    │                                           │
│                │  端口: 6751       │                                           │
│                └───────────────────┘                                           │
└────────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 组件清单

| 组件 | 端口 | 协议 | 说明 |
|------|------|------|------|
| MySQL | 3306 | TCP | 数据存储 |
| AgentCenter | 6751 | gRPC + mTLS | Agent 通信服务 |
| Manager | 8080 | HTTP | 管理 API |
| UI | 80/443 | HTTP/HTTPS | Web 控制台 |
| Agent | - | - | 部署在被管主机 |

---

## 3. 服务器要求

### 3.1 Server 服务器（Manager + AgentCenter）

| 规模 | CPU | 内存 | 磁盘 | 网络 |
|------|-----|------|------|------|
| 小型 (<100 Agent) | 2 核 | 4 GB | 50 GB SSD | 100 Mbps |
| 中型 (100-500 Agent) | 4 核 | 8 GB | 100 GB SSD | 1 Gbps |
| 大型 (>500 Agent) | 8 核 | 16 GB | 200 GB SSD | 1 Gbps |

### 3.2 MySQL 数据库服务器

| 规模 | CPU | 内存 | 磁盘 | 说明 |
|------|-----|------|------|------|
| 小型 | 2 核 | 4 GB | 100 GB SSD | 可与 Server 同机 |
| 中型 | 4 核 | 8 GB | 200 GB SSD | 建议独立部署 |
| 大型 | 8 核 | 16 GB | 500 GB SSD | 主从复制 |

### 3.3 操作系统要求

**Server 端支持：**
- Rocky Linux 9 / 8（推荐）
- CentOS 7 / 8 / Stream 9
- Debian 11 / 12
- Ubuntu 20.04 / 22.04

**Agent 端支持：**
- Rocky Linux 9 / 8
- CentOS 7 / 8 / Stream 9
- Oracle Linux 7 / 8 / 9
- Debian 10 / 11 / 12
- Ubuntu 18.04 / 20.04 / 22.04
- AlmaLinux 8 / 9

---

## 4. 部署步骤

### 4.1 准备工作

#### 4.1.1 下载源码

```bash
# 克隆项目
git clone https://github.com/your-org/mxsec-platform.git
cd mxsec-platform

# 切换到稳定版本
git checkout v1.0.0
```

#### 4.1.2 安装依赖

```bash
# 安装 Go 1.21+
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# 安装 Node.js 18+（用于构建前端）
curl -fsSL https://rpm.nodesource.com/setup_18.x | sudo bash -
sudo yum install -y nodejs

# 安装 nFPM（用于打包 RPM/DEB）
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
```

---

### 4.2 部署 MySQL 数据库

#### 方式一：Docker 部署（推荐用于快速测试）

```bash
docker run -d \
  --name mxsec-mysql \
  -e MYSQL_ROOT_PASSWORD=your_root_password \
  -e MYSQL_DATABASE=mxsec \
  -e MYSQL_USER=mxsec_user \
  -e MYSQL_PASSWORD=your_password \
  -p 3306:3306 \
  -v mysql_data:/var/lib/mysql \
  mysql:8.0 \
  --character-set-server=utf8mb4 \
  --collation-server=utf8mb4_unicode_ci
```

#### 方式二：原生安装

```bash
# Rocky Linux 9 / CentOS 9
sudo dnf install -y mysql-server
sudo systemctl enable --now mysqld

# Debian / Ubuntu
sudo apt-get update
sudo apt-get install -y mysql-server
sudo systemctl enable --now mysql

# 初始化数据库
mysql -u root -p << EOF
CREATE DATABASE mxsec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'mxsec_user'@'%' IDENTIFIED BY 'your_secure_password';
GRANT ALL PRIVILEGES ON mxsec.* TO 'mxsec_user'@'%';
FLUSH PRIVILEGES;
EOF
```

---

### 4.3 部署 Server（AgentCenter + Manager）

#### 4.3.1 生成 mTLS 证书

```bash
# 生成证书（在项目根目录执行）
./scripts/generate-certs.sh

# 证书文件将生成在 certs/ 目录：
# - ca.crt, ca.key       : CA 证书
# - server.crt, server.key : Server 证书
# - agent.crt, agent.key   : Agent 证书（可选，用于预置）
```

#### 4.3.2 构建 Server 二进制

```bash
# 构建 Server
make build-server

# 输出：
# - dist/server/agentcenter
# - dist/server/manager
```

#### 4.3.3 部署 Server

```bash
# 创建目录
sudo mkdir -p /opt/mxsec-platform
sudo mkdir -p /etc/mxsec-platform/certs
sudo mkdir -p /var/log/mxsec-platform

# 复制二进制
sudo cp dist/server/agentcenter /opt/mxsec-platform/
sudo cp dist/server/manager /opt/mxsec-platform/
sudo chmod +x /opt/mxsec-platform/*

# 复制配置文件
sudo cp configs/server.yaml.example /etc/mxsec-platform/server.yaml

# 复制证书
sudo cp certs/* /etc/mxsec-platform/certs/

# 修改配置（重要！）
sudo vi /etc/mxsec-platform/server.yaml
```

#### 4.3.4 配置 server.yaml

```yaml
# /etc/mxsec-platform/server.yaml

server:
  grpc:
    host: "0.0.0.0"
    port: 6751
  http:
    host: "0.0.0.0"
    port: 8080

database:
  type: "mysql"
  mysql:
    host: "127.0.0.1"           # 数据库地址
    port: 3306
    user: "mxsec_user"
    password: "your_secure_password"  # 修改为实际密码
    database: "mxsec"
    max_idle_conns: 10
    max_open_conns: 100

mtls:
  ca_cert: "/etc/mxsec-platform/certs/ca.crt"
  server_cert: "/etc/mxsec-platform/certs/server.crt"
  server_key: "/etc/mxsec-platform/certs/server.key"

log:
  level: "info"
  format: "json"
  file: "/var/log/mxsec-platform/server.log"
  max_age: 30

plugins:
  dir: "/opt/mxsec-platform/plugins"
  base_url: "http://YOUR_SERVER_IP:8080/api/v1/plugins/download"  # 修改为实际 IP
```

#### 4.3.5 配置 systemd 服务

```bash
# 复制 systemd service 文件
sudo cp deploy/systemd/mxsec-agentcenter.service /etc/systemd/system/
sudo cp deploy/systemd/mxsec-manager.service /etc/systemd/system/

# 启动服务
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agentcenter mxsec-manager
sudo systemctl start mxsec-agentcenter mxsec-manager

# 检查状态
sudo systemctl status mxsec-agentcenter
sudo systemctl status mxsec-manager
```

#### 4.3.6 验证 Server

```bash
# 检查 AgentCenter (gRPC)
grpcurl -plaintext localhost:6751 list
# 预期输出：grpc.Transfer

# 检查 Manager (HTTP)
curl http://localhost:8080/health
# 预期输出：{"status":"ok"}

# 检查 API
curl http://localhost:8080/api/v1/hosts
curl http://localhost:8080/api/v1/policies
```

---

### 4.4 部署前端 UI

#### 方式一：Nginx 静态部署（推荐）

```bash
# 构建前端
cd ui
npm install
npm run build

# 部署到 Nginx
sudo mkdir -p /var/www/mxsec-ui
sudo cp -r dist/* /var/www/mxsec-ui/

# 配置 Nginx
sudo tee /etc/nginx/conf.d/mxsec.conf << 'EOF'
server {
    listen 80;
    server_name your-domain.com;  # 修改为实际域名或 IP

    root /var/www/mxsec-ui;
    index index.html;

    # 前端路由
    location / {
        try_files $uri $uri/ /index.html;
    }

    # API 代理
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 健康检查
    location /health {
        proxy_pass http://127.0.0.1:8080;
    }
}
EOF

# 重启 Nginx
sudo nginx -t
sudo systemctl reload nginx
```

#### 方式二：Docker 部署

```bash
# 构建 UI 镜像
docker build -f deploy/docker/Dockerfile.ui -t mxsec-ui:latest .

# 运行
docker run -d \
  --name mxsec-ui \
  -p 80:80 \
  --add-host=host.docker.internal:host-gateway \
  mxsec-ui:latest
```

---

### 4.5 部署插件

```bash
# 构建插件（所有架构）
make package-plugins-all VERSION=1.0.0

# 插件输出目录：
# dist/plugins/
# ├── baseline-linux-amd64
# ├── baseline-linux-arm64
# ├── collector-linux-amd64
# └── collector-linux-arm64

# 复制到 Server 插件目录
sudo mkdir -p /opt/mxsec-platform/plugins
sudo cp dist/plugins/* /opt/mxsec-platform/plugins/
```

---

### 4.6 部署 Agent

#### 4.6.1 构建 Agent 安装包

```bash
# 构建所有架构的 Agent 安装包
make package-agent-all VERSION=1.0.0 SERVER_HOST=YOUR_SERVER_IP:6751

# 输出：
# dist/packages/
# ├── mxsec-agent-1.0.0-1.el9.x86_64.rpm
# ├── mxsec-agent-1.0.0-1.el9.aarch64.rpm
# ├── mxsec-agent_1.0.0_amd64.deb
# └── mxsec-agent_1.0.0_arm64.deb
```

#### 4.6.2 安装 Agent

**RPM 系统 (Rocky/CentOS/Oracle):**

```bash
# 复制安装包到目标主机
scp dist/packages/mxsec-agent-1.0.0-1.el9.x86_64.rpm root@target-host:/tmp/

# 在目标主机安装
ssh root@target-host
sudo rpm -ivh /tmp/mxsec-agent-1.0.0-1.el9.x86_64.rpm
sudo systemctl enable --now mxsec-agent
```

**DEB 系统 (Debian/Ubuntu):**

```bash
# 复制安装包到目标主机
scp dist/packages/mxsec-agent_1.0.0_amd64.deb root@target-host:/tmp/

# 在目标主机安装
ssh root@target-host
sudo dpkg -i /tmp/mxsec-agent_1.0.0_amd64.deb
sudo systemctl enable --now mxsec-agent
```

#### 4.6.3 验证 Agent

```bash
# 检查 Agent 状态
sudo systemctl status mxsec-agent

# 查看 Agent 日志
sudo journalctl -u mxsec-agent -f

# 在 Server 端检查 Agent 是否上线
curl http://SERVER_IP:8080/api/v1/hosts
```

---

## 5. 防火墙配置

### 5.1 Server 端

```bash
# firewalld (Rocky/CentOS)
sudo firewall-cmd --permanent --add-port=6751/tcp  # AgentCenter gRPC
sudo firewall-cmd --permanent --add-port=8080/tcp  # Manager HTTP
sudo firewall-cmd --permanent --add-port=80/tcp    # UI HTTP
sudo firewall-cmd --permanent --add-port=443/tcp   # UI HTTPS（可选）
sudo firewall-cmd --reload

# iptables (Debian/Ubuntu)
sudo iptables -A INPUT -p tcp --dport 6751 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 80 -j ACCEPT
sudo iptables-save | sudo tee /etc/iptables.rules
```

### 5.2 Agent 端

Agent 只需要出站连接到 Server，通常不需要开放入站端口。

```bash
# 确保可以访问 Server
telnet SERVER_IP 6751
```

---

## 6. HTTPS 配置（可选但推荐）

### 6.1 使用 Let's Encrypt

```bash
# 安装 certbot
sudo dnf install -y certbot python3-certbot-nginx  # Rocky/CentOS
# 或
sudo apt-get install -y certbot python3-certbot-nginx  # Debian/Ubuntu

# 获取证书
sudo certbot --nginx -d your-domain.com

# 自动续期
sudo systemctl enable --now certbot-renew.timer
```

### 6.2 使用自签名证书

```bash
# 生成自签名证书
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nginx/ssl/mxsec.key \
  -out /etc/nginx/ssl/mxsec.crt \
  -subj "/CN=your-domain.com"
```

---

## 7. 日志管理

### 7.1 日志位置

| 组件 | 日志路径 |
|------|---------|
| AgentCenter | `/var/log/mxsec-platform/agentcenter.log` |
| Manager | `/var/log/mxsec-platform/manager.log` |
| Agent | `/var/log/mxsec-agent/agent.log` |
| Nginx | `/var/log/nginx/access.log`, `error.log` |

### 7.2 日志轮转

创建 `/etc/logrotate.d/mxsec`:

```
/var/log/mxsec-platform/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 0640 root root
    postrotate
        systemctl reload mxsec-agentcenter mxsec-manager > /dev/null 2>&1 || true
    endscript
}
```

---

## 8. 监控与告警

### 8.1 健康检查端点

```bash
# Manager 健康检查
curl http://localhost:8080/health

# Manager Prometheus metrics
curl http://localhost:8080/metrics
```

### 8.2 Prometheus 集成（可选）

在 `server.yaml` 中配置:

```yaml
metrics:
  prometheus:
    enabled: true
    remote_write_url: "http://prometheus:9090/api/v1/write"
    query_url: "http://prometheus:9090"
```

---

## 9. 备份策略

### 9.1 数据库备份

```bash
# 创建备份脚本 /opt/mxsec-platform/backup.sh
#!/bin/bash
BACKUP_DIR=/backup/mxsec
DATE=$(date +%Y%m%d_%H%M%S)
mkdir -p $BACKUP_DIR

# 备份数据库
mysqldump -u mxsec_user -p'your_password' mxsec > $BACKUP_DIR/mxsec_$DATE.sql

# 备份配置
tar -czf $BACKUP_DIR/config_$DATE.tar.gz /etc/mxsec-platform/

# 保留 30 天
find $BACKUP_DIR -name "*.sql" -mtime +30 -delete
find $BACKUP_DIR -name "*.tar.gz" -mtime +30 -delete
```

### 9.2 配置 cron 定时备份

```bash
# 每天凌晨 2 点备份
echo "0 2 * * * root /opt/mxsec-platform/backup.sh" | sudo tee /etc/cron.d/mxsec-backup
```

---

## 10. 常见问题排查

### 10.1 Agent 无法连接 Server

```bash
# 1. 检查网络连通性
telnet SERVER_IP 6751

# 2. 检查防火墙
sudo firewall-cmd --list-ports

# 3. 检查 AgentCenter 是否运行
sudo systemctl status mxsec-agentcenter

# 4. 查看 Agent 日志
sudo journalctl -u mxsec-agent -n 100
```

### 10.2 数据库连接失败

```bash
# 1. 检查 MySQL 状态
sudo systemctl status mysql

# 2. 测试连接
mysql -h localhost -u mxsec_user -p mxsec

# 3. 检查配置
cat /etc/mxsec-platform/server.yaml | grep -A 10 database
```

### 10.3 插件下载失败

```bash
# 1. 检查 plugins.base_url 配置
grep -A 5 plugins /etc/mxsec-platform/server.yaml

# 2. 测试下载 URL
curl -v http://SERVER_IP:8080/api/v1/plugins/download/baseline-linux-amd64

# 3. 检查插件目录权限
ls -la /opt/mxsec-platform/plugins/
```

---

## 11. 升级指南

### 11.1 Server 升级

```bash
# 1. 备份
mysqldump -u mxsec_user -p mxsec > upgrade_backup.sql
cp /etc/mxsec-platform/server.yaml /etc/mxsec-platform/server.yaml.bak

# 2. 停止服务
sudo systemctl stop mxsec-manager mxsec-agentcenter

# 3. 备份旧版本
sudo cp /opt/mxsec-platform/manager /opt/mxsec-platform/manager.bak
sudo cp /opt/mxsec-platform/agentcenter /opt/mxsec-platform/agentcenter.bak

# 4. 部署新版本
sudo cp dist/server/manager /opt/mxsec-platform/
sudo cp dist/server/agentcenter /opt/mxsec-platform/

# 5. 启动服务
sudo systemctl start mxsec-agentcenter mxsec-manager

# 6. 验证
curl http://localhost:8080/health
```

### 11.2 Agent 升级

Agent 支持通过 Server 统一下发升级，也可以手动升级：

```bash
# 手动升级
sudo systemctl stop mxsec-agent
sudo rpm -Uvh mxsec-agent-1.1.0-1.el9.x86_64.rpm
sudo systemctl start mxsec-agent
```

---

## 12. 参考文档

- [Server 部署指南](./server-deployment.md)
- [Agent 部署指南](./agent-deployment.md)
- [Server 配置文档](./server-config.md)
- [发行版支持](./distribution-support.md)
- [快速开始](./quick-start.md)

---

## 13. 联系支持

如遇到问题，请通过以下方式获取帮助：

1. 查看 [故障排查指南](../development/troubleshooting.md)
2. 提交 Issue: https://github.com/your-org/mxsec-platform/issues
