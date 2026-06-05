# 部署指南 v2

> **平台定位**：mxsec 是**工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**，面向 ToB 政企/金融/互联网客户。本文档为**六微服务**（Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy）的端到端部署手册。
>
> **运行模式默认 `observe`（监听）**，磨合达标后再按 [`operating-modes.md`](operating-modes.md) §3 切 `protect`（防护）。**不允许"部署即阻断"**。
>
> **多租户 from-day-1**：所有部署形态默认开启 `tenant_id` 行级隔离，KA 客户可按 [`multi-tenant.md`](multi-tenant.md) §6 升级到 Schema / Dedicated DB 物理隔离。
>
> **前置阅读**：[`architecture.md`](architecture.md) §1 总体拓扑 / §4 Kafka / §8 容量；[`operating-modes.md`](operating-modes.md) §4 灰度；[`multi-tenant.md`](multi-tenant.md) §6 物理隔离；[`llmproxy-design.md`](llmproxy-design.md) §17 部署形态。

---

## 目录

1. 部署形态总览
2. 部署架构（对应六微服务）
3. 容量规划档位（Demo / 标准 / 中规模 / 大规模 / 极限）
4. 单机 docker-compose（Demo / 评估）
5. 标准多副本 K8s helm chart
6. K8s + 外部 MySQL/Redis/Kafka/CK（生产）
7. 跨 Region 双中心（KA 容灾）
8. 六微服务部署清单
9. 外部依赖部署（MySQL / Redis / Kafka / ClickHouse / Prometheus / Ollama）
10. mxctl CLI（集群部署引擎）
11. helm chart 完整示例
12. docker-compose 完整示例
13. K8s manifest 完整示例
14. 多租户部署形态（Shared / Schema / Dedicated DB）
15. 离网部署（含 LLM 离网 + 镜像私有仓库）
16. 信创 OS 部署适配
17. 证书生成与轮换
18. 升级与回滚
19. 备份与恢复（RPO/RTO 目标）
20. 健康检查与故障排查
21. 网络与端口
22. 平台支持矩阵

---

## 1. 部署形态总览

mxsec 提供 **4 种官方部署形态**，按客户规模与可用性需求选择：

| 形态 | 标识 | 拓扑 | Agent 规模 | 可用性 | 适用 |
|------|------|------|-----------|--------|------|
| Demo / All-in-One | `compose-demo` | 1 台机器 docker-compose | ≤ 500 | 单点 | 评估试用 / POC / 培训 |
| 标准生产 K8s | `helm-standard` | 1 个 K8s 集群 + 内置 Bitnami MySQL/Redis/Kafka/CK | 500 – 10k | 99.95% | 中型政企 / 互联网 |
| 生产 + 外部托管 | `helm-managed-deps` | 1 个 K8s 集群 + 云 RDS / 云 Kafka / 云 ClickHouse | 10k – 50k | 99.95% | 大客户 / 公有云原生 |
| 跨 Region 双中心 | `helm-multi-region` | 2 个 K8s 集群（主-备 / 主-主）+ MGR + MM2 + Thanos | 50k – 300k | 99.99% | 金融 KA / 监管行业 |

> **不再使用 v1.x 时期的 "All-in-One / 标准 3 节点 / 高规格 5+ 节点" 三档命名**。v2.0 起按"部署形态 + 容量档位"二维选择。

---

## 2. 部署架构（对应六微服务）

### 2.1 控制面 / 数据面 划分

```
                       浏览器 / API / CI/CD
                              │ HTTPS + JWT + Tenant
                              v
                  +-----------+-----------+
                  |  Nginx Ingress (TLS)  |
                  +-----------+-----------+
                              │
   +----------+---------+-----+-----+-----------+----------+
   |          |         |           |           |          |
   v          v         v           v           v          v
+---+---+ +---+-----+ +-+---------+ +---+-----+ +-+-------+ +-+-------+
| Manager| | VulnSync | | AgentCenter| | Consumer | | Engine | | LLMProxy|
| HTTP API| | Cron Leader| | gRPC mTLS  | | Kafka→DB | | CEL/ML | | LLM 网关|
| N 副本  | | 1 副本     | | N 副本     | | N 副本    | | N 副本  | | N 副本   |
+----+---+ +-----+----+ +------+----+ +-----+----+ +----+---+ +----+---+
     |           |             |            |          |          |
     +-----+-----+-----+-------+--+---------+----------+----------+
           |           |          |
           v           v          v
        +--+---+    +--+--+    +-+-----+
        | MySQL|    |Redis|    | Kafka |
        | 主从  |    | HA  |    | 3+ Broker
        +--+---+    +-----+    +-+-----+
           |                     |
           v                     v
       +---+--------+        +---+----------+
       | ClickHouse |        | Prometheus + |
       | 副本/分片   |        | Thanos/VM    |
       +------------+        +--------------+
                                  ^
                                  | gRPC BiDi Stream + mTLS
                                  |
        +-------------------------+--------------------------+
        |                                                    |
        v                                                    v
 +------+-------+                                    +-------+------+
 | mxsec-agent  |   ... N 台主机 / N 个 K8s 节点 ... | mxsec-agent  |
 | Linux daemon |                                    | DaemonSet    |
 +--------------+                                    +--------------+
```

### 2.2 流量类型

| 流量 | 协议 | 入口 | 后端 | 说明 |
|------|------|------|------|------|
| 用户 UI / API | HTTPS | Ingress / Nginx | Manager | 7 层 LB |
| Agent 接入 | gRPC mTLS | L4 LB / NodePort | AgentCenter `:6751` | **四层** LB，长连接 |
| 内部服务间 | gRPC mTLS + 内部 Bearer | ClusterIP | Manager↔Engine / Manager↔VulnSync / Engine↔LLMProxy | 服务网格可选 |
| 数据面 | Kafka 协议 | Headless Service | Kafka Broker | 仅 Pod 内访问 |
| 出网 LLM | HTTPS | egress gateway | OpenAI / Anthropic / DashScope ... | 离网模式禁用 |

### 2.3 启动顺序（依赖图）

```
[L0] MySQL  ←─ Redis  ←─ Kafka  ←─ ClickHouse  ←─ Prometheus
                                  │
[L1]                              ├── Consumer  (订阅 Kafka，等 MySQL/CK Ready)
[L1]                              ├── VulnSync (Leader 选举，等 Redis Ready)
                                  │
[L2] AgentCenter  (等 Kafka + Redis Ready)
[L2] Manager      (等 MySQL + Redis + AC 注册 Ready)
[L2] Engine       (等 Kafka + Redis + Manager gRPC Ready)
[L2] LLMProxy     (等 Redis + Kafka Ready，可选)
                                  │
[L3] Ingress / Nginx / L4 LB
                                  │
[L4] mxsec-agent  (DaemonSet / RPM 启动，连接 L4 LB)
```

> Helm chart 通过 `initContainers` + `readinessProbe` 强制顺序；docker-compose 用 `depends_on` 配合健康检查。

---

## 3. 容量规划档位

> 完整档位口径见 [`architecture.md`](architecture.md) §8.2 / §8.3 SLO。本节给出**每档**六微服务 + 外部依赖的副本 / CPU / Mem / 磁盘建议。

### 3.1 六微服务副本数矩阵

| 档位 | Agent | Manager | AC | Consumer | Engine | VulnSync | LLMProxy |
|------|-------|---------|----|----|--------|----------|----------|
| Demo | 100 – 500 | 1×（1C/1G）| 1×（1C/1G）| 1×（1C/1G）| 1×（2C/2G）| 1×（0.5C/512M）| 0 / 1×（0.5C/256M）|
| 标准 | 500 – 2k | 2×（2C/2G）| 2×（2C/2G）| 2×（2C/2G）| 2×（4C/4G）| 1×（1C/1G + 20G PVC）| 2×（0.5C/256M）|
| 中规模 | 2k – 10k | 3×（2C/2G）| 4×（4C/4G）| 4×（4C/4G）| 4×（8C/8G）| 1×（2C/2G）| 2×（1C/512M）|
| 大规模 | 10k – 50k | 6×（4C/4G）| 10×（4C/4G）| 8×（4C/4G）| 10×（8C/16G）| 1×（2C/2G）| 4×（1C/1G）|
| 极限 | 50k – 300k | 12 / Region × 4C/4G | 30 / Region × 4C/4G | 24 / Region × 4C/4G | 30 / Region × 8C/16G | 1 全局 ×（4C/4G）| 8 / Region × 2C/2G |

### 3.2 外部依赖矩阵

| 档位 | MySQL | Redis | Kafka | ClickHouse | Prometheus 长期存储 |
|------|-------|-------|-------|------------|---------------------|
| Demo | 1×（2C/4G/50G SSD）| 1×（0.5C/1G）| 1 Broker KRaft（2C/4G/100G）| 1×（2C/4G/100G）| 1×（1C/2G/50G，30d）|
| 标准 | 主从 1主1从（4C/8G/200G）| Sentinel 1主2从+3 哨兵（1C/2G）| 3 Broker（2C/4G/300G，RF=2）| 单分片双副本（4C/8G/500G）| 单实例（2C/4G/200G，30d）|
| 中规模 | MGR 3 副本（8C/16G/500G）| Cluster 3 主 3 从（2C/4G）| 3 Broker（4C/8G/1T，RF=3，分区 12-24）| 2 分片 × 2 副本（8C/16G/2T）| Prometheus + Thanos + 对象存储 |
| 大规模 | 云 RDS 1 主 2 从（16C/64G）| 云托管 Cluster 3 主 3 从（4C/8G）| 5 Broker 云托管（8C/16G，RF=3，分区 48）| 3 分片 × 2 副本（16C/32G）| VictoriaMetrics Cluster 3 vmstorage（4C/16G，≥ 90d）|
| 极限 | TiDB / Vitess 12 节点（16C/64G）| Cluster 6 主 6 从（4C/16G）| 9 Broker / Region + MM2（16C/32G，分区 96）| 4 分片 × 3 副本（32C/64G）| Thanos 6 store + 3 query 跨 Region 联邦 |

