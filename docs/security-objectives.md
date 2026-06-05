# 三大产品目标 — 安全运营闭环

> **本文位置**：[`architecture.md`](architecture.md) §13 列名"三大产品目标"，在此展开。
>
> **产品哲学一句话**：**看清 → 算清 → 处清**，三段递进，对应 NIST CSF 五段的全周期闭环。
>
> **设计立意**：mxsec 是工业级开源 CWPP，专精 Linux 主机 + Kubernetes 容器，面向 ToB 政企 / 金融 / 互联网客户。三大目标既是"产品功能盘"，也是"商务对话剧本"。任何一个能力点、任何一个 API、任何一条规则，都必须能映射到三大目标之一。落不到目标里的能力，属于"工程兴趣项"，不上路线图。

---

## 0. 一页提要

| 维度 | 目标 1：是什么 | 目标 2：为什么 | 目标 3：怎么做 |
|------|---------------|---------------|---------------|
| **关键词** | 看清（Visibility） | 算清（Insight） | 处清（Response） |
| **NIST CSF** | Identify + Detect | Identify + Detect（脆弱性视角） | Protect + Respond + Recover |
| **运行模式** | observe + protect 都做 | observe + protect 都做 | observe **不下处置**；protect 才下处置 |
| **承载微服务** | AgentCenter + Consumer + Engine + Manager | VulnSync + Engine + Consumer + Manager | Engine + Manager + Agent + LLMProxy |
| **能力点数** | 11 | 8 | 8 |
| **核心交付** | 资产 22 类 + 流量南北东西 + 态势 | 漏洞 + 基线 + 运行时威胁 + 配置 + 弱口令 | 修复 + 应急 + 复盘 |
| **典型问题** | "我这里有什么？" | "我这里风险在哪？" | "出事了我怎么办？" |
| **失败后果** | 资产盲区 → 攻击者比甲方更懂家底 | 漏洞悬挂 → 等保不过 → 0day 必中 | 告警空响 → SOC 失能 → MTTR 不收敛 |

> 三个目标**不分先后、必须共存**。只看不算 = 巨型 SIEM 灯泡墙；只算不处 = 漂亮报表无落地；只处不看 = 全凭直觉乱扣机器。三段闭环才是工业级 CWPP 的合格线。

---

## 1. 产品哲学：看清 → 算清 → 处清

### 1.1 三段递进与 NIST CSF 对照

NIST CSF（Cybersecurity Framework）把安全运营分成五段：

```
                                Identify ────► 资产/暴露面/责任
                              ↗
                            ↗
                  Detect ────► 异常/告警/脆弱性
                ↗
              ↗
   Protect ────► 加固/隔离/策略
              ↘
                ↘
                  Respond ────► 处置/封禁/调查
                            ↘
                              ↘
                                Recover ────► 复盘/重建/教训
```

mxsec 把这五段压缩成对客户最直白的"看清 → 算清 → 处清"三段：

| mxsec 三段 | NIST CSF 对应 | 关键设问 | 主交付物 |
|-----------|--------------|---------|---------|
| **看清（Visibility）** | Identify + 部分 Detect | "我这里有什么？谁在跟谁说话？" | 资产清单 / 流量拓扑 / 态势大屏 |
| **算清（Insight）** | Identify + Detect（脆弱性视角） | "我这里风险在哪？暴露面多大？" | 漏洞清单 / 基线评分 / 告警 / Storyline |
| **处清（Response）** | Protect + Respond + Recover | "出事了怎么办？以后怎么不出事？" | 修复 Plan / 应急 Playbook / 复盘报告 |

> **不可逆**：缺哪一段都崩盘。但**顺序必须从看清开始**——没看清就算账，算的是别人家的账；没算清就处置，处的是自己业务的命。

### 1.2 与 observe/protect 双模式的关系

mxsec 默认部署即**监听模式**（`MODE=observe`），磨合达标后切**防护模式**（`MODE=protect`）。详见 [`operating-modes.md`](operating-modes.md)。

| 模式 | 目标 1（看清） | 目标 2（算清） | 目标 3（处清） |
|------|---------------|---------------|---------------|
| `observe` | ✅ 全量采集 | ✅ 全量分析 + 全量告警 | 仅生成 Plan / `would_action`，**不自动下处置** |
| `protect` | ✅ 全量采集 | ✅ 全量分析 + 全量告警 | ✅ 自动下处置（IP 封禁 / PAM / kill / 文件隔离 / NetworkPolicy 等） |

> **关键约束**：observe 模式下，目标 1、目标 2 全功能；目标 3 退化为"提交建议 + 等待用户审批"。用户主动触发的处置（UI 点"立即修复"）任何模式都允许。

### 1.3 与青藤万象方法论的对照

青藤万象白皮书 §2 提出**安全指标模型**：「资产 / 关系 / 活动 / 操作」四个维度，"无论多么高级的黑客，哪怕利用 0day，必然会触发主机上相应指标的变动"（详见 `ref/appendix/青藤万象-能力清单.md` §3.2）。

mxsec 的"看清 → 算清 → 处清"是把青藤的"四维指标 + 闭环"重新组织成**对客户更可解释的产品语言**：

