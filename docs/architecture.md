# 架构设计

> **平台定位**：mxsec 是一款**工业级开源 CWPP（Cloud Workload Protection Platform）**，专精 **Linux 主机与 Kubernetes 容器**，面向 ToB 政企/金融/互联网客户。
>
> **设计原则**：
> 1. **看清 → 算清 → 处清** — 资产可视化 / 脆弱性识别 / 修复闭环 三段递进；
> 2. **监听优先（Observe-First）** — 默认部署即监听模式，磨合稳定后再切防护模式，详见 [`operating-modes.md`](operating-modes.md)；
> 3. **本地 ML 主导 + LLM 可选** — 实时检测靠本地 ONNX 模型，LLM 仅做语义增强，详见 [`ml-models.md`](ml-models.md)、[`llmproxy-design.md`](llmproxy-design.md)；
> 4. **组件专精** — 每个微服务只做一件事，禁止职责混杂；
> 5. **多租户 from-day-1** — 全平台 `tenant_id` 贯穿，详见 [`multi-tenant.md`](multi-tenant.md)。

---

## 1. 总体拓扑

mxsec 采用 **六微服务 + Kafka 异步解耦** 架构：

```
                                浏览器 / API / CI/CD
                                          |
                                          | HTTPS + JWT
                                          v
                            +-------------+-------------+
                            |    Nginx (TLS + LB)        |
                            +-------------+-------------+
                                          |
   +--------------+--------------+--------+--------+--------------+--------------+
   |              |              |        |        |              |              |
   v              v              v        v        v              v              v
+--+---------+ +--+-----------+ ++--------+--+ +---+----------+ +-+----------+ +-+---------+
| Manager    | | VulnSync     | | AgentCenter| | Consumer    | | Engine     | | LLMProxy  |
|------------| |--------------| |------------| |--------------| |------------| |-----------|
| 业务 API   | | 漏洞情报融合 | | gRPC 接入  | | Kafka→存储  | | 检测分析   | | 多 LLM    |
| RBAC/JWT   | | 11 源同步    | | 任务下发   | | 幂等写入    | | CEL/序列   | | 适配 + 计费|
| 策略/资产  | | advisory     | | Canary灰度| | DLQ         | | IForest/ML | | 缓存/审计  |
| 报表/审计  | | 仲裁/推送    | | 证书下发   | | Sanitize    | | Storyline  | | Fallback   |
| 多租户控制 | |              | | 心跳/SD    | |              | | 告警生成   | |           |
+----+-------+ +----+---------+ +----+-------+ +----+---------+ +---+--------+ +---+-------+
     |              |               |              |               |              |
     |              |               |              |               |              |
     +--------+-----+---------+-----+--------------+---------------+--------------+
              |               |
              v               v
        +-----+----+     +----+----+      +----------+    +-----------+
        |  MySQL   |     |  Redis  |      |ClickHouse|    | Prometheus|
        | 业务主数据|     |  SD/锁/  |      | 事件归档 |    |  指标曲线 |
        |          |     |  缓存    |      |          |    |           |
        +----------+     +---------+      +----------+    +-----------+
                                  ^
                                  |
                          +-------+-------+
                          |     Kafka     |
                          | 10+ Topic     |
                          | ConsumerGroup |
                          |   A: writers  |
                          |   B: engine   |
                          |   C: vulnsync |
                          +-------+-------+
                                  ^
                                  | gRPC BiDi Stream + mTLS
                                  |
            +---------------------+---------------------+
            |                                           |
            v                                           v
   +--------+--------+                         +--------+--------+
   |  mxsec-agent    |  ...  N 台主机/节点  ... |  mxsec-agent    |
   |  Linux 守护进程  |                         |  K8s DaemonSet  |
   |-----------------|                         |-----------------|
   | EDR/eBPF (内置)  |                         | 容器富化         |
   | plugin: baseline|                         | K8s 节点视角     |
   | plugin: scanner |                         |                  |
   | plugin: fim     |                         |                  |
   | plugin: remediation                       |                  |
   | plugin: av-scanner (Phase 4)              |                  |
   | plugin: rasp    (Phase 4 Java MVP)        |                  |
   +-----------------+                         +-----------------+
```

