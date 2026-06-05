# Falco / Sigma / Tetragon 规则集成

> **平台定位**：mxsec 是**工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**，面向 ToB 政企 / 金融 / 互联网。
>
> **本文档定位**：定义 mxsec **不自维护规则**的核心战略 — Engine 复用 **Falco rules** / **Sigma Rules** / **Tetragon Tracing Policies** 三大社区资产，通过统一的 CEL 中间表达式驱动六微服务架构中的检测层。
>
> **关键约束**：
> 1. 规则**默认监听**（`MODE=observe`），磨合达标后按 [`operating-modes.md`](operating-modes.md) §3 流程切防护；
> 2. 规则**带租户作用域**（`tenant_id` 全程贯穿），见 [`multi-tenant.md`](multi-tenant.md) §7；
> 3. 规则在 **Engine** 微服务执行（不是 Manager、不是 Consumer），见 [`architecture.md`](architecture.md) §2.4。

---

## 1. 集成动机：为什么不自维护规则

### 1.1 自维护规则的死路

工业级 CWPP 的检测规则不是"写几条 YAML"的事，至少要做到：

| 维度 | 要求 |
|------|------|
| 规模 | 覆盖 MITRE ATT&CK 全部 14 战术 / 200+ 技术点，对应 1000+ 规则 |
| 时效 | 每次新 CVE / 在野利用 / 新攻击组件曝光后，**24h 内**有可用规则 |
| 质量 | 经红队、蓝队真实环境验证过，假阳性 ≤ 2% |
| 维护 | 内核版本 / 发行版 / 容器运行时 / K8s 版本 升级时同步适配 |
| 信任 | 客户（金融 / 监管 / 国央企）愿意相信规则来源 |

任何单一团队（包括头部厂商）自养这套规则库都会被拖垮。青藤"雷火引擎"团队 50+ 人专职，仍要依靠社区情报融合；CrowdStrike 全球 SOC + 威胁狩猎团队上千人才能撑住自有规则。

**mxsec 作为工业级开源产品，唯一可行路线是：复用社区规则 + 私有规则补充**。

### 1.2 三大社区资产的成熟度

| 规则集 | License | 规模 | 维护者 | 覆盖范围 |
|--------|---------|------|--------|---------|
| Falco rules | Apache-2.0 | 100+ | CNCF Falco / Sysdig | Linux 系统调用 + 容器运行时 + K8s Audit |
| Sigma Rules | DRL-1.1（permissive） | 3000+ | SigmaHQ + 300+ 贡献者 | 通用 SIEM 检测语义（覆盖 EDR / 网络 / 云 / 应用） |
| Tetragon Tracing Policies | Apache-2.0 | 80+（核心）+ 社区扩展 | Cilium / Isovalent | eBPF 行为策略（进程、文件、网络、K8s 上下文） |

三套规则**互补不重叠**：

- Falco rules 强在**系统调用细节**（execve / open / connect）+ **容器原语**（container.id / container.image），是云原生 Workload Protection 的事实标准；
- Sigma Rules 强在**跨数据源的检测语义**（日志 + 进程 + 网络一体），ATT&CK 战术覆盖最广；
- Tetragon Tracing Policies 强在**内核行为约束**（kprobe / tracepoint / LSM），原生表达"哪些 syscall 在哪种上下文不允许"，CRD 化便于 K8s GitOps。

mxsec Engine 同时支持三种源，**让客户选择信任路径**（默认全开，可关闭单一来源）。

### 1.3 vs 青藤万象 / 蜂巢

- **青藤万象**自维护"雷火引擎 200+ 规则"（闭源、不可审计、无法二次贡献，见 `ref/appendix/_raw/qingteng-ppt.txt` slide24+slide45）；
- **蜂巢**容器规则同样自闭，客户无法定制规则、无法复用业界资产；
- **mxsec 路线**：开放规则源 + 透明转换 + 客户可贡献，本质上把"规则市场"做成开源协作产物。

---

## 2. 整体架构：规则中台

```
                       社区规则源 (上游)
   ┌──────────────────────────────────────────────────────────┐
   │  github.com/falcosecurity/rules       (Falco YAML)       │
   │  github.com/SigmaHQ/sigma             (Sigma YAML)       │
   │  github.com/cilium/tetragon           (Tetragon CRD)     │
   └─────────────────────┬─────────────────────────────────────┘
                         │ Cron 1d  +  Webhook 触发
                         ▼
   ┌──────────────────────────────────────────────────────────┐
   │  RuleSync 子模块（VulnSync 内寄生 / 也可独立进程）         │
   │  • git clone / pull --depth=1                            │
   │  • 校验签名（Falco rules 的 cosign / SigmaHQ 的 commit）  │
   │  • 调 RuleConverter 转 CEL                               │
   │  • 写入 RuleStore (MySQL + 对象存储)                     │
   └─────────────────────┬─────────────────────────────────────┘
                         │
                         ▼
   ┌──────────────────────────────────────────────────────────┐
   │  RuleStore (MySQL `engine_rules` + Redis `ConfigStream`)  │
   │  • 全局规则（builtin）                                    │
   │  • 租户规则（tenant_id 隔离）                             │
   │  • 用户私有规则（用户在 UI 写的 CEL）                     │
   │  • 版本号 + 灰度通道（stable / canary）                  │
   └─────────────────────┬─────────────────────────────────────┘
                         │ Pub/Sub: `engine.rules.changed`
                         ▼
   ┌──────────────────────────────────────────────────────────┐
   │  Engine 微服务 × N 副本                                   │
   │  • 启动加载全量规则                                       │
   │  • 订阅 Redis ConfigStream 热重载                         │
   │  • CEL 程序池（lru，每租户独立）                          │
   │  • 对 Kafka `mxsec.agent.*` 消息逐条评估                 │
   │  • 命中产 Alert → Kafka `mxsec.engine.alert`             │
   └──────────────────────────────────────────────────────────┘
```

**核心设计原则**：Engine 内部**只认 CEL**，外部三种源全部在 RuleConverter 转换成 CEL，避免 Engine 维护三套解析器、三套语义。

---

## 3. 规则模型：统一中间表达式（CEL）

mxsec 选择 **CEL（Common Expression Language）** 作为统一规则中间表达式，理由：

| 维度 | CEL 优势 |
|------|---------|
| 语言定义 | Google 维护，proto3 风格，**生产级语言规范**（不是民间发明语言） |
| 性能 | 编译期类型检查 + 字节码执行，**5-10 倍快于** Lua / JS / Starlark |
| 隔离 | 沙箱安全，**禁止系统调用 / 文件 IO / 任意循环**（图灵不完备） |
| 生态 | K8s、Envoy、gRPC、Istio 都用 CEL 做策略表达 |
| Go 集成 | `cel-go` 官方库稳定，编译缓存友好（≤ 50µs/event @500 规则） |
| 可读 | 接近 SQL WHERE 表达式，安全运营人员能改 |

### 3.1 CEL 规则 schema