| 青藤万象四维 | mxsec 落点 |
|-------------|-----------|
| **资产 Assets** | 目标 1（看清）·资产清点 22+ 类 |
| **关系 Relationship** | 目标 1（看清）·东西南北向流量与依赖 |
| **活动 Activities** | 目标 1（看清）+ 目标 2（算清）·EDR 事件流 + 检测 |
| **操作 Operations** | 目标 2（算清）+ 目标 3（处清）·风险评估 + 修复 |

> **结论**：mxsec 的方法论与青藤同源（Gartner Adaptive Security），但裁剪成**开源 CWPP 可落地的工程清单**，不做青藤万象覆盖的 Windows / Unix 小机方向。

---

## 2. 目标 1 — 知道"是什么"（Visibility）

### 2.1 设问与立意

| 设问 | 客户痛 |
|------|--------|
| 我这里有多少台主机 / 容器 / Pod？ | 上一次盘点是 Excel，2 周前的，错 30% |
| 每台机器跑了什么服务？开了什么端口？连了什么外网？ | CMDB 不准，攻防演练前夜手忙脚乱 |
| 谁在跟谁说话？哪些是必要的？哪些是攻击者爬出来的？ | 东西向流量从来没看过，横移一打一个准 |
| 全网整体什么态势？这周新增了什么？哪些主机风险最高？ | 没有大屏，老板问起只能"我去查一下" |

> **看清**是其他两段的前置。**没有资产清单，告警不知道打在谁身上；没有流量拓扑，攻击链画不出来；没有态势，运营会议没得汇报**。

### 2.2 能力地图（11 个能力点）

| # | 能力 | 现状 | 承载模块 | 关键数据源 | 说明 |
|---|------|------|----------|------------|------|
| V-1 | 主机资产清点（OS / 硬件 / 内核 / 网卡） | ✅ | Agent + Consumer | `mxsec.agent.asset` Topic | DataType 5050-5060 |
| V-2 | 进程清点（实时 + 周期快照） | ✅ | Agent EDR | `mxsec.agent.ebpf` | tracepoint sched_process_exec |
| V-3 | 端口清点（监听 + 已建连） | ✅ | Agent | `mxsec.agent.asset` | /proc/net/tcp + ss |
| V-4 | 账号清点（passwd / shadow / sudo / key） | ✅ | Agent | `mxsec.agent.asset` | 含 root / sudo / 过期账号 |
| V-5 | 软件包清点（RPM / DEB / 启动项 / cron） | ✅ | Agent | `mxsec.agent.asset` | 含 systemd unit |
| V-6 | 应用清点（Web / 数据库 / 中间件） | ⚠️ | Scanner Plugin | `mxsec.agent.asset` | 应用指纹库待补，目标 200+ |
| V-7 | 容器与 Pod 清点（含镜像 / 标签 / 命名空间） | ⚠️ | Agent K8s 视角 | `mxsec.agent.asset` | DaemonSet 模式，待补 sidecar 富化 |
| V-8 | 南北向流量采集（出入网连接） | ⚠️ | Agent eBPF | `mxsec.agent.ebpf` | DataType 3000-3002，目前到事件层 |
| V-9 | 东西向流量采集（节点内 / 集群内） | ❌ | Agent eBPF + K8s | — | Phase 2 微隔离 v1 引入 |
| V-10 | 流量拓扑可视化 | ❌ | Manager UI | Graph DB / 物化视图 | Phase 2 引入，含服务依赖图 |
| V-11 | 态势感知大屏（趋势 / 排行 / 新增） | ⚠️ | Manager UI + Engine | ClickHouse 物化视图 | 已有基础 Dashboard，缺态势模板 |

> 评级图例：✅ MVP 内可交付（80%+） / ⚠️ MVP 部分能力（30-80%） / ❌ M1 之后

### 2.3 数据流（端到端）

```
+--------------+    +--------------+    +--------------+    +---------+
| Agent 采集器  |    | AgentCenter  |    | Kafka        |    | Consumer|
|              |───►| (纯转发)     |───►| 8+ Topic     |───►| 幂等写入 |
|  asset       |    |              |    |              |    |  MySQL  |
|  ebpf        |    |              |    |              |    |   CK    |
|  fim         |    |              |    |              |    |  Redis  |
+--------------+    +--------------+    +--------------+    +---------+
                                              │
                                              │  ConsumerGroup B
                                              v
                                       +-------------+      +----------+
                                       | Engine      |─────►| 态势计算  |
                                       |  - 拓扑构建  |      | 排行/趋势 |
                                       |  - 资产关联  |      +----------+
                                       +-------------+
                                              │
                                              v
                                       +-------------+
                                       | Manager UI  |
                                       | 资产/拓扑/态势 |
                                       +-------------+
```

### 2.4 与微服务架构的映射

| 微服务 | 在目标 1 中的角色 |
|--------|------------------|
| **Agent** | 资产采集主体（22+ 类清点 + EDR 流量） |
| **AgentCenter** | 数据面接入 + 路由到 Kafka |
| **Consumer** | 写入 MySQL（资产快照）+ ClickHouse（事件归档）+ Redis（缓存） |
| **Engine** | 拓扑构建（流量 → 图）+ 态势计算（趋势 / 排行） |
| **Manager** | 资产管理 API + 态势大屏 + 拓扑可视化 + 报表 |
| **VulnSync** | 不参与（与"看清"无关） |
| **LLMProxy** | 可选：自然语言查资产（"列出所有 Java 8 主机"） |

### 2.5 KPI（看清 SLO）

