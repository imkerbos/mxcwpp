# Matrix Cloud Security Platform（矩阵云安全平台）

企业级主机与容器安全管理平台，覆盖 Linux 服务器、Docker 容器、Kubernetes 集群的安全基线、资产管理、运行时检测、漏洞管理、病毒查杀与合规审计，面向甲方安全团队提供统一安全运营视图。当前项目已完成 V2 控制面重构，具备 `Manager / AgentCenter / Consumer` 多实例、`Kafka + ClickHouse` 异步解耦和 `Redis SD` 服务发现能力；默认部署下应用层已支持高可用，存储层容灾仍需按生产方案进一步加固。

## 功能概览

| 功能模块 | 当前状态 | 说明 |
|---------|---------|------|
| **安全概览** | 已落地 | Dashboard 已接入主机、容器、集群、漏洞、病毒、基线等核心统计数据 |
| **资产中心** | 已落地 | 已支持 11 类资产采集、关系查询、资产导出与主机详情画像 |
| **主机防护** | 已落地 | 告警列表、白名单、运行时检测、漏洞风险、病毒查杀与批量处置已形成闭环 |
| **基线安全** | 已落地 | 9 种检查器 × 13 个规则文件，覆盖 CIS Benchmark 核心项；支持单机/批量自动修复 |
| **文件完整性监控** | 已落地 | 基于 AIDE 的 FIM 检查，提供概览、策略、事件、任务全链路 |
| **漏洞管理** | 已落地 | 软件包 PURL 采集 + OSV.dev 匹配 + CVSS v3.1 评分 + SBOM 导出 |
| **病毒查杀** | 已落地 | Scanner 插件（ClamAV + YARA-X）+ 任务管理 + 扫描结果 + 隔离箱 |
| **容器集群安全** | 已落地 | K8s 集群管理、容器基线检查、容器安全告警与事件、容器白名单、K8s audit webhook 接入 |
| **应用防护（RASP）** | 规划中 | 已预留前端模块和数据结构，独立探针与策略闭环仍未纳入当前交付范围 |
| **审计日志** | 已落地 | 操作审计全链路记录，支持查询与导出 |
| **系统监控** | 已落地 | 主机性能指标统一使用 Prometheus 查询，服务健康状态基于 AC Registry |
| **高可用架构** | 已落地 | Manager/AC/Consumer 多实例 + Nginx LB + Kafka + Redis SD + ClickHouse；默认控制面支持 HA，数据库与缓存主从容灾需单独部署 |

## 能力状态

README 仅概述当前能力面，详细完成度以 [TODO / 状态看板](docs/TODO.md) 为准。当前重点能力分布如下：

- 已形成主链路：基线安全、文件完整性监控、资产采集、漏洞管理、病毒查杀、容器集群安全、审计日志、告警白名单、主机性能监控、服务健康监控。
- 已完成 V2 架构能力：Manager / AgentCenter / Consumer 多实例、Kafka 消息总线、Consumer 异步落库、Redis SD、ClickHouse 归档与趋势查询。
- 仍在持续增强：RASP、深度资产画像、K8s 准入控制、存储层主从容灾、规则工程化与自动化响应闭环。

其中补充说明如下：

- **资产指纹 / 资产画像**：当前重点在资产采集、主机详情与关系计算，更深入的全局画像和指纹聚合仍在增强。
- **漏洞管理**：当前已完成 PURL 采集、OSV 匹配、CVSS 评分和列表查询，下一阶段重点是镜像/SBOM/离线漏洞源扩展。
- **病毒查杀**：当前已完成 Scanner、任务、结果、隔离箱基础闭环，下一阶段重点是规则治理、行为检测和误报治理。
- **容器 / K8s**：当前已覆盖集群、事件、告警、CIS 基线、白名单和 Audit Webhook，下一阶段重点是准入控制和风险关联。
- **RASP**：当前仍为预留能力，不属于当前已交付主链路。

## 技术栈

