# VulnSync 服务设计

> **服务定位**：mxsec 六微服务之一，专精**漏洞情报多源融合 + advisory 仲裁 + Kafka 推送**。
>
> VulnSync 不直接服务用户、不对接 Agent、不做实时检测，仅在后台做"情报抓取 + 数据治理 + 标准化输出"。它是 mxsec 漏洞链路（看清 → 算清 → 处清）的"看清"侧上游基座。
>
> **与 architecture.md §2.5 的强一致**：本文件是 [`architecture.md`](architecture.md) §2.5 VulnSync 的展开实现细节。如本文件与 `architecture.md` 冲突，以 `architecture.md` 为准。
>
> **关联文档**：
> - 上游运行哲学：[`operating-modes.md`](operating-modes.md)（VulnSync 自身无 mode，但其下游 Engine 受 mode 影响）
> - 多租户：[`multi-tenant.md`](multi-tenant.md)（VulnSync 输出的 advisory 是全局共享数据，主机匹配在下游按 tenant 隔离）
> - 漏洞业务模块：[`vuln-module-design.md`](vuln-module-design.md)（漏洞业务编排 + 修复闭环 + UI；VulnSync 是其情报后端）

---

## 1. 服务边界

### 1.1 定位

VulnSync 是 **CWPP 漏洞链路上游的"情报融合服务"**，单一职责：

```
+----------------------------------------------------------------+
|                    VulnSync 单一职责                            |
|                                                                  |
|   外部权威漏洞源 (15 个)                                         |
|        │                                                         |
|        │  Cron 抓取（增量 1h / 全量 1d）                         |
|        ↓                                                         |
|   解析 → 标准化 → 仲裁融合 (3 级 confidence)                    |
|        │                                                         |
|        ↓                                                         |
|   Kafka: mxsec.vuln.advisory                                    |
|        │                                                         |
|        ├──→ Consumer 持久化 → MySQL vulnerabilities             |
|        ├──→ Engine 主机指纹匹配 → host_vulnerabilities          |
|        └──→ Manager UI 漏洞中心读取                              |
+----------------------------------------------------------------+
```

### 1.2 必须做（强约束）

| 职责 | 说明 |
|------|------|
| 多源同步 | 15 个权威源（NVD/OSV/RHSA/USN/DSA/Alpine/SUSE/CISA KEV/ExploitDB/CNNVD/EPSS + 信创 4 源） |
| 增量 + 全量 | 默认 1h 增量、1d 全量，watermark 持久化 |
| advisory 仲裁 | PURL + NEVRA 双索引模型 + 3 级 confidence（high/medium/low） |
| 失败容错 | 单源失败不阻塞其他源；幂等重试；DLQ 隔离脏数据 |
| Kafka 推送 | 标准化 advisory schema 推 `mxsec.vuln.advisory` |
| Leader Election | 单 Leader 抓取，避免重复请求外部限流封禁 |
| 指标 | 每个源同步状态、count、duration、error_msg 入 Prometheus |
| 信创补全 | 双源订阅 + 离线缓存 + 人工补录通道 |

### 1.3 严禁做

| 反例 | 说明 |
|------|------|
| 业务 API | 用户查询漏洞走 Manager；VulnSync 仅暴露 `/internal/*` 给 Manager/Engine 调度 |
| Agent 通信 | 不与 Agent 直连；Agent 软件清单走 Consumer 落库后由 Engine 拉取 |
| 实时检测 | 不做主机×advisory 匹配（这是 Engine 的职责） |
| 告警生成 | 不产 alert（host_vulnerability 命中告警由 Engine 产） |
| 修复任务调度 | 不调度 remediation（这是 Manager 的职责） |
| MySQL 主写 | 不直接写 `vulnerabilities` 表（推 Kafka，由 Consumer 写） |

> **设计原则**：VulnSync 是"情报数据管道"，不是"业务服务"。任何业务逻辑都不应进入本服务。

---

## 2. 数据源接入（15 源）

### 2.1 总览

| # | 源 | 类型 | 置信度 | 协议 | 认证 | 限流策略 | 增量 | 数据量级 |
|---|-----|------|--------|------|------|----------|------|----------|
| 1 | NVD (NIST) | CVE 元数据 | low | REST JSON | API Key（可选） | 50 req/30s（无 key）/ 50 req/30s（有 key 但更稳） | startDate/endDate | ~250k CVE，每月新增 ~3k |
| 2 | OSV.dev | 语言包 PURL | medium | REST JSON | 无 | 无明确限流，保守 16 并发 | 按 PURL 查 | ~200k vuln，覆盖 18 个 ecosystem |
| 3 | Red Hat RHSA | OS advisory CSAF v2 | **high** | HTTPS | 无 | 无明确限流，UA 标识 + 8 并发 | skipAdvisoryIDs | ~50k advisory |
| 4 | Ubuntu USN | OS advisory | **high** | REST JSON | 无 | 无明确限流 | since cursor | ~8k advisory |
| 5 | Debian DSA/Tracker | OS advisory | **high** | JSON dump | 无 | 全量 dump 约 30MB | 全量重拉 + diff | ~5k DSA |
| 6 | Alpine secdb | OS advisory YAML | **high** | HTTPS YAML | 无 | 全量 dump | 全量重拉 | ~3k vuln |
| 7 | SUSE | OS advisory CSAF | **high** | REST JSON | 无 | 8 并发 | since cursor | ~20k advisory |
| 8 | CISA KEV | 在野利用 | enrichment | JSON | 无 | 1 req/day 足够 | 全量重拉 | ~1.2k KEV |
| 9 | ExploitDB | 公开 PoC/EXP | enrichment | CSV | 无 | 1 req/day | 全量 CSV | ~50k exploit |
| 10 | CNNVD | 国内编号 | enrichment | POST JSON | 无（Web UA 伪装） | 50/页，120s 超时容忍慢响应 | 分页 + CVE 反查 | ~350k CNNVD |
| 11 | EPSS (FIRST.org) | 利用概率 | enrichment | CSV daily | 无 | 1 req/day | 全量 daily CSV | ~250k CVE 评分 |
| 12 | openEuler CSA | 信创 OS | **high** | RSS + JSON | 无 | 无 | RSS 增量 | ~3k CSA |
| 13 | Anolis ANSA | 信创 OS | **high** | HTTPS HTML 解析 | 无 | 无 | 列表分页 | ~2k ANSA |
| 14 | Kylin KYSA | 信创 OS | **high** | HTTPS（需运维约定下载源） | 私有 token / 站点账号 | 无 | 全量 + 手工触发 | ~1.5k KYSA |
| 15 | UOS UOSEC | 信创 OS | **high** | RSS / 文档下载 | 无 | 无 | RSS + 离线补录 | ~1k UOSEC |

> **置信度等级在仲裁中是关键**：
> - `high`（OS 厂商 advisory）：包含 OS-specific 修复版本号（如 `1:3.5.5-1.el9_4`），可直接 NEVRA 比较
> - `medium`（OSV PURL）：精确到语言生态版本（如 `>= 1.0, < 1.5`），适合 npm/PyPI/Maven 等
> - `low`（NVD CPE）：仅用作 metadata 补全（描述/CVSS/CWE），不做主机匹配
> - `enrichment`（KEV/ExploitDB/CNNVD/EPSS）：按 CVE 维度对已入库 vuln 字段做增补，不产生新 CVE

### 2.2 每源接入细节

#### 2.2.1 NVD (National Vulnerability Database)

- **接入点**：`https://services.nvd.nist.gov/rest/json/cves/2.0`
- **认证**：可选 `NVD_API_KEY` Header，提速 6 倍
- **限流**：无 key 50 req/30s，加 key 后 50 req/30s 但请求成功率显著提升
- **增量策略**：按 `lastModStartDate` / `lastModEndDate` 取 14 天窗口（容忍 OSV 延迟）
- **失败处理**：429 退避 30s 重试 3 次；5xx 退避 60s 重试 5 次；超时不计入失败
- **数据用途**：CVE 描述、CVSS v3.1 base score、CWE、references；**不**做主机匹配（confidence=low）

#### 2.2.2 OSV.dev

- **接入点**：`POST /v1/querybatch`（PURL → vuln IDs） + `GET /v1/vulns/{id}`（详情）
- **认证**：无（匿名）
- **限流**：无明确文档，保守 16 并发 + 100/批
- **增量策略**：调用方按 `software.collected_at > since` 筛 PURL 子集
- **失败处理**：429 退避 5s 重试 3 次；OSS-Fuzz ID（`OSV-YYYY-N`）过滤
- **缓存**：`detailCache` 3 策略（None / PreferOnline / OfflineOnly），跨批共享单 vuln 详情

