# Agent 传输层架构设计

本文档描述 Agent 数据传输管道的架构设计，包含当前实现分析、优化方案、与 Elkeid 的对比、以及未来演进路线。

**最后更新**: 2026-03-24

---

## 1. 历史架构（已替换）

### 1.1 数据流

```
Plugin 子进程                    Agent 主进程                      AgentCenter
     |                              |                                |
     |-- Pipe + Protobuf ------>    |                                |
     |                      SendPluginData()                         |
     |                         构建 PackagedData{1 record}           |
     |                              |                                |
     |                     chan PackagedData [2048]                   |
     |                              |                                |
     |                     sendData() 阻塞读 channel                 |
     |                              |                                |
     |                     stream.Send(PackagedData) --------------> |
     |                              |                                |
     |                     每条 record 独立一次 gRPC Send             |
```

### 1.2 核心结构

```go
type Manager struct {
    sendBuffer  chan *grpc.PackagedData  // 容量 2048
    // ...
}
```

- **缓冲机制**: Go channel（`chan *grpc.PackagedData`，容量 2048）
- **发送驱动**: channel 阻塞读，来一条发一条
- **数据粒度**: 每个 `PackagedData` 包含 1 条 `EncodedRecord`
- **元信息携带**: 心跳数据携带完整 Agent 元信息（hostname/IPs/version），插件数据仅携带 `AgentId`

### 1.3 问题分析

#### 问题 1：高频场景下 gRPC Send 次数过高

| 场景 | 数据频率 | gRPC Send 次数 |
|------|----------|----------------|
| 心跳 (60s) | 1条/60s | 1 Send/60s |
| 基线检查 (200 rules) | 200条 突发 | 200 Send (突发) |
| FIM 文件监控 | 数百条/s | 数百 Send/s |
| 资产采集 | 数十条 突发 | 数十 Send (突发) |

当前基线检查和心跳场景尚可，但 FIM、运行时告警等高频实时监控场景，每秒数百次 gRPC Send 将成为瓶颈。

每次 `stream.Send()` 的开销：
- Protobuf 序列化完整 `PackagedData`（含空字段）
- HTTP/2 帧封装（帧头 9 bytes + 数据帧）
- gRPC 帧头（5 bytes: 压缩标志 + 长度）
- 可能触发 TCP 内核缓冲区 flush
- 对端接收后逐条触发 `handlePackagedData()`

当 N 条 record 独立发送时，以上开销被放大 N 倍。

#### 问题 2：数据结构冗余

channel 中每个元素都是完整的 `PackagedData` 结构体。对于心跳，每个 `PackagedData` 都重复携带 hostname、IP 列表、version 等字符串。虽然插件数据不填这些字段，但两种数据混在同一个 channel 中，结构不统一。

#### 问题 3：channel 的特性不完全匹配

Go channel 是一个优秀的 CSP 原语，但在此场景中存在不匹配：
- **非批量消费**: channel 天然是逐条读取，无法一次性取出所有积累的数据
- **发送端阻塞**: 若消费者慢于生产者，生产者侧使用 `select default` 丢弃，无法做更精细的满溢策略
- **内存开销**: 每个 slot 存完整的 `*grpc.PackagedData`（带所有字段指针），而非仅存 `*EncodedRecord`

---

## 2. 当前架构（环形缓冲区 + 批量发送 + Snappy 压缩）

### 2.1 核心改动：环形缓冲区 + 定时批量发送

```
Plugin 子进程                    Agent 主进程                      AgentCenter
     |                              |                                |
     |-- Pipe + Protobuf ------>    |                                |
     |                      SendPluginData()                         |
     |                        序列化为 EncodedRecord                 |
     |                              |                                |
     |                    ringBuffer [2048]*EncodedRecord             |
     |                         (mutex 保护)                          |
     |                              |                                |
     |                    sendData() 100ms ticker                    |
     |                     ↓ ReadAll() 批量取出                      |
     |                     构建 PackagedData{N records}              |
     |                     附加缓存的 Agent 元信息                    |
     |                              |                                |
     |                    stream.Send(PackagedData) ---------------> |
     |                              |                                |
     |                    N 条 record 合并为 1 次 gRPC Send           |
```

### 2.2 性能预期

| 场景 | 改动前 | 改动后 | 说明 |
|------|--------|--------|------|
| FIM 500 events/s | 500 gRPC Send/s | ~5-10 Send/s | 100ms tick 内累积 ~50 条 |
| 基线检查 200 rules | 200 Send (突发) | ~2-3 Send | 200 条在 200ms 内产生完毕 |
| 心跳 (60s/次) | 1 Send/60s | 1 Send/60s | 低频不受影响 |
| 空闲状态 | 0 Send | 0 Send | ReadAll 返回空则跳过 |

