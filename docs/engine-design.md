# Engine 服务设计

> **定位**：Engine 是 mxsec 六微服务中的**检测分析引擎**，把流入 Kafka 的所有 Agent 数据实时跑过"规则 / 序列 / ML / 图 / K8s"五层管线，产出告警、攻击链、反馈三类下游消息。
>
> Engine **只做检测分析、不做存储**，不直接写 MySQL / ClickHouse / Redis 持久化表，所有产出回 Kafka，由 Consumer 统一落库。这是 v1.x 三层架构（Manager + AC + Consumer 三件混杂）→ v2.0 六微服务拆分的核心改动。
>
> 平台默认 `MODE=observe` 监听模式，Engine 只产 `would_action`；磨合达标后切 `MODE=protect` 防护模式，Engine 才真下发处置指令。详见 [`operating-modes.md`](operating-modes.md)。

---

## 1. 服务定位与边界

### 1.1 唯一职责

| 做 | 不做 |
|----|------|
| 消费 Kafka 数据 Topic（ConsumerGroup B `mxsec-engine`） | 直接写 MySQL / ClickHouse / Redis 业务表 |
| 规则层：CEL 表达式 + Sigma / Falco / Tetragon Policies | 业务 API（归 Manager） |
| 序列层：Markov 转移、n-gram、滑动窗口端口扫描 | 数据持久化（归 Consumer） |
| ML 层：ONNX Runtime CPU 推理（IForest / LightGBM / MiniLM 等） | Agent 任务下发协议（归 AgentCenter） |
| 图层：Storyline 攻击链关联 + ATT&CK 战术映射 | 外部漏洞情报抓取（归 VulnSync） |
| K8s 检测：Audit Event + RBAC / 网络 / 工作负载基线（从 Manager 搬入） | LLM 厂商适配 + 计费（归 LLMProxy，可选调用） |
| 响应判定：`mode=observe` 写 `would_action`，`mode=protect` 下发处置 | UI 状态机渲染（归 Manager / 前端） |
| 多租户隔离：按 `tenant_id` 加载规则 / 模型 / 配置 / 阈值 | 多租户身份鉴权（归 Manager 三段鉴权） |
| 产出 `mxsec.engine.alert` / `mxsec.engine.storyline` / `mxsec.engine.feedback` | 告警通知投递（归 Manager 通知模块） |
| 反馈闭环：消费 `mxsec.engine.feedback` 调节规则权重 / 模型阈值 | 模型训练（离线训练在 Manager 训练 Job，Engine 只做推理） |

### 1.2 不变量（违反即设计错误）

1. Engine 进程**无 MySQL DSN 写权限**，部署期 GORM 配置 read-only DSN，强制约束。
2. Engine 进程**不开任何对外 HTTP 端口**给用户，仅暴露 `:8083/metrics` 给 Prometheus + `:9083/grpc` 给 Manager 内控制面。
3. Engine 是**水平无状态**服务，N 副本由 Kafka ConsumerGroup Rebalance 自动分片，单副本挂掉不丢消息。
4. 所有规则 / 模型 / 配置在进程内按 `tenant_id` 分桶加载，**禁止全局变量缓存**跨租户数据。
5. `mode` 字段必须在每条告警中显式声明，**禁止默认 protect**。

### 1.3 与 v1.x 旧架构差异

| 维度 | v1.x（Consumer 内嵌检测） | v2.0（Engine 独立服务） |
|------|--------------------------|------------------------|
| 部署形态 | Consumer 一个进程兼任写入 + 检测，CPU 互抢 | Consumer 纯写入 / Engine 纯检测，独立扩缩 |
| 代码路径 | `internal/server/consumer/celengine|anomaly|storyline` | `internal/server/engine/{rule,sequence,ml,storyline,k8s,response,llm}` |
| ConsumerGroup | `mxsec-writers` 一个，订阅多 Topic 串行处理 | `mxsec-writers` + `mxsec-engine` 两个，Kafka 原生多消费组并行不冲突 |
| 告警写入路径 | 内存 `alarmService.Push()` 直接走 Consumer 写 MySQL | 产 `mxsec.engine.alert` 由 Consumer 持久化（解耦 + 回放可行） |
| K8s 检测 | `internal/server/manager/biz/kube_detector.go` 在 Manager 启动 | 搬入 Engine `k8s/audit_detector.go`，与主机检测同一管线 |
| 多租户 | 无 | 全管线按 `tenant_id` 分桶 |
| mode 字段 | 不存在 | 每条告警必带 |
| 横向扩展 | Consumer 同时承担 IO + CPU，扩副本浪费写入压力 | Engine CPU 密集，独立扩缩（可比 Consumer 多 2~4 倍副本） |

---

## 2. 与其他微服务的接口

```
                Kafka (mxsec-engine ConsumerGroup B)
                       ▲           │
       订阅 8 个 Topic │           │ 产 3 个 Topic
                       │           ▼
+--------+    gRPC   +-+---------+ Kafka  +----------+
| Manager| ────────► |  Engine   | ─────► | Consumer |───► MySQL/CK/Redis
|        | ◄──────── | (无状态 N)| ─────► | (writers)|
+--------+   规则/模型│          │ alert  +----------+
                推送 │          │ story
                     │          │ feedbk
                     │          │
                     │          │ gRPC (可选)
                     │          ▼
                     │     +----+------+
                     │     | LLMProxy  |
                     │     | 告警解释  |
                     │     | story 总结|
                     │     | 去重      |
                     │     +-----------+
                     │
                     │ gRPC 反向：告警下发处置 (protect 模式)
                     ▼
                +----+------+
                |AgentCenter|
                | sendCh    |  ── gRPC stream ──► Agent
                +-----------+
```

### 2.1 与 AgentCenter（AC）

| 方向 | 协议 | 用途 |
|------|------|------|
| AC → Engine | Kafka（间接） | AC 把 Agent 上行数据转发到 `mxsec.agent.*`，Engine 通过 ConsumerGroup B 订阅，**无直连** |
| Engine → AC | gRPC（mTLS + 内部 Bearer Token） | `protect` 模式下，Engine 把处置指令（IP 封禁 / 进程 kill / 端口封禁 / 文件隔离）通过 AC 下发到 Agent。proto: `engine.v1.ResponseService.Dispatch` |

设计上 Engine **不知晓 Agent 在哪个 AC 上**，由 AC 内置 SD 路由（参考 `internal/server/manager/sd/registry.go`），Engine 只声明目标 `agent_id` + `action`。

### 2.2 与 Consumer