> 控制面无状态、水平扩展；数据面通过 Kafka 解耦写入与分析；Agent 端轻量（单核 CPU < 3% / RSS < 80MB 目标）。

---

## 2. 六微服务职责（专精化设计）

每个服务**只做一件事**。下表是强约束，违反即设计错误。

### 2.1 Manager — 业务控制面

- **唯一职责**：HTTP API + 业务编排 + RBAC + 报表 + 通知 + 多租户控制
- **不做**：Kafka 消费 / 数据同步抓取 / 检测分析 / LLM 推理 / 大批量写入
- **核心模块**：
  - REST API（100+ 端点，JWT + RBAC + Tenant 三段鉴权）
  - 资产 / 策略 / 任务 / 告警 / 修复 / 用户 / 租户 CRUD
  - 报表（PDF / Word / CSV，等保自评 / ISO 27001 / 月度合规）
  - 通知（站内信 / 邮件 / 短信 / Syslog / Webhook）
  - 任务编排（与 AgentCenter / Engine / VulnSync 通过 gRPC 协作）
  - 内嵌 AC SD Registry（Redis HSet + Pub/Sub + 主动探测）
- **入口**：`cmd/server/manager/main.go`
- **副本**：N 副本无状态 + Nginx least_conn

### 2.2 AgentCenter — 数据面接入层

- **唯一职责**：gRPC 接入 / 任务下发 / Canary 灰度 / 证书下发 / 心跳
- **不做**：检测 / 规则同步 / IOC 同步 / 告警调度（这些归 Engine）
- **核心模块**：
  - gRPC 双向流（mTLS `VerifyClientCertIfGiven`，证书自动下发）
  - Agent ↔ AC 连接池管理（Keepalive Time=60s / Timeout=10s / MinTime=10s）
  - 任务下发（HTTP `/command` `/command/batch`，从 Manager 收，向 Agent 推）
  - 数据转发（事件按 DataType 路由到 Kafka 对应 Topic）
  - Kafka 不可用时**内存降级队列**（10000 容量 + 5min TTL + 5 重试）
  - Canary 灰度调度（仅 Agent 升级 + 规则同步两类，Engine 触发）
  - 心跳超时检测 / Agent 重启 / 插件更新 三个本地调度器
- **入口**：`cmd/server/agentcenter/main.go`
- **副本**：N 副本无状态 + L4 LB

### 2.3 Consumer — Kafka → 存储 写入器

- **唯一职责**：Kafka 消息消费 + 幂等写入 MySQL / ClickHouse / Redis + DLQ
- **不做**：检测 / 分析 / 告警 / 规则同步 / 外发
- **核心模块**：
  - ConsumerGroup A `mxsec-writers`，订阅 8 个数据 Topic
  - MySQL `ON DUPLICATE KEY UPDATE` 逐条 Upsert
  - ClickHouse `ReplacingMergeTree` 批量写入（5000 条 / 10s）
  - 失败进 `{topic}.dlq` 不重试阻塞
  - 心跳消费时维护 `agent:ac:{agentID}` 映射 + 触发任务补发
  - Sanitize：敏感字段脱敏（PII / Secret）
- **入口**：`cmd/server/consumer/main.go`
- **副本**：N 副本 + Kafka ConsumerGroup 自动 Rebalance

### 2.4 Engine — 检测分析引擎（核心新增）

- **唯一职责**：实时检测 + 规则匹配 + ML 推理 + 序列分析 + Storyline + 告警生成
- **不做**：MySQL 直接写入（产 alert → Kafka，由 Consumer 持久化）
- **核心模块**：
  - ConsumerGroup B `mxsec-engine`，订阅 8 个数据 Topic（与 Consumer 同源不冲突）
  - **规则层**：CEL 规则引擎 + Sigma / Falco / Tetragon Policies 转换（详见 [`falco-sigma-integration.md`](falco-sigma-integration.md)）
  - **序列层**：Markov 转移 / n-gram 序列异常 / 端口扫描滑动窗口
  - **ML 层**：ONNX Runtime CPU 推理（IForest / LightGBM / MiniLM Embedding 等，详见 [`ml-models.md`](ml-models.md)）
  - **图层**：Storyline 攻击链关联 + ATT&CK 战术映射
  - **K8s 检测**：Audit Event 检测规则（从 Manager 搬入）
  - **响应层**：仅在 `MODE=protect` 时下发处置，`MODE=observe` 仅产告警（详见 [`operating-modes.md`](operating-modes.md)）
  - **LLM 增强**：可选调用 LLMProxy 做告警解释 / Storyline 总结 / 告警去重