| 层面 | 技术 |
|------|------|
| 后端 | Golang（Gin + gRPC + Gorm + Zap） |
| 前端 | Vue 3 + TypeScript + Pinia + Ant Design Vue 4 |
| 存储 | MySQL 8.0+、Redis、ClickHouse |
| 监控 | Prometheus（主机性能指标唯一数据源） |
| 消息 | Kafka（5 Topic + DLQ） |
| 通信 | gRPC 双向流 + mTLS + Protobuf |
| 部署 | Docker Compose / Systemd + Nginx |

## 环境形态

- **开发环境（dev）**：`docker-compose + air` 单机热更新，Kafka 使用单节点 KRaft 形态，主要用于本地联调与页面开发，不自动构建 Agent / 插件产物。
- **压测 / 预发环境（perf / pret）**：与生产保持同类拓扑，使用 Docker Compose 部署高可用控制面，Kafka 使用 3 节点 KRaft 集群，用于容量验证、升级演练与稳定性测试。
- **生产环境（prod）**：以 Docker Compose 为基础进行多副本部署，Manager / AgentCenter / Consumer 高可用，Kafka 至少 3 节点 KRaft，结合 Nginx、Redis、MySQL、ClickHouse、Prometheus 形成完整运行面。

## 支持平台

**主机操作系统**：

- Rocky Linux 9 / 10、Oracle Linux 7 / 8 / 9、CentOS 7 / 8 / 9
- Debian 10 / 11 / 12、Ubuntu 20.04 / 22.04

**运行时环境**：

- 物理机 / 虚拟机（VM）
- Docker 容器宿主机
- Kubernetes 节点与集群

通过 `os_family + os_version + runtime_type`（VM/Docker/K8s）匹配策略，按运行环境自动适配检查规则。

---

## 系统架构

系统由 **端点层 → 接入层 → 消息总线 → 处理层 → 存储层 → 管理面 → 展示层** 七层构成，数据上报与管控下发两条链路方向相反，存储层冷热分离。

### 架构总览

```
                         ┌─────────────────────────┐
                         │      用户 / 浏览器        │
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
      │ 下载地址控制 / SD   │                    └────────────────────┘
      └───────┬─────┬──────┘
              │     │
      查询 Prometheus │ 读写 MySQL / Redis / ClickHouse
              │     │
              ▼     ▼
      ┌────────────────────┐                    ┌────────────────────┐
      │     Prometheus     │                    │   MySQL / Redis /   │
      │ 主机性能指标唯一源   │                    │ ClickHouse（业务存储）│
      └─────────▲──────────┘                    └─────────▲──────────┘
                │ scrape /metrics                           │
                │                                           │
      ┌─────────┴──────────┐      Kafka Produce / Consume   │
      │   AgentCenter × N  │────────────────────────────────┘
      │ gRPC 接入 / 调度 /  │
      │ Prometheus Exporter │──────► Kafka ──────► Consumer × N
      └─────────┬──────────┘
                │ gRPC BiDi Stream / mTLS
                ▼
      ┌──────────────────────────────────────────────────────┐
      │               mxsec-agent（每台目标主机）             │
      │  插件基座 + 生命周期管理 + Manager 下载地址消费        │
      │  baseline / collector / fim 等插件通过 Pipe 通信      │
      └──────────────────────────────────────────────────────┘
```

### 数据上报链路

```
  ┌─────────────────────────────┐
  │ baseline / collector / fim  │  插件子进程，执行检查/采集
  └────────────┬────────────────┘
               │ Pipe + Protobuf（fd 3/4）
               ▼
  ┌─────────────────────────────┐
  │         mxsec-agent         │  透传数据，不解析插件内容
  └────────────┬────────────────┘
               │ gRPC BiDi Stream（mTLS，PackagedData{Records[]}）
               ▼
  ┌─────────────────────────────┐
  │       AgentCenter × N       │
  ├─────────────────────────────┤
  │ 主机性能指标 ──► /metrics    │  由 Prometheus 抓取
  │ 业务记录     ──► Kafka       │  heartbeat / baseline / events / asset
  └───────┬───────────────┬─────┘
          │               │
          ▼               ▼
  ┌───────────────┐   ┌─────────────────────────────┐
  │  Prometheus   │   │         Kafka + DLQ         │
  │ 主机监控唯一源 │   └────────────┬────────────────┘
  └───────────────┘                │ ConsumerGroup
                                   ▼
                         ┌─────────────────────────────┐
                         │        Consumer × N         │
                         ├─────────────────────────────┤
                         │ heartbeat ─► MySQL hosts    │
                         │ baseline  ─► scan_results   │
                         │ events    ─► MySQL/CH FIM   │
                         │ asset     ─► 各类资产表      │
                         │ 失败      ─► *.dlq + 告警通知│
                         └─────────────────────────────┘
```