完全通过 Kafka 解耦：

- Engine 消费 Agent 数据（与 Consumer 同源不冲突 —— Kafka 多 ConsumerGroup 互不影响 offset）
- Engine 产出 `mxsec.engine.alert` / `mxsec.engine.storyline` / `mxsec.engine.feedback`
- Consumer 通过 ConsumerGroup A `mxsec-writers` 订阅 Engine 产出，统一落 MySQL + ClickHouse

> Engine **零 SQL 写入**，这是判断 Engine 改动是否越权的金标准。

### 2.3 与 VulnSync

| 方向 | 协议 | 用途 |
|------|------|------|
| VulnSync → Engine | Kafka `mxsec.vuln.advisory` | Engine 接收漏洞情报后注入"漏洞匹配规则"，主机指纹（OS + pkg + CVE）变更时实时关联，产 `host_vulnerability` 类型 alert |
| Engine → VulnSync | gRPC（可选） | Engine 对低 confidence 的 CVE 反查 VulnSync 拿 advisory 元数据补全（如 EPSS / KEV 标签） |

### 2.4 与 LLMProxy

| 方向 | 协议 | 用途 |
|------|------|------|
| Engine → LLMProxy | gRPC（mTLS + 内部 Bearer） | 可选调用 `Complete` / `Stream` / `Embed`，用于：告警自然语言解释、Storyline 总结、相似告警去重（embedding cosine）|

调用前先看 `tenant.llm_enabled`，false 则直接跳过。LLMProxy 不可用时 Engine 不阻塞，告警仍按规则产出。详见 [`llmproxy-design.md`](llmproxy-design.md)。

### 2.5 与 Manager

| 方向 | 协议 | 用途 |
|------|------|------|
| Manager → Engine | gRPC（mTLS + 内部 Bearer） | `RuleService.Push`（规则 CRUD 实时推）/ `ModelService.Activate`（模型版本切换）/ `ModeService.SetTenantMode`（mode 切换）/ `FeedbackQuery`（误报标记同步）|
| Engine → Manager | Prometheus pull + `mxsec.engine.alert` 间接 SSE | Manager 不直接读 Engine 内存，从 Kafka / Prometheus 拿数据 |

控制面 gRPC proto 草案：

```protobuf
// api/proto/engine.proto
syntax = "proto3";
package mxsec.engine.v1;

service ControlService {
  // Manager 推送单条规则变更，Engine 收到后立即热更新该租户规则集
  rpc PushRule(PushRuleRequest) returns (PushRuleResponse);
  // 模型激活/灰度
  rpc ActivateModel(ActivateModelRequest) returns (ActivateModelResponse);
  // 租户 mode 切换
  rpc SetTenantMode(SetTenantModeRequest) returns (SetTenantModeResponse);
  // 健康探活
  rpc Health(HealthRequest) returns (HealthResponse);
}

service ResponseService {
  // Engine 主动调用 AC, protect 模式下发处置
  rpc Dispatch(DispatchRequest) returns (DispatchResponse);
}
```

---

## 3. 输入：Kafka ConsumerGroup B 订阅

### 3.1 订阅 Topic 清单

| Topic | DataType 范围 | 触发的引擎层 | 备注 |
|-------|---------------|-------------|------|
| `mxsec.agent.ebpf` | 3000–3010（EDR）+ 3020–3029（入侵专项） | 规则 + 序列 + ML + Storyline | 主战场，分区 12 |
| `mxsec.agent.events` | 6001 / 6002（FIM） | 规则 + Storyline | 篡改类高优先 |
| `mxsec.agent.baseline` | 8000–8004 | 规则 + ML（漂移） | 基线偏差告警 |
| `mxsec.agent.scanner` | 7000–7004 | 规则 + 漏洞匹配 | 病毒 / 漏洞扫描结果 |
| `mxsec.agent.asset` | 5050–5060 | 规则（变更检测） | 资产指纹变更产 alert |
| `mxsec.agent.heartbeat` | 1000 / 1001 | 序列（离线 / 异常重启） | 心跳缺失序列异常 |
| `mxsec.agent.remediation` | 9100–9299 | 规则（修复结果异常） | 修复任务执行结果反查 |
| `mxsec.vuln.advisory` | 12001–12099 | 漏洞匹配 | VulnSync 推送 |

> 完整 DataType 见 [`datatype-allocation.md`](datatype-allocation.md)。

### 3.2 ConsumerGroup 与并行度

- `group.id = mxsec-engine`
- `session.timeout.ms = 30000`、`heartbeat.interval.ms = 3000`、`max.poll.interval.ms = 300000`
- 每副本订阅全部 8 个 Topic，由 Kafka Rebalance 分配 Partition
- 单副本目标处理能力：单 EDR Partition ≈ 8k EPS，Engine 副本数 = ceil(总 EPS / 8k / 12 分区)
- offset 提交策略：**手动 commit + 批处理边界**，每批处理完成（持久化 + 产 alert 成功）才 commit；中途崩溃由 Kafka 重投保证 at-least-once

```go
// internal/server/engine/consumer/group.go (草案)
type EngineConsumer struct {
    group     sarama.ConsumerGroup
    pipeline  *Pipeline
    logger    *zap.Logger
}

func (c *EngineConsumer) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
    batch := make([]*kafka.MQMessage, 0, 256)
    flushTicker := time.NewTicker(200 * time.Millisecond)
    defer flushTicker.Stop()

    for {
        select {
        case msg, ok := <-claim.Messages():
            if !ok { return nil }
            var mq kafka.MQMessage
            if err := json.Unmarshal(msg.Value, &mq); err != nil {
                c.logger.Warn("decode failed", zap.Error(err))
                sess.MarkMessage(msg, "")
                continue
            }
            batch = append(batch, &mq)
            if len(batch) >= 256 {
                c.flush(sess, batch, msg)
                batch = batch[:0]
            }
        case <-flushTicker.C:
            if len(batch) > 0 {
                c.flush(sess, batch, nil)
                batch = batch[:0]
            }
        case <-sess.Context().Done():
            return nil
        }
    }
}
```

---

## 4. 输出：Kafka 产出 Topic

| Topic | DataType | Partitions | Retention | 消费方 |
|-------|---------|-----------|-----------|--------|
| `mxsec.engine.alert` | 11001–11099 | 12 | 7d | Consumer（持久化）+ Manager SSE（实时推 UI）+ 通知模块 |
| `mxsec.engine.storyline` | 11100–11199 | 6 | 14d | Consumer（落 ClickHouse + MySQL）|
| `mxsec.engine.feedback` | 11900–11999 | 3 | 30d | Engine 自消费（关闭误报反馈环）+ 离线训练 Job 拉取 |