- **入口**：`cmd/server/engine/main.go`
- **副本**：N 副本（CPU 密集，独立扩缩）
- **产出 Topic**：`mxsec.engine.alert` / `mxsec.engine.storyline` / `mxsec.engine.feedback`

### 2.5 VulnSync — 漏洞情报融合（核心新增）

- **唯一职责**：从 11+ 外部源同步漏洞数据 + advisory 仲裁融合 + 推送到 Kafka
- **不做**：业务 API / Agent 通信 / 实时检测
- **核心模块**：
  - 定时任务（默认每 1h 增量、每天 1 次全量）
  - **数据源**：
    - NVD（json API）
    - OSV（Google）
    - RedHat RHSA
    - Ubuntu USN
    - Debian DSA / Tracker
    - Alpine SecDB
    - SUSE
    - CISA KEV
    - ExploitDB
    - CNNVD（编号补全）
    - openEuler CSA / Anolis ANSA / Kylin KYSA / UOS UOSEC（信创 4 源）
    - EPSS（FIRST.org）
  - **融合仲裁**：PURL + NEVRA 双索引模型，3 级 confidence
  - **推送**：advisory 推 Kafka `mxsec.vuln.advisory`，Consumer 持久化，Engine 关联检测
- **入口**：`cmd/server/vulnsync/main.go`
- **副本**：单副本 + Leader Election（避免重复抓取）

### 2.6 LLMProxy — 多 LLM 厂商适配（核心新增）

- **唯一职责**：统一 LLM 接口 + 多厂商路由 + 计费 + 缓存 + Fallback
- **不做**：业务逻辑 / 状态持久化（纯 stateless gateway）
- **支持厂商**：
  - OpenAI（GPT-4o / 4o-mini）
  - Anthropic（Claude 3.5 Sonnet / Haiku）
  - Google（Gemini 1.5 Pro / Flash）
  - 阿里千问 DashScope（Qwen-Max / Plus / Turbo）
  - DeepSeek / Kimi / 智谱 / 火山方舟（OpenAI-Compatible 端点）
  - **本地大模型**：Ollama / vLLM（OpenAI-Compatible，离网部署 Qwen 2.5-7B 推荐）
- **核心模块**：
  - Provider 抽象接口（Complete / Stream / Embed / Count）
  - 场景路由（告警分析 → 便宜模型，规则起草 → 推理强模型）
  - Redis 24h cache（入参 SHA256 → 响应）
  - 失败 Fallback（主厂商失败 3 次黑名单 5min）
  - 租户级 token 上限 + 月度成本告警
  - Audit（每次调用入 audit_log）
- **入口**：`cmd/server/llmproxy/main.go`
- **副本**：N 副本无状态

> **可选启用**：LLMProxy 是可选组件。`llm.enabled=false` 时 Engine / Manager 不依赖它。详见 [`llmproxy-design.md`](llmproxy-design.md)。

---

## 3. 数据链路

### 3.1 上报链路（Agent → 持久化 + 检测）

```
Agent (EDR/插件)
   │ gRPC mTLS BiDi Stream + Snappy 压缩
   v
AgentCenter (纯转发，不解析)
   │ Sarama Produce (Key=AgentID 同主机有序)
   v
Kafka (8+ 数据 Topic，分区 6-12)
   ├── ConsumerGroup A: Consumer x N  ──→ MySQL / ClickHouse / Redis (写入)
   ├── ConsumerGroup B: Engine x N    ──→ CEL/序列/ML/Storyline → Kafka mxsec.engine.alert
   └── ConsumerGroup C: VulnSync(可选) ──→ 漏洞 advisory 关联

Engine 产出 Alert → Kafka mxsec.engine.alert
   ├── Consumer 写 MySQL/CK
   ├── Manager UI SSE 实时推
   └── Notification 推送 (在 protect 模式才下发处置)
```

