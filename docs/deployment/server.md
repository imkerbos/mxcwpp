# 服务端部署

AgentCenter + Manager + UI 的生产部署指南。

## 架构

```
Nginx(:80) → UI 静态文件 + 反向代理 → Manager(:8080)
AgentCenter(:6751) ← gRPC ← Agent
MySQL(:3306) ← 共享数据库
Redis(:6379) ← 缓存
```

所有组件通过 Docker Compose 编排。

## 前置要求

- Linux 服务器（CentOS 7+/Ubuntu 18+）
- Docker >= 20.10, Docker Compose >= 2.0
- 2GB+ RAM, 20GB+ 磁盘
- 开放端口: 80 (UI), 6751 (gRPC), 3306 (MySQL, 可选)

## 配置

### .env 文件

```bash
VERSION=v1.0.0

# MySQL
MYSQL_ROOT_PASSWORD=strong_password
MYSQL_USER=mxsec
MYSQL_PASSWORD=strong_password
MYSQL_DATABASE=mxsec
MYSQL_HOST=mysql
MYSQL_PORT=3306

# Redis
REDIS_ADDR=redis:6379
REDIS_PASSWORD=

# Server
JWT_SECRET=random_strong_string
SERVER_HOST=your-domain-or-ip:6751
LOG_LEVEL=info

# ClickHouse (可选)
CLICKHOUSE_ENABLED=false
CLICKHOUSE_ADDR=clickhouse:9000

# Kafka (可选)
KAFKA_ENABLED=false
KAFKA_BROKERS=kafka:9092

# 连接池
DB_MAX_IDLE_CONNS=10
DB_MAX_OPEN_CONNS=100
DB_CONN_MAX_LIFETIME=1h
```

### server.yaml.tpl

模板文件位于 `deploy/config/server.yaml.tpl`，启动时自动用 .env 变量替换 `__XXX__` 占位符。

### Nginx

配置文件: `deploy/config/nginx.conf`。负责:
- 托管 UI 静态文件 (`/`)
- 反向代理 API 请求 (`/api/` → Manager:8080)
- 反向代理插件下载 (`/uploads/` → Manager:8080)

## 证书

```bash
# 生成自签名证书
./scripts/generate-certs.sh

# 证书存放
deploy/certs/
├── ca.crt / ca.key
├── server.crt / server.key    # AgentCenter 使用
└── client.crt / client.key    # 下发给 Agent
```

## 构建镜像

镜像需在目标服务器上构建（避免跨平台问题）:

```bash
# 构建所有镜像
./scripts/build-images.sh --version v1.0.0

# 构建单个镜像
./scripts/build-images.sh --version v1.0.0 --component agentcenter
```

## 启动

```bash
cd deploy
docker compose --env-file .env up -d
```

## 升级

```bash
# 1. 拉取最新代码
git pull origin main

# 2. 构建新版本镜像
./scripts/build-images.sh --version v1.1.0

# 3. 更新版本号
sed -i 's/^VERSION=.*/VERSION=v1.1.0/' deploy/.env

# 4. 重建容器（不是 restart，restart 不换镜像）
cd deploy && docker compose --env-file .env up -d
```

**注意**: `docker compose restart` 只重启容器，不会切换到新镜像。必须用 `docker compose up -d`。

## 备份

```bash
./deploy/deploy.sh backup    # 备份数据库 + 配置
```

备份文件存放在 `deploy/backups/` 目录。

## 日志

```bash
docker compose logs -f agentcenter   # AgentCenter 日志
docker compose logs -f manager       # Manager 日志
docker compose logs -f ui            # Nginx 日志
docker compose logs -f mysql         # MySQL 日志
```

## 健康检查

```bash
# 检查服务状态
docker compose ps

# 测试 API
curl http://localhost:8080/api/v1/auth/login -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 检查 gRPC 端口
nc -zv localhost 6751
```
