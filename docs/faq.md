# 常见问题（FAQ）

> 本文档分两大块：
> **A. 产品定位与平台理解** — 新增常见问题（重新定位后必读，回答客户与社区在选型 / 部署 / 决策时的高频疑问）。
> **B. 运维与故障排查** — 平台 / Agent / 前端 / 数据库 / mxctl / 集群部署 / mTLS / 业务 / DLQ / 日志 / 错误码（保留 v1.x 全量内容）。
>
> 所有问答以 [`architecture.md`](architecture.md) / [`operating-modes.md`](operating-modes.md) / [`multi-tenant.md`](multi-tenant.md) 三份权威文档为准；与三份文档冲突时以其为准。

---

## A. 产品定位与平台理解

### A1. mxsec 支持 Windows 吗？支持 macOS 吗？

**不支持。**

mxsec 是一款**工业级开源 CWPP（Cloud Workload Protection Platform）**，专精 **Linux 主机 + Kubernetes 容器**，面向 ToB 政企 / 金融 / 互联网客户的服务器侧工作负载安全（不是终端 EDR）。

| 维度 | 支持范围 | 说明 |
|------|----------|------|
| Linux 主机 | ✅ 全栈 | RHEL / CentOS / Rocky / AlmaLinux / Ubuntu / Debian / Oracle Linux 主流发行版 + 信创 4 大 OS（详见 A7） |
| Kubernetes 节点 | ✅ DaemonSet | 1.22+ 全版本，支持 Docker / containerd / CRI-O / iSula 运行时 |
| Windows 服务器 | ❌ 不支持 | 短期不规划，远期视客户需求评估，但不进 v1.x / v2.x 路线 |
| macOS | ❌ 不支持 | macOS 不在 CWPP 服务器场景，永不进路线 |

**为什么不做 Windows？**

- ToB 服务器侧 90%+ 是 Linux，国内政企信创又 100% 是 Linux，客户 ROI 倒挂；
- Windows 安全栈（ETW / WFP / Defender API）与 Linux eBPF / Tetragon / Falco 几乎完全不相交，要做就是另开一个产品；
- mxsec 的核心竞争力（eBPF 内核观测 + 容器富化 + 信创适配）在 Windows 上无对应能力。

**详见**：[`architecture.md`](architecture.md) §1（六微服务 + Agent 部署形态）、[`deployment.md`](deployment.md) §16（信创 OS 支持矩阵）。

---

### A2. mxsec 默认是检测还是阻断？默认会不会"一把梭"把业务打挂？

**默认监听（observe），不阻断、不 kill、不隔离。**

mxsec 默认部署即 `MODE=observe`：**全量采集 + 全量检测 + 全量告警，但所有"动作类响应"全部抑制**。客户的业务进程、IP、端口、文件不会因为 mxsec 而被自动干预。

| 模式 | 默认 | 检测 / 告警 | 自动响应（封 IP / kill / 隔离 / Admission deny） |
|------|------|------------|----------------------------------------------|
| `observe`（监听） | ✅ | 全功能 | ❌ 抑制，仅写 `would_action` 字段供 SOC 参考 |
| `protect`（防护） | ❌ | 全功能 | ✅ 执行（IP 封禁 / PAM 封停 / 进程 kill / 病毒隔离 / NPatch 阻断 / RASP 抛异常） |

**从监听切防护必须满足 6 个准入门槛**（缺一不可）：

| 门槛 | 阈值 |
|------|------|
| G1 数据沉淀期 | 该租户 / 主机持续监听 ≥ **90 天** |
| G2 误报率 | Engine 月度误报率 ≤ **2%** |
| G3 告警准确率 | 用户标记真威胁占比 ≥ **85%** |
| G4 数据回放 | 历史攻击事件回放命中率 ≥ **85%** |
| G5 客户授权 | 客户安全运营负责人 + 业务方双书面签字 |
| G6 灰度准备 | CanaryRollout v2 灰度机制就绪 |

切换粒度支持**全局 / 租户 / 主机标签 / 规则**四级覆盖，优先级`规则级 > 主机标签级 > 租户级 > 全局默认`；灰度按 `5% → 25% → 100%` 三档推进，失败率超阈值自动回滚。

**用户主动触发的响应（UI 点"立即封禁 IP" / "立即隔离" / "立即修复"）任何模式都允许**，只受 RBAC + audit 控制。受 `mode` 约束的只有"Engine 自动响应"。

**完整哲学、切换流程、告警字段定义**：[`operating-modes.md`](operating-modes.md)。

---

### A3. 本地 ML 能离线跑吗？需要 GPU 吗？需要外网吗？

**全部本地 ONNX Runtime CPU 推理，无 GPU 需求，无外网依赖。**

| 维度 | 实现 |
|------|------|
| 推理框架 | ONNX Runtime v1.18+，纯 CPU AVX2 / AVX-512 优化 |
| 模型形态 | 10 个开源 ONNX 模型，预打包随 Engine 镜像分发（不联网下载） |
| 典型场景 | IForest 异常检测 / LightGBM 加权打分 / MiniLM 文本 Embedding / Markov 进程序列 / n-gram 命令异常 / 端口扫描序列 等 |
| 硬件需求 | Engine 推理副本建议 4-8C / 8-16G，**不需要 GPU**，AVX2 在 2013 年后的 Intel/AMD CPU 全覆盖 |
| 外网需求 | **零外网**。模型文件随镜像，IOC / 漏洞库可选离线包导入 |
| 模型更新 | 模型版本随平台发布版本走，离网客户用离线包 `mxctl model import xxx.onnx` |
| 信创架构 | aarch64 (ARM64) 已验证，对 KunPeng / Phytium 国产 CPU 通过 ONNX Runtime CPU 后端原生支持 |

