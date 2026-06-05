# DataType 分配表 v2

**最后更新**: 2026-06-06 | **维护者**: Kerbos | **配套版本**: mxsec v2.0

> **强制规则**：新增任何 DataType 前必须先在本文档注册，确认无冲突再写代码。违反规则会导致消息被错误路由（静默丢弃 / 落入错误 Topic / 被 Plugin SDK 吞掉），排查成本极高。
>
> **配套源**：本文档与 [`architecture.md`](architecture.md) §4 Kafka 拓扑、[`multi-tenant.md`](multi-tenant.md) §5 Kafka 多租户 必须保持一致。任意一方变更必须三方同步。
>
> **平台定位提醒**：mxsec 是 **工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**，采用 **六微服务** 架构（Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy），默认 **监听模式（observe）** 部署，磨合达标后切防护模式。所有 DataType 设计必须服务这一定位。

---

## 0. 版本说明

| 版本 | 时间 | 范围 | 备注 |
|------|------|------|------|
| v1.0 | 2026-05-20 | 8 个 `mxsec.agent.*` Topic / DataType 1000-9999 | 三层架构（Manager / AC / Consumer），无多租户 |
| v2.0 | 2026-06-06 | **本文档** — 新增 6 个 Topic（engine x3 / vuln / llm / metering），DataType 11000-14099；多租户 Key 改 `{tenant_id}:{agent_id}` | 六微服务，from-day-1 多租户 |

v2.0 关键变化：

- **新增 6 个 Topic**：`mxsec.engine.alert` / `mxsec.engine.storyline` / `mxsec.engine.feedback` / `mxsec.vuln.advisory` / `mxsec.llm.audit` / `mxsec.metering.usage`
- **DataType 区段扩展**：在 9999 之后开放 11000-19999 共 9000 个号段，明确划分 Engine / Vuln / LLM / Metering 四大新模块
- **Partition Key 升级**：所有共享 Topic 的 Key 由 `agent_id` 改为 `{tenant_id}:{agent_id}`（详见 §3.1）
- **DLQ 全覆盖**：6 个新 Topic 同步建 `{topic}.dlq`
- **ConsumerGroup 重排**：从 1 个 `mxsec-consumer` 拆为 3 个（`mxsec-writers` / `mxsec-engine` / `mxsec-vulnsync`），同源不互锁

---

## 1. DataType 区段总览

```
0000        ───── 保留（禁止使用）
1000-1099   ───── 心跳 / 健康检查                            [agent → server]
2000-2999   ───── 保留（未来 Agent 扩展）
3000-3099   ───── eBPF 运行时事件 (EDR 内置)                 [agent → server]
4000-4999   ───── 保留
5050-5099   ───── 资产采集 (5050-5060 已用 / 5061-5099 预留) [agent → server]
6000-6099   ───── FIM 文件完整性                              [agent → server]
7000-7099   ───── 恶意文件扫描                                [双向]
8000-8099   ───── 基线合规检查                                [agent → server]
9000-9001   ───── 插件 SDK 内部心跳（禁止业务使用）            [agent ↔ plugin]
9100-9299   ───── 漏洞修复                                    [双向]
9300-9399   ───── 威胁情报 IOC 分发（gRPC Task，不走 Kafka）  [server → agent]
9400-9499   ───── 规则分发（gRPC Task，不走 Kafka）           [server → agent]
9500-9899   ───── 保留（未来安全模块）
9900-9999   ───── 控制指令 / 命令回包                          [双向]

10000-10999 ───── 保留（v2.x 内部缓冲区）
11001-11099 ───── Engine 告警  (NEW)                          [engine → server]
11100-11199 ───── Engine Storyline 攻击链 (NEW)               [engine → server]
11200-11899 ───── 保留（Engine 子模块扩展）
11900-11999 ───── Engine 反馈（误报标记，UI → Engine）(NEW)
12001-12099 ───── VulnSync advisory 推送 (NEW)                [vulnsync → server]
12100-12999 ───── 保留（VulnSync 子模块扩展）
13001-13099 ───── LLMProxy 调用审计 (NEW)                     [llmproxy → server]
13100-13999 ───── 保留（LLM 多场景扩展）
14001-14099 ───── 计量用量 Metering (NEW)                     [manager/llmproxy → server]
14100-19999 ───── 保留（v3.x 战略扩展）

20000+      ───── 保留（KA / OEM 私有号段，需平台 RBAC 审批）
```

> **未注册号段** = 路由"兜底" → 默认入 `mxsec.agent.heartbeat` → Consumer 静默丢弃。这是历史 9200 事故的根因（见 §11），任何新 DataType 必须同步更新本文档 + `RouteDataType()` + Consumer `handleMessage()`。

---

## 2. Topic 现状（v1.x 沿用，8 个）

### 2.1 `mxsec.agent.heartbeat` — 心跳 / 健康检查

| 维度 | 配置 |
|------|------|
| DataType | 1000, 1001 |
| Partitions | 6 |
| Retention | 24h |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Agent / Plugin SDK |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（agent 在线表）+ ClickHouse（指标）+ Redis（`agent:ac:{id}`） |
| DLQ | `mxsec.agent.heartbeat.dlq` (3 分区 / 30d) |

| DataType | 方向 | 说明 | 生产者 | 消费者 |
|----------|------|------|--------|--------|
| 1000 | Agent → Server | Agent 主心跳（在线 + 系统指标） | Agent | Consumer → MySQL + CK + Redis |
| 1001 | Plugin → Server | 插件心跳（CPU / RSS / FD / Goroutine） | Plugin SDK | Consumer → ClickHouse（不入 MySQL） |
| 1002-1099 | - | **未分配** | - | - |

### 2.2 `mxsec.agent.ebpf` — EDR 内核事件

| 维度 | 配置 |
|------|------|
| DataType | 3000-3099 |
| Partitions | 12 |
| Retention | 3d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Agent EDR 引擎（内置，不再独立 plugin） |
| ConsumerGroup | `mxsec-writers` + `mxsec-engine`（同源不互锁） |
| 写入存储 | ClickHouse（事件归档） + Engine CEL / 序列 / ML 检测 |
| DLQ | `mxsec.agent.ebpf.dlq` (6 分区 / 30d) |

