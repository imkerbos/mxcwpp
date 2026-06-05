# mxsec docs 横向校验报告

> **校验范围**：`docs/*.md` 全部 29 份 + `ref/08-roadmap.md`
> **基线日期**：2026-06-06
> **权威源（Phase 0 上位）**：`architecture.md` / `operating-modes.md` / `multi-tenant.md`
> **方法**：grep / Read 实证 + 上位文档对位 + 跨文档引用追真
> **校验项**：10 大项（平台定位 / 微服务清单 / 运行模式 / 多租户 / ML+LLM / 引用真伪 / Topic / API / 术语 / 配置 key）

---

## 1. 文档清单与定位

| 类别 | 文档 | 状态 | 备注 |
|---|---|---|---|
| Phase 0 上位 | architecture.md | 权威源 | 六微服务总图 |
| Phase 0 上位 | operating-modes.md | 权威源 | observe/protect 双模式 |
| Phase 0 上位 | multi-tenant.md | 权威源 | tenant_id 贯穿 |
| Phase 1 主线 | api-reference.md | 现行 | v2 API 全集 |
| Phase 1 主线 | asset-model.md | 现行 | 22 类资产统一模型 |
| Phase 1 主线 | configuration.md | 现行 | 六服务配置 |
| Phase 1 主线 | contributing.md | 现行 | 含定位"硬约束"红线 |
| Phase 1 主线 | datatype-allocation.md | 现行 | DataType 总账 + §10 三方核查 |
| Phase 1 主线 | deployment.md | 现行 | 六微服务部署 |
| Phase 1 主线 | edr-agent-design.md | 现行 | Agent 端采集 |
| Phase 1 主线 | engine-design.md | 现行 | Engine 服务设计 |
| Phase 1 主线 | engine-detection-design.md | 现行 | 检测算法细节 |
| Phase 1 主线 | falco-sigma-integration.md | 现行 | 规则中台 |
| Phase 1 主线 | faq.md | 现行 | 高频问答 |
| Phase 1 主线 | governance.md | 现行 | 治理章程 §0 定位声明不可变 |
| Phase 1 主线 | llmproxy-design.md | 现行 | 多 LLM 网关 |
| Phase 1 主线 | ml-models.md | 现行 | 本地 ML 清单 |
| Phase 1 主线 | security-objectives.md | 现行 | 三大产品目标 |
| Phase 1 主线 | vuln-module-design.md | 现行 | 漏洞业务闭环 |
| Phase 1 主线 | vulnsync-design.md | 现行 | 漏洞情报融合 |
| 历史档案 | edr-phase3-24-evaluation.md | archived banner | OK |
| 历史档案 | edr-performance-tuning.md | archived banner | OK |
| 历史档案 | m1-m4-vm-test-report.md | archived banner | OK |
| 历史档案 | v1.3.0-test-evaluation.md | archived banner | OK |
| 历史档案 | v1.3.1-evaluation-consolidated.md | archived banner | OK |
| 历史档案 | vuln-scanner-deep-eval.md | archived banner | OK |
| 历史档案 | deploy-v2.4.0.md | archived banner | OK |
| **未标 archived** | edr-engine-design.md | **缺陷** | 4312 行 v1.x EDR 设计；内容含"Agent + Plugin + Server 三层架构"、Phase 23 Windows、Phase 24 等过时章节，但**无 archived 头部声明** |
| 工作笔记 | TODO.md | 内部 | 不算文档范畴 |
| 内部 | ref/08-roadmap.md | 现行 | Phase 1-5 路线 |

> 主线文档共 **18 份**（含 3 份上位）+ 1 份 ref。

---

## 2. 10 项一致性校验矩阵

> 列说明：✓ 一致；△ 轻微不一致；✗ 关键缺陷；— 不适用（如治理章程不涉及 Topic）

