# 漏洞模块设计 v2

> **本文定位**：mxsec **漏洞业务全生命周期** 设计。从 advisory 入口 → 主机指纹采集 → 匹配 → 优先级 → 修复闭环 → 验证 → 报告，串通六微服务的 "看清 → 算清 → 处清" 漏洞侧实现。
>
> **与 [`vulnsync-design.md`](vulnsync-design.md) 的分工**：
> - `vulnsync-design.md` 聚焦 **VulnSync 服务自身**（15 源同步、Leader 选举、Kafka 推送、advisory 仲裁工程实现）
> - 本文聚焦 **漏洞业务全生命周期**（从 advisory 落地后到客户修复闭环、UI、报告、风险中心、NPatch、PoC、批量灰度）
>
> **与上位文档的强一致**：
> - 架构：[`architecture.md`](architecture.md) §2.5 + §3.3 漏洞情报链路
> - 运行模式：[`operating-modes.md`](operating-modes.md) — `observe` 仅产 advice、`protect` 才下发自动修复
> - 多租户：[`multi-tenant.md`](multi-tenant.md) — `vulnerabilities` 全局共享，`host_vulnerabilities` 严格按 `tenant_id` 隔离

---

## 1. 模块边界与六微服务分工

漏洞模块横跨六微服务，每个服务只做严格定义的一段：

```
+--------------------------------------------------------------------------------+
|                    漏洞模块在六微服务中的分工                                  |
|                                                                                |
|   外部 15 权威源                                                              |
|        |                                                                      |
|        | (1h 增量 / 1d 全量)                                                 |
|        v                                                                      |
|   +----+-----+                                                                |
|   | VulnSync |  独立服务（Leader 选举，单副本抓取）                          |
|   +----+-----+                                                                |
|        | Kafka mxsec.vuln.advisory                                            |
|        v                                                                      |
|   +----+-----+      +----------+    +-----------+                            |
|   | Consumer |----->|  MySQL   |    |   Engine  |  ConsumerGroup B           |
|   |  写入器  |      | vuln 表族|    |  匹配器   |  订阅 vuln.advisory        |
|   +----------+      +----------+    +-----+-----+                            |
|                                           |                                  |
|                                           | host × advisory NEVRA 比较       |
|                                           v                                  |
|                                     +-----+-----+                            |
|                                     |  mxsec.   |  host_vulnerability alert  |
|                                     |  engine.  |  (含 mode/priority/EPSS)   |
|                                     |  alert    |                            |
|                                     +-----+-----+                            |
|                                           |                                  |
|             +-----------------------------+--------------------+             |
|             v                             v                    v             |
|       +-----+------+              +-------+-------+      +-----+------+      |
|       |  Consumer  |              |  Manager UI   |      | Notify     |      |
|       | 落 host_   |              |  漏洞中心     |      | 邮件/短信  |      |
|       |  vuln 表   |              |  风险中心     |      | Webhook    |      |
|       +-----+------+              +-------+-------+      +------------+      |
|             |                             |                                  |
|             |     用户点修复 / 策略触发    |                                  |
|             |<----------------------------+                                  |
|             v                                                                |
|       +-----+--------+   gRPC /command   +---------------+                  |
|       |   Manager    |------------------>|  AgentCenter  |                  |
|       | Remediation  |                   |    路由       |                  |
|       | Orchestrator |                   +-------+-------+                  |
|       +--------------+                           | gRPC mTLS                 |
|                                                  v                          |
|                                          +-------+-------+                  |
|                                          | mxsec-agent  |                   |
|                                          | plugins/     |                   |
|                                          | remediation  | precheck/dry-run/ |
|                                          | scanner      | install/verify    |
|                                          +---------------+                  |
+--------------------------------------------------------------------------------+
```

### 1.1 各服务边界严格定义

| 服务 | 漏洞模块内职责 | 严禁做 |
|------|----------------|--------|
| **VulnSync** | 多源 advisory 拉取 + 仲裁 + 推 Kafka | 主机匹配 / 告警 / 修复 |
| **Consumer** | advisory → MySQL `vulnerabilities`；host_vuln alert → MySQL `host_vulnerabilities` | 检测匹配 / 优先级计算 |
| **Engine** | 主机软件指纹 × advisory NEVRA/PURL 匹配 → 产 host_vulnerability alert | 修复任务调度 / UI 查询 |
| **Manager** | 漏洞中心 / 修复编排 / pre-check 调度 / 报表 / RBAC | 实时检测 / advisory 抓取 |
| **AgentCenter** | 任务下发（pre-check / scan / remediation） + 回报转发 | 解析 advisory 业务逻辑 |
| **LLMProxy** | 可选：修复建议生成 / advisory 摘要 / CVE 中文化 | 业务决策 |

### 1.2 Agent 插件分工

| Plugin | 漏洞模块职责 |
|--------|--------------|
| `plugins/scanner` | 主机指纹采集（rpm/deb/apk + jar/pypi/npm/go 解析）+ 漏洞结果上报 + 弱口令探测 + 不安全配置采集 + 镜像分层扫描 |
| `plugins/remediation` | pre-check（仓库可用性）+ dry-run + install + 自动 verify + 回滚 |
| `plugins/npatch`（M4+） | eBPF 旁路虚拟补丁（独立守护，与 EDR 共享内核探针） |

---

## 2. 漏洞数据流（advisory → host_vulnerability → 修复）

### 2.1 advisory 入口流

```
[VulnSync]
  Cron 1h 增量 / 1d 全量
       |
       v
  advisory.Coordinator 仲裁
       |
       v
  推 Kafka mxsec.vuln.advisory（partition key = cve_id）
       |
       +--------- ConsumerGroup A: mxsec-writers ---------+
       |                                                    |
       v                                                    v
  Consumer.advisoryWriter                              落 MySQL
    1. UPSERT vulnerabilities BY cve_id            vulnerabilities
    2. UPSERT advisory_packages BY                 advisory_sources
       (cve_id, source, os_family, pkg, arch)      advisory_packages
    3. UPSERT advisory_sources BY                  epss_scores
       (source, source_advisory_id)
    4. enrichment_epss → 更新 epss_score
    5. enrichment_kev  → 更新 in_kev
    6. enrichment_cnnvd→ 更新 cnnvd_id
       |
       v
  Manager 漏洞中心 UI 可见（按 priority_score DESC）
```

> Consumer 永远不计算 `priority_score`；priority 由 Manager `vuln_priority.RecalculateAll()` 周期任务计算，原因：
> 1. priority 依赖主机暴露面（`internet_facing` 来自 ports 表），是租户级动态数据
> 2. Consumer 是纯写入器，不能做联表聚合
> 3. 每次 advisory upsert 都重算 priority 会引起 lock contention

### 2.2 主机匹配流（Engine 侧）

Engine ConsumerGroup B 订阅 `mxsec.vuln.advisory`，每条 advisory 触发主机匹配：

```go
// internal/server/engine/vuln/matcher.go（伪代码）
func (e *Engine) onAdvisory(ctx context.Context, adv *vulnsync.NormalizedAdvisory) error {
    // 1. 按 affected_packages 拆 query
    var queries []SoftwareQuery
    for _, pkg := range adv.AffectedPackages {
        if pkg.OSFamily != "" {
            // OS pkg 走 NEVRA gate
            queries = append(queries, SoftwareQuery{
                OSFamily: pkg.OSFamily,
                OSMajor:  pkg.OSMajor,
                PkgName:  pkg.PkgName,
                Arch:     pkg.Arch,
            })
        } else if pkg.Ecosystem != "" {
            // 语言包走 PURL gate
            queries = append(queries, SoftwareQuery{
                Ecosystem: pkg.Ecosystem,
                PkgName:   pkg.PkgName,
            })
        }
    }

    // 2. 查 software 表（含 tenant_id WHERE 强制注入）
    hits, _ := e.softwareRepo.MultiQuery(ctx, queries)

    // 3. NEVRA / SemVer 精确比较
    affected := matcher.Compare(adv, hits)  // 复用 advisory.matcher

    // 4. 产 host_vulnerability alert 推 Kafka
    for _, ah := range affected {
        e.emitAlert(ctx, &Alert{
            TenantID:      ah.TenantID,
            HostID:        ah.HostID,
            AlertType:     "host_vulnerability",
            CVE:           adv.CVEID,
            Severity:      adv.Severity,
            CurrentVer:    ah.CurrentVersion,
            FixedVer:      ah.FixedVersion,
            Confidence:    adv.Confidence,
            PriorityScore: adv.PriorityScore, // 来自 advisory.epss/kev/has_exploit
            Mode:          e.modeFor(ah.TenantID, ah.HostID),
            Source:        "vulnsync",
        })
    }
    return nil
}
```

> Engine **不重算 priority_score**：advisory 已含 EPSS/KEV/HasExploit，priority 公式中的 `exposure` 维度在 Manager 侧定时重算（见 §6.3）。

### 2.3 反向流（host 软件变化触发匹配）

新增 / 升级软件包时同样需匹配漏洞：

```
Agent.plugins/scanner 周期采集（5min）
        |
        v
  scanner 产 software_fingerprint event (DataType 5050)
        |
        v
  Kafka mxsec.agent.asset
        |
        +--- ConsumerGroup A → MySQL software UPSERT
        +--- ConsumerGroup B → Engine 反向查询 advisory_packages 索引
                                    |
                                    v
                              对新增 / version 变更的包做差异匹配
                                    |
                                    v
                              产 host_vulnerability alert（新发现）或
                                  vanished / patched 状态变更（Reconciler 触发）
```

---

## 3. 主机指纹采集（plugins/scanner）

### 3.1 采集范围

| 指纹类型 | 来源 | PURL 前缀 | 频次 |
|----------|------|-----------|------|
| RPM 包 | `/var/lib/rpm/Packages.db` 或 `rpm -qa` | `pkg:rpm/{distro}/{name}@{epoch}:{ver}-{rel}?arch=` | 5min |
| DEB 包 | `/var/lib/dpkg/status` | `pkg:deb/{distro}/{name}@{ver}?arch=` | 5min |
| APK 包 | `/lib/apk/db/installed` | `pkg:apk/alpine/{name}@{ver}?arch=` | 5min |
| Go 二进制 | `go version -m <bin>` | `pkg:golang/{module}@{ver}` | 15min |
| JAR | 递归 3 层 `META-INF/MANIFEST.MF` + `pom.properties` | `pkg:maven/{group}/{artifact}@{ver}` | 30min |
| PyPI | `pip list --format json` + `site-packages/*.dist-info` | `pkg:pypi/{name}@{ver}` | 30min |
| npm | `package-lock.json` + 全局 `npm ls --depth=0 --json` | `pkg:npm/{name}@{ver}` | 30min |
| RubyGems | `gem list --local` | `pkg:gem/{name}@{ver}` | 60min |
| Composer (PHP) | `composer.lock` | `pkg:composer/{vendor}/{name}@{ver}` | 60min |
| Cargo (Rust) | `Cargo.lock` | `pkg:cargo/{name}@{ver}` | 60min |
| 内核 | `uname -r` | `pkg:rpm/{distro}/kernel@{ver}` | 5min |
| 中间件运行时（被识别的服务） | `ss -tnlp` + `/proc/{pid}/exe` 反查包 | 同上 | 5min |

### 3.2 PURL 规范（关键约束）