#### 2.2.3 Red Hat (RHSA via CSAF v2)

- **接入点**：`https://access.redhat.com/security/data/csaf/v2/advisories/`
- **认证**：无；UA 伪装为 `mxsec-vulnsync/1.0 (+contact)`
- **限流**：8 并发抓取（超过 12 会触发软封禁）
- **增量策略**：
  - 维护 `skipAdvisoryIDs` 集合（已入库 RHSA-ID 反查 `vulnerabilities.reference_url`）
  - 全量首跑：~5w 条 advisory × 60s timeout × 8 并发 ≈ 30min
  - 增量：仅 fetch 新 advisory detail
- **数据用途**：RHEL / Rocky / Alma 系所有主机的精确 NEVRA 修复版本

#### 2.2.4 Ubuntu USN

- **接入点**：`https://ubuntu.com/security/notices.json`
- **认证**：无
- **限流**：无；保守 4 并发
- **增量策略**：`?after=<usn-id>` 游标分页
- **数据用途**：Ubuntu 14.04+ / Debian 系（少量）

#### 2.2.5 Debian DSA/Tracker

- **接入点**：`https://security-tracker.debian.org/tracker/data/json` 全量 dump（~30MB）
- **认证**：无
- **限流**：1 req/h（自我节流，因是全量 dump）
- **增量策略**：本地 ETag + If-None-Match；diff 上次快照仅入新增/变更
- **失败处理**：dump 损坏 fallback 上次成功版本

#### 2.2.6 Alpine secdb

- **接入点**：`https://secdb.alpinelinux.org/v{branch}/main.yaml`（按 branch 拉）
- **认证**：无
- **数据用途**：Alpine 3.10+

#### 2.2.7 SUSE

- **接入点**：`https://ftp.suse.com/pub/projects/security/csaf/`（CSAF v2 dump 目录）
- **认证**：无
- **限流**：8 并发
- **数据用途**：SLES / openSUSE 主机

#### 2.2.8 CISA KEV

- **接入点**：`https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json`
- **认证**：无
- **限流**：1 req/day（数据集小，~1.2k）
- **增量策略**：全量重拉 + UPSERT；标记 `in_kev=true`
- **数据用途**：优先级评分 in_kev 标记

#### 2.2.9 ExploitDB

- **接入点**：`https://gitlab.com/exploit-database/exploitdb/-/raw/main/files_exploits.csv`
- **认证**：无
- **限流**：1 req/day
- **增量策略**：全量 CSV diff（按 EDB-ID）
- **数据用途**：标记 `has_exploit=true` + `exploit_ref`

#### 2.2.10 CNNVD

- **接入点**：`POST https://www.cnnvd.org.cn/web/homePage/cnnvdVulList`
- **认证**：无（需 Mozilla UA + Origin/Referer header 防 WAF）
- **限流**：50/页，120s 超时容忍慢响应
- **增量策略**：分页 + 与本地 `cnnvd_id IS NULL` 的 CVE 做反查（单次 5000 上限）
- **数据用途**：补 `cnnvd_id` 字段（国内合规要求）

#### 2.2.11 EPSS (FIRST.org)

- **接入点**：`https://epss.cyentia.com/epss_scores-current.csv.gz`
- **认证**：无
- **限流**：1 req/day
- **增量策略**：daily CSV 全量替换 `epss_scores` 表
- **许可证**：CC-BY-SA-4.0；仅 import 数值，不二次分发原始数据集
- **数据用途**：`vulnerabilities.epss_score` 字段，参与 priority_score 计算

#### 2.2.12 信创 4 源（openEuler / Anolis / Kylin / UOS）

**openEuler CSA**：
- 接入点：`https://repo.openeuler.org/security/data/csaf/`（CSAF dump 目录） + `https://gitee.com/openeuler/security-committee/raw/master/security-notice/`（RSS 补充）
- 增量：RSS 按时间 cursor + CSAF dump diff
- 失败处理：RSS 失败 fallback CSAF dump

**Anolis ANSA**：
- 接入点：`https://anas.openanolis.cn/api/sec/v1/cves/list` + 备用页面解析
- 增量：游标分页

**Kylin KYSA**：
- 接入点：`https://www.kylinos.cn/support/loophole.html`（无官方 API）
- 认证：可能需运维约定的下载站点账号（KA 客户提供）
- 增量：每天页面抓取 + 增量入库

**UOS UOSEC**：
- 接入点：`https://www.uniontech.com/support/cve_list.html`（无官方 API）
- 增量：RSS（如有）+ 文档下载

**信创共性策略**（详见 §9）：
- 双源订阅（官方 + 第三方 CERT 数据合作可选）
- 离线缓存（gitee/gitlab 镜像）
- 人工补录通道（运维通过 `/internal/v1/advisory/manual` 上传 YAML）

### 2.3 数据源管理表

VulnSync 启动时从 `vuln_data_sources` 表读取每个源的 enabled / API URL / API Key / watermark：

```sql
CREATE TABLE vuln_data_sources (
    id            INT PRIMARY KEY AUTO_INCREMENT,
    name          VARCHAR(50) UNIQUE NOT NULL,    -- nvd / osv / rhsa / usn / debian-tracker / alpine / suse /
                                                   -- cisa-kev / exploit-db / cnnvd / epss /
                                                   -- openeuler / anolis / kylin / uos
    display_name  VARCHAR(100) NOT NULL,
    enabled       TINYINT(1) NOT NULL DEFAULT 1,
    api_url       VARCHAR(512),
    api_key       VARCHAR(255),                   -- AES-GCM 加密存储
    confidence    VARCHAR(10) NOT NULL,           -- high / medium / low / enrichment
    watermark     DATETIME,                       -- 上次成功 advisory.issued_at 最大值
    last_status   VARCHAR(20),                    -- success / failed / running
    last_count    BIGINT DEFAULT 0,
    last_error    VARCHAR(500),
    last_duration_ms INT DEFAULT 0,
    last_run_at   DATETIME,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);
```

> 复用现有 `internal/server/manager/biz/vuln_data_source_service.go` 的接口契约（`EnabledChecker`），VulnSync 通过 gRPC 调 Manager 读取或直接读 DB（首选直接读，避免 Manager 上行依赖）。

---

## 3. advisory 融合算法

### 3.1 双索引模型

mxsec 的核心创新是**同时维护两个索引视角**：

```
+--------------------------------------------------------+
|                     advisory 双索引                     |
|                                                          |
|   索引 A (NEVRA 视角)：OS 包维度                         |
|     KEY = (OS Family, OS Major, Pkg Name, Arch)         |
|     VALUE = [Advisory{FixedVer, AdvisoryID, CVE IDs}]   |
|     用途: rpm / dpkg / apk 精确版本比较                  |
|                                                          |
|   索引 B (PURL 视角)：语言生态维度                       |
|     KEY = pkg:type/namespace/name@version               |
|     VALUE = [Advisory{Ecosystem, AffectedRange}]        |
|     用途: npm / PyPI / Maven / Go / RubyGems / ...      |
|                                                          |
|   双索引在 advisory.AffectedPkgs 中合并存储              |
|   匹配 gate 互斥：                                       |
|     - OS pkg → 走索引 A（OSFamily/OSMajor 强校验）       |
|     - 语言包 → 走索引 B（Ecosystem 强校验）             |
+--------------------------------------------------------+
```

### 3.2 三级 confidence 仲裁

```
+-------------------------------------------------------------+
|        同一 CVE 多源出现时的优先级裁决                       |
|                                                               |
|   high       (RHSA / USN / DSA / Alpine / SUSE / 信创)       |
|     ↑        ← OS 厂商 advisory，含精确 NEVRA 修复版本        |
|     │                                                         |
|   medium     (OSV / GHSA / PyPA / govulndb)                  |
|     ↑        ← PURL 维度精确，语言生态修复版本               |
|     │                                                         |
|   low        (NVD CPE)                                       |
|     ↑        ← 仅作 metadata 补全，不做主机匹配               |
|     │                                                         |
|   enrichment (KEV / ExploitDB / CNNVD / EPSS)                |
|              ← 按 CVE 维度补字段，不产生新 vuln              |
+-------------------------------------------------------------+

仲裁规则：
1. metadata 字段 (description/cvss/severity/cwe)：
   严格更高 confidence 时覆盖；同级保留先入者
2. 主机关联 (affected_hosts)：
   所有 source 并集去重（关键：RHSA 命中 rhel9 + Rocky 命中 rocky9
   各自不同 host，必须并集而非择一）
3. ecosystem 字段保护：
   若 cve 已被 OSV/PURL 路径写过（DB 中已有 PURL 前缀），
   OS source 不覆盖 source/component/purl 等 ecosystem 字段
4. 描述污染防御：
   advisory.Description 含 "Microsoft Windows" 且 OS 是 Linux 系 → 拒入库
```