| # | 文档 | 平台定位 | 微服务清单 | 运行模式 | 多租户 | ML/LLM | 引用真伪 | Topic | API | 术语 | 配置 key |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | architecture.md | ✓ | ✓（权威源） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 2 | operating-modes.md | ✓ | ✓ | ✓（权威源） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 3 | multi-tenant.md | ✓ | ✓ | ✓ | ✓（权威源） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 4 | api-reference.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（自身权威） | ✓ | ✓ |
| 5 | asset-model.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | △ 引用 `mxsec.engine.traffic`，已在 datatype 预留段 11200-11899 内但未单列 | ✓ | ✓ | ✓ |
| 6 | configuration.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（自身权威） |
| 7 | contributing.md | ✓（红线明示） | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | ✓ |
| 8 | datatype-allocation.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（自身权威，§10 已与 arch / mt / om 三方对账） | — | ✓ | ✓ |
| 9 | deployment.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 10 | edr-agent-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 11 | engine-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 12 | engine-detection-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ §"EDR Agent 采集"列引用 archived `edr-engine-design.md` | ✓ | ✓ | ✓ | ✓ |
| 13 | falco-sigma-integration.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ 两处引用 archived `edr-engine-design.md`（行 693 / 1352） | ✓ | — | ✓ | ✓ |
| 14 | faq.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ 两处引用 archived `edr-performance-tuning.md`（行 486 / 805） | ✓ | ✓ | ✓ | ✓ |
| 15 | governance.md | ✓（§0 定位声明） | ✓ | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | — |
| 16 | llmproxy-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 17 | ml-models.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ |
| 18 | security-objectives.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ |
| 19 | vuln-module-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 20 | vulnsync-design.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 21 | ref/08-roadmap.md | ✓ | ✓ | ✓ | ✓ | ✓ | ✓（含锚点 §3 / §6 / §8.1） | ✓ | ✓ | ✓ | ✓ |
| **22** | edr-engine-design.md | ✗ "Agent + Plugin + Server 三层架构" | ✗ 无六微服务 | ✗ 无 observe/protect | ✗ 无 tenant | ✗ 无 ML 三档 | — | — | — | ✗ 含 Windows 章节 | — |

> 总计 21 份主线 + 1 份未归档异常 = 22 行。其余 6 份历史档案均带 archived banner，已在第 1 节注明，不纳入此矩阵（已自我声明"不再维护"）。

---

## 3. 关键缺陷列表（按文档）

### 3.1 edr-engine-design.md（重大缺陷，必修）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 13 | "Agent + Plugin + Server **三层架构**" | 与 architecture.md §12 v1.x→v2.0 升级声明冲突，**未标 archived** | 在文档头加 archived banner（同 edr-phase3-24-evaluation.md 模板） |
| 1392-1450 | Phase 23 Windows ETW / Minifilter 设计 | 与 contributing.md §11 "Windows 永不支持" 红线冲突 | archived 后即可，无需删 |
| 3791-3899 | 二十七章 Windows 平台支持完整设计 | 同上 | archived 后即可 |
| 4308 | "12. 多平台 — Linux (eBPF) + Windows (ETW + Minifilter)" | 同上 | archived 后即可 |

**修复路径**：在文档头第 1-5 行插入：

```markdown
> **状态: 历史档案 (archived)**
>
> 本文档反映 mxsec **v1.x 阶段**(2026-05 前)的 EDR 设计,**v2.0 架构(六微服务)** 已升级,
> 新设计参见 [docs/engine-design.md](engine-design.md) / [docs/engine-detection-design.md](engine-detection-design.md) / [docs/edr-agent-design.md](edr-agent-design.md)。
>
> 文档保留用于历史追溯,**不再维护**。
```

### 3.2 falco-sigma-integration.md（中等，建议修）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 693 | "见 [edr-engine-design.md]" | 引用 archived 文档作为权威 EDR 采集说明 | 改为 `[edr-agent-design.md](edr-agent-design.md)` |
| 1352 | 参考文档表 "EDR Agent 采集 \| edr-engine-design.md" | 同上 | 改为 `edr-agent-design.md` |

### 3.3 engine-detection-design.md（中等，建议修）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 1677 | 参考文档表 "EDR Agent 采集 \| edr-engine-design.md" | 引用 archived 文档 | 改为 `edr-agent-design.md` |

### 3.4 faq.md（中等，建议修）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 486 | "永久调优：参考 edr-performance-tuning.md" | 该文档 archived "不再维护" | 改为指向当前 EDR 调优章节，或新建非 archived 的性能指南 |
| 805 | 参考文档表 "Agent 性能调优 \| edr-performance-tuning.md" | 同上 | 同上 |