### 3.3 关键 SLO 提醒

- Agent CPU 稳态 < 3%、RSS < 80 MB（工业级口径）
- 告警 P95 延迟 ≤ 5s（Agent → UI）
- Kafka Consumer Lag P99 ≤ 30s
- 任务可达率 ≥ 99.9%
- 平台可用性：标准 99.95% / 大规模 99.95% / 跨 Region 99.99%
- Engine 误报率 ≤ 5%（90d 磨合后 ≤ 2%，详见 [`operating-modes.md`](operating-modes.md) §3）

## 4. 单机 docker-compose（Demo / 评估）

适用：100-500 Agent，单点，POC / 培训 / 开发自测。

### 4.1 前置

- Linux x86_64，CentOS 7+ / Rocky 8+ / Ubuntu 20.04+，内核 ≥ 4.18
- Docker Engine ≥ 24，Docker Compose v2
- 16 core CPU / 32 GB RAM / 500 GB SSD
- NTP 时钟同步

### 4.2 部署步骤

```bash
git clone https://github.com/mxsec-platform/mxsec-platform.git
cd mxsec-platform
cp deploy/.env.example deploy/.env
vim deploy/.env
# 必填: SERVER_IP / JWT_SECRET / MYSQL_PASSWORD / REDIS_PASSWORD / CLICKHOUSE_PASSWORD

# 生成 mTLS 证书（自签）
make certs

# 启动六微服务 + 全部依赖
cd deploy
docker compose --env-file .env -f docker-compose.demo.yml up -d \
  --scale manager=1 \
  --scale agentcenter=1 \
  --scale consumer=1 \
  --scale engine=1 \
  --scale vulnsync=1 \
  --scale llmproxy=0   # 默认关闭，按需开启
```

### 4.3 验证

```bash
# 服务状态
docker compose -f docker-compose.demo.yml ps

# 控制面健康
curl -k https://<SERVER_IP>/health
curl -k https://<SERVER_IP>/api/v2/system/mode
# 期望返回 {"default":"observe", ...}

# AgentCenter SD
TOKEN=$(curl -sk -X POST https://<SERVER_IP>/api/v2/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.token')
curl -sk -H "Authorization: Bearer $TOKEN" \
  https://<SERVER_IP>/api/v2/discovery/agentcenter | jq

# Kafka Topics
docker compose exec kafka kafka-topics.sh --bootstrap-server localhost:9092 --list
# 期望看到: mxsec.agent.heartbeat / .ebpf / .events / .scanner / .baseline /
#          mxsec.engine.alert / .storyline / .feedback / mxsec.vuln.advisory / mxsec.llm.audit
```

### 4.4 资源占用预期（500 Agent）

| 指标 | 期望值 |
|------|--------|
| 整机 CPU 使用率 | ≤ 50% |
| 整机内存 | ≤ 24 GB |
| MySQL 数据量 | ≤ 5 GB / 30d |
| ClickHouse 数据量 | ≤ 150 GB / 30d |
| Kafka 磁盘 | ≤ 50 GB（72h 保留） |

---

## 5. 标准多副本 K8s helm chart

适用：500 – 10k Agent，HA 起步，单集群部署，依赖内置或外部均可。

### 5.1 前置

- K8s ≥ 1.27（推荐 1.30）
- Helm ≥ 3.14
- StorageClass 支持动态供应（fast-ssd / standard）
- Ingress Controller（Nginx / Traefik）
- L4 LB（云 NLB / MetalLB BGP）用于 AgentCenter `:6751`
- cert-manager（可选，自动签发服务证书）

### 5.2 chart 仓库

```bash
helm repo add mxsec https://charts.mxsec.io
helm repo update
helm search repo mxsec
# mxsec/mxsec-platform   v1.0.0  矩阵云安全平台 (六微服务 + 全依赖)
# mxsec/mxsec-deps       v1.0.0  仅依赖 (MySQL+Redis+Kafka+CK+Prometheus)
# mxsec/mxsec-agent      v1.0.0  Agent DaemonSet（K8s 节点）
```

### 5.3 标准安装

```bash
kubectl create namespace mxsec
helm install mxsec mxsec/mxsec-platform \
  --namespace mxsec \
  --version v1.0.0 \
  -f my-values.yaml
```

最小 `my-values.yaml` 见 §11.1。

### 5.4 部署完成后

```bash
kubectl -n mxsec get pods
# manager-xxx               1/1 Running
# manager-yyy               1/1 Running
# agentcenter-aaa           1/1 Running
# agentcenter-bbb           1/1 Running
# consumer-ccc              1/1 Running
# consumer-ddd              1/1 Running
# engine-eee                1/1 Running
# engine-fff                1/1 Running
# vulnsync-0                1/1 Running
# llmproxy-ggg              1/1 Running (可选)
# mysql-0 / redis-0 / kafka-0 ... clickhouse-0 ...

kubectl -n mxsec get ingress
# mxsec-ui    nginx   security.example.com   443

kubectl -n mxsec get svc agentcenter-grpc -o wide
# LoadBalancer   10.0.0.45    1.2.3.4  6751:30751/TCP
```

---

## 6. K8s + 外部 MySQL/Redis/Kafka/CK（生产）

适用：10k – 50k Agent，依赖全部托管或 Operator 部署，控制面仅跑微服务。

### 6.1 依赖来源选择

| 依赖 | 推荐来源 |
|------|----------|
| MySQL 8.0+ | 云 RDS（阿里 RDS / AWS RDS / Azure Database）或 Vitess / TiDB |
| Redis | 云 ElastiCache / Tencent Cloud Redis / Bitnami Sentinel |
| Kafka | 云 MSK / Strimzi Operator / Confluent Platform |
| ClickHouse | clickhouse-operator / Altinity Cloud / Yandex Cloud |
| Prometheus | kube-prometheus-stack + Thanos |
| Ollama / vLLM | 独立 namespace，按需 GPU |

### 6.2 disable 内置依赖

`values-prod.yaml`：

```yaml
mysql:
  enabled: false
  external:
    host: rds-mxsec-master.mysql.rds.aliyuncs.com
    port: 3306
    database: mxsec
    user: mxsec_user
    existingSecret: mysql-credentials
    sslMode: VERIFY_IDENTITY

redis:
  enabled: false
  external:
    addr: r-bp123456.redis.rds.aliyuncs.com:6379
    sentinel: false   # 云托管单端点直连
    existingSecret: redis-credentials

kafka:
  enabled: false
  external:
    brokers:
      - alikafka-pre-cn-abc-1.alikafka.aliyuncs.com:9093
      - alikafka-pre-cn-abc-2.alikafka.aliyuncs.com:9093
      - alikafka-pre-cn-abc-3.alikafka.aliyuncs.com:9093
    saslMechanism: PLAIN
    existingSecret: kafka-sasl

clickhouse:
  enabled: false
  external:
    host: clickhouse.mxsec.svc.cluster.local
    httpPort: 8123
    tcpPort: 9000
    database: mxsec
    existingSecret: clickhouse-credentials
```

### 6.3 关键加固

- MySQL `binlog_format=ROW`、`gtid_mode=ON`、连接池上限按 Manager 副本数 × 50 估算
- Redis `maxmemory-policy=allkeys-lru`，TTL 默认 24h
- Kafka `auto.create.topics.enable=false`，启用 ACL（每个 ConsumerGroup 独立 SASL 用户）
- ClickHouse `ReplicatedReplacingMergeTree` + `default_database_engine=Atomic`
- 所有云资源开 VPC peering，控制面与依赖同 AZ 跨 AZ 容灾

---

## 7. 跨 Region 双中心（KA 容灾）

适用：50k – 300k Agent，金融 / 监管行业，**可用性 99.99%，RPO ≤ 15min，RTO ≤ 30min**。

### 7.1 拓扑（active-passive 主备）

```
                                 +-----------------------+
                                 | 全局 DNS / 智能解析   |
                                 +-----+----------+------+
                                       |          |
                                       v          v
              +------------------------+          +------------------------+
              |     Region A (主)       |          |     Region B (备)       |
              |  + 全量控制面 + 数据面   |          |  + 全量控制面（standby）  |
              |  + Manager / AC / Engine|          |  + Manager / AC / Engine|
              |  + Consumer / VulnSync  |          |  + Consumer / 不跑      |
              |  + LLMProxy             |          |  + VulnSync 不跑（避重）|
              |                         |          |                         |
              |  MySQL MGR（主）        |◄────GTID 异步复制────────►| MySQL MGR（从） |
              |  Redis Cluster          |◄──RedisShake 双写─►| Redis Cluster   |
              |  Kafka 3 broker         |◄──MirrorMaker 2─►| Kafka 3 broker   |
              |  ClickHouse 副本表       |◄──CK Replication─►| ClickHouse 副本表 |
              |  Thanos + S3            |◄──S3 bucket sync─►| Thanos          |
              +-------------------------+          +-------------------------+

        +----------+                                          +----------+
        | Agent x N | --(默认主 Region)-------┐                 | Agent x N|
        | Region A  |                          v                 | Region B |
        +-----------+                  +-------+--------+        +----------+
                                       | 全局 L4 LB     |
                                       | (BGP Anycast)  |
                                       +----------------+
```