### 4.1 Alert 消息 schema

```json
{
  "alert_id": "alrt-2026060600001",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "hostname": "prod-app-01",
  "agent_id": "a-abcdef",

  "rule_id": "BRUTE_FORCE_SSH",
  "rule_source": "cel|sigma|falco|tetragon|ml|sequence|k8s|vuln",
  "rule_version": "v3",
  "engine_layer": "rule",
  "severity": "high",
  "category": "credential_access",
  "att_ck": ["T1110.001"],

  "mode": "observe",
  "detected_at": "2026-06-06T10:23:45Z",
  "first_seen": "2026-06-06T10:23:40Z",
  "last_seen": "2026-06-06T10:23:45Z",

  "evidence": {
    "raw_event_id": "evt-uuid",
    "process": {"pid": 1234, "exe": "/usr/sbin/sshd", "cmdline": "sshd: invalid"},
    "network": {"src_ip": "192.0.2.45", "dst_port": 22},
    "fields": {"failed_count": "8", "window_sec": "60"}
  },

  "storyline_id": "story-uuid-or-null",
  "score": 0.86,
  "would_action": {
    "type": "ip_block",
    "target": "192.0.2.45",
    "duration_sec": 3600,
    "reason": "60s 内 SSH 登录失败 8 次"
  },
  "action": null,
  "action_result": null,

  "llm_summary": null,
  "feedback_hint": "如属业务跳板机，请加入白名单 BRUTE_FORCE_SSH:tenant=t-bank-a:cidr=192.0.2.0/24"
}
```

### 4.2 Storyline 消息 schema

```json
{
  "storyline_id": "story-2026060600007",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "started_at": "2026-06-06T10:20:00Z",
  "last_event_at": "2026-06-06T10:25:11Z",
  "stage_count": 5,
  "alert_ids": ["alrt-...01", "alrt-...02", "alrt-...05"],
  "att_ck_tactics": ["Initial Access", "Execution", "Persistence"],
  "att_ck_techniques": ["T1190", "T1059.004", "T1547.006"],
  "severity": "critical",
  "score": 0.92,
  "summary": "Web 漏洞利用 → Bash 反弹 → 写 cron persist",
  "mode": "observe"
}
```

### 4.3 Feedback 消息 schema

```json
{
  "feedback_id": "fb-uuid",
  "tenant_id": "t-bank-a",
  "alert_id": "alrt-...01",
  "rule_id": "BRUTE_FORCE_SSH",
  "label": "false_positive",
  "operator_id": "u-soc-007",
  "reason": "业务跳板机",
  "suggested_whitelist": {"src_ip_cidr": "192.0.2.0/24"},
  "submitted_at": "2026-06-06T11:00:00Z"
}
```

Engine 自消费 feedback：

- `false_positive` → 该规则 `tenant_id` 维度告警权重 ×0.9，连续 10 条同源 → 自动建议白名单 PR
- `true_positive` → 规则 `precision` 指标 +1，进入"protect 候选规则池"
- `uncertain` → 进人工复核队列（写 `mxsec.engine.feedback.review.dlq`）

---

## 5. 五大引擎层次

整体管线：

```
┌──────── Engine 单副本（CPU 密集） ─────────────────────────────────┐
│                                                                    │
│  Kafka Batch (256 msg/200ms)                                       │
│         │                                                          │
│         ▼                                                          │
│  ┌─ Decode + Tenant Routing ───────────────┐                      │
│  │  json.Unmarshal → MQMessage              │                      │
│  │  tenant_id = msg.tenant_id || lookup     │                      │
│  │  按 tenant 分桶到 worker pool             │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  ┌─ L1 Rule Layer ──────────────────────────┐                      │
│  │  CEL 引擎（cel-go）                       │                      │
│  │  Sigma → CEL transpiler                  │                      │
│  │  Falco yaml → CEL transpiler             │                      │
│  │  Tetragon TracingPolicy → CEL transpiler │                      │
│  │  Throttle / Whitelist / 进程树补全        │                      │
│  └─────────────────────────────────────────┘                      │
│         │ 命中 → alertCh                                            │
│         │ 未命中 → 下一层                                            │
│         ▼                                                          │
│  ┌─ L2 Sequence Layer ──────────────────────┐                      │
│  │  Markov 转移概率 (Redis state)           │                      │
│  │  n-gram 序列异常                         │                      │
│  │  滑动窗口端口扫描 / 暴力破解              │                      │
│  │  心跳缺失序列                            │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  ┌─ L3 ML Layer ────────────────────────────┐                      │
│  │  ONNX Runtime CPU 推理                   │                      │
│  │   - IForest (进程/网络异常)              │                      │
│  │   - LightGBM (基线漂移)                  │                      │
│  │   - MiniLM (命令行 Embedding)            │                      │
│  │   - 自训练 EDR Action Embedding          │                      │
│  │  Tenant 独立模型版本 + 灰度               │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  ┌─ L4 Graph / Storyline Layer ─────────────┐                      │
│  │  按 host_id + 进程树 + 时间窗聚合          │                      │
│  │  跨主机 lateral movement 关联              │                      │
│  │  ATT&CK 战术映射                         │                      │
│  │  评分 + 严重度提升                       │                      │
│  │  可选：LLMProxy 总结                     │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  ┌─ L5 K8s Detection Layer ─────────────────┐                      │
│  │  Audit Event 规则（从 Manager 搬入）      │                      │
│  │  Pod / Workload / Network / RBAC 检查    │                      │
│  │  违规 → 标准 alert + att_ck 标签         │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  ┌─ Response Layer ─────────────────────────┐                      │
│  │  mode=observe → would_action JSON         │                      │
│  │  mode=protect → AC gRPC Dispatch          │                      │
│  │  审计写 audit_log via Kafka               │                      │
│  └─────────────────────────────────────────┘                      │
│         │                                                          │
│         ▼                                                          │
│  Producer → Kafka                                                  │
│    mxsec.engine.alert / storyline / feedback                       │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

### 5.1 L1 规则层（Rule）

**实现来源**：`internal/server/consumer/celengine/` 整体搬入 `internal/server/engine/rule/`。

- CEL 表达式编译缓存（`map[ruleID]cel.Program`）
- 支持 `sigma2cel` / `falco2cel` / `tetragon2cel` 三个转换器，统一规则中台
- Throttle：同 `rule_id + host_id` 在窗口内仅产 1 条告警（参考 `celengine/throttle.go`）
- Whitelist：CEL 白名单优先评估，命中即跳过（参考 `celengine/whitelist.go`）
- 进程树补全：通过 `proctree.go` 注入 `parent_exe / grandparent_exe` 字段
- Scan detector：端口扫描专用滑动窗口（来自 `celengine/scan_detector.go`，性质上跨 L1/L2）

规则示例：

```yaml
# /etc/mxsec/engine/rules/T1059.004-bash-reverse-shell.yaml
rule_id: T1059_004_BASH_REVSHELL
source: cel
severity: critical
category: execution
att_ck: ["T1059.004", "T1071.001"]
applies_to_data_types: [3000, 3001]
tenant_filter: ["*"]            # * = 所有租户
mode_override: ""                # 留空 = 遵从全局 / 租户级 mode
expression: |
  data_type == 3000
  && fields["exe"].endsWith("/bash")
  && fields["cmdline"].contains("/dev/tcp/")
  && proctree.parent_exe != "/usr/bin/login"
