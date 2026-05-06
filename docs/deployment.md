# 安装部署

## 前置要求

### 服务端

- Linux 服务器（CentOS 7+ / Rocky Linux 8+ / Ubuntu 20.04+）
- Docker Engine >= 24, Docker Compose v2
- 时钟同步（NTP），所有节点需保持时钟一致

### Agent 目标主机

- 支持的操作系统（参见下方平台支持列表）
- Linux 内核 >= 4.18（eBPF / Tetragon 要求）
- 网络可达 AgentCenter gRPC 端口（默认 6751）

### 平台支持

| 发行版 | 版本 |
|--------|------|
| Rocky Linux | 9, 10 |
| Oracle Linux | 7, 8, 9 |
| CentOS | 7, 8, 9 |
| Debian | 10, 11, 12 |
| Ubuntu | 20.04, 22.04 |

运行时类型：物理机 / VM、Docker 容器宿主机、Kubernetes 节点

---

## 快速部署

适用于开发测试或快速体验。

```bash
git clone https://github.com/mxsec/mxsec-platform.git
cd mxsec-platform/deploy

cp .env.example .env
vim .env   # 修改 SERVER_IP / JWT_SECRET / 数据库密码

docker compose --env-file .env up -d \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

验证：

```bash
docker compose ps
curl -X POST http://localhost/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
```

访问 `http://<SERVER_IP>` 进入管理界面，默认账户 `admin / admin123`。

---

## 开发环境

开发环境采用 `docker-compose + air` 热更新模式，不构建 Agent / 插件产物。

```bash
make dev-docker-up       # 启动开发环境
make dev-docker-logs     # 查看日志
make dev-docker-down     # 停止服务
```

| 服务 | 地址 |
|------|------|
| Manager API | http://localhost:8080 |
| UI | http://localhost:3000 |
| MySQL | localhost:3306 |

---

## 生产部署

### 部署形态选型

| 形态 | 节点数 | Agent 规模 | 适用场景 |
|------|--------|-----------|---------|
| 单机 All-in-One | 1 台 | <= 50 | 评估试用、小型内网 |
| 标准生产 | 3 台 | 50 - 500 | 中小型企业 |
| 高规格生产 | 5+ 台 | 500+ | 大规模多集群 |

### 单机 All-in-One

所有服务运行在同一台机器，Docker Compose 编排。

**硬件要求**：8 核 CPU / 32 GB 内存 / 100 GB SSD 系统盘 / 200 GB 数据盘（推荐：16 核 / 64 GB / 500 GB SSD）

```bash
cd deploy/
cp .env.example .env
vim .env
# 修改: SERVER_IP / JWT_SECRET / 数据库密码 / *_REPLICAS=2

docker compose --env-file .env up -d \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

### 标准生产（3 节点）

控制面、存储层、消息队列分离部署。

```
┌────────────────────┐    ┌────────────────────┐    ┌────────────────────┐
│    Node 1 控制面    │    │    Node 2 存储层    │    │  Node 3 消息队列    │
│                    │    │                    │    │                    │
│  Nginx (80/443)    │    │  MySQL 8.0         │    │  Kafka Broker ×3   │
│  Manager ×2        │    │  Redis 7           │    │  (KRaft 模式)      │
│  AgentCenter ×2    │    │  ClickHouse 24     │    │                    │
│  Consumer ×2       │    │  Prometheus        │    │                    │
│  UI (SPA)          │    │                    │    │                    │
└────────────────────┘    └────────────────────┘    └────────────────────┘
```

**硬件要求**：

| 节点 | CPU | 内存 | 系统盘 | 数据盘 |
|------|-----|------|--------|--------|
| Node 1 控制面 | 8 核 | 32 GB | 100 GB SSD | 100 GB |
| Node 2 存储层 | 8 核 | 32 GB | 100 GB SSD | 500 GB SSD |
| Node 3 消息队列 | 4 核 | 16 GB | 50 GB SSD | 200 GB SSD |

**部署顺序**：Node 3（Kafka）→ Node 2（存储）→ Node 1（控制面）

Node 1 的 `.env` 配置要点：

```bash
SERVER_IP=<Node1 外部 IP>
MYSQL_HOST=<Node2_IP>
REDIS_ADDR=<Node2_IP>:6379
KAFKA_BROKER_1=<Node3_IP>:9092
KAFKA_BROKER_2=<Node3_IP>:9094
KAFKA_BROKER_3=<Node3_IP>:9095
CLICKHOUSE_ADDR=<Node2_IP>:9000
PROMETHEUS_QUERY_URL=http://<Node2_IP>:9090
```

### 高规格生产（5+ 节点）

控制面各组件独立部署，存储层可选主从。

```
Node 1: Manager ×2 + Nginx / UI
Node 2: AgentCenter ×2 + HAProxy(:6751)
Node 3: Consumer ×2
Node 4: MySQL + Redis + ClickHouse + Prometheus
Node 5: Kafka Broker ×3 (KRaft)
Node 6: MySQL 从库 + Redis Sentinel（可选）
```

Node 1-3 可继续水平扩展，Docker Compose `--scale` 或 systemd 部署均可。

---

## 证书管理

```bash
# 生成自签名 mTLS 证书
./scripts/generate-certs.sh