### 3.2 下发链路（用户 → Agent）

```
用户 / API ─→ Manager (业务编排)
   │
   ├─→ 任务持久化 MySQL (status=pending)
   │
   ├─→ SD 查 Redis(agent:ac:{id}) 精准路由
   │
   └─→ AC HTTP /command
            │
            v
       AC sendCh ─→ gRPC stream ─→ Agent ack 回 Kafka
                                       │
                                       v
                                  Consumer 消费 ack → 标记任务完成
```

### 3.3 漏洞情报链路（VulnSync 独立运行）

```
VulnSync (Cron 1h 增量 / 1d 全量)
   │
   ├─→ NVD API
   ├─→ OSV API
   ├─→ RedHat RHSA / Ubuntu USN / Debian DSA / Alpine / SUSE
   ├─→ CISA KEV / ExploitDB
   ├─→ CNNVD（编号补全）
   ├─→ 信创 4 源
   └─→ EPSS
        │
        v
   advisory 融合仲裁
        │
        v
   Kafka mxsec.vuln.advisory
        │
        ├─→ Consumer 写 MySQL (vulnerabilities 表)
        └─→ Engine 主机指纹 vs 漏洞库匹配 → 产 host_vulnerability alert
```

### 3.4 查询链路（前端 → 存储）

| 查询场景 | 数据源 |
|---------|--------|
| 主机/策略/任务/告警状态 | MySQL |
| 监控指标曲线/趋势图 | Prometheus |
| EDR / FIM 事件列表 | ClickHouse（优先），MySQL（fallback） |
| 基线评分 | Redis 缓存 + MySQL 兜底 |
| Dashboard 趋势 | ClickHouse 物化视图 + singleflight 防惊群 |

---

## 4. Kafka 拓扑

### 4.1 Topic 总览

| Topic | DataType | Partitions | Retention | 上行 / 下行 | 说明 |
|-------|----------|-----------|-----------|-------------|------|
| `mxsec.agent.heartbeat` | 1000, 1001 | 6 | 24h | Agent → Consumer | 心跳 + 插件状态 |
| `mxsec.agent.asset` | 5050-5060 | 6 | 7d | Agent → Consumer | 资产清点 |
| `mxsec.agent.events` | 6001, 6002 | 12 | 72h | Agent → Consumer + Engine | FIM 事件 |
| `mxsec.agent.ebpf` | 3000-3002 | 12 | 3d | Agent → Consumer + Engine | EDR 内核事件 |
| `mxsec.agent.baseline` | 8000-8004 | 6 | 7d | Agent → Consumer + Engine | 基线结果 |
| `mxsec.agent.scanner` | 7000-7004 | 6 | 7d | Agent → Consumer + Engine | 病毒/漏洞扫描结果 |
| `mxsec.agent.remediation` | 9100-9299 | 6 | 7d | Agent → Consumer | 修复任务结果 |
| `mxsec.agent.command-ack` | 9999 | 6 | 7d | Agent → Consumer | 命令执行确认 |
| `mxsec.engine.alert` | 11001-11099 | 12 | 7d | Engine → Consumer + Manager SSE | **新** 检测告警 |
| `mxsec.engine.storyline` | 11100-11199 | 6 | 14d | Engine → Consumer | **新** 攻击链 |
| `mxsec.engine.feedback` | 11900-11999 | 3 | 30d | UI → Engine | **新** 误报反馈（用于 ML 训练） |
| `mxsec.vuln.advisory` | 12001-12099 | 6 | 30d | VulnSync → Consumer + Engine | **新** 漏洞情报 |
| `mxsec.llm.audit` | 13001-13099 | 3 | 90d | LLMProxy → Consumer | **新** LLM 调用审计 |
| `mxsec.metering.usage` | 14001-14099 | 3 | 365d | Manager → Consumer | **新** 多租户计量用量（Agent 数 / 事件量 / 告警量 / LLM 成本 / API 调用） |