**为什么不用 GPU？**

- ToB 服务器侧安全检测延迟敏感（P95 ≤ 5s 告警），ONNX CPU 推理够用（单条 < 10ms）；
- GPU 在服务器侧不是默认硬件，强依赖 GPU 会大幅抬高交付门槛；
- 大批量模型推理走 batch（500 条 / 100ms），CPU 性价比更高。

**详见**：[`ml-models.md`](ml-models.md)、[`architecture.md`](architecture.md) §10（智能分析双层）。

---

### A4. LLM 必须接外网吗？必须用 OpenAI / Claude / Gemini 吗？能不接 LLM 吗？

**全都可选。** mxsec 有三档智能分析配置：

| 档位 | `ml.enabled` | `llm.enabled` | 适用场景 |
|------|--------------|---------------|----------|
| Baseline | `off` | `off` | 离网 + 低配 + 政府监管要求"无 AI" |
| Smart（默认推荐） | `on` | `off` | 离网政企首选，纯本地 ONNX |
| AI-Native | `on` | `on` | 有公网或私有大模型，需要 LLM 写报告 / 解释告警 / 起草规则 |

LLM 开启时支持 **8 类 provider**，国内 + 国外 + 本地全覆盖：

| 厂商 | 接入方式 | 离网部署 |
|------|----------|----------|
| OpenAI（GPT-4o / 4o-mini） | 官方 API | ❌ |
| Anthropic（Claude 3.5 Sonnet / Haiku） | 官方 API | ❌ |
| Google（Gemini 1.5 Pro / Flash） | 官方 API | ❌ |
| 阿里千问 DashScope（Qwen-Max / Plus / Turbo） | 官方 API | ❌ |
| DeepSeek / Kimi / 智谱 / 火山方舟 | OpenAI-Compatible | ❌ |
| **本地 vLLM** | OpenAI-Compatible 端点 | ✅ Qwen2.5-7B / 14B / 32B |
| **本地 Ollama** | OpenAI-Compatible 端点 | ✅ Qwen / Llama / Mistral |
| 客户自有大模型 | 自定义 Endpoint + Bearer | ✅ |

**LLM 在 mxsec 中是"语义增强层"，不是实时检测主力**：

- 实时检测：本地 CEL 规则 + ONNX 模型；
- 离线增强：LLM 仅做告警解释、Storyline 总结、规则起草、自然语言查询，**没有 LLM mxsec 仍是完整的工业级 CWPP**。

**详见**：[`llmproxy-design.md`](llmproxy-design.md)。

---

### A5. 数据上不上云？客户的敏感数据会不会被发到 OpenAI？

**默认全本地。** mxsec 是私有化 / 离线部署优先的产品，不存在"客户数据上 mxsec 云"。

| 数据流 | 默认去向 | 是否出网 |
|--------|----------|----------|
| Agent 采集事件（EDR / FIM / 基线 / 漏洞 / 资产） | 客户内部 Kafka → ClickHouse | ❌ 仅内网 |
| 业务主数据（主机 / 用户 / 策略 / 告警 / 修复） | 客户内部 MySQL | ❌ 仅内网 |
| 漏洞情报同步（VulnSync） | 出网拉 NVD / OSV / RHSA 等 11+ 源（**只下不上**） | ✅ 仅 GET，不上传客户数据；离网客户用 [离线漏洞库包](deployment.md) |
| 本地 ML 推理 | Engine 进程内存 | ❌ 不出 Engine |
| LLM 调用（开启时） | 经 LLMProxy 转发至客户配置的厂商 | ⚠️ 出网到客户**指定**厂商，详见下表 |

**LLM 调用时的数据治理（启用 LLMProxy 才涉及）**：

| 控制项 | 默认行为 |
|--------|----------|
| 入参脱敏 | 可选开启（推荐）。`llmproxy.sanitize: true` 时自动脱敏 IP / 主机名 / 用户名 / 文件路径 / Hash 中的 PII 段 |
| 入参缓存 | Redis `mxsec:llm:cache:{sha256}` TTL 24h，命中即不再出网 |
| 出参审计 | 每次调用入 `mxsec.llm.audit` Topic + `audit_log` 表，6 个月不可删 |
| 厂商路由 | 完全由租户配置，平台不强制走任何厂商 |
| 厂商隔离 | 多租户级 LLM provider 独立配置，租户 A 用 Qwen / 租户 B 用本地 vLLM |
| 离网客户 | LLM 强制走本地 vLLM / Ollama，外网厂商在配置层禁用 |

**金融 / 政务 / 信创等强合规场景**：建议 `llm.enabled=false` 或仅启用本地 vLLM。

**详见**：[`llmproxy-design.md`](llmproxy-design.md)、[`multi-tenant.md`](multi-tenant.md)、[`deployment.md`](deployment.md) 离线部署章。

---

### A6. 怎么开多租户？一套 deploy 能跑几个客户？

mxsec 是 **from-day-1 多租户**平台，单 deploy 默认即支持 N 个隔离租户，不需要重新部署。

**4 种租户类型**：

| 类型 | 用途 |
|------|------|
| `standalone` | 独立租户，互不知晓（最常见） |
| `mssp_parent` | MSSP 集团父租户，read-only 聚合查看子租户 |
| `mssp_child` | MSSP 子租户，独立运营，不知晓父租户 |
| `internal` | mxsec 团队 / 客户内部测试用 |

