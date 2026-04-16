# MxSec 平台 V2 架构设计与实现说明：HA + Kafka + ClickHouse

本文档最初用于描述 MxSec 平台从单实例架构向高可用/可扩展架构的重构方案，当前同时作为 V1 → V2 演进的实现说明。

**最后更新**: 2026-04-16
**状态**: 控制面 HA、Kafka 解耦、Consumer、Manager SD 已落地；存储层完整容灾仍在生产加固阶段

> 说明：
>
> - 本文档中的“当前架构与问题”指 **V1 单体架构回顾**。
> - 本文档中的“目标架构”已基本成为当前项目的 **V2 实际架构**。
> - 若与旧部署文档存在差异，以 `README.md`、`docs/architecture.md` 和当前代码实现为准。

---

## 1. V1 架构回顾与重构动因

### 1.1 V1 数据流

```
Agent → gRPC BiDi Stream → AgentCenter(单实例) → 直接写 MySQL
                                                 → Prometheus(可选)
UI → Nginx → Manager(单实例) → MySQL 读写
```

### 1.2 核心问题

| 问题 | 影响 | 严重程度 |
|------|------|---------|
| AgentCenter 直接写 MySQL | AC 与存储强耦合，无法多实例（写冲突） | P0 |
| AgentCenter 单实例 | 宕机后全部 Agent 断连，任务中断 | P0 |
| Manager 单实例 | API 不可用，管理面中断 | P1 |
| MySQL 承载全部数据 | 高频数据（指标/FIM）写入压力大，查询慢 | P1 |
| 无服务发现 | Agent 硬编码 AC 地址，无法动态扩缩容 | P1 |
| 任务调度无分布式锁 | 多实例部署时任务重复分发 | P0 |
| 任务下发无可靠性保障 | Agent 断连时命令静默丢失，无补发机制 | P1 |

---

## 2. 目标架构

### 2.1 总览

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Server 端                                    │
│                                                                          │
│  ┌──────────┐    ┌──────────────────────────────────────────────────┐   │
│  │ UI/Nginx │───>│           Manager 集群 (HTTP :8080)              │   │
│  │  LB      │    │  ├─ Manager-1   ├─ Manager-2                    │   │
│  └──────────┘    │  ├─ 无状态，JWT 认证，Redis 共享缓存             │   │
│                  │  └─ 内嵌 SD 模块：AC 注册/健康探测/发现           │   │
│                  └─────────────┬────────────────────────────────────┘   │
│                                │ HTTP 管理调用 + 服务发现               │
│                                ↓                                        │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │          AgentCenter 集群 (gRPC :6751 + HTTP :8080)              │  │
│  │  ├─ AC-1 (weight=连接数)   ├─ AC-2 (weight=连接数)              │  │
│  │  ├─ gRPC: Agent 双向流通信                                        │  │
│  │  ├─ HTTP: Manager 管理接口 (/command, /command/batch, /health)   │  │
│  │  ├─ Kafka Producer: 按 Topic 分组异步写入                         │  │
│  │  ├─ 内存队列: Kafka 不可用时暂存，恢复后重放                       │  │
│  │  └─ 零数据库依赖 (不连 MySQL/ClickHouse)                         │  │
│  └─────────────┬─────────────────────────────────────────────────────┘  │
│                │                                                        │
│                ↓                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │                       Kafka 集群                                  │   │
│  │  mxsec.agent.heartbeat   (DataType 1000/1001, Retention: 24h)   │   │
│  │  mxsec.agent.events      (DataType 6001 FIM, Retention: 72h)    │   │
│  │  mxsec.agent.baseline    (DataType 8000~8004, Retention: 7d)    │   │
│  │  mxsec.agent.asset       (DataType 5050~5060, Retention: 7d)    │   │
│  │  mxsec.agent.command-ack (命令执行回包, Retention: 7d)           │   │
│  │  mxsec.agent.*.dlq       (Dead Letter Queue, 各 Topic 对应)      │   │
│  └─────────────┬────────────────────────────────────────────────────┘   │
│                ↓                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │         Consumer 服务 (独立部署，可多实例)                         │   │
│  │  消费 Kafka → 按 DataType 路由 → 幂等写入对应存储                 │   │
│  │  写入失败 → 进入 DLQ → 告警通知                                   │   │
│  └────────────┬──────────────────────┬─────────────────────────────┘   │
│               │                      │                                  │
│               ↓                      ↓                                  │
│  ┌─────────────────┐     ┌───────────────────────┐  ┌──────────────┐  │
│  │   MySQL 8.0+    │     │      ClickHouse        │  │    Redis     │  │
│  │  业务元数据      │     │  时序/事件/归档         │  │  SD 缓存     │  │
│  └─────────────────┘     └───────────────────────┘  │  分布式锁    │  │
│                                                      │  业务缓存    │  │
│                                                      └──────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
                         │ gRPC + mTLS