### 7.2 数据同步策略

| 数据 | 策略 | RPO |
|------|------|-----|
| MySQL | GTID 异步 + 半同步可选 | ≤ 5s |
| Redis | RedisShake / 云原生跨区复制 | ≤ 10s |
| Kafka | MirrorMaker 2，主→备单向 | ≤ 15s |
| ClickHouse | ReplicatedMergeTree 跨 Region | ≤ 30s |
| Prometheus | Thanos Sidecar → 对象存储跨 Region 复制 | ≤ 60s |
| 配置 / 规则 | etcd + Manager 双 Region 同步 | ≤ 5s |

### 7.3 切换流程

1. Region A 故障检测（5min 持续不可达）
2. 全局 DNS 切到 Region B（TTL 30s）
3. Region B 的 MySQL MGR 提升为主（`SET GLOBAL group_replication_single_primary_mode = ON`）
4. Kafka MM2 反向同步开启
5. Agent 重连到 Region B 的 L4 LB
6. Engine 继续消费 Region B 的 Kafka（ConsumerGroup offset 已同步）
7. 验证 SLO 恢复（告警 P95 ≤ 5s，任务可达率 ≥ 99.9%）

> 切换全流程由 `mxctl region failover` 一键执行，默认演练每 30 天 1 次。

### 7.4 active-active 形态（可选，复杂度高）

适用极端要求"两地双写"的金融客户，需额外解决：
- `tenant_id` 路由（每个租户固定主 Region）
- 双向 MM2 + 死循环防护（消息打 origin Region tag）
- Engine 检测幂等（按 alert hash 去重）

详见 `ref/01-服务端架构.md` §7 风险与开放问题。

---

## 8. 六微服务部署清单

> 全部组件**默认 `mode=observe`**、**默认 `tenant_id` 行级隔离**、**默认禁用 LLMProxy**（`llm.enabled=false`）。镜像统一 `mxsec/{component}:v1.0.0`。启动顺序见 §2.3。

| 组件 | 副本 | 资源（标准 / 大规模）| 入口 | ConsumerGroup / 输出 Topic | 关键约束 |
|------|------|----------------------|------|---------------------------|----------|
| Manager | ≥ 2 无状态 | 2C/2G ・ 4C/4G | HTTP `:8080` + Ingress + `/health /ready /metrics` | — | 内嵌 AC SD Registry（Redis HSet+Pub/Sub）；`server.yaml` 含 MySQL/Redis/Kafka/JWT/mTLS/Tenant 默认；L2 启动 |
| AgentCenter | N（≤ 5000 Agent / 副本）| 2C/2G ・ 4C/4G | gRPC `:6751` mTLS（**必须 L4 LB**）+ HTTP `:6752` 内网 | — | Keepalive 60s/10s/10s；`server.crt/key` + `ca.crt`，Agent 首次自动下发 `agent.crt/key`；Kafka 不可用降级内存队列 10000 / 5min TTL / 5 次重试；L2 启动 |
| Consumer | ≥ Kafka 总分区 / 12 | 2C/2G ・ 4C/4G | `/metrics :9100` | CG `mxsec-writers` | MySQL `ON DUPLICATE KEY UPDATE`；CK 批量 5000 条 / 10s；DLQ `{topic}.dlq`；L1 启动 |
| Engine | N（CPU 密集）| 4C/4G ・ 8C/16G | gRPC `:18000` + `/metrics :9101` | CG `mxsec-engine` → `mxsec.engine.alert/.storyline/.feedback` | ONNX Runtime CPU；`mode_default=observe`；模型 `/opt/mxsec/models/*.onnx` 镜像或 PVC 挂载；LLM 异步 5s 超时不阻塞主链路；L2 启动 |
| VulnSync | **1**（Leader Election）| 1-2C / 1-2G + 20 GB PVC | gRPC `:18100` + `/metrics :9102` | → `mxsec.vuln.advisory` | Redis 锁 `mxsec:vulnsync:lock` TTL 30m；11 源（NVD/OSV/RHSA/USN/DSA/Alpine/SUSE/KEV/ExploitDB/CNNVD/EPSS）+ 信创 4 源；增量 1h / 全量 24h；L1 启动 |
| LLMProxy（可选）| ≥ 2 无状态 | 0.5C/256M – 2C/2G | gRPC `:18900` + `/healthz /metrics :18901` | → `mxsec.llm.audit` | API Key 仅 Secret / Vault，**严禁** ConfigMap 明文；`air_gapped=true` 须配 Ollama / vLLM；`llm.enabled=true` 才部署；L2 启动 |

## 9. 外部依赖部署

### 9.1 MySQL

```yaml
# 标准：主从（异步复制）
mysql:
  architecture: replication
  auth: { rootPassword: ${MYSQL_ROOT_PASSWORD}, database: mxsec, username: mxsec_user, password: ${MYSQL_PASSWORD} }
  primary:
    persistence: { size: 200Gi, storageClass: fast-ssd }
    configuration: |
      [mysqld]
      character-set-server=utf8mb4
      collation-server=utf8mb4_unicode_ci
      max_connections=2000
      innodb_buffer_pool_size=8G
      binlog_format=ROW
      gtid_mode=ON
      enforce_gtid_consistency=ON
  secondary: { replicaCount: 1, persistence: { size: 200Gi } }

# 中大规模：MGR 多副本单主写
mysql:
  architecture: group-replication
  groupReplication: { members: 3, singlePrimaryMode: ON }

# 生产首推：云 RDS 外部托管，set mysql.enabled=false + external.host=...（见 §6.2）
```

### 9.2 Redis

```yaml
# 标准 / 中规模：Sentinel
redis:
  architecture: replication
  sentinel: { enabled: true, quorum: 2 }
  master:   { persistence: { size: 10Gi } }
  replica:  { replicaCount: 2 }
  auth:     { enabled: true, password: ${REDIS_PASSWORD} }

# 大规模 / 极限：Cluster
redis:
  cluster:  { enabled: true, nodes: 6 }      # 3 主 3 从
  auth:     { enabled: true, password: ${REDIS_PASSWORD} }
```

### 9.3 Kafka（3 Broker KRaft）

```yaml
kafka:
  controller: { replicaCount: 3 }
  broker:
    replicaCount: 3
    persistence: { size: 300Gi, storageClass: fast-ssd }
    resources: { requests: { cpu: 2, memory: 4Gi }, limits: { cpu: 4, memory: 8Gi } }
  zookeeper: { enabled: false }                # KRaft 模式不需要
  kraft: { enabled: true }
  defaultReplicationFactor: 2
  minInsyncReplicas: 1
  autoCreateTopicsEnable: false                # 严禁自动建 Topic
  logRetentionHours: 72
```

Topic 清单（helm post-install hook 自动创建，每个业务 Topic 同时创建 `{topic}.dlq`）：

| Topic | 分区 | RF | 保留 |
|-------|------|----|------|
| `mxsec.agent.heartbeat` | 6 | 2 | 24h |
| `mxsec.agent.asset` | 6 | 2 | 7d |
| `mxsec.agent.events` | 12 | 2 | 3d |
| `mxsec.agent.ebpf` | 12 | 2 | 3d |
| `mxsec.agent.baseline` | 6 | 2 | 7d |
| `mxsec.agent.scanner` | 6 | 2 | 7d |
| `mxsec.agent.remediation` | 6 | 2 | 7d |
| `mxsec.agent.command-ack` | 6 | 2 | 7d |
| `mxsec.engine.alert` | 12 | 2 | 7d |
| `mxsec.engine.storyline` | 6 | 2 | 14d |
| `mxsec.engine.feedback` | 3 | 2 | 30d |
| `mxsec.vuln.advisory` | 6 | 2 | 30d |
| `mxsec.llm.audit` | 3 | 2 | 90d |

### 9.4 ClickHouse（副本表 / 分片）

```yaml
# 标准 / 中规模：单分片双副本
clickhouse:
  shards: 1
  replicasPerShard: 2
  persistence: { size: 500Gi, storageClass: fast-ssd }
  zookeeper:   { enabled: true, replicaCount: 3 }
  configuration: |
    <yandex>
      <merge_tree>
        <max_suspicious_broken_parts>1000</max_suspicious_broken_parts>
        <parts_to_throw_insert>3000</parts_to_throw_insert>
      </merge_tree>
      <default_database>mxsec</default_database>
    </yandex>

# 大规模 / 极限：多分片
clickhouse: { shards: 3, replicasPerShard: 2 }   # 6 节点 + ReplicatedReplacingMergeTree
```

### 9.5 Prometheus + 长期存储