- 完整 DataType 分配见 [`datatype-allocation.md`](datatype-allocation.md)
- Partition Key 默认 `agent_id`，保证同 Agent 数据有序
- 所有 Topic 配套 DLQ `{topic}.dlq`（保留原始消息 + 错误 + 重试次数 + 失败时间）
- `replication_factor=2`，`min.insync.replicas=1`

### 4.2 ConsumerGroup 拓扑

| ConsumerGroup | 服务 | 订阅 | 用途 |
|--------------|------|------|------|
| `mxsec-writers` | Consumer | 8 个 `mxsec.agent.*` + `mxsec.engine.alert` + `mxsec.engine.storyline` + `mxsec.vuln.advisory` + `mxsec.llm.audit` | 持久化 |
| `mxsec-engine` | Engine | `mxsec.agent.ebpf` + `mxsec.agent.events` + `mxsec.agent.scanner` + `mxsec.agent.baseline` + `mxsec.vuln.advisory` | 检测分析 |
| `mxsec-vulnsync` | VulnSync（仅 1 副本 Leader） | 无（VulnSync 是生产者，不消费） | — |

> Kafka 多 ConsumerGroup 同源消费**互不阻塞、不重复扣费**，是数据面解耦的关键设计。

---

## 5. 存储分层

| 存储 | 定位 | 写入方 | 关键设计 |
|------|------|--------|----------|
| MySQL 8.0+ | 业务主数据（主机、策略、任务、告警状态、资产快照、用户、租户） | Consumer / Manager | 全表 `tenant_id` + 索引；主从 / MGR |
| ClickHouse | 时序与事件归档（指标趋势、EDR/FIM、告警时间线、审计日志） | Consumer | TTL + 物化视图；副本表（生产） |
| Redis | SD 同步、`agent:ac` 映射、分布式锁、基线评分缓存、LLM cache、Embedding 向量缓存 | Manager / Consumer / Engine / LLMProxy | Sentinel / Cluster |
| Prometheus | 主机性能指标查询源（CPU / 内存 / 磁盘 / 网络） | AgentCenter Exporter | 长期存储用 Thanos / VictoriaMetrics |

### Redis Key 设计（新增 LLM/ML 相关）

| Key 模式 | TTL | 用途 |
|---------|-----|------|
| `ac:instances` (Hash) | 120s | AC 实例注册表 |
| `ac:sd:changed` (Pub/Sub) | - | AC 状态变更通知 |
| `agent:ac:{agentID}` | 180s | Agent → AC 实例映射 |
| `mxsec:cache:dashboard:stats` | 30s | Dashboard 缓存 + singleflight |
| `mxsec:seq:{ruleID}:{hostID}` | rule.Window | 序列检测中间状态 |
| `mxsec:task:dispatch:lock` | 8s | 任务调度分布式锁 |
| `mxsec:vulnsync:lock` | 30m | VulnSync Leader 锁 |
| `mxsec:ioc:{type}` (Set) | 24h | 威胁情报 IOC |
| `mxsec:llm:cache:{hash}` | 24h | **新** LLM 调用缓存 |
| `mxsec:ml:embedding:{hash}` | 24h | **新** Embedding 向量缓存 |
| `mxsec:llm:tenant:cost:{tenant}:{yyyymm}` | 32d | **新** 租户月度 token 成本 |
| `mxsec:llm:provider:blacklist:{provider}` | 5m | **新** Fallback 黑名单 |

---

## 6. 运行模式（监听 / 防护）

mxsec **默认部署即监听模式**（`MODE=observe`），仅产告警不下处置。
平台稳定后按门槛逐步切防护模式（`MODE=protect`）。

| 模式 | 检测 | 自动响应 | Admission Webhook | Agent 处置 | 适用阶段 |
|------|------|----------|-------------------|-----------|----------|
| `observe` | ✅ 全功能 | ❌ 不下处置（仅 audit） | dry-run 仅 warn | 仅日志 | **默认**，磨合阶段 |
| `protect` | ✅ 全功能 | ✅ IP 封禁 / PAM / 端口 | deny 真拦截 | kill / quarantine | 数据磨合 ≥ 90 天 + P/R 达标后切 |

