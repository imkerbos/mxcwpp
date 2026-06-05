# EDR Agent 侧采集设计

> **本文范围**：mxsec Agent 端 EDR 采集层的全部设计 —— eBPF + LSM hooks、用户态 fallback、事件聚合 / 限速 / 去重、自保护、容器富化、本地启发式检测器（反弹 shell / SUID / 内存马）、性能 SLO 与内核兼容矩阵。
>
> **不在本文范围**：Server 侧 Engine 检测分析（CEL / 序列 / ML / Storyline / 告警生成）见 [`engine-design.md`](engine-design.md) 与 [`engine-detection-design.md`](engine-detection-design.md)。Agent 与 Engine 的边界铁则是 **Agent 只采集 + 本地启发式 / 富化，绝不做规则评估与告警关联**。
>
> **平台定位锚点**：
> - mxsec 是**工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**。
> - **不做 Windows、不做 macOS**。Agent 实现按 Linux 原生 eBPF + 用户态 fallback 两条路。
> - 六微服务架构中 Agent 通过 [`AgentCenter`](architecture.md) 接入，事件经 Kafka 解耦后由 **Consumer 持久化、Engine 检测**。
> - 默认 `MODE=observe` 监听模式，磨合达标后切 `protect`（详见 [`operating-modes.md`](operating-modes.md)）。Agent 侧的"动作类响应"由 `mode` 控制，采集行为在两种模式下完全一致。
> - 多租户 from-day-1，所有上报事件必带 `tenant_id` 字段（详见 [`multi-tenant.md`](multi-tenant.md)）。

---

## 1. 在六微服务架构中的位置

```
   +-------------------------------------------+
   |  mxsec-agent (Linux 守护进程 / K8s DS)    |
   |-------------------------------------------|
   |  EDR 内置：                                |
   |   - eBPF 采集器 (process/file/net/dns/    |
   |     mem/cred/ptrace/signal ...)           |
   |   - 用户态 fallback (cn_proc + fanotify   |
   |     + /proc 轮询)                          |
   |   - 事件聚合 / 去重 / 限速 (10s 窗口)      |
   |   - 容器 / K8s 富化 (containerd/crio/     |
   |     cri-dockerd 多 runtime)               |
   |   - 本地启发式：内存马 / 反弹 shell /      |
   |     SUID / deleted_exe / anon rwx         |
   |   - 自保护 selfprotect.go (sd_notify +    |
   |     chattr +i + watchdog 双进程)          |
   |   - WAL 本地缓冲 (500MB 上限)              |
   |                                            |
   |  插件（独立进程，OS Pipe）：              |
   |   - baseline / scanner / fim /            |
   |     remediation / av-scanner / rasp       |
   +-----------------+-------------------------+
                     |
                     | gRPC BiDi Stream + mTLS + Snappy
                     v
       +-------------+-------------+
       |        AgentCenter        |  纯转发，不解析
       +-------------+-------------+
                     | Sarama (Key=tenant_id:agent_id)
                     v
       +-------------+-------------+
       |          Kafka            |
       |  mxsec.agent.ebpf (3000-  |
       |  3099, 12 partitions, 3d) |
       +------+-------------+------+
              |             |
              v             v
     +--------+--+    +-----+--------+
     |  Consumer |    |    Engine    |
     |  (写存储) |    | (CEL/序列/ML)|
     +-----------+    +--------------+
```

**与 Engine 的解耦红线**：

| 维度 | Agent 侧（本文档） | Server Engine（不在本文档） |
|------|---------------------|-------------------------------|
| eBPF / LSM 内核采集 | ✅ 唯一拥有者 | ❌ |
| 容器 / K8s 上下文富化 | ✅ 本地 cgroup 解析 + runtime CLI | ❌（Engine 信任 Agent 注入字段） |
| 事件聚合 / 限速 | ✅ 10s 窗口本地降噪 | ❌ |
| WAL 本地缓冲 | ✅ 网络抖动兜底 | ❌ |
| 本地启发式（memfd / deleted_exe / SUID） | ✅ 产 `anomaly_hint` 标志 | ❌ |
| 启发式标志 → 是否产告警 | ❌ 由 Engine 决定 | ✅ |
| CEL 规则评估 | ❌（Agent 完全不做） | ✅ |
| 序列 / Storyline / ML | ❌ | ✅ |
| 告警生成 + 聚合 | ❌ | ✅ |
| 处置动作下发 | ❌ Agent 仅作为执行者，由 Engine/Manager 下发 | ✅（仅 `MODE=protect`） |

> Agent 端**绝不进行规则评估**。这是 v2.0 架构相对历史"Agent 端规则引擎"方案的重要修正：保证规则中心化，避免 Agent 与 Server 双源真相、调试灾难。Agent 仅做"轻量启发式打标"——例如发现 `memfd_exec` 时附 `anomaly_hint=memfd`，但是否升级为告警完全由 Engine 决定。

---

## 2. 与 plugins/ 的边界

| 模块 | 位置 | 进程模型 | 通信 | 职责 |
|------|------|----------|------|------|
| **EDR 采集** | `internal/agent/edr/` | **与 Agent 同进程**（内置） | 直接 Go func | 内核事件 / 本地启发式 / 富化 / 自保 |
| baseline | `plugins/baseline/` | 独立子进程 | OS Pipe + Protobuf | CIS / 等保基线扫描 |
| scanner | `plugins/scanner/` | 独立子进程 | OS Pipe + Protobuf | 全盘 ClamAV + YARA |
| fim | `plugins/fim/` | 独立子进程 | OS Pipe + Protobuf | 文件完整性快照 + 增量 |
| remediation | `plugins/remediation/` | 独立子进程 | OS Pipe + Protobuf | 修复任务执行 |
| av-scanner (Phase 4) | `plugins/av-scanner/` | 独立子进程 | OS Pipe + Protobuf | 病毒扫描（专项） |
| rasp (Phase 4) | `plugins/rasp/` | 独立子进程，可能 attach JVM | OS Pipe + Protobuf | Java MVP RASP |

**为什么 EDR 不再做 plugin**：

1. **热路径零 IPC**：内核事件每秒上千条，再走 OS Pipe + Protobuf 序列化是浪费。
2. **eBPF 生命周期同 Agent**：Agent 重启 = BPF 自动重载；plugin 崩溃不应导致 BPF 卸载。
3. **自保更简单**：单进程 + Watchdog 双进程互保，比"守护 Agent + 守护 EDR plugin"两层模型简单。
4. **资源统一管控**：CPU/RSS 预算只看一个进程。
5. **商业标杆一致**：CrowdStrike / SentinelOne / 青藤万象均为单 Agent 架构。

**为什么 scanner / baseline / fim / remediation 保留 plugin**：

- 重 IO（全盘扫描 / 大目录哈希）不能阻塞 EDR 热路径。
- 按需启动，闲时零资源。
- 崩溃不影响 EDR 核心。
- 通信频率低（任务粒度），OS Pipe 可接受。

---

## 3. 目录结构（现状对齐）

```
internal/agent/edr/
├── engine.go              # 内置 EDR 引擎（生命周期 / 流水线编排）
├── engine_other.go        # 非 Linux 平台空实现（不支持）
├── collector/             # 采集层
│   ├── collector.go       # Collector 接口 + 自动模式探测
│   ├── ebpf.go            # cilium/ebpf 加载器（fentry/kprobe）
│   ├── bpf/               # BPF C 源码 + bpf2go 产物
│   ├── process_bpf{el,eb}.{go,o}   # exec/exit
│   ├── file_bpf{el,eb}.{go,o}      # open/write/rename/unlink/chmod
│   ├── network_bpf{el,eb}.{go,o}   # tcp_connect/accept/close + udp_sendmsg
│   ├── dns_parser.go      # UDP 53 payload → DNS query 解析
│   ├── userspace.go       # 用户态 fallback 总入口
│   ├── cnproc.go          # netlink cn_proc（进程）
│   ├── fanotify.go        # fanotify（文件，仅 open/close_write）
│   ├── procnet.go         # /proc/net 轮询（网络，5s 粒度）
│   ├── procscan.go        # /proc 周期对账（防进程树断链）
│   ├── degradation.go     # CPU 自感知 4 级降级
│   └── hookdetect.go      # 启动时 fentry → kprobe → BPF LSM 优先级探测
├── event/event.go         # Event 统一结构 + DataType 常量
├── rule/                  # （历史代码保留）本地规则引擎，新架构下仅做本地启发式打标
├── aggregator.go          # 10s 窗口同签名聚合（DataType 3000/3001/3002）
├── bde/                   # 行为画像 13 维快照（DataType 3010）
│   ├── profiler.go
│   └── format.go
├── container/             # 容器 / K8s 上下文富化
│   ├── resolver.go        # cgroup → containerd/docker/crio CLI 查询
│   └── resolver_other.go
├── memfd/                 # 内存威胁本地扫描
│   ├── scanner.go         # /proc 扫 memfd / deleted_exe / anon rwx
│   └── scanner_test.go
├── ioc/                   # IOC 本地缓存（Server 下发 full/diff）
├── yara/                  # YARA 事件触发（async 默认，pre-block 可选）
├── isolate/               # 主机网络隔离（iptables + nftables，eBPF 升级走 Engine 指令）
├── storyline/             # PID → story_id 因果追踪（仅打标，聚合在 Server）
└── selfprotect.go         # sd_notify + chattr+i，与 Watchdog 双进程互保
```

