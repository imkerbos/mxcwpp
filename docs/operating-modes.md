# 运行模式 — 监听优先 / 防护后置

> **产品哲学**：先看清，再动手。
>
> mxsec 默认部署即**监听模式**（`MODE=observe`），仅产生告警与建议，不执行任何阻断、隔离、kill、封禁等"动作类响应"。
> 平台在客户生产环境磨合至少 90 天、关键指标达标后，按租户 / 主机 / 规则三粒度灰度切换到**防护模式**（`MODE=protect`），开启自动响应。
>
> 这是工业级 CWPP 的成熟路径（CrowdStrike、SentinelOne、Wazuh、青藤万象 同型），原因：上来就阻断 = 业务被打挂 = 客户卸载。

---

## 1. 两种模式对比

| 维度 | `observe`（监听 / 默认） | `protect`（防护） |
|------|--------------------------|-------------------|
| **检测** | ✅ 全功能（规则 / 序列 / ML / Storyline / LLM） | ✅ 全功能 |
| **告警生成** | ✅ 写入 `mxsec.engine.alert` Topic，UI 实时推 | ✅ 同左 |
| **告警字段** | `mode: observe`，`would_action: <预期动作描述>` | `mode: protect`，`action: <实际动作>` |
| **自动响应** | ❌ 仅写 audit，不下发 | ✅ IP 封禁 / PAM 封停 / 端口封禁 / 进程 kill |
| **病毒隔离** | ❌ 仅产 detection，文件不动 | ✅ Agent 端搬迁到 `/var/lib/mxsec/quarantine/` |
| **Admission Webhook** | dry-run（仅 warn，不 deny） | enforce（真 deny） |
| **微隔离** | 仅采流量，不下策略 | 下策略（NetworkPolicy / eBPF 规则） |
| **修复任务** | 仅生成 Plan，不执行 | 执行（仍需用户审批） |
| **NPatch 虚拟补丁** | 仅命中事件，不阻断流量 | 阻断流量 |
| **RASP** | 仅检测 + 告警 | 阻断 + 抛异常 |
| **运行时 EDR** | 仅 detect | detect + 进程 kill / 隔离 |
| **业务影响** | 零（纯被动观察） | 有（可能影响业务） |
| **客户接受度** | 高（无负担） | 需深度信任 |
| **适用阶段** | **默认 + 数据磨合（≥ 90d）** | 磨合达标 + 客户授权 |

---

## 2. 为什么默认监听？

| 理由 | 说明 |
|------|------|
| **业务风险最小** | 客户最担心"一条命令把全网打挂"。监听模式零业务影响，部署 = 零风险。 |
| **数据沉淀** | 90 天真实生产数据是 ML 模型从"60% 准确"到"95% 准确"的必经路径。无数据空谈准确率没意义。 |
| **误报治理** | 误报先磨掉。规则 / 模型 / 阈值在真实环境暴露的问题，沉淀成"客户专属白名单 + 调优"。 |
| **客户信任建立** | 客户看到"告警是真的、误报很少、攻击链清晰"，30 天后愿意主动要求开防护。 |
| **合规友好** | 等保 / ISO 27001 都接受"检测 + 告警"作为合规交付，不强制阻断。 |
| **业内同路线** | CrowdStrike Falcon 默认 detect-only，SentinelOne 默认 detect，Falco 默认仅告警。**没有任何工业级 CWPP 默认即阻断**。 |

---

## 3. 监听 → 防护 切换门槛

切换不是开关，是**多维度准入流程**。以下 6 项**全部满足**才允许全局或租户级切 `protect`。

| 门槛 | 指标 | 阈值 |
|------|------|------|
| G1 数据沉淀期 | 该租户 / 主机持续监听运行 | **≥ 90 天** |
| G2 误报率 | Engine 月度误报率（用户标记） | **≤ 2%** |
| G3 告警准确率 | 用户确认为真威胁的告警占比 | **≥ 85%** |
| G4 数据回放 | 用历史已知攻击事件回放，Engine 命中率 | **≥ 85%** |
| G5 客户授权 | 客户安全运营团队明确书面同意 | **必须** |
| G6 灰度准备 | Playbook 灰度发布机制（CanaryRollout v2）就绪 | **必须** |