| DataType | 方向 | 说明 | 消费者用途 |
|----------|------|------|----------|
| 3000 | Agent → Server | 进程事件 (exec/exit) | CK + CEL / 序列检测 / ATT&CK 映射 |
| 3001 | Agent → Server | 文件事件 (open/write/rename/unlink/chmod) | CK + CEL |
| 3002 | Agent → Server | 网络事件 (tcp_connect/accept/close, udp_send) | CK + CEL + 端口扫描滑窗 |
| 3003 | Agent → Server | DNS 查询事件 (dns_query) | CK + IOC 比对 |
| 3004 | Agent → Server | 内存威胁事件 (memfd_exec/deleted_exe/anonymous_exec) | MySQL + CEL（持久化高价值） |
| 3010 | Agent → Server | BDE 行为画像快照 | Consumer → 行为基线引擎 |
| 3005-3009, 3011-3099 | - | **未分配** | - |

### 2.3 `mxsec.agent.asset` — 资产采集

| 维度 | 配置 |
|------|------|
| DataType | 5050-5099 |
| Partitions | 6 |
| Retention | 7d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Collector 插件 |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（资产主表） |
| DLQ | `mxsec.agent.asset.dlq` (3 分区 / 30d) |

| DataType | 方向 | 说明 |
|----------|------|------|
| 5050 | Plugin → Server | 进程列表 |
| 5051 | Plugin → Server | 端口 / 网络监听 |
| 5052 | Plugin → Server | 用户账户 |
| 5053 | Plugin → Server | 软件包 (rpm / deb / pip / npm) |
| 5054 | Plugin → Server | 容器（Docker / containerd） |
| 5055 | Plugin → Server | 应用程序 |
| 5056 | Plugin → Server | 网卡 |
| 5057 | Plugin → Server | 磁盘 / 卷 |
| 5058 | Plugin → Server | 内核模块 (kmod) |
| 5059 | Plugin → Server | 系统服务 |
| 5060 | Plugin → Server | 定时任务 (cron / systemd timer) |
| 5061-5099 | - | **预留**（K8s 资源 / 云实例 / IaC 等） |

> **5050-5060 已占满**，新增资产类型从 5061 开始。

### 2.4 `mxsec.agent.events` — FIM 文件完整性

| 维度 | 配置 |
|------|------|
| DataType | 6001, 6002 |
| Partitions | 12 |
| Retention | 72h |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | FIM 插件 |
| ConsumerGroup | `mxsec-writers` + `mxsec-engine` |
| 写入存储 | MySQL + ClickHouse + Engine CEL |
| DLQ | `mxsec.agent.events.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 6001 | Plugin → Server | FIM 变更事件 |
| 6002 | Plugin → Server | FIM 任务完成信号 |
| 6004 | Plugin → Server | FIM 基线快照（首次扫描，AC 直处理，**不走 Kafka**） |
| 6000, 6003, 6005-6099 | - | 未分配 |

> **6004 特殊路径**：AC 在 Kafka 发送前拦截 6004，直接调用 `handleFIMBaselineSnapshot()`，原因是基线快照含完整文件哈希列表，涉及 GORM 事务 + 去重，不适合走 Kafka → Consumer 异步链路。

### 2.5 `mxsec.agent.scanner` — 恶意文件 / 漏洞扫描

| 维度 | 配置 |
|------|------|
| DataType | 7000-7099 |
| Partitions | 6 |
| Retention | 7d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Scanner 插件（上行） / Manager（下行） |
| ConsumerGroup | `mxsec-writers` + `mxsec-engine` |
| 写入存储 | MySQL + Engine CEL（关联 IOC / advisory） |
| DLQ | `mxsec.agent.scanner.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 7000 | Server → Plugin | 扫描任务下发 |
| 7001 | Plugin → Server | 扫描结果（威胁检出） |
| 7002 | Plugin → Server | 扫描任务完成信号 |
| 7003 | Server → Plugin | 隔离 / 删除命令（仅 `MODE=protect` 时下发，详见 [`operating-modes.md`](operating-modes.md) §8） |
| 7004 | Plugin → Server | 隔离 / 删除执行结果 |
| 7005-7099 | - | 未分配 |

### 2.6 `mxsec.agent.baseline` — 基线合规

| 维度 | 配置 |
|------|------|
| DataType | 8000-8099 |
| Partitions | 6 |
| Retention | 7d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Baseline 插件 |
| ConsumerGroup | `mxsec-writers` + `mxsec-engine` |
| 写入存储 | MySQL（结果表）+ Redis（评分缓存） |
| DLQ | `mxsec.agent.baseline.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 8000 | Plugin → Server | 基线检查结果 |
| 8001 | Plugin → Server | 基线扫描任务完成信号 |
| 8002 | - | 未分配 |
| 8003 | Plugin → Server | 基线修复结果 |
| 8004 | Plugin → Server | 基线修复任务完成信号 |
| 8005-8099 | - | 未分配 |

### 2.7 `mxsec.agent.remediation` — 漏洞修复

| 维度 | 配置 |
|------|------|
| DataType | 9100-9299 |
| Partitions | 6 |
| Retention | 7d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Manager（下行 9100）/ Remediation 插件（上行 9200） |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（修复任务 + 结果） |
| DLQ | `mxsec.agent.remediation.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 9100 | Server → Plugin | 修复任务下发 |
| 9101-9199 | - | **预留**（预检结果 / 回滚指令 / 多阶段修复） |
| 9200 | Plugin → Server | 修复执行结果 |
| 9201-9299 | - | **预留** |

### 2.8 `mxsec.agent.command-ack` — 命令回包