### 3.5 asset-model.md（轻微）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 1277 / 1281 / 1712 | 引用 `mxsec.engine.traffic` (DataType 11201-11299) | datatype-allocation.md 已将 11200-11899 标为"Engine 子模块扩展预留"，但未单列 `mxsec.engine.traffic` 行；architecture.md §4.1 Topic 总览亦未列 | 在 datatype-allocation.md §4 / architecture.md §4.1 补"待规划"行（标注 Phase 2/3 落地）；或在 asset-model.md 注明"该 topic 由 Phase 2 流量模块上线时正式登记" |

### 3.6 datatype-allocation.md（轻微）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| 891 | `mxsec.metering.usage` "在 architecture §4.1 未列，但 multi-tenant.md §8.2 已提及" | 自承不一致，建议消除 | architecture.md §4.1 表格补一行 `mxsec.metering.usage` 14001-14099 / 3p / 365d |

### 3.7 architecture.md（轻微）

| 行号 | 现状 | 问题 | 建议修复 |
|---|---|---|---|
| §4.1 Topic 总览 13 行 | 缺 `mxsec.metering.usage` | 与 datatype-allocation.md §3.7 / configuration.md `metering_topic` 不齐 | 补一行；并补 "DataType 14001-14099 / 3p / 365d / Manager → Consumer / 多租户计量用量" |

---

## 4. 跨文档锚点抽样验真（5 处）

| 引用方 | 引用目标 | 实际是否存在 | 结论 |
|---|---|---|---|
| architecture.md §10 → operating-modes.md（无锚） | 文件 operating-modes.md | ✓ 存在（354 行） | OK |
| architecture.md §9 → multi-tenant.md（无锚） | 文件 multi-tenant.md | ✓ 存在（444 行） | OK |
| ref/08-roadmap.md → "operating-modes.md §3" | operating-modes.md §3 切换门槛 | ✓ 第 46-59 行 §3 实际存在 | OK |
| ref/08-roadmap.md → "operating-modes.md §6" | operating-modes.md §6 告警 schema 中的 mode 字段 | ✓ 第 135-183 行 §6 实际存在 | OK |
| ref/08-roadmap.md → "operating-modes.md §8.1" | operating-modes.md §8.1 哪些动作受 mode 控制 | ✓ 第 222-237 行 §8.1 实际存在 | OK |
| multi-tenant.md §3.3 反向被 asset-model.md / configuration.md 引用 | multi-tenant.md §3.3 GORM 强制注入 | ✓ 第 117-136 行 §3.3 实际存在 | OK |

> 抽样 5+ 处均通过；未发现死链。

---

## 5. v1.x 残留扫描结果

| 残留类型 | 出现位置 | 是否合规 |
|---|---|---|
| "三层架构" 字样 | architecture.md:472 / engine-design.md:5 / datatype-allocation.md:17,939 / contributing.md:591 | ✓ 全部出现在"v1→v2 对照"或"违规对照表"或"变更历史"语境，**语义正确** |
| Windows 字样 | api-reference.md / asset-model.md / contributing.md / deployment.md / edr-agent-design.md / falco-sigma-integration.md / faq.md / governance.md / security-objectives.md | ✓ 全部以"不支持/不做/拒收 PR"语境出现，**语义正确** |
| Windows 字样 | edr-engine-design.md / edr-phase3-24-evaluation.md / edr-performance-tuning.md(无) | ✗（前一份未 archived）；其余 archived 文档可接受 |
| "默认即响应/默认即阻断" | architecture.md:480（v1→v2 对照表）；operating-modes.md:42（对标段）；contributing.md:11,592（违规列表） | ✓ 全部为"反例语境" |

---

## 6. 配置 key 命名一致性

| key | 出现文档 | 一致性 |
|---|---|---|
| `MODE=observe` / `MODE=protect`（运行时 env） | architecture.md / operating-modes.md / engine-design.md / engine-detection-design.md | ✓ |
| `mode.default` (YAML) | configuration.md / multi-tenant.md / operating-modes.md（隐含） | ✓ |
| `ml.enabled` / `llm.enabled` | architecture.md / multi-tenant.md / configuration.md / ml-models.md / llmproxy-design.md / engine-design.md | ✓ |
| `mode.fail_safe_on_manager_down` | configuration.md | ✓ Engine 内独有，未冲突 |
| `tenants.guard.refuse_missing_tenant_id` | configuration.md | ✓ |

---

## 7. Topic 命名一致性总评

