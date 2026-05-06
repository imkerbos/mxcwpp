# 配置说明

## 配置体系

MxSec 服务端配置由两部分组成：

- **环境变量**（`.env`）：部署级参数，如数据库地址、密码、副本数
- **配置文件**（`server.yaml`）：应用级参数，由 `.env` 渲染模板生成

配置文件链路：

```
deploy/.env → deploy/config/server.yaml.tpl → server.yaml（运行时生效）
```

`deploy.sh` 启动时自动将 `.env` 中的变量替换到 `server.yaml.tpl` 的 `__XXX__` 占位符。生产环境不要直接编辑渲染后的 `server.yaml`，应修改 `.env` 后重新生成。

---

## 环境变量（.env）

### 基础配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `VERSION` | 镜像版本号 | v1.0.0 |
| `SERVER_IP` | 宿主机 IP | - |
| `JWT_SECRET` | JWT 签名密钥（生产必须替换） | - |
| `LOG_LEVEL` | 日志级别 | info |

### MySQL

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MYSQL_HOST` | 主机地址 | mysql |
| `MYSQL_PORT` | 端口 | 3306 |
| `MYSQL_USER` | 用户名 | mxsec |
| `MYSQL_PASSWORD` | 密码 | - |
| `MYSQL_DATABASE` | 数据库名 | mxsec |
| `MYSQL_ROOT_PASSWORD` | root 密码 | - |
| `DB_MAX_IDLE_CONNS` | 空闲连接数 | 10 |
| `DB_MAX_OPEN_CONNS` | 最大连接数 | 100 |
| `DB_CONN_MAX_LIFETIME` | 连接最大存活时间 | 1h |

### Redis

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `REDIS_ADDR` | 地址 | redis:6379 |
| `REDIS_PASSWORD` | 密码 | （空） |
| `REDIS_DB` | 数据库编号 | 0 |

Sentinel 模式配置项已预留，需要时在 `server.yaml.tpl` 中启用。

### Kafka

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `KAFKA_ENABLED` | 是否启用 | true |
| `KAFKA_BROKER_1` | Broker 1 地址 | kafka-1:9092 |
| `KAFKA_BROKER_2` | Broker 2 地址 | kafka-2:9092 |
| `KAFKA_BROKER_3` | Broker 3 地址 | kafka-3:9092 |

### ClickHouse

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CLICKHOUSE_ENABLED` | 是否启用 | true |
| `CLICKHOUSE_ADDR` | 地址（Native 协议） | clickhouse:9000 |

### 控制面副本数

| 变量 | 说明 | 推荐值 |
|------|------|--------|
| `MANAGER_REPLICAS` | Manager 副本数 | 2 |
| `AGENTCENTER_REPLICAS` | AgentCenter 副本数 | 2 |
| `CONSUMER_REPLICAS` | Consumer 副本数 | 2 |

### 插件与 Agent

| 变量 | 说明 |
|------|------|
| `PLUGINS_DIR` | 服务端插件存放目录 |
| `PLUGINS_BASE_URL` | Agent 下载插件的 URL 前缀（必须 Agent 可达） |

> `SERVER_HOST` 不是 `.env` 配置项，而是 Agent 构建参数（`make package-agent-all SERVER_HOST=...`），编译时嵌入 Agent 二进制，决定 Agent 连接哪个 AC gRPC 入口。

---

## 服务端配置文件（server.yaml）

### server

```yaml
server:
  grpc:
    host: "0.0.0.0"
    port: 6751          # AgentCenter gRPC 监听端口
  http:
    host: "0.0.0.0"
    port: 8080          # Manager / AC HTTP 管理端口
  jwt_secret: "xxx"     # JWT 签名密钥
  manager_addr: "http://manager:8080"   # AC 向 Manager 注册使用的地址
  instance_id: ""       # 多实例部署时的实例标识（留空自动生成）
```

### database

```yaml
database:
  driver: mysql
  host: "mysql"
  port: 3306
  user: "mxsec"
  password: "xxx"
  dbname: "mxsec"
  max_idle_conns: 10
  max_open_conns: 100
  conn_max_lifetime: "1h"
```

### redis