> 本文档据此目录结构维护。新增能力（如 Phase 4 的 LSM hooks、Phase 6 的内存马增强）必须先在本文档登记 + 更新 [`datatype-allocation.md`](datatype-allocation.md) + 评审通过，再写代码。

---

## 4. 双模采集架构

### 4.1 总览

| 维度 | eBPF 模式（kernel ≥ 4.18 + CO-RE） | 用户态模式（kernel < 4.18 / 无 BTF） |
|------|--------------------------------------|------------------------------------------|
| 进程 | `tracepoint/sched_process_exec` + `tracepoint/sched_process_exit` | `cn_proc` (netlink `PROC_EVENT_FORK/EXEC/EXIT`) |
| 文件 | `fentry/security_file_open` + `kprobe/security_inode_{rename,unlink,setattr}` | `fanotify` (FAN_OPEN_PERM + FAN_CLOSE_WRITE) |
| 网络 | `fentry/tcp_connect` + `kprobe/inet_csk_accept` + `kprobe/tcp_close` + `kprobe/udp_sendmsg` | `/proc/net/{tcp,tcp6,udp,udp6}` 5s 轮询 |
| DNS | `kprobe/udp_sendmsg` (dst_port=53) + payload 解析 | `/var/log/dnsmasq` 或 `getaddrinfo` LD_PRELOAD（不推荐） |
| 凭据/权限 (Phase 4) | `lsm/bprm_committed_creds` + `kprobe/__sys_setuid` | `/proc/<pid>/status` 5s 周期对账 |
| ptrace / 注入 (Phase 4) | `tracepoint/sys_enter_ptrace` | `/proc/<pid>/status` TracerPid != 0 |
| 信号 (Phase 4 自保) | `tracepoint/signal/signal_deliver` | 无法捕获，依赖 Watchdog |
| 内存 (memfd/anon rwx) | `tracepoint/sys_enter_memfd_create` + 本地扫 | `/proc/<pid>/{maps,fd}` 10s 周期扫描 |
| 容器 cgroup | `task_struct` → `css` → `cgroup` | `/proc/<pid>/cgroup` 解析 |
| 字段完整性 | 完整 (pid/ppid/uid/gid/exe/cmdline/cwd/env/tty/cgroup) | pid/ppid/exe/cmdline 完整，cwd/env/tty 从 /proc 补 |
| 实时性 | 实时（内核回调） | 进程/文件准实时，网络/DNS 5s 延迟 |
| CPU 开销 | 稳态 < 2% | 稳态 < 5%（轮询）|
| 内存开销 | BPF Map + Ringbuf ≈ 20-30 MB | Bloom Filter 白名单 + cache ≈ 15-20 MB |
| 适用 | 默认（kernel ≥ 4.18，主流发行版） | CentOS 7 / 较老内核 / BTF 缺失环境 |

### 4.2 启动时模式选择

`internal/agent/edr/collector/collector.go` 的 `DetectAndCreate(logger)` 是入口：

```go
// 简化伪代码 — 实际实现在 collector.go
func DetectAndCreate(log *zap.Logger) (Collector, error) {
    // 1. 探测 kernel 版本 + BTF 可用性 + 必要 helpers
    caps := probeKernelCaps()

    // 2. 优先级链：fentry → kprobe → BPF LSM (Phase 4) → userspace
    if caps.HasFentry && caps.HasBTF {
        if c, err := newEBPFCollector(log, ModeFentry); err == nil {
            return c, nil
        }
    }
    if caps.HasKprobe && caps.HasBTF {
        if c, err := newEBPFCollector(log, ModeKprobe); err == nil {
            return c, nil
        }
    }
    // 用户态兜底，永不返回错误
    return newUserspaceCollector(log), nil
}
```

启动日志样例（写入心跳）：

```
{
  "collector_mode": "ebpf_full",
  "hook_type": "fentry",
  "kernel_version": "5.15.0-119-generic",
  "btf_available": true,
  "capabilities": {
    "ebpf_full":     true,
    "ebpf_lsm":      false,
    "ebpf_signal":   true,
    "ebpf_memory":   true,
    "container_ctx": true,
    "file_full":     true,
    "dns_full":      true
  },
  "events_supported": ["3000","3001","3002","3003","3004","3010"]
}
```

### 4.3 能力等级（Capability Tier）

Server Engine 在加载规则时按 Agent 上报的 `capabilities` 过滤——不满足 `requires.capabilities` 的规则不会下发到该 Agent。

| Tier | 触发条件 | 启用能力 | 不可用规则示例 |
|------|----------|----------|----------------|
| **Full** | eBPF + BTF + fentry | 全部 | — |
| **Standard** | eBPF + BTF + kprobe（无 fentry） | 同 Full，eval 延迟 ↑ ~3 倍 | — |
| **LSM Plus** (Phase 4) | + CONFIG_BPF_LSM | + 强制凭据/打开拒绝（observe 下仅打标） | — |
| **Userspace Basic** | 无 BTF / 内核 < 4.18 | 进程/文件/网络/容器富化 | 内存马（仅 10s 采样）/ DNS 全采集 / 信号监控 |

---

## 5. eBPF 采集器（13 类事件详表）

### 5.1 事件总表（对照 [`datatype-allocation.md`](datatype-allocation.md) 3000-3099 段）

| # | EventType | DataType | Hook（首选） | Hook（fallback） | Phase | 说明 |
|---|-----------|----------|--------------|-----------------|-------|------|
| 1 | `process_exec` | 3000 | `tp/sched_process_exec` | `cn_proc EXEC` | P1 | 进程执行，含 pid/ppid/exe/cmdline/uid/gid/cwd/cgroup |
| 2 | `process_exit` | 3000 | `tp/sched_process_exit` | `cn_proc EXIT` | P1 | 进程退出（用于 storyline 终结 + 进程树清理） |
| 3 | `file_open` | 3001 | `fentry/security_file_open` | `fanotify FAN_OPEN_PERM` | P1 | 文件 open，BPF 内只放写权限 / 敏感目录 |
| 4 | `file_write` | 3001 | `kprobe/vfs_write` + `fentry/security_file_open` 出口标记 | `fanotify FAN_CLOSE_WRITE` | P1 | 文件写（实际多走 close_write，避免每次 write 触发） |
| 5 | `file_rename` | 3001 | `kprobe/security_inode_rename` | 不支持（fanotify 无） | P1 | 关键日志/二进制改名（横向感知） |
| 6 | `file_unlink` | 3001 | `kprobe/security_inode_unlink` | 不支持 | P1 | 日志/二进制删除（可疑） |
| 7 | `file_chmod` | 3001 | `kprobe/security_inode_setattr` | 不支持 | P1 | 文件权限变更（SUID 启发式核心） |
| 8 | `tcp_connect` | 3002 | `fentry/tcp_connect` | `/proc/net/tcp` 轮询 | P1 | 主动外连，pid/exe + 五元组 |
| 9 | `tcp_accept` | 3002 | `kprobe/inet_csk_accept` | `/proc/net/tcp` 轮询 | P1 | 监听端收连，用于发现新监听 |
| 10 | `tcp_close` | 3002 | `kprobe/tcp_close` | `/proc/net/tcp` diff | P1 | 链接关闭（用于流量统计） |
| 11 | `udp_send` | 3002 | `kprobe/udp_sendmsg` | `/proc/net/udp` 轮询 | P1 | UDP 出，对 DNS / C2 重要 |
| 12 | `dns_query` | 3003 | `kprobe/udp_sendmsg` + payload | 日志解析（不推荐） | P1 | UDP 53 出，解析 query name / type |
| 13 | `dns_response` | 3003 | `kprobe/udp_recvmsg` + payload | 同上 | P1 | UDP 53 入，解析返回 IP / rcode |
| 14 | `memfd_exec` | 3004 | `tp/sys_enter_memfd_create` + `process_exec` 关联 | `/proc/<pid>/fd` 周期扫 | P1 | memfd-backed 进程（无文件落地）|
| 15 | `deleted_exe` | 3004 | `process_exec` 检查 `exe` 末尾 ` (deleted)` | 同 | P1 | 进程二进制已被删（典型木马清痕） |
| 16 | `anonymous_exec` | 3004 | `process_exec` 后扫 `/proc/<pid>/maps` 找 anon + x | 同 | P1 | 匿名可执行内存映射（shellcode）|
| 17 | `signal_send` (Phase 4) | 3005（待分配） | `tp/signal/signal_deliver` | 不支持 | P4 | 监控发往 Agent 自身的 SIGKILL/SIGTERM/SIGSTOP，自保打标 |
| 18 | `cred_change` (Phase 4) | 3006（待分配） | `lsm/bprm_committed_creds` 或 `kprobe/__sys_setuid` | `/proc/<pid>/status` 对账 | P4 | uid/gid/CAP 变化（提权检测） |
| 19 | `ptrace_attach` (Phase 4) | 3007（待分配） | `tp/sys_enter_ptrace` (PTRACE_ATTACH/SEIZE) | `/proc/<pid>/status` TracerPid | P4 | 进程注入检测 |
| 20 | `module_load` (Phase 4) | 3008（待分配） | `tp/module/module_load` 或 `kprobe/load_module` | `/proc/modules` diff | P4 | LKM 加载（Rootkit）|
| 21 | `behavior_profile` | 3010 | 用户态 BDE Profiler 汇总 | 同 | P10 | 4 维行为画像快照（process/file/net/dns），15s 频率 |