**关键前提**: Server 端 `handlePackagedData()` 已使用 `for _, record := range data.Records` 遍历处理，天然兼容批量数据，无需 Server 侧修改。

### 2.3 环形缓冲区设计

```go
// internal/agent/buffer/buffer.go
var (
    mu     sync.Mutex
    buf    [2048]*grpc.EncodedRecord
    offset int
)
```

**写入策略**（区分数据来源的满溢处理）:

| 数据来源 | 写入函数 | 缓冲区满时行为 | 原因 |
|----------|----------|----------------|------|
| 插件数据 | `WriteEncodedRecord(rec)` | 丢弃新数据 | 插件数据量大，宁丢弃不阻塞插件进程 |
| 心跳/内部数据 | `WriteRecord(rec)` | 覆盖 buf[0] | 心跳是状态快照，最新的比最旧的有价值 |

**读取策略**: `ReadAll()` 一次性复制所有数据并重置 offset，消费者得到一个快照切片。

### 2.4 发送循环

```go
func (m *Manager) sendData(ctx context.Context, wg *sync.WaitGroup, stream grpc.Transfer_TransferClient) {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            records := m.ringBuffer.ReadAll()
            if len(records) == 0 {
                continue  // 空闲时不发送
            }
            // 批量构建 PackagedData，附加缓存的 Agent 元信息
            data := m.buildPackagedData(records)
            if err := m.sendWithTimeout(stream, data, 30*time.Second); err != nil {
                m.ringBuffer.Clear()
                return
            }
        }
    }
}
```

### 2.5 Agent 元信息缓存

当前心跳在每次构建 `PackagedData` 时填充 hostname/IPs/version 等字段。改动后，心跳模块调用 `SendHeartbeat()` 时将元信息缓存到 transport 层，`sendData()` 在构建批量 `PackagedData` 时直接使用缓存值。

```go
type Manager struct {
    ringBuffer *buffer.RingBuffer

    // Agent 元信息缓存（由心跳更新，sendData 使用）
    agentMeta   agentMetadata
    agentMetaMu sync.RWMutex
}

type agentMetadata struct {
    hostname     string
    intranetIPv4 []string
    extranetIPv4 []string
    intranetIPv6 []string
    extranetIPv6 []string
    version      string
    product      string
}
```

---

## 3. 与 Elkeid 的对比（基于源码）

> 以下对比基于 Elkeid 开源仓库 `agent/` 目录的实际源码分析，非推测。

### 3.1 缓冲区设计

| 维度 | Elkeid (`agent/buffer/buffer.go`) | 我们的方案 |
|------|----------------------------------|-----------|
| 数据结构 | `[2048]*proto.EncodedRecord` 固定数组 | `[2048]*grpc.EncodedRecord` 固定数组 |
| 容量 | 硬编码 2048，编译时常量 | 硬编码 2048，编译时常量 |
| 是否可配置 | 否 | 否（初期），后续通过 AgentConfig.extra 实现 |
| 并发控制 | `sync.Mutex` | `sync.Mutex` |
| 写入位置 | 追加到 `buf[offset]`，offset++ | 相同 |
| 读取方式 | `copy(ret, buf[:offset])` + `offset=0` | 相同 |

**满溢策略对比**:

| 数据来源 | Elkeid | 我们的方案 |
|----------|--------|-----------|
| 插件数据 | `PutEncodedRecord(rec)` — 丢弃新数据，归还对象池 | 丢弃新数据，返回 false |
| 内部数据（心跳） | `buf[0] = erec` — 覆盖最旧数据 | 覆盖 `buf[0]` |

**分析**: 完全一致。Elkeid 选择 mutex 而非无锁设计，是因为 Agent 端的并发写入者数量有限（通常 3-5 个插件 + 1 个心跳），mutex 的开销（纳秒级）远小于单次 gRPC Send（毫秒级），不构成瓶颈。

### 3.2 发送间隔

| 维度 | Elkeid (`agent/transport/transfer.go`) | 我们的方案 |
|------|----------------------------------------|-----------|
| ticker 间隔 | `time.Millisecond * 100`（硬编码 100ms） | `100 * time.Millisecond`（硬编码 100ms） |
| 是否可配置 | 否 | 否（初期），后续通过 AgentConfig.extra 实现 |
| 是否自适应 | 否 | 否（初期），后续评估 |
| 空闲时行为 | `if len(recs) != 0` 才发送 | `if len(records) == 0 { continue }` |