```yaml
# 标准（本地 30d）
prometheus:
  server: { persistentVolume: { size: 200Gi }, retention: 30d }

# 中大规模（Thanos + 对象存储，本地短保留长期下沉）
prometheus:
  thanos:
    enabled: true
    sidecar:
      objectStorageConfig: { secretName: thanos-objstore, secretKey: objstore.yml }
  retention: 6h

# 极限（VictoriaMetrics Cluster 替代 Prometheus 集群）
victoriametrics:
  vmstorage: { replicaCount: 3, retentionPeriod: 12, persistentVolume: { size: 2Ti } }
  vminsert:  { replicaCount: 3 }
  vmselect:  { replicaCount: 3 }
```

### 9.6 Ollama / vLLM（离网 LLM，可选）

详见 §15 离网部署。

---

## 10. mxctl CLI（集群部署引擎）

`mxctl` 位于 `cmd/tools/mxctl/`，是裸金属 / VM 集群部署的核心工具，**helm chart 内部也调用 mxctl 做配置渲染与证书生成**。

### 10.1 构建

```bash
go build -o ./bin/mxctl ./cmd/tools/mxctl
```

### 10.2 子命令一览

| 子命令 | 用途 |
|--------|------|
| `check` / `validate` | 校验 `cluster.yaml`（语法 + 字段完整 + 节点角色约束）|
| `preflight` | 预部署检查（本地依赖 + SSH 连通 + 远端 OS / sudo / 目录可写）|
| `render` | 按 cluster.yaml 渲染每节点的 docker-compose / server.yaml / 证书 |
| `deploy` | 完整部署（render + scp 上传 + 按角色顺序启动 + 健康检查）|
| `upgrade` | 升级（备份 → 滚动重建 → 健康检查 → 失败自动回滚）|
| `rollback` | 回滚到指定 release |
| `cert rotate` | 轮换 mTLS 证书 |
| `tenant create` / `migrate` | 创建租户 / 迁移到 schema / db 隔离 |
| `region failover` | 跨 Region 切换 |
| `llmproxy rotate-keys` | 轮换 LLM 厂商 API Key |
| `helm install/upgrade` | 调用 helm 部署 K8s 形态 |

### 10.3 通用参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `-f` | `deploy/prod/cluster.example.yaml` | cluster.yaml 路径 |
| `-o` | `deploy/prod/out` | 渲染输出目录 |
| `--skip-install` | false | 跳过远端依赖安装 |
| `--skip-healthcheck` | false | 跳过部署后健康检查 |
| `--dry-run` | false | 仅打印执行计划，不实际操作 |

### 10.4 典型使用流程（裸金属）

```bash
# 1. 校验配置
./bin/mxctl check -f deploy/prod/my-cluster.yaml

# 2. 预检查
./bin/mxctl preflight -f deploy/prod/my-cluster.yaml

# 3. 仅渲染
./bin/mxctl render -f deploy/prod/my-cluster.yaml -o deploy/prod/out

# 4. 完整部署
./bin/mxctl deploy -f deploy/prod/my-cluster.yaml

# 5. 升级
./bin/mxctl upgrade -f deploy/prod/my-cluster.yaml --version v1.1.0

# 6. 回滚
./bin/mxctl rollback -f deploy/prod/my-cluster.yaml --to v1.0.0
```

### 10.5 deploy 执行顺序（5 步）

```
[1/5] 准备远端节点    -- 创建 release 目录、scp 上传 bundle、切换 current 软链、安装依赖、docker login
[2/5] 启动 Kafka      -- docker compose up kafka 节点
[3/5] 启动 Storage    -- docker compose up storage 节点（MySQL / Redis / ClickHouse / Prometheus）
[4/5] 启动 Control    -- docker compose up control 节点（Manager / AC / Consumer / Engine / VulnSync / LLMProxy）
[5/5] 健康检查        -- docker compose ps + curl /health + Kafka Topic 自检
```

每次部署生成独立 release 目录 `{install_dir}/releases/{version}-{timestamp}/`，通过 `current` 软链切换版本，支持秒级回滚。

---

## 11. helm chart 完整示例

### 11.1 标准生产 values（`values-standard.yaml`）

```yaml
global:
  imageRegistry: registry.example.com/mxsec
  imagePullSecrets: [mxsec-pull-secret]
  storageClass: fast-ssd
  timezone: Asia/Shanghai

mxsec:
  release: { version: v1.0.0 }
  mode: { default: observe }                # 默认监听，磨合达标后切 protect
  multiTenant: { enabled: true, defaultTenantId: t-default, isolationStrategy: shared }
  llm: { enabled: false }                   # 默认关闭 LLMProxy
  ml:  { enabled: true }                    # 本地 ONNX 默认开启

# 六微服务（标准配比 N=2，Engine 4C/4G，VulnSync 1 副本 + Leader Election）
manager:
  replicaCount: 2
  resources: { requests: { cpu: 1, memory: 1Gi }, limits: { cpu: 2, memory: 2Gi } }
  ingress:
    enabled: true
    className: nginx
    hosts: [{ host: security.example.com, paths: [{ path: /, pathType: Prefix }] }]
    tls:   [{ hosts: [security.example.com], secretName: mxsec-ui-tls }]
  config: { jwtSecret: ${MXSEC_JWT_SECRET}, logLevel: info, logFormat: json }
  podDisruptionBudget: { minAvailable: 1 }

agentcenter:
  replicaCount: 2
  resources: { requests: { cpu: 1, memory: 1Gi }, limits: { cpu: 2, memory: 2Gi } }
  service:
    type: LoadBalancer                      # 暴露 :6751 给外部 Agent
    annotations: { service.beta.kubernetes.io/aws-load-balancer-type: "nlb" }
  keepalive: { time: 60s, timeout: 10s, minTime: 10s }
  mTLS: { autoIssue: true, caSecret: mxsec-ca }

consumer:
  replicaCount: 2
  resources: { requests: { cpu: 1, memory: 1Gi }, limits: { cpu: 2, memory: 2Gi } }
  batch: { clickhouseBatchSize: 5000, clickhouseBatchInterval: 10s }
  dlq:   { enabled: true }

engine:
  replicaCount: 2
  resources: { requests: { cpu: 2, memory: 2Gi }, limits: { cpu: 4, memory: 4Gi } }
  modelsPVC: { enabled: true, size: 10Gi }
  detection:
    cel: { enabled: true }
    sequence: { enabled: true }
    ml: { enabled: true, runtime: onnx, cpuOnly: true }
    storyline: { enabled: true }
  modeDefault: observe

vulnsync:
  replicaCount: 1                           # 单副本 + Leader Election
  resources: { requests: { cpu: 1, memory: 1Gi }, limits: { cpu: 2, memory: 2Gi } }
  cache: { persistence: { size: 20Gi } }
  sources:
    nvd: { enabled: true }
    osv: { enabled: true }
    rhsa: { enabled: true }
    usn: { enabled: true }
    dsa: { enabled: true }
    alpine: { enabled: true }
    suse: { enabled: true }
    kev: { enabled: true }
    exploitdb: { enabled: true }
    cnnvd: { enabled: true }
    epss: { enabled: true }
    xinchuang: { openeuler: { enabled: true }, anolis: { enabled: true }, kylin: { enabled: true }, uos: { enabled: true } }
  schedule: { incremental: "0 */1 * * *", full: "0 3 * * *" }

llmproxy:
  enabled: false                            # 默认关闭，set true 才启用
  replicaCount: 2
  resources: { requests: { cpu: 0.5, memory: 256Mi }, limits: { cpu: 1, memory: 512Mi } }
  config: { airGapped: false, autoDowngradeOnOffline: true }
  providers:
    openai:    { enabled: false, secretName: llm-openai-key }
    dashscope: { enabled: false, secretName: llm-dashscope-key }
    deepseek:  { enabled: false, secretName: llm-deepseek-key }
    ollama:    { enabled: false, baseUrl: http://ollama.mxsec.svc:11434/v1 }

# 内置依赖（生产建议改用外部托管，见 §6.2）
mysql:      { enabled: true, architecture: replication, auth: { rootPassword: ${MYSQL_ROOT_PASSWORD}, database: mxsec, username: mxsec_user, password: ${MYSQL_PASSWORD} }, primary: { persistence: { size: 200Gi } }, secondary: { replicaCount: 1 } }
redis:      { enabled: true, architecture: replication, sentinel: { enabled: true }, master: { persistence: { size: 10Gi } }, replica: { replicaCount: 2 } }
kafka:      { enabled: true, controller: { replicaCount: 3 }, broker: { replicaCount: 3, persistence: { size: 300Gi } }, kraft: { enabled: true } }
clickhouse: { enabled: true, shards: 1, replicasPerShard: 2, persistence: { size: 500Gi }, zookeeper: { enabled: true, replicaCount: 3 } }
prometheus: { enabled: true, server: { persistentVolume: { size: 200Gi }, retention: 30d }, thanos: { enabled: false } }
```

### 11.2 大规模 values（外部依赖，`values-large.yaml`）

```yaml
mxsec: { llm: { enabled: true } }

manager:     { replicaCount: 6,  resources: { requests: { cpu: 2, memory: 2Gi } } }
agentcenter: { replicaCount: 10, resources: { requests: { cpu: 2, memory: 2Gi } } }
consumer:    { replicaCount: 8,  resources: { requests: { cpu: 2, memory: 2Gi } } }
engine:      { replicaCount: 10, resources: { requests: { cpu: 4, memory: 8Gi } } }
vulnsync:    { replicaCount: 1 }
llmproxy:    { enabled: true, replicaCount: 4 }

mysql:      { enabled: false, external: { host: rds-master.example.com, existingSecret: mysql-credentials } }
redis:      { enabled: false, external: { addr: redis-cluster.example.com:6379, existingSecret: redis-credentials } }
kafka:      { enabled: false, external: { brokers: [mq-1:9093, mq-2:9093, mq-3:9093], existingSecret: kafka-sasl } }
clickhouse: { enabled: false, external: { host: ck.example.com, existingSecret: ck-credentials } }
prometheus: { enabled: true, thanos: { enabled: true } }
```