> **Phase 4 增量** 新增 4 个 DataType（3005-3008）须先在 [`datatype-allocation.md`](datatype-allocation.md) 注册，再实现。当前 3005-3009 处于 "未分配" 状态，本设计预留这 4 个槽位。

### 5.2 BPF 程序组织

```
internal/agent/edr/collector/bpf/
├── common.h       # 公共结构: event_t (header) + per-type 子结构
├── maps.h         # BPF Map 定义
│   ├── whitelist_map    : LRU_HASH (PID/comm → 1)
│   ├── pid_filter_map   : HASH (tenant_id-keyed 采样率)
│   ├── event_ringbuf    : RINGBUF (kernel ≥ 5.8) / PERF_EVENT_ARRAY (fallback)
│   ├── config_map       : ARRAY (degradation_level / sample_rate / mode)
│   └── self_pid_map     : ARRAY (Agent 自身 PID，避免 self-loop)
├── process.bpf.c  # exec/exit tracepoint
├── file.bpf.c     # security_file_open + security_inode_{rename,unlink,setattr}
├── network.bpf.c  # tcp_connect / inet_csk_accept / tcp_close / udp_sendmsg
├── dns.bpf.c      # udp_sendmsg (dst=53) payload 解析
├── memfd.bpf.c    # sys_enter_memfd_create
└── signal.bpf.c   # signal_deliver (Phase 4)
```

BPF 程序的"防爆"原则：

1. **每个 hook 入口第一行查白名单**：`if (whitelist_lookup(pid)) return 0;`，BPF Map LRU_HASH，O(1)。
2. **可调采样率**：`config_map[0] = sample_rate (0-100)`，用户态可动态调整，BPF 内部按 PID hash 取模。
3. **CPU/内存预算硬约束**：单个 BPF 程序 ≤ 4096 instructions，stack ≤ 512 bytes（verifier 限制）。
4. **Ringbuf 双写**：高优先级事件（process_exec / memfd / signal）走优先 Ringbuf，低优先级（tcp_close）走次级 Ringbuf，CPU > 60% 时只读优先 Ringbuf。
5. **CO-RE**：所有内核结构访问用 `bpf_core_read` + `BTF`，避免 ko_list 维护噩梦。

### 5.3 Go 用户态加载与读取骨架

```go
// internal/agent/edr/collector/ebpf.go (现状对齐)

package collector

import (
    "context"
    "errors"

    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/ringbuf"
    "go.uber.org/zap"

    "github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// HookType 描述当前 BPF 程序的挂载方式。
type HookType string

const (
    HookFentry HookType = "fentry"
    HookKprobe HookType = "kprobe"
    HookLSM    HookType = "lsm"     // Phase 4
)

// Collector is the unified collector interface.
// 同一份接口可同时实现 eBPF / userspace 两种模式，对上游透明。
type Collector interface {
    // Start 启动采集器，事件通过 out channel 输出。
    // ctx Done 后必须释放所有内核资源（detach BPF / 关 netlink / 停 goroutine）。
    Start(ctx context.Context, out chan<- *event.Event) error

    // Mode 返回当前模式标识（"ebpf_full" / "userspace_basic"），用于心跳上报。
    Mode() string

    // Capabilities 返回当前可用的细粒度能力清单（供 Engine 过滤规则）。
    Capabilities() map[string]bool

    // SetDegradationLevel 由 degradation.go 调用，CPU 自感知降级。
    SetDegradationLevel(level int)

    // Stop 优雅退出。
    Stop() error
}

// ebpfCollector 用 cilium/ebpf 加载 bpf2go 生成的 ELF。
type ebpfCollector struct {
    log     *zap.Logger
    hook    HookType
    progs   *bpfPrograms        // 由 bpf2go 生成
    links   []link.Link
    rb      *ringbuf.Reader
    capsMap map[string]bool
}

func newEBPFCollector(log *zap.Logger, hook HookType) (*ebpfCollector, error) {
    objs := &bpfPrograms{}
    if err := loadBpfObjects(objs, nil); err != nil {
        return nil, err
    }

    c := &ebpfCollector{log: log, hook: hook, progs: objs}
    if err := c.attach(); err != nil {
        _ = objs.Close()
        return nil, err
    }

    var err error
    c.rb, err = ringbuf.NewReader(objs.EventRingbuf)
    if err != nil {
        c.detach()
        return nil, err
    }
    return c, nil
}

func (c *ebpfCollector) Start(ctx context.Context, out chan<- *event.Event) error {
    go c.readLoop(ctx, out)
    return nil
}

func (c *ebpfCollector) readLoop(ctx context.Context, out chan<- *event.Event) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        rec, err := c.rb.Read()
        if err != nil {
            if errors.Is(err, ringbuf.ErrClosed) {
                return
            }
            c.log.Warn("ringbuf read error", zap.Error(err))
            continue
        }
        evt := decodeRawEvent(rec.RawSample)
        if evt == nil {
            continue
        }
        select {
        case out <- evt:
        case <-ctx.Done():
            return
        }
    }
}
```

### 5.4 Hook 优先级探测

`hookdetect.go` 在启动时尝试三档 hook，自动降级：

```
+------ fentry (kernel ≥ 5.5) ---------+
|  BPF trampoline 直接 patch 函数入口  |
|  开销 = 普通函数 call，比 kprobe ↓ 5x |
|  cilium/ebpf 原生 ProgramTypeTracing  |
+--------------+------------------------+
               |  HaveProgramType + attach 失败
               v
+------ kprobe (kernel ≥ 4.18) --------+
|  int3 断点陷阱 → handler → 单步     |
|  开销较高，但白名单 fast-path 抵消   |
|  作为通用兜底                        |
+--------------+------------------------+
               |  CONFIG_KPROBES=n 或 BTF 缺失
               v
+------ BPF LSM (Phase 4 增强) --------+
|  挂 LSM hooks (security_*)，最优雅   |
|  需 CONFIG_BPF_LSM=y                 |
|  生产环境不一定开                    |
+--------------+------------------------+
               |
               v
+------ Userspace fallback ------------+
|  cn_proc + fanotify + /proc 轮询     |
|  永远可用                            |
+--------------------------------------+
```

**高频路径专用优化（`security_file_open`）**：

- 每次 `open()` 触发，繁忙服务器可达数万次/秒。
- BPF 程序第一行查白名单 → return。
- 第二步检查 `flags`，过滤 `O_RDONLY` 只读 open。
- 仅对 `O_WRONLY/O_RDWR/O_CREAT/O_TRUNC` 上送事件。
- 两层过滤后实际上送事件 < 5%。

---

## 6. LSM Hooks（Phase 4 增强）

### 6.1 设计取舍