**3 档物理隔离选项**：

| 策略 | DB | Kafka | 适用 |
|------|-----|-------|------|
| `shared`（默认） | 共库共表 + 行级 `tenant_id` 隔离 | 共享 Topic + Key 含 tenant | 中小客户 / 互联网 |
| `schema` | 同实例独立 schema | 独立 Topic | 中型政企 |
| `db` | 独立 MySQL / CK 实例 | 独立 Topic + 可选独立 Kafka 集群 | 金融 KA / 合规要求绝对物理隔离 |

**4 级配置覆盖**：`规则级 > 主机标签级 > 租户级 > 全局默认`，每级独立可配 `mode` / ML / LLM / quota / 保留期 / 通知。

**租户三段鉴权**：每次 API 请求经过 JWT → Tenant → RBAC 三层，所有 DB 查询走 `TenantScope` GORM 中间件强制注入 `tenant_id`，**裸 `db.Find` 会 panic**（CI 强制检查）。

**典型部署示例**：

```yaml
# /etc/mxsec/manager.yaml（节选）
tenant:
  default_strategy: shared
  jwt_claim: tenant_id

mode:
  default: observe
  tenants:
    - id: t-bank-a
      mode: observe          # 银行 A 仍监听
    - id: t-internal
      mode: protect          # 内部测试租户已切防护
```

**完整表结构、API 规范、Kafka Key、MSSP 模型**：[`multi-tenant.md`](multi-tenant.md)。

---

### A7. 信创支持哪些 OS？信创漏洞情报怎么办？信创 CPU 架构支持吗？

**信创支持矩阵（M1 完工）**：

| 操作系统 | 版本 | Agent | Server | 备注 |
|----------|------|-------|--------|------|
| openEuler | 22.03 LTS / 24.03 LTS | ✅ | ✅ | 内核 ≥ 5.10，eBPF 完整 |
| 龙蜥 Anolis OS | 8.x / 23 | ✅ | ✅ | 兼容 RHEL 生态 |
| 麒麟 KylinOS | V10 SP3 | ✅ | ✅ | aarch64 / x86_64，支持 sm2/sm4 国密 |
| 银河麒麟 | V10 | ✅ | ✅ | 与 KylinOS 同源 |
| 统信 UOS | 1060 / 20 | ✅ | ✅ | 桌面与服务器版均支持 |

**XC 信创 CI 矩阵**：

每次发版必跑 5 大信创 OS × {x86_64, aarch64} × {Agent, Server} 全矩阵，单元测试 + 集成测试 + Agent 启动 / 心跳 / 上报 / 任务执行端到端验证。CI 在 M1 阶段完工，回归覆盖率 100%。

**信创漏洞情报（VulnSync 默认开启）**：

| 源 | 用途 |
|----|------|
| openEuler CSA | openEuler 官方漏洞公告 |
| Anolis ANSA | 龙蜥官方漏洞公告 |
| Kylin KYSA | 麒麟官方漏洞公告（含银河麒麟） |
| UOS UOSEC | 统信 UOS 官方漏洞公告 |

加上 NVD / OSV / RedHat RHSA / Ubuntu USN / Debian DSA / Alpine SecDB / SUSE / CISA KEV / ExploitDB / CNNVD / EPSS 共 **15 源**，VulnSync 按 PURL + NEVRA 双索引仲裁融合，3 级 confidence。

**国产 CPU 架构**：

| 架构 | 支持 | 备注 |
|------|------|------|
| x86_64 | ✅ | 全功能 |
| aarch64 (ARM64) | ✅ | 华为鲲鹏 / 飞腾 / 龙芯 LoongArch64 兼容模式 / Apple Silicon 开发机 |
| LoongArch64 | 评估中 | M2 阶段进路线 |

**容器运行时**：Docker / containerd / CRI-O / **iSulad**（华为信创自有运行时）全部覆盖。

**详见**：[`deployment.md`](deployment.md) §16 信创 OS 部署适配、[`vulnsync-design.md`](vulnsync-design.md)。

---

### A8. mxsec 跟 Wazuh / Falco 是什么关系？是不是重复造轮子？

**不是重复造轮子，定位完全不同**。

| 项目 | 定位 | 范围 | 与 mxsec 的关系 |
|------|------|------|----------------|
| **Falco** | 运行时**规则引擎**（内核事件 + Sigma/Falco rules） | 单点检测能力 | **mxsec 集成 Falco rules**，把 Falco 规则集转 CEL 在 Engine 跑，并扩充序列 / ML / Storyline 上层能力 |
| **Wazuh** | HIDS + SIEM（日志聚合 + 主机入侵检测 + 合规扫描） | 主机日志侧 + 部分基线 | 偏 SOC 日志侧；mxsec 走 eBPF 内核观测 + 容器富化 + 修复闭环，是更上层的 CWPP |
| **mxsec** | **工业级 CWPP** | 资产 + 漏洞 + 基线 + 运行时 + 容器 + 修复闭环全栈 | 把 Falco/Sigma 当"规则源"用，把 SIEM 部分功能内嵌（告警 + Storyline + 报表），不需要再装 Wazuh |

**mxsec 的能力栈分层**：