### 3.3 入库前 validate gate

```go
func validateAdvisory(adv *Advisory) bool {
    if adv == nil || len(adv.CVEIDs) == 0 { return false }
    if len(adv.AffectedPkgs) == 0 { return false }
    for _, p := range adv.AffectedPkgs {
        if p.Name == "" || p.FixedVersion == "" { return false }
    }
    // OS gate 与 Ecosystem gate 二选一
    if adv.OSFamily == "" && adv.Ecosystem == "" {
        return false // 无法做主机过滤
    }
    // 防 OS-mismatch 漏网
    if isLinuxOS(adv.OSFamily) && containsCI(adv.Description, "Microsoft Windows") {
        return false
    }
    return true
}
```

### 3.4 NEVRA 比较算法选择

```go
// pkgManagerFromType 决定 matcher 用哪种版本比较算法
func pkgManagerFromType(pkgType, osFamily string) string {
    switch pkgType {
    case "rpm":  return "rpm"     // CompareRPMVersion
    case "deb":  return "dpkg"    // CompareDpkgVersion
    case "apk":  return "apk"     // CompareAPKVersion
    }
    switch osFamily {
    case "ubuntu", "debian":  return "dpkg"
    case "alpine":            return "apk"
    }
    return "rpm" // RHEL / Rocky / CentOS / 信创默认 rpm
}
```

> 实现已落地：`internal/server/manager/biz/advisory/dpkg_vercmp.go`、`matcher.go` 共 700+ 行。VulnSync 服务化时直接复用同一个 `advisory` 包，不再复制实现。

---

## 4. 输入输出

### 4.1 输入

**触发源**：

| 触发方式 | 频次 | 用途 |
|----------|------|------|
| Cron 增量同步 | 每 1h | 拉取各源 `since=watermark` 的新 advisory |
| Cron 全量同步 | 每天 03:30（避开峰值） | 全量重拉 + advisory_packages 重建 |
| gRPC `/internal/v1/sync/trigger` | 按需 | Manager UI "立即同步" 按钮触发 |
| gRPC `/internal/v1/sync/source` | 按需 | 单源重跑（运维排错） |
| Webhook（信创人工补录） | 按需 | 运维 YAML 上传补漏 |

**配置文件**：`/etc/mxsec/vulnsync.yaml`

```yaml
vulnsync:
  # 实例标识，用于 Leader Election Key
  instance_id: "vulnsync-1"

  # Leader 选举
  leader_election:
    backend: "redis"                  # redis | etcd
    redis_addr: "redis-master:6379"
    redis_db: 0
    lock_key: "mxsec:vulnsync:leader"
    lock_ttl: "30m"                   # leader 心跳间隔 10m，TTL 30m 容忍 1 次心跳丢失
    heartbeat_interval: "10m"

  # 全局调度
  schedule:
    incremental_cron: "0 0 * * * *"   # 每 1h
    full_cron:        "0 30 3 * * *"  # 每日 03:30
    max_parallel_sources: 4           # 不同 source 之间最大并行度

  # 数据源（覆盖 vuln_data_sources 表的运行时配置）
  sources:
    nvd:
      enabled: true
      api_key: "${NVD_API_KEY}"
      window_days: 14
      concurrency: 4
      timeout: "60s"
    osv:
      enabled: true
      batch_size: 100
      concurrency: 16
      timeout: "90s"
    rhsa:
      enabled: true
      concurrency: 8
      timeout: "2h"
      ua: "mxsec-vulnsync/1.0"
    usn: { enabled: true, concurrency: 4 }
    debian-tracker: { enabled: true, dump_etag_cache: "/var/lib/mxsec/vulnsync/cache/debian.etag" }
    alpine: { enabled: true }
    suse: { enabled: true, concurrency: 8 }
    cisa-kev: { enabled: true }
    exploit-db: { enabled: true }
    cnnvd:
      enabled: true
      api_url: "https://www.cnnvd.org.cn/web/homePage/cnnvdVulList"
      max_pages_per_run: 200
      max_cve_per_run: 5000
    epss:
      enabled: true
      url: "https://epss.cyentia.com/epss_scores-current.csv.gz"
    openeuler: { enabled: true, mirror: "https://gitee.com/openeuler/security-committee" }
    anolis: { enabled: true }
    kylin: { enabled: true, offline_cache: "/var/lib/mxsec/vulnsync/cache/kylin" }
    uos: { enabled: true, offline_cache: "/var/lib/mxsec/vulnsync/cache/uos" }

  # 数据库（直接读 vuln_data_sources / 写 advisory_packages 备份；主写仍走 Kafka）
  db:
    dsn: "mxsec:${DB_PASS}@tcp(mysql:3306)/mxsec?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"
    max_open_conns: 20
    max_idle_conns: 5

  # Kafka 生产者
  kafka:
    brokers: ["kafka-1:9092", "kafka-2:9092", "kafka-3:9092"]
    topic_advisory: "mxsec.vuln.advisory"
    topic_dlq:      "mxsec.vuln.advisory.dlq"
    acks: "all"
    compression: "snappy"
    max_message_bytes: 10485760       # 10MB（CSAF 单 advisory 可达数百 KB）
    flush_messages: 100
    flush_frequency: "1s"

  # 缓存
  cache:
    redis_addr: "redis-master:6379"
    osv_detail_ttl: "30d"             # OSV vuln detail 离线缓存
    rhsa_csaf_ttl: "90d"

  # 观测
  observability:
    log_level: "info"
    prom_addr: ":9090"
    pprof_addr: ":6060"
    health_addr: ":8080"

  # 失败熔断
  circuit_breaker:
    consecutive_failures: 5            # 单源连续失败 5 次进入 5min 熔断
    cooldown: "5m"
```

### 4.2 输出

**主输出**：Kafka topic `mxsec.vuln.advisory`

| 维度 | 配置 |
|------|------|
| Topic | `mxsec.vuln.advisory` |
| Partitions | 6 |
| Partition Key | `cve_id`（保证同 CVE 多 advisory 串行到同 partition） |
| Retention | 30 天 |
| Compression | Snappy |
| Replication | 2，min.insync.replicas=1 |

**消息 schema**（详见 §8）

**Health/Metrics 端口**：

| 端口 | 用途 |
|------|------|
| `:8080/healthz` | 健康检查（GET） |
| `:8080/readyz`  | 是否 Leader + Kafka 就绪 |
| `:9090/metrics` | Prometheus 抓取 |
| `:6060/debug/pprof/*` | 性能调试 |

---

## 5. Leader Election（单 Leader 抓取）

### 5.1 为什么需要

- 外部源（尤其 NVD/RHSA/CNNVD）对 IP 维度限流 / 封禁；多副本同时抓 = 触发软封禁
- 全量 dump（Debian/Alpine/EPSS）数据相同，多副本浪费带宽 + 数据库 UPSERT 重复
- 信创源限流极严，多副本 100% 触发封禁

### 5.2 实现方式

**首选 Redis SET NX**（与 mxsec 现有 Redis SD 同基础设施）：