| 维度 | tracepoint / kprobe | BPF LSM |
|------|---------------------|---------|
| 内核要求 | 4.18+ | 5.7+ 且 `CONFIG_BPF_LSM=y` |
| 函数 ABI 稳定 | tracepoint 稳定，kprobe 不稳 | LSM hooks ABI 极稳 |
| 是否能拦截 | tracepoint 只能观察；kprobe 极少数能改返回值 | 可返回 `-EPERM` 真拦截（仅 protect 模式启用） |
| 性能 | kprobe 慢，fentry 快 | 与 fentry 近似 |
| 生产可用 | 主流发行版默认 | RHEL 9 / Ubuntu 22.04+ 默认开，老内核未必开 |

**mxsec 决策**：

- **观察类**（exec/file_open/connect 上报）→ 优先 fentry，老内核走 kprobe，**不动用 LSM 改返回值**（即使有能力，observe 模式也不动作）。
- **拦截类**（Phase 4，仅在 `MODE=protect` 且管理员显式开启时）→ 走 BPF LSM。LSM hooks 列表见下。

### 6.2 Phase 4 拦截类 LSM Hooks

| LSM Hook | 用途 | observe 行为 | protect 行为 |
|----------|------|---------------|---------------|
| `lsm/bprm_check_security` | 进程执行准入 | 仅产 `process_exec`（不拦） | 命中 IOC/YARA 黑名单 → 返回 `-EPERM` |
| `lsm/file_open` | 文件 open 准入 | 仅产 `file_open` | 命中蜜罐文件 → 返回 `-EACCES` |
| `lsm/socket_connect` | TCP/UDP 出连接准入 | 仅产 `tcp_connect` | 命中 IOC IP → 返回 `-ECONNREFUSED` |
| `lsm/task_kill` | kill 准入 | 仅产 `signal_send` | 防止攻击者 kill Agent → 返回 `-EPERM` |
| `lsm/bprm_committed_creds` | 凭据提交 | 产 `cred_change` | 不动作（提权已发生，只能告警）|

**LSM 拦截的硬约束**（避免业务事故）：

1. 仅在 `MODE=protect` + 该 hook 显式开启 + 命中"高置信度"规则时才返回非 0。
2. 默认 dry-run：BPF Map 标志位 `enforce[lsm_hook]=0` 时仅打标 `would_block=true`，不真拦。
3. 每次拦截走熔断器：5 分钟内同 Agent 拦截 > N 次 → 自动回 dry-run。
4. Engine 必须先经 6 门槛（详见 [`operating-modes.md`](operating-modes.md) §3）才能下发 enforce=1。

---

## 7. 用户态 fallback

### 7.1 进程：cn_proc（netlink PROC_CONNECTOR）

`cnproc.go` 通过 NETLINK_CONNECTOR + PROC_CN_MCAST_LISTEN 订阅内核进程事件：

- 实时性：内核唤醒，无轮询。
- 字段缺口：内核侧只给 pid/ppid/uid/gid + event type，**exe/cmdline/cwd/env 需用户态读 /proc 补**。
- 短命进程：`/proc/<pid>` 可能在补字段时已消失，需 `O_PATH` + readlink + try-catch。
- 容量：netlink socket 缓冲区 ≤ 256KB，繁忙系统可能 ENOBUFS → 走 `/proc` 周期对账兜底。

### 7.2 文件：fanotify

`fanotify.go` 走 `FAN_CLOSE_WRITE | FAN_OPEN_PERM` mark：

- 仅监控 4 类目录：`/etc /tmp /dev/shm /home /var/log`（可配置）。
- 缺口：无 rename / unlink / chmod 事件，需 `/proc` 周期对账补。
- 降级三档（`degradation_other.go`）：
  - Level 1: 移除 `/var/log` 监控
  - Level 2: 仅保留 `/tmp /dev/shm`
  - Level 3: 关 fanotify，仅 cn_proc

### 7.3 网络：/proc/net 轮询

`procnet.go`：

- 5s 间隔，diff 上次 snapshot 出 connect / close 事件。
- 通过 inode → `/proc/<pid>/fd/*` 反查 pid（一次 O(N) 扫 /proc）。
- 缺口：5s 内的短连接会丢；inode 复用可能误关联。
- 优化：仅 diff 时扫 /proc，稳态扫描间隔 30s。

### 7.4 /proc 周期对账

`procscan.go` 是用户态采集的**进程树防断链**核心：

- 启动时一次性快照 `/proc/<pid>/{stat,cmdline,exe,cwd,status}` → 作为初始化事件批量上送（`event_type=proc_snapshot`，DataType 复用 3000 + `snapshot=true` flag）。
- 之后每 5 分钟扫一次：
  - 本地缓存有但 `/proc` 无 → 补发 `process_exit`
  - `/proc` 有但本地缓存无 → 补发 `process_exec`（cn_proc / BPF 漏抓兜底）
- 孤儿节点（ppid 不在缓存中）→ 字段标 `parent_known=false`，Engine 端 `ancestor_has()` 对该节点返回 `unknown` 而非 `false`，避免漏报。

---

## 8. DataType 分配（对照 [`datatype-allocation.md`](datatype-allocation.md)）

EDR Agent 占用的 DataType 段及当前状态（**任何新增前必须先注册**）：

| DataType | 状态 | EventType | Topic | Phase |
|----------|------|-----------|-------|-------|
| 3000 | ✅ 已分配 | process_exec / process_exit / proc_snapshot | `mxsec.agent.ebpf` | P1 |
| 3001 | ✅ 已分配 | file_open / file_write / file_rename / file_unlink / file_chmod | `mxsec.agent.ebpf` | P1 |
| 3002 | ✅ 已分配 | tcp_connect / tcp_accept / tcp_close / udp_send | `mxsec.agent.ebpf` | P1 |
| 3003 | ✅ 已分配 | dns_query / dns_response | `mxsec.agent.ebpf` | P1 |
| 3004 | ✅ 已分配 | memfd_exec / deleted_exe / anonymous_exec | `mxsec.agent.ebpf` | P1 |
| 3005 | ⚠ 预留 | signal_send（Phase 4 自保） | `mxsec.agent.ebpf` | P4 |
| 3006 | ⚠ 预留 | cred_change（Phase 4） | `mxsec.agent.ebpf` | P4 |
| 3007 | ⚠ 预留 | ptrace_attach（Phase 4） | `mxsec.agent.ebpf` | P4 |
| 3008 | ⚠ 预留 | module_load（Phase 4） | `mxsec.agent.ebpf` | P4 |
| 3010 | ✅ 已分配 | behavior_profile（BDE 快照） | `mxsec.agent.ebpf` | P10 |
| 3011-3099 | ⚠ 未分配 | 留作未来 EDR 扩展 | — | — |

> 预留段的最终生效以 [`datatype-allocation.md`](datatype-allocation.md) 评审为准。Engine 侧 `mxsec-engine` ConsumerGroup 订阅整个 `mxsec.agent.ebpf` topic，无需配合扩段。

**事件载荷统一结构**（与 `event.Event` 对齐）：

```go
type Event struct {
    DataType  int32             // 3000-3010
    EventType EventType         // "process_exec" 等
    Timestamp time.Time         // 内核时钟（CLOCK_MONOTONIC 转 UTC）
    Fields    map[string]string // 共享 KV，与 bridge.Payload.Fields 同构
}
```

`Fields` 中**所有 Agent 必填字段**（Engine 信任）：

| Key | 来源 | 必填 |
|-----|------|------|
| `event_type` | 采集层 | ✅ |
| `pid`, `ppid` | 内核 | ✅ |
| `exe`, `cmdline` | /proc | ✅（process_exec）|
| `uid`, `gid` | 内核 | ✅ |
| `cgroup` | cgroup 路径 | ✅ |
| `container_id`, `container_name`, `container_image` | container/resolver.go | 容器场景 ✅ |
| `k8s_namespace`, `k8s_pod`, `k8s_node` | Downward API / Kubelet | K8s 场景 ✅ |
| `tenant_id` | Agent 配置 | ✅ **多租户硬约束** |
| `agent_id` | Agent UUID | ✅ |
| `host_id` | Server 注册返回 | ✅ |
| `agent_timestamp_ns` | CLOCK_MONOTONIC 转 UTC | ✅ |
| `agg_count`, `agg_window` | aggregator.go | 聚合场景 |
| `anomaly_hint` | 本地启发式 | 启发式命中场景 |
| `parent_known` | procscan.go | 孤儿节点场景 |

---

## 9. 事件聚合 + 去重 + 限速

### 9.1 聚合（`aggregator.go`）