```
PURL spec: https://github.com/package-url/purl-spec

例:
  RHEL openssl:    pkg:rpm/redhat/openssl-libs@1:3.5.5-1.el9_4?arch=x86_64
  Ubuntu libssl3:  pkg:deb/ubuntu/libssl3@3.0.2-0ubuntu1.15?arch=amd64
  Maven netty:     pkg:maven/io.netty/netty-codec@4.1.115.Final
  Go x/crypto:     pkg:golang/golang.org/x/crypto@v0.17.0

强约束：
  1. distro 字段对 OS 包是 mandatory（rhel/centos/rocky/almalinux/openeuler/anolis/kylin/uos/ubuntu/debian/alpine）
  2. epoch 对 RPM 是 mandatory（即便为 0，写成 "0:")，否则 NEVRA 比较出错
  3. arch 对 OS 包是 mandatory（用于排除 noarch vs x86_64 误匹配）
  4. Maven 命名空间用 "/"，不能用 ":"（spec 要求）
  5. Go module 路径保留斜杠（pkg:golang/golang.org/x/crypto）
```

### 3.3 上报 schema（Agent → Kafka）

```protobuf
// DataType 5050
message SoftwareFingerprint {
    string agent_id        = 1;
    string tenant_id       = 2;
    string host_id         = 3;
    int64  collected_at_ms = 4;
    string os_family       = 5;   // rhel/ubuntu/openeuler/kylin/...
    string os_major        = 6;   // "9" / "22" / "V10"
    string arch            = 7;   // x86_64 / aarch64
    repeated SoftwareItem items = 8;
}

message SoftwareItem {
    string name           = 1;
    string version_raw    = 2;    // 原始字符串
    string epoch          = 3;
    string version        = 4;    // 拆出的 version
    string release        = 5;    // 拆出的 release（仅 rpm/dpkg）
    string pkg_arch       = 6;
    string purl           = 7;
    string ecosystem      = 8;    // rpm/dpkg/apk/maven/pypi/go/npm/...
    string source_path    = 9;    // 二进制路径 or lock 文件路径
    string scope          = 10;   // system/embedded/container
    string container_id   = 11;   // 容器内时填
    int64  installed_at_ms = 12;
}
```

### 3.4 软件包指纹去重

同一台 host 同一 `name + version + arch + scope` 上报多次 → MySQL `software` 表 `ON DUPLICATE KEY UPDATE collected_at, version_raw`。

`software` 表索引：

```sql
UNIQUE KEY uk_host_pkg_arch_scope (tenant_id, host_id, name, version, arch, scope, container_id),
KEY idx_purl (purl(191)),
KEY idx_eco_name_ver (ecosystem, name, version),
KEY idx_collected (tenant_id, collected_at)
```

> tenant_id 是所有索引前缀（参考 [`multi-tenant.md`](multi-tenant.md) §3.2）。

### 3.5 容器内软件采集

容器内运行的进程通过 EDR 子系统（pid namespace）识别，scanner 通过 `nsenter` 或读 `/proc/{pid}/root/...` 采集容器内包：

```
plugins/scanner 启动时：
  1. 监听 EDR 容器事件 stream（container_created/destroyed）
  2. 对每个新容器执行：
     - nsenter -t {pid} -m -u -i -p -- rpm -qa  （or dpkg-query -l, apk info）
     - 上报时 scope=container, container_id 填充
  3. 容器销毁 → 标记 software 表对应记录为 deleted_at（保留 30 天）
```

---

## 4. 应用版本指纹（PURL + NEVRA 双模型）

mxsec **同时维护两个版本比较视角**，由 `pkg:type/` 前缀和 `os_family` 字段 gate 决定走哪条：

### 4.1 NEVRA 视角（OS 包）

适用：rpm（RHEL/Rocky/Alma/CentOS/openEuler/Anolis/Kylin/UOS）/ dpkg（Ubuntu/Debian） / apk（Alpine）

```
Name : openssl-libs
Epoch: 1                   <-- 关键，0 也要写
Version: 3.5.5
Release: 1.el9_4           <-- distro-specific
Arch: x86_64
```

比较算法：

| pkg_manager | 算法 | 代码 |
|-------------|------|------|
| rpm | RPM-vercmp（epoch → version → release，逐段 numeric/alpha 切分比较） | `internal/server/manager/biz/advisory/source.go` `CompareRPMVersion` |
| dpkg | Debian Policy §5.6.12（epoch → upstream_version → debian_revision，逐字符 ascii_order_with_tilde） | `internal/server/manager/biz/advisory/dpkg_vercmp.go` `CompareDpkgVersion` |
| apk | Alpine algorithm（与 RPM 类似但 suffix 排序不同） | `internal/server/manager/biz/advisory/source.go` `CompareAPKVersion` |

**关键不变量**：`pkgManagerFromType(pkgType, osFamily)` 决定 matcher 引擎；不允许跨 distro 比较（RHEL `1:3.5.5-1.el9_4` 不能与 Ubuntu `3.0.2-0ubuntu1.15` 直接比较，必须分别匹配各自 advisory）。

### 4.2 PURL 视角（语言包）

适用：Maven / PyPI / Go module / npm / RubyGems / Cargo / NuGet / Pub / Hex / Composer

```
PURL: pkg:maven/io.netty/netty-codec@4.1.110.Final
Ecosystem: Maven
Name: io.netty:netty-codec
Version: 4.1.110.Final
```

比较算法：按 ecosystem 各自规则（SemVer / Maven version / PEP 440 / Go modules SemVer），实现在 `advisory/matcher.go` 的 `CompareEcosystemVersion`。

### 4.3 双 gate 互斥

```go
// matcher.go 关键路径
func matchAdvisory(adv *Advisory, sw *Software) bool {
    // OS pkg gate
    if adv.OSFamily != "" && sw.OSFamily != "" {
        if adv.OSFamily != sw.OSFamily {
            return false
        }
        if adv.OSMajor != "" && adv.OSMajor != sw.OSMajor {
            return false
        }
        // 走 NEVRA 比较
        return compareNEVRA(adv, sw) < 0  // sw < adv.FixedVersion
    }

    // Ecosystem gate
    if adv.Ecosystem != "" && sw.Ecosystem != "" {
        if !strings.EqualFold(adv.Ecosystem, sw.Ecosystem) {
            return false
        }
        return compareEcosystemVersion(adv, sw) < 0
    }

    return false // 两 gate 均不满足 → 不匹配
}
```

---

## 5. 匹配引擎（Coordinator + 11 源融合）

> 实现在 [`internal/server/manager/biz/advisory/`](../internal/server/manager/biz/advisory/)（已落地）。本节为业务视角说明，工程细节见 [`vulnsync-design.md`](vulnsync-design.md) §3。

### 5.1 11 源 + 4 enrichment 一览

| 类别 | 数量 | 源 | confidence |
|------|------|----|----|
| 国际 OS advisory | 5 | RedHat / Rocky / Ubuntu / Debian / Alpine | high |
| 信创 OS advisory | 4 | openEuler / Anolis / Kylin / UOS | high |
| 语言包 advisory | 1 | OSV.dev（含 GHSA/PyPA/Go vulndb 等聚合） | medium |
| CVE metadata | 1 | NVD | low |
| Enrichment | 4 | CISA KEV / ExploitDB / CNNVD / EPSS | — |

### 5.2 仲裁 3 级 confidence

```
high      OS 厂商 advisory（含 NEVRA + distro-specific 版本）
   ↑
medium    OSV PURL（语言包精确版本范围）
   ↑
low       NVD CPE（仅 metadata）
   ↑
enrichment KEV / ExploitDB / CNNVD / EPSS（按 CVE 补字段）
```

仲裁规则：

1. **metadata 字段**（description/cvss/severity/cwe）：严格更高 confidence 覆盖；同级保留先入者
2. **affected_packages**：所有 source 并集去重（关键：RHSA + Rocky + Alma 各自有 fixed_version，必须并集）
3. **ecosystem 字段保护**：若 cve 已被 OSV 路径写过（DB 中已有 PURL 前缀），OS source 不覆盖 source/component/purl
4. **描述污染防御**：`description` 含 "Microsoft Windows" 且 OS 是 Linux 系 → 拒入库

### 5.3 fake vuln 治理

历史误产物（早期 keyword match 阶段）通过 `confidence=fake` 标记 + `deleted_at` 软删：

```go
// internal/server/manager/biz/vuln_scanner.go
func (v *VulnScanner) markFake(ctx context.Context, vulnIDs []uint, reason string) {
    db.Model(&Vulnerability{}).
        Where("id IN ?", vulnIDs).
        Updates(map[string]any{
            "confidence":   "fake",
            "deleted_at":   time.Now(),
            "patched_reason": reason,
        })
}
```

UI 默认隐藏 `confidence=fake`，仅 SystemAdmin 在 audit 视图可见。

---

## 6. 优先级评分（CVSS + EPSS + KEV + 资产重要性 + 公网暴露）

### 6.1 v2 评分公式（6 维）

```
PriorityScore = 0.25 · CVSS_normalized
              + 0.20 · ExploitScore
              + 0.15 · EPSS
              + 0.15 · ExposureScore
              + 0.15 · AssetCriticality
              + 0.10 · PatchAvailability
```

| 维度 | 来源 | 取值 |
|------|------|------|
| `CVSS_normalized` | `vulnerabilities.cvss_score / 10.0` | 0~1 |
| `ExploitScore` | `in_kev=1.0` / `has_exploit=0.7` / 无=0.0 | 0~1 |
| `EPSS` | `vulnerabilities.epss_score`（VulnSync 同步） | 0~1 |
| `ExposureScore` | `host_ratio · 0.5 + internet_facing · 0.5`（见 §6.2） | 0~1 |
| `AssetCriticality` | `hosts.criticality` 自定义评分（见 §6.4） | 0~1 |
| `PatchAvailability` | 有 `fixed_version`=0.8 / 无=0.2 | 0~1 |

阈值：

| 分数 | 等级 | 颜色 | UI 推送 |
|------|------|------|---------|
| `≥ 0.80` | critical | 红 | UI 顶 banner + 短信 + 邮件 |
| `≥ 0.60` | high | 橙 | UI 推送 + 邮件 |
| `≥ 0.35` | medium | 黄 | UI 列表 |
| `< 0.35` | low | 蓝 | UI 列表（默认折叠） |

### 6.2 ExposureScore 计算

```sql
-- 单 vuln 的 ExposureScore（vuln_priority.RecalculateOne）
WITH affected AS (
    SELECT hv.host_id
    FROM host_vulnerabilities hv
    WHERE hv.tenant_id = ? AND hv.vuln_id = ? AND hv.status = 'unpatched'
),
host_total AS (
    SELECT COUNT(*) AS n FROM hosts WHERE tenant_id = ? AND status = 'online'
),
host_ratio AS (
    SELECT LEAST(COUNT(*) * 1.0 / NULLIF((SELECT n FROM host_total), 0), 1.0) AS r
    FROM affected
),
internet_facing AS (
    -- 任一受影响主机暴露公网端口（listen != 127.0.0.1 / ::1 且服务监听）
    SELECT CASE WHEN EXISTS(
        SELECT 1 FROM ports p
        JOIN affected a ON p.host_id = a.host_id
        WHERE p.tenant_id = ?
          AND p.listen_address NOT IN ('127.0.0.1', '::1')
          AND p.state = 'LISTEN'
    ) THEN 1.0 ELSE 0.0 END AS v
)
SELECT (SELECT r FROM host_ratio) * 0.5 + (SELECT v FROM internet_facing) * 0.5;
```