| 指标 | 目标值 | 度量方法 |
|------|--------|---------|
| 资产清点完整率 | ≥ 98% | 抽样 50 台主机人工核对 / 与 CMDB 对比 |
| 资产更新延迟 | ≤ 5min | 进程拉起 → UI 可见的端到端时延 |
| 拓扑可视化覆盖率 | ≥ 90%（流量节点） | 抽样审计周流量 vs 拓扑节点 |
| 态势大屏 P95 加载 | ≤ 3s | 前端打点 + ClickHouse 物化视图 |
| 资产 API QPS | ≥ 200（单租户） | 压测 + Prometheus |
| 流量采集丢包率 | ≤ 0.1% | eBPF perf buffer 丢失计数 |

### 2.6 落地范围与边界

- **支持**：Linux 主机（CentOS / Ubuntu / RedHat / Debian / openEuler / Anolis / Kylin / UOS）、Kubernetes 容器（cri-o / containerd / cri-dockerd）
- **不支持**：Windows 主机、macOS、Unix 小机（AIX / HP-UX / Solaris）

---

## 3. 目标 2 — 知道"为什么"（脆弱性 Insight）

### 3.1 设问与立意

| 设问 | 客户痛 |
|------|--------|
| 我这里中了多少 CVE？哪些有 PoC？哪些已经被 CISA KEV 列了？ | 漏扫报告几千条，不知道修哪个 |
| 等保 2.0 三级 / CIS Benchmark 哪些项还没满足？ | 等保测评前补作业 |
| 现在主机上有没有反弹 shell、暴力破解、异常登录、提权痕迹？ | 入侵了不知道 |
| MySQL / Redis 是不是裸跑？SSH 是不是允许 root 密码登录？ | 老问题反复出 |
| 主机和应用的弱口令清单？ | 红队最爱 |

> **算清**的本质是把目标 1 看到的"资产 + 活动"映射到**风险维度**上：漏洞、基线、运行时威胁、配置、弱口令。每一个维度都要可量化、可追踪、可关闭。

### 3.2 能力地图（8 个能力点）

| # | 能力 | 现状 | 承载模块 | 关键数据源 | 说明 |
|---|------|------|----------|------------|------|
| I-1 | 漏洞检测（主机 / 容器 / 镜像） | ⚠️ | Scanner Plugin + VulnSync | `mxsec.agent.scanner` + `mxsec.vuln.advisory` | OS Package + Trivy DB；待扩 SBOM CycloneDX |
| I-2 | 应用层漏洞（Web 框架 / 中间件 / RASP 视角） | ❌ | RASP Plugin（Phase 4） | `mxsec.agent.events` | Java MVP 优先 |
| I-3 | 基线（等保 2.0 / CIS / Docker / K8s / 中间件） | ⚠️ | Baseline Plugin + Engine | `mxsec.agent.baseline` | 已有 212 条，目标 800+ |
| I-4 | 运行时威胁（反弹 shell / 提权 / 异常登录 / 暴力破解 / Web 后门） | ⚠️ | Agent EDR + Engine | `mxsec.agent.ebpf` + `mxsec.engine.alert` | "六件套"中已覆盖 2，目标 6 全 |
| I-5 | 内存马 / Rootkit 检测 | ❌ | Anti-Rootkit + RASP | `mxsec.agent.ebpf` | M1 引入 |
| I-6 | 系统 / 应用配置风险（Redis 裸跑 / SSH root 等） | ⚠️ | Baseline Plugin | `mxsec.agent.baseline` | 现 6 应用，目标 20+ |
| I-7 | 弱口令探测（系统账号 + 应用） | ❌ | Scanner Plugin | `mxsec.agent.scanner` | MVP P0 收口 |
| I-8 | 攻击链 Storyline + ATT&CK 映射 | ⚠️ | Engine | `mxsec.engine.storyline` | 已有骨架，目标 ATT&CK 32 → 200+ |

### 3.3 与运行时威胁"六件套"的对照（青藤口径）

青藤万象把入侵监控分为 8 锚点（详见 `ref/appendix/青藤万象-能力清单.md` §2.3），mxsec 收口为**入侵检测六件套**：

| # | 六件套 | 检测载体 | mxsec 现状 |
|---|--------|---------|-----------|
| 1 | 暴力破解（SSH / vsftpd / postfix） | Agent EDR + Engine CEL | ❌ MVP P0 |
| 2 | 异常登录（geoip + 时间基线） | Engine ML + Geo IP | ❌ MVP P0 |
| 3 | 反弹 shell（多形态） | Agent EDR | ✅ 5 条规则 |
| 4 | 本地提权（SUID / sudo / capabilities） | Agent EDR | ⚠️ 1 条 SUID 规则 |
| 5 | 后门 / Rootkit（LKM / fops / syscall_table） | Anti-Rootkit Plugin | ❌ M1 |
| 6 | Web 后门（PHP / JSP / ASPX YARA） | Scanner Plugin + YARA-X | ❌ MVP P0 |

> 详见 `ref/04-运行时.md` §4 P0-1 入侵检测六件套专项化。

### 3.4 漏洞 → 修复链路（与 VulnSync 协同）