```
┌────────────────────────────────────────────────────┐
│  CWPP 全栈（mxsec 自有）                            │
│  ├─ 资产可视化（看清）                              │
│  ├─ 脆弱性识别（算清）：漏洞 + 基线 + 攻击面        │
│  ├─ 运行时检测（处清）：EDR + FIM + 微隔离 + RASP   │
│  ├─ 修复闭环：Plan → Approve → Apply → Verify       │
│  └─ 合规报表：等保 / ISO 27001 / 月度合规           │
├────────────────────────────────────────────────────┤
│  规则引擎层（集成上游）                              │
│  ├─ Falco rules（YAML 转 CEL）                      │
│  ├─ Sigma rules（YAML 转 CEL）                      │
│  └─ Tetragon Policies（K8s 内核策略）               │
├────────────────────────────────────────────────────┤
│  数据采集层（mxsec 自研 + 上游 eBPF）                │
│  ├─ EDR 内置（eBPF Tracepoints + Tetragon-like）    │
│  ├─ FIM / 基线 / 漏洞扫描 / 资产 / 远程命令 / 修复  │
│  └─ K8s 节点视角 + 容器富化                          │
└────────────────────────────────────────────────────┘
```

**简单说**：

- **Falco / Sigma**：mxsec 的**规则上游**，社区写啥我们用啥；
- **Wazuh**：定位类似但不完整（缺漏洞情报融合 / ML / Storyline / 修复闭环 / 多租户），客户不需要 mxsec + Wazuh 同时跑。

**详见**：[`architecture.md`](architecture.md)、[`falco-sigma-integration.md`](falco-sigma-integration.md)、[`engine-design.md`](engine-design.md)。

---

### A9. 多 LLM 厂商怎么选？同时配多家会不会很贵？

**LLMProxy 提供场景路由 + 缓存 + Fallback + 配额四层成本控制**。

**1. 场景路由（按任务匹配模型档位）**：

| 场景 | 推荐模型档位 | 典型 |
|------|--------------|------|
| 告警解释 / 摘要 | 便宜模型 | GPT-4o-mini / Qwen-Turbo / Claude Haiku |
| 误报判定 / 去重 | 中档 | GPT-4o / Qwen-Plus / Gemini Flash |
| 规则起草 / 攻击链复盘 | 推理强模型 | Claude 3.5 Sonnet / GPT-4o / Qwen-Max |
| Embedding（文本向量化） | 专用 Embedding | OpenAI text-embedding-3-small / 本地 BGE-M3 |

**2. 24h 缓存**：入参 SHA256 → 响应，缓存命中**直接走 Redis 不出网**。`mxsec:llm:cache:{hash}` TTL 24h，告警去重场景命中率通常 ≥ 60%。

**3. Fallback**：主厂商连续失败 3 次进 `mxsec:llm:provider:blacklist:{provider}` 黑名单 5 min，自动切 fallback 厂商。配置示例：

```yaml
llm:
  enabled: true
  primary: qwen-plus
  fallback:
    - gpt-4o-mini
    - local-vllm-qwen2.5-7b
  cache_ttl: 24h
  sanitize: true
```

**4. 配额**：

- **租户级**月度 token / USD 上限（`tenants.quota_llm_usd`）；
- 超限自动停 LLM 调用，回退到纯本地 ML；
- `mxsec:llm:tenant:cost:{tenant}:{yyyymm}` Redis 实时累计，触达 80% / 100% 通知。

**5. 选型建议**：

| 客户类型 | 推荐配置 |
|----------|----------|
| 离网 / 信创 / 强合规 | **仅本地 vLLM**（Qwen2.5-7B 起步） |
| 国内政企 | 阿里千问 DashScope 为主 + 本地 vLLM fallback |
| 互联网 / 出海 | OpenAI / Claude / Gemini 任一为主 + 阿里千问 fallback |
| 极致省钱 | 仅 `ml.enabled=on`，`llm.enabled=off`（纯本地 ML 路线） |

**详见**：[`llmproxy-design.md`](llmproxy-design.md)。

---

### A10. 怎么给 mxsec 贡献规则？社区版怎么参与？

mxsec 接收 4 类规则贡献：

| 类型 | 仓库路径 | 格式 | 准入流程 |
|------|----------|------|----------|
| 内置检测规则（CEL） | `configs/rules/builtin-rules.yaml` | YAML + CEL 表达式 | PR + 单测 + Reviewer 审 |
| 基线策略（CIS / 等保） | `plugins/baseline/policies/` | YAML | PR + 截图 + 标注合规条款 |
| Falco / Sigma 规则映射 | `internal/server/engine/ruleconv/` | 上游 YAML + 转换 YAML | PR + 转换 fixture 单测 |
| 修复脚本（Playbook） | `plugins/remediation/playbooks/` | YAML + 幂等 Bash | PR + Idempotency 测试 |

**贡献流程**（简版，完整章节见 [`contributing.md`](contributing.md)）：

1. Fork → 从 `dev` 分支拉 `<github>/feat-rule-<name>` 工作分支；
2. 加规则 + 加单元测试 fixture（必须）；
3. 跑 `make fmt && make lint && make test`；
4. PR 标 `area/rules` label + 写 Use Case + 描述误报治理思路；
5. Reviewer 审：CEL 语法 / ATT&CK 映射 / 误报评估 / 测试覆盖；
6. CI 通过 + 双 Reviewer Approve 后合并 `dev`，下个 Release 发版。

**规则质量门槛**：

- 必须带 ATT&CK 战术映射（`tactic` / `technique` 字段）；
- 必须带误报率自评（在测试数据集上跑出来的 P/R 数据）；
- 必须支持 `mode: observe`（默认不做处置）；
- 必须可被租户级 / 主机级覆盖（不能写死 enable / disable）。