### 11.3 安装与升级

```bash
helm install mxsec mxsec/mxsec-platform -n mxsec --create-namespace   -f values-standard.yaml --version v1.0.0

helm upgrade mxsec mxsec/mxsec-platform -n mxsec   -f values-standard.yaml --version v1.0.1 --atomic --timeout 10m

helm rollback mxsec 1 -n mxsec
```

`--atomic` 失败自动回滚，**生产升级必加**。

## 12. docker-compose 完整示例

`deploy/docker-compose.demo.yml` 节选关键服务（六微服务共享 `x-mxsec-env`，对依赖 `depends_on` 健康检查）：

```yaml
version: "3.9"

x-mxsec-env: &mxsec-env { TZ: Asia/Shanghai, LOG_LEVEL: info, LOG_FORMAT: json }

services:
  mysql:
    image: mysql:8.0
    restart: unless-stopped
    environment: { MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}, MYSQL_DATABASE: mxsec, MYSQL_USER: mxsec_user, MYSQL_PASSWORD: ${MYSQL_PASSWORD} }
    ports: ["13306:3306"]
    volumes: [mysql-data:/var/lib/mysql, ./config/mysql.cnf:/etc/mysql/conf.d/mxsec.cnf:ro]
    healthcheck: { test: ["CMD","mysqladmin","ping","-h","localhost","-uroot","-p${MYSQL_ROOT_PASSWORD}"], interval: 10s, timeout: 5s, retries: 10 }

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server","--requirepass","${REDIS_PASSWORD}","--appendonly","yes"]
    ports: ["16379:6379"]
    volumes: [redis-data:/data]
    healthcheck: { test: ["CMD","redis-cli","-a","${REDIS_PASSWORD}","ping"], interval: 10s, timeout: 5s, retries: 10 }

  kafka:
    image: bitnami/kafka:3.7
    restart: unless-stopped
    ports: ["9092:9092"]
    environment:
      KAFKA_CFG_NODE_ID: 1
      KAFKA_CFG_PROCESS_ROLES: controller,broker
      KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093
      KAFKA_CFG_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:9093
      KAFKA_CFG_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_CFG_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE: "false"
      KAFKA_CFG_LOG_RETENTION_HOURS: 72
    volumes: [kafka-data:/bitnami/kafka]

  clickhouse:
    image: clickhouse/clickhouse-server:24.3
    restart: unless-stopped
    ports: ["8123:8123","9000:9000"]
    environment: { CLICKHOUSE_DB: mxsec, CLICKHOUSE_PASSWORD: ${CLICKHOUSE_PASSWORD} }
    ulimits: { nofile: { soft: 262144, hard: 262144 } }
    volumes: [clickhouse-data:/var/lib/clickhouse]

  prometheus:
    image: prom/prometheus:v2.55.0
    restart: unless-stopped
    ports: ["9090:9090"]
    volumes: [./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro, prometheus-data:/prometheus]
    command: ["--config.file=/etc/prometheus/prometheus.yml","--storage.tsdb.retention.time=30d"]

  # 六微服务（mxsec-env + 配置挂载 + 健康依赖）
  manager:
    image: mxsec/manager:v1.0.0
    restart: unless-stopped
    depends_on: { mysql: { condition: service_healthy }, redis: { condition: service_healthy }, kafka: { condition: service_started } }
    environment: { <<: *mxsec-env, MXSEC_MODE_DEFAULT: observe, MXSEC_JWT_SECRET: ${JWT_SECRET}, MXSEC_TENANT_ISOLATION_STRATEGY: shared }
    volumes: [./config/server.yaml:/etc/mxsec/server.yaml:ro, ./certs:/etc/mxsec/certs:ro]
    ports: ["8080:8080"]

  agentcenter:
    image: mxsec/agentcenter:v1.0.0
    restart: unless-stopped
    depends_on: { redis: { condition: service_healthy }, kafka: { condition: service_started } }
    environment: { <<: *mxsec-env, MXSEC_MODE_DEFAULT: observe }
    volumes: [./config/server-ac.yaml:/etc/mxsec/server-ac.yaml:ro, ./certs:/etc/mxsec/certs:ro]
    ports: ["6751:6751","6752:6752"]                # 6751 mTLS Agent；6752 内网任务下发

  consumer:
    image: mxsec/consumer:v1.0.0
    restart: unless-stopped
    depends_on: { mysql: { condition: service_healthy }, clickhouse: { condition: service_started }, kafka: { condition: service_started } }
    environment: { <<: *mxsec-env }
    volumes: [./config/consumer.yaml:/etc/mxsec/consumer.yaml:ro]

  engine:
    image: mxsec/engine:v1.0.0
    restart: unless-stopped
    depends_on: { kafka: { condition: service_started }, redis: { condition: service_healthy }, manager: { condition: service_started } }
    environment: { <<: *mxsec-env, MXSEC_MODE_DEFAULT: observe, MXSEC_ML_ENABLED: "true" }
    volumes: [./config/engine.yaml:/etc/mxsec/engine.yaml:ro, ./models:/opt/mxsec/models:ro]

  vulnsync:
    image: mxsec/vulnsync:v1.0.0
    restart: unless-stopped
    depends_on: { redis: { condition: service_healthy }, kafka: { condition: service_started } }
    environment: { <<: *mxsec-env }
    volumes: [./config/vulnsync.yaml:/etc/mxsec/vulnsync.yaml:ro, vulnsync-cache:/var/lib/mxsec/vulnsync]

  llmproxy:
    image: mxsec/llmproxy:v1.0.0
    restart: unless-stopped
    profiles: ["llm"]                               # 默认不启动，加 --profile llm 才开
    depends_on: { redis: { condition: service_healthy }, kafka: { condition: service_started } }
    environment: { <<: *mxsec-env, OPENAI_API_KEY: ${OPENAI_API_KEY:-}, DASHSCOPE_API_KEY: ${DASHSCOPE_API_KEY:-}, DEEPSEEK_API_KEY: ${DEEPSEEK_API_KEY:-} }
    volumes: [./config/llmproxy.yaml:/etc/mxsec/llmproxy.yaml:ro, ./certs:/etc/mxsec/certs:ro]

  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    depends_on: [manager]
    ports: ["80:80","443:443"]
    volumes: [./config/nginx.conf:/etc/nginx/nginx.conf:ro, ./certs/server.crt:/etc/nginx/cert.pem:ro, ./certs/server.key:/etc/nginx/key.pem:ro]

volumes: { mysql-data: {}, redis-data: {}, kafka-data: {}, clickhouse-data: {}, prometheus-data: {}, vulnsync-cache: {} }
```

启动：

```bash
docker compose --env-file .env -f docker-compose.demo.yml up -d                  # 不带 LLMProxy
docker compose --env-file .env -f docker-compose.demo.yml --profile llm up -d    # 带 LLMProxy
```

## 13. K8s manifest 完整示例

> Helm chart 渲染后的 manifest 等价于本节内容；手工部署可参考。**示例只展示 Manager（其余 5 个微服务复用同结构，仅镜像名 / 端口 / configMap 不同）**。

### 13.1 ConfigMap + Secret

```yaml
apiVersion: v1
kind: ConfigMap
metadata: { name: mxsec-manager-config, namespace: mxsec }
data:
  server.yaml: |
    server: { listen: ":8080", mode: observe }       # 默认监听
    mysql: { host: mysql.mxsec.svc.cluster.local, port: 3306, database: mxsec, user: mxsec_user }
    redis: { addr: redis-master.mxsec.svc.cluster.local:6379 }
    kafka: { brokers: [kafka-0.kafka.mxsec.svc:9092, kafka-1.kafka.mxsec.svc:9092, kafka-2.kafka.mxsec.svc:9092] }
    tenant: { default_id: t-default, isolation_strategy: shared }
    llm:    { enabled: false, endpoint: llmproxy.mxsec.svc.cluster.local:18900 }
    mtls:   { ca: /etc/mxsec/certs/ca.crt, cert: /etc/mxsec/certs/manager.crt, key: /etc/mxsec/certs/manager.key }
---
apiVersion: v1
kind: Secret
metadata: { name: mxsec-credentials, namespace: mxsec }
type: Opaque
stringData:
  MYSQL_PASSWORD: ${MYSQL_PASSWORD}
  REDIS_PASSWORD: ${REDIS_PASSWORD}
  CLICKHOUSE_PASSWORD: ${CLICKHOUSE_PASSWORD}
  JWT_SECRET: ${JWT_SECRET}
  INTERNAL_BEARER: ${INTERNAL_BEARER}
```