┌──────────────────────────────────────────────────────────────────────────┐
│                       Agent 端 (每台主机)                                 │
│  环形缓冲区 [2048] + 100ms 批量发送 + Snappy 压缩                        │
│  多地址服务发现（查 Manager SD）+ 指数退避无限重试                         │
└──────────────────────────────────────────────────────────────────────────┘
```

### 2.2 与 Elkeid 的关键差异

| 维度 | Elkeid | MxSec (本方案) | 理由 |
|------|--------|---------------|------|
| **存储** | MongoDB | MySQL + ClickHouse | MySQL 已有完整 ORM/迁移；ClickHouse 替代 MongoDB 做分析 |
| **服务发现** | 独立 SD 服务 | Manager 内嵌 SD 模块 | Manager 已有监控 UI，内嵌 SD 无需独立组件；远期可迁 etcd |
| **Kafka Topic** | 单 Topic `hids_svr` | 分组 5 Topic | 不同数据特征需独立 Retention/Partition 策略 |
| **任务分发** | Redis PubSub | 持久化 + 心跳补发 | 保证命令不丢，Agent 断连重连后自动恢复 |
| **Agent→AC 映射** | Manager 定时采集 → Redis | Consumer 消费心跳写入 | 解耦 AC，由 Consumer 维护，TTL 精确对齐心跳周期 |
| **对象池** | sync.Pool 复用 MQData | sync.Pool 复用 | 参照 Elkeid |
| **批量写入** | 15s/1000条 BulkWrite | 5s/500条 MySQL，10s/5000条 ClickHouse | ClickHouse 更适合大批量低频写 |
| **负载均衡** | 最小连接数 | power-of-two-choices | 防止新 AC 上线时雪崩效应 |

---

## 3. 组件详细设计

### 3.1 AgentCenter 改造（核心变更）

**设计原则：AgentCenter 零数据库依赖，只做连接管理 + 数据转发。**

#### 3.1.1 双端口设计

```
AgentCenter
├── gRPC :6751  ← Agent 双向流通信（已有）
└── HTTP :8080  ← Manager 管理接口（当前实现共用 server.http.port）
    ├── POST /command          向单个 Agent 发送命令
    ├── POST /command/batch    向多个 Agent 批量发送命令
    ├── GET  /conn/stat        连接统计
    ├── GET  /conn/list        Agent 列表
    └── GET  /health           健康检查（供 Manager SD 探测）
```

#### 3.1.2 数据处理流（核心改动）

**改前**（当前）：
```go
func handlePackagedData(data) {
    switch dataType {
    case 1000: updateHostInMySQL(data)       // 直接写 MySQL
    case 8000: insertScanResult(data)        // 直接写 MySQL
    case 5050: insertAssetData(data)         // 直接写 MySQL
    case 6001: insertFIMEvent(data)          // 直接写 MySQL
    }
}
```

**改后**：
```go
func handlePackagedData(data *grpc.PackagedData, conn *Connection) {
    for _, record := range data.Records {
        // 1. 构建 Kafka 消息（对象池复用）
        mqMsg := mqMsgPool.Get().(*MQMessage)
        mqMsg.DataType    = record.DataType
        mqMsg.AgentID     = data.AgentId
        mqMsg.Body        = record.Data
        mqMsg.AgentTime   = record.Timestamp
        mqMsg.SvrTime     = time.Now().Unix()
        mqMsg.Hostname    = data.Hostname
        mqMsg.TraceID     = newTraceID() // 端到端追踪
        // ... 填充其他元信息

        // 2. 特殊处理（仅内存操作，不写数据库）
        switch record.DataType {
        case 1000: parseAgentHeartbeat(record, data, conn)   // 更新内存 Connection
        case 1001: parsePluginHeartbeat(record, data, conn)  // 更新内存插件状态
        }

        // 3. 按 DataType 路由到对应 Topic（异步非阻塞）
        topic := routeTopic(record.DataType)
        if err := kafkaProducer.SendWithKey(topic, data.AgentId, mqMsg); err != nil {
            // Kafka 不可用，写本地内存队列
            fallbackQueue.Enqueue(topic, mqMsg)
        }
    }
}

func routeTopic(dataType int32) string {
    switch {
    case dataType == 1000 || dataType == 1001:
        return "mxsec.agent.heartbeat"
    case dataType == 6001:
        return "mxsec.agent.events"
    case dataType >= 8000 && dataType <= 8004:
        return "mxsec.agent.baseline"
    case dataType >= 5050 && dataType <= 5060:
        return "mxsec.agent.asset"
    default:
        return "mxsec.agent.heartbeat" // 兜底
    }
}
```

#### 3.1.3 Kafka Producer 配置

```go
config := sarama.NewConfig()
config.Producer.Return.Successes = true
config.Producer.MaxMessageBytes = 4 * 1024 * 1024   // 4MB
config.Producer.Timeout = 6 * time.Second
config.Producer.Flush.Bytes = 4 * 1024 * 1024       // 4MB 触发 flush
config.Producer.Flush.Frequency = 10 * time.Second   // 10s 触发 flush
config.Producer.Retry.Max = 3
config.Producer.RequiredAcks = sarama.WaitForLocal   // acks=1

// 对象池
var mqMsgPool = &sync.Pool{
    New: func() interface{} { return &MQMessage{} },
}
```

#### 3.1.4 Kafka 写失败降级策略

```go
// 内存降级队列（有界，防止 OOM）
type FallbackQueue struct {
    ch      chan *pendingMsg   // 容量 10000
    running int32
}

// Kafka 不可用时，写降级队列
func (q *FallbackQueue) Enqueue(topic string, msg *MQMessage) {
    select {
    case q.ch <- &pendingMsg{topic, msg}:
        // 写入成功
    default:
        // 队列满，丢弃最旧数据（心跳类型可丢，基线结果类型告警）
        metrics.Inc("kafka_fallback_dropped")
    }
}