```
[VulnSync (1h 增量)]
  │  NVD / OSV / RHSA / USN / DSA / Alpine / SUSE / CISA KEV / ExploitDB / EPSS
  │  + 信创 4 源（CSA / ANSA / KYSA / UOSEC）
  │  + CNNVD 编号补全
  v
[融合仲裁]
  │  PURL + NEVRA 双索引模型，3 级 confidence
  v
[Kafka mxsec.vuln.advisory]
  │
  ├──► Consumer ─► MySQL vulnerabilities 表
  │
  └──► Engine ─► 主机指纹 vs advisory ─► 产 host_vulnerability alert
                                               │
                                               v
                                       [Manager UI 漏洞中心]
                                               │
                                               │  按 EPSS / CISA KEV / CVSS 排序
                                               v
                                       [Phase 3 处清：修复 Plan]
```

详见 [`vulnsync-design.md`](vulnsync-design.md) 与 [`vuln-module-design.md`](vuln-module-design.md)。

### 3.5 与微服务架构的映射

| 微服务 | 在目标 2 中的角色 |
|--------|------------------|
| **VulnSync** | 11 源漏洞情报融合，每 1h 增量、1d 全量 |
| **Engine** | CEL 规则 / 序列 / ML 推理 / Storyline / ATT&CK / Sigma & Falco 转换 |
| **Agent + Plugin** | EDR / FIM / Baseline / Scanner / RASP（Phase 4） |
| **Consumer** | 写入 alert / vulnerability / baseline_result |
| **Manager** | 风险中心 UI（6-tab：漏洞 / 基线 / 病毒 / 弱口令 / 配置 / 告警） |
| **LLMProxy** | 可选：告警语义解释 / Storyline 总结 / 误报降权建议 |

### 3.6 KPI（算清 SLO）

| 指标 | 目标值 | 度量方法 |
|------|--------|---------|
| 漏洞检出率 | ≥ 90%（与 NVD 对比） | 抽样 100 台主机 |
| 漏洞 advisory 同步延迟 | ≤ 1h（增量） | VulnSync 心跳 |
| 基线扫描完成时间 | ≤ 5min（5k 主机） | 任务 dispatch → 全部回执 |
| Engine 告警 P95 延迟 | ≤ 5s（Agent → UI） | TraceID 端到端 |
| Engine 月度误报率 | ≤ 5%（90d 后 ≤ 2%） | 用户标记反馈 |
| 告警准确率 | ≥ 85% | 用户标记 true_positive / 总告警 |
| ATT&CK 覆盖技战法数 | ≥ 80（MVP）→ 200+（M2） | 规则映射表 |
| 弱口令探测主机覆盖率 | ≥ 95% | 任务回执 |

### 3.7 监听 vs 防护下的算清差异

| 步骤 | observe | protect |
|------|---------|---------|
| 采集 | ✅ 全量 | ✅ 全量（与 observe 完全一致） |
| 检测（规则 / 序列 / ML） | ✅ 全量 | ✅ 全量 |
| 告警生成 | ✅ 写 `mxsec.engine.alert`，UI 推 | ✅ 同上 |
| 告警字段 `would_action` | ✅ 描述应执行的动作 | null |
| 告警字段 `action` | null | ✅ 实际下发的动作 + 结果 |
| Storyline 攻击链 | ✅ 全量构建 | ✅ 全量构建 |

> **算清不受模式影响**。模式只影响"处清"。

---

## 4. 目标 3 — 知道"怎么做"（闭环 Response）

### 4.1 设问与立意

| 设问 | 客户痛 |
|------|--------|
| 这批漏洞怎么修？谁去修？什么时候修完？修了会不会影响业务？ | 修复无人 owner，漏洞悬挂半年 |
| 基线项漂了怎么自动 fix？fix 会不会改坏？能不能回滚？ | 配置漂移持续发生 |
| 暴力破解发生了，IP 封了吗？SSH 是不是该禁 root 了？被打到的主机要不要扫一遍？ | 一次入侵手忙脚乱 5 个小时 |
| 病毒文件已隔离，怎么取证？还原入口在哪？ | 处置后没复盘，下次还来 |
| 这次攻击的完整链路是什么？ATT&CK 走了哪几个 stage？ | 老板要事件报告，3 天写不完 |

> **处清**是检验"看清 + 算清"是否真有商业价值的唯一标尺。**告警不闭环 = SOC 灯泡墙**；漏洞不修复 = 等保过不了；事件不复盘 = 同一个洞反复中。

### 4.2 能力地图（8 个能力点）

| # | 能力 | 现状 | 承载模块 | 关键数据源 | 说明 |
|---|------|------|----------|------------|------|
| R-1 | 修复 Plan 编排（pre / verify / rollback） | ⚠️ | Manager 编排器 | MySQL remediation_plans | 已有 v1 骨架 |
| R-2 | 漏洞补丁修复（含灰度发布） | ⚠️ | Remediation Plugin + Manager | `mxsec.agent.remediation` | CanaryRollout v2 5% → 25% → 100% |
| R-3 | 基线一键 fix（含影响评估） | ⚠️ | Baseline Plugin + Remediation Plugin | `mxsec.agent.remediation` | 含回滚脚本 |
| R-4 | 病毒处置（隔离 / 删除 / 还原） | ❌ | AV Scanner Plugin（Phase 4） | `mxsec.agent.remediation` | 隔离箱 mv-perm-sha256 三件套 |
| R-5 | 应急响应（IP 封禁 / PAM 封停 / 端口封禁 / 进程 kill） | ⚠️ | Engine + Agent | `mxsec.engine.alert` action | observe 仅 would_action |
| R-6 | Playbook SOAR（条件触发 + 多步骤编排） | ❌ | Engine + Manager | engine_playbooks 表 | M1 引入 |
| R-7 | 攻击链 Storyline + 取证报告 | ⚠️ | Engine + Manager UI | `mxsec.engine.storyline` | ATT&CK 映射 + 时间线 + 证据下载 |
| R-8 | 复盘报告（事件 / 月度 / 合规 / 等保自评） | ⚠️ | Manager 报表引擎 | MySQL audit_log + alerts | PDF / Word / CSV |