| 项 | 设计 |
|----|------|
| 触发条件 | 同 `aggKey` 在 `aggregateWindow=10s` 内重复 |
| aggKey 规则 | 进程: `event_type|exe`<br>文件: `event_type|file_path`<br>网络: `event_type|remote_addr:remote_port`<br>其他: `event_type` |
| Bypass 条件 | `agent_match=true`（本地启发式打标）或 `ioc_match=true`（IOC 命中）→ 直接放行不聚合 |
| 窗口首条 | 立即放行（保证 P99 延迟）|
| 窗口内重复 | 累加 `count`，10s 窗口结束时若 `count > 1` 上送一条 `agg_count=N agg_window=10s` 的汇总事件 |
| 内存上限 | 10000 entries（LRU 淘汰最旧）|
| 上送时机 | 后台 `Flush()` goroutine 每 1s 调用一次 |

**聚合后效果实测目标**（基线：单主机 ~2000 EPS）：

- 进程 exec：`/usr/bin/ls` 类高频 → 10s 内聚合 1 条，节省 ~95%
- 文件 open：`/var/log/messages` 类持续写 → 10s 内聚合 1 条，节省 ~99%
- TCP connect：`prometheus_node_exporter` 类周期上报 → 10s 内聚合 1 条，节省 ~90%
- **整体降噪 60-80%**，从 2000 EPS → 400-800 EPS 上送

### 9.2 去重（与聚合协同）

- 严格去重通过 `aggKey` 自然实现。
- 进一步在 5 分钟级别用 LRU Set 防止"窗口外重复"风暴（罕见场景，例如某进程每 11 秒触发一次）。
- 去重只针对**非安全事件**；命中本地启发式或 IOC 必上送，不去重。

### 9.3 限速（多档）

```
+ Layer A: 内核态 BPF Map 白名单 ----+
|  whitelist_map LRU_HASH，pid/comm  |
|  Server 下发，Agent 启动加载       |
|  典型节省 60% 内核事件             |
+--+----------------------------------+
   |
   v
+ Layer B: 用户态 Bloom Filter ------+
|  补内核白名单不全的场景             |
|  fpr=0.1%, 1MB 内存                |
|  典型节省额外 30%                  |
+--+----------------------------------+
   |
   v
+ Layer C: 10s 窗口聚合 -------------+
|  aggregator.go，60-80% 降噪        |
+--+----------------------------------+
   |
   v
+ Layer D: 资源降级 4 级 ------------+
|  degradation.go 自感知 CPU         |
|  Level 1: CPU > 60% → 丢 file_open |
|           / udp_send 低危事件      |
|  Level 2: CPU > 80% → 仅留         |
|           process_exec / file_write|
|           / tcp_connect            |
|  Level 3: CPU > 95% → 仅留         |
|           process_exec             |
|  Level 4: CPU > 98% 持续 5min →    |
|           停止采集 + 上报告警       |
+--+----------------------------------+
   |
   v
最终上送 (gRPC 流 + Snappy)
```

每一档都通过 `config_map[idx] = level` 在 BPF / 用户态共享，**用户态可热调**（Server 下发命令 → Agent 写 BPF Map），无需重启 Agent。

---

## 10. 性能 SLO

### 10.1 目标值（对外承诺，对标 [`architecture.md`](architecture.md) §8.3）

| 指标 | 目标 | 工业级口径 | 测量来源 |
|------|------|-----------|----------|
| CPU 稳态 | **< 3%**（单核） | CrowdStrike / SentinelOne 同档 | `/proc/self/stat` + 5min 滑窗 |
| CPU 峰值（短时） | < 8% | 压测 | 同上 P99 |
| RSS | **< 80 MB** | 略宽于青藤 40MB（CO-RE + BPF Map 占用） | `/proc/self/status` VmRSS |
| 单事件用户态处理延迟 | < 100 µs | benchmark | go-bench |
| 事件吞吐 | ≥ 5000 EPS 持续 | 压测 | 每秒输出 |
| 聚合命中率 | ≥ 60% 降噪 | 实测 | aggregator metrics |
| WAL 写入延迟 | < 100 µs / event | 顺序追加 | benchmark |
| 启动到首事件延迟 | < 3s | 含 BPF verifier | 启动日志 |
| 升级期间事件丢失 | 0（WAL 兜底） | 工业级口径 | WAL 一致性 |
| 心跳间隔 | 30s | 与 AgentCenter Keepalive 配套 | — |

### 10.2 性能预算分摊（详细）

| 子模块 | CPU 预算 | 内存预算 | 备注 |
|--------|----------|----------|------|
| eBPF Verifier / Loader（启动期）| 一次性 ≤ 1.5s | 0 | 运行期不再消耗 |
| BPF Programs（内核态） | 由内核调度，不计入用户态 % | Map + Ringbuf ≈ 25 MB | verifier 限制 stack ≤ 512B |
| Ringbuf 读取 goroutine | 0.5% | 2 MB | 单 goroutine + back-pressure |
| 事件解码 + struct 构造 | 0.5% | 5 MB（sync.Pool） | binary.Read + 字符串复用 |
| 富化（container/k8s） | 0.3% | 10 MB cache (TTL 5min) | runtime CLI 调用走 LRU |
| 启发式（memfd/SUID/反弹 shell） | 0.5% | 5 MB | /proc 周期扫 |
| 聚合 + 限速 | 0.2% | 5 MB | 10000 entries LRU |
| BDE Profiler | 0.3% | 8 MB | 15s 窗口 |
| WAL 写盘 | 0.2% | 10 MB sender buffer | 顺序追加 |
| Storyline tracker | 0.1% | 6 MB | 10000 entries, 2h TTL |
| 自保护 / Watchdog 互保 | 0.1% | 2 MB | Unix Socket 1s 心跳 |
| **合计** | **~2.7%** | **~78 MB** | **达标** |

### 10.3 调优指南

**CPU 高排查路径**：

1. 心跳 `events_per_sec` > 5000 → 检查白名单是否生效（`whitelist_map_size`）
2. `eval_latency_p99_us` > 200 → 检查聚合是否打开（aggregator metrics）
3. `degradation_level > 0` → 主机本身 CPU 占用过高，Agent 自动降级
4. `cpu_percent > 5%` 持续 → 切换到 kprobe（如当前是 fentry，反查内核版本异常）

**RSS 高排查路径**：

1. `wal_buffer_used_mb > 100` → 检查 sender 是否 stuck（gRPC 流断了 / Kafka 不通）
2. `container_cache_size > 2000` → 容器爆炸，调低 cacheMaxSize
3. `storyline_entries > 10000` → TTL 设置过长或泄漏，检查 Tracker
4. `bpf_map_memory > 30MB` → 调小 whitelist_map / event_ringbuf 容量

**事件丢失排查**：

1. `events_dropped_total` 递增 → Ringbuf 满 → 用户态消费跟不上
2. WAL 写满（`wal_buffer_used_mb > 400`）→ Kafka 不可达 + 网络长时间断
3. cn_proc ENOBUFS → 通过 procscan 兜底（5min 内必有 diff）

---

## 11. 内核兼容矩阵

### 11.1 支持范围（Linux only）

| 发行版 | 最低内核 | eBPF 模式 | 备注 |
|--------|----------|-----------|------|
| CentOS 7 / RHEL 7 | 3.10 | ❌ → 用户态 | EOL，仅维护版本 |
| CentOS Stream 8 / RHEL 8 | 4.18 | ✅ kprobe（无 fentry） | 主流政企 |
| Ubuntu 18.04 LTS | 4.15 → 5.4 HWE | ✅ kprobe（4.15）/ fentry（5.4+） | — |
| Ubuntu 20.04 LTS | 5.4 → 5.15 HWE | ✅ fentry | 推荐 |
| Ubuntu 22.04 LTS | 5.15 | ✅ fentry + BPF LSM | 最佳 |
| Debian 10 | 4.19 | ✅ kprobe | — |
| Debian 11 / 12 | 5.10 / 6.1 | ✅ fentry / + LSM | — |
| openEuler 22.03 LTS | 5.10 | ✅ fentry | 信创 |
| Anolis OS 8 | 4.18 / 5.10 | ✅ kprobe / fentry | 信创 |
| Kylin V10 | 4.19 | ✅ kprobe | 信创 |
| UOS V20 | 4.19 | ✅ kprobe | 信创 |

> **不支持平台**：Windows、macOS、Solaris、FreeBSD。这是 mxsec 工业级 CWPP 的产品边界，**永不破例**。

### 11.2 CO-RE 必要条件

- 内核编译时 `CONFIG_DEBUG_INFO_BTF=y`（主流发行版默认开）。
- 缺 BTF 时 → Agent 内嵌 `vmlinux.h` snapshot + BTFHub fallback。BTFHub 离线包随 Agent 发布。
- 内核 < 4.18 时直接走用户态，**不尝试加载 BPF**（避免老内核 verifier bug）。

