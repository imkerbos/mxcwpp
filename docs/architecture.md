# 系统架构

## 1. 概述

矩阵云安全平台是 Linux 基线合规性检查平台，采用 Agent + Plugin + Server 架构（参考 Elkeid）。

**技术栈**: Go 1.21+ (Gin/gRPC/Gorm/Zap), Vue 3 + TS (Pinia/Ant Design Vue), Docker, Protobuf

## 2. 架构图

```
┌──────────────────────────────────────────────────────────────┐
│                         Server 端                             │
│                                                              │
│  ┌──────────┐     ┌──────────┐          ┌──────────────┐    │
│  │ UI/Nginx │────>│ Manager  │          │ AgentCenter  │    │
│  │ Vue3+TS  │HTTP │ Gin:8080 │          │  gRPC:6751   │    │
│  └──────────┘     └────┬─────┘          └──────┬───────┘    │
│                        │                       │             │
│              ┌─────────┴───────────────────────┘             │
│              │        共享数据库                                │
│       ┌──────▼──────┐ ┌───────┐ ┌────────────┐ ┌────────┐  │
│       │ MySQL 8.0+  │ │ Redis │ │ ClickHouse │ │ Kafka  │  │
│       └─────────────┘ └───────┘ └────────────┘ └────────┘  │
└──────────────────────────────────────────────────────────────┘
                         │ gRPC + mTLS
┌──────────────────────────────────────────────────────────────┐
│                    Agent 端 (每台主机)                         │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │           mxsec-agent (systemd daemon)                │   │
│  │  Heartbeat | Transport(gRPC) | PluginManager | mTLS  │   │
│  └──────────────────────┬───────────────────────────────┘   │
│                         │ Pipe + Protobuf                    │
│  ┌──────────┐  ┌───────────┐  ┌─────┐                      │
│  │ baseline │  │ collector │  │ fim │  ← Plugin 子进程       │
│  └──────────┘  └───────────┘  └─────┘                      │
└──────────────────────────────────────────────────────────────┘
```

**通信方式**:

| 路径 | 协议 | 认证 |
|------|------|------|
| UI ↔ Manager | HTTP REST | JWT |
| Agent ↔ AgentCenter | gRPC 双向流 | mTLS |
| Agent ↔ Plugin | OS Pipe + Protobuf | 无（父子进程） |

## 3. 组件说明

### Agent

轻量守护进程，root 权限运行。管理插件生命周期，通过 gRPC 与 AgentCenter 通信，定时上报心跳（60s），透传插件数据。

- 入口: `cmd/agent/main.go`
- 核心: `internal/agent/`

### AgentCenter

gRPC 服务端。接收心跳/检测结果/资产数据，下发任务和插件配置。

- 入口: `cmd/server/agentcenter/main.go`
- 核心: `internal/server/agentcenter/`

### Manager

HTTP API 服务端。策略/任务/结果管理，Dashboard 数据，JWT 认证。

- 入口: `cmd/server/manager/main.go`
- 核心: `internal/server/manager/`

### Plugins

独立子进程，通过 Pipe + Protobuf 与 Agent 通信：

| 插件 | 职责 | DataType |
|------|------|----------|
| baseline | 基线合规检查 | 8000 |
| collector | 资产采集（进程/端口/账户等） | 5050-5064 |
| fim | 文件完整性监控（基于 AIDE） | 6001-6002 |

## 4. 数据模型

```
Policy 1───N Rule
  │                 │
  ▼                 ▼
ScanTask N───N Host ───N ScanResult
                │
                ├── FIMEvent
                └── HostMetric
```

**核心表**:

| 表 | 说明 | 写入方 |
|---|---|---|
| hosts | 主机信息 (host_id, hostname, os_family, status) | AgentCenter |
| policies | 策略集 (id, name, os_family, enabled) | Manager |
| rules | 基线规则 (rule_id, policy_id, category, severity, check_config) | Manager |
| scan_tasks | 扫描任务 (task_id, policy_id, status) | Manager |
| scan_results | 检测结果 (host_id, rule_id, status:pass/fail/error) | AgentCenter |
| fim_events | FIM 变更事件 (host_id, file_path, change_type, severity) | AgentCenter |
| host_metrics | 主机监控指标 (cpu/mem/disk/network) | AgentCenter |

## 5. 数据流