### 4.3 NIST CSF 三段映射

| NIST 段 | mxsec 落点 | 关键交付物 |
|---------|-----------|-----------|
| **Protect** | 加固类（基线 fix / SSH 禁 root / 网络隔离） | RemediationPlan v1 |
| **Respond** | 阻断类（IP 封禁 / 进程 kill / 文件隔离） | Engine action（仅 protect 模式自动） |
| **Recover** | 复盘类（取证 / 报告 / Playbook 沉淀） | Storyline + 月度报告 |

### 4.4 修复 Plan 标准结构

每次修复必须按以下结构编排，禁止"裸命令直推"：

```yaml
plan:
  id: plan-2026060500001
  tenant_id: t-bank-a
  type: vuln_remediation      # vuln_remediation / baseline_fix / av_quarantine / network_isolation
  source_alert_id: alrt-xxx
  target:
    selector:
      - host_label: "env=prod"
      - host_label: "app=mysql"
    count: 23
  canary:
    enabled: true
    waves:
      - percent: 5
        observe_minutes: 60
      - percent: 25
        observe_minutes: 60
      - percent: 100
  steps:
    - id: pre
      type: precheck
      script: scripts/precheck-mysql.sh
      failure: abort
    - id: backup
      type: snapshot
      paths: [/etc/my.cnf]
    - id: apply
      type: package_upgrade
      package: mysql-server
      version: 8.0.36-1.el8
    - id: verify
      type: postcheck
      script: scripts/verify-mysql.sh
      failure: rollback
    - id: rollback
      type: snapshot_restore
      paths: [/etc/my.cnf]
      auto: false               # 默认手动触发
  approval:
    required: true
    approvers:
      - role: security-ops
      - role: app-owner
  notification:
    on_start: [email, slack]
    on_each_wave: [slack]
    on_complete: [email, slack, station]
    on_failure: [email, slack, sms]
  audit:
    record: true
    retention_days: 180
```

### 4.5 应急响应 Playbook（与 Engine 联动）

```
┌─────────────────────────────────────────────────────────────────┐
│                  Engine 产 alert                                 │
│                  rule_id=BRUTE_FORCE_SSH                         │
│                  severity=high  tenant=t-bank-a                  │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                  mode=observe │ mode=protect
                ┌──────────────┴──────────────┐
                v                              v
   ┌──────────────────────────┐    ┌─────────────────────────┐
   │ 仅写 would_action         │    │ Engine 下 action        │
   │ UI 显示"建议封禁 IP"        │    │  1. Manager 创任务      │
   │ 等待用户点击"立即执行"      │    │  2. AC 推送 Agent       │
   │  ↓                        │    │  3. Agent 写 iptables    │
   │  用户审批 → 走 R-5 通道   │    │  4. ack 回 Kafka         │
   └──────────────────────────┘    └──────────┬──────────────┘
                                              │
                                              v
                                  ┌──────────────────────────┐
                                  │ Engine 同时触发 Playbook  │
                                  │  - 对该 IP 历史活动审计    │
                                  │  - 该主机连发同源告警合并  │
                                  │  - 扫描该主机后门痕迹      │
                                  │  - 通知 SOC + 业务方       │
                                  └──────────────────────────┘
```

### 4.6 受 `mode` 控制 vs 不受控制（重申）

| 类别 | 动作 | observe | protect | 用户主动 |
|------|------|---------|---------|---------|
| **加固类** | 基线 fix | 仅生 Plan | 仍需审批后执行 | 允许 |
| **加固类** | SSH 配置变更 | 仅生 Plan | 仍需审批后执行 | 允许 |
| **加固类** | 漏洞补丁 | 仅生 Plan | 仍需审批后执行 | 允许 |
| **阻断类** | IP 封禁 / PAM 封停 / 端口封禁 | ❌ would | ✅ 自动 | 允许 |
| **阻断类** | 进程 kill | ❌ would | ✅ 自动 | 允许 |
| **阻断类** | 病毒文件隔离 | ❌ 仅 detection | ✅ Agent 搬迁 | 允许 |
| **阻断类** | K8s Admission deny | dry-run warn | enforce deny | — |
| **观察类** | 采集 + 告警 + 报表 + Storyline | ✅ | ✅ | — |

> **规则**：自动响应受 mode 控制，用户主动响应不受。详见 [`operating-modes.md`](operating-modes.md) §8。

### 4.7 与微服务架构的映射

| 微服务 | 在目标 3 中的角色 |
|--------|------------------|
| **Engine** | 产生 action + 触发 Playbook + Storyline 关联 |
| **Manager** | 修复 Plan 编排 + 审批工作流 + 报表生成 + audit |
| **AgentCenter** | 下发 action 到具体 Agent + Canary 灰度 |
| **Agent + Plugin** | 执行 action（iptables / pam / kill / 文件隔离 / 包升级 / 配置修改） |
| **Consumer** | 写入 remediation_result + audit_log |
| **VulnSync** | 不参与（补丁信息已经在算清阶段沉淀） |
| **LLMProxy** | 可选：Storyline 总结成自然语言报告 / Plan 起草 |