### 13.2 Deployment（Manager 模板）

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: manager, namespace: mxsec }
spec:
  replicas: 2
  strategy: { type: RollingUpdate, rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } }
  selector: { matchLabels: { app: manager } }
  template:
    metadata: { labels: { app: manager } }
    spec:
      serviceAccountName: mxsec
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm: { labelSelector: { matchLabels: { app: manager } }, topologyKey: kubernetes.io/hostname }
      containers:
        - name: manager
          image: registry.example.com/mxsec/manager:v1.0.0
          ports: [{ name: http, containerPort: 8080 }, { name: metrics, containerPort: 9090 }]
          envFrom: [{ secretRef: { name: mxsec-credentials } }]
          volumeMounts:
            - { name: config, mountPath: /etc/mxsec, readOnly: true }
            - { name: certs,  mountPath: /etc/mxsec/certs, readOnly: true }
          resources: { requests: { cpu: "1", memory: "1Gi" }, limits: { cpu: "2", memory: "2Gi" } }
          readinessProbe: { httpGet: { path: /ready, port: http }, initialDelaySeconds: 5, periodSeconds: 10 }
          livenessProbe:  { httpGet: { path: /health, port: http }, initialDelaySeconds: 30, periodSeconds: 30 }
      volumes:
        - { name: config, configMap: { name: mxsec-manager-config } }
        - { name: certs,  secret: { secretName: mxsec-tls } }
```

### 13.3 Service + Ingress + AgentCenter LoadBalancer

```yaml
apiVersion: v1
kind: Service
metadata: { name: manager, namespace: mxsec }
spec:
  type: ClusterIP
  selector: { app: manager }
  ports: [{ name: http, port: 8080, targetPort: http }, { name: metrics, port: 9090, targetPort: metrics }]
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mxsec-ui
  namespace: mxsec
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/proxy-body-size: "100m"
spec:
  ingressClassName: nginx
  tls:   [{ hosts: [security.example.com], secretName: mxsec-ui-tls }]
  rules: [{ host: security.example.com, http: { paths: [{ path: /, pathType: Prefix, backend: { service: { name: manager, port: { number: 8080 } } } }] } }]
---
# AgentCenter L4 LoadBalancer 直通 6751
apiVersion: v1
kind: Service
metadata:
  name: agentcenter-grpc
  namespace: mxsec
  annotations: { service.beta.kubernetes.io/aws-load-balancer-type: "nlb", service.beta.kubernetes.io/aliyun-loadbalancer-spec: "slb.s2.small" }
spec:
  type: LoadBalancer
  externalTrafficPolicy: Local
  selector: { app: agentcenter }
  ports: [{ name: grpc, port: 6751, targetPort: 6751, protocol: TCP }]
```

### 13.4 StatefulSet（VulnSync，单副本 + PVC）

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: vulnsync, namespace: mxsec }
spec:
  serviceName: vulnsync
  replicas: 1                                          # Leader Election，禁止 > 1
  selector: { matchLabels: { app: vulnsync } }
  template:
    metadata: { labels: { app: vulnsync } }
    spec:
      containers:
        - name: vulnsync
          image: registry.example.com/mxsec/vulnsync:v1.0.0
          ports: [{ name: grpc, containerPort: 18100 }, { name: metrics, containerPort: 9102 }]
          envFrom: [{ secretRef: { name: mxsec-credentials } }]
          volumeMounts: [{ name: cache, mountPath: /var/lib/mxsec/vulnsync }, { name: config, mountPath: /etc/mxsec, readOnly: true }]
          resources: { requests: { cpu: "1", memory: "1Gi" }, limits: { cpu: "2", memory: "2Gi" } }
      volumes: [{ name: config, configMap: { name: mxsec-vulnsync-config } }]
  volumeClaimTemplates:
    - metadata: { name: cache }
      spec: { accessModes: [ReadWriteOnce], storageClassName: fast-ssd, resources: { requests: { storage: 20Gi } } }
```

### 13.5 PVC / HPA / PDB / NetworkPolicy

```yaml
# Engine 模型 PVC（ReadOnlyMany）
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: engine-models, namespace: mxsec }
spec: { accessModes: [ReadOnlyMany], storageClassName: standard, resources: { requests: { storage: 10Gi } } }
---
# Engine HPA：CPU + Kafka lag 双指标
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: { name: engine, namespace: mxsec }
spec:
  scaleTargetRef: { apiVersion: apps/v1, kind: Deployment, name: engine }
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource: { name: cpu, target: { type: Utilization, averageUtilization: 70 } }
    - type: Pods
      pods: { metric: { name: kafka_consumer_lag }, target: { type: AverageValue, averageValue: "5000" } }
---
# PDB：升级/驱逐保护
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: { name: manager, namespace: mxsec }
spec: { minAvailable: 1, selector: { matchLabels: { app: manager } } }
---
# NetworkPolicy：默认拒绝 + 仅放行 mxsec / ingress-nginx + DNS
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: mxsec-default-deny, namespace: mxsec }
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
  ingress:
    - from: [{ namespaceSelector: { matchLabels: { name: mxsec } } }, { namespaceSelector: { matchLabels: { name: ingress-nginx } } }]
  egress:
    - to: [{ namespaceSelector: { matchLabels: { name: mxsec } } }]
    - to: [{ namespaceSelector: { matchLabels: { name: kube-system } } }]
      ports: [{ port: 53, protocol: UDP }]
```

## 14. 多租户部署形态

> 完整设计见 [`multi-tenant.md`](multi-tenant.md) §6 物理隔离。本节按"如何部署"展开。

### 14.1 三档隔离策略对比

| 策略 | `tenants.isolation_strategy` | DB | Kafka | Helm 配置 | 适用 |
|------|------------------------------|-----|-------|-----------|------|
| Shared（默认） | `shared` | 共库共表 + tenant_id | 共享 Topic + Key=`{tenant}:{agent}` | 单 release，默认 | 中小客户 / 互联网 |
| Schema | `schema` | 同 MySQL 实例独立 schema（`mxsec_t_bank_a.hosts`） | 独立 Topic `mxsec.{tenant}.agent.*` | 单 release + 多 schema 自动建 | 中型政企 |
| Dedicated DB | `db` | 独立 MySQL / CK 实例 | 独立 Topic（可独立 Kafka 集群）| **每租户独立 helm release** | 金融 KA / 监管 |

### 14.2 Shared（默认）

```yaml
mxsec:
  multiTenant:
    enabled: true
    defaultTenantId: t-default
    isolationStrategy: shared
```

无额外动作。`mxctl tenant create --id t-bank-a --name "银行 A"` 即可创建新租户。

### 14.3 Schema

```yaml
mxsec:
  multiTenant:
    isolationStrategy: schema
    schemas:
      - tenantId: t-bank-a
        mysqlSchema: mxsec_t_bank_a
        kafkaTopicPrefix: mxsec.t_bank_a
      - tenantId: t-bank-b
        mysqlSchema: mxsec_t_bank_b
        kafkaTopicPrefix: mxsec.t_bank_b
```

Helm post-install hook 自动执行：

```sql
CREATE DATABASE IF NOT EXISTS mxsec_t_bank_a CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT ALL ON mxsec_t_bank_a.* TO 'mxsec_user'@'%';
```

Kafka Topic：`mxsec.t_bank_a.agent.heartbeat` / `.ebpf` / `.events` ... 自动创建。

### 14.4 Dedicated DB

每个 KA 租户**独立 helm release**：

```bash
helm install mxsec-bank-a mxsec/mxsec-platform \
  -n mxsec-bank-a --create-namespace \
  -f values-bank-a.yaml \
  --set mxsec.multiTenant.isolationStrategy=db \
  --set mysql.external.host=rds-bank-a.example.com \
  --set kafka.external.brokers="{mq-bank-a-1:9093,mq-bank-a-2:9093,mq-bank-a-3:9093}"

helm install mxsec-bank-b mxsec/mxsec-platform \
  -n mxsec-bank-b --create-namespace \
  -f values-bank-b.yaml \
  --set mxsec.multiTenant.isolationStrategy=db \
  ...
```

控制面联邦：通过中央 `mxsec-federation` chart 注册各租户 release 入口，统一在 SystemAdmin UI 列出。

### 14.5 租户迁移 `shared → schema → db`

```bash
mxctl tenant migrate prepare --id t-bank-a --target schema     # 1. 创建目标 schema / DB
mxctl tenant pause           --id t-bank-a                     # 2. 暂停写入（API 503）
mxctl tenant migrate exec    --id t-bank-a --target schema     # 3. dump+restore（按 tenant_id 过滤）
mxctl tenant migrate switch  --id t-bank-a --target schema     # 4. 切换 Manager 路由
mxctl tenant resume          --id t-bank-a                     # 5. 恢复写入
mxctl tenant migrate verify  --id t-bank-a                     # 6. 验证一致性
mxctl tenant migrate cleanup --id t-bank-a --confirm           # 7. 30d 后清理源 schema
```

## 15. 离网部署（含 LLM 离网 + 镜像私有仓库）

适用：信创 / 金融 / 政务等**绝对不允许出网**的客户环境。

### 15.1 镜像私有仓库

中转机一次性拉取并打包，客户内网 load 到自有 Harbor / Nexus / Quay：