**100ms 的合理性分析**:

选择 100ms 而非其他值的平衡点：

| 间隔 | 优势 | 劣势 |
|------|------|------|
| 10ms | 更低延迟 | 每秒 100 次 ticker 唤醒，CPU 开销显著 |
| 50ms | 低延迟 | 每秒 20 次唤醒，低负载时略浪费 |
| **100ms** | **每秒 10 次唤醒，CPU 开销极低** | **最大 100ms 延迟，实时监控完全可接受** |
| 200ms | 更低唤醒频率 | 200ms 延迟在安全告警场景偏高 |
| 500ms | 批量更大，网络效率更高 | 半秒延迟，实时性差 |
| 1s | 极大批量 | 对安全事件响应太慢 |

100ms ticker 的 CPU 开销：每次唤醒检查 `offset==0` 后立即返回（空闲态），仅涉及一次 mutex Lock/Unlock + 一次整数比较，消耗在纳秒级。`time.Ticker` 本身是基于 Go runtime timer 堆实现的，10 个/s 的频率对 runtime 无压力。

**AgentCenter 高可用**: AgentCenter 后续会设计为高可用/负载均衡架构（多实例 + 负载均衡），Server 端接收能力可水平扩展，因此本方案聚焦 Agent 侧优化，不考虑 AgentCenter 的吞吐瓶颈。

### 3.3 压缩

| 维度 | Elkeid (`agent/transport/compressor/snappy.go`) | 我们的方案 (Phase 2) |
|------|------------------------------------------------|---------------------|
| 算法 | Snappy | Snappy |
| 集成方式 | gRPC `encoding.RegisterCompressor` | 相同 |
| 作用级别 | per-stream（整个双向流的所有消息） | 相同 |
| 使用方式 | `grpc.UseCompressor("snappy")` 作为 CallOption | 相同 |
| 对象池 | `sync.Pool` 复用 writer/reader | 相同 |
| 兼容性 | gRPC encoding header 自动协商 | 相同 |
| 空 import 注册 | `_ "agent/transport/compressor"` | `_ "internal/common/compressor"` |

**为什么选 Snappy 而非 gzip/zstd**:

| 算法 | 压缩率 | 速度 (encode) | CPU 开销 | 适用场景 |
|------|--------|---------------|----------|----------|
| gzip | ~60-70% | ~150 MB/s | 高 | 带宽受限、CPU 富余 |
| **Snappy** | **~50-60%** | **~500 MB/s** | **极低** | **低延迟、CPU 敏感** |
| zstd | ~65-75% | ~400 MB/s | 中 | 综合最优 |

Agent 作为安全守护进程，CPU 开销是核心约束。Snappy 压缩速度是 gzip 的 3-5 倍，解压速度更快，且压缩率对于 Protobuf 数据已足够（Protobuf 本身已经是紧凑二进制格式）。

### 3.4 对象池

| 维度 | Elkeid (`agent/buffer/pool.go`) | 我们的方案（初期不实现） |
|------|--------------------------------|------------------------|
| 实现 | 4 级 `sync.Pool`（1KB/2KB/3KB/4KB） | 暂不实现 |
| 分级策略 | 按 `EncodedRecord.Data` 大小路由到对应池 | - |
| 归还条件 | `cap(Data) <= 4096` 才归还，超大对象直接 GC | - |

**为什么初期不实现**:

对象池的收益取决于对象分配频率。当前场景：
- 心跳：1 次/60s，单条 record ~2-5KB → 分配频率极低
- 基线检查：200 条/次，每条 ~500B → 突发但不持续
- FIM（未来）：高频但每条小（~200B）→ 值得优化

初期没有 FIM 高频场景，对象池 ROI 不高。待 FIM 上线后通过 pprof 观察 GC 压力，再决定是否引入。

### 3.5 连接管理

| 维度 | Elkeid | 我们的方案 |
|------|--------|-----------|
| 重连策略 | 固定 5s 间隔，最多 5 次后传输层永久退出 | 指数退避（1s→10s），无限重试 |
| 服务端选择 | 三级回退：服务发现 → 内网 → 公网 | 两级回退：服务发现 → 直连 |
| TLS | 编译时 `go:embed` 嵌入证书 | Server 首次连接时下发证书 |

我们的重连策略比 Elkeid 更健壮（无限重试 vs 5 次后放弃），更适合网络不稳定的环境。

---

