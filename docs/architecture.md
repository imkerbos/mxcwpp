# 系统架构

## 1. 概述

矩阵云安全平台当前采用 **Agent + Plugin + Manager/AgentCenter/Consumer** 的 V2 架构。相比 V1 的单体式直写数据库模式，V2 将控制面拆分为接入层、管理层、消息层、消费层和分层存储，目标是让控制面具备多实例、高吞吐和更清晰的职责边界。

当前实际能力边界：

- 应用层已支持 `Manager / AgentCenter / Consumer` 多实例部署
- Agent 侧已支持 Manager 服务发现和多 AC 接入
- Kafka / Consumer / ClickHouse 异步链路已纳入当前项目主架构
- 默认部署下存储层仍以单节点为主，MySQL / Redis / ClickHouse 的主从与容灾需在生产环境单独加固

**技术栈**: Go 1.21+ (Gin/gRPC/Gorm/Zap), Vue 3 + TS, Protobuf, Kafka, Redis, MySQL, ClickHouse, Prometheus

## 2. 总体拓扑

```
┌──────────────────────────────────────────────────────────────────────────┐
│                               控制面                                     │
│                                                                          │
│  浏览器 ── HTTPS ──► Nginx ──► Manager × N                               │
│                                  │                                       │
│                                  ├─ MySQL / Redis / ClickHouse           │
│                                  ├─ Prometheus 查询                      │
│                                  └─ AC 服务发现 / 任务路由               │
│                                                                          │
│  Agent ── gRPC(mTLS) ──► AgentCenter × N ──► Kafka ──► Consumer × N      │
│                          │                        │                       │
│                          ├─ HTTP 管理接口         ├─ MySQL               │
│                          ├─ 连接池 / 命令下发     ├─ ClickHouse          │
│                          └─ Kafka 失败降级队列    └─ Redis(agent:ac)     │
└──────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                              端点层                                      │
│                                                                          │
│  mxsec-agent + plugins                                                   │
│  ├─ baseline   基线检查 / 修复                                            │
│  ├─ collector  资产采集                                                   │
│  ├─ fim        文件完整性监控                                             │
│  ├─ scanner    病毒查杀 / 隔离                                            │
│  └─ sensor     Tetragon eBPF 运行时事件采集                               │
└──────────────────────────────────────────────────────────────────────────┘
```

## 3. 组件职责

### Agent

部署在目标主机上的轻量守护进程，负责：

- 管理插件生命周期
- 通过 gRPC 双向流与 AgentCenter 通信
- 使用 mTLS 与控制面建立安全连接
- 通过服务发现或静态地址列表选择可用 AC
- 透传插件数据，不承担复杂业务存储逻辑

入口：`cmd/agent/main.go`

### AgentCenter

Agent 接入层，当前定位为 **无状态连接管理 + 数据转发层**。

- 维护 Agent 连接池和在线状态
- 通过 gRPC 下发命令和插件任务
- 通过 HTTP 暴露 `/health`、`/conn/stat`、`/conn/list`、`/command`、`/command/batch`
- 将 Agent 上报数据按 DataType 路由到 Kafka
- Kafka 不可用时使用内存降级队列暂存
- 启动后向 Manager 注册，运行中周期性心跳

入口：`cmd/server/agentcenter/main.go`

### Manager

管理面服务，当前定位为 **无状态 API + 调度 + 服务发现**。

- 提供前端 REST API 和 JWT 认证
- 管理策略、规则、任务、告警、报告、组件、用户等业务对象
- 内嵌 AC Registry / SD 模块，负责 AC 注册、探测、发现
- 调用 Redis `agent:ac` 映射对任务做精准路由
- 查询 MySQL / ClickHouse / Redis / Prometheus 聚合前端所需数据

入口：`cmd/server/manager/main.go`

### Consumer

异步消费服务，负责将 Kafka 数据可靠写入存储。