| 维度 | 配置 |
|------|------|
| DataType | 9999 |
| Partitions | 6 |
| Retention | 7d |
| Partition Key | `{tenant_id}:{agent_id}` |
| Producer | Agent |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（任务状态机：pending → running → done） |
| DLQ | `mxsec.agent.command-ack.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 9900 | Server → Agent | 任务取消信号（Agent 内部处理，不走 Kafka） |
| 9997 | Server → Agent | 网络阻断 / 隔离（block_ip / unblock_ip / isolate / release） |
| 9998 | Server → Agent | 自动响应（kill_process / quarantine_file） |
| 9999 | Agent → Server | 命令执行回包 |
| 9901-9996 | - | 未分配 |

> 9997 / 9998 **受 `MODE` 控制**：`observe` 时 Engine 不下发，仅产 `would_action` 字段告警；`protect` 时下发到 Agent 执行。详见 [`operating-modes.md`](operating-modes.md) §8.1。

### 2.9 插件 SDK 内部禁区（9000-9001）

| DataType | 方向 | 说明 |
|----------|------|------|
| 9000 | Agent → Plugin | 插件心跳 Ping（SDK 内部拦截） |
| 9001 | Plugin → Agent | 插件心跳 Pong（SDK 内部拦截） |

> **禁区**：9000 / 9001 被插件 SDK `ReceiveTask()` 内部拦截，业务 DataType 使用这两个值会被吞掉。这是历史 DataType 9000→9100 冲突事件的根因。

### 2.10 IOC / 规则分发（9300-9499，不走 Kafka）

| DataType | 方向 | 说明 | 链路 |
|----------|------|------|------|
| 9300 | Server → Agent | IOC 下发（全量 / 增量） | AC IOCSyncScheduler → gRPC Task → Agent ioc.Store |
| 9301-9399 | - | 预留威胁情报扩展 | - |
| 9400 | Server → Agent | Agent 检测规则（YAML 全量） | AC RuleSyncScheduler → gRPC Task → Agent rule.Manager |
| 9401-9499 | - | 预留规则分发扩展 | - |

> **特殊路径**：9300 / 9400 走 gRPC Task 机制（`ObjectName="edr"`）直接推送，不进 Kafka，避免 Kafka 不可用时无法下发安全更新。

---

## 3. v2.0 新增 Topic（6 个）

### 3.1 多租户 Partition Key 设计

**默认策略：共享 Topic + 复合 Key**

所有共享 Topic 的 Partition Key 由 v1.x 的 `agent_id` 升级为 `{tenant_id}:{agent_id}`：

```go
// internal/server/common/kafka/topics.go
func BuildKey(tenantID, agentID string) string {
    if tenantID == "" {
        tenantID = "default"
    }
    return tenantID + ":" + agentID
}
```

**好处**：

1. 同一 Agent 数据仍有序（一个 Agent 只属于一个租户，复合 Key 保证哈希到同分区）
2. 跨租户负载均衡（不同租户的相同 agent_id 不再撞分区）
3. Consumer 侧排错 / Trace 直接看 Key 即可识别归属租户

**可选策略：KA 独立 Topic per tenant**

对 `tenants.isolation_strategy = schema | db` 的 KA 客户，启用独立 Topic：

```
t.{tenant_id}.mxsec.agent.heartbeat
t.{tenant_id}.mxsec.agent.ebpf
t.{tenant_id}.mxsec.engine.alert
...
```

适用场景（详见 [`multi-tenant.md`](multi-tenant.md) §5.2）：

- 金融 / 政府监管要求"我的数据绝不出现在他人 partition"
- 单租户 > 100k EPS，独占带宽避免噪音邻居
- 独立 Kafka 集群部署（`isolation_strategy=db` 极端隔离）

**Topic 派生函数**：

```go
func RouteTopic(tenantID string, dataType int32) string {
    base := RouteDataType(dataType, "mxsec")  // 例: mxsec.agent.ebpf
    strategy := lookupIsolationStrategy(tenantID)
    if strategy == "shared" || tenantID == "" {
        return base
    }
    return "t." + tenantID + "." + base
}
```

### 3.2 `mxsec.engine.alert` — Engine 检测告警 ⭐NEW

> Engine 是 v2.0 新增微服务，专司检测分析（CEL / 序列 / ML / Storyline）。所有告警**先入 Kafka 再持久化**，避免 Engine 与 MySQL 强耦合。

| 维度 | 配置 |
|------|------|
| DataType | 11001-11099 |
| Partitions | 12 |
| Retention | 7d |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{host_id}`（按主机聚合便于 Storyline 关联） |
| Producer | **Engine** |
| ConsumerGroup | `mxsec-writers`（持久化）+ Manager SSE 旁路（实时推 UI） |
| 写入存储 | MySQL（`alerts` 表）+ ClickHouse（`alert_timeline` 时序） |
| DLQ | `mxsec.engine.alert.dlq` (3 分区 / 30d) |

| DataType | 方向 | 说明 | 触发源 |
|----------|------|------|--------|
| 11001 | Engine → Server | CEL 规则告警 | 规则引擎匹配 |
| 11002 | Engine → Server | 序列异常告警（Markov / n-gram） | 序列检测层 |
| 11003 | Engine → Server | ML 异常告警（IForest / LightGBM） | ONNX Runtime 推理 |
| 11004 | Engine → Server | 端口扫描告警（滑动窗口） | 网络事件聚合 |
| 11005 | Engine → Server | 暴力破解告警（SSH / DB / Web） | 登录失败聚合 |
| 11006 | Engine → Server | Webshell 告警 | 文件 + 网络复合规则 |
| 11007 | Engine → Server | 横向移动告警（lateral_movement） | Storyline 子图 |
| 11008 | Engine → Server | 持久化告警（persistence） | 文件 / cron / kmod 复合规则 |
| 11009 | Engine → Server | 数据外发告警（exfiltration） | DNS / HTTPS 流量 + ML |
| 11010 | Engine → Server | 主机漏洞匹配告警（host_vulnerability） | 资产指纹 × VulnSync advisory |
| 11011 | Engine → Server | K8s Admission Webhook 告警 | K8s Audit 检测规则 |
| 11012 | Engine → Server | 容器逃逸告警 | EDR + capabilities |
| 11013 | Engine → Server | IOC 命中告警 | 9300 IOC 库碰撞 |
| 11014-11099 | - | **预留**（按 ATT&CK 战术持续扩展） | - |

**Alert 消息 schema**（关键字段）：

```json
{
  "data_type": 11001,
  "alert_id": "alrt-2026060100001",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "agent_id": "a-67890",
  "rule_id": "BRUTE_FORCE_SSH",
  "severity": "high",
  "mode": "observe",
  "att_ck": ["T1110.001"],
  "detected_at": "2026-06-06T10:23:45Z",

  "would_action": {           // observe 模式专属
    "type": "ip_block",
    "target": "192.0.2.45",
    "duration_sec": 3600
  },
  "action": null,             // protect 模式才填
  "action_result": null,

  "evidence": {...},
  "trace_id": "abc...def"
}
```

完整 schema 见 [`operating-modes.md`](operating-modes.md) §6。

### 3.3 `mxsec.engine.storyline` — 攻击链 ⭐NEW

> Storyline 是 Engine 对多条 alert / event 的图聚合，输出"攻击链"完整故事。Storyline 持久化窗口长（14d），便于事后 SOC 复盘。