# 证书目录结构
deploy/certs/
├── ca.crt / ca.key            # CA 证书
├── server.crt / server.key    # AgentCenter 使用
└── client.crt / client.key    # 下发给 Agent
```

Agent 首次连接时 AgentCenter 自动下发证书，后续连接切换为正式 mTLS。

---

## 镜像构建

```bash
# 构建所有镜像
./scripts/build-images.sh --version v1.0.0

# 构建单个组件
./scripts/build-images.sh --version v1.0.0 --component agentcenter
```

镜像建议在目标架构的机器上构建，避免跨平台问题。

---

## Agent 部署

### 构建安装包

```bash
# 构建 Agent RPM/DEB（SERVER_HOST 编译时嵌入，指向 AC gRPC 入口）
make package-agent-all VERSION=1.0.0 SERVER_HOST=<AC_LB_IP>:6751

# 构建插件包
make package-plugins-all VERSION=1.0.0
```

> `SERVER_HOST` 是 Agent 构建参数，不是服务端配置项。生产环境应指向四层 LB 或 AC 的外部入口地址。

### 安装

```bash
# RPM (CentOS / Rocky / Oracle)
sudo rpm -ivh mxsec-agent-1.0.0.x86_64.rpm

# DEB (Debian / Ubuntu)
sudo dpkg -i mxsec-agent_1.0.0_amd64.deb
```

### 目录结构

| 路径 | 说明 |
|------|------|
| `/var/lib/mxsec-agent/` | 工作目录 |
| `/var/lib/mxsec-agent/certs/` | 证书目录 |
| `/var/lib/mxsec-agent/plugin/` | 插件目录 |
| `/var/log/mxsec-agent/` | 日志目录 |

### 管理

```bash
systemctl status mxsec-agent
systemctl restart mxsec-agent
journalctl -u mxsec-agent -f
```

---

## 升级

### 服务端升级

推荐使用 `deploy.sh upgrade`（自动备份 → 更新版本 → 重建容器）：

```bash
git pull origin main
./scripts/build-images.sh --version v1.1.0
cd deploy && ./deploy.sh upgrade
```

手动升级时必须显式带 `--scale` 保持副本数：

```bash
cd deploy
sed -i 's/^VERSION=.*/VERSION=v1.1.0/' .env
docker compose --env-file .env up -d \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

> `docker compose restart` 只重启容器不切换镜像，升级必须用 `up -d`。

### Agent 升级

```bash
# 服务端推送（管理界面触发）
# CLI 主动更新
mxsec-agent --update
mxsec-agent --update --server http://manager:8080
# 本地包更新
mxsec-agent --update --file ./mxsec-agent-1.1.0.rpm
```

---

## 备份

```bash
./deploy/deploy.sh backup
```

备份文件存放在 `deploy/backups/` 目录，包含数据库和配置文件。

---

## 网络与端口

| 端口 | 协议 | 方向 | 说明 |
|------|------|------|------|
| 80 / 443 | HTTP/S | 用户 → Nginx | 管理界面 + API |
| 6751 | gRPC | Agent → AC | AgentCenter 接入（mTLS），生产必须接 L4 LB |
| 3306 | TCP | 内网 | MySQL |
| 6379 | TCP | 内网 | Redis |
| 9000 | TCP | 内网 | ClickHouse Native |
| 8123 | HTTP | 运维 | ClickHouse HTTP |
| 9092-9095 | TCP | 内网 | Kafka Broker |
| 9090 | HTTP | 内网 | Prometheus |

**防火墙规则**：仅 80/443 和 6751 对外开放，存储层端口限制为控制面节点访问。

---

## 存储容量估算

以 100 台 Agent、30 天保留为基准：

| 数据类型 | 存储位置 | 估算量 |
|---------|---------|--------|
| 心跳 | MySQL | ~4 GB |
| 资产指纹 | MySQL | ~1 GB |
| 基线结果 | ClickHouse | ~180 MB |
| FIM 事件 | ClickHouse | ~150 MB |
| eBPF 事件 | ClickHouse | ~130 GB（TTL 30 天自动清理） |
| 告警 | MySQL + ClickHouse | ~500 MB |
| Kafka 消息 | Kafka 磁盘 | ~50 GB 峰值（保留 72h） |

eBPF 事件量取决于 Tetragon 策略配置，建议根据试运行数据调整磁盘规划。

---

## 健康检查

```bash
# 服务状态
docker compose ps

# API 健康检查
curl http://localhost/health
curl http://localhost/api/v1/health

# AC 服务发现（需认证）
TOKEN=$(curl -s -X POST http://localhost/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.token')
curl -H "Authorization: Bearer $TOKEN" http://localhost/api/v1/discovery/agentcenter

# Consumer 消费状态
docker compose logs consumer | tail -20

# Agent 连通性（需已暴露 AC gRPC 端口）
nc -zv <AC_HOST> 6751
```