### 6.3 计算时机

| 时机 | 触发器 |
|------|--------|
| VulnSync 推 advisory_upsert 后 | Consumer 异步标记 `priority_dirty=true`，每 5 分钟批量重算 dirty 漏洞 |
| host_vulnerability 状态变化 | 仅重算该 vuln（debounce 30s） |
| host 上线 / 下线 | 批量重算该 host 关联 vuln（最多影响 ~50 条） |
| 凌晨 04:00 全量重算 | 应对 EPSS daily 更新 + asset_criticality 调整 |

### 6.4 AssetCriticality 评级

```sql
-- hosts.criticality 由 4 个维度合成（自动 + 人工标签）
hosts.criticality =
    0.40 · business_line_weight    -- "核心交易" 1.0 / "测试" 0.2
  + 0.30 · public_exposure          -- 有公网 IP 1.0 / 内网 0.5 / DMZ 0.7
  + 0.20 · data_sensitivity         -- 标签 含 pii/pci/phi 1.0 / 无 0.3
  + 0.10 · manual_override          -- 客户手动标 "核心资产" 1.0 / 默认 0.5
```

UI 提供"主机标签"页支持人工评级（详见 [`asset-model.md`](asset-model.md)）。

### 6.5 SSVC 双轨（M5 前置）

CISA SSVC（Stakeholder-Specific Vulnerability Categorization）作为可选评分体系，与 priority_score 并列：

```
SSVC Decision Points:
  Exploitation: none / poc / active
  Exposure: small / controlled / open
  Utility: laborious / efficient / super_effective
  Human Impact: low / medium / high / very_high
  →
  SSVC Decision: Track / Track* / Attend / Act
```

入库到 `vulnerabilities.ssvc_decision` 字段（M5 阶段实现）。

---

## 7. 修复闭环（pre-check / dry-run / verify / rollback / 灰度）

### 7.1 11 状态机

```
pending          用户创建任务，等待审批
   │  user_confirm
   v
confirmed        审批通过，等待 pre-check
   │  enqueue
   v
prechecking      Agent 执行 pre-check
   │
   ├─→ unavailable    pre-check 失败（仓库无包 / 已最新 / 不适用）→ closed_no_op
   │
   v
ready            pre-check 通过，等待 dry-run
   │  start
   v
dry_running      Agent 执行 dry-run（yum/apt --assumeno）
   │
   ├─→ dry_failed    dry-run 失败 → closed_no_op
   │
   v
dry_done         dry-run 成功，进入实际执行队列
   │  exec
   v
running          Agent 实际执行（yum/apt -y install）
   │
   ├─→ failed       执行失败 → 触发 rollback or human investigate
   │
   v
verifying        Agent 重新采集软件指纹，对比 fixed_version
   │
   ├─→ verify_failed 版本对不上 → 升级到 manual 状态
   │
   v
success          状态 closed（patched）

任意状态 → cancelled / rolled_back（admin 手动）
```

### 7.2 各阶段细节

#### 7.2.1 pre-check

`api/host_vuln_precheck.go` 下发 DataType 9101 任务到 Agent，Agent 端 `plugins/remediation/precheck.go` 执行：

```
1. detect_os         读 /etc/os-release
2. check_installed   rpm -q / dpkg -s 确认包已装
3. check_available   yum --showduplicates / apt-cache madison 拿仓库可用版本
4. check_repo        本地 repo 是否已 enable 必要 channel（EPEL / fast）
5. check_processes   lsof / fuser 查正在使用该包的进程
6. report 9201       回报 status + repo_versions + affected_processes
```

8 个 precheck_status：

| status | 说明 | 下一步 |
|--------|------|--------|
| unchecked | 任务未触发 | 触发 pre-check |
| not_installed | 包未装（误匹配） | 标 fake、closed_no_op |
| available | 仓库有修复版本 | 进入 confirmed |
| available_epel | 需 EPEL 仓库 | 提示用户开启 EPEL |
| outdated_repo | 仓库存在但版本过旧 | 提示运维升级仓库 |
| not_in_repo | 仓库无该包修复版本 | 提示走第三方 patch / 标 closed_no_op |
| not_applicable | 该主机架构 / OS 不适用 | closed_no_op |
| failed | pre-check 执行失败 | retry × 3 后 manual |

#### 7.2.2 dry-run

```
Agent 端 plugins/remediation：
  rpm:  yum --assumeno update {pkg}-{fixed_version}
  dpkg: apt-get install --simulate {pkg}={fixed_version}
  apk:  apk add --simulate {pkg}={fixed_version}

输出捕获：
  - 将升级的依赖列表
  - 将下载的总字节数
  - 预计耗时
  - 是否需要重启服务（依据 needs-restarting / checkrestart）

UI 展示「dry-run 预览面板」：依赖列表 + 影响进程 + 是否重启
```

#### 7.2.3 install + verify

```
1. 备份 dnf history / dpkg log 当前 transaction ID（用于 rollback）
2. 执行 yum -y install {pkg}-{fixed_version}（带 --downloadonly 阶段可选）
3. 完成后立即触发 plugins/scanner 部分指纹刷新（仅该 pkg）
4. Agent verify：
   - rpm -q {pkg} → 拿到新版本
   - 与 advisory.fixed_version 用 RPM-vercmp 比较 ≥ 0 → success
   - 否则 verify_failed
5. Manager 收 verify 结果，更新 host_vulnerabilities.status = patched
```

#### 7.2.4 rollback

```
策略：
  - dnf history rollback {transaction_id}（RHEL 8+）
  - apt-get install {pkg}={prev_version}（基于 dpkg log）
  - apk del + apk add {pkg}={prev_version}（基于 apk world 备份）

不支持自动回滚的场景：
  - 内核升级（涉及 grub 配置）
  - 跨 release（如 7.9 → 8.x，远超 dnf history 范围）
  - 二进制 patch（kpatch / livepatch）→ 走专用回滚 API

回滚后置：
  - host_vulnerabilities 退到 rolled_back 状态
  - 告警 Webhook 触发（"Vuln A 在 host B 修复失败回滚"）
  - 写 audit_log（180d 保留）
```

### 7.3 批量灰度 RemediationOrchestrator

```
PlanOpts:
  BatchPercents      []int    // 默认 [10, 30, 100]
  FailureThreshold   int      // 单批失败超过该数自动暂停
  AutoVerify         bool     // 每批结束自动 verify
  AutoRollbackOnFail bool     // 失败自动 rollback
  CanaryHours        int      // 各批之间等待小时数（默认 24h）

执行流程：
  T0     Plan(vuln, hosts) → 拆 10/30/100 三批
  T0     Execute batch 1（10% 主机）
            ├─ 成功 ≥ 80% → 等 24h（or 用户手动 promote）
            ├─ 失败 ≥ FailureThreshold → 自动暂停 + 通知
            └─ 失败 < threshold → 等 24h
  T+24h  Execute batch 2（30% 主机）
            同样判定
  T+48h  Execute batch 3（100% 主机）
            完成
  任意时刻 → admin Pause/Resume/Rollback 当前批次
```

#### 表结构

```sql
CREATE TABLE remediation_plans (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL,
    vuln_id           BIGINT NOT NULL,
    total_hosts       INT NOT NULL,
    batch_percents    VARCHAR(64) NOT NULL,    -- "10,30,100"
    current_batch     INT DEFAULT 0,
    status            VARCHAR(20) NOT NULL,    -- planning/executing/paused/rolled_back/completed/failed
    failure_threshold INT DEFAULT 5,
    auto_rollback     TINYINT(1) DEFAULT 1,
    canary_hours      INT DEFAULT 24,
    created_by        VARCHAR(64) NOT NULL,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_tenant_status (tenant_id, status)
);

CREATE TABLE remediation_plan_hosts (
    plan_id     VARCHAR(64) NOT NULL,
    tenant_id   VARCHAR(64) NOT NULL,
    host_id     VARCHAR(64) NOT NULL,
    batch_no    INT NOT NULL,
    task_id     VARCHAR(64),                   -- 关联 remediation_tasks
    status      VARCHAR(20) NOT NULL,          -- pending/running/success/failed/rolled_back
    started_at  TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    PRIMARY KEY (plan_id, host_id),
    KEY idx_plan_batch (plan_id, batch_no),
    KEY idx_tenant (tenant_id)
);
```

### 7.4 operating-modes 交互

| mode | 修复任务行为 |
|------|--------------|
| `observe` | 创建任务后 **仅生成 plan + pre-check + dry-run**，停在 `dry_done`，UI 出"已就绪待执行"提示；不下发实际 install |
| `protect` | 创建任务后正常走完整 11 state |
| `protect` + 自动响应规则 `auto_remediate_critical=true` | critical 且 in_kev=true 的漏洞**自动**执行（仍需走 plan/canary） |

> 即便 `observe` 模式，用户**手动点"立即修复"按钮**仍可执行（用户主动响应不受 mode 控制，见 [`operating-modes.md`](operating-modes.md) §8.3）。

---

## 8. SBOM 导出（CycloneDX 1.5）

### 8.1 SBOM 用途

| 用途 | 受益方 |
|------|--------|
| 供应链合规（人行 19 号文、SOC 2、ISO 27001） | 客户审计 |
| 漏洞溯源（"哪些主机装了 log4j 2.14.1"） | SOC 应急 |
| CI/CD 卡点（Build 阶段输出 SBOM 给 Manager 扫漏） | 客户 DevOps |
| 主机交接 / 资产移交 | 客户运维 |

### 8.2 导出范围

```
GET /api/v2/sbom/host/{host_id}        # 单主机 SBOM
GET /api/v2/sbom/tenant                 # 整租户 SBOM（含所有 host）
GET /api/v2/sbom/host/{host_id}/diff    # 与上次 SBOM 的 diff
GET /api/v2/sbom/host/{host_id}.json    # CycloneDX 1.5 JSON
GET /api/v2/sbom/host/{host_id}.xml     # CycloneDX 1.5 XML
GET /api/v2/sbom/host/{host_id}.spdx    # SPDX 2.3（备用）
```

### 8.3 CycloneDX 1.5 schema 映射

```json
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "serialNumber": "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
  "version": 1,
  "metadata": {
    "timestamp": "2026-06-06T03:00:00Z",
    "tools": [
      {"vendor": "mxsec", "name": "mxsec-sbom-exporter", "version": "v2.0.0"}
    ],
    "component": {
      "type": "operating-system",
      "name": "RHEL",
      "version": "9.4",
      "bom-ref": "host-h-12345"
    }
  },
  "components": [
    {
      "type": "library",
      "bom-ref": "pkg:rpm/redhat/openssl-libs@1:3.5.5-1.el9_4?arch=x86_64",
      "name": "openssl-libs",
      "version": "1:3.5.5-1.el9_4",
      "purl": "pkg:rpm/redhat/openssl-libs@1:3.5.5-1.el9_4?arch=x86_64",
      "scope": "required",
      "properties": [
        {"name": "mxsec:ecosystem", "value": "rpm"},
        {"name": "mxsec:installed_at", "value": "2026-05-15T08:23:11Z"}
      ]
    }
  ],
  "vulnerabilities": [
    {
      "id": "CVE-2024-12345",
      "source": {"name": "vulnsync", "url": "https://access.redhat.com/errata/RHSA-2024:1234"},
      "ratings": [
        {"score": 8.8, "severity": "high", "method": "CVSSv3.1", "vector": "CVSS:3.1/AV:N/..."}
      ],
      "cwes": [89],
      "description": "SQL injection in ...",
      "advisories": [{"title": "RHSA-2024:1234", "url": "..."}],
      "affects": [{"ref": "pkg:rpm/redhat/openssl-libs@1:3.5.5-1.el9_4?arch=x86_64"}],
      "properties": [
        {"name": "mxsec:priority_score", "value": "0.82"},
        {"name": "mxsec:in_kev", "value": "false"},
        {"name": "mxsec:epss_score", "value": "0.0421"}
      ]
    }
  ]
}
```