### 11.3 内核探测代码骨架

```go
// internal/agent/edr/collector/hookdetect.go (现状对齐)

type KernelCaps struct {
    Major, Minor   int
    HasBTF         bool
    HasFentry      bool
    HasKprobe      bool
    HasBPFLSM      bool      // Phase 4
    HasRingbuf     bool
    HasCGroupV2    bool
}

func probeKernelCaps() KernelCaps {
    var c KernelCaps
    c.Major, c.Minor = parseKernelVersion()
    c.HasBTF = fileExists("/sys/kernel/btf/vmlinux")
    c.HasRingbuf = c.Major > 5 || (c.Major == 5 && c.Minor >= 8)
    c.HasFentry = ebpf.HaveProgramType(ebpf.Tracing) == nil
    c.HasKprobe = ebpf.HaveProgramType(ebpf.Kprobe) == nil
    c.HasBPFLSM = ebpf.HaveProgramType(ebpf.LSM) == nil &&
                  fileExists("/sys/kernel/security/bpf")
    c.HasCGroupV2 = fileExists("/sys/fs/cgroup/cgroup.controllers")
    return c
}
```

---

## 12. 容器富化（Resolver）

### 12.1 多 runtime 自动探测

`internal/agent/edr/container/resolver.go` 现状：

```go
// detectRuntime 顺序探测，返回首个找到的 runtime
func (r *Resolver) detectRuntime() string {
    candidates := []struct {
        sock    string
        runtime string
    }{
        {"/run/containerd/containerd.sock", "containerd"},  // K8s 默认
        {"/run/crio/crio.sock", "crio"},                    // OpenShift
        {"/var/run/cri-dockerd.sock", "cri-dockerd"},       // 老 K8s + Docker
        {"/var/run/docker.sock", "docker"},                 // 独立 Docker
        {"/run/podman/podman.sock", "podman"},              // RHEL 9+
    }
    for _, cand := range candidates {
        if _, err := os.Stat(cand.sock); err == nil {
            if hasCLI(cand.runtime) {
                return cand.runtime
            }
        }
    }
    return ""
}
```

### 12.2 容器 ID 提取

**eBPF 模式**：BPF 内 `task_struct → css → cgroup → kn → name`，直接拿 cgroup 路径，user-space 解析。

**用户态模式**：`/proc/<pid>/cgroup` 解析：

```
# containerd / cri-containerd（K8s）
12:memory:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod<uid>.slice/cri-containerd-<cid>.scope
# → container_id = <cid> 截前 12 字符
# → k8s_pod_uid = <uid>

# CRI-O
/kubepods.slice/.../crio-<cid>.scope

# cri-dockerd
/kubepods/.../docker/<cid>

# 独立 Docker
/docker/<cid>

# Podman
/libpod_parent/libpod-<cid>.scope
```

### 12.3 元数据查询（CLI 走，避免 SDK 依赖）

| Runtime | 命令 | 超时 |
|---------|------|------|
| containerd | `ctr -n k8s.io containers info <cid>` | 3s |
| crio | `crictl inspect <cid>` | 3s |
| cri-dockerd | `docker inspect <cid>` 或 `crictl` | 3s |
| docker | `docker inspect <cid>` | 3s |
| podman | `podman inspect <cid>` | 3s |

**缓存策略**：LRU 2000 条 + TTL 5min，CLI 调用失败时仅保留 `container_id`，其他字段空。

### 12.4 K8s 上下文注入（三级 fallback）

```
+----- Level 1 (优先) -----+
| Downward API 环境变量    |
| MY_POD_NAME / NAMESPACE  |
| / NODE_NAME              |
+----- Level 2 -----+
| Kubelet 10250 API        |
| GET /pods → 按 container |
| _id 匹配                 |
+----- Level 3 -----+
| /proc/<pid>/mountinfo +  |
| service account secret   |
+--------------------+
```

注入字段：`k8s_pod`, `k8s_namespace`, `k8s_node`, `k8s_deployment`, `k8s_service_account`, `k8s_host_network`。

---

## 13. 内存马 / 本地启发式

> **重要**：所有本地启发式仅做"打标"（`anomaly_hint=<reason>` + `agent_match=true` 走聚合 bypass），**不在 Agent 端产生告警**。Engine 收到打标事件后按 CEL 规则 + 上下文决定是否告警。这是为了保证规则中心化、可灰度。

### 13.1 内存马（`internal/agent/edr/memfd/scanner.go`）

| 启发式 | 触发条件 | DataType | EventType |
|--------|----------|----------|-----------|
| **memfd-backed exec** | `process_exec` 时 `/proc/<pid>/exe` 链接指向 `memfd:` 或 `/proc/self/fd/<n>` | 3004 | `memfd_exec` |
| **deleted_exe** | `/proc/<pid>/exe → /path/to/binary (deleted)` | 3004 | `deleted_exe` |
| **anonymous rwx** | `/proc/<pid>/maps` 含 `[anon]` 段且权限含 `rwx` | 3004 | `anonymous_exec` |
| **fileless from /tmp + ld** | exe 在 `/tmp /dev/shm /run/user`，且 cmdline 含 `ld.so --library-path` | 3004 | `anonymous_exec` + hint=`ld_so_tmp` |

**实现要点**：

- eBPF 模式：`process_exec` 触发后异步入 `memfdEventCh`（深度 64），扫描 goroutine 处理。
- 用户态：每 10s 扫一次 `/proc/<pid>/{maps,fd,exe}`，仅对 `first_seen` 进程或外连进程做。
- 限速：单进程 5min 内同种 hint 只打 1 次标。
- 不阻塞内核 hot path。

### 13.2 反弹 shell 启发式

不在 Agent 做完整匹配（避免规则分散），仅做"**强信号特征**"打标：

| 启发式 | 触发条件 |
|--------|----------|
| `/dev/tcp/` 重定向 | cmdline 含 `>& /dev/tcp/` 或 `0>&1` 且 exe 是 shell |
| socat 反弹模式 | exe=`socat` 且 cmdline 含 `EXEC:.*pty,stderr,setsid` |
| python pty.spawn | cmdline 含 `pty.spawn` 且包含 socket import |
| nc -e | exe=`nc/ncat` 且 cmdline 含 `-e /bin/` |

打标方式：在 `process_exec` event 上加 `anomaly_hint=reverse_shell_<flavor>` + `agent_match=true`。

### 13.3 SUID / 本地提权启发式

| 启发式 | 触发条件 |
|--------|----------|
| `chmod +s` | `file_chmod` 事件，新模式含 SUID/SGID 位 |
| 新增 SUID 文件 | 该 inode 历史无 SUID，本次首次设置 |
| 非系统目录 SUID | `file_path` 不在 `/usr /bin /sbin` 且设置 SUID |
| commit_creds 异常 (Phase 4) | `cred_change` 事件，新 uid=0 且原 uid != 0 且无父链可解释 |

### 13.4 与 Engine 协作

```
Agent 打标流程:
+----------------------------+
| eBPF/userspace 上送原始事件 |
+--------------+--------------+
               |
               v
+------ 启发式扫描 -------+
| memfd / deleted_exe /   |
| anon rwx / reverse_shell|
| / SUID set / ...        |
+--------------+--------------+
               |
       +-------+--------+
       |                |
       v                v
+--- 未命中 ---+   +--- 命中 ---+
| 走聚合       |   | 加 fields:|
| 10s 窗口     |   | anomaly_  |
| 可能被合并   |   | hint=<r>  |
+--------------+   | agent_    |
                   | match=true|
                   | bypass 聚合|
                   +--+--------+
                      |
                      v
                  gRPC stream
                      |
                      v
                  Engine 接管
                  CEL/序列/ML
                  最终告警决定
```

---

## 14. 自保护（selfprotect.go 升级）

### 14.1 现状

```go
// internal/agent/edr/selfprotect.go (现状)
// - sd_notify(READY=1) + WATCHDOG=1 与 systemd 协作
// - chattr +i 对 /usr/local/mxsec/bin/mxsec-agent + /etc/mxsec/
// - 启动期完整性校验（二进制 sha256）
```

### 14.2 v2.0 升级清单