### 管控下发链路

```
  ┌────────────────────────────┐
  │       用户（浏览器）        │
  └────────────┬───────────────┘
               │ HTTP REST / JWT
               ▼
  ┌────────────────────────────┐
  │       Manager × 2          │  查 Redis SD注册表，选健康的 AC 实例
  └────────────┬───────────────┘
               │ HTTP 内部接口（/api/v1/internal/ac/...）
               ▼
  ┌────────────────────────────┐
  │      AgentCenter（目标）    │
  └────────────┬───────────────┘
               │ gRPC BiDi Stream（Command 下行帧）
               ▼
  ┌────────────────────────────┐
  │         mxsec-agent        │
  └────────────┬───────────────┘
               │ Pipe（下发任务指令）
               ▼
  ┌────────────────────────────┐
  │  插件（执行扫描/修复/升级） │
  └────────────────────────────┘
```

### 数据查询链路

```
  ┌────────────────────────────┐
  │          前端               │
  └────────────┬───────────────┘
               │ HTTP REST
               ▼
  ┌────────────────────────────────────────────────────────┐
  │                    Manager × 2                          │
  ├────────────────────────────────────────────────────────┤
  │ 主机/策略/任务/告警/审计  ──────────────► MySQL         │
  │ 监控指标曲线/趋势图       ──────────────► Prometheus    │
  │ FIM 事件列表/统计         ──► ClickHouse（优先）        │
  │                           └── MySQL（fallback）        │
  │ 基线评分                  ──────────────► Redis（缓存） │
  └────────────────────────────────────────────────────────┘
```

---

## 服务组件

### mxsec-agent

部署在目标主机上的**插件基座**，自身不提供安全能力，所有能力通过插件实现。

- 插件生命周期管理（启动、停止、升级、崩溃重启）
- gRPC 双向流通信（mTLS）
- 服务发现（Manager SD 接口 + power-of-two-choices 负载均衡）
- 数据透传，不解析插件内容

### 插件

| 插件 | 功能 | 触发方式 |
|------|------|---------|
| `baseline` | 基线检查 + 自动修复（9 种检查器） | 任务下发 / 定时 |
| `collector` | 资产采集（10 种采集器） | 定时（1–12h） |
| `fim` | 文件完整性监控（仅 VM） | 任务下发 |
| `scanner` | 病毒查杀、样本识别、隔离箱处置 | 任务下发 / 定时 |
| `sensor` | 基于 Tetragon/eBPF 的运行时事件采集 | 常驻运行 |

当前的分发模型是：

- `dev` 与生产环境都不在启动阶段自动构建 Agent 或插件产物。
- Agent / 插件由用户自行编译并上传，Manager 负责统一维护下载地址。
- Agent、AgentCenter 侧只消费 Manager 下发的 `/api/v1/agent/download/*` 与 `/api/v1/plugins/download/*` 地址。

**Baseline 检查器**：`file_kv`、`file_exists`、`file_permission`、`file_line_match`、`file_owner`、`command_exec`、`sysctl`、`service_status`、`package_installed`

**Collector 采集器**：进程、端口、用户、软件包、容器、应用、网卡、磁盘、内核模块、系统服务/定时任务

### mxsec-agentcenter

gRPC 接入服务，处理 Agent 连接和数据上行。

- Agent 注册与心跳（gRPC 双向流，mTLS）
- 插件管理（版本控制、配置下发、升级）
- 任务下发与状态追踪
- 数据上行：主机性能指标经 `/metrics` 暴露给 Prometheus 抓取，其余业务数据按 DataType 写入 Kafka
- SD 客户端：启动时向 Manager 注册，15s 心跳，优雅注销
- HTTP 管理接口：`/health` `/conn/stat` `/conn/list` `/command` `/command/batch`
- 后台调度器（离线检测、插件推送、任务超时）