**详见**：[`contributing.md`](contributing.md) 规则贡献章 + [`falco-sigma-integration.md`](falco-sigma-integration.md) 转换规范。

---

## B. 运维与故障排查

> 本节为 v1.x 平台运维手册，按 Server / Agent / 前端 / 数据库 / mxctl / 集群 / mTLS / 业务 / DLQ / 日志 / 错误码 分组。

### B.1 Server 端

#### AgentCenter 无法启动

1. 检查端口占用：`lsof -i :6751`
2. 检查证书文件：`ls -la deploy/certs/`，确认 `ca.crt`、`server.crt`、`server.key` 存在且权限正确
3. 检查配置：`deploy/config/server.yaml` 中 `grpc.port` 是否与 `.env` 一致
4. 查看日志：`docker compose logs agentcenter`

#### Manager API 返回 500

1. 查看日志定位错误：`docker compose logs manager | grep ERROR`
2. 检查 MySQL 连接：确认 MySQL 服务运行且 `.env` 中的凭证正确
3. 检查表结构：重启 Manager 触发 Gorm AutoMigrate 自动建表/补字段

#### Consumer 持续报错

1. 检查 Kafka 连通性：确认 Broker 地址与 `.env` 配置一致
2. 检查 DLQ 堆积：观察 `*.dlq` Topic 是否有大量失败消息
3. 检查 ClickHouse 连接：Consumer 写入 ClickHouse 失败时会进 DLQ
4. 查看日志：`docker compose logs consumer | grep ERROR`

#### Engine 无告警产出（v2.0 新增）

1. 确认 Engine 副本启动：`docker compose ps engine`，副本数 ≥ 1 且 STATUS=Up
2. 检查 ConsumerGroup B `mxsec-engine` 是否正常订阅源 Topic：
   ```bash
   docker compose exec kafka-1 kafka-consumer-groups.sh --bootstrap-server localhost:9092 --group mxsec-engine --describe
   ```
3. 检查规则加载：`docker compose logs engine | grep -i "rule\|cel"`，确认从 MySQL `engine_rules` 加载规则数 > 0
4. 确认 `MODE` 配置：observe 模式下仍应产生告警（只是不下处置），若连告警都没有，看 §A2 是否误关检测开关
5. 查 Engine 指标：`mxsec_engine_alerts_total{tenant, mode, severity}` 是否在涨

#### VulnSync 没拉到漏洞数据（v2.0 新增）

1. 确认 Leader：`docker compose logs vulnsync | grep -i "leader\|elect"`，单副本 + Redis 锁 `mxsec:vulnsync:lock`
2. 检查外网连通性（按源逐一）：
   ```bash
   docker compose exec vulnsync curl -I https://nvd.nist.gov/feeds/json/cve/1.1/
   docker compose exec vulnsync curl -I https://api.osv.dev/v1/query
   ```
3. 离网客户：确认离线漏洞库已导入 `mxctl vuln import xxx.tgz`
4. 看 `mxsec.vuln.advisory` Topic 是否有新消息：
   ```bash
   docker compose exec kafka-1 kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic mxsec.vuln.advisory --max-messages 5
   ```

#### LLMProxy 调用全部失败（v2.0 新增）

1. 确认 `llm.enabled=true` 且至少配置一个 provider 的 API Key
2. 检查 provider 黑名单：`redis-cli KEYS "mxsec:llm:provider:blacklist:*"`，若主厂商在黑名单内，等 5 min 自动解除或手动 `DEL`
3. 检查租户配额：`mxsec:llm:tenant:cost:{tenant}:{yyyymm}` 是否超 `tenants.quota_llm_usd`
4. 查审计日志：`docker compose logs llmproxy | grep ERROR`
5. 离网客户：确认本地 vLLM / Ollama 已起且端口可达

#### 数据库连接失败

1. 确认 MySQL 服务运行：`docker compose ps mysql`
2. 检查凭证：`deploy/.env` 中的 `MYSQL_USER` / `MYSQL_PASSWORD`
3. 测试连接：`docker compose exec mysql mysql -u mxsec -p mxsec`

#### 服务启动顺序异常

MySQL / Redis / Kafka / ClickHouse 需要先就绪，控制面组件有健康检查依赖。如果数据库尚未初始化完成，Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy 会自动重启重试。

如果持续失败，手动检查依赖服务状态：

```bash
docker compose ps
docker compose logs mysql | tail -20
docker compose logs redis | tail -20
docker compose logs kafka-1 | tail -20
```

---

### B.2 Agent 端

#### Agent 无法连接 Server

1. **检查地址**：Agent 构建时嵌入的 `SERVER_HOST` 是否指向正确的 AC 入口（生产环境应为 L4 LB 地址）
2. **检查网络**：`nc -zv <agentcenter-host> 6751`
3. **检查防火墙**：确认 6751 端口开放
4. **检查证书**：
   - 首次连接：AgentCenter 自动下发证书，确认服务端 `deploy/certs/` 完整
   - 后续连接：检查 `/var/lib/mxsec-agent/certs/` 下 `ca.crt`、`client.crt`、`client.key`
   - mTLS 连续失败 3 次后 Agent 会暂时降级为不安全模式重新取证
5. **检查 DNS**：Agent 配置的主机名能否正确解析

#### 插件未启动

1. 检查插件文件是否存在：`ls -la /var/lib/mxsec-agent/plugin/`
2. 检查执行权限：`chmod +x /var/lib/mxsec-agent/plugin/baseline`
3. 查看 Agent 日志：`tail -f /var/log/mxsec-agent/agent.log | grep plugin`
4. 确认 Server 已下发插件配置（插件版本和 sha256 需匹配）