throttle:
  window_sec: 60
  max_alerts: 1
would_action:
  type: kill_process
  target_pid_field: fields.pid
  reason: "Bash 反弹 shell 模式"
```

详细规则结构、Sigma / Falco / Tetragon 转换映射见 [`falco-sigma-integration.md`](falco-sigma-integration.md)。

### 5.2 L2 序列层（Sequence）

**实现来源**：`internal/server/consumer/celengine/sequence.go` 重构，`internal/server/consumer/anomaly/detector.go`（行为相关）拆出序列部分。

- Markov 状态转移：每 host_id 进程 → 进程 链上算 P(child|parent)，低概率转移 + 严重 → 告警
- n-gram 命令序列：进程 cmdline 切 3-gram，与租户 baseline gram 集差异度 > 阈值
- 滑动窗口端口扫描：同 src_ip 60s 内 dst_port 唯一数 > N
- 暴力破解：同 (src_ip, user) 失败次数累加（参考 ref/04-运行时.md §5.2 `bruteforce.Detector` 草案）
- 心跳序列：30s × 3 缺失 → 离线序列异常

中间状态走 Redis：

| Key | TTL | 用途 |
|-----|-----|------|
| `mxsec:seq:{rule_id}:{tenant}:{host_id}` | rule.Window | 滑动窗口计数 |
| `mxsec:seq:bf:{tenant}:{src_ip}:{user}` | 600s | 暴力破解累加 |
| `mxsec:seq:markov:{tenant}:{host_id}` | 24h | Markov 状态机 |

> Markov 状态机离线训练在 Manager 训练 Job，输出 transition matrix JSON 推到 Engine（gRPC `ControlService.PushModel`）热加载，详见 [`ml-models.md`](ml-models.md)。

### 5.3 L3 ML 层（机器学习推理）

**实现来源**：`internal/server/consumer/anomaly/` 升级为 ONNX 推理服务，IForest 保留为 fallback。

- 运行时：ONNX Runtime CPU 1.17+（CGO bridge，纯 Go 调用）
- 模型仓库：`/var/lib/mxsec/engine/models/{tenant}/{model_id}/{version}/model.onnx`
- 加载策略：进程启动时仅加载已激活模型；Manager 调 gRPC `ActivateModel` 触发热加载
- 灰度：同 model_id 允许 v1（稳定）+ v2（candidate）并行，按 `host_id` 哈希分流 5/25/100%
- 推理 fallback：ONNX runtime 不可用时回落到内置 Go IForest（`consumer/anomaly/iforest.go`），保证基本检测可用

模型清单与数据规格见 [`ml-models.md`](ml-models.md)。10 个开源模型默认开关、模型大小、推理延迟（P95 ≤ 50ms）在该文档定义。

### 5.4 L4 图层（Storyline）

**实现来源**：`internal/server/consumer/storyline/engine.go` + `kube_*` 整体搬入 `internal/server/engine/storyline/`。

- 单主机攻击链：按 `host_id` 维度 + 进程树深度 + 30min 时间窗聚合
- 跨主机横移：基于 `src_ip / ssh_session / mountpoint` 关联两个 host 的告警序列
- ATT&CK 映射：每条规则在 yaml 中声明 `att_ck`，Storyline 自动汇总 tactics
- 评分：基础分 = 各 alert 严重度加权和；增益 = 阶段数 × 0.1（≤ 1.0）
- LLM 总结（可选）：调用 LLMProxy `Complete`，prompt 注入"按 ATT&CK 描述这条攻击链"，结果填 `summary` 字段；LLMProxy 不可用则填 stage list 标题
- 严重度提升：单条 alert high，关联到 5 阶段链路自动升 critical

刷写策略：每 5s flush 一次，story 关闭条件 = 30min 无新事件 / 显式 close 信号。

### 5.5 L5 K8s 检测层

**实现来源**：`internal/server/manager/biz/kube_detector.go` / `kube_baseline_check*.go` 整体搬入 `internal/server/engine/k8s/`。

- 输入：Kafka `mxsec.agent.kube` 或 Manager Audit 转发的 `model.AuditEvent`
- 规则：Pod 特权 / hostPath / hostNetwork / SA token mount / NetworkPolicy 缺失 / RBAC ClusterAdmin 滥用 / 工作负载副本零等
- Admission Webhook 配合：`observe` 模式 Engine 仅产 alert，Webhook dry-run 仅 warn；`protect` 模式 Engine 输出 verdict 走 Webhook deny
- K8s 规则与主机规则共用同一 `EngineRule` 表 + 同一 mode 字段优先级

---

## 6. 多租户隔离

### 6.1 三段隔离

```
[1] 消息层：MQMessage.tenant_id 必填
       │  Engine 拒绝 tenant_id 缺失的消息，丢 DLQ
       ▼
[2] 引擎层：按 tenant 独立持有 RuleSet / ModelSet / Config
       │  tenantBucket map[tenantID]*EngineTenantCtx
       ▼
[3] 状态层：Redis key 强制带 {tenant_id} 段
        mxsec:seq:{rule_id}:{tenant_id}:{host_id}
        mxsec:ml:embedding:{tenant_id}:{hash}