### 8.4 导入扫描（CI/CD 反向）

用户在 CI 流程生成 SBOM 上传到 Manager，触发漏洞扫描：

```
POST /api/v2/sbom/import
  body: multipart/form-data
    file: <sbom.json>
    project_name: "my-app-v1.2.3"
    format: "cyclonedx" | "spdx"
    tenant_id: 自动从 JWT 注入
  →
  Manager.sbomImporter
    1. 解析 SBOM components → 提取 PURL 列表
    2. 写入 software 表（host_id 用特殊前缀 sbom:<project_name>）
    3. 调 VulnScanner.ScanPURLs(purls) → 触发 OSV 批量查 + advisory 匹配
    4. 返回扫描结果摘要（component_count / vuln_count / critical / high / medium / low）
```

> CI 集成示例：GitLab CI 中 `mxsec-cli sbom-scan --project my-app --file cyclonedx.json --fail-on-critical` 实现卡点。

---

## 9. 弱口令探测器（plugins/scanner 扩展）

### 9.1 探测范围（4 类）

| 类别 | 目标 | 探测方式 |
|------|------|----------|
| **Linux 系统账号** | `/etc/shadow` 中所有 uid > 0 用户 | 离线 PBKDF2/bcrypt 字典爆破（不访问网络） |
| **SSH 默认口令** | sshd 监听端口 | 仅检查 `PermitRootLogin`/`PasswordAuthentication` 配置 + 已知公开 known-host 字典 |
| **数据库** | mysql / postgres / redis（无密码 / 默认口令） | 本地 socket 优先（无密码 redis），远端 TCP 尝试 1 次 root/admin/默认 |
| **应用** | vsftpd / mongo / memcached / ftp / telnet | 端口扫 + banner + 默认凭证字典 |

### 9.2 探测策略

```yaml
plugins/scanner/weak_password 配置：
  linux_account:
    enabled: true
    dict_path: "/etc/mxsec/dict/top10000-passwords.txt"  # 嵌入 + 可覆盖
    max_attempts_per_user: 100                            # 防止单用户全字典爆破 CPU 飙
    max_cpu_percent: 10                                   # 限制 CPU
    hash_algos: ["sha512crypt", "bcrypt", "yescrypt"]    # /etc/shadow 主流
  ssh:
    enabled: true
    check_config_only: true                               # 仅看 sshd_config，不主动尝试登录
  mysql:
    enabled: true
    socket_paths: ["/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock"]
    tcp_targets: ["127.0.0.1:3306"]
    creds: ["root:", "root:root", "root:123456", "admin:admin"]
    max_attempts: 4                                       # 每个 target 最多 4 次
  redis:
    enabled: true
    socket_paths: ["/var/run/redis/redis.sock"]
    tcp_targets: ["127.0.0.1:6379"]
    no_auth_check: true                                   # 优先检查"无密码"模式
  postgres:
    enabled: true
    socket_paths: ["/var/run/postgresql"]
    tcp_targets: ["127.0.0.1:5432"]
    creds: ["postgres:", "postgres:postgres", "postgres:123456"]
```

### 9.3 探测结果（DataType 7050）

```protobuf
message WeakPasswordResult {
    string agent_id     = 1;
    string tenant_id    = 2;
    string host_id      = 3;
    int64  scanned_at_ms = 4;
    repeated WeakPasswordItem items = 5;
}

message WeakPasswordItem {
    string service_type   = 1;   // linux_account / ssh / mysql / redis / postgres / vsftpd / mongo
    string service_target = 2;   // 用户名 / 端口
    bool   cracked        = 3;
    string crack_method   = 4;   // dict / no_auth / default_creds
    string severity       = 5;   // critical / high / medium
    string recommendation = 6;   // "立即修改 root 密码"
    int64  attempts       = 7;   // 仅 audit 用，不公开
}
```

### 9.4 表结构

```sql
CREATE TABLE weak_passwords (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id       VARCHAR(64)  NOT NULL,
    host_id         VARCHAR(64)  NOT NULL,
    service_type    VARCHAR(30)  NOT NULL,
    service_target  VARCHAR(200) NOT NULL,
    cracked         TINYINT(1)   NOT NULL,
    crack_method    VARCHAR(30),
    severity        VARCHAR(20),
    recommendation  TEXT,
    discovered_at   TIMESTAMP    NOT NULL,
    closed_at       TIMESTAMP    NULL,
    closed_reason   VARCHAR(64),
    status          VARCHAR(20)  DEFAULT 'open',
    UNIQUE KEY uk_tenant_host_target (tenant_id, host_id, service_type, service_target),
    KEY idx_tenant_status (tenant_id, status, severity)
);
```

### 9.5 法律 + 安全约束

| 约束 | 实施 |
|------|------|
| **客户授权** | 部署 Agent 时 EULA 含弱口令探测条款；UI 全局开关默认关闭，由租户管理员显式启用 |
| **CPU/IO 限制** | 默认 max_cpu_percent=10，离线 hash 比对走低优先级队列 |
| **审计** | 所有探测会话写 `audit_log`，180 天保留；探测的具体口令绝不入库（只存 cracked: bool） |
| **不上传明文** | 即便 cracked=true，明文密码不出 Agent 内存（用 `RECOMMEND_RESET_NOW` 标记） |
| **OS 信号合规** | 不写 `last` 不记 PAM 日志（探测使用内存 hash compare，不调 PAM） |

---

## 10. 信创 OS 4 源真实接入

> VulnSync 已提供工程拆分（参考 [`vulnsync-design.md`](vulnsync-design.md) §2.2.12、§9）。本节聚焦**业务侧的数据接入策略**。

### 10.1 4 源数据获取矩阵

| OS | 厂商 | 主源 | 备源 | 离线源 | 数据特征 |
|----|------|------|------|--------|----------|
| **openEuler** | openAtom 基金会 | `repo.openeuler.org/security/data/csaf/` | `gitee.com/openeuler/security-committee` RSS | gitee mirror clone | CSAF v2，每周更新 ~50 条 |
| **Anolis OS** | 阿里 | `anas.openanolis.cn/api/sec/v1/cves/list` | 列表页 HTML 解析 | 站点抓取归档 | JSON，每月更新 ~30 条 |
| **Kylin V10** | 麒麟软件 | `kylinos.cn/support/loophole.html` | 第三方 CERT 合作（漏洞盒子/奇安信） | KA 客户提供内网镜像 | HTML 抓取，频次不稳 |
| **UOS** | 统信 | `uniontech.com/support/cve_list.html` | UOS 官方下载站 RSS | 客户提供内网镜像 | HTML 抓取，~月度 |

### 10.2 业务关键不变量

1. **Kylin / UOS 数据滞后**：官方公开滞后于厂商内部 1-4 周。商用客户必须开通**双源订阅**（"第三方 CERT 合作"渠道补滞后窗）。
2. **CSA 与 RHSA 编号不通用**：openEuler CSA 不直接复用 RHSA；同一 CVE 在 openEuler 和 RHEL 的 `fixed_version` 完全不同（rpm release 字段后缀 `oe2203` vs `el9_4`），必须分别匹配。
3. **架构差异**：Kylin 含 LoongArch / Sunway / 飞腾 / 鲲鹏多架构，`arch` 字段必须严格区分（`loongarch64` 不能和 `x86_64` 套）。
4. **断网客户兜底**：信创客户 80% 是离网部署，必须支持 `mxsec-sync` CLI 拉取 advisory dump 在内网导入。

### 10.3 接入触发条件

| 条件 | 说明 |
|------|------|
| 信创主机数 ≥ 5 | 平台启用对应信创源 |
| 信创主机数 ≥ 50 | 强制启用双源（主+备） |
| 离网部署 | 强制启用离线缓存模式 |
| 单源连续失败 ≥ 7 天 | 自动告警 + 切离线缓存 |

### 10.4 业务侧失败兜底

```
Manager 漏洞中心检测到「该 host 是信创 OS 且过去 24h 未收到 advisory_upsert」：
  1. UI 横幅：⚠ openEuler 漏洞库 N 小时未更新
  2. 自动触发 VulnSync 的 `/internal/v1/sync/source/openeuler` 重跑
  3. 仍失败 → 自动切换到 fallback 源
  4. 仍失败 7 天 → 工单系统派单到运维
```

### 10.5 人工补录通道

针对 Kylin/UOS 紧急公告（厂商发安全通告但官网页面未更新），运维通过 gRPC `/internal/v1/advisory/manual` 上传 YAML 入 `manual_advisories` 表，审批后由 VulnSync 推 Kafka。详见 [`vulnsync-design.md`](vulnsync-design.md) §9.3。

---

## 11. 镜像分层扫描（Trivy + 自研补丁库）

### 11.1 方案选型

**自研引擎不与 Trivy 重复造轮子**，但**Trivy DB 是 AGPL 不能商业捆绑**，因此：

- **扫描引擎**：调用 `trivy-server` 子进程（Apache-2.0），输出 JSON
- **漏洞库**：**不**用 Trivy 的 vuln DB（AGPL），而是用 mxsec 自有 advisory 库（已经覆盖 11 源 + 信创）做二次匹配
- **修复建议库**：自研 patch policy（基于 mxsec advisory `fixed_version` 字段，针对镜像基础层提示重建）

### 11.2 扫描流程

```
方式一：Server 端集中扫描（推荐）

  Manager API → 触发 ImageScanner
       │
       v
  ImageScanner（Server 端）
       ├─ trivy image --format json --skip-db-update --severity CRITICAL,HIGH \
       │           --vex registry/repo:tag
       │   （--skip-db-update：禁用 Trivy DB，仅做 layer 解包 + SBOM 提取）
       │
       ├─ 解析 trivy SBOM 部分（components/PURL）
       │
       ├─ 调 advisory.Coordinator 做 PURL 匹配（用 mxsec 自有库）
       │
       └─ 写 image_scans + image_vulnerabilities 表

方式二：Agent 端扫描（无 Registry 场景）

  Agent.plugins/scanner image-scan 子命令
       │
       v
  本地 docker images / podman images 拉镜像 → 同上流程
       │
       v
  上报 Server 端汇总
```

### 11.3 表结构