| 维度 | 配置 |
|------|------|
| DataType | 11100-11199 |
| Partitions | 6 |
| Retention | 14d |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{storyline_id}` |
| Producer | Engine（Storyline 子模块） |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（`storylines` 头表 + `storyline_nodes` 节点表） + ClickHouse（节点时序快照） |
| DLQ | `mxsec.engine.storyline.dlq` (3 分区 / 30d) |

| DataType | 方向 | 说明 |
|----------|------|------|
| 11100 | Engine → Server | Storyline 头（创建 / 更新） |
| 11101 | Engine → Server | Storyline 节点追加（新告警 / 新事件入链） |
| 11102 | Engine → Server | Storyline 闭合（攻击者放弃 / 已被阻断） |
| 11103 | Engine → Server | Storyline LLM 总结回写（LLMProxy 调用后将摘要回写，便于离线 RAG） |
| 11104-11199 | - | **预留**（多链合并 / 跨主机攻击图 / MITRE Navigator 导出） |

### 3.4 `mxsec.engine.feedback` — 误报反馈 ⭐NEW

> 反馈通道是"监听优先"哲学的工程化落地。每条告警 UI 提供 3 按钮（真威胁 / 误报 / 不确定），数据写入此 Topic，Engine 离线读取用于：CEL 白名单建议、ML 增量训练、阈值校准。

| 维度 | 配置 |
|------|------|
| DataType | 11900-11999 |
| Partitions | 3 |
| Retention | 30d |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{rule_id}`（按规则聚合便于离线训练） |
| Producer | **Manager UI**（用户标记后由 Manager 通过内部 API 投递） |
| ConsumerGroup | `mxsec-writers`（持久化） + `mxsec-engine`（在线统计） |
| 写入存储 | MySQL（`alert_feedback` 表） + ClickHouse（feedback 时序，便于训练取数） |
| DLQ | `mxsec.engine.feedback.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 11900 | UI → Engine | 告警标记反馈（TP / FP / uncertain） |
| 11901 | UI → Engine | Storyline 标记反馈（链准确 / 链断裂 / 链混淆） |
| 11902 | UI → Engine | 规则白名单建议（用户提交） |
| 11903 | UI → Engine | ML 模型版本评价（用户对当周模型的整体反馈） |
| 11904-11999 | - | **预留** |

**Feedback 消息 schema**：

```json
{
  "data_type": 11900,
  "tenant_id": "t-bank-a",
  "alert_id": "alrt-2026060100001",
  "rule_id": "BRUTE_FORCE_SSH",
  "label": "false_positive",
  "user_id": "u-12345",
  "submitted_at": "2026-06-06T11:00:00Z",
  "comment": "运维堡垒机批量自动化，已白名单",
  "suggested_whitelist": {
    "field": "source_ip",
    "value": "10.1.0.0/16"
  }
}
```

### 3.5 `mxsec.vuln.advisory` — VulnSync 漏洞情报 ⭐NEW

> VulnSync 微服务从 11+ 外部源同步漏洞数据（NVD / OSV / RHSA / USN / DSA / Alpine / SUSE / KEV / ExploitDB / CNNVD / 信创 4 源 / EPSS），advisory 仲裁融合后推送本 Topic。Consumer 持久化 + Engine 读取做主机指纹 × advisory 匹配。

| 维度 | 配置 |
|------|------|
| DataType | 12001-12099 |
| Partitions | 6 |
| Retention | 30d |
| Replication / minISR | 2 / 1 |
| Partition Key | `{cve_id}` 或 `{advisory_id}`（advisory 本身无租户属性，全局可见） |
| Producer | **VulnSync**（单副本 Leader Election） |
| ConsumerGroup | `mxsec-writers`（持久化）+ `mxsec-engine`（主机匹配） |
| 写入存储 | MySQL（`vulnerabilities` / `advisories` / `cve_purl_index` / `cve_nevra_index` 等） + Redis（热点 CVE 缓存） |
| DLQ | `mxsec.vuln.advisory.dlq` (3 分区 / 90d) |

| DataType | 方向 | 说明 | 数据源 |
|----------|------|------|--------|
| 12001 | VulnSync → Server | NVD CVE 主体 | NVD API |
| 12002 | VulnSync → Server | OSV 软件包漏洞 | OSV API（Google） |
| 12003 | VulnSync → Server | 厂商发行版 advisory（RHSA / USN / DSA / Alpine / SUSE） | 各厂商 |
| 12004 | VulnSync → Server | CISA KEV（已知被利用） | CISA KEV |
| 12005 | VulnSync → Server | ExploitDB PoC | ExploitDB |
| 12006 | VulnSync → Server | CNNVD 编号补全 | CNNVD |
| 12007 | VulnSync → Server | 信创 OS advisory（openEuler / Anolis / Kylin / UOS） | CSA / ANSA / KYSA / UOSEC |
| 12008 | VulnSync → Server | EPSS 利用概率分数 | FIRST.org |
| 12009 | VulnSync → Server | 仲裁融合结果（PURL + NEVRA 双索引，3 级 confidence） | VulnSync 内部仲裁 |
| 12010 | VulnSync → Server | 删除 / 撤回（CVE 被合并或撤销） | 上游变更 |
| 12011-12099 | - | **预留**（如内部 0day 情报 / 客户专属订阅源） | - |

**Advisory 消息 schema**（核心字段）：

```json
{
  "data_type": 12009,
  "advisory_id": "adv-2026-0001",
  "cve_id": "CVE-2026-12345",
  "cnnvd_id": "CNNVD-202606-001",
  "severity": "critical",
  "cvss_v3": 9.8,
  "epss_score": 0.92,
  "kev_listed": true,
  "exploit_available": true,
  "published_at": "2026-06-01T00:00:00Z",
  "updated_at": "2026-06-06T08:00:00Z",
  "purl_match": ["pkg:golang/github.com/foo/bar@<1.2.3"],
  "nevra_match": [
    {"distro": "rhel", "version": "8", "nevra": "openssl-0:1.1.1k-7.el8_6"}
  ],
  "confidence": "high",
  "fix_versions": ["1.2.3"],
  "references": ["https://..."],
  "trace_id": "..."
}
```

### 3.6 `mxsec.llm.audit` — LLMProxy 调用审计 ⭐NEW

> LLMProxy 每次对外厂商调用都入此 Topic，用于：合规审计（PII 检测）、计费、模型质量复盘、租户成本归集。

| 维度 | 配置 |
|------|------|
| DataType | 13001-13099 |
| Partitions | 3 |
| Retention | 90d（合规口径，**不可短于 90d**） |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{user_id}`（便于按用户回溯） |
| Producer | **LLMProxy** |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | ClickHouse（`llm_audit_log` 大表） + MySQL（聚合统计每日 / 每租户） |
| DLQ | `mxsec.llm.audit.dlq` |