```go
// internal/server/engine/rule/model.go
type Rule struct {
    // 基础字段
    ID          string   `json:"id"`           // 全平台唯一 ID
    Source      Source   `json:"source"`       // falco / sigma / tetragon / mxsec / user
    UpstreamID  string   `json:"upstream_id"`  // 上游原 ID（便于追溯）
    Version     string   `json:"version"`      // 规则版本号 (semver)
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Severity    Severity `json:"severity"`     // info / low / medium / high / critical

    // 数据源筛选
    DataTypes   []int32  `json:"data_types"`   // 3000-3010 / 5050-5060 / 6001-6002 / 11001+
    EventTypes  []string `json:"event_types"`  // process_exec / tcp_connect / k8s_audit

    // 核心表达式
    Expression  string   `json:"expression"`   // CEL 字符串

    // 告警字段（命中时填充）
    Tags        []string `json:"tags"`         // ATT&CK / D3FEND / CWE / OWASP
    Mitre       MitreRef `json:"mitre"`        // tactics + techniques
    D3fend      []string `json:"d3fend"`       // D3FEND 防御策略
    References  []string `json:"references"`   // URL 引用

    // 运行控制
    Enabled     bool     `json:"enabled"`
    Mode        Mode     `json:"mode"`         // observe / protect（与 operating-modes.md 对齐）
    Action      Action   `json:"action"`       // alert / kill / quarantine / ip_block
    Throttle    Throttle `json:"throttle"`     // 限流（避免告警风暴）

    // 租户作用域
    TenantID    string   `json:"tenant_id"`    // 空字符串 = 全局 builtin
    Scope       Scope    `json:"scope"`        // global / tenant / host_label

    // 灰度
    Channel     Channel  `json:"channel"`      // stable / canary
    CanaryPct   int      `json:"canary_pct"`   // 0-100

    // 元数据
    Hash        string   `json:"hash"`         // SHA256(Expression+Action+Mode)
    Signature   string   `json:"signature"`    // Ed25519 签名
    SyncedAt    int64    `json:"synced_at"`    // 上游同步时间戳
}

type MitreRef struct {
    Tactics    []string `json:"tactics"`    // ["TA0002","TA0003"]
    Techniques []string `json:"techniques"` // ["T1059.004","T1546.003"]
}

type Throttle struct {
    MaxPerHost     int `json:"max_per_host"`     // 单主机每分钟最大告警数
    MaxPerTenant   int `json:"max_per_tenant"`
    SilenceSeconds int `json:"silence_seconds"`  // 命中后静默期
}
```

### 3.2 Engine 端 CEL 求值环境

CEL 求值环境暴露的变量与函数集合（**这是 RuleConverter 的目标 schema**，转换器必须把上游字段映射到这套变量名）：

```go
// internal/server/engine/rule/env.go
func NewCelEnv() *cel.Env {
    env, _ := cel.NewEnv(
        // 顶层变量：每条 Kafka 消息抽象成 event
        cel.Variable("event",   cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("process", cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("file",    cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("network", cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("dns",     cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("k8s",     cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("container", cel.MapType(cel.StringType, cel.DynType)),
        cel.Variable("host",    cel.MapType(cel.StringType, cel.DynType)),

        // 辅助函数
        cel.Function("matches_glob",
            cel.Overload("matches_glob_string",
                []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
                cel.BinaryBinding(globMatch))),
        cel.Function("ip_in_cidr",
            cel.Overload("ip_in_cidr_string",
                []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
                cel.BinaryBinding(cidrMatch))),
        cel.Function("startswith",
            cel.Overload("startswith_string",
                []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
                cel.BinaryBinding(startsWith))),
        cel.Function("endswith", /* 同上 */),
        cel.Function("contains_any",
            cel.Overload("contains_any_strings",
                []*cel.Type{cel.StringType, cel.ListType(cel.StringType)}, cel.BoolType,
                cel.BinaryBinding(containsAny))),
        cel.Function("ioc_hit",
            cel.Overload("ioc_hit_string",
                []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
                cel.BinaryBinding(iocHit))),
    )
    return env
}
```

**通用字段**（每个 event 都有）：

| 路径 | 类型 | 来源 |
|------|------|------|
| `event.type` | string | `process_exec` / `tcp_connect` / `file_open` / `k8s_audit` 等 |
| `event.timestamp` | int64 | 毫秒时间戳 |
| `event.tenant_id` | string | 多租户 ID |
| `host.id` | string | 主机 ID |
| `host.os` | string | `linux` / `k8s-node` |
| `host.labels` | map<string,string> | 主机标签（用于规则按 label 启用） |
| `container.id` / `container.image` / `container.runtime` | string | 容器富化字段（仅容器场景） |
| `k8s.namespace` / `k8s.pod` / `k8s.node` | string | K8s 富化字段 |

**专属字段（按 event.type 不同存在）**：

```
process_exec:
  process.pid / process.ppid / process.exe / process.cmdline / process.args[]
  process.user / process.uid / process.gid / process.tty / process.cwd
  process.parent.exe / process.parent.cmdline / process.ancestors[]

file_open:
  file.path / file.flags / file.mode / file.fd

tcp_connect:
  network.proto / network.saddr / network.sport / network.daddr / network.dport
  network.direction (inbound / outbound) / network.bytes

dns_query:
  dns.qname / dns.qtype / dns.answers[]

k8s_audit:
  k8s.verb / k8s.resource / k8s.username / k8s.request_object / k8s.response_status
```

### 3.3 CEL 表达式示例（mxsec 原生）

```cel
event.type == "process_exec" &&
process.exe == "/bin/sh" &&
process.cmdline.contains(" -i ") &&
network.daddr != "" &&
!ip_in_cidr(network.daddr, "10.0.0.0/8")
```

含义：交互式 shell 启动 + 出向连接非内网 = 反弹 shell。

---

## 4. RuleConverter：三套上游 → CEL 转换器

```go
// internal/server/engine/rule/converter/converter.go
package converter

import (
    "context"
    "github.com/imkerbos/mxsec-platform/internal/server/engine/rule"
)

// RuleConverter 把上游规则源转换为统一 mxsec Rule
type RuleConverter interface {
    // Source 返回所属来源（falco / sigma / tetragon）
    Source() rule.Source
    // Parse 解析单个上游规则文件（可能含多条规则）
    Parse(ctx context.Context, raw []byte, filename string) ([]rule.Rule, error)
    // Capabilities 返回该 converter 支持的字段映射 + 不支持项（用于兼容性矩阵）
    Capabilities() Capability
}

type Capability struct {
    SupportedFields    []string  // 可直接映射的上游字段
    UnsupportedFields  []string  // 不支持的字段（fallback：忽略 + warn）
    PartialFields      []string  // 部分支持（精度损失）
    SemanticGap        []string  // 语义层面的差异说明
}

// Registry 注册三个内置 converter
type Registry struct {
    converters map[rule.Source]RuleConverter
}

func (r *Registry) Register(c RuleConverter) {
    r.converters[c.Source()] = c
}

func (r *Registry) Get(s rule.Source) (RuleConverter, bool) {
    c, ok := r.converters[s]
    return c, ok
}

func DefaultRegistry() *Registry {
    r := &Registry{converters: map[rule.Source]RuleConverter{}}
    r.Register(NewFalcoConverter())
    r.Register(NewSigmaConverter())
    r.Register(NewTetragonConverter())
    return r
}
```