```sql
CREATE TABLE image_scans (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    image         VARCHAR(500) NOT NULL,
    digest        VARCHAR(100),
    os            VARCHAR(50),
    os_version    VARCHAR(50),
    total_layers  INT,
    total_vulns   INT,
    critical_cnt  INT,
    high_cnt      INT,
    medium_cnt    INT,
    low_cnt       INT,
    sbom_path     VARCHAR(500),               -- 完整 SBOM JSON 落 OSS / 本地路径
    status        VARCHAR(20),                 -- pending/scanning/done/failed
    scanned_at    TIMESTAMP NULL,
    duration_ms   INT,
    triggered_by  VARCHAR(64),                 -- user_id / ci_pipeline / cron
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_tenant_image (tenant_id, image),
    KEY idx_tenant_status (tenant_id, status)
);

CREATE TABLE image_vulnerabilities (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    image_scan_id BIGINT NOT NULL,
    vuln_id       BIGINT NULL,                 -- 关联 vulnerabilities（可能 NULL，仅 Trivy 临时漏洞）
    cve_id        VARCHAR(50) NOT NULL,
    layer_digest  VARCHAR(100),                -- 漏洞来自哪一层
    package       VARCHAR(200),
    version       VARCHAR(100),
    fixed_version VARCHAR(100),
    severity      VARCHAR(20),
    purl          VARCHAR(500),
    priority_score DECIMAL(5,3),
    KEY idx_scan (image_scan_id),
    KEY idx_tenant_cve (tenant_id, cve_id)
);
```

### 11.4 CI/CD 卡点 API

```
POST /api/v2/ci/image-scan
  Header: X-API-Key: <tenant 级专用 CI key>
  Body:
    image: "registry.example.com/app:v1.2.3"
    fail_on: "critical"               # never / high / critical
    timeout_sec: 300
  →
  {
    "scan_id": "is-xxxx",
    "status": "done",
    "summary": {"critical": 2, "high": 5, ...},
    "decision": "block"               # pass / warn / block
  }

CI 脚本：
  curl ... -X POST /api/v2/ci/image-scan ...
  if [ "$(jq -r .decision)" = "block" ]; then exit 1; fi
```

### 11.5 镜像基础层修复建议

```
检测："debian:11" 基础层有 6 个 high 漏洞
建议：
  1. 升级基础镜像到 "debian:12-slim"（已修复 5 / 6）
  2. 剩余 1 个走 apt patch（生成 Dockerfile 补丁片段）

输出 Dockerfile patch:
  FROM debian:12-slim
  RUN apt-get update && apt-get install -y --no-install-recommends \
        openssl=3.0.11-1~deb12u2 && \
      rm -rf /var/lib/apt/lists/*
```

### 11.6 镜像漏洞与运行时关联

通过 `kube_runtime` 收集器（详见 [`asset-model.md`](asset-model.md)）拿到"集群内正在跑的镜像 digest 列表"，自动触发已扫描镜像复用 + 未扫镜像异步排队：

```
Engine 监听 mxsec.engine.alert（host_vulnerability）：
  若 alert.asset_type == "image" 且对应镜像有正在运行的 Pod：
    UI 漏洞详情多一栏「正在运行 Pod 列表」
    优先级 += 0.1（运行时存在 = 暴露面增强）
```

---

## 12. PoC 验证沙箱（高危 CVE 自动验证）

### 12.1 设计原则

| 原则 | 说明 |
|------|------|
| **默认关闭** | 全局 + 租户级开关，默认 off；client EULA 单独勾选授权 |
| **管理员显式触发** | 不允许批量自动跑 PoC；单 CVE 由 admin 在 UI 点 "verify" |
| **沙箱执行** | Agent 端 `plugins/poc-verifier` 守护进程，子进程 + seccomp + namespace + cgroup 限制 |
| **不外联** | sandbox 内 dial 任意非 127.0.0.1 直接 block（egress firewall + DNS resolution disabled） |
| **强清理** | 每条规则 mandatory `cleanup` 步骤（删临时文件 / kill 进程 / 还原配置） |
| **业务时段保护** | 默认仅在客户配置的"维护窗口"内执行；高峰期入队列 |

### 12.2 规则 DSL（参考 nuclei 子集）

```yaml
# plugins/poc-verifier/rules/CVE-2024-12345.yaml
id: CVE-2024-12345
name: "OpenSSL CVE-2024-12345 LPE PoC"
severity: high
tags: [openssl, lpe, sandbox-safe]

require:
  # 仅在符合条件的 host 上执行
  os_family: ["rhel", "centos", "rocky", "almalinux"]
  package:
    name: "openssl-libs"
    version: ">= 1:3.5.0, < 1:3.5.5-1.el9_4"

match:
  # 通过本地命令/环境探测漏洞存在
  - type: command
    cmd: "openssl version -a"
    regex: "built on:.*2026 Apr"

detect:
  # 检测漏洞是否真实可触发（不实际利用）
  - type: command
    cmd: "/usr/bin/openssl s_client -connect 127.0.0.1:443 -tls1_3 < /dev/null"
    timeout_sec: 5
    expect_exit_code: [0, 1]
    expect_output_not_contain: "panic"

cleanup:
  - rm -f /tmp/mxsec-poc-CVE-2024-12345-*
  - true   # mandatory at least one cleanup step
```

### 12.3 沙箱实现

```
plugins/poc-verifier（独立守护，与 plugins/scanner 互不影响）

  接收任务（DataType 9301，由 Manager API 下发）
       │
       v
  Pre-check：
    - host 是否在维护窗口（配置项 maintenance_window: "02:00-06:00"）
    - require 条件是否全满足
    - 不满足 → 直接 skipped 回报
       │
       v
  启动沙箱子进程
    - namespace: clone(CLONE_NEWNS | CLONE_NEWUTS | CLONE_NEWIPC | CLONE_NEWPID | CLONE_NEWNET)
    - seccomp profile: allow [open/read/write/close/exit/sigaction/...] deny [socket/connect (非 127.0.0.1) / fork / exec 非白名单]
    - cgroup: cpu 10% / memory 256MB / pids 32
    - 网络：CLONE_NEWNET 后仅创建 lo 接口
    - 文件系统：read-only bind mount (/usr/bin/openssl 等需要)
       │
       v
  执行 match 步骤
    - 输出捕获到 stdout/stderr buffer（最大 64KB，超限截断）
    - 超时 10s（默认）
       │
       v
  执行 detect 步骤
    - 同样捕获 + 校验 expect_*
       │
       v
  执行 cleanup 步骤（必须执行，即便上面失败）
       │
       v
  上报结果（DataType 9302）
    - status: success_vuln / success_safe / failed / timeout / skipped
    - raw_output (base64, 64KB max)
    - exec_seconds
```

### 12.4 表结构 + 审计

```sql
CREATE TABLE poc_executions (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    job_id        VARCHAR(64) NOT NULL UNIQUE,
    cve_id        VARCHAR(50) NOT NULL,
    host_id       VARCHAR(64) NOT NULL,
    rule_id       VARCHAR(64) NOT NULL,
    rule_version  VARCHAR(20) NOT NULL,
    status        VARCHAR(20) NOT NULL,    -- pending/running/success_vuln/success_safe/failed/timeout/skipped
    raw_output_b64 TEXT,
    exec_seconds  INT,
    triggered_by  VARCHAR(64) NOT NULL,
    started_at    TIMESTAMP,
    finished_at   TIMESTAMP,
    KEY idx_tenant_cve (tenant_id, cve_id),
    KEY idx_host (host_id),
    KEY idx_tenant_status (tenant_id, status)
);
```

每条 PoC 执行写 `audit_log`（180 天保留）：

```json
{
  "actor": "admin@bank-a.com",
  "action": "poc_verify_trigger",
  "target": "host=h-12345 cve=CVE-2024-12345",
  "rule_id": "openssl-cve-2024-12345-v1",
  "approval_chain": ["admin1", "security-lead"],
  "result": "success_vuln",
  "timestamp": "2026-06-06T04:23:11Z"
}
```

### 12.5 PoC 命中后的联动

```
status=success_vuln →
  1. 更新 host_vulnerabilities.poc_verified=true
  2. priority_score 提升（额外 +0.15）
  3. 漏洞中心 UI 高亮 "PoC 已验证"
  4. 自动推荐：NPatch 虚拟补丁（M4+） / 立即修复 / 隔离主机
```

---

## 13. 漏洞 → 进程 → 业务关联

### 13.1 设计目标

SOC 收到 high CVE 告警时第一问：

> 这个漏洞影响哪些业务系统？哪些进程在用？打补丁要重启什么服务？

mxsec 提供**常态化**视图（不仅 pre-check 时跑一次）：

```
host_vulnerabilities 详情页：
  漏洞: CVE-2024-12345 (OpenSSL high)
  ├─ 受影响包: openssl-libs (1:3.5.0)
  ├─ 受影响进程（常态化 lsof）:
  │   ├─ /usr/sbin/nginx (PID 1234)         业务线: 核心交易
  │   │     ├─ 监听端口: 80, 443
  │   │     ├─ 上游 service: payment-api
  │   │     └─ 重启影响: 业务中断 ~3s
  │   ├─ /usr/bin/mysqld (PID 2345)         业务线: 核心交易
  │   │     ├─ 监听端口: 3306
  │   │     └─ 重启影响: 5min（主从切换）
  │   └─ /opt/app/api-server (PID 3456)     业务线: 风控
  │         └─ 重启影响: 30s（自动 graceful）
  └─ 修复建议:
      1. dnf upgrade openssl-libs        （需重启 nginx / mysql / api-server）
      2. NPatch 虚拟补丁                  （零业务中断，M4+）
```

### 13.2 数据采集（持续 lsof）

`plugins/scanner` 周期任务（每 5min）：

```
1. 读 software 表所有有 has_open_vuln=true 的包
2. 对每个包：
   - lsof -F pcfi -- /path/to/file（找占用文件的进程）
   - 或 fuser /path/to/file
3. 关联进程信息：
   - cmdline / pid / uid / start_time
   - 监听端口（ss -tnlp）
   - 业务标签（环境变量 MXSEC_BUSINESS=core-trading or hosts.business_line 配置）
4. 上报 DataType 5060 process_pkg_link
```

### 13.3 表结构

```sql
CREATE TABLE host_vuln_processes (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id       VARCHAR(64) NOT NULL,
    host_vuln_id    BIGINT NOT NULL,
    pid             INT NOT NULL,
    process_name    VARCHAR(200),
    cmdline         VARCHAR(500),
    binary_path     VARCHAR(500),
    user            VARCHAR(64),
    listen_ports    VARCHAR(200),         -- "80,443"
    business_line   VARCHAR(100),         -- 来自主机标签
    started_at      TIMESTAMP NULL,
    detected_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    restart_impact  VARCHAR(30),          -- none/quick/long/manual
    UNIQUE KEY uk_hv_pid (host_vuln_id, pid),
    KEY idx_tenant (tenant_id),
    KEY idx_business (business_line)
);
```

### 13.4 业务影响视图聚合

```sql
-- "我有多少业务线被 CVE-2024-12345 影响"
SELECT
    hvp.business_line,
    COUNT(DISTINCT hv.host_id) AS affected_hosts,
    COUNT(DISTINCT hvp.process_name) AS affected_processes
FROM host_vulnerabilities hv
JOIN host_vuln_processes hvp ON hvp.host_vuln_id = hv.id
JOIN vulnerabilities v ON v.id = hv.vuln_id
WHERE v.cve_id = 'CVE-2024-12345'
  AND hv.tenant_id = ?
  AND hv.status = 'unpatched'
GROUP BY hvp.business_line
ORDER BY affected_hosts DESC;
```

---