```

### 6.2 tenant 上下文结构

```go
// internal/server/engine/tenant/context.go
type TenantCtx struct {
    TenantID   string
    Mode       string                  // observe / protect (该租户默认)
    Rules      *rule.Set               // 规则集（含 tenant_filter 过滤后）
    Models     map[string]*ml.Model    // model_id → 当前激活版本
    Whitelist  *rule.Whitelist
    Throttle   *rule.Throttler
    Config     *Config                 // ml.enabled / llm.enabled / 阈值
    LLMClient  llmproxy.Client         // 可空（llm.enabled=false）
    LastReload time.Time
}

type Registry struct {
    mu      sync.RWMutex
    tenants map[string]*TenantCtx
    db      *gorm.DB                   // read-only DSN
    rdb     *redis.Client
    logger  *zap.Logger
}

func (r *Registry) Get(tenantID string) (*TenantCtx, error) { /* ... */ }
func (r *Registry) Reload(tenantID string) error            { /* gRPC 推送时调 */ }
```

### 6.3 跨租户穿越防护

- `Pipeline.Process(ctx, msg)` 首句强制 `if msg.TenantID == "" { return errMissingTenant }`
- 引擎不存在跨租户聚合需求；Storyline 严格限定在 `(tenant_id, host_id)` 内
- 单元测试用例：构造 tenant A 上报的事件 + tenant B 的规则集 → 预期 0 命中
- Lint 规则：禁止 `tenantBucket` map 通过 `range` 全量遍历产 alert

详见 [`multi-tenant.md`](multi-tenant.md)。

---

## 7. mode=observe / protect 行为差异

### 7.1 决策优先级

`规则级 mode_override > 主机标签 mode > 租户 default_mode > 全局 default_mode`

在 Engine 内部实现：

```go
// internal/server/engine/mode/resolver.go
func (r *Resolver) Resolve(tenantID, hostID, ruleID string) string {
    if m, ok := r.ruleOverride(ruleID); ok { return m }
    if m, ok := r.hostLabelOverride(tenantID, hostID); ok { return m }
    if m, ok := r.tenantDefault(tenantID); ok { return m }
    return r.globalDefault  // "observe"
}
```

### 7.2 行为表

| 步骤 | observe | protect |
|------|---------|---------|
| 检测 | 全功能 | 全功能 |
| 产 alert | ✅，`mode=observe` | ✅，`mode=protect` |
| `would_action` 字段 | ✅ 填充 | `null` |
| `action` 字段 | `null` | ✅ 填充 |
| 调用 AC `Dispatch` gRPC | ❌ 不调 | ✅ 调 |
| Storyline 评分 | 正常 | 正常 |
| Admission Webhook 输出 | `allow_with_warning` | `deny` 或 `allow` |
| Prometheus `mxsec_engine_actions_executed_total` | 0 | + 1 |
| Prometheus `mxsec_engine_actions_would_total` | + 1 | 0 |

### 7.3 protect 模式下发

```go
// internal/server/engine/response/dispatcher.go
func (d *Dispatcher) Dispatch(ctx context.Context, alert *Alert) error {
    if alert.Mode != "protect" {
        d.metrics.WouldAction.WithLabelValues(alert.TenantID, alert.WouldAction.Type).Inc()
        return nil
    }
    cmd := buildAgentCommand(alert.WouldAction)
    cmd.AgentID = alert.AgentID
    cmd.Idempotency = alert.AlertID  // 防抖
    if _, err := d.ac.Dispatch(ctx, cmd); err != nil {
        d.metrics.ActionFailed.WithLabelValues(alert.TenantID, cmd.Type).Inc()
        return fmt.Errorf("dispatch: %w", err)
    }
    alert.Action = cmd
    alert.ActionResult = &ActionResult{Status: "pending_ack", DispatchedAt: time.Now()}
    return nil
}
```

> Agent 执行 ACK 通过 `mxsec.agent.command-ack` 回流，Consumer 写回 alert 表更新 `action_result`，Engine 不持有 ACK 状态。

---

## 8. 配置示例 YAML

```yaml
# /etc/mxsec/engine.yaml
service:
  name: mxsec-engine
  http_addr: ":8083"          # 仅 /metrics + /healthz
  grpc_addr: ":9083"          # 控制面 gRPC（Manager 调用）
  log_level: info
  log_format: json

mode:
  global_default: observe
  # 租户级 / 主机标签 / 规则级 mode 由 Manager 推送，运行时持有

kafka:
  brokers:
    - "kafka-0:9092"
    - "kafka-1:9092"
    - "kafka-2:9092"
  consumer_group: mxsec-engine
  session_timeout_sec: 30
  heartbeat_interval_sec: 3
  max_poll_interval_sec: 300
  fetch_min_bytes: 1024
  fetch_max_wait_ms: 200
  topics:
    - mxsec.agent.ebpf
    - mxsec.agent.events
    - mxsec.agent.baseline
    - mxsec.agent.scanner
    - mxsec.agent.asset
    - mxsec.agent.heartbeat
    - mxsec.agent.remediation
    - mxsec.vuln.advisory
  producer:
    acks: all
    compression: snappy
    max_message_bytes: 4194304
    flush_frequency_ms: 200
    flush_max_messages: 256

mysql_readonly:               # 仅用于初始加载规则 / 租户配置
  dsn: "mxsec_ro:***@tcp(mysql:3306)/mxsec?parseTime=true"
  max_open: 8

redis:
  addrs: ["redis-sentinel:26379"]
  master: mxsec
  password: "***"
  pool_size: 64

rule:
  reload_interval_sec: 30
  cel_program_cache: 4096
  whitelist_enabled: true
  throttle:
    default_window_sec: 60
    default_max_alerts: 1

sequence:
  markov_min_samples: 1000
  ngram_size: 3
  port_scan:
    window_sec: 60
    unique_ports_threshold: 30
  bruteforce:
    window_sec: 60
    failed_threshold: 5
    auto_block_ttl_sec: 3600

ml:
  enabled: true
  runtime: onnx
  models_dir: "/var/lib/mxsec/engine/models"
  inference_timeout_ms: 50
  fallback_on_runtime_error: true
  canary:
    enabled: true
    candidate_traffic_pct: 5

storyline:
  flush_interval_sec: 5
  story_idle_close_sec: 1800
  enable_lateral_movement: true
  llm_summary:
    enabled: false           # llm.enabled=true 时才生效
    max_chars: 240

k8s:
  enabled: true
  rules_dir: "/etc/mxsec/engine/k8s-rules"
  admission_dryrun_observe: true

llmproxy:
  enabled: false             # 全局默认关闭
  endpoint: "llmproxy:9091"
  tls_cert: "/etc/mxsec/certs/engine.crt"
  tls_key:  "/etc/mxsec/certs/engine.key"
  internal_token_env: "MXSEC_LLM_INTERNAL_TOKEN"
  timeout_ms: 8000
  cache_ttl_sec: 86400

