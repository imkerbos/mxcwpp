# 路线图与交付状态

> **这份文档是交付状态的唯一权威来源。** 与代码或其它文档冲突时以本文档为准，
> 并当场修正落后的那一份。
>
> 最后核实：2026-08-04
>
> 本仓库公开。涉及具体部署环境的迁移步骤、主机规模与运维细节不在此文档，
> 留在 `local-reports/`（不入库）。
>
> 接手开发前请先读 `local-reports/session-handoff-20260804.md`——
> 那里有禁区清单（哪些同名包不能删）、接线新检测的停止条件，以及环境与工具坑。

---

## 一、平台规模（实测，非估算）

| 维度 | 数量 |
|------|------|
| 后端服务 | 7（manager / agentcenter / consumer / engine / vulnsync / llmproxy / scanner）|
| 检测能力（Stage）| 14 定义，**14 已接线，0 未接线** |
| Agent 插件 | 12 |
| 前端页面 | 83 |
| 基线策略 | 30 个 / 614 条规则 |
| 内置 CEL 检测规则 | 259 |

> 「未接线」= 有构造器但从未接入流水线。清单见 `internal/server/engine/capability.go`，
> 有 CI 门禁校验清单与代码一致。**不要按未接线的代码去理解线上行为。**

---

## 二、能力交付状态

平台已交付的能力域：

- Linux 基线合规（等保 2.0 三级 / CIS）、中间件基线、K8s 容器基线
- 漏洞管理（情报同步 / 匹配 / 修复下发 / 陈旧核对）
- EDR 事件采集与 CEL 检测、行为基线引擎（BDE）
- FIM 文件完整性、资产采集、报表与 PDF 导出
- RBAC（action 级 + 自定义角色）、安全审计
- 事件运营闭环（Case 生命周期 / 值班表 / 处置审批与回滚）
- 检测工程（规则生命周期、标注语料回放）
- ML 异常检测（分档治理，见 §五）
- SOC 态势感知大屏

各能力的接线状态以 `internal/server/engine/capability.go` 为准，有 CI 门禁校验。

---

## 三、进行中 / 未完成

### 5.1 ML 治理（约 70%）

已做：漂移与投毒防护、精确率口径（仅人工研判）、升档门槛、模型持久化与版本回滚、
`ranking` 档、按主机配额训练采样、阈值可配。

**未做（4 项，全部卡在同一个前提：缺标注的主机指标时序语料）**：

- 攻击回放标注
- 时间切分验证（train/test 按时间分割）
- recall 测量
- PR / F1 曲线

> 这四项**不要用其它数据凑**。检测规则那套回放语料是进程/文件事件，
> 不覆盖主机指标时序；拿它充数只会得到一个看起来有 recall 的假数字。

### 5.2 检测工程

标注语料目前 21 样本 / 8 个 ATT&CK 技术，覆盖面偏小。
`UncoveredTechniques()` 已实现但尚未接入「应覆盖技术清单」作为 CI 门禁。
回放只覆盖 CEL 规则，未覆盖 sequence / IOC / 行为引擎。

### 5.3 未接线代码

2026-08-03 清理了一批：删掉 `engine/honeypot`（空 Detector，对 7020-7029 段事件
无条件产 critical 告警，接上即刷屏）、`stage_kube`（永远返回 nil alerts 的旁路）、
`engine/ml` 与 `stage_ml`（与在跑的 `engine/anomaly` 重复）、`engine/scheduler`
（零调用方，实际跑的是 `agentcenter/scheduler`）、`stage_audit` 与 `manager/biz/audit`
（死生产者-死消费者配对；在跑的是 `internal/server/audit`）。

2026-08-04 接线 `abnormal_login`，Stage 侧不再有未接线项。它的两个阻塞都已解除：
冷启动刷屏由每主机学习期治住（默认 7 天且 ≥10 次登录，两个条件都满足才退出），
画像落 `host_login_profile_states`（启动恢复 + 5 分钟 checkpoint），重启后学习期不重来。
学习期是静默的，`mxcwpp_engine_abnormal_login_hosts_learning / _hosts_graduated /
_suppressed_total` 三个指标一并导出，并配了「8 天无一台毕业」的告警——
否则「一切正常」和「检测没生效」在告警面上都是零条。
接线同时收窄了事件面：`pam_unix(sudo:session)` / `(cron:session)` 等本地会话不再当登录，
它们的量比真实登录高几个数量级，会让主机靠噪声凑够样本毕业。

剩余未接线：

| 位置 | 状态 |
|------|------|
| `internal/server/manager/biz/mlmodel/` | 模型分发链路（上传/审批/订阅/下发）完整实现，零路由零调用方。**保留待接线**；连带 `model/ml_model.go` 的 3 张表 |

---

## 四、未经验证的部分

**不要把下列内容当成已验证。**

| 项 | 状态 |
|----|------|
| Agent 装机链路（`install.sh` → systemd → 真机启动）| **从未在真实 Linux 机器验证**。只有单测 |
| Kafka → detector 事件通路 | 本地容器网络限制，未跑通 |
| 前端交互行为 | 只验了页面可加载、接口字段匹配、i18n key 存在；**没有浏览器实际点击验证** |
| ML recall | 完全没有测量（见 §5.1）|

---

## 五、目录与文档规范

### 目录约定