## 14. 不安全配置（SSH / Redis / Mongo / Apache / Nginx / MySQL 6 类）

### 14.1 配置项清单

| 应用 | 检查项 | 严重级别 |
|------|--------|----------|
| **SSH** | PermitRootLogin=yes | high |
|        | PasswordAuthentication=yes 且无 MFA | high |
|        | Protocol 1 启用 | critical |
|        | 弱 Cipher (3des/arcfour) | medium |
|        | 监听 0.0.0.0 而非内网 IP | medium |
| **Redis** | bind 0.0.0.0 且无 requirepass | critical |
|          | protected-mode no | high |
|          | RDB 持久化关闭 | low |
|          | maxmemory 未设置 | medium |
| **MongoDB** | bindIp=0.0.0.0 且 auth=disabled | critical |
|            | scram-sha-1 而非 sha-256 | medium |
|            | TLS 未启用 | high |
| **Apache** | ServerTokens Full | low |
|           | Indexes 启用 | medium |
|           | mod_status 暴露 /server-status | high |
|           | TraceEnable on | medium |
| **Nginx** | server_tokens on | low |
|          | autoindex on | medium |
|          | ssl_protocols 含 SSLv2/SSLv3/TLSv1.0 | high |
| **MySQL** | bind-address=0.0.0.0 且 mysql.user 含 ''@'%' | critical |
|          | log-bin 关闭（无审计） | medium |
|          | local_infile=ON | medium |
|          | skip-grant-tables 启用 | critical |

### 14.2 探测实现（plugins/scanner 扩展）

```
plugins/scanner/insecure_config/
├── ssh.go          # 解析 /etc/ssh/sshd_config + /etc/ssh/sshd_config.d/*
├── redis.go        # 解析 /etc/redis/redis.conf + CONFIG GET *（仅本地 socket）
├── mongo.go        # 解析 /etc/mongod.conf
├── apache.go       # 解析 /etc/httpd/conf/httpd.conf + /etc/apache2/apache2.conf + conf.d/*
├── nginx.go        # 解析 /etc/nginx/nginx.conf + conf.d/*.conf
└── mysql.go        # 解析 my.cnf + 查 information_schema（仅本地 socket）
```

### 14.3 与漏洞模块统一（风险中心）

不安全配置在统一**风险中心**呈现，与漏洞 / 弱口令 / 镜像漏洞共用 priority_score：

```
风险中心 6 类标签：
  漏洞 (vulnerability)
  弱口令 (weak_password)
  不安全配置 (insecure_config)
  账号风险 (account_risk)
  镜像漏洞 (image_vuln)
  容器配置 (k8s_config)
```

统一表 `risks`（详见 §16）。

---

## 15. CVSS 4.0 / SSVC 双轨

### 15.1 CVSS 4.0 字段扩展

NVD 自 2023.11 起对部分 CVE 同时发布 v3.1 和 v4.0 评分。mxsec 双轨存储：

```sql
ALTER TABLE vulnerabilities
    ADD COLUMN cvss4_score     DECIMAL(4,1) DEFAULT 0 AFTER cvss_score,
    ADD COLUMN cvss4_vector    VARCHAR(250) DEFAULT '' AFTER cvss_vector,
    ADD COLUMN cvss_version    VARCHAR(10)  DEFAULT '3.1' AFTER cvss_vector,
    ADD INDEX idx_cvss4 (cvss4_score);
```

`cvss_version` 字段标识"该漏洞实际采用 v3.1 还是 v4.0 主导评分"：

| cvss_version | 含义 |
|--------------|------|
| `3.1` | 仅 v3.1（默认） |
| `4.0` | NVD 发布了 v4.0，cvss_vector / cvss_score 字段以 v4 为主 |
| `both` | 双轨，UI 同时展示 |

### 15.2 priority 计算策略

```
若 cvss_version == "4.0" or "both"，CVSS_normalized 优先取 cvss4_score
否则 fallback cvss_score（v3.1）
```

### 15.3 SSVC 决策点

CISA SSVC 输出 4 档决策（Track / Track\* / Attend / Act），与 priority_score 并列展示：

```sql
ALTER TABLE vulnerabilities
    ADD COLUMN ssvc_exploitation VARCHAR(20) DEFAULT '',      -- none/poc/active
    ADD COLUMN ssvc_exposure     VARCHAR(20) DEFAULT '',      -- small/controlled/open
    ADD COLUMN ssvc_utility      VARCHAR(20) DEFAULT '',
    ADD COLUMN ssvc_human_impact VARCHAR(20) DEFAULT '',
    ADD COLUMN ssvc_decision     VARCHAR(20) DEFAULT '' INDEX; -- track/track*/attend/act
```

UI 在漏洞详情面板提供 SSVC tab，租户可选择主轨用 priority_score 还是 SSVC（[`multi-tenant.md`](multi-tenant.md) §7.1 租户配置覆盖）。

---

## 16. 多租户隔离

### 16.1 隔离矩阵

| 实体 | 隔离方式 |
|------|----------|
| `vulnerabilities` | **全局共享**，不带 tenant_id（漏洞知识库是平台级数据） |
| `advisory_packages` | 全局共享 |
| `epss_scores` | 全局共享 |
| `host_vulnerabilities` | **租户隔离**，强制 `tenant_id` 索引前缀 |
| `software` | 租户隔离 |
| `weak_passwords` | 租户隔离 |
| `insecure_configs` | 租户隔离 |
| `image_scans` | 租户隔离 |
| `remediation_tasks` | 租户隔离 |
| `remediation_plans` | 租户隔离 |
| `poc_executions` | 租户隔离 |
| `risks`（统一风险中心） | 租户隔离 |
| `audit_log` | 租户隔离 |

### 16.2 查询路径

所有 host_vulnerability / remediation / weak_password / image_scan 查询走 [`multi-tenant.md`](multi-tenant.md) §3.3 的 `TenantScope.Apply`，禁止裸 `db.Where`：

```go
// internal/server/manager/biz/vuln_scanner.go 中所有 query 都走 TenantScope
db.Scopes(tenant.FromContext(ctx).Apply).
    Where("vuln_id = ?", vulnID).
    Find(&hostVulns)
```

> Engine 侧消费 `mxsec.vuln.advisory`（全局 topic），匹配主机时也强制 `tenant_id` 注入 query。

### 16.3 LLM 调用租户隔离

修复建议 / advisory 中文化 / CVE 摘要的 LLM 调用都经 LLMProxy 按租户路由：

| 场景 | 调用方 | 注意 |
|------|--------|------|
| advisory 中文摘要 | Manager（异步） | 不传 host 信息，仅传 description |
| 修复建议生成 | Manager（同步） | 严格 tenant_id 检查，禁止跨租户 |
| 主机修复历史问答 | LLM Q&A 模块 | sensitive 字段 mask + 全程 audit |

详见 [`llmproxy-design.md`](llmproxy-design.md) §7 多租户隔离。

---

## 17. 表结构完整清单

### 17.1 核心表

```sql
-- 漏洞知识库（全局共享）
CREATE TABLE vulnerabilities (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    cve_id          VARCHAR(50)  UNIQUE NOT NULL,
    osv_id          VARCHAR(100) DEFAULT '',
    cnnvd_id        VARCHAR(50)  DEFAULT '',
    cnvd_id         VARCHAR(50)  DEFAULT '',
    purl            VARCHAR(500) DEFAULT '',
    severity        VARCHAR(20)  NOT NULL,
    cvss_score      DECIMAL(4,1) DEFAULT 0,
    cvss_vector     VARCHAR(250) DEFAULT '',
    cvss4_score     DECIMAL(4,1) DEFAULT 0,
    cvss4_vector    VARCHAR(250) DEFAULT '',
    cvss_version    VARCHAR(10)  DEFAULT '3.1',
    epss_score      DECIMAL(7,6) DEFAULT 0,
    in_kev          TINYINT(1) DEFAULT 0,
    has_exploit     TINYINT(1) DEFAULT 0,
    exploit_ref     VARCHAR(500) DEFAULT '',
    cwe_id          VARCHAR(200) DEFAULT '',
    cwe_category    VARCHAR(30)  DEFAULT 'other',
    component       VARCHAR(200) DEFAULT '',
    description     TEXT,
    description_cn  TEXT,                      -- LLM 中文摘要
    current_version VARCHAR(100) DEFAULT '',
    fixed_version   VARCHAR(100) DEFAULT '',
    affected_versions VARCHAR(500) DEFAULT '',
    reference_url   VARCHAR(500) DEFAULT '',
    source          VARCHAR(20),
    confidence      VARCHAR(10)  DEFAULT 'low',
    attack_vector   VARCHAR(20)  DEFAULT '',
    vuln_type       VARCHAR(30)  DEFAULT '',
    vuln_category   VARCHAR(30)  DEFAULT 'other',
    restart_action  VARCHAR(30)  DEFAULT 'unknown',
    patch_available TINYINT(1)   DEFAULT 0,
    priority_score  DECIMAL(5,3) DEFAULT 0,
    exposure_score  DECIMAL(3,2) DEFAULT 0,
    ssvc_decision   VARCHAR(20)  DEFAULT '',
    discovered_at   TIMESTAMP,
    updated_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMP    NULL,
    KEY idx_priority (priority_score),
    KEY idx_severity_priority (severity, priority_score),
    KEY idx_confidence (confidence),
    KEY idx_cnnvd (cnnvd_id),
    KEY idx_deleted (deleted_at)
);

-- 主机漏洞关联（租户隔离）
CREATE TABLE host_vulnerabilities (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id       VARCHAR(64) NOT NULL,
    vuln_id         BIGINT NOT NULL,
    host_id         VARCHAR(64) NOT NULL,
    hostname        VARCHAR(200),
    ip              VARCHAR(45),
    current_version VARCHAR(100),
    status          VARCHAR(20)  NOT NULL DEFAULT 'unpatched',
                                          -- unpatched/patching/patched/vanished/resurfaced/ignored/false_positive
    patched_reason  VARCHAR(32)  DEFAULT '',
    prev_status     VARCHAR(20)  DEFAULT '',
    patched_at      TIMESTAMP    NULL,
    vanished_at     TIMESTAMP    NULL,
    resurfaced_at   TIMESTAMP    NULL,
    asset_type      VARCHAR(20)  DEFAULT 'host',     -- host/image/container
    subscope        VARCHAR(30)  DEFAULT 'unknown',
    fix_owner       VARCHAR(20)  DEFAULT 'unknown',  -- ops/dev/security
    host_binary_path VARCHAR(500),
    precheck_status VARCHAR(30)  DEFAULT 'unchecked',
    precheck_message VARCHAR(500),
    precheck_packages TEXT,
    precheck_affected_processes TEXT,
    precheck_checked_at TIMESTAMP NULL,
    poc_verified    TINYINT(1)   DEFAULT 0,
    npatch_active   TINYINT(1)   DEFAULT 0,
    last_scanned_at TIMESTAMP    NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_tenant_vuln_host (tenant_id, vuln_id, host_id),
    KEY idx_tenant_status (tenant_id, status),
    KEY idx_tenant_host (tenant_id, host_id),
    KEY idx_tenant_priority (tenant_id, status, vuln_id)
);

-- 软件指纹（租户隔离）
CREATE TABLE software (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    host_id       VARCHAR(64) NOT NULL,
    name          VARCHAR(200) NOT NULL,
    version       VARCHAR(100) NOT NULL,
    version_raw   VARCHAR(200),
    epoch         VARCHAR(20),
    release_str   VARCHAR(100),
    arch          VARCHAR(20),
    purl          VARCHAR(500),
    ecosystem     VARCHAR(30),
    source_path   VARCHAR(500),
    scope         VARCHAR(20) DEFAULT 'system',
    container_id  VARCHAR(100) DEFAULT '',
    installed_at  TIMESTAMP NULL,
    collected_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at    TIMESTAMP NULL,
    UNIQUE KEY uk_host_pkg (tenant_id, host_id, name, version, arch, scope, container_id),
    KEY idx_purl (purl(191)),
    KEY idx_ecosystem (tenant_id, ecosystem, name)
);
```