下面分别给出三套 converter 的设计与转换示例。

---

## 5. Falco Rules → CEL

### 5.1 Falco 规则结构

Falco 规则（YAML）有 4 类对象：

- `rule`：实际检测规则
- `macro`：可复用条件片段（如 `spawned_process`）
- `list`：值列表（如 `shell_binaries`）
- `required_engine_version`：要求引擎最低版本

示例（来自 `falcosecurity/rules` 仓库）：

```yaml
- list: shell_binaries
  items: [sh, bash, ksh, zsh, csh, tcsh, dash]

- macro: spawned_process
  condition: evt.type=execve and evt.dir=<

- rule: Run shell untrusted
  desc: An attempt to spawn a shell below a non-shell application.
  condition: >
    spawned_process and
    proc.name in (shell_binaries) and
    proc.pname exists and
    not proc.pname in (shell_binaries) and
    not container
  output: >
    Shell spawned in container with non-shell parent
    (user=%user.name container_id=%container.id parent=%proc.pname cmdline=%proc.cmdline)
  priority: NOTICE
  tags: [process, mitre_execution]
```

### 5.2 转换映射表

| Falco 字段 | mxsec CEL | 备注 |
|-----------|-----------|------|
| `evt.type=execve` | `event.type == "process_exec"` | 一一对应 |
| `evt.type=open` | `event.type == "file_open"` | 一一对应 |
| `evt.type=connect` | `event.type == "tcp_connect"` | 一一对应 |
| `proc.name` | `process.exe`（取 basename） | mxsec 用 exe 全路径，需 `process.exe.split("/").last()` |
| `proc.exepath` | `process.exe` | 直接映射 |
| `proc.cmdline` | `process.cmdline` | 直接映射 |
| `proc.pname` | `process.parent.exe.split("/").last()` | 转 basename |
| `proc.aname[X]` | `process.ancestors[X-1].exe` | Falco 1-indexed → CEL 0-indexed |
| `user.name` | `process.user` | 直接映射 |
| `container.id` | `container.id` | 直接映射 |
| `container.image.repository` | `container.image.split(":")[0]` | mxsec 不拆分，需 split |
| `fd.name` | `file.path`（file_open 场景）/ `network.daddr`（connect 场景） | 上下文相关 |
| `fd.sip` / `fd.cip` | `network.saddr` / `network.daddr` | 一一对应 |
| `fd.sport` / `fd.cport` | `network.sport` / `network.dport` | 一一对应 |
| `not container` | `container.id == ""` | mxsec 用空字符串表示宿主机 |
| `in (...)` | `[...].exists(x, x == ...)` 或 `contains_any(...)` | Falco 原生 in → CEL 用列表函数 |
| `pmatch (...)` | `matches_glob(field, pattern)` | 自定义 CEL 函数 |
| `startswith` | `startswith(field, prefix)` | 自定义 CEL 函数 |
| `contains` | `field.contains(...)` | CEL 原生 |

### 5.3 转换示例

上面那条 `Run shell untrusted` Falco 规则转成 mxsec Rule：

```yaml
id: falco-run-shell-untrusted
source: falco
upstream_id: "Run shell untrusted"
version: "1.0.0"
name: "Run shell untrusted (Falco)"
description: "An attempt to spawn a shell below a non-shell application."
severity: low
data_types: [3000]
event_types: [process_exec]
expression: |
  event.type == "process_exec" &&
  contains_any(process.exe.split("/").last(),
               ["sh","bash","ksh","zsh","csh","tcsh","dash"]) &&
  process.parent.exe != "" &&
  !contains_any(process.parent.exe.split("/").last(),
                ["sh","bash","ksh","zsh","csh","tcsh","dash"]) &&
  container.id == ""
tags: [process, mitre_execution]
mitre:
  tactics: ["TA0002"]   # Execution
  techniques: ["T1059.004"]  # Command and Scripting Interpreter: Unix Shell
mode: observe
action: alert
throttle:
  max_per_host: 30
  silence_seconds: 60
```

### 5.4 Falco macro / list 处理

mxsec converter 内部维护**全局符号表**：

```go
type FalcoParser struct {
    lists  map[string][]string   // list 名 → 字符串切片
    macros map[string]string     // macro 名 → CEL 片段
}

func (p *FalcoParser) Parse(ctx context.Context, raw []byte, filename string) ([]rule.Rule, error) {
    // Phase 1: 先收所有 list / macro
    docs, err := yamlSplit(raw)
    for _, d := range docs {
        if d.List != "" {
            p.lists[d.List] = d.Items
        }
        if d.Macro != "" {
            p.macros[d.Macro] = p.translateExpr(d.Condition)
        }
    }
    // Phase 2: 翻译 rule
    var out []rule.Rule
    for _, d := range docs {
        if d.Rule == "" { continue }
        cel := p.translateExpr(d.Condition)
        // macro / list 名展开
        cel = p.expandMacros(cel)
        out = append(out, p.toMxsecRule(d, cel))
    }
    return out, nil
}
```

### 5.5 兼容性矩阵（Falco）

| Falco 能力 | mxsec 支持 | 备注 |
|-----------|----------|------|
| `evt.type=execve / open / connect / accept / setuid / mount` | ✅ 直接映射 | mxsec EDR 已采 |
| `proc.* / user.* / container.* / fd.* / k8s.*` | ✅ 完整映射 | 见 §5.2 |
| `evt.arg.path / evt.arg.cmd` 等 syscall 原始 arg | ⚠️ 部分 | mxsec 富化字段已封装，原生 arg 不直接暴露 |
| `proc.aname[N]` 祖先链 | ✅ Engine 维护 ancestors 数组 | 由 Storyline 注入 |
| `proc.pname` | ✅ basename 转换 | converter 自动 split |
| `fd.cip` / `fd.sip` 等 fd 字段 | ✅ 网络上下文映射 | `network.saddr` / `daddr` |
| `evt.dir=<` 系统调用方向 | ⚠️ 忽略（mxsec 只采返回后的事件） | 不影响语义 |
| `evt.is_open_write` 等组合谓词 | ⚠️ 翻译为 `file.flags & O_WRONLY != 0` | 需 flags 富化 |
| `falco.engine_version` 检查 | ⚠️ 忽略 + warn | mxsec 自有版本号 |
| `append` 字段（追加既有规则） | ✅ 同名规则合并 condition with `||` |
| `exception` / `exceptions` 字段（白名单） | ✅ 转 CEL `&& !(...)` | Falco 0.32+ 特性 |
| `output` 字段 | ✅ 转 mxsec alert template | mustache 风格 |

**不直接映射的字段**（converter 给 warn，规则降级为 disabled + UI 提示）：

- `evt.rawres` / `evt.failed`（系统调用返回值细节）
- `evt.buffer`（syscall arg 缓冲区内容）
- `evt.around` 时间窗口语法

---

## 6. Sigma Rules → CEL

### 6.1 Sigma 规则结构

