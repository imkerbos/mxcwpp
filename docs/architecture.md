# 架构设计

## 概述

MxSec Platform 采用 **Agent + Plugin + Manager / AgentCenter / Consumer** 分层架构。控制面无状态，支持多实例水平扩展；数据面通过 Kafka 异步解耦，按存储特征分层写入 MySQL（业务主数据）和 ClickHouse（时序与事件归档）。

## 系统拓扑

```
                         ┌─────────────────────────┐
                         │      用户 / 浏览器       │
                         └────────────┬────────────┘
                                      │ HTTPS
                                      ▼
                         ┌─────────────────────────┐
                         │         Nginx           │
                         │    反向代理 / 负载均衡    │
                         └────────────┬────────────┘
                       /api/*         │         静态资源
                ┌─────────────────────┘────────────────────┐
                ▼                                          ▼
      ┌────────────────────┐                    ┌────────────────────┐
      │     Manager × N    │                    │      Vue3 SPA      │
      │ REST API / 调度 /   │                    │    前端控制台       │
      │ 服务发现 / SD 模块  │                    └────────────────────┘
      └───────┬─────┬──────┘
              │     │
      Prometheus    │ MySQL / Redis / ClickHouse
              │     │
              ▼     ▼
      ┌────────────────────┐                    ┌────────────────────┐
      │     Prometheus     │                    │   MySQL / Redis /   │
      │   主机指标数据源    │                    │    ClickHouse      │
      └─────────▲──────────┘                    └─────────▲──────────┘
                │ scrape /metrics                          │
                │                                          │
      ┌─────────┴──────────┐                               │
      │   AgentCenter × N  │───── Kafka Produce ──────────▶│
      │ gRPC 接入 / 转发    │                               │
      │ Prometheus Exporter │            Kafka ──▶ Consumer × N
      └─────────┬──────────┘
                │ gRPC BiDi Stream / mTLS
                ▼
      ┌──────────────────────────────────────────────────────┐
      │               mxsec-agent（每台目标主机）             │
      │  插件基座 + 生命周期管理 + 服务发现                    │
      │  baseline / collector / fim / scanner / sensor       │
      └──────────────────────────────────────────────────────┘
```

## 组件职责

### Manager

管理面 HTTP API 服务，无状态，前置 Nginx 负载均衡。

- 提供 100+ 个 REST API 端点，JWT 认证
- 策略、规则、任务、告警、报告、用户等业务 CRUD
- 内嵌 AC Registry / SD 模块，负责 AgentCenter 注册、主动健康探测、服务发现
- 任务调度：Redis 分布式锁，5s 调度间隔
- 查询 MySQL / ClickHouse / Redis / Prometheus 聚合前端数据
- 任务下发：查 Redis `agent:ac` 映射精准路由到目标 AC

入口：`cmd/server/manager/main.go`

### AgentCenter

Agent 接入层，无状态连接管理 + 数据转发，零数据库依赖。

- 维护 Agent gRPC 双向流连接池（令牌限流，单实例最大 2000 连接）
- 将 Agent 上报数据按 DataType 路由到 Kafka Topic
- Kafka 不可用时使用内存降级队列暂存，恢复后自动重放
- HTTP 管理接口：`/health` `/conn/stat` `/conn/list` `/command` `/command/batch`
- 启动时向 Manager SD 注册，15s 心跳，优雅注销
- 主机性能指标通过 `/metrics` 暴露给 Prometheus 抓取

入口：`cmd/server/agentcenter/main.go`

### Consumer

Kafka 异步消费服务，负责数据持久化。

- 订阅 5 个业务 Topic，按 DataType 路由写入 MySQL / ClickHouse
- 幂等写入：MySQL `ON DUPLICATE KEY UPDATE`，ClickHouse `ReplacingMergeTree`
- 批量优化：MySQL 500 条/5s，ClickHouse 5000 条/10s
- 写入失败进 Dead Letter Queue（`*.dlq` Topic）
- 消费心跳时维护 Redis `agent:ac:{agentID}` 映射，检查 pending 任务触发补发
- CEL 规则引擎集成：eBPF 事件实时匹配告警规则，触发自动响应

入口：`cmd/server/consumer/main.go`

### Agent

部署在目标主机上的轻量守护进程，自身不提供安全能力，所有能力通过插件实现。

- 插件生命周期管理（启动、停止、升级、崩溃重启、watchdog 健康检查）
- gRPC 双向流通信（mTLS），环形缓冲区 2048 条 + 100ms 批量发送 + Snappy 压缩
- 服务发现：查 Manager SD 接口获取健康 AC 列表，power-of-two-choices 负载均衡
- 首次接入允许跳过证书校验以获取证书，后续连接切换为正式 mTLS
- 数据透传，不解析插件内容

入口：`cmd/agent/main.go`

### 插件

每个插件是独立子进程，通过 `os.Pipe` + Protobuf 与 Agent 通信。

| 插件 | 功能 | 触发方式 |
|------|------|---------|
| baseline | 基线检查 + 自动修复（9 种检查器） | 任务下发 / 定时 |
| collector | 资产采集（11 种采集器） | 定时（1-12h） |
| fim | 文件完整性监控（仅 VM） | 任务下发 |
| scanner | 病毒查杀（ClamAV + YARA-X）+ 隔离箱 | 任务下发 / 定时 |
| sensor | Tetragon/eBPF 运行时事件采集 | 常驻运行 |

## 数据链路

### 上报链路（Agent → 存储）