> 完整哲学、切换门槛、灰度策略见 [`operating-modes.md`](operating-modes.md)。

---

## 7. 安全与通信

| 链路 | 协议 | 认证方式 |
|------|------|---------|
| 浏览器 ↔ Nginx / Manager | HTTPS + REST | JWT + RBAC + Tenant |
| Agent ↔ AgentCenter | gRPC 双向流 | mTLS（`VerifyClientCertIfGiven` + 自动证书下发） |
| Agent ↔ Plugin | OS Pipe + Protobuf | 父子进程隔离 |
| Manager ↔ AgentCenter | gRPC | **mTLS**（Phase 1 升级，原 `X-Internal-Secret` 仅过渡） |
| Manager ↔ Engine / VulnSync / LLMProxy | gRPC | mTLS + 内部 Bearer Token |
| Engine ↔ LLMProxy | gRPC | mTLS + 内部 Bearer Token |
| LLMProxy ↔ 外部 LLM API | HTTPS | 客户配置的 API Key + TLS 证书校验 |

- mTLS 细节、证书自动下发、Keepalive 见 `internal/server/agentcenter/server/server.go`
- 证书生成 `scripts/generate-certs.sh`

---

## 8. 高可用与容量

### 8.1 已具备 HA 能力

| 组件 | 方式 | 说明 |
|------|------|------|
| Manager / AC / Consumer / Engine / LLMProxy | xN 副本 + LB | 全部无状态 |
| VulnSync | 单副本 + Leader Election | 避免重复抓取 |
| Kafka | 3 Broker KRaft | `replication_factor=2` |
| Redis SD | Pub/Sub + 30s 全量兜底 | Manager 内存为源头 |

### 8.2 容量上限（按当前架构）

| 档位 | Agent 数 | 推荐部署 | 备注 |
|------|---------|----------|------|
| Demo | 100-500 | 单机 docker-compose | 默认开发 |
| 小规模 | 500-2k | 标准多副本 | 无需改动 |
| 中规模 | 2k-10k | 扩 Kafka 分区 + CK 副本表 | 1-2 月工程化 |
| 大规模 | 10k-50k | + Kafka 多集群 + MySQL 主从异地 | 2-3 月 |
| 极限 | 50k-300k | + TiDB/Vitess + 独立 SD 服务 + Region Federation | M2 阶段 |

详见 [`deployment.md`](deployment.md) 容量规划章节。

### 8.3 关键 SLO 目标

| 指标 | 目标 | 来源 |
|------|------|------|
| Agent CPU 稳态 | < 3% | 工业级口径 |
| Agent RSS | < 80 MB | 工业级口径 |
| 平台可用性 | 99.95%（中规模）/ 99.99%（KA） | KA 合同 |
| 告警 P95 延迟 | ≤ 5s（Agent → UI） | SOC 运营 |
| 任务可达率 | ≥ 99.9% | KA 合同 |
| Kafka Consumer Lag | P99 ≤ 30s | 数据面 SLO |
| Engine 误报率 | ≤ 5%（90d 数据磨合后 ≤ 2%） | observe → protect 切换前置 |
| 数据 RPO | ≤ 15min（KA）/ ≤ 1h（标准） | 备份与重放 |

---

## 9. 多租户

全平台 `tenant_id` 贯穿，从 JWT claims → API 中间件 → DB 行级隔离 → Kafka topic 命名 → 告警归属 → 计费统计。

详见 [`multi-tenant.md`](multi-tenant.md)。

---

## 10. 智能分析（本地 ML + LLM 双层）

**用户可选**：纯规则 / +本地 ML / +LLM API，三档独立开关，UI 全局配置。

| 档位 | 配置 | 适用 |
|------|------|------|
| Baseline | `ml=off`, `llm=off` | 离网 + 低配 + 不信任 AI |
| Smart（默认推荐） | `ml=on`, `llm=off` | 离网政企首选 |
| AI-Native | `ml=on`, `llm=on` | 有公网客户 |