```bash
# 1. 中转机拉取（6 个微服务 + 6 个依赖）
for i in manager agentcenter consumer engine vulnsync llmproxy; do docker pull mxsec/$i:v1.0.0; done
docker pull bitnami/kafka:3.7 mysql:8.0 redis:7-alpine             clickhouse/clickhouse-server:24.3 prom/prometheus:v2.55.0 ollama/ollama:latest

# 2. 打包
docker save -o mxsec-images-v1.0.0.tar $(docker images mxsec/* -q)             bitnami/kafka:3.7 mysql:8.0 redis:7-alpine             clickhouse/clickhouse-server:24.3 prom/prometheus:v2.55.0 ollama/ollama:latest

# 3. 客户内网导入
docker load -i mxsec-images-v1.0.0.tar
for i in manager agentcenter consumer engine vulnsync llmproxy; do
  docker tag mxsec/$i:v1.0.0 harbor.internal/mxsec/$i:v1.0.0
  docker push harbor.internal/mxsec/$i:v1.0.0
done
```

helm 覆盖 `values-airgap.yaml`：

```yaml
global: { imageRegistry: harbor.internal/mxsec, imagePullSecrets: [harbor-pull-secret] }
```

### 15.2 漏洞情报离线包

VulnSync 11 源默认全部出网，离网环境通过**离线情报包**更新（中转机每日拉取 → 内网导入）：

```bash
# 中转机
mxctl vulnsync export --output mxsec-vulns-2026-06-06.tar.gz
# 客户内网
mxctl vulnsync import --input mxsec-vulns-2026-06-06.tar.gz
```

离线包包含：NVD JSON / OSV PURL 索引 / RHSA / USN / DSA / Alpine / SUSE / 信创 4 源 / EPSS 全量。

### 15.3 LLM 离网（Ollama / vLLM）

```yaml
llmproxy:
  enabled: true
  config: { airGapped: true }                  # 永不出网
  providers:
    ollama: { enabled: true, baseUrl: http://ollama.mxsec.svc.cluster.local:11434/v1 }
  scenes:
    alert_explain:     { primary: { provider: ollama, model: qwen2.5:7b  } }
    storyline_summary: { primary: { provider: ollama, model: qwen2.5:14b } }
    nl2query:          { primary: { provider: ollama, model: qwen2.5:7b  } }
    rule_draft:        { primary: { provider: ollama, model: qwen2.5:14b } }

ollama:
  enabled: true
  replicaCount: 1
  resources: { requests: { cpu: 4, memory: 8Gi }, limits: { cpu: 8, memory: 16Gi } }
  persistence: { size: 50Gi }
  models: [qwen2.5:7b, qwen2.5:14b, bge-m3]   # init container 预拉

# GPU 加速（可选）：ollama.resources.limits.nvidia.com/gpu=1, memory=32Gi
# 高吞吐（可选）：vLLM + AWQ 量化
vllm:
  enabled: false
  image: vllm/vllm-openai:latest
  args: ["--model", "Qwen/Qwen2.5-14B-Instruct-AWQ", "--max-model-len", "32768"]
  resources: { limits: { nvidia.com/gpu: 1, memory: 32Gi } }
```

### 15.4 Agent 包离线分发

```bash
# 中转机构建（amd64 + arm64 同出）
make package-agent-all VERSION=1.0.0 SERVER_HOST=ac.internal.example.com:6751
# 输出: dist/packages/mxsec-agent-1.0.0.{x86_64,aarch64}.rpm / mxsec-agent_1.0.0_{amd64,arm64}.deb
# 客户内网通过 Ansible / SaltStack / 自有 yum 仓库分发
```

## 16. 信创 OS 部署适配

支持矩阵（社区 + 商业测试覆盖）：

| OS | 版本 | Agent | 服务端 | 备注 |
|----|------|-------|--------|------|
| openEuler | 22.03 LTS / 24.03 LTS | ✅ | ✅ | 内核 ≥ 5.10，eBPF 完整 |
| 龙蜥 Anolis OS | 8.x / 23 | ✅ | ✅ | 兼容 RHEL 生态 |
| 麒麟 KylinOS | V10 SP3 | ✅ | ✅ | aarch64 / x86_64，需 sm2/sm4 |
| 统信 UOS | 1060 / 20 | ✅ | ✅ | 桌面与服务器版均支持 |
| 中科方德 NFS | NeoKylin Server 7.6 | ✅ | ⚠️ | Agent 全功能，服务端建议容器化 |
| 红旗 Asianux | 7 / 8 | ✅ | ✅ | RHEL 生态 |

关键适配点：

| 适配 | 实现 |
|------|------|
| CPU 架构 | x86_64 / aarch64 双架构镜像（`make package-all-arch`）|
| 国密 | TLS 链路可切 sm2 / sm4（`tjfoc/gmsm` 集成，编译标签 `-tags=gmsm`）|
| 信创漏洞库 | VulnSync 默认开启 openEuler CSA / Anolis ANSA / Kylin KYSA / UOS UOSEC 4 源 |
| systemd | RPM/DEB 自带 `mxsec-agent.service`，兼容 systemd 219+ |
| 容器运行时 | Docker / containerd / iSulad（华为 / 信创自有运行时） |
| K8s | KubeOperator / KubeSphere / 华为 CCE 国产化版 |

`values-xinchuang.yaml`：

```yaml
global:
  imageRegistry: harbor.gov.example.com/mxsec   # 信创私有 Harbor
  imageArchitecture: arm64                       # 多数信创主机为 aarch64
mxsec: { release: { version: v1.0.0-gmsm } }    # 国密编译版本
manager:     { image: { repository: manager-arm64 } }
agentcenter: { image: { repository: agentcenter-arm64 }, mTLS: { cipherSuites: [sm4-gcm] } }   # 国密 TLS
vulnsync:
  sources:
    xinchuang: { openeuler: { enabled: true }, anolis: { enabled: true }, kylin: { enabled: true }, uos: { enabled: true } }
```

## 17. 证书生成与轮换

证书目录 `/etc/mxsec/certs/`：`ca.crt + ca.key`（CA，仅 Manager 持有 key）+ 每个微服务 `{component}.crt / .key`（manager / ac / engine / vulnsync / llmproxy / consumer）+ 模板 `agent.crt / .key`（首次下发用）。

```bash
# 一键自签
make certs
# 等价：
./scripts/generate-certs.sh   --ca-cn "mxsec-ca" --validity-days 3650   --san-ips "10.0.0.10,10.0.0.11,10.0.0.12"   --san-dns "security.example.com,ac.internal.example.com"

# mxctl 运维
mxctl cert check       -f deploy/prod/cluster.yaml                                # 剩余有效期
mxctl cert rotate      -f deploy/prod/cluster.yaml --service agentcenter          # 热重载组件证书
mxctl cert rotate-ca   -f deploy/prod/cluster.yaml --window 2026-06-10T02:00      # CA 轮换（停服窗口）
```

**Agent 证书首次下发**：Agent 首次连接 AC 时 mTLS 不严格校验（`VerifyClientCertIfGiven`），AC 验证 Agent 注册码 → 签发独立 `agent.crt/key` → 后续连接严格 mTLS。

**K8s cert-manager 自动续签**：

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: { name: mxsec-tls, namespace: mxsec }
spec:
  secretName: mxsec-tls
  duration: 2160h                                # 90d
  renewBefore: 720h                              # 30d 前续签
  issuerRef: { name: mxsec-ca, kind: ClusterIssuer }
  commonName: mxsec.internal
  dnsNames: ["*.mxsec.svc.cluster.local", "security.example.com", "ac.internal.example.com"]
```

## 18. 升级与回滚

### 18.1 升级原则

- **滚动升级**：`maxUnavailable: 0`，控制面始终 N-1 在线
- **数据库迁移先行**：Schema 变更先单独 migration job 跑，应用滚动跟进
- **配置热更新**：`tenant_configs` / 规则可热更，无需重启
- **禁止跨 minor**：v1.0.0 → v1.1.0 → v1.2.0，不允许 v1.0 → v1.2
- **Agent 灰度**：Canary 5% → 25% → 100% 三步必走，见 [`operating-modes.md`](operating-modes.md) §5

### 18.2 标准升级（helm）

```bash
mxctl backup snapshot --tag pre-v1.1.0                                                     # 1. 备份
helm repo update                                                                            # 2. 拉新 chart
helm upgrade mxsec mxsec/mxsec-platform -n mxsec -f values-standard.yaml   --version v1.1.0 --dry-run --debug | grep -A20 "Job/mxsec-migration"                    # 3. 校验迁移
helm upgrade mxsec mxsec/mxsec-platform -n mxsec -f values-standard.yaml   --version v1.1.0 --atomic --timeout 15m                                                  # 4. 升级（atomic 失败自动回滚）
kubectl -n mxsec rollout status deploy/manager deploy/agentcenter deploy/engine            # 5. 验证
mxctl health -f deploy/prod/cluster.yaml
```

### 18.3 回滚

```bash
helm rollback mxsec <revision> -n mxsec --wait --timeout 10m
mxctl rollback -f deploy/prod/cluster.yaml --to v1.0.0
```

**数据库回滚**：down migration **必须配套上线**，否则 schema 不兼容时只能恢复备份。

### 18.4 Agent 灰度升级流程

```
[UI] 选择 v1.1.0 → [Manager] 创建 CanaryRollout → [Engine] 调度 5% → 24h 观察 → 25% → 24h → 100%
                                                       ↓
                                                  [AC] 下发升级命令 → [Agent] 下载 + 校验签名 + 重启
                                                       ↓