agentcenter:
  endpoints: ["ac-0:9080","ac-1:9080"]
  tls_cert: "/etc/mxsec/certs/engine.crt"
  tls_key:  "/etc/mxsec/certs/engine.key"
  internal_token_env: "MXSEC_AC_INTERNAL_TOKEN"
  dispatch_timeout_ms: 5000

manager:
  endpoints: ["manager-0:9080","manager-1:9080"]
  tls_cert: "/etc/mxsec/certs/engine.crt"
  tls_key:  "/etc/mxsec/certs/engine.key"

metrics:
  path: /metrics
  port: 8083

tenant_isolation:
  refuse_missing_tenant_id: true
  default_tenant_id_fallback: ""    # 留空 = 严格模式（推荐生产）
```

---

## 9. Go 接口骨架

```go
// internal/server/engine/engine.go
package engine

import (
    "context"
    "github.com/imkerbos/mxsec-platform/internal/server/common/kafka"
    "github.com/imkerbos/mxsec-platform/internal/server/engine/tenant"
    "go.uber.org/zap"
)

// EngineProvider 是 Engine 整体对外契约
type EngineProvider interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    ReloadTenant(tenantID string) error
    Health() Health
}

// Pipeline 单条消息 5 层管线
type Pipeline interface {
    Process(ctx context.Context, msg *kafka.MQMessage) ([]*Alert, error)
}

// 规则层
type RuleProvider interface {
    Evaluate(ctx context.Context, tc *tenant.TenantCtx, dataType int32, fields map[string]string) []RuleHit
    Reload(tenantID string) error
    Throttled(ruleID, hostID string) bool
    Whitelisted(ruleID string, fields map[string]string) bool
}

// 序列层
type SequenceProvider interface {
    Update(ctx context.Context, tc *tenant.TenantCtx, hostID string, dataType int32, fields map[string]string) []SeqHit
}

// ML 层
type MLModel interface {
    ID() string
    Version() string
    Predict(ctx context.Context, features []float32) (score float32, label string, err error)
    InputDims() int
    Active() bool
}

type MLProvider interface {
    Infer(ctx context.Context, tc *tenant.TenantCtx, modelID string, features []float32) (*Prediction, error)
    Activate(tenantID, modelID, version string) error
    Health() map[string]string
}

// Storyline 图层
type StorylineProvider interface {
    Ingest(ctx context.Context, tc *tenant.TenantCtx, hostID string, alert *Alert) (storylineID string, err error)
    Flush(ctx context.Context) error
}

// K8s 检测层
type KubeDetector interface {
    DetectAudit(ctx context.Context, tc *tenant.TenantCtx, evt *AuditEvent) []*Alert
}

// 响应层
type ResponseDispatcher interface {
    Dispatch(ctx context.Context, alert *Alert) error
}

// LLM 客户端（可选）
type LLMClient interface {
    Summarize(ctx context.Context, tenantID, alertChain string) (string, error)
    Embed(ctx context.Context, tenantID, text string) ([]float32, error)
    DedupHint(ctx context.Context, tenantID string, embeddings [][]float32) (groups []int, err error)
}

// Alert / Storyline / RuleHit / SeqHit / Prediction 等结构略
```

### 9.1 主程序骨架

```go
// cmd/server/engine/main.go
package main