未达标的租户 / 主机 / 规则保持 `observe`，**不强制全网切换**。

---

## 4. 切换粒度（4 级灰度）

mxsec 不支持"全网一刀切"。`MODE` 配置必须分级：

```yaml
# /etc/mxsec/manager.yaml
mode:
  # 全局默认（最低优先级）
  default: observe

  # 租户级覆盖
  tenants:
    - id: t-bank-a
      mode: observe          # 银行 A 仍监听
    - id: t-internal
      mode: protect          # 内部测试租户已切防护

  # 主机标签级覆盖（在租户范围内）
  host_labels:
    - tenant: t-internal
      label: "env=dev"
      mode: protect          # dev 主机激进
    - tenant: t-internal
      label: "env=prod-core"
      mode: observe          # 核心生产仍保守

  # 规则级覆盖（精细到单条规则）
  rules:
    - rule_id: BRUTE_FORCE_SSH
      mode: protect          # 暴力破解全网都防（误报低）
    - rule_id: ML_ANOMALY_PROCESS
      mode: observe          # ML 异常类仍监听（误报高）
```

**优先级**：规则级 > 主机标签级 > 租户级 > 全局默认。

---

## 5. 切换流程（标准化）

每次切换都走流程，禁止 SSH 改配置文件直推。

```
[1] 安全运营人员发起切换请求（UI / API）
    └── 指定范围：全局 / 租户 / 主机标签 / 规则
    └── 指定灰度：5% → 25% → 100%
    └── 指定失败阈值：5% 主机告警率超基线则自动回退
        ↓
[2] Manager 校验 6 个门槛（G1-G6）
    ├── 全部满足 → 进入审批
    └── 任一未达 → 拒绝 + 显示原因
        ↓
[3] 审批（双人 + 客户签字）
    ├── 安全运营负责人审批
    └── 客户业务方授权
        ↓
[4] CanaryRollout 灰度推送
    ├── T0:  5% 主机切 protect（24h 观察）
    ├── T+1: 25% 主机切 protect（24h 观察）
    └── T+2: 100% 主机切 protect
        ↓
[5] 监控 + 自动回滚
    ├── 触发 audit + Slack/邮件 通知
    ├── 失败阈值触发 → 自动回 observe
    └── 客户随时一键回滚
        ↓
[6] 记录 + 报告
    └── 切换历史入 audit_log，6 月不可删
```

---

## 6. 告警 schema 中的 `mode` 字段

所有 Engine 产出的告警都带 `mode` 字段，前端 / SIEM 据此判断是否真处置。

```json
{
  "alert_id": "alrt-2026060100001",
  "tenant_id": "t-bank-a",
  "host_id": "h-12345",
  "rule_id": "BRUTE_FORCE_SSH",
  "severity": "high",
  "mode": "observe",
  "detected_at": "2026-06-01T10:23:45Z",
  "attack_chain": [...],
  "att_ck": ["T1110.001"],

  // observe 模式专属字段
  "would_action": {
    "type": "ip_block",
    "target": "192.0.2.45",
    "duration_sec": 3600,
    "reason": "5 次 SSH 登录失败"
  },

  // protect 模式专属字段（observe 模式为 null）
  "action": null,
  "action_result": null
}
```

`protect` 模式下：

```json
{
  "alert_id": "alrt-2026060100002",
  "mode": "protect",
  "would_action": null,
  "action": {
    "type": "ip_block",
    "target": "192.0.2.45",
    "executed_at": "2026-06-01T10:23:46Z"
  },
  "action_result": {
    "status": "success",
    "agent_ack_at": "2026-06-01T10:23:47Z",
    "iptables_rule_id": "MXSEC-AUTO-1234"
  }
}
```

---

## 7. 模式可见性

### 7.1 UI 指示