### 17.2 修复闭环

```sql
CREATE TABLE remediation_tasks (
    id               BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id        VARCHAR(64) NOT NULL,
    vuln_id          BIGINT NOT NULL,
    cve_id           VARCHAR(50),
    host_id          VARCHAR(64) NOT NULL,
    plan_id          VARCHAR(64),                   -- 关联 remediation_plans，单点修复为 NULL
    component        VARCHAR(200),
    fixed_version    VARCHAR(100),
    command          TEXT,
    status           VARCHAR(20) NOT NULL DEFAULT 'pending',
    precheck_status  VARCHAR(30),
    dry_run_output   TEXT,
    exec_output      TEXT,
    exit_code        INT,
    verify_result    VARCHAR(20),                    -- success/failed/skipped
    rollback_status  VARCHAR(20),
    rollback_txn_id  VARCHAR(64),                    -- dnf history transaction id
    created_by       VARCHAR(64),
    confirmed_by     VARCHAR(64),
    confirmed_at     TIMESTAMP NULL,
    started_at       TIMESTAMP NULL,
    finished_at      TIMESTAMP NULL,
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_tenant_status (tenant_id, status),
    KEY idx_tenant_host (tenant_id, host_id),
    KEY idx_plan (plan_id)
);

CREATE TABLE remediation_task_events (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id   VARCHAR(64) NOT NULL,
    task_id     BIGINT NOT NULL,
    stage       VARCHAR(30) NOT NULL,                -- precheck/dry_run/install/verify/rollback
    status      VARCHAR(20) NOT NULL,
    payload     TEXT,                                 -- agent 回报 raw JSON
    happened_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_task (task_id),
    KEY idx_tenant (tenant_id)
);

CREATE TABLE remediation_plans (
    id                VARCHAR(64) PRIMARY KEY,
    tenant_id         VARCHAR(64) NOT NULL,
    vuln_id           BIGINT NOT NULL,
    total_hosts       INT NOT NULL,
    batch_percents    VARCHAR(64) NOT NULL,
    current_batch     INT DEFAULT 0,
    status            VARCHAR(20) NOT NULL,
    failure_threshold INT DEFAULT 5,
    auto_rollback     TINYINT(1) DEFAULT 1,
    canary_hours      INT DEFAULT 24,
    created_by        VARCHAR(64) NOT NULL,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_tenant_status (tenant_id, status)
);

CREATE TABLE remediation_plan_hosts (
    plan_id     VARCHAR(64) NOT NULL,
    tenant_id   VARCHAR(64) NOT NULL,
    host_id     VARCHAR(64) NOT NULL,
    batch_no    INT NOT NULL,
    task_id     BIGINT,
    status      VARCHAR(20) NOT NULL,
    started_at  TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    PRIMARY KEY (plan_id, host_id),
    KEY idx_plan_batch (plan_id, batch_no),
    KEY idx_tenant (tenant_id)
);
```

### 17.3 弱口令 / 不安全配置 / 镜像

```sql
CREATE TABLE weak_passwords (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id       VARCHAR(64) NOT NULL,
    host_id         VARCHAR(64) NOT NULL,
    service_type    VARCHAR(30) NOT NULL,
    service_target  VARCHAR(200) NOT NULL,
    cracked         TINYINT(1) NOT NULL,
    crack_method    VARCHAR(30),
    severity        VARCHAR(20),
    recommendation  TEXT,
    discovered_at   TIMESTAMP NOT NULL,
    closed_at       TIMESTAMP NULL,
    closed_reason   VARCHAR(64),
    status          VARCHAR(20) DEFAULT 'open',
    UNIQUE KEY uk_tenant_host_target (tenant_id, host_id, service_type, service_target),
    KEY idx_tenant_status (tenant_id, status, severity)
);

CREATE TABLE insecure_configs (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id       VARCHAR(64) NOT NULL,
    host_id         VARCHAR(64) NOT NULL,
    application     VARCHAR(30) NOT NULL,        -- ssh/redis/mongo/apache/nginx/mysql
    rule_id         VARCHAR(64) NOT NULL,        -- ssh.permit_root_login / redis.bind_0.0.0.0_no_auth
    severity        VARCHAR(20),
    current_value   VARCHAR(500),
    recommend_value VARCHAR(500),
    config_path     VARCHAR(500),
    detected_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status          VARCHAR(20) DEFAULT 'open',
    UNIQUE KEY uk_tenant_host_rule (tenant_id, host_id, rule_id),
    KEY idx_tenant_status (tenant_id, status, severity)
);

CREATE TABLE image_scans (...);              -- §11.3
CREATE TABLE image_vulnerabilities (...);    -- §11.3
CREATE TABLE poc_executions (...);           -- §12.4
CREATE TABLE host_vuln_processes (...);      -- §13.3
```

### 17.4 风险中心统一表

```sql
-- 5 类风险统一索引（性能优化用，不取代各业务表）
CREATE TABLE risks (
    id              VARCHAR(64) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    type            VARCHAR(20) NOT NULL,    -- vulnerability/weak_password/insecure_config/account_risk/image_vuln
    ref_table       VARCHAR(64) NOT NULL,
    ref_id          BIGINT NOT NULL,
    asset_id        VARCHAR(64) NOT NULL,
    asset_type      VARCHAR(20) NOT NULL,
    severity        VARCHAR(20) NOT NULL,
    priority_score  DECIMAL(5,3) DEFAULT 0,
    status          VARCHAR(20) NOT NULL DEFAULT 'discovered',
                                            -- discovered/managing/fixing/verifying/closed/reopened/false_positive
    discovered_at   TIMESTAMP NOT NULL,
    closed_at       TIMESTAMP NULL,
    closed_reason   VARCHAR(64),
    poc_verified    TINYINT(1) DEFAULT 0,
    npatch_active   TINYINT(1) DEFAULT 0,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_tenant_type_status (tenant_id, type, status),
    KEY idx_tenant_priority (tenant_id, priority_score DESC),
    KEY idx_tenant_asset (tenant_id, asset_id, asset_type)
);
```

### 17.5 数据源管理 + 信创补录 + advisory 源表

详见 [`vulnsync-design.md`](vulnsync-design.md) §7.1（`vuln_data_sources`） / §7.3（`advisory_sources`、`advisory_packages`） / §7.5（`epss_scores`） / §7.6（`manual_advisories`），本文不复述。

---

## 18. Go 接口骨架

### 18.1 包结构

```
internal/server/manager/biz/vuln/
├── scanner.go              # VulnScanner（已落地，复用）
├── priority.go             # PriorityCalculator v2（6 维）
├── reconcile.go            # Reconciler 三状态机（vanished/patched/resurfaced）
├── targeted.go             # ScanTaskManager（已落地）
├── precheck.go             # PreCheckDispatcher → Agent 9101 任务
├── remediation_executor.go # RemediationExecutor（11 state，已落地）
├── orchestrator.go         # RemediationOrchestrator（批量灰度，§7.3）
├── sbom_exporter.go        # CycloneDX 1.5 导出 / SPDX 备用
├── sbom_importer.go        # SBOM 导入扫描
├── image_scanner.go        # 镜像扫描 + Trivy server 适配
├── weak_password.go        # 弱口令任务调度
├── insecure_config.go      # 不安全配置任务调度
├── poc_verifier.go         # PoC 验证调度
├── risk_center.go          # 5 类风险统一查询
└── lifecycle.go            # 通用状态机机

internal/server/engine/vuln/
├── matcher.go              # advisory × software NEVRA/PURL 匹配
├── consumer.go             # Kafka mxsec.vuln.advisory 消费
└── alert_emitter.go        # 产 host_vulnerability alert → Kafka mxsec.engine.alert

plugins/scanner/
├── pkg/                    # OS 包采集（rpm/dpkg/apk）
├── lang/                   # 语言包采集
├── weak_password/          # 弱口令探测
├── insecure_config/        # 不安全配置
└── image/                  # 容器镜像扫描

plugins/remediation/
├── precheck.go             # pre-check（已落地）
├── dry_run.go
├── installer.go            # rpm/dpkg/apk install
├── verifier.go             # 重采集 + NEVRA 比对
└── rollback.go             # dnf history rollback

plugins/poc-verifier/       # M4+ 新增
├── sandbox.go              # namespace + seccomp 沙箱
├── rules/                  # YAML 规则库（git 维护）
└── runner.go

plugins/npatch/             # M4+ 新增
├── ebpf/                   # cgroup_skb 钩子
└── userspace_fallback.go   # netfilter 退路
```

### 18.2 核心接口（业务层）