| DataType | 方向 | 说明 |
|----------|------|------|
| 13001 | LLMProxy → Server | LLM 同步调用审计（Complete） |
| 13002 | LLMProxy → Server | LLM 流式调用审计（Stream，每会话一条汇总） |
| 13003 | LLMProxy → Server | Embedding 调用审计 |
| 13004 | LLMProxy → Server | LLM 调用失败 / Fallback 触发记录 |
| 13005 | LLMProxy → Server | 缓存命中记录（不计费，但要审计） |
| 13006 | LLMProxy → Server | PII 检测命中（请求中含敏感字段，已脱敏后调用） |
| 13007-13099 | - | **预留**（多模态 / 函数调用 / 工具使用扩展） |

**审计消息 schema**（核心字段）：

```json
{
  "data_type": 13001,
  "request_id": "req-2026060611001",
  "tenant_id": "t-bank-a",
  "user_id": "u-12345",
  "scene": "alert_explain",
  "provider": "anthropic",
  "model": "claude-3-5-sonnet-20240620",
  "tokens_in": 1240,
  "tokens_out": 380,
  "cost_usd": 0.0124,
  "latency_ms": 1830,
  "cache_hit": false,
  "fallback_chain": [],
  "pii_redacted_fields": ["host_ip"],
  "trace_id": "...",
  "called_at": "2026-06-06T11:23:45Z"
}
```

> **合规约束**：审计字段保留 90 天不可篡改（ClickHouse 设 TTL=90d 且禁止 DELETE）。详见 [`llmproxy-design.md`](llmproxy-design.md)。

### 3.7 `mxsec.metering.usage` — 计量用量 ⭐NEW

> 多租户计费基础设施。Manager / LLMProxy / Consumer 每日聚合一次推送用量数据；MySQL 落库后供计费引擎 + 客户用量 Dashboard 使用。

| 维度 | 配置 |
|------|------|
| DataType | 14001-14099 |
| Partitions | 3 |
| Retention | 365d（按年对账） |
| Replication / minISR | 2 / 1 |
| Partition Key | `{tenant_id}:{date}`（按租户+日聚合） |
| Producer | Manager（聚合）/ LLMProxy（LLM 维度）/ Consumer（事件量维度） |
| ConsumerGroup | `mxsec-writers` |
| 写入存储 | MySQL（`tenant_metrics_daily` 主表 + `tenant_llm_usage` 明细） |
| DLQ | `mxsec.metering.usage.dlq` (1 分区 / 365d) |

| DataType | 方向 | 说明 |
|----------|------|------|
| 14001 | Manager → Server | 租户日度聚合用量（Agent 数 / 事件量 / 告警量 / API 调用） |
| 14002 | LLMProxy → Server | 租户 LLM 用量（tokens_in/out / cost_usd / 调用次数） |
| 14003 | Consumer → Server | 事件量分类型计数（按 DataType bucket） |
| 14004 | Manager → Server | 配额超限事件（quota_agents / quota_llm_usd / quota_events_per_day） |
| 14005-14099 | - | **预留**（OEM / 多 Region 跨 Region 计量） |

**Metering 消息 schema** 详见 [`multi-tenant.md`](multi-tenant.md) §8.2。

---

## 4. Kafka Topic 路由总表

完整路由表，与 [`architecture.md`](architecture.md) §4.1 一一对应：

| Topic | DataType 范围 | Partitions | Retention | Replication | DLQ |
|-------|--------------|-----------|-----------|-------------|-----|
| `mxsec.agent.heartbeat` | 1000-1099 | 6 | 24h | 2 | `mxsec.agent.heartbeat.dlq` |
| `mxsec.agent.ebpf` | 3000-3099 | 12 | 3d | 2 | `mxsec.agent.ebpf.dlq` |
| `mxsec.agent.asset` | 5050-5099 | 6 | 7d | 2 | `mxsec.agent.asset.dlq` |
| `mxsec.agent.events` | 6001, 6002 | 12 | 72h | 2 | `mxsec.agent.events.dlq` |
| `mxsec.agent.scanner` | 7000-7099 | 6 | 7d | 2 | `mxsec.agent.scanner.dlq` |
| `mxsec.agent.baseline` | 8000-8099 | 6 | 7d | 2 | `mxsec.agent.baseline.dlq` |
| `mxsec.agent.remediation` | 9100-9299 | 6 | 7d | 2 | `mxsec.agent.remediation.dlq` |
| `mxsec.agent.command-ack` | 9999 | 6 | 7d | 2 | `mxsec.agent.command-ack.dlq` |
| **`mxsec.engine.alert`** ⭐ | 11001-11099 | 12 | 7d | 2 | `mxsec.engine.alert.dlq` |
| **`mxsec.engine.storyline`** ⭐ | 11100-11199 | 6 | 14d | 2 | `mxsec.engine.storyline.dlq` |
| **`mxsec.engine.feedback`** ⭐ | 11900-11999 | 3 | 30d | 2 | `mxsec.engine.feedback.dlq` |
| **`mxsec.vuln.advisory`** ⭐ | 12001-12099 | 6 | 30d | 2 | `mxsec.vuln.advisory.dlq` |
| **`mxsec.llm.audit`** ⭐ | 13001-13099 | 3 | 90d | 2 | `mxsec.llm.audit.dlq` |
| **`mxsec.metering.usage`** ⭐ | 14001-14099 | 3 | 365d | 2 | `mxsec.metering.usage.dlq` |
| **兜底（未注册）** | other | — | — | — | — |

> **兜底陷阱**：未注册 DataType 默认入 `mxsec.agent.heartbeat`，Consumer 静默丢弃。这是 §11 历史事故根因。任何新 DataType 必须先在本文档登记 + 同步更新 `RouteDataType()` + Consumer `handleMessage()`。

### 4.1 分区数推荐（按规模档）

| 规模档 | Agent 数 | EBPF/事件类 | 告警类 | 配置类 |
|--------|---------|-----------|--------|--------|
| Demo | 100-500 | 6 | 3 | 1 |
| 小规模 | 500-2k | 12（默认） | 6（默认） | 3（默认） |
| 中规模 | 2k-10k | 24 | 12 | 6 |
| 大规模 | 10k-50k | 48 | 24 | 12 |
| 极限 | 50k-300k | 96（多 Kafka 集群联邦） | 48 | 24 |

> 分区数升级需 Kafka Admin API 扩容 + Consumer 滚动重启，详见 [`deployment.md`](deployment.md) 容量规划章节。

### 4.2 ConsumerGroup 拓扑