- 控制台**顶部固定 Banner** 显示当前全局模式 + 颜色（observe=绿、protect=红）
- 主机列表每行带 `mode` 标签（observe=蓝灯、protect=红灯）
- 告警详情页明确 `mode` + `would_action` / `action`

### 7.2 API 暴露

```
GET /api/v2/system/mode
{
  "default": "observe",
  "tenants": {...},
  "host_labels_overrides_count": 12,
  "rules_overrides_count": 8,
  "switched_at": "2026-05-15T08:00:00Z",
  "next_eligible_for_protect_at": "2026-08-15T00:00:00Z"
}
```

### 7.3 Prometheus 指标

```
mxsec_engine_alerts_total{tenant, mode, severity}
mxsec_engine_actions_executed_total{tenant, action_type, status}  # 仅 protect
mxsec_engine_actions_would_total{tenant, action_type}             # 仅 observe
mxsec_mode_switch_total{tenant, from, to}
```

---

## 8. 与功能模块的交互

### 8.1 哪些动作受 `mode` 控制

| 动作 | observe | protect |
|------|---------|---------|
| IP 封禁（iptables） | ❌ would | ✅ 执行 |
| PAM 登录封停 | ❌ would | ✅ 执行 |
| 端口封禁 | ❌ would | ✅ 执行 |
| 进程 kill | ❌ would | ✅ 执行 |
| 病毒文件隔离 | ❌ 仅 detection | ✅ Agent 搬迁 |
| 病毒文件删除 | ❌ would | ✅ 执行（需双确认） |
| K8s Admission Webhook | dry-run warn | enforce deny |
| 微隔离策略下发 | ❌ 仅采流量 | ✅ 下 NetworkPolicy / eBPF |
| NPatch 虚拟补丁 | ❌ 仅命中记录 | ✅ 阻断 |
| RASP 异常抛出 | ❌ 仅 detection | ✅ 阻断 + throw |
| 修复任务执行 | ❌ 仅生成 Plan | ✅ 执行（仍需用户审批） |

### 8.2 哪些动作**不受** `mode` 控制（任何模式都执行）

| 动作 | 原因 |
|------|------|
| 数据采集（EDR / 基线 / 资产 / 漏洞扫描） | 这是"看清"的基础，永远要做 |
| 告警生成 + UI 展示 | 这是"算清"的产物 |
| 通知（站内信 / 邮件 / Webhook） | 告警通知本身不算干预业务 |
| 报表生成 | 不影响业务 |
| 病毒库 / 规则库 / 漏洞库更新 | 内部数据流，不影响业务 |
| LLM 调用 / ML 推理 | 仅分析，不影响业务 |
| Agent 升级（带灰度） | 走 CanaryRollout，独立机制 |

### 8.3 用户主动触发的动作（任何模式都允许，但需鉴权 + audit）

- 用户在 UI 点击"立即修复"漏洞 → 执行（即便全局 observe）
- 用户在 UI 点击"立即隔离"病毒 → 执行
- 用户在 UI 点击"立即封禁 IP" → 执行
- 用户在 UI 创建"立即扫描"任务 → 执行

> **规则**：自动响应受 `mode` 控制，用户主动响应不受。

---

## 9. 数据磨合期的"反馈通道"

监听阶段的核心价值是**沉淀 + 反馈**。

### 9.1 用户标记反馈

每条告警 UI 提供 3 按钮：

- **真威胁** → `feedback.label = true_positive`，权重 +1
- **误报** → `feedback.label = false_positive`，规则 / 模型自动降权
- **不确定** → `feedback.label = uncertain`，进入人工复核队列

反馈数据写 `mxsec.engine.feedback` Topic，Engine 读取用于：

- CEL 规则白名单自动建议
- ML 模型增量训练（每周离线训练 + ONNX 重新打包 + 灰度发布）
- 阈值自动校准（IForest contamination 参数 / Markov 转移概率阈值）

### 9.2 自动磨合指标

Engine 持续上报：