Sigma 是日志检测的通用 schema，规则 YAML 由 `logsource` + `detection` 组成：

```yaml
title: Suspicious Curl Download
id: 8e3b76d8-9e0e-4ef4-8d11-26b7b97c1e2d
status: stable
description: Detects curl downloading executable files
references:
  - https://attack.mitre.org/techniques/T1105/
author: SigmaHQ
date: 2021/04/12
tags:
  - attack.command_and_control
  - attack.t1105
logsource:
  product: linux
  category: process_creation
detection:
  selection:
    Image|endswith: '/curl'
    CommandLine|contains:
      - 'http://'
      - 'https://'
    CommandLine|contains|all:
      - '-o'
  filter:
    CommandLine|contains: '.html'
  condition: selection and not filter
level: medium
```

### 6.2 Sigma → mxsec 字段映射

Sigma 是抽象 schema，**先做语义投影**：`process_creation` → mxsec `process_exec` 等。

| Sigma logsource | mxsec event.type | DataType |
|----------------|------------------|----------|
| `linux/process_creation` | `process_exec` | 3000 |
| `linux/file_event` | `file_open / file_write / file_rename` | 3001 |
| `linux/network_connection` | `tcp_connect` | 3002 |
| `linux/dns_query` | `dns_query` | 3003 |
| `linux/auditd` | 多 event.type（按 type 细分） | 多 |
| `kubernetes/audit` | `k8s_audit` | 11050+ |
| `webserver` | `http_log` | 业务事件（v2） |

| Sigma 字段 | mxsec CEL |
|-----------|-----------|
| `Image` | `process.exe` |
| `CommandLine` | `process.cmdline` |
| `ParentImage` | `process.parent.exe` |
| `User` | `process.user` |
| `DestinationIp` | `network.daddr` |
| `DestinationPort` | `network.dport` |
| `SourceIp` | `network.saddr` |
| `TargetFilename` | `file.path` |
| `QueryName` | `dns.qname` |

### 6.3 Sigma 操作符 → CEL

| Sigma 修饰符 | CEL 等价 |
|------------|---------|
| `field` 直接 | `field == value` |
| `field|contains` | `field.contains(value)` |
| `field|startswith` | `startswith(field, value)` |
| `field|endswith` | `endswith(field, value)` |
| `field|re` | `field.matches(regex)` |
| `field|cidr` | `ip_in_cidr(field, cidr)` |
| `field|contains|all` | `[v1,v2].all(x, field.contains(x))` |
| 值列表（无修饰符） | `[v1,v2,v3].exists(x, field == x)` |
| `null` | `field == ""` |
| `condition: A and B` | `(A) && (B)` |
| `condition: A or B` | `(A) \|\| (B)` |
| `condition: not A` | `!(A)` |
| `condition: 1 of selection*` | OR 折叠 |
| `condition: all of selection*` | AND 折叠 |

### 6.4 转换示例

上面 `Suspicious Curl Download` 转 mxsec Rule：

```yaml
id: sigma-suspicious-curl-download
source: sigma
upstream_id: "8e3b76d8-9e0e-4ef4-8d11-26b7b97c1e2d"
version: "1.0.0"
name: "Suspicious Curl Download (Sigma)"
description: "Detects curl downloading executable files"
severity: medium
data_types: [3000]
event_types: [process_exec]
expression: |
  event.type == "process_exec" &&
  endswith(process.exe, "/curl") &&
  ["http://","https://"].exists(x, process.cmdline.contains(x)) &&
  process.cmdline.contains("-o") &&
  !(process.cmdline.contains(".html"))
tags: ["attack.command_and_control","attack.t1105"]
mitre:
  tactics: ["TA0011"]   # Command and Control
  techniques: ["T1105"] # Ingress Tool Transfer
references:
  - "https://attack.mitre.org/techniques/T1105/"
mode: observe
action: alert
throttle:
  max_per_host: 20
  silence_seconds: 120
```

### 6.5 Sigma 兼容性矩阵

| Sigma 能力 | mxsec 支持 | 备注 |
|-----------|----------|------|
| `linux/process_creation` | ✅ | DataType 3000 直接覆盖 |
| `linux/file_event` | ✅ | DataType 3001 + FIM 6001/6002 |
| `linux/network_connection` | ✅ | DataType 3002 |
| `linux/dns_query` | ✅ | DataType 3003 |
| `linux/auditd` | ⚠️ 部分 | mxsec EDR 已覆盖大部分关键 auditd 字段，少数键需 fallback |
| `kubernetes/audit` | ✅ | K8s Audit 事件 11050+ |
| `webserver` | ❌ Phase 1 不做 | mxsec v1 不覆盖 Web 日志（v2 接入 nginx/apache 日志） |
| `windows/*` | ❌ 永不支持 | mxsec 专精 Linux + K8s，**不做 Windows** |
| `macos/*` | ❌ 永不支持 | 同上 |
| `cloud/aws / azure / gcp` | ⚠️ Phase 3 | CSPM 模块独立接入，Engine 不直接消费云审计日志 |
| 操作符 `contains / startswith / endswith / re / cidr` | ✅ 全支持 | 见 §6.3 |
| `condition: 1 of / all of` | ✅ 编译期折叠 |
| `fields` / `falsepositives` / `references` | ✅ 元数据保留 |
| `timeframe` 时间窗口聚合 | ⚠️ 转 mxsec SequenceDetector | 不是单事件 CEL，需走序列层 |

**Windows / macOS / 通用 SIEM 部分（占 Sigma 60%+）默认不导入**，由 Sigma Converter 启动时根据 `logsource.product` 过滤，跳过的规则数量在 RuleSync 任务报表中明示。

---

## 7. Tetragon Tracing Policies → CEL

### 7.1 Tetragon Policy 结构

Tetragon 用 K8s CRD 表达 eBPF 行为策略（`TracingPolicy` / `TracingPolicyNamespaced`）：

```yaml
apiVersion: cilium.io/v1alpha1
kind: TracingPolicy
metadata:
  name: "detect-bash-exec"
spec:
  kprobes:
    - call: "sys_execve"
      syscall: true
      args:
        - index: 0
          type: "string"
        - index: 1
          type: "string"
      selectors:
        - matchArgs:
            - index: 0
              operator: "Postfix"
              values:
                - "/bin/bash"
                - "/usr/bin/bash"
          matchActions:
            - action: Post
```

### 7.2 Tetragon → CEL 映射

Tetragon 表达的是"哪个 kprobe / tracepoint 触发 + 哪些参数符合 / 触发什么动作"，mxsec 把它**降维**到 event 层：