| 层 | 现状 | 升级 |
|----|------|------|
| L1 Watchdog 双进程 | ❌ | 新增 `mxsec-watchdog` 5MB 守护进程，与 Agent 通过 Unix Domain Socket 1s 心跳互保，任一被杀 1s 内拉起 |
| L2 chattr +i 文件保护 | ✅ | 保持；扩展到规则缓存目录 `/var/lib/mxsec/rules/` |
| L3 信号监控 | ❌（仅 eBPF 可做） | Phase 4 增加 `tp/signal_deliver`（DataType 3005），监控发往 Agent 的 SIGKILL/SIGTERM/SIGSTOP，记录攻击者 pid/comm/uid 上送，**仅打标不阻断**（eBPF 无法阻 kill） |
| L4 Server 心跳超时 | ✅ | 保持；超时 3min 升级为 `agent_tamper` 告警 |
| L5 配置完整性校验 | ✅ | 保持；启动期 + 每 30min |
| L6 BPF 程序自校验 | ❌ | 启动后定期 `bpftool prog show` diff，BPF 程序被 detach → 告警 + 重 attach |
| L7 mTLS 证书保护 | ✅（chattr +i） | 保持 |

### 14.3 自保护设计原则

1. **自保 ≠ 不可卸载** — root 用户通过官方 `mxctl uninstall` 可正常卸载。
2. **防的是攻击者 `kill -9` / `rm -rf`，不是合法运维** — 运维操作走 mxctl，绕开自保检测。
3. **不阻断任何进程** — 仅检测 + 告警 + 重启。
4. **自保失败不影响主机** — 最坏 = 失去监控，不影响业务进程。

---

## 15. 与 Server 侧 Engine 的解耦细节

### 15.1 红线

| 行为 | Agent 做？ | Engine 做？ |
|------|-----------|-------------|
| eBPF / 用户态采集 | ✅ | ❌ |
| 容器 / K8s 富化 | ✅ | ❌（信任 Agent） |
| 启发式打标（anomaly_hint） | ✅ | ❌ |
| 字段脱敏（cmdline 中的 password） | ✅ | ❌（敏感数据不出主机） |
| CEL 规则评估 | ❌ | ✅ |
| 序列检测 / Storyline 聚合 | ❌（仅 PID 因果打 story_id） | ✅ |
| ML 推理（IForest / MiniLM） | ❌（Phase 17 可选 Agent 端 ELF 静态 ML，本设计不含） | ✅ |
| IOC 实时碰撞 | ⚠ 本地 cache O(1) 命中产 `ioc_match=true`，但**告警仍在 Engine 生成** | ✅ |
| 告警生成 | ❌ | ✅ |
| 处置动作 | ✅ 执行（仅 protect 模式 + Engine 指令） | ✅ 编排（observe 仅打 `would_action`） |

### 15.2 IOC 本地缓存的边界

- Agent `ioc/store.go` 维护本地 cache：IP Set / Hash Set / Domain Set，O(1) `SISMEMBER`。
- Server 通过 DataType 9300（IOC 分发）full + diff 推送，Agent 5min 一次心跳报告当前 IOC 版本。
- 命中只产 `ioc_match=true` + `ioc_source=<feed>` + `ioc_score=<int>` 字段，**不在 Agent 直接生成告警**。
- 之所以让 Agent 命中而非全靠 Engine：典型政企 1000+ host × 400 EPS = 400K EPS，全部走 Redis 碰撞会拖死 Engine；本地 cache 命中后只把"已命中"事件上送，节省 80% Redis QPS。

### 15.3 mode 字段传递

- Agent 接收 Server 下发的当前 mode（全局 / 租户 / 主机标签 / 规则四级合并后的最终态）。
- Agent **不自己决定** observe / protect；mode 仅影响 Agent 是否执行 Engine 下发的处置指令。
- 处置指令收到时检查 `cmd.mode == "protect"`，否则只回 ack 不执行（"mode mismatch, refused"）。

---

## 16. 配置示例

### 16.1 `/etc/mxsec/agent.yaml`

```yaml
# /etc/mxsec/agent.yaml — Agent 主配置
agent:
  id_file: /var/lib/mxsec/agent_id      # UUID 持久化文件
  tenant_id: t-bank-a                   # 必填：多租户隔离
  log_dir: /var/log/mxsec
  log_level: info
  data_dir: /var/lib/mxsec

server:
  endpoint: agentcenter.mxsec.svc:6751  # AgentCenter gRPC
  tls:
    ca_file: /etc/mxsec/certs/ca.crt
    cert_file: /etc/mxsec/certs/agent.crt
    key_file: /etc/mxsec/certs/agent.key
  keepalive:
    time: 60s
    timeout: 10s
  heartbeat_interval: 30s

edr:
  enabled: true

  collector:
    # 模式自动探测，可强制覆盖（调试用）
    mode: auto                          # auto / ebpf / userspace
    sample_rate: 100                    # 0-100
    ringbuf_size_kb: 4096               # BPF Ringbuf 容量
    whitelist:
      # 内核态白名单（推 BPF Map，O(1) 丢弃）
      pids: []                          # 一般留空，运维场景用
      comms:
        - node_exporter
        - prometheus
        - kube-proxy
        - calico-node
      exe_paths:
        - /opt/ansible/*
        - /usr/local/zabbix/*
    fanotify:
      enabled: true                      # 仅用户态模式生效
      paths:
        - /etc
        - /tmp
        - /dev/shm
        - /home
        - /var/log

  events:
    # 事件类型开关（与 datatype-allocation 对齐）
    process_exec:   { enabled: true }
    process_exit:   { enabled: true }
    file_open:      { enabled: true, only_write: true }    # BPF 内仅放写权限 open
    file_write:     { enabled: true }
    file_rename:    { enabled: true }
    file_unlink:    { enabled: true }
    file_chmod:     { enabled: true }
    tcp_connect:    { enabled: true }
    tcp_accept:     { enabled: true }
    tcp_close:      { enabled: false }   # 默认关，仅流量统计场景开
    udp_send:       { enabled: true }
    dns_query:      { enabled: true }
    dns_response:   { enabled: false }
    memfd_exec:     { enabled: true }
    deleted_exe:    { enabled: true }
    anonymous_exec: { enabled: true }
    behavior_profile: { enabled: true, window: 15s }

  aggregator:
    window: 10s
    max_entries: 10000
    flush_interval: 1s

  degradation:
    enabled: true
    thresholds:
      level1_cpu_percent: 60
      level2_cpu_percent: 80
      level3_cpu_percent: 95
      level4_cpu_percent: 98
      level4_duration: 5m

  wal:
    enabled: true
    dir: /var/lib/mxsec/wal
    max_size_mb: 500
    segment_size_mb: 10

  container:
    enabled: true
    cache_ttl: 5m
    cache_max_size: 2000
    runtimes:
      - containerd
      - crio
      - cri-dockerd
      - docker
      - podman

  k8s:
    enabled: true
    method: auto                         # auto / downward_api / kubelet / mountinfo
    kubelet_endpoint: https://127.0.0.1:10250

  heuristics:
    memfd: true
    deleted_exe: true
    anonymous_exec: true
    reverse_shell: true
    suid_set: true

  selfprotect:
    watchdog_enabled: true
    watchdog_socket: /var/run/mxsec-watchdog.sock
    watchdog_interval: 1s
    chattr_enabled: true
    bpf_integrity_check_interval: 1m
```

### 16.2 心跳上报样例（DataType 1000）

```json
{
  "agent_id": "ag-7f3e2a1b",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "timestamp": "2026-06-05T10:00:00Z",
  "version": "2.0.3",
  "edr": {
    "collector_mode": "ebpf_full",
    "hook_type": "fentry",
    "kernel": "5.15.0-119-generic",
    "capabilities": {
      "ebpf_full": true,
      "ebpf_lsm": false,
      "ebpf_memory": true,
      "container_ctx": true
    },
    "events_per_sec": 380,
    "events_per_sec_by_type": {
      "process_exec": 45,
      "file_open": 180,
      "tcp_connect": 120,
      "dns_query": 35
    },
    "aggregator_hit_rate": 0.72,
    "degradation_level": 0,
    "wal_buffer_used_mb": 0.3,
    "wal_events_pending": 120,
    "events_dropped_total": 0,
    "heuristic_hits": {
      "memfd": 0,
      "deleted_exe": 0,
      "reverse_shell": 0,
      "suid_set": 1
    },
    "container_cache_size": 87,
    "selfprotect": {
      "watchdog_alive": true,
      "chattr_intact": true,
      "bpf_progs_loaded": 11
    }
  },
  "health": {
    "cpu_percent": 2.3,
    "mem_rss_mb": 56,
    "fd_used": 38,
    "uptime_sec": 864000
  }
}
```

---

## 17. Go 接口骨架（关键模块）

### 17.1 Collector 接口