// Kafka 恢复后，后台 goroutine 重放
func (q *FallbackQueue) ReplayLoop(producer KafkaProducer) {
    for msg := range q.ch {
        for {
            if err := producer.Send(msg.topic, msg.data); err == nil {
                break
            }
            time.Sleep(1 * time.Second)
        }
    }
}
```

**与 Agent 侧缓冲的配合**：
- Agent 环形缓冲区 2048 条，100ms 发送间隔，短暂中断（< 几秒）Agent 侧自动补发
- AC 内存降级队列托底中等时长中断（秒级 ~ 分钟级）
- 长时间 Kafka 不可用（> 5 分钟）：触发告警，运维介入

#### 3.1.5 AC 向 Manager SD 注册

```go
// AC 启动时向 Manager 注册
func (ac *AgentCenter) registerToSD() {
    info := ACRegisterRequest{
        ID:       ac.instanceID,
        GRPCAddr: ac.cfg.GRPCAddr,
        HTTPAddr: ac.cfg.HTTPAddr,
        Version:  ac.version,
    }
    managerClient.Post("/api/v1/internal/ac/register", info)
}

// 每 15s 发送心跳（Manager 探测失败 3 次才判定下线，双保险）
func (ac *AgentCenter) heartbeatLoop() {
    ticker := time.NewTicker(15 * time.Second)
    for range ticker.C {
        managerClient.Post("/api/v1/internal/ac/heartbeat", ACHeartbeat{
            ID:        ac.instanceID,
            ConnCount: ac.connPool.Count(),
        })
    }
}

// 优雅关闭
func (ac *AgentCenter) deregister() {
    managerClient.Post("/api/v1/internal/ac/deregister", ACDeregisterRequest{
        ID: ac.instanceID,
    })
}
```

#### 3.1.6 连接管理

```go
type ConnPool struct {
    connections sync.Map           // AgentID → *Connection
    tokenChan   chan struct{}       // 连接令牌（最大 2000）
}

// 连接令牌机制，超出限制时 Agent 通过 SD 连其他 AC
func (p *ConnPool) AcquireToken() bool {
    select {
    case <-p.tokenChan: return true
    default: return false
    }
}

// Agent 连接注册（不写 Redis，由 Consumer 消费心跳后写）
func (p *ConnPool) Register(agentID string, conn *Connection) {
    p.connections.Store(agentID, conn)
}
```

#### 3.1.7 AC HTTP 管理接口

```go
// POST /command — 单 Agent 下发
func PostCommand(c *gin.Context) {
    var req CommandRequest // { agent_id, command }
    c.BindJSON(&req)
    err := connPool.SendCommand(req.AgentID, req.Command)
    if err != nil {
        c.JSON(404, gin.H{"error": "agent not connected to this instance"})
        return
    }
    c.JSON(200, gin.H{"status": "ok"})
}

// POST /command/batch — 批量下发（Manager 发起，减少 HTTP 调用次数）
func PostCommandBatch(c *gin.Context) {
    var req BatchCommandRequest // { agent_ids: [], command }
    c.BindJSON(&req)

    var sent, notFound []string
    var wg sync.WaitGroup
    var mu sync.Mutex

    // AC 内部并发下发，同步等待结果
    for _, agentID := range req.AgentIDs {
        wg.Add(1)
        go func(id string) {
            defer wg.Done()
            err := connPool.SendCommand(id, req.Command)
            mu.Lock()
            defer mu.Unlock()
            if err != nil {
                notFound = append(notFound, id)
            } else {
                sent = append(sent, id)
            }
        }(agentID)
    }
    wg.Wait()

    c.JSON(200, BatchCommandResponse{Sent: sent, NotFound: notFound})
}

// GET /health — Manager SD 主动探测
func GetHealth(c *gin.Context) {
    c.JSON(200, gin.H{
        "status":     "ok",
        "conn_count": connPool.Count(),
        "version":    version,
    })
}
```

---

### 3.2 Kafka 设计

#### 3.2.1 Topic 策略（分组设计）

**按写入特征分组，而非单 Topic 或按 DataType 一一对应。**

| Topic | DataType | Partitions | Retention | 理由 |
|-------|----------|-----------|-----------|------|
| `mxsec.agent.heartbeat` | 1000, 1001 | 6 | 24h | 高频小消息，Consumer 写 MySQL hosts 表 |
| `mxsec.agent.events` | 6001 (FIM) | 12 | 72h | 峰值高吞吐，Consumer 写 ClickHouse |
| `mxsec.agent.baseline` | 8000~8004 | 6 | 7d | 低频大消息，Consumer 写 MySQL |
| `mxsec.agent.asset` | 5050~5060 | 6 | 7d | 低频大消息，Consumer 写 MySQL |
| `mxsec.agent.command-ack` | 命令回包 | 6 | 7d | Agent 执行结果反向上报 |

各 Topic 同时配套 DLQ：`mxsec.agent.{topic-name}.dlq`

**Partition Key**：AgentID（保证同一 Agent 的数据有序）

**Replication**：2（生产环境），`min.insync.replicas: 1`

#### 3.2.2 消息格式

```protobuf
// MQMessage — Kafka 消息体
message MQMessage {
    int32  data_type     = 1;   // DataType 编码
    string agent_id      = 2;   // Agent UUID
    bytes  body          = 3;   // 原始数据（Protobuf 编码）
    int64  agent_time    = 4;   // Agent 端时间戳
    int64  svr_time      = 5;   // AC 接收时间戳
    string hostname      = 6;
    string intranet_ipv4 = 7;
    string extranet_ipv4 = 8;
    string version       = 9;
    string product       = 10;
    string trace_id      = 11;  // 端到端追踪 ID（AC 生成，贯穿整个链路）
}
```

#### 3.2.3 Kafka 集群配置

```yaml
kafka:
  brokers: 3 节点
  producer:
    acks: 1                      # Leader 确认即可
    batch.size: 65536            # 64KB 批量
    linger.ms: 10                # 10ms 等待凑批
    buffer.memory: 67108864      # 64MB 发送缓冲
    compression.type: snappy