## 4. 设计决策详细评估

### 4.1 缓冲区容量为什么是 2048？

**Elkeid 的选择**: 2048 是一个权衡值，在 Elkeid 的 100ms 发送间隔下：
- 理论吞吐上限：2048 条/100ms = 20,480 条/s
- 单条 `EncodedRecord` 平均大小（含 Data 字段）：~500B-2KB
- 缓冲区内存占用：2048 × 8B（指针）= 16KB（仅指针数组），实际数据内存由 record 对象持有

**2048 能否满足我们的场景？**

| 场景 | 100ms 内最大产生量 | 是否溢出 |
|------|-------------------|---------|
| 心跳 | 1 条 | 否 |
| 基线 200 rules | ~20 条（200 条/~1s） | 否 |
| FIM 500 events/s | ~50 条 | 否 |
| FIM 极端 5000 events/s | ~500 条 | 否 |
| FIM + 基线 + 资产采集同时 | ~600 条 | 否 |

即使在极端场景下，100ms 内产生 2048 条的可能性极低。2048 对我们绰绰有余。

**是否应该做成可配置？**

当前阶段不需要。原因：
1. 2048 的裕量足够，不是瓶颈
2. Go 数组 `[N]T` 的 N 是编译时常量，运行时无法修改数组大小
3. 如果要运行时可配，需改为 slice + 容量限制，增加复杂度但无实际收益
4. Elkeid 在数十万 Agent 的生产环境中也从未改过这个值

**未来演进**: 如果生产监控发现满溢率 > 1%，可以在不改接口的前提下将 `[2048]` 改为 `[4096]` 或更大值，这是一行代码的改动。

### 4.2 发送间隔为什么是 100ms？

**核心权衡**: 延迟 vs 吞吐量 vs CPU 开销

```
延迟         ←→  吞吐量
10ms: 低延迟       小批量，网络开销高
100ms: 可接受延迟   中批量，网络开销低
1s: 高延迟         大批量，网络效率最高
```

**100ms 的定量分析**:

1. **延迟可接受性**:
   - 安全告警（如 FIM 检测到恶意文件篡改）：从事件产生到 Server 收到，最大额外延迟 100ms。安全告警的响应 SLA 通常是秒级（人工介入是分钟级），100ms 完全可接受
   - 基线检查结果：本身就是任务制，结果延迟 100ms 无感知
   - 心跳：60s 周期，延迟 100ms 占比 0.17%，可忽略

2. **CPU 开销**:
   ```
   每次 ticker 唤醒的操作：
   1. mutex.Lock()         — ~10ns
   2. 检查 offset == 0     — ~1ns
   3. mutex.Unlock()       — ~10ns
   总计: ~20ns/次 × 10次/s = ~200ns/s
   ```
   占一个 CPU 核心的 0.00002%，完全可忽略。

3. **网络效率**:
   - FIM 500 events/s 场景：每 100ms 发送 ~50 条，每次 gRPC Send 摊薄的固定开销（帧头、序列化、系统调用）降低 50 倍
   - HTTP/2 帧开销：每帧 9 bytes 帧头。50 条合并发送节省 49 × 9 = 441 bytes 帧头。更重要的是减少了 49 次系统调用（`write` syscall）

**是否应该做成可配置？**

中期值得做。通过 `AgentConfig.extra` 字段下发 `send_interval_ms` 参数：

```protobuf
// 已有的 AgentConfig.extra 字段
message AgentConfig {
    // ...
    map<string, string> extra = 5;  // 可下发 "send_interval_ms": "200"
}
```

Agent 侧在收到配置更新时调整 ticker 间隔。这样运维可以：
- 对高负载主机（FIM 事件多）调大间隔到 200ms，减少 Send 次数
- 对需要快速响应的蜜罐主机调小间隔到 50ms，降低告警延迟
- 全局默认 100ms，无需每台单独配

但这不是 Phase 1 的范围。Phase 1 硬编码 100ms（与 Elkeid 一致），后续通过 `AgentConfig.extra` 实现可配。

### 4.3 Mutex vs 无锁

**结论**: 使用 Mutex。与 Elkeid 一致。

**定量分析**:

```
mutex Lock+Unlock 开销: ~20-50ns (无竞争)
                        ~100-500ns (有竞争，但 Agent 端写入者 ≤ 6)

atomic CAS 开销:        ~10-20ns (无竞争)
                        ~50-200ns (有竞争)
```

差异在几十纳秒级。而单次 gRPC `stream.Send()` 的耗时在 **0.1-10ms** 级别（取决于数据大小和网络状况），是 mutex 开销的 **1000-100000 倍**。