| ConsumerGroup | 服务 | 订阅 Topic | 用途 | 副本 |
|---------------|------|-----------|------|------|
| `mxsec-writers` | Consumer | 8 个 `mxsec.agent.*` + 3 个 `mxsec.engine.*` + `mxsec.vuln.advisory` + `mxsec.llm.audit` + `mxsec.metering.usage` | 持久化 MySQL / CK / Redis | N |
| `mxsec-engine` | Engine | `mxsec.agent.ebpf` / `mxsec.agent.events` / `mxsec.agent.scanner` / `mxsec.agent.baseline` / `mxsec.vuln.advisory` / `mxsec.engine.feedback` | 检测分析 + 在线反馈统计 | N |
| `mxsec-vulnsync` | VulnSync | 无（生产者） | — | 1（Leader） |
| `mxsec-llmproxy` | LLMProxy | 无（生产者） | — | N |

> Kafka 多 ConsumerGroup 同源消费**互不阻塞、不重复扣费**，是六微服务数据面解耦的关键设计。

### 4.3 DLQ 命名与处理

DLQ 统一命名规则：`{topic}.dlq`，3 分区 30 天（高保留 Topic 如 `vuln.advisory` 90 天 / `metering.usage` 365 天）。

DLQ 消息 schema：

```json
{
  "original_topic": "mxsec.engine.alert",
  "original_partition": 7,
  "original_offset": 1234567,
  "original_key": "t-bank-a:h-12345",
  "original_value": "<base64 原始 protobuf>",
  "error_type": "deserialize_failed",
  "error_msg": "proto: unknown field 'mode_v3'",
  "retry_count": 5,
  "first_failed_at": "2026-06-06T10:23:45Z",
  "last_failed_at": "2026-06-06T10:24:01Z",
  "consumer_group": "mxsec-writers",
  "consumer_instance": "consumer-2"
}
```

DLQ 处理：

- Manager 提供 `/api/v2/admin/dlq/list` 查看
- Manager 提供 `/api/v2/admin/dlq/replay` 重新投递（修复 bug 后）
- DLQ 滞留 > 1000 条触发 Prometheus 告警 `mxsec_dlq_backlog`

---

## 5. DataType 与 Proto Message 映射

所有 Kafka 消息 body 都是 Protobuf 编码（`bridge.Record` 或对应业务消息），DataType 决定如何反序列化。

| DataType 范围 | Proto Message | 定义路径 |
|--------------|--------------|----------|
| 1000-1001 | `pb.Heartbeat` / `pb.PluginHeartbeat` | `api/proto/agent.proto` |
| 3000-3099 | `pb.EDREvent`（含 oneof 子类型） | `api/proto/edr.proto` |
| 5050-5099 | `pb.AssetRecord`（含 oneof 子类型） | `api/proto/asset.proto` |
| 6001-6004 | `pb.FIMEvent` / `pb.FIMSnapshot` | `api/proto/fim.proto` |
| 7000-7004 | `pb.ScannerTask` / `pb.ScannerResult` | `api/proto/scanner.proto` |
| 8000-8004 | `pb.BaselineResult` / `pb.BaselineFix` | `api/proto/baseline.proto` |
| 9100-9299 | `pb.RemediationTask` / `pb.RemediationResult` | `api/proto/remediation.proto` |
| 9300 | `pb.IOCBundle` | `api/proto/ioc.proto` |
| 9400 | `pb.RuleBundle` | `api/proto/rule.proto` |
| 9997-9999 | `pb.Command` / `pb.CommandAck` | `api/proto/grpc.proto` |
| **11001-11099** | `pb.EngineAlert` | `api/proto/engine.proto` ⭐NEW |
| **11100-11199** | `pb.EngineStoryline` / `pb.EngineStorylineNode` | `api/proto/engine.proto` ⭐NEW |
| **11900-11999** | `pb.EngineFeedback` | `api/proto/engine.proto` ⭐NEW |
| **12001-12099** | `pb.VulnAdvisory` / `pb.VulnPurlMatch` / `pb.VulnNevraMatch` | `api/proto/vuln.proto` ⭐NEW |
| **13001-13099** | `pb.LLMAuditRecord` | `api/proto/llm.proto` ⭐NEW |
| **14001-14099** | `pb.MeteringUsage` | `api/proto/metering.proto` ⭐NEW |

Proto 文件统一在 `api/proto/`，`make proto` 一键生成 Go stub。每个 Proto Message **必带** `tenant_id` 字段（除 12001-12010 advisory 是全局共享外）。

---

## 6. 多租户 Key 设计完整规范

### 6.1 复合 Key 默认（共享 Topic）

```go
// 生产者侧
func ProduceAlert(ctx context.Context, alert *pb.EngineAlert) error {
    key := tenant.BuildKey(alert.TenantId, alert.HostId)  // "t-bank-a:h-12345"
    topic := topics.RouteTopic(alert.TenantId, alert.DataType)
    return producer.SendKey(ctx, topic, key, alert)
}

// 消费者侧 — 从 Key 还原归属
func handleMessage(msg *sarama.ConsumerMessage) {
    tenantID, _, _ := strings.Cut(string(msg.Key), ":")
    ctx := tenant.WithTenant(context.Background(), tenantID)
    // 后续 GORM 查询自动 Apply TenantScope
    db.Scopes(tenant.FromContext(ctx).Apply).Save(...)
}
```

### 6.2 独立 Topic per tenant（KA）

启用条件（`tenants.isolation_strategy`）：

| 策略 | DB | Kafka Topic | 适用 |
|------|----|-----|------|
| `shared`（默认） | 共库共表 + `tenant_id` | 共享 Topic + 复合 Key | 中小客户 / 互联网 |
| `schema` | 同实例独立 schema | **独立 Topic** `t.{tenant_id}.{base_topic}` | 中型政企 |
| `db` | 独立 MySQL / CK 实例 | **独立 Topic + 可选独立 Kafka 集群** | 金融 KA / 监管 |

策略迁移单向：`shared → schema → db`，工具 `mxctl tenant migrate`。

### 6.3 跨租户穿越测试

每个发布版本必须跑：

- 模拟租户 A token 投递 Key=`t-bank-b:h-1` 消息 → Consumer 应根据 Key 落到租户 B 库，Manager 查询时租户 A 看不到（行级隔离生效）
- 删除 `TenantScope.Apply` 调用 → 单元测试 panic
- Engine 加载规则时按 tenant_id 隔离，规则不串

---

## 7. 处理路径对照表