```go
// internal/server/vulnsync/leader/redis.go
type RedisLeader struct {
    client   *redis.Client
    key      string                  // mxsec:vulnsync:leader
    value    string                  // hostname-pid-startTime
    ttl      time.Duration           // 30m
    interval time.Duration           // 10m heartbeat
    onLeader func()                  // 成为 leader 触发 Cron
    onLost   func()                  // 失去 leader 停止 Cron
}

func (l *RedisLeader) Run(ctx context.Context) {
    ticker := time.NewTicker(l.interval)
    defer ticker.Stop()

    isLeader := false
    for {
        select {
        case <-ctx.Done():
            if isLeader {
                _ = l.release()
            }
            return
        case <-ticker.C:
            ok, err := l.tryAcquireOrRefresh()
            if err != nil {
                // Redis 不可用：保持当前状态，下次重试
                continue
            }
            if ok && !isLeader {
                isLeader = true
                l.onLeader()
            } else if !ok && isLeader {
                isLeader = false
                l.onLost()
            }
        }
    }
}

func (l *RedisLeader) tryAcquireOrRefresh() (bool, error) {
    // 用 SET NX EX 原子获取
    setOK, err := l.client.SetNX(context.Background(), l.key, l.value, l.ttl).Result()
    if err != nil {
        return false, err
    }
    if setOK {
        return true, nil
    }
    // 已有持有者：检查是否是自己
    cur, err := l.client.Get(context.Background(), l.key).Result()
    if err == redis.Nil {
        // 刚好过期，下次重试
        return false, nil
    }
    if err != nil {
        return false, err
    }
    if cur == l.value {
        // 自己持有，续期
        _, err := l.client.Expire(context.Background(), l.key, l.ttl).Result()
        return err == nil, err
    }
    return false, nil
}

func (l *RedisLeader) release() error {
    // 仅当 value 匹配时才删（防误删别人的锁）
    script := `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
    _, err := l.client.Eval(context.Background(), script, []string{l.key}, l.value).Result()
    return err
}
```

### 5.3 部署形态

| 部署 | 副本 | 行为 |
|------|------|------|
| Demo / 小规模 | 1 副本 | 直接是 Leader，无需选举 |
| 标准 / 中规模 | 2-3 副本 | 1 个 Leader 抓取，其余 follower 仅响应 health/readyz |
| 极限 | 不增副本 | VulnSync 是 CPU/带宽轻负载，单 Leader 足够；如需提速分 source 跑独立服务 |

Follower 行为：
- 注册 `/healthz` 返回 200，`/readyz` 返回 200 + `is_leader: false`
- Cron 任务不执行
- Kafka producer 不连接
- 仅做 metric 上报（`mxsec_vulnsync_is_leader{instance} 0`）

---

## 6. Go 接口骨架

### 6.1 包结构

```
internal/server/vulnsync/
├── main.go                      // cmd/server/vulnsync/main.go 调度入口
├── config/
│   └── config.go                // viper 加载 vulnsync.yaml
├── leader/
│   └── redis.go                 // Redis SET NX Leader
├── source/                       // 复用 internal/server/manager/biz/advisory/ 包
│   └── adapter.go               // 适配 EnabledChecker / cache
├── fetcher/
│   ├── fetcher.go               // Fetcher 调度器
│   ├── runner_incremental.go    // 增量任务
│   └── runner_full.go           // 全量任务
├── merger/
│   └── merger.go                // 复用 advisory.Coordinator 仲裁
├── publisher/
│   ├── kafka_publisher.go       // Kafka 推送
│   ├── retry.go                 // 失败重试 + DLQ
│   └── schema.go                // 消息 schema
├── manual/
│   └── grpc_server.go           // /internal/v1/* gRPC API（人工补录 + 触发同步）
└── observability/
    ├── metrics.go               // Prometheus
    └── health.go                // /healthz /readyz
```

### 6.2 核心接口

```go
// Package vulnsync 是 mxsec 漏洞情报融合服务的服务化入口。
//
// 设计原则:
//   - 复用 internal/server/manager/biz/advisory 包（不复制实现）
//   - Source / Fetcher / Merger / Publisher 四层解耦
//   - Leader Election 单副本抓取
//   - 所有 advisory 出口经 Publisher 推 Kafka，不直接写 MySQL
package vulnsync

import (
    "context"
    "time"

    "github.com/imkerbos/mxsec-platform/internal/server/manager/biz/advisory"
)

// Source 单个权威漏洞数据源契约。
//
// 复用 advisory.Source（已落地 RHSA/USN/DSA/Alpine/OSV/CentOS/Rocky/
// openEuler/Anolis/Kylin/UOS 共 11 源），新增 NVD/SUSE/EPSS/CNNVD/CISA-KEV/
// ExploitDB 通过 advisory.Source 接口扩展即可。
type Source = advisory.Source

// PURLSource 是 Source 的扩展，按 PURL 批量查询能力的源（OSV/未来 GHSA/Snyk）。
type PURLSource = advisory.PURLSource

// Fetcher 单源抓取调度器。
//
// 职责:
//  1. 读 vuln_data_sources.watermark 决定 since
//  2. 调 Source.Fetch（或 PURLSource.FetchByPURLs）
//  3. 推进 watermark
//  4. 失败时进入熔断 + 指标上报
type Fetcher interface {
    // RunOne 执行单源单次抓取（增量或全量由 since 决定）
    RunOne(ctx context.Context, src Source, since time.Time) (*FetchResult, error)

    // RunAll 并行执行所有 enabled 源
    RunAll(ctx context.Context, mode FetchMode) ([]*FetchResult, error)
}

// FetchMode 抓取模式。
type FetchMode string

const (
    FetchModeIncremental FetchMode = "incremental" // 按 watermark
    FetchModeFull        FetchMode = "full"        // 全量重拉
)

// FetchResult 单源抓取结果。
type FetchResult struct {
    SourceName    string
    AdvisoryCount int
    Duration      time.Duration
    Watermark     time.Time           // 新 watermark
    Advisories    []*advisory.Advisory
    Error         error
}

// Merger advisory 仲裁融合器。
//
// 输入: 多源 advisory 列表
// 输出: 按 CVE 去重 + confidence 仲裁后的 normalized advisory
//
// 复用 advisory.Coordinator.mergeByConfidence（已落地）
type Merger interface {
    Merge(ctx context.Context, raw []*advisory.Advisory) ([]*NormalizedAdvisory, error)
}

// NormalizedAdvisory 推往 Kafka 的标准化 advisory。
type NormalizedAdvisory struct {
    EventID      string                // UUID v4
    EventType    string                // advisory_upsert | advisory_delete
    SchemaVer    string                // "v1"
    EmittedAt    time.Time
    CVEID        string                // 主键
    OSVID        string                // 可选
    CNNVDID      string                // 可选
    AdvisoryIDs  []string              // 上游 advisory ID 列表（RHSA-xx / USN-xx）
    Source       string                // 选定的主 source（confidence 最高者）
    Confidence   string                // high / medium / low
    Severity     string
    CVSSScore    float64
    CVSSVector   string
    EPSS         float64
    InKEV        bool
    HasExploit   bool
    ExploitRef   string
    CWE          string
    Description  string
    ReferenceURL string
    IssuedAt     time.Time
    UpdatedAt    time.Time
    AffectedPkgs []AffectedPkg         // 详见 §8
    PURL         string                // 语言包 advisory
    Ecosystem    string
    AttackVector string
    VulnType     string
}

// AffectedPkg 受影响包条目（多 source 合并后的最高 confidence 版本）。
type AffectedPkg struct {
    OSFamily     string
    OSMajor      string
    Ecosystem    string
    PkgName      string
    Arch         string
    FixedVersion string
    Confidence   string
    SourceAdvID  string                // 该 fix 来源 advisory ID
}

// Publisher Kafka 推送器。
//
// 职责:
//  1. 按 cve_id 做 partition key
//  2. 异步 batch + linger
//  3. 失败重试 3 次（指数退避），仍失败入 DLQ
//  4. acks=all 保证强一致
type Publisher interface {
    Publish(ctx context.Context, adv *NormalizedAdvisory) error
    PublishBatch(ctx context.Context, advs []*NormalizedAdvisory) (success, failed int, err error)
    Close() error
}

// Service 顶层调度器（main.go 持有）。
type Service interface {
    Start(ctx context.Context) error
    Stop() error
    IsLeader() bool
}

// 工厂
func NewService(cfg *Config) (Service, error)
func NewFetcher(cfg *Config, sources []Source) Fetcher
func NewMerger() Merger
func NewKafkaPublisher(cfg *KafkaConfig) (Publisher, error)
```

### 6.3 main.go 调度骨架

```go
// cmd/server/vulnsync/main.go
package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "go.uber.org/zap"

    "github.com/imkerbos/mxsec-platform/internal/server/vulnsync"
    "github.com/imkerbos/mxsec-platform/internal/server/vulnsync/config"
    "github.com/imkerbos/mxsec-platform/internal/server/vulnsync/leader"
)