```yaml
redis:
  addr: "redis:6379"
  password: ""
  db: 0
  # Sentinel 模式（可选）
  # sentinel:
  #   master_name: "mymaster"
  #   addrs: ["sentinel-1:26379", "sentinel-2:26379", "sentinel-3:26379"]
```

### kafka

```yaml
kafka:
  enabled: true
  brokers:
    - "kafka-1:9092"
    - "kafka-2:9092"
    - "kafka-3:9092"
  topic_prefix: "mxsec"    # 环境隔离前缀
```

### clickhouse

```yaml
clickhouse:
  enabled: true
  addr: "clickhouse:9000"
  database: "mxsec"
```

### mtls

```yaml
mtls:
  ca_cert: "/app/certs/ca.crt"
  server_cert: "/app/certs/server.crt"
  server_key: "/app/certs/server.key"
```

### log

```yaml
log:
  level: "info"           # debug / info / warn / error
  format: "json"          # json / console
  file: "/app/logs/server.log"
  error_file: "/app/logs/error.log"
  max_age: 30             # 日志保留天数
```

### plugins

```yaml
plugins:
  dir: "/app/plugins"
  base_url: "http://<SERVER_IP>/uploads/plugins"   # Agent 可达的下载地址
```

---

## Agent 配置

Agent 配置分为编译时嵌入和运行时服务端下发两部分。

### 编译时参数

通过 `make package-agent-all` 嵌入：

| 参数 | 说明 |
|------|------|
| `VERSION` | Agent 版本号 |
| `SERVER_HOST` | AC gRPC 入口地址（IP:Port 或域名:Port） |

### 运行时配置

服务端通过 gRPC 下发，Agent 合并本地默认配置：

```yaml
agent:
  heartbeat_interval: 60     # 心跳间隔（秒）
  work_dir: "/var/lib/mxsec-agent"
```

### 服务发现

Agent 支持两种方式发现 AgentCenter：

1. **SD 接口**：查询 Manager `GET /api/v1/discovery/agentcenter`，获取健康 AC 列表
2. **静态地址**：配置文件中写死 AC 地址列表，作为 SD 不可用时的回退

```yaml
server:
  agent_center:
    discovery_url: "http://manager-lb:8080/api/v1/discovery/agentcenter"
    addresses:                    # 回退静态地址
      - "agentcenter-1:6751"
      - "agentcenter-2:6751"
```

Agent 使用 power-of-two-choices 算法选择负载较低的 AC 实例。

---

## Nginx 配置

配置文件：`deploy/config/nginx.conf`

主要职责：
- 托管 UI 静态文件（`/`）
- 反向代理 API 到 Manager 集群（`/api/` → upstream manager）
- 反向代理插件下载（`/uploads/` → Manager:8080）

多 Manager 实例的 upstream 配置示例：

```nginx
upstream mxsec-manager {
    least_conn;
    server manager-1:8080;
    server manager-2:8080;
}
```

---

## 关键配置文件一览

| 文件 | 说明 |
|------|------|
| `deploy/.env` | 部署环境变量 |
| `deploy/.env.example` | 环境变量模板 |
| `deploy/config/server.yaml.tpl` | 服务端配置模板 |
| `deploy/config/nginx.conf` | Nginx 配置 |
| `deploy/config/mysql.cnf` | MySQL 配置 |
| `deploy/docker-compose.yml` | Docker Compose 编排 |
| `configs/server.yaml.example` | 本地开发配置示例 |
| `configs/agent.yaml` | Agent 配置（本地开发用） |
| `configs/rules/` | 内置告警规则 |
| `configs/policies/` | 基线策略规则文件 |

---

## 配置建议

1. **JWT_SECRET**：生产环境必须使用高强度随机字符串，建议 32 字符以上
2. **PLUGINS_BASE_URL**：不能写成 Agent 无法访问的 `localhost`，必须是 Agent 网络可达的地址
3. **manager_addr**：必须保证 AgentCenter 能回连 Manager（同一 Docker 网络内可用服务名）
4. **数据盘**：MySQL、ClickHouse、Kafka 数据建议挂载到独立磁盘，避免磁盘 IO 争抢
5. **Sentinel / Kafka / ClickHouse**：启用对应功能时，必须确保端点可达