### mxsec-consumer

Kafka 消费服务，负责数据持久化。

- 订阅 5 个业务 Topic，按 DataType 路由到 MySQL / ClickHouse
- 支持的 DataType：心跳(1000/1001)、资产(5050~5060)、FIM(6001/6002)、基线(8000/8001/8003/8004)
- 幂等写入：MySQL `ON DUPLICATE KEY UPDATE`，ClickHouse `ReplacingMergeTree`
- Dead Letter Queue：写入失败进 `*.dlq` Topic
- Redis `agent:ac:` 映射写入（供 Manager 精准任务路由）

### mxsec-manager

HTTP API 服务，提供管理面接口。

- 100+ 个 HTTP 端点，22+ 个 API 处理器
- SD 模块：AC 注册/心跳/注销 + Redis Pub/Sub 多副本同步 + 主动健康探测
- 任务调度器：Redis 分布式锁 + 5s 调度间隔
- 系统监控 API（主机指标查 Prometheus，服务状态查 AC Registry）
- 30+ 个数据表

### mxsec-console

Vue 3 SPA 前端控制台（45+ 个组件）。

- Dashboard（统计概览、主机状态分布、风险趋势）
- 主机管理（列表、详情、指标、资产采集、风险聚合）
- 策略管理（策略/规则 CRUD、策略组、导入导出）
- 任务管理（创建、执行、详情）
- 基线修复（可修复项、修复任务、修复结果）
- 告警管理（列表、详情、白名单、批量处理）
- 报表统计、审计日志、系统设置、用户管理
- 漏洞管理、病毒查杀、系统监控等模块已接入真实后端能力
- RASP 模块仍以页面预留和架构预研为主

### 策略仓库

13 个规则文件：`ssh-baseline`、`password-policy`、`file-permissions`、`account-security`、`service-status`、`sysctl-security`、`audit-logging`、`network-protocols`、`login-banner`、`secure-boot`、`cron-security`、`file-integrity`、`mac-security`

规则特性：多 OS 版本适配、运行时类型过滤、检查逻辑、修复命令、严重级别（Critical/High/Medium/Low）

---

## 快速开始

### Docker 开发环境

开发环境采用 `docker-compose + air` 热更新模式，仅启动控制面与依赖服务，不自动产出 Agent / 插件安装包。

```bash
# 启动开发环境（带热更新）
make dev-docker-up

# 查看日志
make dev-docker-logs

# 停止服务
make dev-docker-down
```

**访问地址**：
- Manager API: http://localhost:8080
- UI: http://localhost:3000

### 生产部署（HA，推荐）

```bash
cd deploy/

# 1. 配置环境变量
cp .env.example .env
vim .env
# 关键项: MANAGER_REPLICAS=2 / AGENTCENTER_REPLICAS=2 / CONSUMER_REPLICAS=2
# 以及: SERVER_IP / JWT_SECRET / 各数据库密码

# 2. 显式以多副本启动控制面（推荐手动方式，和文档其他位置口径一致）
docker compose --env-file .env up -d \
  --scale manager=2 \
  --scale agentcenter=2 \
  --scale consumer=2

# 日常运维
./deploy.sh upgrade   # 升级（按 .env 的 *_REPLICAS 保持副本数）
./deploy.sh status    # 服务状态
./deploy.sh backup    # 备份数据库
```

> 默认 compose 不直接暴露 AgentCenter gRPC (`6751`)，生产环境需额外做端口映射或接入四层负载均衡（HAProxy / SLB / Nginx stream）。

### 单机 / 功能验证

> ⚠️ 以下方式为单副本启动，**不是生产部署方式**，仅用于开发机本地验证或单机评估：

```bash
cd deploy/
cp .env.example .env  # 保持 *_REPLICAS=1
./deploy.sh start     # 单副本启动，会输出非 HA 的告警
```