```

---

### 3.3 Consumer 服务设计

#### 3.3.1 职责与数据路由

```go
func (c *Consumer) processMessage(msg *MQMessage) error {
    switch {
    // ——— heartbeat topic ———
    case msg.DataType == 1000:
        // 写 MySQL hosts 表（更新主机状态/最后心跳时间）
        // 同时写 Redis: agent:ac:{agentID} = acID，TTL=180s
        // 同时检查该 Agent 是否有 pending 任务 → 触发补发
        return c.mysqlWriter.WriteHeartbeat(msg)
    case msg.DataType == 1001:
        return c.mysqlWriter.WritePluginStatus(msg)

    // ——— baseline topic ———
    case msg.DataType == 8000:
        return c.mysqlWriter.WriteScanResult(msg)
    case msg.DataType == 8001, msg.DataType == 8003, msg.DataType == 8004:
        return c.mysqlWriter.WriteTaskCompletion(msg)

    // ——— asset topic ———
    case msg.DataType >= 5050 && msg.DataType <= 5060:
        return c.mysqlWriter.WriteAssetData(msg)

    // ——— events topic ———
    case msg.DataType == 6001:
        return c.chWriter.WriteFIMEvent(msg)

    // ——— 告警（双写）———
    case isAlertType(msg.DataType):
        if err := c.mysqlWriter.WriteAlertStatus(msg); err != nil {
            return err
        }
        return c.chWriter.WriteAlertDetail(msg)
    }
    return nil
}
```

**心跳消费时的额外职责**（解决任务下发可靠性问题）：

```go
func (c *Consumer) WriteHeartbeat(msg *MQMessage) error {
    // 1. 更新 MySQL hosts 表
    if err := c.db.UpsertHost(msg); err != nil {
        return err
    }
    // 2. 更新 Redis Agent→AC 映射（TTL=180s，3×心跳间隔）
    c.redis.Set("agent:ac:"+msg.AgentID, msg.ACID, 180*time.Second)

    // 3. 检查 pending 任务，触发补发
    pendingTasks, _ := c.db.GetPendingTasks(msg.AgentID)
    for _, task := range pendingTasks {
        c.taskDispatcher.Dispatch(msg.AgentID, task)
    }
    return nil
}
```

#### 3.3.2 批量写入优化

```go
type BatchWriter struct {
    queue     chan interface{}
    batchSize int           // MySQL: 500, ClickHouse: 5000
    interval  time.Duration // MySQL: 5s, ClickHouse: 10s
}