#### 没有检测数据上报

1. 确认 Agent 在线：查看管理界面主机列表或 `GET /api/v2/hosts`
2. 确认已创建扫描任务：查看 `GET /api/v2/tasks`
3. 检查插件运行状态：Agent 日志中搜索插件名称
4. 检查 AgentCenter 是否收到数据：`docker compose logs agentcenter | grep "baseline\|8000"`
5. 检查 Consumer 是否正常消费：`docker compose logs consumer | grep "write"`

#### Agent 更新方式

Agent 支持三种更新方式：

```bash
# 服务端推送更新（管理界面触发，走 CanaryRollout 灰度）
# CLI 主动更新
mxsec-agent --update
mxsec-agent --update --server http://manager:8080
# 本地文件更新（离网客户）
mxsec-agent --update --file ./mxsec-agent-2.0.0.rpm
```

#### Agent 资源占用过高

mxsec Agent SLO 目标：稳态 CPU < 3% / RSS < 80 MB。超出时排查：

1. 看插件维度占用：`top -p $(pgrep -d, mxsec-agent)`，分进程定位是 EDR 还是 plugin
2. EDR eBPF map 大小：`bpftool map show`，确认无异常增长
3. 临时降级：`mxctl agent throttle <host_id> --cpu 2`（生产环境运维 CLI）
4. 永久调优：参考 [`edr-agent-design.md`](edr-agent-design.md) 性能 SLO 与采集策略章节

---

### B.3 前端

#### 无法连接 API

1. 确认 Manager 运行：`curl http://localhost/api/v2/health`
2. 检查 Nginx 代理配置：`deploy/config/nginx.conf` 中 `/api/*` 的 upstream 是否指向 Manager
3. 检查 CORS 配置

#### 登录后立即跳回登录页

1. 检查 Token 存储：浏览器 DevTools → Application → localStorage
2. 检查 login 接口响应是否返回了 token
3. 检查 Nginx 是否正确代理了 API 请求（注意 `/api/` 结尾的斜杠）
4. 检查 JWT claims：v2.0 起 JWT 必须含 `tenant_id`，若客户从 v1.x 升级未带 `tenant_id` 会被新中间件拒绝

#### 页面空白

1. 检查浏览器 Console（F12）是否有 JavaScript 错误
2. 确认前端构建成功：`cd ui && npm run build`
3. 检查 Nginx 静态文件路径配置

#### 顶部模式 Banner 颜色异常

`observe` 应为绿色，`protect` 应为红色。若颜色错误：

1. 调 `GET /api/v2/system/mode` 看 default / tenants / overrides 的真实状态
2. 强刷浏览器（Ctrl+Shift+R），前端有缓存
3. 多租户登录用户：检查当前 token 的 `tenant_id`，模式按租户独立

---

### B.4 数据库

#### 查询慢

1. 检查关键索引：
   - 所有业务表必须 `tenant_id` 前缀索引（v2.0 强约束）
   - `scan_results`：`(tenant_id, host_id, rule_id, checked_at DESC)`、`(tenant_id, host_id, checked_at DESC)`
   - `scan_tasks`：`(tenant_id, status, created_at)`
   - `alerts`：`(tenant_id, detected_at DESC)`、`(tenant_id, severity, mode)`
2. 检查数据量：`SELECT COUNT(*) FROM scan_results WHERE tenant_id=?`，超大表考虑清理历史数据或按 `tenant_id` 哈希分区
3. 开启慢查询日志：`SET GLOBAL slow_query_log = 'ON'`
4. 排查跨租户穿越：慢查询无 `tenant_id` 过滤的语句一律是 bug，立即提 issue

#### 表不存在

重启 Manager / Consumer / Engine 任一服务触发 Gorm AutoMigrate 自动建表。

#### ClickHouse 写入积压

1. 检查 ClickHouse 磁盘空间
2. 检查 parts 数量：`SELECT count() FROM system.parts WHERE active AND database = 'mxsec'`
3. 如果 parts 过多，等待后台 merge 完成或适当调大 Consumer 的批量写入间隔（默认 5000 条 / 10s）

---

### B.5 mxctl 工具

#### 如何使用 mxctl 部署集群

构建：`go build -o ./bin/mxctl ./cmd/tools/mxctl`。典型流程：`mxctl check -f cluster.yaml`（校验配置）→ `mxctl preflight -f cluster.yaml`（SSH 连通性和远端环境检查）→ `mxctl deploy -f cluster.yaml`（完整部署）。详见[部署文档](deployment.md)。

#### mxctl preflight 失败提示 unsupported_os

远端节点的 `/etc/os-release` 中 `ID` 不在支持列表中。v2.0 支持列表：`ubuntu / debian / rocky / rhel / centos / almalinux / ol / openeuler / anolis / kylin / uos`。确认操作系统发行版是否在矩阵内（详见 §A7）。

#### mxctl tenant 子命令

```bash
# 创建租户
mxctl tenant create --id t-bank-a --name "银行 A" --type standalone

# 切换隔离策略（shared → schema）
mxctl tenant migrate --id t-bank-a --to schema

# 列表
mxctl tenant list

# 配额
mxctl tenant quota --id t-bank-a --agents 5000 --llm-usd 1000
```

详见 [`multi-tenant.md`](multi-tenant.md)。

---

### B.6 集群部署问题

#### 多节点部署时 AC 注册不上 Manager