每个上行 DataType 有两条处理路径：Kafka 路径（主路径）与 AC 直写路径（Kafka 关闭时兜底）。

| DataType | Kafka 路由 | Consumer 处理 | AC 直写兜底 | 说明 |
|----------|-----------|---------------|------------|------|
| 1000 | heartbeat | MySQL+CK+Redis | Y | 完整双路径 |
| 1001 | heartbeat | ClickHouse | - | Kafka-only，指标无需 MySQL |
| 3000-3003 | ebpf | ClickHouse + Engine 检测 | - | Kafka-only，时序数据 |
| 3004 | ebpf | MySQL + Engine 检测 | - | Kafka-only，高价值持久化 |
| 5050-5060 | asset | MySQL | Y | 完整双路径 |
| 6001 | events | MySQL+CK + Engine 检测 | Y | 完整双路径 |
| 6002 | events | MySQL | Y | 完整双路径 |
| 6004 | **AC 拦截** | — | Y | 不走 Kafka，AC 直处理（事务 + 去重） |
| 7001 | scanner | MySQL + Engine 检测 | Y | 完整双路径 |
| 7002 | scanner | MySQL | Y | 完整双路径 |
| 7004 | scanner | MySQL | Y | 完整双路径 |
| 8000 | baseline | MySQL + Engine 检测 | Y | 完整双路径 |
| 8001/8003/8004 | baseline | MySQL | Y | 完整双路径 |
| 9200 | remediation | MySQL | Y | 完整双路径 |
| 9999 | command-ack | MySQL | - | Kafka-only |
| **11001-11099** | engine.alert | MySQL + CK + SSE | — | Engine 唯一产出路径（无直写） |
| **11100-11199** | engine.storyline | MySQL + CK | — | Engine 唯一产出路径 |
| **11900-11999** | engine.feedback | MySQL + CK + Engine 在线统计 | — | UI 触发，无 Agent 链路 |
| **12001-12099** | vuln.advisory | MySQL + Redis 缓存 + Engine 匹配 | — | VulnSync 唯一产出 |
| **13001-13099** | llm.audit | ClickHouse + MySQL 日聚合 | — | LLMProxy 唯一产出 |
| **14001-14099** | metering.usage | MySQL | — | 仅 Manager / LLMProxy 内部投递 |

> **设计原则**：v2.0 新增 6 个 Topic 全部 **Kafka-only**，不提供 AC / Engine 直写 MySQL 兜底。Kafka 必须 HA（3 Broker KRaft + `replication_factor=2`）。Kafka 不可用走 §8 降级。

---

## 8. Kafka 不可用降级

延续 v1.x 机制：

- AsyncProducer 内置 10000 容量内存队列 + 5min TTL + 最大 5 次重试
- Kafka 恢复后自动回放
- 内存队列超阈值（80%）触发 Prometheus 告警 `mxsec_kafka_fallback_queue_size`

v2.0 新增约束：

- `mxsec.engine.alert` / `mxsec.vuln.advisory` 必须**持久化降级**（写本地 BoltDB 而非纯内存），避免 Engine / VulnSync 宕机丢告警 / 漏洞情报
- `mxsec.llm.audit` 不可丢失，纯内存降级到 100% 后 LLMProxy **拒绝新请求**（fail-closed），保证审计完备

---

## 9. 新增 DataType 申请流程

新增任何 DataType 前必须完成以下流程，**不允许跳步**：

### 9.1 申请步骤

```
[1] 提交 DataType 注册申请（Issue 模板：.github/ISSUE_TEMPLATE/datatype.md）
    │   - 模块归属：哪个 Topic？哪个微服务产 / 消？
    │   - 申请号段：起止区间（≤ 100 个号段，避免浪费）
    │   - 方向：agent→server / server→agent / 内部
    │   - Proto Message：含字段清单
    │   - 多租户：是否带 tenant_id？
    │   - 检测路径：是否触发 Engine？是否写 CK？
    ↓
[2] 平台负责人评审（≤ 2 工作日）
    │   - 检查号段冲突
    │   - 检查 Proto 字段是否冗余
    │   - 检查 Topic 容量（如 Topic 已 > 50 个 DataType 需考虑拆分）
    ↓
[3] 文档登记（本文件 §1 / §2 / §3 对应章节）
    ↓
[4] 代码同步（最少 4 个改动）
    │   ✔ internal/server/common/kafka/topics.go → RouteDataType()
    │   ✔ internal/server/consumer/router.go → handleMessage() switch case
    │   ✔ Consumer Writer（如写 MySQL，新增 WriteXxx 方法）
    │   ✔ Agent isCompletionSignal()（如是任务完成信号）
    │   ✔ 如新增 Topic：Router topics 切片 + Topic Catalog 配置
    ↓
[5] 测试验证
    │   - 发送 1 条测试消息 → 确认 Kafka 路由正确
    │   - Consumer 消费 → 确认 DB 写入
    │   - 跨租户穿越测试
    │   - DLQ 测试（构造畸形消息）
    ↓
[6] PR 评审 + 合并
```

### 9.2 检查清单（PR 模板）

PR 必须勾选以下全部项才允许合并：

- [ ] 本文档（§1/§2/§3）已登记 DataType 值、方向、说明
- [ ] Proto Message 已添加到 `api/proto/`，`make proto` 通过
- [ ] `RouteDataType()` 已添加路由规则
- [ ] Consumer `handleMessage()` 已添加分支
- [ ] Consumer Writer 已实现（如需写 MySQL）
- [ ] Topic 订阅已添加（如新增 Topic）
- [ ] Agent `isCompletionSignal()` 已注册（如是任务完成信号）
- [ ] 单元测试覆盖：路由 / 反序列化 / 落库
- [ ] 跨租户穿越测试通过
- [ ] DLQ 异常路径测试
- [ ] Prometheus 指标已加：`mxsec_kafka_messages_total{topic, data_type}`
- [ ] `docs/api-reference.md` 已同步（如对外暴露）

### 9.3 号段废弃流程

废弃 DataType 走"软废弃"：

1. 标记为 `deprecated`，本文档段落加注 `~~⛔ Deprecated @ v2.x — use 1234 instead~~`
2. Consumer 继续接收但只写 audit log，不写主表
3. **6 个月观察期**，确认无 Agent 端仍在使用
4. 下个大版本（v3.0）才物理移除路由 + Consumer 分支

### 9.4 KA / OEM 私有号段

20000+ 区间留给 KA 客户私有扩展（如金融客户自定义合规事件类型）：