func main() {
    cfg := mustLoad("/etc/mxsec/engine.yaml")
    logger := mustLogger(cfg.Service.LogLevel, cfg.Service.LogFormat)

    redisCli := mustRedis(cfg.Redis)
    dbRO     := mustGorm(cfg.MySQLReadonly)

    tenantReg := tenant.NewRegistry(dbRO, redisCli, logger)
    if err := tenantReg.WarmAll(); err != nil { logger.Fatal("warm tenants", zap.Error(err)) }

    rule  := rule.New(tenantReg, cfg.Rule, logger)
    seq   := sequence.New(redisCli, cfg.Sequence, logger)
    ml    := ml.New(cfg.ML, logger)
    story := storyline.NewEngine(redisCli, cfg.Storyline, logger)
    k8s   := k8sdet.New(cfg.K8s, logger)

    acClient    := acclient.New(cfg.AgentCenter, logger)
    llmClient   := llmproxy.New(cfg.LLMProxy, logger)
    dispatcher  := response.NewDispatcher(acClient, logger)

    pipeline := pipeline.New(tenantReg, rule, seq, ml, story, k8s, dispatcher, llmClient, logger)

    producer := kafka.NewAsyncProducer(cfg.Kafka.Brokers, cfg.Kafka.Producer, logger)
    consumer := consumer.New(cfg.Kafka, pipeline, producer, logger)

    ctrl := control.NewGRPCServer(tenantReg, ml, logger, cfg.Service.GRPCAddr)

    go ctrl.Serve()
    go consumer.Run(rootCtx)
    go startMetrics(cfg.Metrics)

    awaitSignal()
    consumer.Stop()
    producer.Close()
    ctrl.Stop()
}
```

---

## 10. 性能 SLO

| 指标 | 目标 | 测量点 |
|------|------|--------|
| 端到端告警延迟 P95（Agent 上报 → `mxsec.engine.alert` 写出） | **≤ 5s** | Kafka Header 注入 `received_at`，alert 产出时差 |
| 端到端告警延迟 P99 | ≤ 10s | 同上 |
| 单副本吞吐 | ≥ 10k msg/s（规则层平均 800µs/msg） | 内部 benchmark |
| 规则评估 P95 | ≤ 1.2ms / 条 | `mxsec_engine_rule_eval_seconds` |
| ML 推理 P95 | ≤ 50ms / 条 | `mxsec_engine_ml_inference_seconds` |
| Storyline flush 周期 | 5s | `mxsec_engine_storyline_flush_seconds` |
| 误报率（磨合后） | ≤ 2% | UI feedback 月度 |
| 告警准确率（磨合后） | ≥ 85% | UI feedback 月度 |
| 内存稳态 | < 4 GiB / 副本（含 200 模型 + 50 租户） | RSS |
| CPU 稳态 | < 6 核（满载 < 8 核） | host cgroup |
| Kafka Consumer Lag P99 | ≤ 30s | `kafka_consumergroup_lag` |

> 5s 端到端是 SOC 实战门槛：用户在 UI 看到告警时事件最迟发生在 5s 前；超出 5s 则"事件发生—告警可见"窗口允许攻击者完成多步横移。

### 10.1 关键 Prometheus 指标

```
mxsec_engine_messages_consumed_total{topic, tenant}
mxsec_engine_messages_decoded_failed_total{topic}
mxsec_engine_pipeline_latency_seconds{stage}        # decode/rule/seq/ml/story/dispatch
mxsec_engine_rule_eval_seconds_bucket{rule_id}
mxsec_engine_rule_hits_total{rule_id, tenant, mode}
mxsec_engine_seq_hits_total{rule_id, tenant}
mxsec_engine_ml_inference_seconds_bucket{model_id, tenant}
mxsec_engine_ml_score_histogram{model_id, tenant}
mxsec_engine_storyline_active{tenant}
mxsec_engine_alerts_emitted_total{tenant, mode, severity, source}
mxsec_engine_actions_executed_total{tenant, action_type, status}   # protect
mxsec_engine_actions_would_total{tenant, action_type}              # observe
mxsec_engine_feedback_consumed_total{tenant, label}
mxsec_engine_tenant_count
mxsec_engine_rule_count{tenant}
mxsec_engine_model_active{tenant, model_id, version}
mxsec_engine_precision{tenant, rule_id}
mxsec_engine_recall{tenant, rule_id}
mxsec_engine_fp_rate{tenant, rule_id}
mxsec_engine_llm_calls_total{tenant, purpose, status}
mxsec_engine_grpc_dispatch_seconds_bucket{target=agentcenter}
mxsec_engine_kafka_lag{topic, partition}
```

---

## 11. 横向扩展

### 11.1 Kafka Rebalance 驱动

- ConsumerGroup `mxsec-engine` 内 N 副本均分 partition
- 8 个 Topic、平均 8 分区，理论上 64 个 partition；N=8 时每副本独占 8 partition
- Engine 扩容步骤：
  1. 灰度发布新副本（K8s rolling update）
  2. Kafka 自动触发 Rebalance（30s 内完成）
  3. 老副本释放部分 partition，新副本接管
  4. Prometheus `kafka_consumergroup_lag` 验证 Lag ≤ 30s

### 11.2 状态依赖

| 状态 | 位置 | 多副本一致性 |
|------|------|-------------|
| 规则 / 模型 / 租户配置 | 进程内存 + Manager gRPC 推送 | Manager 同时推送所有副本，最终一致 |
| Markov / 序列窗口 / 暴破计数 | Redis | 共享，强一致 |
| Storyline | Redis（聚合状态）+ 单副本持有 in-flight stories（按 host_id 哈希） | host_id 哈希决定 owner 副本，避免冲突 |
| Throttle | 进程内（host_id × rule_id 组合稀疏，可接受副本间重复阈值） | 弱一致，可容忍 |

### 11.3 容量上限规划

| 档位 | Agent | Engine 副本 | 备注 |
|------|-------|------------|------|
| Demo | < 500 | 1 | docker-compose 默认 |
| 小规模 | 500–2k | 2 | HPA 上限 |
| 中规模 | 2k–10k | 4–8 | Kafka 分区相应扩到 24 |
| 大规模 | 10k–50k | 16–32 | Kafka 多集群 + 租户级 ConsumerGroup |
| 极限 | 50k–300k | 64+ | Federation；按租户切多 Engine 集群 |

---

## 12. 处理管线详细图（ASCII）

```
                Kafka Cluster (mxsec-engine group)
                       │
   ┌──────────────────-┼──────────────────┐
   │                   │                  │
   ▼                   ▼                  ▼
┌──────────┐     ┌──────────┐     ┌──────────┐
│Engine #1 │     │Engine #2 │ ... │Engine #N │   水平副本
└────┬─────┘     └──────────┘     └──────────┘
     │
     │  单副本内部
     ▼
┌────────────────────────────────────────────────────────┐
│ FetchLoop  ── batch 256 / 200ms ──► Decoder            │
│                                       │                │
│                                       ▼                │
│                              tenant_id 路由            │
│                                       │                │
│   ┌─── worker pool per tenant (sharded by host_id) ──┐ │
│   │                                                  │ │
│   │  ┌─ L1 Rule (CEL/Sigma/Falco/Tetragon) ────┐    │ │
│   │  │   whitelist → cel.Eval → throttle       │    │ │
│   │  └──────┬──────────┬─────────────────────-─┘    │ │
│   │         │alert     │未命中                       │ │
│   │         ▼          ▼                             │ │
│   │      alertCh    ┌─ L2 Sequence ──────────────┐  │ │
│   │                 │  redis state + markov +    │  │ │
│   │                 │  ngram + scan + bf         │  │ │
│   │                 └──────┬──────────┬─────────-┘  │ │
│   │                        │alert     │             │ │
│   │                        ▼          ▼             │ │
│   │                     alertCh    ┌─ L3 ML ─────┐  │ │
│   │                                │ ONNX infer  │  │ │
│   │                                │ fallback IF │  │ │
│   │                                └──┬──────┬───┘  │ │
│   │                                   │alert │      │ │
│   │                                   ▼      ▼      │ │
│   │                                alertCh   pass   │ │
│   │                                                  │ │
│   │  ┌─ L4 Storyline 聚合 ◄─── alertCh ──────┐      │ │
│   │  │ 按 host_id / 进程树 / 时间窗 聚合      │      │ │
│   │  │ 跨主机 lateral movement 关联            │      │ │
│   │  │ ATT&CK tactics 汇总                    │      │ │
│   │  │ 严重度提升 + 评分                       │      │ │
│   │  │ optional LLMProxy summary               │      │ │
│   │  └──────┬─────────────────────────────────-┘     │ │
│   │         │                                          │
│   │  ┌─ L5 K8s (audit) ◄─ 仅 K8s audit data type ──┐  │ │
│   │  │   pod/rbac/workload/network rule           │   │ │
│   │  └──────┬─────────────────────────────────────┘   │ │
│   │         │                                          │
│   │  ┌─ Response Layer ─────────────────────────────┐ │ │
│   │  │ mode=protect → AC.Dispatch + action 字段填   │ │ │
│   │  │ mode=observe → would_action 字段填           │ │ │
│   │  └──────┬─────────────────────────────────────-─┘ │ │
│   └─────────│──────────────────────────────────────────┘ │
│             │                                            │
│             ▼                                            │
│     AsyncProducer → Kafka                                │
│       mxsec.engine.alert                                 │
│       mxsec.engine.storyline                             │
│             │                                            │
│             ▼                                            │
│     offset commit (after produce success)                │
└────────────────────────────────────────────────────────────┘
             │
             ▼
       Kafka Cluster (consumer group A: mxsec-writers)
             │
             ▼
       Consumer → MySQL + ClickHouse + Redis
                                       │
                                       └─► Manager SSE
                                       └─► 通知模块