| Tetragon 字段 | mxsec CEL |
|--------------|-----------|
| `kprobes.call: sys_execve` | `event.type == "process_exec"` |
| `kprobes.call: sys_openat` | `event.type == "file_open"` |
| `kprobes.call: tcp_connect` | `event.type == "tcp_connect"` |
| `tracepoints.subsystem: syscalls, event: sys_enter_execve` | `event.type == "process_exec"` |
| `args[i].type=string + matchArgs.operator=Equal` | 取对应字段（按 syscall 语义）`== value` |
| `matchArgs.operator: Postfix` | `endswith(field, value)` |
| `matchArgs.operator: Prefix` | `startswith(field, value)` |
| `matchArgs.operator: Mask` | 按位 `&` |
| `matchPIDs` | `process.pid in [...]` |
| `matchNamespaces` | `k8s.namespace in [...]` |
| `matchBinaries` | `process.exe in [...]` |
| `matchActions.action: Post` | mxsec `action: alert` |
| `matchActions.action: Sigkill` | mxsec `action: kill`（仅 `mode: protect` 生效） |
| `matchActions.action: Override` | ❌ Phase 1 不支持（kernel-level override） |
| `matchActions.action: FollowFD` | ⚠️ 部分（mxsec 不跟 fd，文件路径已富化） |

### 7.3 转换示例

上面 `detect-bash-exec` 转 mxsec Rule：

```yaml
id: tetragon-detect-bash-exec
source: tetragon
upstream_id: "detect-bash-exec"
version: "1.0.0"
name: "Detect bash exec (Tetragon)"
description: "execve(bash) 触发, eBPF kprobe sys_execve"
severity: info
data_types: [3000]
event_types: [process_exec]
expression: |
  event.type == "process_exec" &&
  (endswith(process.exe, "/bin/bash") ||
   endswith(process.exe, "/usr/bin/bash"))
tags: ["execution","kprobe.sys_execve"]
mitre:
  tactics: ["TA0002"]
  techniques: ["T1059.004"]
mode: observe
action: alert
throttle:
  max_per_host: 100
  silence_seconds: 30
```

### 7.4 Tetragon 兼容性矩阵

| Tetragon 能力 | mxsec 支持 | 备注 |
|--------------|---------|------|
| `kprobes` 系统调用 hook | ✅ 通过 EDR eBPF 等价覆盖 | 见 [`edr-agent-design.md`](edr-agent-design.md) |
| `tracepoints` syscall enter/exit | ✅ EDR 同上 |  |
| `uprobes` 用户态符号 hook | ⚠️ Phase 4 RASP 实现 | mxsec v1 不做用户态 uprobe |
| `lsm` LSM hook | ⚠️ Phase 4 | 等 mxsec 启用 LSM-BPF |
| `pod selectors` (namespace / labels / containers) | ✅ K8s 富化已注入 | Engine 加载时按租户 + namespace 过滤 |
| `matchActions: Sigkill / Override` | ✅ Sigkill（`mode: protect` 时）/ ❌ Override | Override 需 LSM-BPF |
| `matchActions: NotifyKiller / FollowFD` | ❌ 暂不实现 | 等价语义可由 mxsec Playbook 覆盖 |
| `matchBinaries / matchPIDs / matchNamespaces` | ✅ CEL 全覆盖 |  |
| 内核版本要求 | mxsec 已声明 ≥ 5.4 与 Tetragon 兼容 | CO-RE 自适应 |
| `enforce` 模式 | ✅ `action: kill` + `mode: protect` 等价 | 不直接生效内核态 enforce |

---

## 8. MITRE ATT&CK / D3FEND 标签映射

三套规则源的标签风格不同，**RuleConverter 必须规整为统一 ATT&CK / D3FEND 标识**。

### 8.1 输入到 ATT&CK ID 的映射

| 来源 | 标签形态 | 转换规则 |
|------|---------|---------|
| Falco | `tags: [mitre_execution, T1059]` 等 | 提取 `T\d{4}(\.\d{3})?` 形 → `techniques`；提取 `mitre_<tactic>` → 反查 `tactics` |
| Sigma | `tags: [attack.execution, attack.t1059.004]` | `attack.<tactic>` → `tactics`；`attack.t<X>` → `techniques`（大写） |
| Tetragon | 一般无 ATT&CK 标签 | 走 mxsec 内部 **kprobe → ATT&CK 字典表**（见 §8.2） |

### 8.2 Tetragon kprobe → ATT&CK 字典（节选）

```yaml
sys_execve:        [TA0002/T1059]
sys_setuid:        [TA0004/T1548.001]
sys_ptrace:        [TA0005/T1055.008]
sys_init_module:   [TA0003/T1547.006, TA0005/T1014]
sys_mount:         [TA0005/T1564.006]
tcp_connect:       [TA0011/T1071]
sys_unlink/unlinkat:[TA0040/T1070.004]
sys_memfd_create:  [TA0005/T1620]
```

### 8.3 D3FEND 映射

D3FEND 是 ATT&CK 的"防御对位"，mxsec 在 Engine 端**自动补全**：

```go
// internal/server/engine/rule/d3fend.go
var techToD3fend = map[string][]string{
    "T1059":     {"D3-PA"},    // Process Analysis
    "T1059.004": {"D3-PA","D3-PSA"}, // Process Spawn Analysis
    "T1110":     {"D3-AL"},    // Authentication Logging
    "T1071":     {"D3-NTA"},   // Network Traffic Analysis
    "T1547.006": {"D3-KIVA"},  // Kernel Integrity Validation
}
```

D3FEND 字段供 SOC 控制台呈现"为什么 mxsec 能查到这种攻击"，是售前差异化卖点。

### 8.4 覆盖度报表

RuleSync 同步任务每次执行后产 `coverage_report.json`：

```json
{
  "synced_at": "2026-06-05T10:00:00Z",
  "total_rules": 1247,
  "by_source": {"falco": 102, "sigma": 1078, "tetragon": 67},
  "attack_coverage": {
    "tactics_total": 14,
    "tactics_covered": 14,
    "techniques_total": 201,
    "techniques_covered": 156,
    "coverage_pct": 77.6
  },
  "by_tactic": {
    "TA0001-Initial-Access": 18,
    "TA0002-Execution": 215,
    "TA0003-Persistence": 142,
    "TA0004-Privilege-Escalation": 89,
    "TA0005-Defense-Evasion": 198,
    "TA0006-Credential-Access": 76,
    "TA0007-Discovery": 54,
    "TA0008-Lateral-Movement": 41,
    "TA0009-Collection": 38,
    "TA0010-Exfiltration": 27,
    "TA0011-Command-and-Control": 119,
    "TA0040-Impact": 65
  },
  "d3fend_coverage_pct": 62.4
}
```

报表写入 ClickHouse `mxsec_rules_coverage_daily`，UI 展示战术覆盖热图。

---

## 9. 规则市场（RuleMarket）

### 9.1 设计目标

- 开源用户与商业客户**共用**一个规则市场（社区版与企业版共享）
- 客户私有规则**租户隔离**，不入公共池
- 用户贡献规则走 PR 流程（先入 staging → 审核 → 发 stable channel）

### 9.2 规则来源分类

| Channel | 来源 | 默认开启 | 签名要求 |
|--------|------|---------|---------|
| `community.falco` | github.com/falcosecurity/rules | ✅ | Falco cosign 签名 |
| `community.sigma` | github.com/SigmaHQ/sigma | ✅ | Git commit GPG（可选） |
| `community.tetragon` | github.com/cilium/tetragon | ✅ | Apache 项目签名 |
| `mxsec.curated` | 由 mxsec 团队精选与本地化（中文化、信创适配） | ✅ | mxsec Ed25519 签名 |
| `mxsec.contrib` | 用户提交（Web UI / GitHub PR） | ❌ 默认关闭 | 需人工审核 + 重新签名 |
| `tenant.private` | 客户私有规则 | ✅ 仅该租户 | 租户内私钥签名（KA） |