- 消费 Kafka 多 Topic 数据
- 按 DataType 路由到 MySQL / ClickHouse
- 执行幂等写入
- 写入失败时投递到 DLQ
- 在消费心跳时维护 `agent:ac:{agentID}` 映射

入口：`cmd/server/consumer/main.go`

## 4. 高可用能力

### 已具备的控制面 HA 能力

- `Manager` 支持多实例，无状态，前置 Nginx 负载均衡
- `AgentCenter` 支持多实例，向 Manager SD 注册并接受主动健康探测
- `Consumer` 支持多实例，通过 Kafka Consumer Group 自动分摊分区
- Agent 支持通过服务发现接口获取健康 AC 列表，并具备静态地址回退
- 任务调度已使用 Redis 分布式锁避免多副本重复分发

### 当前高可用边界

- `MySQL` 默认仍为单实例业务主存储
- `ClickHouse` 默认仍为单实例事件/时序分析存储
- `Redis` 在项目中承担共享缓存与同步状态，Sentinel/主从切换仍属于后续生产加固项
- 因此当前更准确的说法是：**控制面高可用已落地，存储层完整容灾需按生产环境独立建设**

## 5. 数据链路

### 上报链路

```
Plugin → mxsec-agent → gRPC(mTLS) → AgentCenter
      → Kafka(topic by DataType)
      → Consumer
      → MySQL / ClickHouse / Redis
```

### 下发链路

```
用户 / API → Manager
          → 查询 SD + Redis(agent:ac)
          → 调用目标 AC 的 HTTP 管理接口
          → gRPC 下发给 Agent
          → Agent 路由到对应插件执行
```

## 6. Topic 与存储分层

### Kafka Topic

按数据特征拆分，而不是所有数据共用一个 Topic：

- `mxsec.agent.heartbeat`
- `mxsec.agent.events`
- `mxsec.agent.baseline`
- `mxsec.agent.asset`
- `mxsec.agent.command-ack`
- `mxsec.agent.*.dlq`

### 存储分工

| 组件 | 用途 |
|------|------|
| MySQL | 任务、策略、主机、告警状态、资产快照等业务主数据 |
| Redis | SD 同步、`agent:ac` 映射、分布式锁、缓存 |
| ClickHouse | 指标趋势、FIM 事件、告警时间线、历史归档 |
| Prometheus | 主机性能指标查询源 |

## 7. 安全与通信

| 路径 | 协议 | 认证 |
|------|------|------|
| Browser ↔ Nginx / Manager | HTTPS / HTTP REST | JWT |
| Agent ↔ AgentCenter | gRPC 双向流 | mTLS |
| Agent ↔ Plugin | OS Pipe + Protobuf | 父子进程 |
| Manager ↔ AgentCenter | HTTP 内部接口 | 内网管理调用 |

证书生成与分发流程仍保持不变，见 `scripts/generate-certs.sh`。

## 8. 部署形态

| 环境 | 拓扑特征 |
|------|---------|
| `dev` | 单机联调，控制面可简化，适合功能开发 |
| `pret / perf` | 多副本控制面 + 3 节点 Kafka，适合压测与预发验证 |
| `prod` | 推荐启用 Manager/AC/Consumer 多副本，Kafka/ClickHouse/Redis/MySQL 需结合实际做容灾 |

## 9. 相关代码路径

```text
mxsec-platform/
├── cmd/server/manager/          # Manager 入口
├── cmd/server/agentcenter/      # AgentCenter 入口
├── cmd/server/consumer/         # Consumer 入口
├── internal/server/manager/sd/  # AC 服务发现与注册中心
├── internal/server/common/kafka/# Kafka Topic / Producer 封装
├── internal/server/consumer/    # Consumer 路由 / Writer / DLQ
├── internal/server/database/    # MySQL / Redis / ClickHouse 客户端
├── internal/agent/              # Agent 连接、传输、插件管理
├── plugins/scanner/             # 病毒查杀插件
├── plugins/sensor/              # 运行时检测插件
└── plugins/fim/                 # 文件完整性监控插件
```