```
Plugin ─→ Agent Ring Buffer ─→ gRPC(mTLS) ─→ AgentCenter
    ─→ Kafka(按 DataType 路由到 Topic)
    ─→ Consumer
    ─→ MySQL / ClickHouse / Redis
```

### 下发链路（用户 → Agent）

```
用户/API ─→ Manager ─→ 查 SD + Redis(agent:ac) ─→ 调用目标 AC HTTP 接口
    ─→ AC gRPC 下发 ─→ Agent ─→ 路由到对应插件执行
```

### 查询链路（前端 → 存储）

| 查询场景 | 数据源 |
|---------|--------|
| 主机/策略/任务/告警状态 | MySQL |
| 监控指标曲线/趋势图 | Prometheus |
| FIM 事件列表/统计 | ClickHouse（优先），MySQL（fallback） |
| 基线评分 | Redis 缓存 |
| Dashboard 趋势 | ClickHouse 物化视图 |

## Kafka 设计

按数据写入特征分组，各 Topic 独立 Retention 和 Partition 策略：

| Topic | DataType | Partitions | Retention | 说明 |
|-------|----------|-----------|-----------|------|
| `mxsec.agent.heartbeat` | 1000, 1001 | 6 | 24h | 心跳/插件状态 → MySQL |
| `mxsec.agent.events` | 6001, eBPF | 12 | 72h | FIM/运行时事件 → ClickHouse |
| `mxsec.agent.baseline` | 8000-8004 | 6 | 7d | 基线结果/任务完成 → MySQL |
| `mxsec.agent.asset` | 5050-5060 | 6 | 7d | 资产数据 → MySQL |
| `mxsec.agent.command-ack` | 命令回包 | 6 | 7d | Agent 执行结果 |

Partition Key 为 AgentID，保证同一 Agent 数据有序。Replication Factor = 2，`min.insync.replicas = 1`。

各 Topic 配套 DLQ：`mxsec.agent.{topic-name}.dlq`。

## 存储分层

| 存储 | 定位 | 写入方 |
|------|------|--------|
| MySQL 8.0+ | 业务主数据（主机、策略、任务、告警状态、资产快照、用户） | Consumer / Manager |
| ClickHouse | 时序分析与事件归档（指标趋势、FIM、告警时间线、审计日志） | Consumer |
| Redis | SD 同步、`agent:ac` 映射、分布式锁、基线评分缓存 | Manager / Consumer |
| Prometheus | 主机性能指标查询源（CPU / 内存 / 磁盘 / 网络） | AgentCenter Exporter |

## 高可用设计

### 已具备 HA 能力

| 组件 | 方式 | 说明 |
|------|------|------|
| Manager | ×N 副本 + Nginx least_conn | 无状态，JWT 认证，Redis 共享缓存 |
| AgentCenter | ×N 副本 + L4 LB | 无状态，零数据库依赖，Agent 自动重连 |
| Consumer | ×N 副本 + Kafka ConsumerGroup | Partition 自动 Rebalance |
| Kafka | 3 Broker KRaft 集群 | replication_factor=2 |
| Redis SD | Pub/Sub 多副本同步 | Manager 内存为源头，Redis 为多实例同步缓存 |

### 需按生产环境独立建设

| 组件 | 推荐方案 |
|------|---------|
| MySQL | 主从复制 / MGR / 云 RDS |
| Redis | Sentinel / 云托管 Redis |
| ClickHouse | 副本表 / 云 ClickHouse（当前单实例，数据有 TTL，丢失可从 Kafka 重放） |

### 任务下发可靠性

- 所有任务先持久化到 MySQL（status=pending）
- Manager 查 SD + Redis 路由到目标 AC 下发
- Agent 离线或 AC 不可达时任务保持 pending
- Consumer 消费心跳时检查 pending 任务，触发补发
- 任务调度使用 Redis 分布式锁，避免多副本重复分发

## 安全与通信

| 链路 | 协议 | 认证方式 |
|------|------|---------|
| 浏览器 ↔ Nginx / Manager | HTTPS / REST | JWT |
| Agent ↔ AgentCenter | gRPC 双向流 | mTLS |
| Agent ↔ Plugin | OS Pipe + Protobuf | 父子进程隔离 |
| Manager ↔ AgentCenter | HTTP 内部接口 | 内网调用 |

证书生成：`scripts/generate-certs.sh`

## 与 Elkeid 的关键差异

本项目在设计理念上参考了 Elkeid 的 Agent + Plugin + Server 架构，但在实现上有以下差异：

| 维度 | Elkeid | MxSec |
|------|--------|-------|
| 存储 | MongoDB | MySQL + ClickHouse |
| 服务发现 | 独立 SD 服务 | Manager 内嵌 SD 模块 |
| Kafka Topic | 单 Topic | 按数据特征分组 5 Topic |
| 任务分发 | Redis PubSub | 持久化 + 心跳补发 |
| 负载均衡 | 最小连接数 | power-of-two-choices |
| Agent→AC 映射 | Manager 定时采集 | Consumer 消费心跳时写入 |

## 关键代码路径

```
cmd/server/manager/              # Manager 入口
cmd/server/agentcenter/          # AgentCenter 入口
cmd/server/consumer/             # Consumer 入口
internal/server/manager/sd/      # AC 服务发现与注册
internal/server/common/kafka/    # Kafka Producer / Topic 路由
internal/server/consumer/        # Consumer 路由 / Writer / DLQ / CEL 引擎
internal/server/database/        # MySQL / Redis / ClickHouse 客户端
internal/agent/                  # Agent 连接 / 传输 / 插件管理
plugins/                         # 各插件实现
```