func main() {
    cfg, err := config.Load("/etc/mxsec/vulnsync.yaml")
    if err != nil { panic(err) }

    logger, _ := zap.NewProduction()
    defer logger.Sync()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    svc, err := vulnsync.NewService(cfg)
    if err != nil { logger.Fatal("init service", zap.Error(err)) }

    // Leader Election
    leaderClient := leader.NewRedis(cfg.LeaderElection, leader.Hooks{
        OnLeader: func() {
            logger.Info("成为 Leader，启动 Cron")
            _ = svc.Start(ctx)
        },
        OnLost: func() {
            logger.Warn("失去 Leader，停止 Cron")
            _ = svc.Stop()
        },
    })
    go leaderClient.Run(ctx)

    // 信号处理
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    logger.Info("收到退出信号，graceful shutdown")
    cancel()
    _ = svc.Stop()
}
```

---

## 7. 表结构

VulnSync 自身**不主写**业务表（写入由 Consumer 通过 Kafka 消费完成），但为运行需要维护以下"状态/缓存"类表（直接读写）：

### 7.1 vuln_data_sources（源管理表）

已在 §2.3 列出。

### 7.2 vulnerabilities（业务主表，Consumer 写）

```sql
CREATE TABLE vulnerabilities (
    id              BIGINT PRIMARY KEY AUTO_INCREMENT,
    cve_id          VARCHAR(50)  UNIQUE NOT NULL,
    osv_id          VARCHAR(100) INDEX,
    cnnvd_id        VARCHAR(50)  INDEX,
    cnvd_id         VARCHAR(50)  INDEX,
    purl            VARCHAR(500) INDEX,
    severity        VARCHAR(20)  NOT NULL,
    cvss_score      DECIMAL(4,1) DEFAULT 0,
    cvss_vector     VARCHAR(200),
    epss_score      DECIMAL(5,4) DEFAULT 0,
    in_kev          TINYINT(1) DEFAULT 0,
    has_exploit     TINYINT(1) DEFAULT 0,
    exploit_ref     VARCHAR(500),
    cwe_id          VARCHAR(200),
    component       VARCHAR(200),
    description     TEXT,
    current_version VARCHAR(100),
    fixed_version   VARCHAR(100),
    affected_versions VARCHAR(500),
    reference_url   VARCHAR(500),
    attack_vector   VARCHAR(20),       -- network / adjacent / local / physical
    vuln_type       VARCHAR(30),       -- rce / lpe / sqli / ...
    source          VARCHAR(20),       -- 选定主 source
    patch_available TINYINT(1) DEFAULT 0,
    confidence      VARCHAR(10) DEFAULT 'low' INDEX,
    status          VARCHAR(20)  NOT NULL DEFAULT 'unpatched',
    discovered_at   TIMESTAMP,
    patched_at      TIMESTAMP NULL,
    priority_score  DECIMAL(5,3) DEFAULT 0 INDEX,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMP NULL INDEX
);
```

### 7.3 advisory_sources（多源 advisory 落点表）

```sql
CREATE TABLE advisory_sources (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    source            VARCHAR(20)  NOT NULL,   -- rhsa / usn / debian-tracker / osv / ...
    source_advisory_id VARCHAR(100) NOT NULL,  -- RHSA-2024:1234 / USN-7890-1
    confidence        VARCHAR(10)  NOT NULL,
    severity          VARCHAR(20),
    cvss_score        DECIMAL(4,1) DEFAULT 0,
    description       TEXT,
    reference_url     VARCHAR(500),
    issued_at         TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    raw_json          MEDIUMTEXT,              -- 原始 CSAF / JSON（debug & 重建）
    UNIQUE KEY uk_source_id (source, source_advisory_id)
);