### 4.8 KPI（处清 SLO）

| 指标 | 目标值 | 度量方法 |
|------|--------|---------|
| 高危漏洞 MTTR（中规模） | ≤ 7d | 漏洞产出 → 修复完成 |
| 应急响应 MTTR | ≤ 30min | 告警 → 处置完成（protect） |
| 修复任务可达率 | ≥ 99.9% | Agent ack 回执率 |
| 修复任务失败回滚率 | ≥ 99%（失败时自动 / 一键回滚） | 灰度测试 |
| Playbook 误处置率 | ≤ 1% | 用户标记"误处置" |
| Storyline 完整率 | ≥ 90% | 攻击链节点齐全 |
| 月度报告自动化率 | 100% | 不需人工剪切 |
| 双人审批通过率 | 100% 强制（高危动作） | Casbin + 审批流 |

---

## 5. 端到端用户故事 — 运营人员的一天

> 主角：李工，某城商行安全运营人员，负责 5000+ 台 Linux 主机 + 200+ Pod 的 K8s 集群。租户 ID：`t-bank-a`，模式：`observe`（运行 95 天，准备灰度切 protect）。

### 5.1 09:00 — 看清

李工打开 mxsec 控制台，首页是**态势感知大屏**：

- 在管主机 5,212 / 在线 5,198（**目标 1 · V-1**）
- 昨日新增进程异常 12 类，新增对外连接 IP 8 个（**目标 1 · V-2 + V-8**）
- 高危告警 5 条，悬挂漏洞 234 条（**目标 2 · I-1**）
- 顶部固定 Banner 显示「当前全局模式：observe」（绿色）（**[`operating-modes.md`](operating-modes.md) §7.1**）

注意到大屏的"今日新增资产"卡片标红：3 台主机昨夜新增（来自 K8s autoscaling），自动归到 `env=prod-core` 标签下。

**李工点击「资产 → 主机列表」**，过滤 `created_at > 24h`，看到 3 台新主机的进程清单 / 端口清单 / 软件清单，确认是业务发版触发的扩容。**目标 1 闭环**。

### 5.2 10:30 — 算清

切到「漏洞中心」（**目标 2 · I-1**）：

- 234 条悬挂漏洞中，CISA KEV 命中 4 条，EPSS > 0.9 的有 7 条 — 都需要本周处理
- 点开 CVE-2026-1247（OpenSSH 高危），影响范围 23 台主机，全部带 `env=prod` 标签
- 该 CVE 的 advisory 来源：NVD + RHSA-2026:1234 + Ubuntu USN-7890-1，融合 confidence = 3（最高）

**Engine 告警栏** 同时产了新告警：rule_id=`BRUTE_FORCE_SSH`，目标主机 `h-12345`，severity=high。打开告警详情：

- `mode: observe`
- `would_action: {type: ip_block, target: 192.0.2.45, duration_sec: 3600, reason: "5 次 SSH 登录失败"}`（**[`operating-modes.md`](operating-modes.md) §6**）
- Storyline 已经构建：T1110.001（暴力破解）→ T1078.003（合法账号）→ 可疑 sudo（待确认）
- ATT&CK 覆盖 3 个 stage

**李工点击「真威胁」反馈按钮**，Engine 收到反馈写入 `mxsec.engine.feedback` Topic，规则置信度 +1。**目标 2 闭环**。

### 5.3 11:00 — 处清（修复决策）

李工决定先处理 IP 封禁，再排期漏洞修复。

**应急响应（用户主动）**（**目标 3 · R-5**）：

1. 在告警详情页点击「立即执行 would_action」 — 因为是用户主动触发，observe 模式也允许
2. 系统弹双人审批，李工的同事在审批中心点了批准
3. Manager 创建任务，AC 路由到 `h-12345` 所在的 AgentCenter 实例，gRPC 下推
4. Agent 执行 `iptables -A INPUT -s 192.0.2.45 -j DROP`，ack 回 Kafka
5. UI 上 30 秒内显示 `action.status = success`、`agent_ack_at`、`iptables_rule_id = MXSEC-AUTO-1234`
6. 全程进 audit_log，180 天不可删

**漏洞修复（编排）**（**目标 3 · R-2**）：

1. 漏洞中心点击「批量修复」，选中 CVE-2026-1247 涉及的 23 台主机
2. 系统自动生成 RemediationPlan，类型 `vuln_remediation`，含 5 步骤：precheck → backup → apply（`yum update openssh`）→ verify → rollback
3. 默认走 Canary 灰度：5%（2 台）→ 25%（6 台）→ 100%（剩 15 台），每波观察 60min
4. 提交审批 → 安全运营负责人 + 业务 owner 双签
5. 灰度推送启动，李工在「修复任务」页面实时看进度
6. 第二波（25%）有 1 台主机 verify 步骤失败，自动 rollback，任务暂停
7. 通知矩阵触发：Slack + 邮件 + 站内信

李工去定位失败原因，发现是该主机有自编译 ssh，包管理器无法覆盖，标记为「人工处理」队列。**目标 3 修复链路闭环**。

### 5.4 16:00 — 处清（复盘报告）