详见 [生产环境部署方案](docs/deployment/production-deployment.md) 和 [服务端部署](docs/deployment/server.md)。

---

## 生产集群部署

### 部署架构选型

根据 Agent 规模和可用性要求，提供三种部署形态：

| 形态 | 节点数 | Agent 规模 | 适用场景 |
|------|--------|-----------|---------|
| **单机 All-in-One** | 1 台 | ≤ 50 | 评估试用、小型内网 |
| **标准生产** | 3 台 | 50 – 500 | 中小型企业生产环境 |
| **高规格生产** | 5+ 台 | 500+ | 大规模多集群、高可用要求高 |

### 单机 All-in-One（1 台）

所有服务运行在同一台机器，使用 Docker Compose 一键启动。

**硬件配置**：

| 资源 | 最低配置 | 推荐配置 |
|------|---------|---------|
| CPU | 8 核 | 16 核 |
| 内存 | 32 GB | 64 GB |
| 系统盘 | 100 GB SSD | 200 GB SSD |
| 数据盘 | 200 GB | 500 GB SSD（挂载到 `DATA_DIR`） |

**说明**：适合 ≤ 50 台 Agent 的评估或小型内网环境。控制面建议至少 2 副本（HA），存储层为单实例。

```bash
cd deploy/
cp .env.example .env
# 修改: SERVER_IP / JWT_SECRET / 数据库密码 / *_REPLICAS=2
vim .env

docker compose --env-file .env up -d \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

### 标准生产（3 台）

控制面、存储层、消息队列分离部署，兼顾性能和可维护性。

**集群拓扑**：

```
                    ┌──────────────────────────────────────┐
                    │            四层 LB / SLB             │
                    │    Agent gRPC → :6751 → Node 1 AC    │
                    └───────────────────┬──────────────────┘
                                        │
              ┌─────────────────────────┼─────────────────────────┐
              │                         │                         │
    ┌─────────┴─────────┐    ┌─────────┴─────────┐    ┌─────────┴─────────┐
    │    Node 1          │    │    Node 2          │    │    Node 3          │
    │    控制面           │    │    存储层           │    │    消息队列         │
    │                    │    │                    │    │                    │
    │  Nginx (80/443)    │    │  MySQL 8.0         │    │  Kafka Broker ×3   │
    │  Manager ×2        │    │  Redis 7           │    │  (KRaft 模式)      │
    │  AgentCenter ×2    │    │  ClickHouse 24     │    │                    │
    │  Consumer ×2       │    │  Prometheus        │    │                    │
    │  UI (Nginx SPA)    │    │                    │    │                    │
    └────────────────────┘    └────────────────────┘    └────────────────────┘
```

**硬件配置**：

| 节点 | 角色 | CPU | 内存 | 系统盘 | 数据盘 | 说明 |
|------|------|-----|------|--------|--------|------|
| **Node 1** | 控制面 | 8 核 | 32 GB | 100 GB SSD | 100 GB（日志+插件） | Manager ×2 + AC ×2 + Consumer ×2 + Nginx + UI |
| **Node 2** | 存储层 | 8 核 | 32 GB | 100 GB SSD | 500 GB SSD（MySQL + ClickHouse） | MySQL + Redis + ClickHouse + Prometheus |
| **Node 3** | 消息队列 | 4 核 | 16 GB | 50 GB SSD | 200 GB SSD（Kafka 数据） | Kafka ×3（3 Broker 单机部署，KRaft 集群） |

**部署步骤**：

**Node 3（消息队列）** — 先启动，其他节点依赖它：

```bash
# 在 Node 3 上单独运行 Kafka 3 broker KRaft 集群
# 可以使用 docker-compose 只启动 kafka-1/2/3 服务
# 或直接部署 Confluent / Apache Kafka 原生发行版
# 确保 Node 1 / Node 2 能访问 kafka-1:9092 / kafka-2:9094 / kafka-3:9095
```

**Node 2（存储层）**：

```bash
# 在 Node 2 上部署 MySQL / Redis / ClickHouse / Prometheus
# 可以用 Docker Compose 只启动存储服务，也可以用裸机部署
# 确保 Node 1 能访问:
#   - MySQL:      Node2_IP:3306
#   - Redis:      Node2_IP:6379
#   - ClickHouse: Node2_IP:9000
#   - Prometheus: Node2_IP:9090
```

**Node 1（控制面）**：

```bash
cd deploy/
cp .env.example .env
vim .env
# 关键修改:
#   SERVER_IP=<Node1 外部 IP>
#   MYSQL_HOST=<Node2_IP>
#   MYSQL_PORT=3306
#   REDIS_ADDR=<Node2_IP>:6379
#   KAFKA_BROKER_1=<Node3_IP>:9092
#   KAFKA_BROKER_2=<Node3_IP>:9094
#   KAFKA_BROKER_3=<Node3_IP>:9095
#   CLICKHOUSE_ADDR=<Node2_IP>:9000
#   PROMETHEUS_QUERY_URL=http://<Node2_IP>:9090
#   MANAGER_REPLICAS=2
#   AGENTCENTER_REPLICAS=2
#   CONSUMER_REPLICAS=2
#   JWT_SECRET=<强随机字符串>