CREATE TABLE advisory_packages (
    id                  BIGINT PRIMARY KEY AUTO_INCREMENT,
    cve_id              VARCHAR(50)  NOT NULL,
    source              VARCHAR(20)  NOT NULL,
    source_advisory_id  VARCHAR(100) NOT NULL,
    os_family           VARCHAR(20),
    os_major            VARCHAR(10),
    ecosystem           VARCHAR(30),
    pkg_name            VARCHAR(200) NOT NULL,
    arch                VARCHAR(20),
    fixed_version       VARCHAR(200) NOT NULL,
    confidence          VARCHAR(10)  NOT NULL,
    severity            VARCHAR(20),
    issued_at           TIMESTAMP NULL,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_cve_src_os_pkg_arch (cve_id, source, os_family, os_major, pkg_name, arch),
    KEY idx_cve (cve_id),
    KEY idx_os_pkg (os_family, os_major, pkg_name)
);
```

> `advisory_packages` 是仲裁的真值表：单个 CVE 在不同 OS / source 下的修复版本不同（如 OpenSSL CVE 在 RHEL 9.4 vs Ubuntu 22.04 修复版本号完全不同），必须按 (OS, source, pkg, arch) 维度存储。

### 7.4 advisory_fingerprints（去重指纹）

```sql
-- 用于 advisory 上游变更检测：相同 fingerprint 跳过下游处理
CREATE TABLE advisory_fingerprints (
    source            VARCHAR(20)  NOT NULL,
    source_advisory_id VARCHAR(100) NOT NULL,
    content_hash      CHAR(64)     NOT NULL,   -- sha256 of normalized JSON
    last_seen_at      TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    last_published_at TIMESTAMP NULL,           -- 上次推 Kafka 的时间
    PRIMARY KEY (source, source_advisory_id),
    KEY idx_hash (content_hash)
);
```

### 7.5 epss_scores（EPSS 评分表）

```sql
CREATE TABLE epss_scores (
    cve_id      VARCHAR(50) PRIMARY KEY,
    epss        DECIMAL(7,6) NOT NULL,         -- 0.000000-1.000000
    percentile  DECIMAL(7,6) NOT NULL,
    date        DATE NOT NULL,
    KEY idx_date (date),
    KEY idx_epss (epss)
);
```

每天 EPSS 同步后，VulnSync 把 epss 值合入 advisory 推 Kafka，由 Consumer 回写 `vulnerabilities.epss_score`。

### 7.6 信创人工补录表

```sql
CREATE TABLE manual_advisories (
    id           BIGINT PRIMARY KEY AUTO_INCREMENT,
    cve_id       VARCHAR(50)  NOT NULL,
    os_family    VARCHAR(20)  NOT NULL,        -- openeuler / anolis / kylin / uos
    os_major     VARCHAR(10),
    pkg_name     VARCHAR(200) NOT NULL,
    fixed_version VARCHAR(200) NOT NULL,
    severity     VARCHAR(20),
    cvss_score   DECIMAL(4,1) DEFAULT 0,
    reference_url VARCHAR(500),
    description  TEXT,
    submitted_by VARCHAR(64)  NOT NULL,         -- operator user id
    submitted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    approved_by  VARCHAR(64),
    approved_at  TIMESTAMP NULL,
    status       VARCHAR(20) DEFAULT 'pending', -- pending / approved / published / rejected
    KEY idx_status (status)
);
```

---

## 8. Kafka 消息 schema

### 8.1 Topic 元数据

```yaml
topic: mxsec.vuln.advisory
partitions: 6
replication: 2
min_isr: 1
retention_ms: 2592000000             # 30d
compression: snappy
key: cve_id                          # 同 CVE 串行到同 partition
```

### 8.2 消息体（JSON）

```json
{
  "event_id": "ev-9c3a1f2e-...",
  "event_type": "advisory_upsert",
  "schema_ver": "v1",
  "emitted_at": "2026-06-06T03:35:21Z",
  "data_type": 12001,

  "cve_id": "CVE-2024-12345",
  "osv_id": "GHSA-abcd-efgh-1234",
  "cnnvd_id": "CNNVD-202405-1234",
  "cnvd_id": "",
  "advisory_ids": ["RHSA-2024:1234", "USN-7890-1", "DSA-5678-1"],

  "source": "rhsa",
  "confidence": "high",
  "severity": "high",
  "cvss_score": 8.8,
  "cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:U/C:H/I:H/A:H",
  "epss_score": 0.0421,
  "in_kev": false,
  "has_exploit": true,
  "exploit_ref": "https://www.exploit-db.com/exploits/52345",
  "cwe_id": "CWE-89",

  "description": "SQL injection in foo-bar 2.x ...",
  "reference_url": "https://access.redhat.com/errata/RHSA-2024:1234",
  "issued_at": "2026-05-20T00:00:00Z",
  "updated_at": "2026-06-05T12:00:00Z",

  "attack_vector": "network",
  "vuln_type": "sqli",
  "patch_available": true,

  "affected_packages": [
    {
      "os_family": "rhel",
      "os_major": "9",
      "ecosystem": "",
      "pkg_name": "openssl-libs",
      "arch": "x86_64",
      "fixed_version": "1:3.5.5-1.el9_4",
      "confidence": "high",
      "source_advisory_id": "RHSA-2024:1234"
    },
    {
      "os_family": "ubuntu",
      "os_major": "22",
      "ecosystem": "",
      "pkg_name": "libssl3",
      "arch": "amd64",
      "fixed_version": "3.0.2-0ubuntu1.15",
      "confidence": "high",
      "source_advisory_id": "USN-7890-1"
    },
    {
      "os_family": "",
      "os_major": "",
      "ecosystem": "Maven",
      "pkg_name": "io.netty:netty-codec",
      "arch": "",
      "fixed_version": "4.1.115.Final",
      "confidence": "medium",
      "source_advisory_id": "GHSA-abcd-efgh-1234"
    }
  ],

  "purl": "pkg:maven/io.netty/netty-codec@4.1.110.Final",
  "ecosystem": "Maven",
  "affected_versions": ">= 4.0.0, < 4.1.115.Final"
}
```

### 8.3 DataType 分配

| DataType | 用途 | Topic |
|----------|------|-------|
| 12001 | advisory_upsert（新增/更新） | mxsec.vuln.advisory |
| 12002 | advisory_delete（撤回/废弃） | mxsec.vuln.advisory |
| 12003 | advisory_recall（误报回撤） | mxsec.vuln.advisory |
| 12010 | enrichment_kev（KEV 标记） | mxsec.vuln.advisory |
| 12011 | enrichment_epss（EPSS 评分） | mxsec.vuln.advisory |
| 12012 | enrichment_cnnvd（CNNVD 编号） | mxsec.vuln.advisory |

详见 [`datatype-allocation.md`](datatype-allocation.md) 12000-12099 段。

### 8.4 DLQ

| Topic | 触发条件 | 处理 |
|-------|----------|------|
| `mxsec.vuln.advisory.dlq` | acks=all 超时 3 次 / 序列化失败 / message > 10MB | 写本地文件 + 7 天保留，运维 CLI 重放 |

DLQ 消息额外字段：

```json
{
  "original_event_id": "...",
  "failure_reason": "kafka write timeout",
  "retry_count": 3,
  "failed_at": "2026-06-06T03:35:25Z",
  "original_payload_b64": "..."
}
```

---

## 9. 信创源稳定性策略

信创 OS 数据源的稳定性是商业化关键风险（详见 `ref/06-漏洞.md` §4 P0-3 / `ref/06-漏洞.md` §7.2）。VulnSync 设计了三层防御：

### 9.1 双源订阅

每个信创源至少 2 个数据获取通道：

| OS | 主源 | 备源 | 离线源 |
|----|------|------|--------|
| openEuler | `repo.openeuler.org/security/data/csaf/` | `gitee.com/openeuler/security-committee` RSS | gitee mirror clone |
| Anolis | `anas.openanolis.cn/api/sec/v1/cves/list` | `anas.openanolis.cn` 列表页 HTML 解析 | 站点抓取归档 |
| Kylin | `kylinos.cn/support/loophole.html` | 第三方 CERT 数据合作（漏洞盒子/奇安信） | 内网镜像（KA 客户提供） |
| UOS | `uniontech.com/support/cve_list.html` | UOS 官方下载站 RSS | 内网镜像 |

主源失败 → 自动降级到备源；备源失败 → 报警 + 加载离线缓存最近一次成功数据。

### 9.2 离线缓存

```
/var/lib/mxsec/vulnsync/cache/
├── kylin/
│   ├── snapshot-20260601.json
│   ├── snapshot-20260602.json
│   └── latest -> snapshot-20260606.json
├── uos/
└── openeuler/
```

- 每次成功同步后保存快照
- 主/备源全失败时加载 `latest` 快照（最多容忍 7 天）
- 超过 7 天且未恢复 → 健康检查 `/readyz` 返回 503 触发告警

### 9.3 人工补录通道

运维通过 gRPC 上传 YAML 补漏（覆盖 CVE 紧急公告）：

```yaml
# /etc/mxsec/manual-advisories/kylin-cve-2024-7890.yaml
cve_id: CVE-2024-7890
os_family: kylin
os_major: V10
pkg_name: openssh-server
fixed_version: "9.6p1-2.ky10"
severity: critical
cvss_score: 9.8
reference_url: https://www.kylinos.cn/support/loophole/detail/7890
description: "kylin V10 openssh-server pre-auth RCE，建议立即升级"
submitted_by: ops-001
```

gRPC API（详见 §11）：
- `POST /internal/v1/advisory/manual` → 写 `manual_advisories` 表 status=pending
- `POST /internal/v1/advisory/manual/{id}/approve` → status=approved，下次 cron 推 Kafka
- `GET  /internal/v1/advisory/manual` → 列出待审

### 9.4 监控告警

| 指标 | 阈值 | 告警 |
|------|------|------|
| `mxsec_vulnsync_source_last_success_age_seconds{source="kylin"}` | > 86400 (24h) | warning |
| 同上 | > 604800 (7d) | critical + 自动跳过该源 |
| `mxsec_vulnsync_offline_cache_used{source}` | == 1 | warning |
| `mxsec_vulnsync_manual_pending_count` | > 10 | warning（运维积压） |

---

## 10. 与 Engine 的交互

Engine 是 advisory 的下游消费者，通过 ConsumerGroup B 订阅 `mxsec.vuln.advisory`：

```
+----------------------+              +-----------------------+
|     VulnSync         | ── advisory ─►|  Engine              |
|  (15 source 同步)    |   via Kafka  |  ConsumerGroup B      |
|                      |              |                       |
|  推:                 |              |  收到 advisory_upsert: |
|   - cve_id          |              |   1. 查 software 表    |
|   - affected_pkgs   |              |      (host + pkg + ver)|
|   - confidence      |              |   2. matcher.Match()   |
|   - severity / cvss  |              |   3. 命中 → 推         |
|   - epss / kev / ... |              |      mxsec.engine.alert|
+----------------------+              |   4. 进 storyline 关联 |
                                       +-----------------------+