func (w *BatchWriter) Run() {
    ticker := time.NewTicker(w.interval)
    batch := make([]interface{}, 0, w.batchSize)
    for {
        select {
        case item := <-w.queue:
            batch = append(batch, item)
            if len(batch) >= w.batchSize {
                w.flush(batch)
                batch = batch[:0]
            }
        case <-ticker.C:
            if len(batch) > 0 {
                w.flush(batch)
                batch = batch[:0]
            }
        }
    }
}
```

| 存储 | 批量大小 | 刷新间隔 | 写入方式 |
|------|---------|---------|---------|
| MySQL | 500 条 | 5 秒 | `INSERT ... ON DUPLICATE KEY UPDATE` |
| ClickHouse | 5000 条 | 10 秒 | 批量 `INSERT` |

#### 3.3.3 幂等性保障

| 存储 | 幂等方式 | 说明 |
|------|---------|------|
| MySQL | `ON DUPLICATE KEY UPDATE` | 所有写入操作必须包含唯一键 |
| ClickHouse hosts_metrics | `MergeTree` 自然幂等 | 时序数据，重复消费只是重复插入相同时间点 |
| ClickHouse alert_events | `ReplacingMergeTree` | 按 alert_id 去重，保留最新状态 |
| ClickHouse fim_events | `MergeTree` 自然幂等 | 事件数据，重复消费可接受 |

#### 3.3.4 Dead Letter Queue

```go
func (c *Consumer) safeProcess(msg *MQMessage) {
    if err := c.processMessage(msg); err != nil {
        // 写入 DLQ，格式包含原始消息 + 错误信息 + 重试次数
        dlqMsg := DLQMessage{
            Original:  msg,
            Error:     err.Error(),
            Timestamp: time.Now(),
            Topic:     msg.SourceTopic,
        }
        c.dlqProducer.Send(msg.SourceTopic+".dlq", dlqMsg)

        // 触发告警
        alerter.Send("consumer_write_failed", err)
    }
}
```

DLQ 消息需要人工或自动重放工具处理，不在 Consumer 主链路中自动重试（避免无限循环阻塞 Partition）。

---

### 3.4 数据分层存储

#### 3.4.1 MySQL — 业务元数据

保留现有全部表结构，继续存储需要事务和关联查询的数据：

| 表 | 写入方 | 说明 |
|---|--------|------|
| hosts | Consumer (心跳) | 主机信息、状态、最后心跳时间 |
| policies / rules | Manager | 策略和规则（CRUD） |
| scan_tasks | Manager / Consumer | 任务状态管理，含 pending 任务补发状态 |
| scan_results | Consumer (基线) | 最新一次扫描结果 |
| assets_* | Consumer (资产) | 最新资产快照 |
| alerts | Consumer (告警) | 告警状态（pending/resolved/ignored） |
| users / configs | Manager | 用户和系统配置 |

#### 3.4.2 ClickHouse — 时序/事件/归档

```sql
-- 主机监控指标（时序数据）
CREATE TABLE host_metrics (
    timestamp    DateTime64(3),
    host_id      String,
    hostname     String,
    cpu_usage    Float32,
    mem_usage    Float32,
    disk_usage   Float32,
    load_1       Float32,
    load_5       Float32,
    load_15      Float32,
    net_in       UInt64,
    net_out      UInt64
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (host_id, timestamp)
TTL timestamp + INTERVAL 90 DAY;

-- 小时级预聚合物化视图（Dashboard 趋势图查询用）
CREATE MATERIALIZED VIEW host_metrics_hourly
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (host_id, hour)
AS SELECT
    host_id,
    toStartOfHour(timestamp) AS hour,
    avgState(cpu_usage)  AS cpu_avg,
    maxState(cpu_usage)  AS cpu_max,
    avgState(mem_usage)  AS mem_avg,
    maxState(mem_usage)  AS mem_max
FROM host_metrics
GROUP BY host_id, hour;

-- FIM 文件变更事件
CREATE TABLE fim_events (
    timestamp     DateTime64(3),
    host_id       String,
    hostname      String,
    file_path     String,
    change_type   Enum8('added'=1, 'removed'=2, 'changed'=3),
    severity      Enum8('low'=1, 'medium'=2, 'high'=3, 'critical'=4),
    category      LowCardinality(String),
    detail        String
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (host_id, timestamp)
TTL timestamp + INTERVAL 180 DAY;

-- 扫描结果历史归档
-- ORDER BY 包含 timestamp 以支持时间范围查询
CREATE TABLE scan_results_history (
    timestamp   DateTime64(3),
    task_id     String,
    host_id     String,
    rule_id     String,
    status      Enum8('pass'=1, 'fail'=2, 'error'=3),
    actual      String,
    expected    String
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (host_id, timestamp, task_id, rule_id)
TTL timestamp + INTERVAL 365 DAY;

-- 告警事件明细（用 ReplacingMergeTree 支持状态更新）
CREATE TABLE alert_events (
    timestamp     DateTime64(3),
    alert_id      String,
    host_id       String,
    hostname      String,
    alert_type    LowCardinality(String),
    severity      Enum8('low'=1, 'medium'=2, 'high'=3, 'critical'=4),
    source        LowCardinality(String),
    detail        String,
    status        LowCardinality(String),  -- pending/resolved/ignored
    updated_at    DateTime64(3)
) ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (host_id, alert_id)
TTL timestamp + INTERVAL 365 DAY;

-- 审计日志
CREATE TABLE audit_log (
    timestamp   DateTime64(3),
    user_id     String,
    action      LowCardinality(String),
    resource    String,
    detail      String,
    ip          String
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (timestamp)
TTL timestamp + INTERVAL 365 DAY;
```

#### 3.4.3 数据查询路由

| API | 数据源 | 说明 |
|-----|--------|------|
| `GET /hosts` | MySQL | 实时状态 |
| `GET /hosts/:id/metrics` | ClickHouse `host_metrics_hourly` | 趋势图（预聚合） |
| `GET /results` (最新) | MySQL | 当前扫描结果 |
| `GET /results/history` | ClickHouse `scan_results_history` | 历史趋势 |
| `GET /fim/events` | ClickHouse `fim_events` | FIM 事件列表（分页） |
| `GET /alerts` (状态) | MySQL | 告警处理状态 |
| `GET /alerts/timeline` | ClickHouse `alert_events` | 告警时间线 |
| `GET /dashboard/trends` | ClickHouse 物化视图 | Dashboard 趋势图 |

---

### 3.5 Manager HA

#### 3.5.1 无状态多实例

```nginx
upstream mxsec-manager {
    server manager-1:8080;
    server manager-2:8080;
}
```

| 问题 | 方案 |
|------|------|
| JWT 认证 | 无状态，天然支持多实例 |
| Redis 缓存 | 共享，无需改动 |
| 文件上传 | 共享存储（NFS 或 MinIO） |
| ClickHouse 查询 | 每个 Manager 实例直连 ClickHouse |
| AC 管理调用 | 查 SD 模块获取 AC 实例列表 |

#### 3.5.2 Manager 内嵌 SD 模块（服务发现）

**设计目标**：替代 Redis-as-SD，提供主动健康检测，消除被动 TTL 的延迟感知问题。

**AC 注册流程**：

```
AC 启动 → POST /api/v1/internal/ac/register
AC 运行 → POST /api/v1/internal/ac/heartbeat （每 15s）
AC 关闭 → POST /api/v1/internal/ac/deregister
```

**Manager 主动探测（双保险）**：

```go
type SDModule struct {
    instances map[string]*ACInstance  // instanceID → ACInstance
    mu        sync.RWMutex
    redis     RedisClient             // 多 Manager 实例间同步
}

type ACInstance struct {
    ID        string
    GRPCAddr  string
    HTTPAddr  string
    ConnCount int
    Status    ACStatus   // Healthy / Unhealthy / Down
    FailCount int
}

// 每 10s 主动探测所有已注册 AC
func (sd *SDModule) probeLoop() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        sd.mu.RLock()
        instances := maps.Clone(sd.instances)
        sd.mu.RUnlock()

        for _, ac := range instances {
            go sd.probe(ac)
        }
    }
}

func (sd *SDModule) probe(ac *ACInstance) {
    resp, err := httpGet(ac.HTTPAddr+"/health", 3*time.Second)
    sd.mu.Lock()
    defer sd.mu.Unlock()

    if err != nil || resp.StatusCode != 200 {
        ac.FailCount++
        if ac.FailCount >= 3 {
            // 连续 3 次失败（约 30s）→ 标记下线
            ac.Status = Down
            sd.syncToRedis(ac)
            alerter.Send("ac_instance_down", ac.ID)
        }
    } else {
        ac.FailCount = 0
        ac.Status = Healthy
        sd.syncToRedis(ac)
    }
}

// 同步到 Redis（供其他 Manager 实例读取）
func (sd *SDModule) syncToRedis(ac *ACInstance) {
    sd.redis.HSet("ac:instances", ac.ID, marshal(ac))
    sd.redis.Expire("ac:instances", 120*time.Second)
}
```

**对外接口**：

```go
// GET /api/v1/discovery/agentcenter — Agent 查询可用 AC 列表
// 返回健康 AC 列表，按 power-of-two-choices 权重排序
func GetDiscovery(c *gin.Context) {
    healthy := sd.GetHealthyInstances()
    c.JSON(200, DiscoveryResponse{Instances: healthy})
}

// 仅供 Manager 内部使用：获取指定 Agent 所在的 AC
func (sd *SDModule) GetACForAgent(agentID string) (*ACInstance, error) {
    acID := sd.redis.Get("agent:ac:" + agentID)
    if acID == "" {
        return nil, ErrAgentNotConnected
    }
    ac := sd.GetInstance(acID)
    if ac == nil || ac.Status != Healthy {
        return nil, ErrACUnavailable
    }
    return ac, nil
}
```

**Redis 在 SD 中的角色**（缓存，非源头）：

| Key | 写入方 | 用途 |
|-----|--------|------|
| `ac:instances` | Manager SD 模块 | 多 Manager 实例间同步 AC 健康状态 |
| `agent:ac:{agentID}` | Consumer（消费心跳时） | Agent 所在 AC，TTL=180s |

Manager 内存是源头，Redis 是多实例同步的缓存。Redis 宕机时，Manager 内存状态仍可服务，多实例短暂不一致（可接受）。

**远期演进**：规模超过 500 Agent 或有 K8s 化需求时，将 SD 模块底层存储替换为 etcd（Manager Lease + Watch），对外接口不变。

---

### 3.6 Agent 多地址服务发现

```yaml
# agent.yaml
server:
  agent_center:
    discovery_url: "http://manager-lb:8080/api/v1/discovery/agentcenter"
    # 回退：静态地址列表
    addresses:
      - "agentcenter-1:6751"
      - "agentcenter-2:6751"
```

```go
// connection.go — discoverServer
func (m *Manager) discoverServer(ctx context.Context) (string, error) {
    // 1. 服务发现（查 Manager SD）
    if m.cfg.DiscoveryURL != "" {
        addrs, err := fetchFromDiscovery(m.cfg.DiscoveryURL)
        if err == nil && len(addrs) > 0 {
            return selectByPowerOfTwo(addrs), nil
        }
    }
    // 2. 回退到静态地址列表（轮转）
    if len(m.cfg.Addresses) > 0 {
        addr := m.cfg.Addresses[m.addrIndex%len(m.cfg.Addresses)]
        m.addrIndex++
        return addr, nil
    }
    // 3. 兼容旧配置
    return m.cfg.PrivateHost, nil
}

// power-of-two-choices：随机选两个，取连接数更少的
// 防止新 AC 上线时所有 Agent 都往它连的雪崩效应
func selectByPowerOfTwo(instances []ACInstance) string {
    if len(instances) == 1 {
        return instances[0].GRPCAddr
    }
    a := instances[rand.Intn(len(instances))]
    b := instances[rand.Intn(len(instances))]
    if a.ConnCount <= b.ConnCount {
        return a.GRPCAddr
    }
    return b.GRPCAddr
}
```

---

### 3.7 跨实例任务下发

#### 3.7.1 单 Agent 任务流（可靠下发）

```
1. Manager API 创建任务 → MySQL scan_tasks (status=pending)
2. Manager 查 SD 模块: ac = GetACForAgent(agentID)
   - ac 健康 → 调 POST http://ac/command
   - ac 不健康 / agent 离线 → 任务保持 pending，等心跳触发
3. AC 查本地连接池 → gRPC Stream 下发给 Agent
   - 找不到 Agent（刚断连）→ 返回 404
   - Manager 收到 404 → 任务继续保持 pending（不报错）
4. Agent 执行 → 结果 → gRPC → AC → Kafka → Consumer → MySQL
5. Consumer 消费心跳时：查 pending 任务 → 重新触发下发
```

#### 3.7.2 批量任务分发

```go
func (m *Manager) dispatchBatchTask(task *ScanTask, agentIDs []string) {
    // 1. 所有任务先持久化（状态=pending）
    m.db.CreateTasksForAgents(task.ID, agentIDs)

    // 2. 按 AC 实例分组（批量查 Redis）
    acGroups := m.groupByAC(agentIDs)

    // 3. 每个 AC 一次 HTTP 请求（批量接口）
    var wg sync.WaitGroup
    for acAddr, agents := range acGroups {
        wg.Add(1)
        go func(addr string, ids []string) {
            defer wg.Done()
            resp, err := m.acClient.PostBatch(addr, BatchCommandRequest{
                AgentIDs: ids,
                Command:  buildTaskCommand(task),
            })
            if err != nil {
                // AC 整体不可用，所有 agent 保持 pending，等心跳补发
                return
            }
            // not_found 的 agent 保持 pending，等心跳补发
            // 只更新 sent 的状态为 dispatched
            m.db.UpdateTaskStatus(task.ID, resp.Sent, "dispatched")
        }(acAddr, agents)
    }
    wg.Wait()
}

// 批量查 Redis，获取 Agent→AC 映射
func (m *Manager) groupByAC(agentIDs []string) map[string][]string {
    pipe := m.redis.Pipeline()
    for _, id := range agentIDs {
        pipe.Get(ctx, "agent:ac:"+id)
    }
    results, _ := pipe.Exec(ctx)
    // 按 AC 地址分组（跳过无映射/AC 不健康的 agent，它们保持 pending）
    groups := make(map[string][]string)
    for i, result := range results {
        acID := result.(*redis.StringCmd).Val()
        ac := m.sd.GetInstance(acID)
        if ac == nil || ac.Status != Healthy {
            continue // 保持 pending
        }
        groups[ac.HTTPAddr] = append(groups[ac.HTTPAddr], agentIDs[i])
    }
    return groups
}
```

---

## 4. 性能优化要点

### 4.1 AgentCenter 侧

| 优化项 | 说明 |
|--------|------|
| Kafka 对象池 | `sync.Pool` 复用 MQMessage，减少 GC |
| 异步 Kafka Producer | 不等 ack，失败写降级队列 |
| 连接令牌限流 | 每 AC 实例最大 2000 连接，超出拒绝 |
| 心跳仅更新内存 | 不写数据库，通过 Kafka 异步持久化 |
| gRPC Snappy 压缩 | 已实现 |
| 批量命令接口 | `/command/batch` 减少 Manager→AC HTTP 调用次数 |

### 4.2 Consumer 侧

| 优化项 | 说明 |
|--------|------|
| ClickHouse 大批量写入 | 5000 条/10 秒 |
| MySQL 批量 Upsert | 500 条/5 秒 |
| Consumer 多实例 | Kafka Consumer Group 自动分配 Partition |
| 幂等写入 | ON DUPLICATE KEY UPDATE / ReplacingMergeTree |

### 4.3 Manager 侧

| 优化项 | 说明 |
|--------|------|
| Redis 缓存 | 基线得分、Dashboard 统计缓存 5 分钟 |
| ClickHouse 物化视图 | 小时级预聚合，Dashboard 查询毫秒响应 |
| 分页查询优化 | ClickHouse 用 `WHERE timestamp < ? LIMIT N` 而非 OFFSET |
| SD 模块缓存 | AC 健康状态内存缓存，读取无锁 |

---

## 5. Redis 使用规划

Redis 定位：**Manager 内部的共享状态缓存**，不是服务注册的源头。

| Key 模式 | 用途 | TTL | 写入方 | 读取方 |
|----------|------|-----|--------|--------|
| `ac:instances` | AC 健康状态（Manager SD 同步用） | 120s | Manager SD | Manager SD（多实例同步） |
| `agent:ac:{agentID}` | Agent→AC 映射 | 180s (= 3×心跳) | Consumer | Manager（任务分发查询） |
| `baseline:score:{hostID}` | 基线得分缓存 | 5min | Manager | Manager |
| `scheduler:lock` | 任务调度分布式锁 | 10s | Consumer | Consumer |
| `task:dispatch:{taskID}` | 任务分发幂等去重 | 1h | Consumer | Consumer |

**Redis 不可用时的行为**：
- AC 健康状态：Manager 内存可用，多实例短暂不同步（可接受）
- Agent→AC 映射：任务下发降级为全量 AC 广播（每个 AC 都收到命令，自己判断是否有该 Agent）
- 基线得分缓存：降级为每次查 MySQL，性能略降

---

## 6. 实施计划

### Phase 1：基础设施准备 + Manager HA（低风险）

**目标**：Manager 多实例 + Redis 正式启用 + Kafka/ClickHouse 容器就绪

| 改动 | 文件 | 说明 |
|------|------|------|
| Redis 正式初始化 | `internal/server/manager/setup/init.go` | Manager/AC 启动时连 Redis |
| Nginx LB 配置 | `deploy/config/nginx.conf` | upstream 多 Manager |
| Docker Compose | `deploy/docker-compose.yml` | manager replicas:2，Kafka/ClickHouse 提升为默认服务 |
| ClickHouse 建表 DDL | `deploy/init-clickhouse.sql` | 含物化视图 |

**验证**：Manager 双实例运行，API 通过 Nginx LB 正常访问。

### Phase 2：Kafka 接入 + AC 解耦存储（核心改动）

**目标**：AgentCenter 数据写 Kafka，不再直接写 MySQL

| 改动 | 文件 | 说明 |
|------|------|------|
| Kafka Producer 封装 | `internal/server/common/kafka/producer.go` | 含对象池、降级队列 |
| AC 数据处理改造 | `internal/server/agentcenter/transfer/service.go` | handlePackagedData 改写 Kafka |
| AC 批量命令接口 | `internal/server/agentcenter/httptrans/` | 新增 /command/batch |
| Consumer 服务 | `cmd/server/consumer/main.go` | 新建独立服务 |
| Consumer 数据路由 | `internal/server/consumer/` | 按 DataType 分流 MySQL/ClickHouse |
| DLQ 处理 | `internal/server/consumer/dlq.go` | 失败消息进 DLQ |
| 幂等写入 | `internal/server/consumer/writer/` | MySQL Upsert + CH ReplacingMergeTree |

**验证**：Agent 数据经 AC → Kafka → Consumer → MySQL/ClickHouse，端到端正常，DLQ 可用。

### Phase 3：Manager SD 模块 + AC HA（关键改动）

**目标**：Manager 内嵌 SD + AC 多实例 + 跨实例任务路由 + 批量下发

| 改动 | 文件 | 说明 |
|------|------|------|
| Manager SD 模块 | `internal/server/manager/sd/` | 注册/心跳/探测/发现 |
| Manager SD API | `internal/server/manager/api/discovery.go` | /discovery/agentcenter |
| Manager SD 内部 API | `internal/server/manager/api/internal.go` | AC 注册/心跳/注销 |
| AC 向 Manager 注册 | `internal/server/agentcenter/` | 启动/心跳/关闭 |
| Manager 任务路由 | `internal/server/manager/biz/task.go` | 查 SD → 调 AC 批量接口 |
| 任务 pending 补发 | `internal/server/consumer/heartbeat.go` | 心跳消费时检查 pending 任务 |
| Agent 多地址发现 | `internal/agent/connection/connection.go` | power-of-two-choices |
| Docker Compose | `deploy/docker-compose.yml` | AC replicas:2 |

**验证**：双 AC 实例，AC 下线 30s 内 Manager 感知，任务跨实例正确下发，心跳触发 pending 补发。

### Phase 4：Manager 接入 ClickHouse 查询（功能完善）

**目标**：Dashboard 趋势图、指标、FIM 事件从 ClickHouse 返回

| 改动 | 文件 | 说明 |
|------|------|------|
| ClickHouse 客户端 | `internal/server/database/clickhouse.go` | 新建 |
| Manager 初始化 | `internal/server/manager/setup/init.go` | 添加 CH 连接 |
| 指标查询 API | `internal/server/manager/api/host_metrics.go` | 查 host_metrics_hourly |
| FIM 事件 API | `internal/server/manager/api/fim.go` | 查 fim_events |
| Dashboard 趋势 API | `internal/server/manager/api/dashboard.go` | 查物化视图 |

**验证**：Dashboard 趋势图、FIM 事件列表从 ClickHouse 正常返回，系统监控页面接通。

### Phase 5：生产加固（稳定性）

| 改动 | 说明 |
|------|------|
| Kafka 集群化 | 3 Broker，replication=2，min.insync.replicas=1 |
| Redis Sentinel | 主从 + Sentinel 故障转移 |
| ClickHouse 监控 | 慢查询告警、parts 数量监控 |
| Consumer 压测 | 模拟 1000 Agent 并发，验证 DLQ 触发率 |
| 数据迁移工具 | MySQL 历史指标数据迁移到 ClickHouse |
| SD 模块远期规划 | 评估是否迁移到 etcd（规模 >500 Agent 或 K8s 化时） |

---

## 7. 容量规划参考（1000 Agent 场景）

| 数据类型 | 产生频率 | 单条大小 | 日写入量 | 存储 |
|----------|---------|---------|---------|------|
| 心跳 | 1000×1/60s | ~2KB | ~2.8GB | MySQL |
| 基线结果 | 1000×200/天 | ~500B | ~95MB | MySQL |
| 资产采集 | 1000×50/12h | ~1KB | ~100MB | MySQL |
| FIM 事件 | 100×500/s(峰值) | ~200B | ~864GB(峰值) | ClickHouse |
| 主机指标 | 1000×1/60s | ~500B | ~720MB | ClickHouse |
| 告警 | ~1000/天 | ~1KB | ~1MB | 双写 |

**Kafka 吞吐**：峰值 ~50,000 msg/s（FIM 场景），events Topic 12 分区，单 Broker 可承载。

**ClickHouse 存储**：按 90 天保留 + Snappy 压缩（~4:1），FIM 峰值场景约 20TB。实际生产 FIM 事件通常远低于峰值。

---

## 8. 参考

- Elkeid Agent Center 源码: `Elkeid/server/agent_center/`
- Elkeid Manager 源码: `Elkeid/server/manager/`
- Agent 传输层架构设计: `docs/development/transport-architecture.md`
- ClickHouse MergeTree 最佳实践: 按日分区，ORDER BY (常用查询列)，TTL 自动过期
- ClickHouse ReplacingMergeTree: 适用于需要状态更新的事件表（如告警）
- Kafka 生产者调优: acks=1，batch.size=64KB，linger.ms=10，compression=snappy
- power-of-two-choices 负载均衡: 随机选两个实例，取负载更低的一个