```
mxsec_engine_precision{tenant, rule_id}  # 准确率 = TP / (TP+FP)
mxsec_engine_recall{tenant, rule_id}     # 召回率（需金标准数据集）
mxsec_engine_fp_rate{tenant, rule_id}    # 误报率
mxsec_engine_alerts_per_host_per_day{tenant, rule_id}  # 每主机每天告警数（用于评估"告警疲劳"）
```

满足 90 天 + `precision ≥ 0.95` + `fp_rate ≤ 0.02` 的规则，UI 提示"可建议切 protect"。

---

## 10. FAQ

**Q1：监听模式下平台还有用吗？**
A：完全有用。监听模式提供**告警 + 攻击链 + 资产 + 漏洞 + 基线 + 修复建议**全功能，等同于一套"高质量 SIEM + CWPP 资产管理 + 合规扫描器"。客户的 SOC 团队可以基于告警手动响应，与监听模式完全互补。

**Q2：客户能跳过监听直接上防护吗？**
A：技术上可以（manual override + 客户签字），但**强烈不建议**。无 90 天数据 = 模型未校准 = 高误报 = 业务事故风险。文档明确标注"manual override 路径"为非推荐配置。

**Q3：监听切防护是不是单向？**
A：不是。任何时间客户可一键回 observe，回滚秒级生效，Agent 收到 `MODE=observe` 后立即停止动作类响应。

**Q4：observe 期间的告警怎么处置？**
A：客户 SOC 手动响应（参考 `would_action` 字段提示），mxsec 提供"一键执行 would_action"按钮（鉴权 + audit）。

**Q5：observe 模式数据会少吗？**
A：不会。observe 与 protect 在**数据采集层完全一致**，只有"动作执行"差异。

**Q6：observe 期间是否需要持续付费/续约？**
A：是。商业版按 Agent 数计费，不区分模式。observe 期间的核心价值（告警 + 资产 + 漏洞 + 基线 + 报告）与 protect 等价。

---

## 11. 与对标产品对照

| 产品 | 默认模式 | 切换机制 |
|------|----------|----------|
| **mxsec** | `observe` | 6 门槛准入 + 4 级灰度 |
| CrowdStrike Falcon | Detection-only（默认） | 客户在控制台手动开启 Prevention 策略组 |
| SentinelOne | Detect（默认） | 策略组开启 Protect / Block |
| Wazuh | 告警（默认） | 通过 active-response 模块手动启用 |
| Falco | 仅告警（无原生响应） | 需外挂 Falco Talon |
| 青藤万象 | 检测 + 部分自动响应（出厂） | 入侵检测 6 模块开关 + 默认低危不自动处置 |

> **mxsec 是国内开源 CWPP 中第一个明确"监听优先"产品哲学并在产品层级硬约束的项目**。

---

## 12. 切换实施清单（运维侧）

- [ ] 该租户运行 ≥ 90 天
- [ ] Engine 月度 `mxsec_engine_fp_rate` ≤ 0.02（所有规则）
- [ ] 用户反馈准确率 ≥ 85%
- [ ] 数据回放历史攻击命中率 ≥ 85%
- [ ] 客户安全运营负责人签字
- [ ] 客户业务方书面授权
- [ ] CanaryRollout v2 灰度机制就绪并演练过
- [ ] 失败回滚机制演练过（≥ 1 次）
- [ ] 通知矩阵（Slack / 邮件 / 短信）配置完成
- [ ] 切换记录 + 切换原因入 audit
- [ ] 切换后第 1 / 7 / 30 天复盘

---

## 13. 参考文档

- [`architecture.md`](architecture.md) — 平台架构总图
- [`engine-design.md`](engine-design.md) — Engine 服务设计（mode 字段实现）
- [`multi-tenant.md`](multi-tenant.md) — 租户级 mode 覆盖
- [`api-reference.md`](api-reference.md) — `/system/mode` API
- [`security-objectives.md`](security-objectives.md) — 三大产品目标（监听是"是什么 + 为什么"，防护是"怎么做"）
- `ref/08-roadmap.md` — Phase 5 进入防护模式的产品决策
