# 服务端部署

V2 控制面部署指南，适用于 `UI + Manager + AgentCenter + Consumer + MySQL + Redis + Kafka + ClickHouse` 组合部署。

## 架构

```
Nginx(:80/:443) → UI 静态文件 + 反向代理 → Manager × 2(:8080)
AgentCenter × 2(容器内 :6751 gRPC / :8080 HTTP 管理口) ← gRPC ← Agent
AgentCenter → Kafka → Consumer × 2 → MySQL / ClickHouse / Redis
Manager ↔ Redis SD / MySQL / ClickHouse / Prometheus
```

所有组件通过 Docker Compose 编排。控制面支持多副本，存储层是否具备主从容灾取决于实际生产部署方案。

默认 `deploy/docker-compose.yml` 直接对宿主机暴露的是 `ui`、MySQL、Redis、Kafka、ClickHouse；`manager` 不单独 publish，管理 API 通过 Nginx `/api/*` 访问。`agentcenter` 的 gRPC 入口需要按生产环境额外做端口映射，或接入 [HAProxy 配置模板](../../deploy/config/haproxy-agentcenter.cfg) / 四层负载均衡。

## 前置要求

- Linux 服务器（CentOS 7+/Ubuntu 18+）
- Docker >= 20.10, Docker Compose >= 2.0
- 建议 8GB+ RAM, 50GB+ 磁盘（启用 Kafka / ClickHouse 时更高）
- 开放端口: 80/443 (UI/API), 6751 (仅在额外暴露 AC gRPC 时), 13306/16379/9092/9094/9095/8123/9000（按实际是否暴露决定）

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
LOG_LEVEL=info

# ClickHouse
CLICKHOUSE_ENABLED=true
CLICKHOUSE_ADDR=clickhouse:9000

# Kafka
KAFKA_ENABLED=true
KAFKA_BROKER_1=kafka-1:9092
KAFKA_BROKER_2=kafka-2:9092
KAFKA_BROKER_3=kafka-3:9092

# 连接池
DB_MAX_IDLE_CONNS=10
DB_MAX_OPEN_CONNS=100
DB_CONN_MAX_LIFETIME=1h
```

**注意**：`SERVER_HOST` 不是服务端 `.env` 配置项，而是 Agent 构建参数（`make package-agent-all SERVER_HOST=...`），决定 Agent 连接哪个 AC gRPC 地址。`.env` 中只有 `SERVER_IP`（宿主机 IP）和 `GRPC_PORT`（容器内端口）。默认 compose 不直接暴露 `6751`，需额外做端口映射或前置 L4 LB，再将该地址传入 `SERVER_HOST` 编译进 Agent 包。

### server.yaml.tpl

模板文件位于 `deploy/config/server.yaml.tpl`，启动时自动用 .env 变量替换 `__XXX__` 占位符。

### Nginx

配置文件: `deploy/config/nginx.conf`。负责:
- 托管 UI 静态文件 (`/`)
- 反向代理 API 请求到多实例 Manager (`/api/` → upstream Manager)
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
docker compose --env-file .env up -d \
  --scale manager=2 \
  --scale agentcenter=2 \
  --scale consumer=2
```

`deploy/docker-compose.yml` 已按多副本拓扑编排 `manager / agentcenter / consumer`。实际启动时建议显式传入 `--scale`，避免不同 Compose 运行时对 `deploy.replicas` 的处理差异。

## 升级

推荐走 `deploy.sh upgrade`（自动备份 → 更新版本号 → 重新渲染 `server.yaml` → 按 `.env` 的 `*_REPLICAS` 重建容器）：

```bash
# 1. 拉取最新代码并构建镜像
git pull origin main
./scripts/build-images.sh --version v1.1.0

# 2. 确认 .env 里控制面副本数符合预期（HA 至少 2）
#    MANAGER_REPLICAS / AGENTCENTER_REPLICAS / CONSUMER_REPLICAS
vim deploy/.env

# 3. 执行升级（交互式输入新版本号）
cd deploy && ./deploy.sh upgrade
```

如需手动执行（跳过脚本），必须显式带 `--scale` 保持副本数一致：

```bash
cd deploy
sed -i 's/^VERSION=.*/VERSION=v1.1.0/' .env
docker compose --env-file .env up -d \
  --scale manager=2 \
  --scale agentcenter=2 \
  --scale consumer=2
```

**注意**：
- `docker compose restart` 只重启容器，不会切换到新镜像，升级必须用 `up -d`。
- `up -d` 不带 `--scale` 会按 Compose 默认或当前状态处理副本，容易把 HA 副本数缩回 1；`deploy.sh upgrade` 通过读取 `.env` 的 `*_REPLICAS` 规避这个问题。

## 备份

```bash
./deploy/deploy.sh backup    # 备份数据库 + 配置
```

备份文件存放在 `deploy/backups/` 目录。

## 日志

```bash
docker compose logs -f agentcenter   # AgentCenter 日志
docker compose logs -f manager       # Manager 日志
docker compose logs -f consumer      # Consumer 日志
docker compose logs -f ui            # Nginx 日志
docker compose logs -f mysql         # MySQL 日志
```

## 健康检查

```bash
# 检查服务状态
docker compose ps

# 测试 UI / API 入口
curl http://localhost/health
curl http://localhost/api/v1/health
curl http://localhost/api/v1/auth/login -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 检查 AC gRPC 暴露（仅在你已额外暴露 6751 或接入 LB 时）
nc -zv localhost 6751
```