```

---

## 13. 反馈闭环与磨合

监听阶段（observe 默认 90 天）的核心价值是沉淀。Engine 通过 `mxsec.engine.feedback` Topic 实现：

| 输入 | 处理 | 输出 |
|------|------|------|
| UI 标记 `false_positive` | 该 rule × tenant 计数 +1；连续 10 条同源自动生成"白名单建议"草案 | Manager UI 提示运营审批 |
| UI 标记 `true_positive` | 规则进入 `protect_candidate` 池；30d 持续 `fp_rate ≤ 2%` 触发提示"可建议切 protect" | UI Banner |
| UI 标记 `uncertain` | 入 `mxsec.engine.feedback.review.dlq`，运营人审 | 人工复核 |
| 自动磨合指标 | `mxsec_engine_precision / recall / fp_rate` 持续上报 | Prometheus + Grafana 仪表盘 |
| 离线训练 Job | Manager 训练 Job 拉 `mxsec.engine.feedback` + ClickHouse 历史样本，输出 ONNX + transition matrix | gRPC `ActivateModel` 灰度推 5% → 25% → 100% |

详见 [`operating-modes.md`](operating-modes.md) §9 数据磨合反馈通道。

---

## 14. 失败处理与容错

| 故障 | 表现 | Engine 行为 |
|------|------|------------|
| Kafka 不可用 | Consumer claim 失败 | 副本反复退避重连；Producer 异常时**告警直接写本地落盘 jsonl**（`/var/lib/mxsec/engine/alert-fallback/`），恢复后 replayer 回填 |
| Redis 不可用 | 序列层失败 | Markov / 序列降级为"仅规则 + ML"，记录 `mxsec_engine_redis_unavailable_total` 指标 |
| AC 不可用（protect） | Dispatch 超时 | 重试 3 次后告警仍发出，`action_result.status = "ac_unreachable"` |
| LLMProxy 不可用 | Storyline summary 缺失 | `summary` 回落到拼接 stage list，不阻塞主链路 |
| ONNX 模型加载失败 | 推理失败 | 回落到内置 Go IForest；活跃模型表 `active = false` 上报 |
| 单条消息毒丸 | panic | recover + 写 `mxsec.agent.{topic}.dlq` + 计数器 |
| Manager 不可用 | 规则推送阻塞 | Engine 持续用当前内存中规则集，30s 后主动尝试重连；不影响检测 |
| 租户配置缺失 | tenantReg.Get 返回 errMissingTenant | 消息进 DLQ，`mxsec_engine_tenant_missing_total` +1 |

---

## 15. 与对标产品差异

| 维度 | mxsec Engine | 青藤万象 / 蜂巢 |
|------|--------------|----------------|
| 部署形态 | 独立微服务，K8s HPA 水平扩展 | Server 集群内嵌检测，含资产/检测/合规等多模块单体 |
| 检测层级 | 规则 + 序列 + ML + Storyline + K8s 五层显式 | 入侵检测 6 模块 + 容器行为模型（隐式分层） |
| 规则中台 | CEL + Sigma + Falco + Tetragon 四源统一转 CEL | 自研规则 DSL + 行为模型，不开放 Sigma/Falco |
| ML 推理 | 本地 ONNX CPU 主导 + LLM 可选 | 行为基线学习（云端聚合 + 本地异常告警） |
| 多租户 | from-day-1 全管线分桶 | 单部署单租户为主（KA 多套部署） |
| 运行模式 | 默认 observe，6 门槛 + 4 级灰度切 protect | 检测+部分自动响应出厂，模块开关 |
| 反馈闭环 | UI feedback → Kafka → 离线训练 → 灰度模型 | UI 标注 → 模型重训（云端） |
| 开源/可定制 | Apache-2.0，规则/模型可客户改 | 闭源商业 |

---

## 16. 实现计划（v2.0 重构 Sprint）

| Sprint | 内容 | 验收 |
|--------|------|------|
| S0（1 周） | proto 草案 + 配置 + 目录骨架；空 Engine 进程能跑 | `make build-engine` + 启动 + `/healthz` 200 |
| S1（2 周） | L1 规则层从 Consumer 搬入，跑通 CEL + throttle + whitelist | 老规则集 ≥ 200 条全部通过单测 |
| S2（2 周） | L2 序列 + L3 ML 整合，IForest fallback + ONNX runtime 接通 | benchmark 8k EPS / 副本 |
| S3（2 周） | L4 Storyline + L5 K8s 从 Manager 搬入 | 攻击链多阶段 e2e 测试 |
| S4（1 周） | 多租户 tenantBucket + gRPC ControlService | 100 租户压测 |
| S5（1 周） | mode 决策 + Response Dispatcher + AC gRPC 集成 | observe/protect 切换 e2e |
| S6（1 周） | LLMProxy 集成（可选）+ 反馈消费 + Prometheus 指标全 | SLO 全部达标 |
| S7（1 周） | Falco / Sigma / Tetragon 转换器接入 | 50 条社区规则导入跑通 |

---

## 17. 安全约束

- Engine 进程证书 = `engine.{ns}.svc` mTLS，与 Manager / AC / LLMProxy 互信
- 不持有任何客户 LLM API Key（由 LLMProxy 持有）
- 配置文件中 Redis / MySQL 密码必须从 `env` 或 K8s Secret 注入，禁止明文落 yaml
- 容器以 `nonroot` user 运行（uid=10001）
- 启动期校验 `tenant_isolation.refuse_missing_tenant_id=true`，false 仅允许 dev 环境
- Audit：每次 `protect` 模式 Dispatch 必须写 `mxsec.audit` Topic（含 `operator=engine`, `rule_id`, `action`, `target`）

---

## 18. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构总图 | [`architecture.md`](architecture.md) |
| 运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 检测细节（规则结构 / 字段映射） | [`engine-detection-design.md`](engine-detection-design.md) |
| EDR Agent 采集 | [`edr-agent-design.md`](edr-agent-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| Falco / Sigma / Tetragon 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| 漏洞模块设计 | [`vuln-module-design.md`](vuln-module-design.md) |
| 资产统一模型 | [`asset-model.md`](asset-model.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署指南 | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 原始评估（内部） | `ref/01-服务端架构.md` §3 / `ref/04-运行时.md` §5 |