失败阈值（≥ 5% Agent 升级后 24h 内告警率 > 基线 2x）→ 自动回滚
```

实现：`internal/server/agentcenter/scheduler/canary_scheduler.go`。

## 19. 备份与恢复

### 19.1 RPO / RTO 目标

| 形态 | RPO | RTO |
|------|-----|-----|
| Demo | ≤ 24h | ≤ 4h |
| 标准 | ≤ 1h | ≤ 30min |
| 大规模 | ≤ 15min | ≤ 15min |
| KA 双中心 | ≤ 15min | ≤ 30min |

### 19.2 备份矩阵

| 数据 | 工具 | 频次 | 异地 | 默认保留 |
|------|------|------|------|---------|
| MySQL | XtraBackup（全量）+ binlog（增量）| 全量 1/d、binlog 实时 | OSS / S3 | 30d 全量 + 7d binlog |
| ClickHouse | `BACKUP DATABASE` → S3 | 全量 1/d | 跨 Region 复制 | 14d |
| Redis | RDB + AOF | RDB 1/h、AOF 实时 | OSS | 7d |
| Kafka | MirrorMaker 2 异地 | 实时 | 异地 Kafka | 实时 |
| 配置 / 证书 | git + KMS 加密 | 实时 | git 远端 | 永久 |
| 规则 / 策略 | `mxctl rules export` | 按变更 | OSS | 90d |
| 漏洞情报 | VulnSync 缓存 + 离线包 | 随 cron | OSS | 30d |
| 异地副本（所有） | — | — | — | 90d |

### 19.3 mxctl 备份命令

```bash
mxctl backup snapshot -f deploy/prod/cluster.yaml --tag manual-2026-06-06
mxctl backup list     -f deploy/prod/cluster.yaml
mxctl backup restore  -f deploy/prod/cluster.yaml --tag manual-2026-06-06
mxctl backup verify   -f deploy/prod/cluster.yaml --tag manual-2026-06-06    # 试还原到沙箱
```

### 19.4 演练

**每 30 天**必演练 1 次：沙箱恢复最新备份 → 验证 Agent 心跳完整、告警时间线连续、漏洞库版本一致 → 记录入 `audit_log`。

## 20. 健康检查与故障排查

### 20.1 一键健康检查

```bash
mxctl health -f deploy/prod/cluster.yaml
# 输出示例（每行一项）：
# [OK]   manager       (2/2 ready, P95 12ms)
# [OK]   agentcenter   (2/2 ready, 1842 agents connected)
# [OK]   consumer      (2/2 ready, lag 234)
# [OK]   engine        (2/2 ready, 12 rules loaded, 3 ML models)
# [OK]   vulnsync      (1/1 leader, last sync 12min ago)
# [SKIP] llmproxy      (disabled)
# [OK]   mysql / redis / kafka / clickhouse / prometheus  ...
```

### 20.2 关键端点期望

| 服务 | 端点 | 期望 |
|------|------|------|
| Manager | `GET /health` `/ready` `/metrics` `/api/v2/system/mode` | 200；`{"default":"observe"}` |
| AgentCenter | `GET /health` `/metrics` | 200 |
| Consumer | `GET /metrics` | Kafka lag < 10000 |
| Engine | `GET /metrics` | `mxsec_engine_alerts_total` rate > 0 |
| VulnSync | `GET /metrics` | `mxsec_vulnsync_last_sync_ts` ≥ now - 1h |
| LLMProxy | `GET /healthz` `/metrics` | `mxsec_llm_provider_healthy` ≥ 1 |

### 20.3 故障排查 checklist

| 症状 | 关键排查命令 |
|------|--------------|
| **Agent 无法连接 AC** | `nc -zv <AC_LB_IP> 6751`；`openssl s_client -connect <AC_LB_IP>:6751 -CAfile /var/lib/mxsec-agent/certs/ca.crt`；`journalctl -u mxsec-agent -f`；`curl -H "Authorization: Bearer $T" /api/v2/discovery/agentcenter` |
| **Kafka Consumer Lag 持续增长** | `kafka-consumer-groups.sh --describe --group mxsec-writers`；扩 Consumer 副本（≤ 分区数）；检查 MySQL 慢查询 / CK `system.merges` 积压；检查 DLQ 是否爆 |
| **Engine 不出告警** | 规则加载：`/api/v2/engine/rules`；CG `mxsec-engine` 在消费；模型加载日志含 `onnx` 关键字；`/api/v2/system/mode` 未被错误覆盖 |
| **LLMProxy 配额超限** | Redis `mxsec:llm:tenant:cost:{tenant}:{yyyymm}`；`/api/v2/llm/health`；Prom `mxsec_llm_fallback_total{tenant=...}` |
| **VulnSync Leader 未选出** | Redis `GET mxsec:vulnsync:lock`；确认 StatefulSet `replicas: 1`（多副本会互锁） |
| **mTLS 失败** | 检查证书剩余有效期：`mxctl cert check`；CA 是否轮换；hostname SAN 是否一致 |
| **Ingress 502** | Manager Pod 是否 ready；Ingress 后端服务端口是否 8080；NetworkPolicy 是否放行 ingress-nginx 命名空间 |

### 20.4 日志收集

所有微服务**结构化 JSON 日志**输出 stdout，由 Fluent Bit / Vector 采集到 ClickHouse `mxsec_logs.container_logs`，按 7d / 30d 分级保留。

## 21. 网络与端口

| 端口 | 协议 | 方向 | 用途 | 暴露 |
|------|------|------|------|------|
| 80 / 443 | HTTP/S | 用户 → Ingress | UI + API | **公网** |
| 6751 | gRPC mTLS | Agent → AC | 数据面接入 | **L4 LB 公网** |
| 8080 | HTTP | 集群内 | Manager API | ClusterIP |
| 6752 | HTTP | Manager → AC | 任务下发 | ClusterIP |
| 18000 / 18100 / 18900 | gRPC mTLS | 内部 | Manager / Engine ↔ Engine / VulnSync / LLMProxy | ClusterIP |
| 18901 / 9090 / 9100-9102 | HTTP | 运维 | `/healthz /metrics` + Prometheus | ClusterIP |
| 3306 / 6379 / 8123 / 9000 / 9092-9093 / 11434 | TCP / HTTP | 集群内 | MySQL / Redis / CK / Kafka / Ollama | ClusterIP |

**防火墙规则**：仅 80 / 443 + 6751 对外开放；所有依赖端口（MySQL / Redis / Kafka / CK / Prometheus）**严禁**对外暴露。

**公网 LLM 模式出网域名**（离网 `air_gapped=true` 时全部禁用）：
- LLM：`api.openai.com` / `api.anthropic.com` / `generativelanguage.googleapis.com` / `dashscope.aliyuncs.com` / `api.deepseek.com` / `api.moonshot.cn` / `open.bigmodel.cn` / `ark.cn-beijing.volces.com`
- 漏洞情报：`services.nvd.nist.gov` / `api.osv.dev` / `access.redhat.com` / `ubuntu.com` / `security-tracker.debian.org` / `secdb.alpinelinux.org` / `ftp.suse.com` / `www.cisa.gov` / `gitlab.com/exploit-database` / `epss.cyentia.com`

## 22. 平台支持矩阵

> **mxsec 不支持 Windows / macOS**。专精 Linux 主机 + Kubernetes 容器，符合工业级 CWPP 定位。

### 22.1 服务端 OS

商业支持：Rocky / AlmaLinux / Oracle Linux 8、9；Ubuntu Server 20.04 / 22.04 / 24.04。
社区支持：CentOS Stream 9；Debian 11、12。
信创支持：openEuler 22.03 LTS / 24.03 LTS、Anolis OS 8 / 23、KylinOS V10 SP3、UOS 1060 / 20。

### 22.2 Agent OS

| 发行版 | 版本 | EDR (eBPF) | 基线 / 漏洞 / 修复 |
|--------|------|------------|---------------------|
| Rocky / AlmaLinux / Oracle Linux | 8, 9 | ✅ | ✅ |
| CentOS | 7 | ⚠️ kprobe 降级 | ✅ |
| CentOS | 8, Stream 9 | ✅ | ✅ |
| Ubuntu | 20.04, 22.04, 24.04 | ✅ | ✅ |
| Debian | 10, 11, 12 | ✅ | ✅ |
| Alpine | 3.18+ | ⚠️ musl | ✅ |
| Amazon Linux | 2, 2023 | ✅ | ✅ |
| openEuler | 22.03+ | ✅ | ✅（含 CSA / 信创 4 源漏洞库）|
| Anolis / Kylin / UOS | LTS | ✅ | ✅（含信创 4 源漏洞库）|

### 22.3 K8s / 容器运行时 / 内核

| 维度 | 支持 |
|------|------|
| K8s | 1.27 / 1.28 / 1.29 / **1.30 推荐** / 1.31；1.32 Beta |
| 运行时 | Docker ≥ 24、containerd ≥ 1.7、CRI-O ≥ 1.28、iSula（信创）|
| 内核（EDR eBPF）| ≥ 4.18，推荐 5.10+；< 4.18 自动降级 `kprobe + auditd`，能力下降约 30% |
| 内核（Tetragon）| ≥ 5.10，推荐 5.15+ |

## 23. 参考文档

| 主题 | 文档 |
|------|------|
| 架构总图 | [`architecture.md`](architecture.md) |
| 运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 本地 ML 模型 | [`ml-models.md`](ml-models.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| 总体评估 | `ref/00-总体评估与商业化路线.md` |
| 服务端架构差距 | `ref/01-服务端架构.md` §6 MVP |
| 路线图 | `ref/08-roadmap.md` |