检查 `network_mode: host` 下端口是否可达，确认 Manager HTTP 端口 8080 在节点间可访问。AC 注册接口为 `POST /api/v2/internal/ac/register`，启动时最多重试 3 次。

排查步骤：

1. 在 AC 所在节点执行：`curl http://<manager-ip>:8080/api/v2/health`
2. 检查 AC 启动日志：`docker compose logs agentcenter | grep register`
3. 确认防火墙规则允许节点间 8080 端口通信
4. 检查 `.env` 中 `MANAGER_HOST` 是否配置为 Manager 节点的实际 IP

#### Kafka 跨节点 Broker 通信失败

检查 `KAFKA_ADVERTISED_LISTENERS` 配置，生产 host 网络下需使用实际 IP 而非容器名。

排查步骤：

1. 确认每个 Broker 的 `KAFKA_ADVERTISED_LISTENERS` 使用了节点真实 IP
2. 在其他节点测试连通性：`nc -zv <broker-ip> 9092`
3. 检查 Kafka 日志：`docker compose logs kafka-1 | grep -i "listener\|advertised"`
4. 确认所有 Broker 节点间 9092 端口互通

#### 六微服务副本如何扩缩

| 服务 | 扩缩方式 | 备注 |
|------|----------|------|
| Manager | `docker compose up -d --scale manager=N` | 无状态 + Nginx least_conn |
| AgentCenter | `--scale agentcenter=N` | 无状态 + L4 LB，扩缩时 Agent gRPC 长连接会逐步重连 |
| Consumer | `--scale consumer=N` | ConsumerGroup A 自动 Rebalance |
| Engine | `--scale engine=N` | ConsumerGroup B 自动 Rebalance，CPU 密集型 |
| VulnSync | **不可扩容**，始终 1 副本 | Leader Election + Redis 锁 |
| LLMProxy | `--scale llmproxy=N` | 无状态 |

详见 [`deployment.md`](deployment.md)。

---

### B.7 mTLS 证书问题

#### Agent 首次连接报 TLS 握手失败

Agent 首次连接允许无证书降级（insecure 模式），连接后 Server 自动下发证书，后续恢复 mTLS。如果首次连接就报 TLS 握手失败，说明降级逻辑未生效或 CA 证书有问题。

排查步骤：

1. 检查 Server 端 `ca.crt` 是否存在且有效：`openssl x509 -in deploy/certs/ca.crt -noout -dates`
2. 检查 AgentCenter 日志中是否有证书相关错误：`docker compose logs agentcenter | grep -i "tls\|cert"`
3. 确认 Agent 版本支持 insecure 降级（v1.0.0 及以上）
4. 如果 CA 证书损坏，执行 `make certs` 重新生成后重启 Server

#### 证书过期如何更新

执行 `make certs` 重新生成全部证书，然后重启 Server 组件。Agent 会在下次心跳时自动获取新证书。

操作步骤：

```bash
# 1. 重新生成证书
make certs

# 2. 重启 Server 端所有组件
docker compose restart agentcenter manager consumer engine vulnsync llmproxy

# 3. 验证证书有效期
openssl x509 -in deploy/certs/server.crt -noout -dates
openssl x509 -in deploy/certs/ca.crt -noout -dates
```

注意事项：

- 证书更新后无需手动操作 Agent，Agent 心跳周期内会自动拉取新证书
- 如果大量 Agent 同时拉取证书，注意 Server 端负载
- 建议在业务低峰期执行证书更换
- 证书 SAN 务必含所有 AC 节点 IP / 域名（历史事故见 [`reference_prod_cert_san_incident.md`](../docs/) 内部记录）

---

### B.8 业务问题

#### 如何修改内置检测规则

内置规则通过 `configs/rules/builtin-rules.yaml` 嵌入到二进制中（`go:embed`），服务启动时自动同步到数据库 `engine_rules` 表。用户可在管理界面编辑内置规则，编辑后自动标记 `user_modified=true`，后续版本升级不会覆盖用户修改。

v2.0 起规则同步与生效路径：

```
go:embed builtin-rules.yaml
   → Manager 启动加载入 engine_rules 表
   → Engine 启动从 engine_rules 加载 + 监听变更
   → CEL 编译 + 内存索引
```

#### 内置规则和自定义规则的区别

内置规则带 `builtin=true` 标记，不可删除只能禁用；自定义规则可自由编辑和删除。版本升级时，新增的内置规则自动导入，已有未修改的内置规则自动更新，用户修改过的规则保持不变。

#### 修复任务执行失败如何排查

1. 查看 Manager 日志中 remediation 相关记录
2. 确认 Agent 侧 remediation 插件是否已安装且状态正常（管理界面 → 主机详情 → 插件列表）
3. 检查修复命令是否需要 root 权限（Agent 以 root 运行时插件也以 root 执行）
4. 查看 Agent 日志中对应任务 ID 的执行记录
5. 确认 `MODE` 配置：observe 模式下系统自动修复任务不执行，用户在 UI 主动触发的修复任务任何模式都执行（受 RBAC + audit 控制）

#### 检测规则已启用但告警为 0

可能原因及排查步骤：

1. **检查规则数据**：确认 `engine_rules` 表中有对应规则数据，且 `enabled` 字段为 true，`tenant_id` 匹配当前租户
2. **检查 Engine 加载**：查看 Engine 日志中规则编译是否成功
   ```bash
   docker compose logs engine | grep -i "cel\|rule\|compile"
   ```