### 9.3 数据模型

```sql
CREATE TABLE engine_rules (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    rule_uid          VARCHAR(128) NOT NULL,    -- 业务唯一 ID
    source            VARCHAR(32)  NOT NULL,    -- falco / sigma / tetragon / mxsec / user
    upstream_id       VARCHAR(255),
    version           VARCHAR(32)  NOT NULL,
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    severity          ENUM('info','low','medium','high','critical') NOT NULL,
    data_types        JSON NOT NULL,
    event_types       JSON NOT NULL,
    expression        TEXT NOT NULL,
    tags              JSON,
    mitre_tactics     JSON,
    mitre_techniques  JSON,
    d3fend            JSON,
    references_json   JSON,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    mode              ENUM('observe','protect') NOT NULL DEFAULT 'observe',
    action            VARCHAR(32) NOT NULL DEFAULT 'alert',
    throttle_json     JSON,
    tenant_id         VARCHAR(64),               -- NULL = 全局 builtin
    scope             ENUM('global','tenant','host_label') NOT NULL,
    channel           ENUM('stable','canary','staging') NOT NULL DEFAULT 'stable',
    canary_pct        TINYINT UNSIGNED NOT NULL DEFAULT 0,
    hash              VARCHAR(64) NOT NULL,
    signature         VARCHAR(255) NOT NULL,
    synced_at         BIGINT NOT NULL,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL,

    UNIQUE KEY uk_rule_uid_tenant (rule_uid, tenant_id),
    INDEX idx_tenant_enabled (tenant_id, enabled),
    INDEX idx_source_channel (source, channel),
    INDEX idx_synced_at (synced_at)
) ENGINE=InnoDB;
```

### 9.4 客户私有规则贡献回流

UI 提供 "提交到 community" 按钮，规则脱敏（去掉 host_id / username 等内部字段）后走 GitHub PR：

```
租户 t-bank-a 安全运营写规则
   │
   ▼
UI → 提交 to mxsec.contrib
   │
   ▼
脱敏（mxsec-bot 自动检查）
   │
   ▼
GitHub PR @ mxsec-community/rules
   │
   ▼
2 maintainer review + 红队验证
   │
   ▼
merge → 下一次 RuleSync 进入 community 池
```

这套机制让客户私有积累的"实战规则"反哺社区，是工业级开源 CWPP 的护城河。

---

## 10. 规则版本控制 + 灰度发布

### 10.1 版本号

每条规则带 semver `version`，规则升级路径：

```
1.0.0 → 1.0.1 (打字 / 描述修正,语义不变)
1.0.1 → 1.1.0 (条件加严, 仍是 superset)
1.1.0 → 2.0.0 (条件改变, 可能误报飙升或漏报)
```

Engine 加载时按 `(rule_uid, channel, MAX(version))` 取最高版本，灰度通道独立。

### 10.2 灰度通道

| Channel | 角色 | 流量 |
|--------|------|------|
| `staging` | 内部测试 / 红蓝对抗演练租户 | mxsec 内部租户专享 |
| `canary` | 主动报名客户（5% 主机） | 客户在 UI 勾选「我愿意试新规则」 |
| `stable` | 默认 | 100% 客户 |

灰度规则**与 `operating-modes.md` 的灰度准入**对齐：

- 新规则上线先入 `staging` 7 天
- 进入 `canary` 30 天（≥ 10 个志愿租户，FP rate ≤ 2%）
- 进入 `stable`

### 10.3 灰度失败回滚

Engine 收到 `engine.rules.changed` 事件后，对每条规则计算 7 天滑动窗口的：

- `precision`（用户标记真威胁 / 总告警）
- `fp_rate`（用户标记误报 / 总告警）

任一指标恶化超阈值（`precision < 80%` 或 `fp_rate > 5%`）：

1. Engine 自动把该规则切回 `disabled`
2. 写 `engine.rules.alert` Topic，通知 mxsec 维护团队
3. UI 在规则市场页面置灰，标注"灰度失败，已自动回滚"

---

## 11. 规则签名（防篡改）

### 11.1 签名链

```
社区源（GitHub）
   │ git tag + cosign（Falco / Tetragon）
   │ commit GPG（Sigma 部分规则）
   ▼
RuleSync 校验上游签名
   │
   ▼
RuleConverter 转 CEL
   │
   ▼
mxsec Ed25519 重签
   │
   ▼
RuleStore (signature 字段)
   │
   ▼
Engine 加载时校验 signature
   │ 校验失败 → 拒绝加载 + 告警
   ▼
执行 CEL
```

### 11.2 Ed25519 签名实现

复用 `internal/common/signing/`（已存在的 mxsec 平台级签名工具）：

```go
// internal/server/engine/rule/sign.go
package rule

import (
    "crypto/ed25519"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
)

// SignRule 用 mxsec 平台私钥签名规则
func SignRule(r *Rule, priv ed25519.PrivateKey) error {
    payload := canonicalForm(r)
    digest := sha256.Sum256(payload)
    r.Hash = hex.EncodeToString(digest[:])
    sig := ed25519.Sign(priv, digest[:])
    r.Signature = hex.EncodeToString(sig)
    return nil
}

// VerifyRule Engine 加载时校验
func VerifyRule(r *Rule, pub ed25519.PublicKey) error {
    payload := canonicalForm(r)
    digest := sha256.Sum256(payload)
    if hex.EncodeToString(digest[:]) != r.Hash {
        return fmt.Errorf("rule hash mismatch: id=%s", r.ID)
    }
    sig, err := hex.DecodeString(r.Signature)
    if err != nil {
        return fmt.Errorf("signature decode: %w", err)
    }
    if !ed25519.Verify(pub, digest[:], sig) {
        return fmt.Errorf("rule signature invalid: id=%s", r.ID)
    }
    return nil
}

// canonicalForm 规则规整成可重现的字节序列（避免 JSON 顺序差异）
func canonicalForm(r *Rule) []byte {
    // 字段按字典序拼接：id|source|version|expression|action|mode|...
    // 实现见 internal/common/signing/canonical.go
    return canonical([]string{
        r.ID, string(r.Source), r.Version,
        r.Expression, string(r.Action), string(r.Mode),
    })
}
```

KA 客户可上传自有公钥与私有规则签名链。

---

## 12. 规则热重载（ConfigStream）

### 12.1 设计

Engine 副本 N 个，每个副本：

1. 启动从 MySQL 全量加载该租户的规则（按 `enabled=true` + `channel` 过滤）
2. 编译每条规则为 CEL Program，缓存到内存
3. 订阅 Redis `engine.rules.changed:{tenant_id}` 频道
4. 收到变更后增量重载（**不重启进程**）

### 12.2 ConfigStream 消息格式