### 基线扫描

```
UI 创建任务 → Manager 写入 scan_tasks(pending)
→ AgentCenter 调度: 读取 pending 任务, gRPC 下发到 Agent
→ Agent 路由到 baseline Plugin
→ Plugin 执行检查, 逐条上报 Record(DataType=8000) → Pipe → Agent
→ Agent gRPC 透传 → AgentCenter 写入 scan_results
→ UI 查询 GET /api/v1/results
```

### 心跳

```
Agent 每60s → gRPC PackagedData(DataType=1000)
→ AgentCenter: 更新 hosts 表 + 写入 host_metrics
```

## 6. 安全

### mTLS 证书

```
deploy/certs/
├── ca.crt / ca.key       # CA
├── server.crt / server.key   # AgentCenter 使用
└── client.crt / client.key   # 下发给 Agent
```

Agent 首次连接时 AgentCenter 通过 gRPC 下发证书包，Agent 保存到 `/var/lib/mxsec-agent/certs/`。

生成: `scripts/generate-certs.sh`

### JWT

Manager HTTP API 使用 JWT Token，Secret 通过 `server.yaml` → `jwt_secret` 配置。

### gRPC DataType 编码

| DataType | 方向 | 说明 |
|----------|------|------|
| 1000 | Agent→Server | 心跳 |
| 1001 | Agent→Server | 插件状态 |
| 5050-5064 | Plugin→Server | 资产数据 |
| 6000 | Server→Plugin | FIM 任务 |
| 6001 | Plugin→Server | FIM 事件 |
| 8000 | Plugin→Server | 基线结果 |

## 7. 存储设计

### 当前方案

| 组件 | 用途 |
|------|------|
| MySQL 8.0+ | 主存储: 所有业务数据 |
| Redis | 缓存、会话 |
| ClickHouse (可选) | 大规模扫描结果/监控指标 |
| Kafka (可选) | Agent 数据异步处理 |

### 配置 (server.yaml.tpl)

```yaml
database:
  mysql:
    host: "__MYSQL_HOST__"
    port: __MYSQL_PORT__
    database: "__MYSQL_DATABASE__"
    max_open_conns: __DB_MAX_OPEN_CONNS__

redis:
  addr: "__REDIS_ADDR__"

clickhouse:
  enabled: __CLICKHOUSE_ENABLED__
  addrs: ["__CLICKHOUSE_ADDR__"]

kafka:
  enabled: __KAFKA_ENABLED__
  brokers: ["__KAFKA_BROKERS__"]
```

## 8. Agent 配置

构建时嵌入（推荐）:

```bash
go build -ldflags "-X main.serverHost=10.0.0.1:6751 -X main.buildVersion=1.0.0"
```

配置文件 (`/etc/mxsec-agent/agent.yaml`):

```yaml
agent:
  id_file: "/var/lib/mxsec-agent/agent_id"    # UUID, 首次自动生成
  work_dir: "/var/lib/mxsec-agent"
server:
  agent_center:
    private_host: "agentcenter:6751"
tls:
  ca_file: "/var/lib/mxsec-agent/certs/ca.crt"
  cert_file: "/var/lib/mxsec-agent/certs/client.crt"
  key_file: "/var/lib/mxsec-agent/certs/client.key"
heartbeat:
  interval: 60s
log:
  level: "info"
  file: "/var/log/mxsec-agent/agent.log"
```

## 9. 项目目录

```
mxsec-platform/
├── cmd/agent/                  # Agent 入口
├── cmd/server/agentcenter/     # AgentCenter 入口
├── cmd/server/manager/         # Manager 入口
├── internal/agent/             # Agent 核心
├── internal/server/agentcenter/ # AgentCenter 核心
├── internal/server/manager/    # Manager (api/biz)
├── internal/server/model/      # 数据库模型
├── plugins/baseline/           # 基线检查插件
├── plugins/collector/          # 资产采集插件
├── plugins/fim/                # FIM 插件
├── plugins/lib/go/             # 插件 SDK
├── api/proto/                  # Protobuf 定义
├── ui/                         # Vue 3 前端
├── deploy/                     # 部署配置
├── configs/                    # 配置文件
├── scripts/                    # 构建/部署脚本
├── tools/                      # 工具（baseline-fixer）
└── docs/                       # 文档
```