- 必须走平台 RBAC 审批
- 必须本文档登记到 §9.5 "外部私有号段"附录
- 必须独立 Topic（不混入 mxsec 主 Topic），命名 `oem.{client_id}.{topic}`

### 9.5 外部私有号段附录

| 号段 | 客户 | 用途 | 申请日 | 文档链接 |
|------|------|------|--------|----------|
| _（当前无私有号段登记）_ | — | — | — | — |

> 当前无 KA 私有号段申请。所有新增条目必须经平台架构组评审，并在本表追加一行（号段、客户名、用途、申请日、私有 Proto 文档链接）。

---

## 10. 一致性核查

### 10.1 与 `architecture.md` §4 Kafka 拓扑

本文档 §4 路由总表与 `architecture.md` §4.1 Topic 总览**逐条对照**：

| `architecture.md` §4.1 行 | 本文档 §4 行 | 一致性 |
|---|---|---|
| `mxsec.agent.heartbeat` 1000, 1001 / 6 / 24h | 同 | ✓ |
| `mxsec.agent.asset` 5050-5060 / 6 / 7d | 5050-5099（含预留） / 6 / 7d | ✓（含预留扩展） |
| `mxsec.agent.events` 6001, 6002 / 12 / 72h | 同 | ✓ |
| `mxsec.agent.ebpf` 3000-3002 / 12 / 3d | 3000-3099（含 3003/3004/3010） / 12 / 3d | ✓（扩展已登记） |
| `mxsec.agent.baseline` 8000-8004 / 6 / 7d | 8000-8099 / 6 / 7d | ✓ |
| `mxsec.agent.scanner` 7000-7004 / 6 / 7d | 7000-7099 / 6 / 7d | ✓ |
| `mxsec.agent.remediation` 9100-9299 / 6 / 7d | 同 | ✓ |
| `mxsec.agent.command-ack` 9999 / 6 / 7d | 同 | ✓ |
| `mxsec.engine.alert` 11001-11099 / 12 / 7d | 同 | ✓ |
| `mxsec.engine.storyline` 11100-11199 / 6 / 14d | 同 | ✓ |
| `mxsec.engine.feedback` 11900-11999 / 3 / 30d | 同 | ✓ |
| `mxsec.vuln.advisory` 12001-12099 / 6 / 30d | 同 | ✓ |
| `mxsec.llm.audit` 13001-13099 / 3 / 90d | 同 | ✓ |
| `mxsec.metering.usage` — | 14001-14099 / 3 / 365d（本文档补充） | ✓（在 architecture §4.1 未列，但 multi-tenant.md §8.2 已提及） |

### 10.2 与 `multi-tenant.md` §5

| `multi-tenant.md` §5 要点 | 本文档实现 |
|---|---|
| Key = `{tenant_id}:{agent_id}` | §3.1 / §6.1 |
| Topic 默认共享 + Body 含 tenant_id | §3.1 |
| KA 独立 Topic 命名 `t.{tenant_id}.*` | §3.1 / §6.2 |
| `mxsec-writers` / `mxsec-engine` 双 CG 同源 | §4.2 |
| `isolation_strategy = shared/schema/db` 三档 | §6.2 |

### 10.3 与 `operating-modes.md` §6 / §8

| `operating-modes.md` 要点 | 本文档实现 |
|---|---|
| 所有 Engine 告警带 `mode` 字段 | §3.2 Alert schema |
| `observe` 时仅 `would_action`，`protect` 时填 `action` | §3.2 / §2.8 9997/9998 |
| `mxsec.engine.feedback` 用于误报反馈 | §3.4 |

---

## 11. 历史冲突事件

| 日期 | DataType | 冲突描述 | 影响 | 根因 | 修复 |
|------|----------|----------|------|------|------|
| 2026-05-18 | 9000 | 修复任务 DataType 与插件 SDK 心跳 Ping 冲突 | 任务被 Plugin SDK 拦截吞掉 | 未查阅 SDK 保留值 | 改用 9100 |
| 2026-05-18 | 9200 | 修复结果无 Kafka 路由，默认落 heartbeat Topic | 结果被 Consumer 静默丢弃 | 新增 DataType 未同步 `RouteDataType()` | 补 9200 → `mxsec.agent.remediation` |
| 2026-05-18 | 6004 | FIM 基线快照在 Kafka 启用后被丢弃 | 基线数据丢失 | AC 直写路径被 Kafka 路径旁路，6004 无 Kafka 路由 | AC 拦截 6004 直处理 |

> **教训**：新增 DataType 必须按 §9 流程同步更新本文档 + 路由表 + Consumer 分支三处。任意一处漏改都会复现上述事故。

---

## 12. 版本变更历史

### v2.0 (2026-06-06) — 六微服务版本

- 新增 6 个 Topic：`mxsec.engine.alert` / `mxsec.engine.storyline` / `mxsec.engine.feedback` / `mxsec.vuln.advisory` / `mxsec.llm.audit` / `mxsec.metering.usage`
- 新增 DataType 号段：11001-11099 / 11100-11199 / 11900-11999 / 12001-12099 / 13001-13099 / 14001-14099
- Partition Key 升级为 `{tenant_id}:{agent_id}`
- 引入 KA 独立 Topic 选项 `t.{tenant_id}.*`
- ConsumerGroup 拆分为 `mxsec-writers` / `mxsec-engine` / `mxsec-vulnsync` 三组
- 引入 9.4 KA / OEM 私有号段（20000+）
- 引入 9.3 号段废弃流程（6 个月观察期）
- 引入 8.2 持久化降级（engine.alert / vuln.advisory）+ fail-closed（llm.audit）
- 新增 §10 与 architecture / multi-tenant / operating-modes 三方一致性核查

### v1.0 (2026-05-20) — 三层架构初版

- 定义 8 个 `mxsec.agent.*` Topic
- DataType 1000-9999 完整分配
- 单 ConsumerGroup `mxsec-consumer`
- Partition Key = `agent_id`
- DLQ 全覆盖

---

## 13. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构总图（六微服务） | [`architecture.md`](architecture.md) |
| 监听 / 防护双模式 | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| 漏洞模块设计 | [`vuln-module-design.md`](vuln-module-design.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| API 参考（含 `/api/v2/admin/dlq/*`） | [`api-reference.md`](api-reference.md) |
| 部署指南（容量规划 / Kafka 分区扩容） | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 服务端架构深度（青藤 / mxsec 对比） | `ref/01-服务端架构.md` |
| 总体评估与商业化路线 | `ref/00-总体评估与商业化路线.md` |