```

### 10.1 Engine 订阅契约

- ConsumerGroup: `mxsec-engine`
- Topic: `mxsec.vuln.advisory`
- Offset commit: 显式（处理完成后才提交）
- 失败处理: 单消息处理失败 → 重试 3 次 → 进 `mxsec.engine.advisory.dlq`

### 10.2 匹配触发

Engine 收到 advisory 后的检测流程：

```go
// internal/server/engine/vuln_matcher.go（伪代码）
func (e *Engine) onAdvisory(ctx context.Context, adv *NormalizedAdvisory) error {
    // 1. 解 affected_pkgs，按 OS / Ecosystem 拆 query
    queries := buildSoftwareQueries(adv.AffectedPkgs)

    // 2. 查 software 表（含 tenant 隔离）
    var hits []HostHit
    for _, q := range queries {
        rows, _ := e.db.Query(softwareLookupSQL, q...)
        hits = append(hits, rows...)
    }

    // 3. 精确 NEVRA / SemVer 比较
    affected := matcher.Compare(adv, hits)

    // 4. 产 host_vulnerability 告警（按 mode 决定是否下处置；这里只产 alert）
    for _, ah := range affected {
        e.emitAlert(ctx, &Alert{
            TenantID: ah.TenantID,
            HostID:   ah.HostID,
            CVE:      adv.CVEID,
            Severity: adv.Severity,
            Mode:     e.modeFor(ah.TenantID, ah.HostID),
            Source:   "vulnsync",
            Confidence: adv.Confidence,
        })
    }
    return nil
}
```

### 10.3 优先级

VulnSync 推送的 advisory 已带 epss/kev/exploit 等富化字段。Engine **不重新计算优先级**，仅在产 alert 时携带：

```json
{
  "alert_type": "host_vulnerability",
  "cve_id": "CVE-2024-12345",
  "severity": "high",
  "priority_score": 0.82,
  "in_kev": false,
  "has_exploit": true,
  "epss_score": 0.0421
}
```

后续 Manager UI 漏洞中心读取该 alert 即可，不需要重新查 EPSS / KEV。

---

## 11. 与 Manager 漏洞中心的接口

VulnSync 通过 gRPC 暴露 `/internal/v1/*` 给 Manager 使用：

### 11.1 接口列表

| 接口 | 方法 | 用途 |
|------|------|------|
| `/internal/v1/sync/status` | GET | Manager UI 显示各源同步状态 |
| `/internal/v1/sync/trigger` | POST | UI 立即同步按钮（指定 source 或全部） |
| `/internal/v1/sync/source/{name}` | POST | 单源重跑 |
| `/internal/v1/source/list` | GET | 列出所有源 + enabled / confidence |
| `/internal/v1/source/{name}/enable` | POST | 启用源 |
| `/internal/v1/source/{name}/disable` | POST | 禁用源 |
| `/internal/v1/advisory/manual` | POST | 人工补录 advisory |
| `/internal/v1/advisory/manual` | GET | 列出待审补录 |
| `/internal/v1/advisory/manual/{id}/approve` | POST | 审批通过 |
| `/internal/v1/leader/status` | GET | Leader 信息（instance_id + lease_until） |

### 11.2 鉴权

- mTLS 双向证书校验
- 内部 Bearer Token（与 Manager↔Engine 同机制）

### 11.3 Proto 骨架

```protobuf
syntax = "proto3";
package mxsec.vulnsync.v1;

option go_package = "github.com/imkerbos/mxsec-platform/api/proto/vulnsync;vulnsyncpb";

service VulnSync {
    rpc GetSyncStatus(Empty)                returns (SyncStatusResponse);
    rpc TriggerSync(TriggerSyncRequest)     returns (TriggerSyncResponse);
    rpc ListSources(Empty)                  returns (ListSourcesResponse);
    rpc EnableSource(SourceNameRequest)     returns (Empty);
    rpc DisableSource(SourceNameRequest)    returns (Empty);
    rpc SubmitManualAdvisory(ManualAdvisory) returns (ManualAdvisoryResponse);
    rpc ListManualAdvisories(ListManualRequest) returns (ListManualResponse);
    rpc ApproveManualAdvisory(ManualAdvisoryID) returns (Empty);
    rpc GetLeaderStatus(Empty)              returns (LeaderStatusResponse);
}

message SyncStatusResponse {
    repeated SourceStatus sources = 1;
    int64 total_advisories = 2;
    int64 total_vulns = 3;
}

message SourceStatus {
    string name = 1;
    string display_name = 2;
    bool enabled = 3;
    string confidence = 4;          // high | medium | low | enrichment
    string last_status = 5;         // success | failed | running
    int64 last_count = 6;
    int64 last_duration_ms = 7;
    string last_error = 8;
    int64 last_run_at_unix = 9;
    int64 watermark_unix = 10;
    bool offline_cache_used = 11;
}

message TriggerSyncRequest {
    repeated string sources = 1;     // 空则全部
    string mode = 2;                 // incremental | full
    string operator = 3;
}

message ManualAdvisory {
    string cve_id = 1;
    string os_family = 2;
    string os_major = 3;
    string pkg_name = 4;
    string fixed_version = 5;
    string severity = 6;
    double cvss_score = 7;
    string reference_url = 8;
    string description = 9;
    string operator = 10;
}
```

### 11.4 Manager 侧 UI 展示

Manager 漏洞中心新增"情报源管理"页（仅 SystemAdmin 可见），展示：

- 15 个源的实时状态（绿/黄/红灯）
- 上次成功时间 / count / duration
- 启用/禁用开关
- "立即同步" / "单源重跑" 按钮
- 信创人工补录列表 + 审批操作
- Leader 当前实例 + lease_until

---

## 12. 配置示例（最小可运行）

完整配置见 §4.1，这里给出最小 Demo 可启动版本：

```yaml
# /etc/mxsec/vulnsync.yaml — Demo 单副本最小配置
vulnsync:
  instance_id: "vulnsync-1"

  leader_election:
    backend: "redis"
    redis_addr: "redis:6379"
    lock_key: "mxsec:vulnsync:leader"
    lock_ttl: "30m"
    heartbeat_interval: "10m"

  schedule:
    incremental_cron: "0 0 * * * *"
    full_cron:        "0 30 3 * * *"
    max_parallel_sources: 4

  sources:
    osv:       { enabled: true,  concurrency: 16 }
    rhsa:      { enabled: true,  concurrency: 8 }
    usn:       { enabled: true,  concurrency: 4 }
    debian-tracker: { enabled: true }
    alpine:    { enabled: true }
    cisa-kev:  { enabled: true }
    exploit-db: { enabled: true }
    cnnvd:     { enabled: true }
    epss:      { enabled: true }
    nvd:       { enabled: false }   # Demo 默认关，需 API Key
    suse:      { enabled: false }
    openeuler: { enabled: false }
    anolis:    { enabled: false }
    kylin:     { enabled: false }
    uos:       { enabled: false }

  db:
    dsn: "mxsec:mxsec@tcp(mysql:3306)/mxsec?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"

  kafka:
    brokers: ["kafka:9092"]
    topic_advisory: "mxsec.vuln.advisory"
    topic_dlq:      "mxsec.vuln.advisory.dlq"
    acks: "all"
    compression: "snappy"

  observability:
    log_level: "info"
    prom_addr: ":9090"
    health_addr: ":8080"
```

---

## 13. 可观测性

### 13.1 Prometheus 指标

```
# Leader 状态
mxsec_vulnsync_is_leader{instance}                  # 0/1

# 源同步
mxsec_vulnsync_source_run_total{source, mode, status}
mxsec_vulnsync_source_duration_seconds{source, mode}      # histogram
mxsec_vulnsync_source_advisory_count{source, mode}
mxsec_vulnsync_source_last_success_age_seconds{source}    # 距上次成功秒数
mxsec_vulnsync_source_last_error{source, error_type}      # 当前错误类型 1
mxsec_vulnsync_source_offline_cache_used{source}          # 0/1
mxsec_vulnsync_source_circuit_breaker_state{source}       # 0=closed 1=open

# 仲裁
mxsec_vulnsync_advisory_merge_total{confidence}
mxsec_vulnsync_advisory_validate_rejected_total{reason}

# Kafka 推送
mxsec_vulnsync_publish_total{event_type, status}
mxsec_vulnsync_publish_duration_seconds              # histogram
mxsec_vulnsync_publish_dlq_total{reason}
mxsec_vulnsync_publish_lag_ms                        # 与 emitted_at 差值

# 人工补录
mxsec_vulnsync_manual_pending_count
mxsec_vulnsync_manual_approved_total
```

### 13.2 日志

所有日志走 Zap structured logging。关键 event：

```
{
  "level": "info",
  "ts": "2026-06-06T03:35:21Z",
  "logger": "vulnsync.fetcher",
  "msg": "source 拉取完成",
  "source": "rhsa",
  "mode": "incremental",
  "count": 234,
  "duration_ms": 12456,
  "watermark": "2026-06-05T22:00:00Z"
}
```

### 13.3 健康检查

```
GET /healthz
{
  "status": "ok",
  "uptime_seconds": 12345,
  "git_sha": "a9d22a8"
}

GET /readyz
{
  "status": "ok",          // ok | degraded | unhealthy
  "is_leader": true,
  "kafka_connected": true,
  "db_connected": true,
  "stale_sources": ["kylin"],     // 超过 24h 未成功的源
  "broken_sources": []            // 超过 7d 未成功的源
}
```

`status` 判定规则：
- `broken_sources` 非空 → `unhealthy`（503）
- `stale_sources` 非空 → `degraded`（200，告警但不重启）
- 全绿 → `ok`（200）

---

## 14. 失败处理矩阵

| 失败类型 | 现象 | 处理 |
|----------|------|------|
| 单源 HTTP 429 | 限流封禁 | 退避 30s × 重试 3，仍失败标 `failed`，触发熔断 5min |
| 单源 5xx | 上游故障 | 退避 60s × 重试 5，仍失败标 `failed`，下次 cron 重试 |
| 单源 timeout | 网络慢 | 不计入失败，下次 cron 重试 |
| 单源数据解析错 | JSON/YAML 损坏 | 跳过该 advisory，count -1，metric 上报 |
| Kafka 推送失败 | broker 不可达 | 重试 3 次（指数退避 1s/2s/4s），仍失败入 DLQ |
| Kafka acks 超时 | 配置 acks=all，min_isr 不满足 | 等待 broker 恢复 30s，仍失败入 DLQ |
| Redis Leader 锁丢失 | 网络抖动 / Redis 故障 | onLost 钩子停 Cron；重连后重新选举 |
| MySQL 不可用 | DB 故障 | 仅 watermark 读取 / status 写入失败；advisory 仍可推 Kafka |
| 信创主源失败 | 网站改版 | 切备源 → 仍失败切离线缓存 → 仍失败标 unhealthy |
| 全量任务超时 | 数据量增长 | timeout 2h 仍未完成 → 标 failed，下次增量补 |
| 全量任务 OOM | RHSA 5w 条全部加载到内存 | 流式处理（按 advisory 一条一条写 Kafka，不全量加载） |

---

## 15. 容量与性能

### 15.1 单源同步耗时（实测/估算）

| 源 | 增量耗时 | 全量耗时 | 单 advisory 大小 |
|----|----------|----------|------------------|
| NVD | ~5min | ~2h | ~5KB |
| OSV | ~3min（已知 ID skip） | ~30min | ~3KB |
| RHSA | ~10min | ~30min（8 并发） | ~50KB（CSAF） |
| USN | ~2min | ~10min | ~2KB |
| Debian-tracker | ~1min（diff） | ~5min（dump） | dump 30MB |
| Alpine | ~30s | ~5min | YAML 10MB |
| SUSE | ~5min | ~20min | ~30KB |
| CISA KEV | ~10s | ~30s | 1.2MB JSON |
| ExploitDB | ~20s | ~60s | 5MB CSV |
| CNNVD | ~30min（5000 上限） | N/A（仅补编号） | ~1KB |
| EPSS | ~30s | ~2min | 8MB CSV |
| 信创 ×4 | ~5min | ~15min | ~5KB |

**单实例增量同步总耗时**：~60min（含并行优化），满足 1h cron 周期。

### 15.2 Kafka 吞吐

- 全量首跑：~25w advisory_upsert 消息
- 按 acks=all + Snappy + batch 100 + linger 1s：~2000 msg/s
- 全量推送耗时：~125s
- 单消息平均大小：~3KB（含 affected_packages 数组）
- 全量总流量：~750MB

### 15.3 内存占用

- 单 Source goroutine 工作集：< 50MB（流式处理）
- 全部 10+ 源并行：< 500MB
- Service Pod 限制建议：1 vCPU / 1Gi RAM

### 15.4 网络

- 出向：~1GB/day（CSAF + JSON dump + EPSS）
- 入向（gRPC + Kafka）：< 10MB/s 峰值
- 部署建议：放在能访问公网 + Kafka cluster 的可用区

---

## 16. 部署与升级

### 16.1 部署形态

| 部署方式 | 副本 | 数据源 | 备注 |
|----------|------|--------|------|
| docker-compose（dev） | 1 | 默认 8 源 | 信创关闭 |
| 标准多副本（prod） | 2 | 全 15 源 | Leader Election 自动选主 |
| 离网部署 | 1 | RHSA mirror + OSV mirror + 信创离线包 | 全部走内网镜像 |

### 16.2 升级流程

VulnSync 升级遵循 mxsec 通用流程：
1. `mxctl deploy --service vulnsync --version v2.x.x`
2. 滚动升级（先 follower 后 leader）
3. 升级过程中 Cron 暂停 ≤ 5min（leader 切换窗口）
4. 新版本启动后首次 cron 强制全量同步（容忍 schema 变更）

### 16.3 灰度

VulnSync 是无 Agent 副作用的纯后端服务，**不需要 Canary 灰度**。直接升级即可，错误通过指标告警 + 自动回滚（K8s Deployment maxSurge=1 maxUnavailable=0）。

---

## 17. 安全考虑

### 17.1 上游证书校验

所有 HTTPS 请求**禁止跳过证书校验**（`InsecureSkipVerify=false`）。私有镜像源使用自签证书时必须通过配置注入 CA 证书。

### 17.2 API Key 加密存储

`vuln_data_sources.api_key` 字段 AES-GCM 加密，密钥从 `MXSEC_DATA_KEY` 环境变量读取（与 mxsec 通用 secret 加密一致）。

### 17.3 上游数据投毒防御

| 风险 | 防御 |
|------|------|
| 上游 advisory 伪造（DNS 劫持） | TLS 证书校验 + 已知 Issuer 白名单 |
| 上游 advisory 注入恶意 reference_url | URL scheme 白名单（仅 https） |
| 上游 advisory 包含恶意脚本（description XSS） | 入库前 HTML escape，前端展示走 `v-text` |
| CNNVD WAF 触发账号封禁 | UA 伪装 + 速率自控 + Cloudflare 519 退避 |

### 17.4 Kafka 通信

VulnSync ↔ Kafka 走 SASL/SSL（生产环境）或明文（Demo）；与 mxsec 通用 Kafka 配置一致。

### 17.5 人工补录审计

`manual_advisories` 表每条记录 `submitted_by` + `submitted_at`，approve 操作必须 RBAC 校验 + 写 audit_log（180 天保留）。

---

## 18. 与现有 vuln-module-design.md 的关系

| 文档 | 关注点 | 维护方 |
|------|--------|--------|
| `vulnsync-design.md`（本文） | **VulnSync 服务自身**：服务化拆分、Cron 调度、Leader Election、Kafka 推送、源接入工程细节、人工补录 | mxsec 后端组 |
| `vuln-module-design.md` | **漏洞业务模块**：漏洞中心 UI、host_vulnerability 状态机、修复闭环、pre-check、targeted scan、reconcile | mxsec 业务组 |
| `engine-detection-design.md` | **Engine 检测层**：host × advisory 匹配规则、告警生成、storyline | mxsec Engine 组 |

> 三文档无重复职责。当 VulnSync 推 advisory → Consumer 落库 → Engine 匹配产 alert → vuln-module-design 接管 UI/修复闭环。

---

## 19. 落地路线

VulnSync 服务化从现有单体（`internal/server/manager/biz/vuln_scanner.go` 859 行 + `biz/advisory/` 12 个 source）拆分而来。落地分两阶段：

### 19.1 Phase 1（M3 阶段）：服务拆分（已部分完成）

- [x] `advisory.Coordinator` 多源融合（已落地）
- [x] PURL + NEVRA 双索引（已落地）
- [x] 3 级 confidence 仲裁（已落地）
- [x] 11 个 source 接入（已落地，含 4 个信创 stub）
- [x] CNNVD 官方 API 接入（已落地）
- [x] CISA KEV / ExploitDB（已落地）
- [x] watermark 持久化（已落地）

### 19.2 Phase 2（M4 阶段）：独立服务化（进行中）

- [ ] 拆出独立 `cmd/server/vulnsync/main.go`
- [ ] 实现 Redis Leader Election
- [ ] Kafka Producer 替代直接 DB 写入
- [ ] 新增 SUSE / EPSS 源
- [ ] 信创 4 源对接真实数据（双源 + 离线缓存）
- [ ] gRPC `/internal/v1/*` API + Manager UI 情报源管理页
- [ ] 人工补录通道（YAML 上传 + 审批）

### 19.3 Phase 3（M5+ 阶段）：增强能力

- [ ] CVSS 4.0 双轨支持
- [ ] OSV mirror 私有部署能力（KA 离网客户）
- [ ] 自动学习上游 advisory 模式（应对 schema 变更）
- [ ] SBOM 反向匹配（给定 SBOM 直接出漏洞列表）

详细排期见 `ref/08-roadmap.md` M4 / M5 阶段。

---

## 20. 参考

- [`architecture.md`](architecture.md) §2.5 — VulnSync 在六微服务中的位置
- [`operating-modes.md`](operating-modes.md) — Engine 消费 advisory 时的 mode 行为
- [`multi-tenant.md`](multi-tenant.md) — 主机 × advisory 关联的 tenant 隔离
- [`vuln-module-design.md`](vuln-module-design.md) — 漏洞业务模块（VulnSync 的下游）
- [`engine-design.md`](engine-design.md) — Engine 消费 advisory 的匹配实现
- [`datatype-allocation.md`](datatype-allocation.md) — DataType 12001-12099 分配
- [`api-reference.md`](api-reference.md) — Manager 漏洞中心对外 API
- `ref/06-漏洞.md` §5 — 推荐方案原始评估
- `ref/06-漏洞.md` §7 — 风险与开放问题
- `ref/appendix/_raw/qingteng-ppt.txt` — 青藤万象/蜂巢能力对照
- 源码：
  - `internal/server/manager/biz/vuln_scanner.go`
  - `internal/server/manager/biz/advisory/`
  - `internal/server/manager/biz/nvd_sync.go`
  - `internal/server/manager/biz/cnnvd_sync.go`
  - `internal/server/manager/biz/exploit_sync.go`
  - `internal/server/manager/biz/mitre_cve.go`
  - `internal/server/manager/biz/vuln_data_source_service.go`