3. **检查数据源**：EDR/eBPF 已内置于 agent（不再独立 sensor/tetragon plugin），确认 agent 进程含 EDR 采集器
   ```bash
   sudo systemctl status mxsec-agent
   sudo journalctl -u mxsec-agent --since=10min | grep -iE "edr|ebpf"
   ```
4. **检查数据流转**：确认 EDR 事件已写入 Kafka 且 ConsumerGroup B `mxsec-engine` 正常消费
5. **检查 mode 字段**：observe 模式下告警仍应正常产生（仅不下处置），告警数 = 0 不是 mode 引起的

#### 任务创建后一直 pending 不执行

可能原因及排查步骤：

1. **确认 Agent 在线**：检查主机心跳是否正常，管理界面主机状态应为"在线"
2. **检查 AgentCenter 注册**：确认 AC 已成功注册到 Manager 的服务发现（SD）
   ```bash
   docker compose logs agentcenter | grep -i "register\|sd"
   ```
3. **检查任务匹配**：确认任务的 `target_type` 和目标条件能匹配到至少一台主机
4. **检查任务下发日志**：
   ```bash
   docker compose logs manager | grep -i "task\|dispatch"
   docker compose logs agentcenter | grep -i "task"
   ```
5. **检查租户匹配**：v2.0 起任务必带 `tenant_id`，跨租户下发会被中间件拒绝

#### Kafka 消费积压如何处理

增加 Consumer / Engine 副本数（`CONSUMER_REPLICAS` / `ENGINE_REPLICAS` 环境变量），ConsumerGroup 使用 RoundRobin 策略自动 rebalance。

排查和处理步骤：

1. **确认积压情况**：查看各 Topic 的 Consumer Lag
2. **排除下游阻塞**：检查 ClickHouse 写入是否正常
   ```bash
   docker compose logs consumer | grep -i "clickhouse\|write\|error"
   ```
3. **扩容 Consumer / Engine**：
   ```bash
   docker compose up -d --scale consumer=3 --scale engine=2
   ```
4. **监控恢复进度**：观察 Consumer Lag 是否持续下降，目标 P99 ≤ 30s

---

### B.9 DLQ 处理

#### 发现大量消息进了 DLQ

检查 `{topic}.dlq` 中的错误信息，常见原因包括：数据库连接失败、字段格式不匹配、下游服务不可用。

排查和处理步骤：

1. **查看 DLQ 消息内容**：确认错误类型和来源 Topic
   ```bash
   docker compose logs consumer | grep -i "dlq\|dead.letter"
   ```
2. **分析常见原因**：
   - 数据库连接失败：检查 MySQL / ClickHouse 连接状态
   - 字段格式不匹配：通常由 Agent 版本不一致导致，确认上报数据结构
   - 序列化错误：检查 Protobuf 定义是否与 Agent 端一致
   - 跨租户穿越：v2.0 起消息缺 `tenant_id` 字段会被 Consumer 拒收，进 DLQ
3. **修复根因**：解决底层问题后，DLQ 中的新消息将不再增加
4. **手动重放 DLQ 消息**：DLQ 消息不会自动重放，修复问题后需手动处理
   - 编写脚本从 DLQ Topic 消费并重新投递到原 Topic
   - 或根据业务需要直接丢弃过期的 DLQ 消息

---

### B.10 日志位置

| 组件 | 位置 |
|------|------|
| Manager | `docker compose logs manager` |
| AgentCenter | `docker compose logs agentcenter` |
| Consumer | `docker compose logs consumer` |
| Engine | `docker compose logs engine` |
| VulnSync | `docker compose logs vulnsync` |
| LLMProxy | `docker compose logs llmproxy` |
| Nginx | `docker compose logs ui` |
| MySQL | `docker compose logs mysql` |
| Agent | `/var/log/mxsec-agent/agent.log` |
| 插件 | `/var/log/mxsec-agent/<plugin-name>.log` |

---

### B.11 常见错误码

| 错误 | 原因 | 处理 |
|------|------|------|
| 401 Unauthorized | Token 过期或无效 | 重新登录获取 Token |
| 403 Forbidden | 权限不足 / 跨租户 | 检查用户角色 + 当前 token 的 `tenant_id` |
| 404 Not Found | 资源不存在或 URL 错误 | 检查 API 路径 |
| 429 Too Many Requests | 超 Quota（Agent 数 / LLM token / API 调用） | 看 `/api/v2/tenants/{id}/quota` 当前用量 |
| 500 Internal Error | 服务端异常 | 查看对应微服务日志 |
| 502 Bad Gateway | Nginx 无法连接后端 | 检查 Manager / AgentCenter 是否运行 |
| 503 Service Unavailable | 租户迁移中 / 维护窗口 | 查 `tenants.status`，等迁移完成 |
| gRPC UNAVAILABLE | Agent 无法连接 AC | 检查网络、证书、端口 |

---

## C. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构总图（六微服务） | [`architecture.md`](architecture.md) |
| 监听 / 防护双模式 | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| EDR Agent 采集 | [`edr-agent-design.md`](edr-agent-design.md) |
| Agent 性能调优 | [`edr-agent-design.md`](edr-agent-design.md) §性能 SLO |
| 资产统一模型 | [`asset-model.md`](asset-model.md) |
| 漏洞模块设计 | [`vuln-module-design.md`](vuln-module-design.md) |
| 三大产品目标 | [`security-objectives.md`](security-objectives.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署指南（含信创） | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 社区贡献规范 | [`contributing.md`](contributing.md) |
| 治理与版本策略 | [`governance.md`](governance.md) |