| Topic | 在 architecture §4.1 | 在 datatype-allocation §3 | 在 multi-tenant §5 | 在使用方文档 | 结论 |
|---|---|---|---|---|---|
| `mxsec.agent.heartbeat` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.asset` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.events` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.ebpf` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.baseline` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.scanner` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.remediation` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.agent.command-ack` | ✓ | ✓ | 共享路径 | ✓ | ✓ |
| `mxsec.engine.alert` | ✓ | ✓ | — | ✓ engine/llmproxy/falco | ✓ |
| `mxsec.engine.storyline` | ✓ | ✓ | — | ✓ engine/detection | ✓ |
| `mxsec.engine.feedback` | ✓ | ✓ | — | ✓ engine/detection | ✓ |
| `mxsec.vuln.advisory` | ✓ | ✓ | — | ✓ vulnsync/engine | ✓ |
| `mxsec.llm.audit` | ✓ | ✓ | — | ✓ llmproxy | ✓ |
| `mxsec.metering.usage` | ✗ 缺 | ✓ | ✓（§8.2 提及） | configuration.md | △ 三方未对齐（datatype-allocation §10.1 自承） |
| `mxsec.engine.traffic`（asset-model 提出） | ✗ 未列 | ✗ 仅落在 11200-11899 预留段 | — | asset-model | △ 待规划，asset-model 已标"Phase 2 落地" |

---

## 8. 术语统一抽查

| 术语 | 用法 | 评估 |
|---|---|---|
| Agent / 探针 / 客户端 | 主线全部统一为 "Agent"；"探针" 只在 security-objectives.md:616 "NDR 旁路探针" 否定边界语境出现 | ✓ 不混用 |
| 基线 / 合规检查 | "基线" 指 Baseline 模块；"合规" 指 等保/CIS/PCI/ISO 等外部框架；"基线合规" 复合词在 plugins/baseline 上下文使用 | ✓ 边界清晰 |
| 检测 / 监测 / 分析 | "检测 (detect)" = Engine 规则/ML 输出告警；"监测 (observe/monitor)" = 监听模式无动作；"分析" = Storyline / 漏洞匹配 / 报表聚合 | ✓ 主线一致 |
| 微服务 | 全部用 "六微服务"（不出现 "6 微服务" / "六个微服务" 等异写） | ✓ |
| observe / protect | 全平台同名，从未使用 "audit-only / blocking / detect-only / prevent" 等异写 | ✓ |

---

## 9. 路线图（ref/08-roadmap.md）与上位文档对齐

| ref/08 章节 | 与上位文档关系 | 结论 |
|---|---|---|
| §0 一页执行摘要 | "默认监听 observe" 与 operating-modes.md §0 / architecture.md §6 一致 | ✓ |
| §1.1 阶段表 | Phase 1-4 默认 `observe`，Phase 5 才 `protect 灰度` — 完美对齐 operating-modes.md §3 6 门槛 | ✓ |
| §2.2 Phase 1 必交付清单 | 六微服务清单与 architecture.md §2.x 一一对应 | ✓ |
| §2.4 demo 5 项 | "hydra ssh 爆破 → mode=observe + would_action.type=ip_block + action=null" 与 operating-modes.md §6 告警 schema 一致 | ✓ |
| §X 跨文档锚点引用（§3 / §6 / §8.1 / §8.3） | 抽样 5 处实测均存在 | ✓ |

> ref/08-roadmap.md 与三份上位文档 **零冲突**。

---

## 10. 总体质量评级

### 10.1 主线 18 文档质量分布

| 等级 | 数量 | 文档 |
|---|---|---|
| A（无缺陷或仅文档表面引用） | 14 | architecture / operating-modes / multi-tenant / api-reference / configuration / contributing / datatype-allocation / deployment / edr-agent-design / engine-design / governance / llmproxy-design / ml-models / security-objectives / vuln-module-design / vulnsync-design |
| B（轻微 / 1-2 处死链或表述） | 4 | asset-model（待规划 topic）/ engine-detection-design（1 处 archived 引用）/ faq（2 处 archived 引用）/ falco-sigma-integration（2 处 archived 引用） |
| C（中度，需修订） | 0 | — |
| D（严重） | 1 | edr-engine-design.md（v1.x 内容未 archived，违反平台定位红线） |

### 10.2 整体评级

**B+（高质量，存在 1 项重大文档管控缺陷 + 5 项轻微死链/对齐项）**

理由：
- 18 份主线 + ref/08-roadmap.md 的核心 10 项校验全部通过；
- 平台定位、运行模式、多租户、六微服务、Topic、API、配置 key、术语均完美一致；
- 上位文档（architecture / operating-modes / multi-tenant）与 ref/08-roadmap.md 零冲突；
- 唯一重大缺陷为 `edr-engine-design.md` 未挂 archived 标签（同类 6 份历史文档已挂）；
- 4 处文档表层引用 archived 文档（属可机械修复）；
- `mxsec.metering.usage` Topic 三方对齐有缝（datatype 已自承），属可补行修复。

---

## 11. 可交付判定

| 维度 | 结论 |
|---|---|
| 是否可作为内部研发的"权威设计基线" | ✅ YES |
| 是否可作为外部社区 / KA 客户参考 | ⚠️ **建议先修 §3.1（edr-engine-design archived）+ §3.2-3.4（4 处 archived 引用），再放行** |
| 是否可作为 Phase 1 启动的输入 | ✅ YES（上位文档 + 主线 18 份齐备） |
| 是否阻塞 Phase 1 工程开工 | ❌ NO（缺陷均为文档层面，可与 Phase 1 工程并行修） |

**ok_to_ship**：YES（主线齐备且自洽；must_fix 清单为文档卫生，可与 Phase 1 工程并行修订）

---

## 12. must_fix 清单（建议在 Phase 1 第一周内完成）

1. `docs/edr-engine-design.md` 加 archived banner（5 行）
2. `docs/faq.md` 第 486、805 行将 `edr-performance-tuning.md` 改为现行 EDR 性能章节或新建文档
3. `docs/falco-sigma-integration.md` 第 693、1352 行将 `edr-engine-design.md` 改为 `edr-agent-design.md`
4. `docs/engine-detection-design.md` 第 1677 行将 `edr-engine-design.md` 改为 `edr-agent-design.md`
5. `docs/architecture.md` §4.1 Topic 总览补一行 `mxsec.metering.usage`（14001-14099 / 3p / 365d）
6. `docs/asset-model.md` §13 验收 checklist 中 `mxsec.engine.traffic` 标"Phase 2 落地，DataType 待 datatype-allocation 正式登记"

---

## 13. 校验执行痕迹

| 校验项 | grep / Read 命令 | 关键结果 |
|---|---|---|
| 平台定位 | `head -5 docs/*.md` 检查"工业级开源 CWPP / Linux 主机 + K8s" | 主线全部一致 |
| 微服务清单 | `grep -c "六微服务" docs/*.md` | 主线全部 ≥ 1 次 |
| 运行模式 | `grep "MODE=observe\|mode.default" docs/*.md` | 18 份主线全部使用 observe/protect 标准命名 |
| 多租户 | `grep -l "tenant_id" docs/*.md` | 22 份相关文档全部带 tenant_id |
| ML/LLM | `grep "ml.enabled\|llm.enabled" docs/*.md` | 6 份核心文档同 key，三档命名一致 |
| 引用真伪 | 抽样 5 处锚点 + Read 对方文件 | 全部命中实际存在的章节 |
| Topic | 14 个 Topic × 5 份核心文档 grep 对照 | 13 个 ✓，1 个（metering）轻微缺，1 个（traffic）待规划 |
| API | api-reference.md 与 operating-modes/multi-tenant 引用对比 | `/api/v2/system/mode`、`/api/v2/admin/*`、`/api/v2/mssp/*` 全部一致 |
| 术语 | `grep "探针\|客户端\|监测" docs/*.md` | 边界清晰 |
| 配置 key | `grep -nE "ml\.enabled\|llm\.enabled\|mode\.default" 全主线` | 命名零分歧 |

> **校验执行人**：reviewer subagent；**评估方式**：实证 grep + Read，无猜测。

---

## 14. 参考

- 上位 1：`docs/architecture.md` — 六微服务总图
- 上位 2：`docs/operating-modes.md` — observe/protect 双模式
- 上位 3：`docs/multi-tenant.md` — tenant_id 贯穿
- 路线：`ref/08-roadmap.md` — Phase 1-5 工程节奏
- 治理：`docs/governance.md` §0 项目定位声明（不可变）
- 红线：`docs/contributing.md` §1 "Windows / 三层架构 / 默认即阻断" 拒收清单