# 只启动控制面服务（不启动基础设施）
docker compose --env-file .env up -d \
  manager agentcenter consumer ui \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

> **Agent 接入**：需要在 Node 1 前方配置四层负载均衡（HAProxy / SLB / Nginx stream）将 Agent 的 gRPC 流量 `:6751` 转发到 AC 容器。参考配置见 `deploy/config/haproxy-agentcenter.cfg`。

### 高规格生产（5+ 台）

控制面水平扩展，存储层主从分离，消息队列独立集群。

**集群拓扑**：

```
                           ┌─────────────────────┐
                           │    七层 LB (Nginx)   │   HTTP/HTTPS :80/443
                           │    四层 LB (SLB)     │   gRPC :6751
                           └──────────┬──────────┘
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
    ┌─────────┴─────────┐  ┌─────────┴─────────┐  ┌─────────┴─────────┐
    │    Node 1          │  │    Node 2          │  │    Node 3          │
    │  Manager ×2        │  │  AgentCenter ×2    │  │  Consumer ×2       │
    │  Nginx / UI        │  │  HAProxy (:6751)   │  │                    │
    └────────────────────┘  └────────────────────┘  └────────────────────┘

    ┌────────────────────┐  ┌────────────────────┐
    │    Node 4          │  │    Node 5          │
    │    存储层           │  │    消息队列         │
    │  MySQL (主)         │  │  Kafka Broker ×3   │
    │  Redis (主)         │  │  (KRaft 集群)      │
    │  ClickHouse         │  │                    │
    │  Prometheus         │  │                    │
    └────────────────────┘  └────────────────────┘

    可选: Node 6 (MySQL 从库 + Redis Sentinel)
```

**硬件配置**：

| 节点 | 角色 | CPU | 内存 | 系统盘 | 数据盘 | 说明 |
|------|------|-----|------|--------|--------|------|
| **Node 1** | Manager + Nginx | 4 核 | 16 GB | 50 GB SSD | 50 GB | HTTP API + 前端 + 文件上传 |
| **Node 2** | AgentCenter | 8 核 | 16 GB | 50 GB SSD | 100 GB | gRPC 接入 + 插件分发 + Prometheus Exporter |
| **Node 3** | Consumer | 4 核 | 8 GB | 50 GB SSD | 50 GB | Kafka 消费 + MySQL/ClickHouse 写入 |
| **Node 4** | 存储层 | 8 核 | 64 GB | 100 GB SSD | 1 TB SSD | MySQL + Redis + ClickHouse + Prometheus |
| **Node 5** | 消息队列 | 8 核 | 24 GB | 50 GB SSD | 500 GB SSD | 3 Kafka Broker（KRaft），消息保留 72h |
| **Node 6** | 存储从库（可选） | 8 核 | 32 GB | 100 GB SSD | 1 TB SSD | MySQL Replica + Redis Sentinel + ClickHouse 备份 |