```json
{
  "tenant_id": "t-bank-a",
  "event": "upsert",        // upsert / delete
  "rule_uid": "sigma-suspicious-curl-download",
  "version": "1.0.1",
  "channel": "stable",
  "actor": "system|user-12345",
  "timestamp": 1733400000
}
```

### 12.3 Engine 端处理

```go
// internal/server/engine/rule/registry.go
type Registry struct {
    mu       sync.RWMutex
    rules    map[string]*compiledRule          // rule_uid → 编译后程序
    byEvent  map[string][]*compiledRule        // event.type → 候选规则（O(1) 路由）
    tenantID string
}

type compiledRule struct {
    Rule    *Rule
    Program cel.Program
}

func (r *Registry) OnRuleChange(ctx context.Context, ev RuleChangeEvent) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    switch ev.Event {
    case "upsert":
        rule, err := r.store.Get(ctx, ev.TenantID, ev.RuleUID)
        if err != nil { return err }
        // 校验签名
        if err := VerifyRule(rule, r.pubKey); err != nil {
            return fmt.Errorf("verify failed: %w", err)
        }
        // 编译
        prog, err := r.compile(rule)
        if err != nil { return err }
        // 替换 / 新增
        old := r.rules[rule.RuleUID]
        r.rules[rule.RuleUID] = &compiledRule{Rule: rule, Program: prog}
        r.rebuildEventIndex(old, prog)
    case "delete":
        old := r.rules[ev.RuleUID]
        delete(r.rules, ev.RuleUID)
        r.rebuildEventIndex(old, nil)
    }
    return nil
}
```

### 12.4 热重载 SLO

| 指标 | 目标 |
|------|------|
| 单规则上线/下线生效延迟（P95） | ≤ 3s |
| 全量重载（启动） | ≤ 10s @1500 规则 / 租户 |
| 重载期间不丢事件 | ✅（旧规则池保留 5s 兜底） |
| 重载失败回退 | ✅（保留上一个有效版本，UI 显示错误） |

---

## 13. 与 Engine 微服务的交互

### 13.1 数据流

```
Kafka mxsec.agent.ebpf (DataType 3000-3002)
        │
        ▼
Engine 副本 X
        │
        ├── 1. 反序列化 → Event 结构
        ├── 2. 多租户路由：按 event.tenant_id 取对应 Registry
        ├── 3. event.type 索引：从 byEvent[event.type] 取候选 compiledRule 列表
        ├── 4. 并行评估（候选数 > 20 时启 4 worker）
        │     ├── CEL.Eval(event)
        │     └── 命中 → 进 throttle 过滤
        ├── 5. throttle 通过 → 产 Alert
        ├── 6. 根据 mode + action 决定下发：
        │     ├── observe → 仅写 alert（would_action 字段）
        │     └── protect → 写 alert + 下发处置（IP 封禁 / kill / quarantine）
        └── 7. 写 Kafka mxsec.engine.alert (DataType 11001-11099)
                │
                ▼
        Consumer 持久化 / Manager UI SSE / Notification
```

### 13.2 性能 SLO

| 指标 | 目标 | 备注 |
|------|------|------|
| 单事件 CEL 评估延迟（P95） | ≤ 50µs / 条规则 | cel-go 编译缓存 |
| 1500 规则 / 租户全量评估（P95） | ≤ 5ms | 通过 event.type 索引降 90% 候选 |
| Engine 单副本吞吐 | ≥ 50k EPS（4 vCPU / 8GB） | 实测目标 |
| Engine ConsumerLag P99 | ≤ 30s | 与 `architecture.md` §8.3 SLO 对齐 |
| 热重载丢事件率 | 0 | 旧 Program 5s 兜底窗口 |

### 13.3 Engine 与其它服务的边界

| 关注点 | Engine | 别处 |
|-------|--------|------|
| 规则下发 | ❌ | Manager `POST /api/v2/rules`，写 MySQL + 推 Redis |
| 规则同步社区源 | ❌（订阅 ConfigStream 即可） | RuleSync（VulnSync 寄生 / 独立进程） |
| 规则签名 | ✅ 校验，❌ 签发 | Manager 端 mxsec 平台密钥签 |
| Alert 持久化 | ❌ | Consumer 消费 `mxsec.engine.alert` |
| 用户反馈学习 | ❌ 直接写 | Engine 写 `mxsec.engine.feedback`（用于 ML） |

---

## 14. 持续同步社区规则

### 14.1 同步任务 Cron

| 任务 | 频次 | 触发方 |
|------|------|--------|
| Falco rules 同步 | 每日 02:00 + GitHub Webhook | RuleSync |
| Sigma Rules 同步 | 每日 03:00 + 每周一全量校准 | RuleSync |
| Tetragon Policies 同步 | 每日 04:00 | RuleSync |
| 覆盖度报表生成 | 每日 05:00 | RuleSync |
| mxsec curated 规则同步 | 每周一 10:00 | RuleSync |

### 14.2 同步流程

```go
// internal/server/vulnsync/rulesync/sync.go
package rulesync

type SyncJob struct {
    source     rule.Source
    repoURL    string
    branch     string
    converter  converter.RuleConverter
    store      rule.Store
    signer     rule.Signer
}

func (j *SyncJob) Run(ctx context.Context) (*SyncReport, error) {
    // 1. git pull
    repo, err := j.cloneOrPull(ctx)
    if err != nil { return nil, err }

    // 2. 列出 YAML / CRD 文件
    files, err := j.listFiles(repo)
    if err != nil { return nil, err }

    // 3. 转换
    var allRules []rule.Rule
    var errs []error
    for _, f := range files {
        raw, _ := os.ReadFile(f)
        rules, err := j.converter.Parse(ctx, raw, f)
        if err != nil { errs = append(errs, err); continue }
        allRules = append(allRules, rules...)
    }

    // 4. 签名 + 入库（事务）
    for i := range allRules {
        if err := j.signer.Sign(&allRules[i]); err != nil {
            errs = append(errs, err); continue
        }
    }
    if err := j.store.UpsertBatch(ctx, allRules); err != nil {
        return nil, err
    }

    // 5. 推 ConfigStream（一次性通知所有 Engine 副本重载）
    j.publishConfigStream(ctx, allRules)

    return &SyncReport{
        Source:        j.source,
        TotalParsed:   len(allRules),
        Errors:        len(errs),
        StartedAt:     start,
        FinishedAt:    time.Now(),
    }, nil
}
```

### 14.3 同步失败处理

- 单条规则转换失败：跳过 + 计入 `parse_errors`，不阻塞其它
- 整批失败（GitHub 拉不到）：保留上一次成功版本，告警
- 签名校验失败：拒绝写库，告警维护团队
- 同步报表存 ClickHouse `mxsec_rule_sync_history`，保留 90 天

### 14.4 离网部署

KA 客户离网场景下，提供 `mxsec-rules-offline-{date}.tar.gz` 离线规则包：

```bash
# 在有网环境打包
mxctl rules export --output mxsec-rules-2026-06-05.tar.gz

# 离网客户导入
mxctl rules import mxsec-rules-2026-06-05.tar.gz \
   --verify-signature \
   --target-channel stable
```