```go
// internal/agent/edr/collector/collector.go (现状对齐 + 微调)
package collector

import (
    "context"

    "go.uber.org/zap"

    "github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// Collector is the unified collector interface.
type Collector interface {
    Start(ctx context.Context, out chan<- *event.Event) error
    Mode() string
    Capabilities() map[string]bool
    SetDegradationLevel(level int)
    Stop() error
}

// DetectAndCreate auto-selects the best collector for the running kernel.
// Order: ebpf(fentry) → ebpf(kprobe) → userspace.
func DetectAndCreate(log *zap.Logger) (Collector, error) {
    caps := probeKernelCaps()

    if caps.HasFentry && caps.HasBTF {
        if c, err := newEBPFCollector(log, HookFentry); err == nil {
            return c, nil
        } else {
            log.Warn("fentry collector init failed, trying kprobe", zap.Error(err))
        }
    }
    if caps.HasKprobe && caps.HasBTF {
        if c, err := newEBPFCollector(log, HookKprobe); err == nil {
            return c, nil
        } else {
            log.Warn("kprobe collector init failed, falling back to userspace", zap.Error(err))
        }
    }
    return newUserspaceCollector(log), nil
}
```

### 17.2 启发式扫描器接口

```go
// internal/agent/edr/heuristics/heuristics.go (新增)
package heuristics

import (
    "github.com/imkerbos/mxsec-platform/internal/agent/edr/event"
)

// Scanner 是所有本地启发式扫描器的统一抽象。
// Scan 返回需要追加的标签 KV（空表示未命中），不修改原 event。
type Scanner interface {
    Name() string
    Scan(evt *event.Event) map[string]string
}

// Hub 顺序执行多个 Scanner，命中即附加到 evt.Fields。
type Hub struct {
    scanners []Scanner
}

func (h *Hub) Apply(evt *event.Event) {
    for _, s := range h.scanners {
        tags := s.Scan(evt)
        if len(tags) == 0 {
            continue
        }
        for k, v := range tags {
            evt.Fields[k] = v
        }
        evt.Fields["agent_match"] = "true"
        evt.Fields["agent_match_by"] = s.Name()
    }
}
```

### 17.3 容器富化接口（现状对齐）

```go
// internal/agent/edr/container/resolver.go (现状)
type Resolver struct { /* ... */ }

func NewResolver(logger *zap.Logger) *Resolver
func (r *Resolver) Resolve(pid int) *Info
func (r *Resolver) Close() error

type Info struct {
    ContainerID string
    Name        string
    Image       string
    Runtime     string
    Labels      map[string]string
    PodName     string
    Namespace   string
    PodUID      string
}
```

### 17.4 事件聚合接口（现状对齐）

```go
// internal/agent/edr/aggregator.go (现状)
type eventAggregator struct { /* ... */ }

func newEventAggregator(logger *zap.Logger) *eventAggregator
func (a *eventAggregator) TryAggregate(evt *event.Event) bool   // true = 已缓冲
func (a *eventAggregator) Flush() []*event.Event                // 窗口到期吐汇总
```

---

## 18. 实施路径（Phase 1-6 与 [`engine-design.md`](engine-design.md) 同步）

| Phase | 主要交付 | 预计 |
|-------|----------|------|
| **P1 基础采集** | eBPF 进程/文件/网络/DNS + 用户态 fallback + /proc 对账 + 容器富化 + WAL | 4-6 周（已完成）|
| **P2 启发式打标** | memfd / deleted_exe / anon rwx / 反弹 shell / SUID + 聚合 + IOC 本地 cache | 2-3 周（基本完成）|
| **P3 多 runtime + K8s** | crio / cri-dockerd / podman 适配 + K8s Downward API + Kubelet API | 2 周（已完成）|
| **P4 LSM + 新事件** | LSM hooks + signal/cred/ptrace/module_load 4 类事件（DataType 3005-3008）+ Watchdog 双进程 | 3-4 周 |
| **P5 性能压测** | 持续 5000 EPS + CPU < 3% + RSS < 80MB 实测验收 + 资源降级 4 级 | 2 周 |
| **P6 升级 + 灰度** | Agent 自更新 + Canary 灰度 + 自动回滚 | 2-3 周 |

---

## 19. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| 内核版本碎片 | 老 CentOS 7 / 信创 OS 不能用 eBPF | 用户态 fallback + 能力 Tier 声明，规则按 Tier 下发 |
| BPF verifier 限制 | 程序超 4096 instructions 拒绝加载 | 拆分多个 BPF 程序 + 每个 Hook 入口白名单 fast-path |
| BTF 缺失 | CO-RE 失败 | 内嵌 vmlinux.h snapshot + BTFHub 离线包 |
| Ringbuf 拥塞 | 用户态消费跟不上 | 双 Ringbuf 优先级 + 资源降级丢低优先事件 + WAL 兜底 |
| /proc 扫描重 | 万级进程主机扫一次百 ms | 仅扫 diff + 5 分钟周期 + nice 19 |
| 容器 CLI 阻塞 | runtime CLI 卡住 → 富化 goroutine 堆积 | 3s 超时 + LRU cache + 失败仅保留 container_id |
| 启发式误打标 | Agent 标错 → Engine 误告警 | 启发式只打 `anomaly_hint`，最终决定在 Engine + CEL 白名单 |
| Agent 被 kill | 失去监控 | Watchdog 双进程 + sd_notify + Server 心跳超时告警 |
| WAL 磁盘占用 | 长断网写满 | 500MB 上限 + 淘汰最旧 + 磁盘使用率监控 |
| 升级期间事件丢失 | 重启窗口 | WAL 持久化 + sd_notify ready 后才接管事件 |
| 多租户配置错乱 | `tenant_id` 漏配 → 事件归错租户 | 启动期校验 `tenant_id` 必填，否则拒绝启动 + 告警 |
| LSM enforce 误拦截业务 | protect 模式下规则错 | 6 门槛准入（详见 [`operating-modes.md`](operating-modes.md)）+ dry-run 默认 + 熔断器 |
| eBPF 兼容性 bug | 不同内核 BPF helper 差异 | CI 在 4.18 / 5.4 / 5.15 / 6.1 四档容器跑回归 |

---

## 20. 与对标产品的差异

| 维度 | mxsec EDR Agent | 青藤万象 | 蜂巢容器 |
|------|------------------|----------|----------|
| 内核采集 | eBPF (fentry/kprobe) + 用户态 fallback | "非侵入式"用户态为主 | 用户态为主 |
| 事件类型 | 13 类（P1 完成 10 类 + P4 增 3 类） | 5/55/126 类三档套餐 | 容器 3-4 大类 |
| 容器 runtime | containerd / crio / cri-dockerd / docker / podman 自动探测 | 万相主推主机 | 蜂巢主推容器 |
| K8s 上下文 | Downward + Kubelet + mountinfo 三级 | 万相部分支持 | ✅ Pod/Namespace/labels 完整 |
| 聚合去噪 | 10s 窗口同签名 | 资料未明示，提"事件过滤" | 同上 |
| 自保护 | Watchdog 双进程 + chattr + signal eBPF 监控（P4） | 加壳 + 抗逆向 + 2min 自启 | 同上 |
| 内存马 | memfd / deleted_exe / anon rwx 启发式打标，Java RASP 走 plugin（P4） | Java RASP 类加载监控 + 反编译下载 | 容器内 RASP |
| 多租户 | from-day-1，tenant_id 全程贯穿 | SaaS 多租户 | 同上 |
| 运行模式 | 默认 observe，磨合后 protect | 默认部分自动响应 | 同上 |
| 平台范围 | **Linux + K8s only**（不做 Win/Mac） | 万相 Linux + Win | 容器 only |

---

## 21. 参考文档

- [`architecture.md`](architecture.md) — 六微服务总图
- [`operating-modes.md`](operating-modes.md) — 监听 / 防护双模式
- [`multi-tenant.md`](multi-tenant.md) — 多租户 tenant_id 全程贯穿
- [`engine-design.md`](engine-design.md) — Server 侧检测引擎（与本文档对偶）
- [`engine-detection-design.md`](engine-detection-design.md) — Engine 检测细节
- [`datatype-allocation.md`](datatype-allocation.md) — DataType 注册表
- [`falco-sigma-integration.md`](falco-sigma-integration.md) — 规则中台（Engine 侧消费 Agent 事件）
- [`asset-model.md`](asset-model.md) — 资产模型与 host_id / agent_id 关系
- [`security-objectives.md`](security-objectives.md) — 三大产品目标
- `ref/04-运行时.md` — 运行时模块差距分析（青藤 / 蜂巢对比）
- `ref/02-Agent.md` — Agent 框架对标
- `ref/appendix/_raw/wanxiang-host.txt` — 青藤万象白皮书原文
- `ref/appendix/_raw/fengchao-container.txt` — 青藤蜂巢容器白皮书原文