> **说明**：Node 1-3 可以合并部署，也可以继续水平扩展（Manager ×4 / AC ×4 / Consumer ×4），Docker Compose 的 `--scale` 或直接 systemd 部署均可。

### 网络与端口规划

| 端口 | 协议 | 方向 | 说明 |
|------|------|------|------|
| **80 / 443** | HTTP/S | 用户 → Nginx | Web 控制台 + Manager API 代理入口 |
| **6751** | gRPC | Agent → AC | AgentCenter gRPC 接入（mTLS），生产环境必须接入四层 LB |
| **3306** | TCP | 控制面 → MySQL | 业务数据库，仅内网可达 |
| **6379** | TCP | 控制面 → Redis | 缓存 + SD 注册 + 分布式锁，仅内网可达 |
| **9000** | TCP | 控制面 → ClickHouse | 时序数据写入/查询（Native 协议），仅内网可达 |
| **8123** | HTTP | 运维 → ClickHouse | ClickHouse HTTP 管理接口，仅运维网段可达 |
| **9092-9095** | TCP | 控制面 → Kafka | 消息队列，仅内网可达 |
| **9090** | HTTP | Manager → Prometheus | 主机性能指标查询，仅内网可达 |
| **8080** | HTTP | Nginx → Manager / AC | 应用 HTTP 管理端口，容器内部通信，不对外暴露 |

**防火墙规则**：

- 仅 `80/443` 和 `6751` 对外开放（分别给用户和 Agent）
- 存储层端口（3306 / 6379 / 9000 / 9092-9095）仅允许控制面节点访问
- 节点间内网互通

### 存储容量估算

| 数据类型 | 存储位置 | 单条大小 | 估算公式 | 100 台 Agent 30 天 |
|---------|---------|---------|---------|-------------------|
| 心跳 | MySQL hosts | ~1 KB | Agent数 × 1440次/天 × 30天 | ~4 GB |
| 资产指纹 | MySQL | ~2 KB | Agent数 × 11类 × 每类50条 | ~1 GB |
| eBPF 事件 | ClickHouse | ~0.5 KB | Agent数 × 100事件/秒 × 86400秒 × 30天 | ~130 GB（TTL 30 天自动清理） |
| 基线结果 | ClickHouse | ~0.3 KB | Agent数 × 200规则 × 30天 | ~180 MB |
| FIM 事件 | ClickHouse | ~0.5 KB | Agent数 × 100事件/天 × 30天 | ~150 MB |
| 告警 | MySQL + ClickHouse | ~1 KB | 取决于规则命中率 | ~500 MB |
| Kafka 消息 | Kafka 磁盘 | - | 保留 72h，之后自动清理 | ~50 GB 峰值 |

> **关键瓶颈**：eBPF 事件量最大，ClickHouse 的 `ebpf_events` 表设有 30 天 TTL 自动清理。实际事件量取决于 Tetragon 策略配置，建议根据试运行数据调整磁盘规划。

### 高可用说明

**已支持 HA 的组件**：

| 组件 | HA 方式 | 说明 |
|------|---------|------|
| Manager | ×2 副本 + Nginx least_conn | 无状态，直接水平扩展 |
| AgentCenter | ×2 副本 + HAProxy/SLB | 无状态，零数据库依赖，Agent 支持自动重连 |
| Consumer | ×2 副本 + Kafka ConsumerGroup | Kafka 分区自动 Rebalance |
| Kafka | 3 Broker KRaft 集群 | replication_factor=2，允许 1 个 Broker 宕机 |
| Redis SD | Pub/Sub 多副本同步 | Manager 内嵌 SD 模块，AC 注册表存 Redis |

**需要额外建设的 HA**：

| 组件 | 方案 | 说明 |
|------|------|------|
| MySQL | 主从复制 / MGR / 云 RDS | 需自行部署，`.env` 中 `MYSQL_HOST` 指向主库或 VIP |
| Redis | Sentinel / 云 Redis | `.env` 已预留 `REDIS_SENTINEL` 配置项 |
| ClickHouse | 副本表 / 云 ClickHouse | 当前为单实例，数据有 TTL，丢失可从 Kafka 重放 |

### 操作系统要求