mutex 的实际竞争频率：假设 3 个插件 + 1 个心跳同时写入缓冲区，100ms 内每个写入者写入 ~50 条（极端），则 100ms 内有 200 次 Lock 操作。200 次 Lock / 100ms = 2000 ops/s，Go 的 mutex 在此量级下几乎不会产生竞争（因为每次 Lock 持有时间仅几十纳秒）。

无锁 ring buffer 的额外复杂度：
- 需要处理 head/tail 指针的 ABA 问题
- 多生产者 + 单消费者的无锁队列实现 ~200 行代码
- 调试困难，出 bug 时难以复现

**结论**: mutex 方案正确。无锁设计在 Agent 端没有可观测的性能收益，但显著增加维护成本。

---

## 5. 实施阶段

### Phase 1：环形缓冲区 + 批量发送 ✅ 已完成

**改动文件**:

| 文件 | 动作 | 说明 |
|------|------|------|
| `internal/agent/buffer/buffer.go` | 新建 | 环形缓冲区实现 |
| `internal/agent/buffer/buffer_test.go` | 新建 | 单元测试（含并发 + race 检测） |
| `internal/agent/transport/transport.go` | 修改 | 替换 channel 为 ringBuffer，sendData 改为 100ms ticker 驱动 |

**注**: heartbeat.go 无需修改，SendHeartbeat 接口保持不变，transport 内部从 PackagedData 提取 record 和元信息。Server 端无改动。

### Phase 2：Snappy 压缩 ✅ 已完成

**改动文件**:

| 文件 | 动作 | 说明 |
|------|------|------|
| `internal/common/compressor/snappy.go` | 新建 | Snappy gRPC 编码器（Agent/Server 共用，sync.Pool 复用） |
| `internal/agent/transport/transport.go` | 修改 | `client.Transfer(ctx, grpc.UseCompressor("snappy"))` |
| `internal/server/agentcenter/server/server.go` | 修改 | 空 import compressor 包注册解压器 |
| `go.mod` | 修改 | 添加 `github.com/golang/snappy` 依赖 |

**兼容性**: gRPC 按 per-call encoding header 自动协商，老 Agent（无压缩）+ 新 Server 正常工作。

### Phase 3：按需优化（生产数据驱动）

以下优化项不预设时间表，根据生产监控数据决定是否实施。

#### 3a. 优先级分离

**实施条件**: ringBuffer 满溢率 > 1%

将单一 ringBuffer 拆分为两个：

| 队列 | DataType | 说明 |
|------|----------|------|
| highPriority | 6001 (FIM 告警), 8001/8003/8004 (任务结果), 6002 (FIM 任务完成) | 安全告警和任务完成确认 |
| normal | 其余 | 心跳、资产数据、基线结果 |

`sendData` 每 tick 优先消费 highPriority 队列。

#### 3b. 对象池

**实施条件**: pprof 显示 `EncodedRecord` 分配是 GC 热点

参考 Elkeid 的 4 级 `sync.Pool` 设计，按 `Data` 字段大小分级复用 `EncodedRecord` 对象。

#### 3c. 发送间隔可配

**实施条件**: 需要对不同主机差异化调参

通过 `AgentConfig.extra["send_interval_ms"]` 由 Server 下发，Agent 收到后动态调整 ticker 间隔。

#### 3d. 缓冲区监控指标

**实施条件**: 上线后需要可观测性

通过心跳上报 buffer 使用指标：
- `buffer_usage`: 当前 offset / 2048（使用率）
- `buffer_overflow_count`: 累计满溢丢弃次数
- `send_batch_avg_size`: 平均每次 Send 的 records 数

---

## 6. 验证方式

### 6.1 单元测试

`buffer/buffer_test.go`:
- 基本读写：Write N 条 → ReadAll 得到 N 条
- 满溢策略：写入 > 2048 条 → 验证插件数据丢弃新数据，心跳覆盖 buf[0]
- 并发安全：多 goroutine 并发 Write + 1 goroutine ReadAll，-race 检测无竞争
- 空读取：ReadAll 在无数据时返回空切片

### 6.2 编译验证

```bash
go build ./internal/agent/...
go build ./internal/server/...
```

### 6.3 集成测试

- Agent + Server 联调，FIM 产生大量事件
- 验证 Server 收到的 `PackagedData` 中 `len(Records) > 1`（批量生效）
- 新 Agent + 旧 Server、旧 Agent + 新 Server 均正常（兼容性）