离线包含三套源最新 stable channel 规则 + mxsec curated + 覆盖度报表。

---

## 15. Go 接口骨架（汇总）

```go
// internal/server/engine/rule/types.go
package rule

type Source string
const (
    SourceFalco    Source = "falco"
    SourceSigma    Source = "sigma"
    SourceTetragon Source = "tetragon"
    SourceMxsec    Source = "mxsec"
    SourceUser     Source = "user"
)

type Mode string
const (
    ModeObserve Mode = "observe"
    ModeProtect Mode = "protect"
)

type Action string
const (
    ActionAlert      Action = "alert"
    ActionKill       Action = "kill"
    ActionQuarantine Action = "quarantine"
    ActionIPBlock    Action = "ip_block"
    ActionPAMDeny    Action = "pam_deny"
)

type Channel string
const (
    ChannelStable  Channel = "stable"
    ChannelCanary  Channel = "canary"
    ChannelStaging Channel = "staging"
)

// Store 规则存储抽象（MySQL 实现 + 测试用 in-memory 实现）
type Store interface {
    Get(ctx context.Context, tenantID, ruleUID string) (*Rule, error)
    List(ctx context.Context, tenantID string, filter ListFilter) ([]Rule, error)
    UpsertBatch(ctx context.Context, rules []Rule) error
    Delete(ctx context.Context, tenantID, ruleUID string) error
    SubscribeChange(ctx context.Context, tenantID string) (<-chan ChangeEvent, error)
}

// Registry Engine 端运行时注册表
type Registry interface {
    Load(ctx context.Context, tenantID string) error
    Reload(ctx context.Context, ev ChangeEvent) error
    Match(ctx context.Context, event Event) ([]MatchResult, error)
    Stats() RegistryStats
}

// Converter 上游规则源转换器
type Converter interface {
    Source() Source
    Parse(ctx context.Context, raw []byte, filename string) ([]Rule, error)
    Capabilities() Capability
}

// Signer 规则签名 / 校验
type Signer interface {
    Sign(r *Rule) error
    Verify(r *Rule) error
}

// RuleSync 上游同步
type RuleSync interface {
    SyncOnce(ctx context.Context, source Source) (*SyncReport, error)
    Schedule(ctx context.Context) error   // 启动 Cron
}
```

---

## 16. 实施路线（与全局 Roadmap 对齐）

| Sprint | 范围 | 目标 |
|-------|------|------|
| **S0**（设计冻结，1 周） | 本文档评审通过 + CEL env 接口冻结 | 出 review 结论 |
| **S1**（规则模型 + Store，3 周） | engine_rules 表 + Registry 加载器 + 签名 / 校验 + 热重载 | Engine 能跑 mxsec 内置 50 条规则 |
| **S2**（Falco Converter，3 周） | falco YAML parser + macro/list 展开 + 100 条上游规则转换成功 | 兼容性矩阵交付 |
| **S3**（Sigma Converter，4 周） | sigma YAML parser + 操作符全集 + 1000 条 Linux 规则转换 | 跳过 Win/Mac/Cloud 数量明示 |
| **S4**（Tetragon Converter，2 周） | CRD parser + kprobe → ATT&CK 字典 + 50 条上游规则 | 与 EDR eBPF 字段冲突梳理 |
| **S5**（RuleSync + 报表，2 周） | git pull + cron + 覆盖度报表 + 离线包 | RuleMarket UI 上线 |
| **S6**（灰度 + 反馈回路，3 周） | canary / staging / stable 流转 + 自动回滚 + 与 operating-modes.md 对齐 | 与 ML 反馈 Topic 打通 |

总计 **18 周**，与 [`architecture.md`](architecture.md) §11 路径中 Engine 微服务上线节奏一致。

---

## 17. 测试矩阵

| 测试类型 | 范围 | 通过标准 |
|---------|------|---------|
| Converter 单元测试 | Falco / Sigma / Tetragon 各 100+ 上游规则 fixture | 转换无 panic、CEL 编译通过、字段映射符合矩阵 |
| CEL 求值压力 | 1500 规则 × 100k EPS | P95 ≤ 5ms / 事件 |
| 热重载正确性 | upsert / delete 并发 1000 次 | 0 丢事件、0 错配版本 |
| 签名 / 校验 | 篡改 hash / signature / expression 各 1 种 | 全部 Verify 失败 |
| 兼容性矩阵复测 | 上游规则按 source 抽样 5% | 与文档矩阵一致 |
| 多租户隔离 | 租户 A 规则不出现在租户 B Registry | ✅ |
| 灰度回滚 | 注入 FP rate=10% 的规则 | 7d 内自动回 disabled |
| 离网导入 | 离线包导入 + 签名校验 | Engine 加载成功 |

---

## 18. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 上游规则语义有歧义 / converter bug | Converter 转换后**回归 fixture**（每条上游规则有对应输入事件 + 期望命中），CI 强制跑 |
| Sigma 60%+ 是 Windows 规则导致导入率低 | converter 启动按 `logsource.product` 过滤，报表展示**导入率**而不是吹"全量 3000 规则" |
| CEL 求值性能不达 SLO | event.type 索引 + parallelThreshold + 编译缓存；超阈值规则降级为 `staging` 通道 |
| 客户私有规则被脱敏不彻底 | UI 提交时强制 lint 内部字段名（host_id / username / ip）+ 维护团队二审 |
| 上游 license 风险 | Sigma 用 DRL-1.1（permissive）、Falco / Tetragon 用 Apache-2.0，均与 mxsec 商业版兼容；社区贡献规则要求作者 sign DCO |
| 灰度规则误报扩散 | 失败回滚机制 §10.3 + UI 显式 canary 标记 |
| 签名私钥泄露 | mxsec 平台公钥固化在 Engine 镜像里，私钥放 HSM / Vault；KA 客户私钥独立 |
| 上游仓库被供应链攻击 | 校验上游 cosign / GPG + Engine 端 Ed25519 重签 + 灰度 7+30+100 流程止血 |

---

## 19. 参考文档

| 主题 | 文档 |
|------|------|
| 平台总架构 | [`architecture.md`](architecture.md) |
| 运行模式（observe/protect） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| EDR Agent 采集 | [`edr-agent-design.md`](edr-agent-design.md) |
| ML 模型清单 | [`ml-models.md`](ml-models.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 资产模型 | [`asset-model.md`](asset-model.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 运行时差距评估 | `ref/04-运行时.md` |
| 容器 / K8s 差距评估 | `ref/05-容器K8s.md` |
| 服务端架构差距评估 | `ref/01-服务端架构.md` |
| 商业化路线 | `ref/00-总体评估与商业化路线.md` |

外部规则源：

- Falco rules — `https://github.com/falcosecurity/rules`（Apache-2.0）
- Sigma Rules — `https://github.com/SigmaHQ/sigma`（DRL-1.1）
- Tetragon Tracing Policies — `https://github.com/cilium/tetragon`（Apache-2.0）
- CEL 规范 — `https://github.com/google/cel-spec`
- MITRE ATT&CK — `https://attack.mitre.org`
- MITRE D3FEND — `https://d3fend.mitre.org`