**服务端节点**（运行控制面 + 基础设施）：

- **OS**：CentOS 7+ / Rocky Linux 8+ / Ubuntu 20.04+
- **Docker**：Docker Engine 24+ / Docker Compose v2
- **内核**：Linux 4.18+（eBPF 要求；仅 Agent 需要，服务端无要求）
- **时钟**：所有节点 NTP 同步（Kafka / 证书 / 日志时序依赖）
- **文件系统**：数据盘建议 XFS 或 ext4，MySQL / ClickHouse / Kafka 数据分盘挂载

### 常用命令

```bash
make proto           # 生成 Protobuf 代码
make build-agent     # 构建 Agent
make build-server    # 构建 Server
make test            # 运行测试
make fmt             # 格式化代码
make lint            # 代码检查
```

---

## 项目结构

```text
mxsec-platform/
├── cmd/
│   ├── server/
│   │   ├── manager/         # Manager HTTP API Server
│   │   ├── agentcenter/     # AgentCenter gRPC Server
│   │   └── consumer/        # Consumer Kafka 消费服务
│   ├── agent/               # Agent 主程序
│   └── tools/               # 辅助工具（压测、数据迁移等）
├── internal/
│   ├── server/
│   │   ├── manager/         # Manager（api/ biz/ sd/ router/ middleware/）
│   │   ├── agentcenter/     # AgentCenter（transfer/ service/ httptrans/ sdclient/ scheduler/）
│   │   ├── consumer/        # Consumer（router + writer/）
│   │   ├── common/kafka/    # Kafka 封装（Producer、Topic 路由、MQMessage）
│   │   ├── config/          # 配置管理
│   │   ├── database/        # 数据库连接（MySQL + Redis + ClickHouse）
│   │   ├── model/           # 数据模型（30+ 表）
│   │   └── migration/       # 数据库迁移
│   └── agent/               # Agent（connection/ transport/ plugin/ heartbeat/）
├── plugins/
│   ├── baseline/            # 基线检查插件
│   ├── collector/           # 资产采集插件
│   ├── fim/                 # 文件完整性监控插件
│   ├── scanner/             # 病毒查杀插件
│   ├── sensor/              # 运行时检测插件
│   └── lib/go/              # 插件 SDK
├── api/proto/               # gRPC / Protobuf 定义
├── ui/                      # 前端工程（Vue 3 + TypeScript）
├── configs/                 # 配置文件（server.yaml、agent.yaml、policies/）
├── deploy/                  # 部署配置（Docker Compose、Dockerfile、Nginx、systemd）
├── scripts/                 # 构建与部署脚本
├── docs/                    # 文档
└── tests/                   # API 测试、性能测试
```

---

## 文档

### 部署

- [生产环境部署方案](docs/deployment/production-deployment.md)
- [服务端部署](docs/deployment/server.md)
- [Server 部署指南（兼容入口）](docs/deployment/server-deployment.md)
- [Server 配置文档](docs/deployment/server-config.md)
- [Agent 部署指南](docs/deployment/agent-deployment.md)
- [发行版支持](docs/deployment/distribution-support.md)

### 设计

- [HA 架构设计](docs/design/ha-architecture.md)
- [Agent 架构设计](docs/design/agent-architecture.md)
- [Baseline 策略模型](docs/design/baseline-policy-model.md)
- [Server API 设计](docs/design/server-api.md)

### 开发

- [开发指南](docs/development/development-guide.md)
- [快速开始](docs/development/quick-start.md)
- [插件开发指南](docs/development/plugin-development.md)
- [规则编写指南](docs/RULE_WRITING_GUIDE.md)
- [故障排查](docs/development/troubleshooting.md)

### 其他

- [TODO / 状态看板](docs/TODO.md)
- [基线修复功能说明](docs/BASELINE_FIX_IMPLEMENTATION.md)
- [Agent 连接故障排查](docs/AGENT_CONNECTION_TROUBLESHOOTING.md)
- [Agent 更新指南](docs/AGENT_UPDATE.md)

---

## License

本项目为独立实现，在设计理念上参考了 Elkeid 的 Agent + Plugin + Server 架构。