| 目录 | 用途 |
|------|------|
| `cmd/` | 各服务与工具入口，一个子目录一个二进制 |
| `internal/agent/` | Agent 侧代码 |
| `internal/server/` | 服务端，按服务再分子包 |
| `internal/common/` | 跨端共享库 |
| `internal/deploy/` | 部署产物与**门禁测试**（路由清单、告警覆盖、文档同步等）|
| `plugins/` | Agent 插件，每个可独立编译 |
| `web/` | 前端（Next.js）。**没有 `ui/`**——Vue 版已下线 |
| `deploy/` | 编排、配置模板、证书与部署脚本 |
| `docs/` | 对外文档，受门禁校验 |
| `local-reports/` | 本地评估报告，**gitignore，不入库** |

不入库的：`.scratch/`、`.superpowers/`、`local-reports/`、
`*.bck.yml`（golangci migrate 的备份，旧配置在 git 历史里可回溯）。

### 文档与代码同步

改动与文档的对应关系见 [CLAUDE.md · 文档实时同步](../CLAUDE.md)。
其中有门禁的部分由 `internal/deploy/docs_sync_test.go` 在 `go test` 中强制：

| 门禁 | 拦什么 |
|------|--------|
| `TestDocLinksResolve` | 文档里指向仓库内但不存在的路径 |
| `TestRoadmapNumbersMatchReality` | 本文档的规模数字与代码实际不符 |
| `TestArchitectureDocCoversAllServices` | 新增服务未写进架构文档 / CLAUDE.md |
| `TestConfigKeysDocumented` | 配置模板里的节未写进配置文档 |
| `TestBaselinePolicyCountMatchesDoc` | 基线策略数量与 README 声明不符 |
| `TestClaudeMdReferencesExist` | CLAUDE.md 引用了不存在的文档 |
| `TestRoadmapExistsAndIsDated` | 本文档缺失或没有核实日期 |

这些门禁跑在 CI 的 **Build & Test** job 里。该 job 必须保持阻塞——
门禁只有在构建会因它失败时才起作用。

---

## 六、待办清单（backlog）

> 标「需前置条件」的，等的不是工时。

### 需决策

| 项 | 说明 |
|----|------|
| `biz/mlmodel` 接线 | 模型分发链路已完整实现，零路由零调用方。接上要做端到端验证并补路由文档；见 §5.3 |

### 需前置条件

| 项 | 卡在哪 |
|----|--------|
| ML recall / 时间切分 / PR·F1 / 攻击回放标注 | **缺标注的主机指标时序语料**。要人去标数据，不是写代码 |
| 检测语料扩充 | 当前 21 样本 / 8 技术。需补 ATT&CK 覆盖清单并逐技术标注 |

### 需要真实环境

| 项 | 说明 |
|----|------|
| Agent 装机链路验证 | 需真实 Linux 靶机走完 `install.sh` → systemd → 启动 |
| Kafka → detector 通路 | 本地容器网络限制，未跑通 |
| 前端交互验证 | 已验页面可加载、接口字段匹配、i18n key 齐全；交互行为需浏览器实际点击 |
| `abnormal_login` 真实误报面 | 已接线，标注语料的正常运维登录零命中，但语料是构造的。真实机群头 7 天全静默，第 8 天起看 `mxcwpp_engine_abnormal_login_suppressed_total` 与实际告警量，再判要不要收窄 |

### 工程债

| 项 | 说明 |
|----|------|
| `UncoveredTechniques()` 未接门禁 | 已实现，缺「应覆盖技术清单」作为输入 |

### 漏洞情报：跨发行版标签污染（已根治，存量待回填）

CVE 的 `source` 与 `fixed_version` 是 CVE 级塌缩值。合并多来源 advisory 时，
此前只按 confidence 挑赢家，**不看该 advisory 是否匹配到本环境的主机** ——
一条毫不相关的 debian advisory 能把 rpm 主机的修复版本标成 deb 版本，
运维照着修根本修不掉。

已完成：

| 项 | 改动 |
|----|------|
| P0（早先上生产）| 清理侧加覆盖性守卫；修复闸以 precheck 的主机真值为准 |
| **P1 根治** | `mergeByConfidence` 引入覆盖性判据：匹配到主机的 advisory 优先于 confidence 更高但不适用的（`coordinator.go` `betterMetadata`）|
| **P2 展示** | 漏洞详情页的受影响主机表新增「当前版本 / 适用修复版本」两列，取 per-host 的 `matchedFixedVersion` 而非 CVE 级塌缩值 |

**存量数据待回填**：`host_vulnerabilities` 共 15468 行，其中只有 264 行
（1.7%）填了 `matched_fixed_version`。该字段只在 advisory 同步路径写入，
历史行需要重跑一次同步才会补上。在那之前，详情页对多数主机显示「—」——
这是如实的「未知」，好过继续显示一个可能错误的塌缩值。

验证方式：重跑同步回填后，debian 标签挂在 rpm 主机上的链应归零，
详情页对 rpm 主机显示 el9 版本而不是 deb 版本。

> 更彻底的长期方案：`host_vulnerabilities` 落地每主机匹配到的 source 与
> fixed_version（该表已有 current_version），下游全读 per-host 真值，
> 彻底摆脱 CVE 级塌缩。

> 部署与升级相关的前置条件见 `local-reports/roadmap-internal.md`（不入库）。

---

## 七、文档维护约定

- 本文档在**每次任务完成或受阻时**更新，与代码同一个提交。
- 「状态」一栏只写核实过的结论，不写计划或预期。
- 未验证的事情写进 §七，**不要在正文里描述成已完成**。
- 文档链接失效、基线策略数量对不上、CLAUDE.md 引用不存在的文档，
  都会被 `internal/deploy/docs_sync_test.go` 在构建时拦下。