```go
// Package vuln — mxsec 漏洞业务全生命周期。
package vuln

import (
    "context"
    "time"

    "go.uber.org/zap"
    "gorm.io/gorm"
)

// RemediationOrchestrator 批量灰度修复编排。
type RemediationOrchestrator interface {
    // Plan 制定批次计划（按 [10,30,100] 分批）
    Plan(ctx context.Context, vulnID uint, hostIDs []string, opts PlanOpts) (*Plan, error)

    // Execute 启动第 N 批
    Execute(ctx context.Context, planID string, batchNo int) error

    // Pause 暂停后续批次
    Pause(ctx context.Context, planID string, reason string) error

    // Resume 恢复
    Resume(ctx context.Context, planID string) error

    // Rollback 回滚指定批次（基于 dnf history / dpkg log）
    Rollback(ctx context.Context, planID string, batchNo int) error
}

type PlanOpts struct {
    BatchPercents      []int     // 默认 [10, 30, 100]
    FailureThreshold   int       // 单批失败超过该数自动暂停
    CanaryHours        int       // 各批之间等待小时数（默认 24）
    AutoVerify         bool
    AutoRollbackOnFail bool
}

type Plan struct {
    ID             string
    TenantID       string
    VulnID         uint
    TotalHosts     int
    BatchPercents  []int
    CurrentBatch   int
    Status         string
    Hosts          []PlanHost
}

type PlanHost struct {
    HostID    string
    BatchNo   int
    TaskID    int64
    Status    string
}

// SBOMExporter CycloneDX 1.5 导出。
type SBOMExporter interface {
    // ExportHost 单主机 SBOM
    ExportHost(ctx context.Context, hostID string, format SBOMFormat) ([]byte, error)

    // ExportTenant 整租户 SBOM（按 host 分章节）
    ExportTenant(ctx context.Context, format SBOMFormat) ([]byte, error)

    // Diff 与上次快照对比
    Diff(ctx context.Context, hostID string, since time.Time) (*SBOMDiff, error)
}

type SBOMFormat string

const (
    SBOMFormatCycloneDXJSON SBOMFormat = "cyclonedx-json"
    SBOMFormatCycloneDXXML  SBOMFormat = "cyclonedx-xml"
    SBOMFormatSPDXJSON      SBOMFormat = "spdx-json"
)

// WeakPasswordScheduler 弱口令探测调度。
type WeakPasswordScheduler interface {
    // ScheduleScan 触发指定 host 的弱口令探测
    ScheduleScan(ctx context.Context, hostIDs []string, services []string) (taskID string, err error)

    // ListFindings 查询结果（按 host / severity / status 筛选）
    ListFindings(ctx context.Context, q FindingQuery) ([]*WeakPasswordFinding, error)
}

// InsecureConfigScheduler 不安全配置检测调度。
type InsecureConfigScheduler interface {
    ScheduleScan(ctx context.Context, hostIDs []string, applications []string) (taskID string, err error)
    ListFindings(ctx context.Context, q FindingQuery) ([]*InsecureConfigFinding, error)
}

// PoCVerifier 漏洞 PoC 验证。
type PoCVerifier interface {
    // Verify 对单 host 单 CVE 执行 PoC（异步）
    Verify(ctx context.Context, hostID string, cveID string, operatorID string) (jobID string, err error)

    // GetResult 查询执行结果
    GetResult(ctx context.Context, jobID string) (*PoCResult, error)

    // RegisterRule 注册规则（仅 SystemAdmin）
    RegisterRule(ctx context.Context, rule *PoCRule) error

    // ListRules 规则列表
    ListRules(ctx context.Context, tags []string) ([]*PoCRule, error)
}

// RiskCenter 5 类风险统一查询。
type RiskCenter interface {
    // List 联合查询
    List(ctx context.Context, q RiskQuery) ([]*RiskItem, int64, error)

    // Lifecycle 推进单条风险状态
    Lifecycle(ctx context.Context, riskID string, action LifecycleAction) error

    // Stats 风险统计（按类型 / 严重级别 / 资产）
    Stats(ctx context.Context, scope StatsScope) (*RiskStats, error)
}

type RiskType string

const (
    RiskTypeVulnerability  RiskType = "vulnerability"
    RiskTypeWeakPassword   RiskType = "weak_password"
    RiskTypeInsecureConfig RiskType = "insecure_config"
    RiskTypeAccountRisk    RiskType = "account_risk"
    RiskTypeImageVuln      RiskType = "image_vuln"
)

type RiskItem struct {
    ID            string
    TenantID      string
    Type          RiskType
    Title         string
    Severity      string
    PriorityScore float64
    AssetID       string
    AssetType     string
    Description   string
    Suggestion    string
    Status        string
    DiscoveredAt  time.Time
    PoCVerified   bool
    NPatchActive  bool
}

// PriorityCalculator v2 6 维评分。
type PriorityCalculator interface {
    RecalculateAll(ctx context.Context, tenantID string) error
    RecalculateOne(ctx context.Context, tenantID string, vulnID uint) (float64, error)
    RecalculateForHost(ctx context.Context, tenantID, hostID string) error
}

// 工厂
func NewRemediationOrchestrator(db *gorm.DB, logger *zap.Logger) RemediationOrchestrator
func NewSBOMExporter(db *gorm.DB, logger *zap.Logger) SBOMExporter
func NewWeakPasswordScheduler(db *gorm.DB, logger *zap.Logger) WeakPasswordScheduler
func NewInsecureConfigScheduler(db *gorm.DB, logger *zap.Logger) InsecureConfigScheduler
func NewPoCVerifier(db *gorm.DB, logger *zap.Logger) PoCVerifier
func NewRiskCenter(db *gorm.DB, logger *zap.Logger) RiskCenter
func NewPriorityCalculator(db *gorm.DB, logger *zap.Logger) PriorityCalculator
```

### 18.3 Engine 匹配接口

```go
// Package vuln — Engine 侧
package vuln

// AdvisoryMatcher 消费 mxsec.vuln.advisory 并产 host_vulnerability alert。
type AdvisoryMatcher interface {
    // OnAdvisory 收到一条 advisory_upsert，触发匹配
    OnAdvisory(ctx context.Context, adv *NormalizedAdvisory) error

    // OnSoftwareChanged software 表新增/升级时反向匹配
    OnSoftwareChanged(ctx context.Context, tenantID, hostID string, items []SoftwareItem) error
}

// PriorityEnricher Engine 侧给 alert 加上 priority/EPSS/KEV
type PriorityEnricher interface {
    Enrich(ctx context.Context, alert *Alert) error
}
```

---

## 19. API 规范（节选）

详见 [`api-reference.md`](api-reference.md)。本节聚焦 v2 新增。

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/v2/vulnerabilities` | 漏洞列表（priority / severity / cve_id / cnnvd 搜索） |
| GET | `/api/v2/vulnerabilities/{id}` | 漏洞详情（含影响 host / 进程 / 业务线） |
| POST | `/api/v2/vulnerabilities/{id}/recalc-priority` | 手动重算优先级 |
| GET | `/api/v2/host-vulnerabilities` | 主机漏洞列表 |
| POST | `/api/v2/host-vulnerabilities/{id}/poc-verify` | 触发 PoC 验证（admin） |
| POST | `/api/v2/host-vulnerabilities/{id}/precheck` | 触发 pre-check |
| POST | `/api/v2/remediation/tasks` | 单点修复任务 |
| POST | `/api/v2/remediation/plans` | 创建批量灰度计划 |
| POST | `/api/v2/remediation/plans/{id}/execute` | 启动指定批次 |
| POST | `/api/v2/remediation/plans/{id}/pause` | 暂停 |
| POST | `/api/v2/remediation/plans/{id}/rollback` | 回滚 |
| GET | `/api/v2/sbom/host/{host_id}` | 主机 SBOM |
| POST | `/api/v2/sbom/import` | 导入扫描 |
| GET | `/api/v2/weak-passwords` | 弱口令列表 |
| POST | `/api/v2/weak-passwords/scan` | 触发探测 |
| GET | `/api/v2/insecure-configs` | 不安全配置列表 |
| POST | `/api/v2/image/scans` | 触发镜像扫描 |
| POST | `/api/v2/ci/image-scan` | CI 卡点（X-API-Key） |
| GET | `/api/v2/risks` | 5 类风险统一查询 |
| GET | `/api/v2/risks/stats` | 风险统计 |
| POST | `/api/v2/risks/{id}/lifecycle` | 状态机推进 |

---

## 20. 实施路线

### 20.1 当前进度（已完成）

| 项 | 状态 |
|----|------|
| advisory.Coordinator 多源融合 | ✅ |
| PURL + NEVRA 双索引 | ✅ |
| 3 级 confidence 仲裁 | ✅ |
| 11 source（含 4 信创 stub） | ✅ |
| CNNVD 官方 API | ✅ |
| CISA KEV / ExploitDB | ✅ |
| PriorityCalculator v1（4 维） | ✅ |
| Reconciler 三状态机 | ✅ |
| Targeted scan + 业务线 | ✅ |
| pre-check + 8 状态 | ✅ |
| RemediationExecutor 11 state | ✅ |
| image_scanner v0 | ✅ |
| sbom_import v0 | ✅ |

### 20.2 v2.0 落地（M4 阶段，2-3 月内）

| 项 | 章节 |
|----|------|
| VulnSync 服务化拆分 | [`vulnsync-design.md`](vulnsync-design.md) §19.2 |
| Kafka mxsec.vuln.advisory 推送 | §2.1 |
| Engine 订阅 advisory 触发匹配 | §2.2 |
| PriorityCalculator v2（6 维） | §6 |
| AssetCriticality 评级 | §6.4 |
| EPSS 同步 + 入优先级公式 | §6 |
| 信创 4 源真实接入 | §10 |
| 信创人工补录通道 | §10.5 |
| SBOM 导出 CycloneDX 1.5 | §8 |
| 弱口令探测器（4 类） | §9 |
| 不安全配置检测（6 类） | §14 |
| 镜像扫描 Trivy 集成 | §11 |
| CI/CD 卡点 API | §11.4 |
| 风险中心 5 类统一 | §16、§17.4 |

### 20.3 v2.1（M5 阶段，6 月内）

| 项 | 章节 |
|----|------|
| RemediationOrchestrator 批量灰度 | §7.3 |
| 自动 rollback（dnf history） | §7.2.4 |
| PoC 验证沙箱 + 50 条主流 CVE 规则 | §12 |
| 漏洞→进程→业务关联常态化 | §13 |
| CVSS 4.0 双轨 | §15 |
| SSVC 决策点 | §15.3 |

### 20.4 v2.2（M6 阶段，9 月内）

| 项 | 章节 |
|----|------|
| NPatch 虚拟补丁 eBPF | 详见 [`ref/06-漏洞.md`](../ref/06-漏洞.md) §5.1、M2 阶段 |
| RASP 集成 | 详见 [`ml-models.md`](ml-models.md) + Phase 4 |
| 镜像漏洞与运行时关联（Pod 影响） | §11.6 |
| 风险闭环 6 步生命周期 UI | §16 |

详细排期见 `ref/08-roadmap.md` M4-M6 阶段。

---

## 21. 与 vulnsync-design.md 的引用关系

| 主题 | 本文 | vulnsync-design.md |
|------|------|-------------------|
| advisory 仲裁算法 | §5 业务视角 | §3 工程实现 |
| 表结构 vuln_data_sources / advisory_sources / advisory_packages | §17.5 引用 | §7 完整定义 |
| Kafka mxsec.vuln.advisory schema | §2.1 概述 | §8 完整 schema |
| 信创 4 源 | §10 业务策略 | §2.2.12、§9 工程实现 |
| Leader Election | 不涉及 | §5 完整实现 |
| 多租户隔离 | §16 完整 | §1.2 概述 |
| 修复闭环 | §7 完整 | 不涉及 |
| SBOM | §8 完整 | 不涉及 |
| 弱口令 | §9 完整 | 不涉及 |
| 镜像扫描 | §11 完整 | 不涉及 |
| PoC 沙箱 | §12 完整 | 不涉及 |
| 漏洞×进程×业务 | §13 完整 | 不涉及 |
| 不安全配置 | §14 完整 | 不涉及 |
| CVSS 4.0 / SSVC | §15 完整 | 不涉及 |

> 两文档**严格不重复**。VulnSync 是漏洞模块的"情报后端"，本文是漏洞模块的"业务全生命周期"。

---

## 22. 参考文档

| 主题 | 文档 |
|------|------|
| 架构总图 | [`architecture.md`](architecture.md) §2.5 |
| 运行模式 | [`operating-modes.md`](operating-modes.md) |
| 多租户 | [`multi-tenant.md`](multi-tenant.md) |
| VulnSync 服务 | [`vulnsync-design.md`](vulnsync-design.md) |
| Engine 检测 | [`engine-detection-design.md`](engine-detection-design.md) |
| 资产模型 | [`asset-model.md`](asset-model.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| LLMProxy | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML | [`ml-models.md`](ml-models.md) |
| 路线图 | `ref/08-roadmap.md` |
| 商业对标 | `ref/06-漏洞.md` |
| 青藤白皮书 | `docs/主机安全-万相白皮书.docx` / `docs/容器安全-蜂巢白皮书.docx` |
| 现有源码 | `internal/server/manager/biz/vuln_*.go` / `advisory/` / `plugins/scanner/` / `plugins/remediation/` |