下午李工要给客户领导汇报本月运营成果（**目标 3 · R-8**）：

- 点击「报表中心 → 等保自评月报」
- 系统按等保 2.0 三级模板自动生成 Word 文档，含：
  - 资产清单（**目标 1**）
  - 漏洞处置率 / 基线达标率 / 告警闭环率（**目标 2 + 3 KPI**）
  - 本月攻击事件清单 + Storyline 截图（**目标 3 · R-7**）
  - 修复任务清单 + 灰度发布历史 + 回滚记录（**目标 3 · R-2**）
- 一键导出，签字盖章。**目标 3 复盘闭环**。

### 5.5 17:00 — 准备切 protect

模式管理页提示「该租户已运行 95 天，符合 G1 数据沉淀期；月度误报率 1.6%（达标）；用户反馈准确率 87%（达标）；数据回放命中率 86%（达标）」。详见 [`operating-modes.md`](operating-modes.md) §3。

李工发起「切 protect 申请」，范围限定 `env=dev` 标签的 200 台开发机，灰度策略 5% → 25% → 100%。Manager 校验 6 门槛通过，进入审批环节。

明天早会上跟客户安全负责人 + 业务方共同审签后，灰度开始执行。**三段闭环 + 模式升级路径 全部走通**。

---

## 6. 与七大模块的总映射

| 模块（ref 文档） | 目标 1 看清 | 目标 2 算清 | 目标 3 处清 |
|------|----------|----------|----------|
| 01 服务端架构 | ✅ 资产 API / 拓扑 / 态势 | ✅ 风险中心 / 告警中心 | ✅ 修复编排 / Playbook / 报表 |
| 02 Agent | ✅ 22+ 类清点 / EDR 流量 | ✅ 入侵检测六件套（部分） | ✅ 命令执行 / 隔离 / 包升级 |
| 03 基线 | ✅ 配置清点 | ✅ 等保 / CIS 评分 | ✅ 一键 fix + 回滚 |
| 04 运行时 EDR | ✅ 进程 / 网络 / FIM 事件 | ✅ 行为检测 + ATT&CK | ✅ IP 封禁 / PAM / kill |
| 05 容器 K8s | ✅ Pod / 镜像 / 命名空间 / 流量 | ✅ Admission / 镜像漏洞 / K8s CIS | ✅ Admission deny / NetworkPolicy |
| 06 漏洞 | ⚠️ 仅清单 | ✅ CVE / EPSS / SBOM | ✅ 灰度修复 / NPatch（M2） |
| 07 病毒 | — | ✅ YARA-X 扫描 + 引擎 | ✅ 隔离箱 / 反勒索（M1） |

> 七模块差距评级与人月详见 `ref/00-总体评估与商业化路线.md` §2.2。

---

## 7. 与微服务架构的总映射

| 微服务 | 看清 | 算清 | 处清 |
|--------|------|------|------|
| **Manager** | 资产 / 拓扑 / 态势 API | 风险中心 6-tab | 修复编排 / 审批 / 报表 / audit |
| **AgentCenter** | gRPC 接入 / 心跳 | — | 任务下推 / Canary 灰度 |
| **Consumer** | 资产 / 事件入库 | alert / vulnerability 持久化 | remediation_result / audit_log |
| **Engine** | 拓扑构建 / 态势计算 | CEL / 序列 / ML / Storyline / ATT&CK | action 下发（仅 protect） / Playbook 触发 |
| **VulnSync** | — | 11 源 advisory 融合 | — |
| **LLMProxy** | NL 查资产（可选） | 告警解释 / 误报降权（可选） | Storyline 总结 / Plan 起草（可选） |

---

## 8. 路线图对齐

> 详见 `ref/00-总体评估与商业化路线.md` §4。

### 8.1 MVP（0-3 月）三段交付物

| 段 | MVP 必交付 |
|------|-----------|
| 看清 | 22+ 类资产 + EDR 南北向流量 + 基础态势大屏 |
| 算清 | 入侵检测六件套（三件 P0：暴力破解 / 异常登录 / Web 后门） + 等保 100 条 + CIS RHEL 8 L1 100 条 + 信创 4 源 advisory + EPSS + 弱口令 |
| 处清 | RemediationPlan v1（pre / verify / rollback） + iptables 自动响应 + 隔离箱端到端 + 等保 docx 报表 |

### 8.2 M1（3-6 月）增量

| 段 | M1 增量 |
|------|---------|
| 看清 | 微隔离 v1（DaemonSet eBPF FlowCollector + 拓扑） + 容器富化 |
| 算清 | Java RASP MVP + Anti-Rootkit + 反勒索 honeypot + ATT&CK 80→200 + 多源 TI |
| 处清 | 灰度修复 1k 主机批次 + Playbook SOAR + 合规审计引擎 + 5 种外发连接器 |

### 8.3 M2（6-12 月）增量

| 段 | M2 增量 |
|------|---------|
| 看清 | 多 Region 部署 + 拓扑可视化大屏 v2 + SaaS 计费 |
| 算清 | NPatch 虚拟补丁 + RASP 五栈 + 自研补丁库 5w+ + 跨主机攻击链 |
| 处清 | 微隔离 v2（enforce） + Admission v2 + MSSP 控制台 + CNNAP 认证 |

---

## 9. 与对标产品的对照