- 本地 ML：ONNX Runtime CPU 推理，10 个开源模型，详见 [`ml-models.md`](ml-models.md)
- LLM：多厂商适配，详见 [`llmproxy-design.md`](llmproxy-design.md)
- 规则中台：Falco + Sigma + Tetragon 转 CEL，详见 [`falco-sigma-integration.md`](falco-sigma-integration.md)

---

## 11. 关键代码路径

```
cmd/server/manager/              # Manager 入口
cmd/server/agentcenter/          # AgentCenter 入口
cmd/server/consumer/             # Consumer 入口
cmd/server/engine/               # Engine 入口（Phase 1 新增）
cmd/server/vulnsync/             # VulnSync 入口（Phase 1 新增）
cmd/server/llmproxy/             # LLMProxy 入口（Phase 1 新增）

internal/server/manager/         # 业务 API + RBAC + 编排
internal/server/manager/sd/      # AC 服务发现 Registry
internal/server/agentcenter/     # gRPC 接入 + 任务下发
internal/server/consumer/writer/ # 幂等写入器（Consumer 唯一职责）
internal/server/engine/          # 检测引擎（rule/sequence/ml/storyline/k8s）
internal/server/vulnsync/        # 多源同步 + 仲裁
internal/server/llmproxy/        # 多 provider 适配
internal/server/common/kafka/    # Kafka Producer / Topic 路由
internal/server/common/tenant/   # 多租户中间件
internal/server/database/        # MySQL / Redis / ClickHouse 客户端

internal/agent/                  # Agent 框架（连接 / 传输 / 插件管理）
internal/agent/edr/              # EDR 内置（不再独立 plugin）
internal/agent/updater/          # 自更新 + 灰度

plugins/baseline/                # 基线插件
plugins/scanner/                 # 漏洞扫描插件
plugins/fim/                     # 文件完整性插件
plugins/remediation/             # 修复执行插件
plugins/av-scanner/              # 病毒扫描插件（Phase 4 新增）
plugins/rasp/                    # RASP 插桩插件（Phase 4 新增）
plugins/lib/                     # 插件共享库

api/proto/                       # Protobuf 定义
cmd/tools/mxctl/                 # 集群部署 CLI
internal/deploy/cluster/         # 集群部署引擎
internal/common/signing/         # Ed25519 签名校验
```

---

## 12. 与上一代架构的差异

mxsec 在 v1.x 阶段（截至 2026-05）使用 **Manager + AC + Consumer 三层架构**。v2.0 起重构为 **六微服务**，关键差异如下：

| 维度 | v1.x | v2.0 |
|------|------|------|
| 服务数 | 3（Manager / AC / Consumer） | 6（+ Engine / VulnSync / LLMProxy） |
| Consumer 职责 | 写入 + 分析混杂（celengine / anomaly / storyline / siem 全塞） | **纯写入**，分析全搬到 Engine |
| Manager 职责 | 业务 + 数据同步 + K8s 检测 + LLM 大杂烩 | **纯业务**，同步搬 VulnSync，检测搬 Engine，LLM 搬 LLMProxy |
| 智能分析 | 仅规则 + LLM 辅助（嵌在 Manager） | **本地 ML + 多 LLM** 双层架构 |
| 运行模式 | 默认即响应 | **默认监听**，按门槛切防护 |
| 多租户 | 无 | from-day-1 全平台贯穿 |
| 内部认证 | `X-Internal-Secret` 共享密钥 | 全 mTLS + 内部 Bearer Token |

---

## 13. 参考文档

| 主题 | 文档 |
|------|------|
| 双运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| EDR Agent 采集 | [`edr-agent-design.md`](edr-agent-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| 漏洞模块设计 | [`vuln-module-design.md`](vuln-module-design.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| 资产统一模型 | [`asset-model.md`](asset-model.md) |
| 三大产品目标 | [`security-objectives.md`](security-objectives.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署指南 | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 路线图（内部） | `ref/08-roadmap.md` |
| 对标评估（内部） | `ref/00-总体评估与商业化路线.md` |