| 维度 | mxsec | 青藤万象 | 青藤蜂巢 |
|------|-------|---------|---------|
| 看清 · 资产清点类目数 | 22+（目标） | 22 类清点 + 800+ 业务应用 | 容器维度 + 镜像血缘 |
| 看清 · 流量东西向 | M1 微隔离 v1 | 微隔离扩展包 | 蜂巢内置 |
| 看清 · 态势大屏 | MVP 基础 + M1 完整 | 完整 | 完整 |
| 算清 · 入侵检测锚点 | 六件套（MVP P0 三件） | 8 锚点 | 容器侧偏多 |
| 算清 · 基线规则数 | 212 → 800+ → 1500+（M2） | 1500+ | 容器侧 200+ |
| 算清 · 漏洞源 | 11+ 源 + 信创 4 源 | NVD + 商业源 | 镜像 CVE 主 |
| 算清 · ATT&CK 覆盖 | 32 → 80（MVP）→ 200+（M2） | 200+ | 容器 ATT&CK |
| 处清 · 修复灰度 | CanaryRollout v2（必备） | 修复 + 验证闭环 | — |
| 处清 · NPatch 虚拟补丁 | M2 引入 | 独立产品 | — |
| 处清 · MSSP 父子租户 | Phase 2 内置 | 商业模块 | — |
| **运行模式** | **observe → protect 双模式 + 6 门槛准入** | 检测 + 部分自动响应 | 检测 + 部分自动响应 |
| **多租户** | from-day-1 | 商业模块 | 商业模块 |

> mxsec 在"模式准入"和"多租户骨架"两条线**领先两家**，在能力厚度（规则数 / RASP / NPatch / 微隔离）上**距 M2 终态仍有 12-15 个月工程纵深**。

---

## 10. 边界与不做的事

| 不做 | 原因 |
|------|------|
| Windows 主机 | 与开源 CWPP 定位不符；运行时 hook 路径与 Linux 不重叠 |
| macOS / Unix 小机 | 客户群极小 |
| 端点 EDR（员工电脑） | EDR 与 CWPP 是两条产品线 |
| 网络流量 NDR（旁路探针） | 不在 CWPP 边界 |
| WAF / API 安全 | 与 RASP 部分重叠，但应用层 WAF 不做 |
| 数据安全 / DLP | 与 CWPP 边界外 |
| 工业控制 / OT | 协议栈差异大 |
| 全自研杀毒引擎 | M1 走 ClamAV + YARA-X，M2 评估自研 |

> **取舍原则**：能用开源 fork 解决的，不自研；不在 Linux + K8s 边界内的，不上车。

---

## 11. 总结

| 段 | 一句话 |
|------|-------|
| 看清 | 主机 / 容器 / 流量 / 态势全维度，覆盖青藤"资产 + 关系"两维 |
| 算清 | 漏洞 / 基线 / 运行时威胁 / 配置 / 弱口令五维全量化，覆盖青藤"活动"维 |
| 处清 | 修复 + 应急 + 复盘三段闭环，覆盖青藤"操作"维 + NIST Protect/Respond/Recover |

**三段共同遵守的工业级约束**：

1. **默认监听**（observe），磨合达标后切防护（protect），不上来就阻断业务
2. **多租户 from-day-1**，所有数据带 `tenant_id`
3. **灰度发布 + 回滚**，任何写操作都走 CanaryRollout v2
4. **可观测**，OTel 全链路 + Prometheus 指标 + audit_log 6 月不可删
5. **本地 ML 主导，LLM 可选**，离网政企首选 Smart 档位，AI-Native 仅有公网客户

**三段失败的反例（不可踩坑）**：

- 资产盲区 → 攻防演练 30% 主机不在管，红队比甲方更懂家底
- 漏扫报告几千条无优先级 → 等保不过 → 0day 必中
- 告警空响、告警疲劳、告警闭环率 < 50% → SOC 失能 → MTTR 不收敛

**最终交付的产品语言**：

> mxsec 让客户**看清**家底，**算清**风险，**处清**事件，**默认不打挂业务**，**磨合后自动响应**。

---

## 12. 参考文档

- [`architecture.md`](architecture.md) — 六微服务架构总图
- [`operating-modes.md`](operating-modes.md) — 监听 / 防护双模式
- [`multi-tenant.md`](multi-tenant.md) — 多租户 from-day-1
- [`engine-design.md`](engine-design.md) — Engine 检测引擎
- [`engine-detection-design.md`](engine-detection-design.md) — 检测设计细节
- [`vulnsync-design.md`](vulnsync-design.md) — VulnSync 11 源融合
- [`vuln-module-design.md`](vuln-module-design.md) — 漏洞模块设计
- [`llmproxy-design.md`](llmproxy-design.md) — 多 LLM 适配
- [`ml-models.md`](ml-models.md) — 本地 ML 模型清单
- [`falco-sigma-integration.md`](falco-sigma-integration.md) — Falco / Sigma 转 CEL
- [`asset-model.md`](asset-model.md) — 资产统一模型
- [`api-reference.md`](api-reference.md) — 全 API
- `ref/00-总体评估与商业化路线.md` — 路线与差距
- `ref/01-服务端架构.md` ~ `ref/07-病毒.md` — 七模块深度报告
- `ref/appendix/青藤万象-能力清单.md` — 万象功能清单
- `ref/appendix/蜂巢-能力清单.md` — 蜂巢功能清单
