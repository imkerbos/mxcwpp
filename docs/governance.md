# 开源治理

> **本文档定位**：mxsec 项目的治理章程（Charter），约束维护者协作、版本发布、安全响应、社区行为、开源与商业版边界。任何贡献者、维护者、商业用户在介入项目前都应阅读本文档。
>
> 修订需走"重大决策"流程（见 §3）。

---

## 0. 项目定位声明（不可变）

mxsec 是**工业级开源 CWPP（Cloud Workload Protection Platform）**，专精 **Linux 主机与 Kubernetes 容器**，面向 ToB 政企/金融/互联网客户。

| 项 | 立场 |
|----|------|
| 产品类别 | CWPP（Cloud Workload Protection Platform） |
| 工作负载范围 | **仅** Linux 主机 + Kubernetes 容器；**不做** Windows、macOS、移动端 |
| 客户群体 | 政企 / 金融 / 央国企 / 互联网 ToB；不做 ToC |
| 架构形态 | **六微服务**（Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy）+ Kafka 异步解耦 + Agent + Plugin |
| 运行哲学 | **监听优先（Observe-First）**：默认部署即监听模式，磨合达标后才切防护模式 |
| 智能能力 | **本地 ML 主导（ONNX Runtime CPU 推理）+ LLM 可选（多厂商可路由）+ 用户可关 AI** |
| 多租户 | **from-day-1**：`tenant_id` 全平台贯穿，单 deploy 支持 N 隔离租户 |
| 商业模式 | **Open Core**：核心 Apache-2.0 开源，商业模块独立分发（详见 §10） |

**所有违反上述立场的设计、PR、Issue、营销材料一律驳回**。维护者有权将偏离上述立场的提议关单（关闭时附引用本节）。

权威源：

- [`architecture.md`](architecture.md) — 六微服务架构总图
- [`operating-modes.md`](operating-modes.md) — 监听/防护双模式产品哲学
- [`multi-tenant.md`](multi-tenant.md) — 多租户设计
- 战略评估（内部）：`ref/00-总体评估与商业化路线.md`

---

## 1. 项目使命

mxsec 的产品使命用 9 个字概括：**看清 → 算清 → 处清**。

| 阶段 | 目标 | 工程交付 |
|------|------|----------|
| **看清** | 把客户工作负载里"有什么"全部摸清——主机、进程、网络、文件、容器、镜像、K8s 资源、依赖、漏洞、配置、用户、密钥 | 资产清点（Agent → AgentCenter → Consumer 写存储）；统一资产模型 ([`asset-model.md`](asset-model.md)) |
| **算清** | 把"哪些是脆弱性 / 入侵 / 风险"按优先级算清——CVE 关联、基线偏差、行为异常、攻击链、ATT&CK 标定 | Engine 检测引擎（规则 / 序列 / ML / Storyline / LLM 增强）；VulnSync 漏洞情报融合 |
| **处清** | 把"该怎么修 / 怎么阻断 / 怎么追溯"按风险分级闭环——修复计划、自动响应（仅 protect）、虚拟补丁、隔离、报表、合规导出 | 修复模块（remediation 插件 + RemediationPlan）；运行模式控制（observe → protect）；合规报表 |

> mxsec **不接受**仅做"看清"或仅做"算清"的孤立模块。每一项新增能力都要能在使命三段中说清自己的位置。设计评审拒绝"为做而做"的功能。

进一步细化产出指标见 [`security-objectives.md`](security-objectives.md)。

---

## 2. 项目治理模型

mxsec Platform 采用 **Maintainer 委员会制**，由核心维护者共同决策项目方向和重大技术选型。

### 2.1 角色定义

| 角色 | 职责 | 权限 |
|------|------|------|
| Maintainer | 项目整体方向、架构决策、版本发布、代码审查 | 合并 PR、管理 Issue、发布版本 |
| Committer | 特定模块的代码审查与合并 | 对应模块的 PR 合并权限 |
| Contributor | 提交代码、文档、Issue、讨论 | Fork + PR |
| Security Responder | 漏洞响应专员（可由 Maintainer 兼任） | 接收 `security@mxsec.io`、协调修复、CVE 申报 |

### 2.2 晋升路径

- **Contributor → Committer**：持续贡献且代码质量稳定，经 Maintainer 提名并获多数同意
- **Committer → Maintainer**：长期深度参与，对架构有全面理解，经全体 Maintainer 投票通过
- **Maintainer → Security Responder**：自愿担任 + 满足背景与响应能力要求；任期 1 年可续

### 2.3 模块归属（Committer Scope）

每位 Committer 对应一个或多个模块。当前模块划分：

| 模块 | 路径 |
|------|------|
| Manager（业务面） | `internal/server/manager/`、`cmd/server/manager/` |
| AgentCenter（接入面） | `internal/server/agentcenter/`、`cmd/server/agentcenter/` |
| Consumer（写入器） | `internal/server/consumer/`、`cmd/server/consumer/` |
| Engine（检测引擎） | `internal/server/engine/`、`cmd/server/engine/` |
| VulnSync（漏洞情报） | `internal/server/vulnsync/`、`cmd/server/vulnsync/` |
| LLMProxy（多厂商网关） | `internal/server/llmproxy/`、`cmd/server/llmproxy/` |
| Agent + 插件 | `internal/agent/`、`plugins/` |
| UI | `ui/` |
| 部署 / mxctl | `cmd/tools/mxctl/`、`internal/deploy/`、`scripts/` |

模块边界与各微服务"专精化设计"严格对齐（详见 [`architecture.md`](architecture.md) §2），跨模块改动必须由对应模块的 Committer 共同审查。

---

## 3. 决策流程

### 3.1 委员会组成

Maintainer 委员会最少由 3 人组成。当委员会人数不足 3 人时，应优先从现有 Committer 中提名补充，以保证决策流程的有效性。

### 3.2 日常决策

Bug 修复、文档改进、小型功能增强等日常变更，由任一 Committer 审查通过后即可合并。

### 3.3 重大决策

涉及以下事项时，需提交设计文档并在 Issue 中公开讨论，经 Maintainer 委员会 2/3 多数同意后方可实施：

- 架构层面的重大变更（含六微服务边界调整、Kafka Topic 增删、数据库分表策略）
- 新增外部依赖或替换核心组件
- 不向后兼容的 API 变更
- 安全模型变更（认证 / 授权 / mTLS / 多租户隔离）
- 修改本文档（§0 定位声明、§10 开源/商业边界、§11 数据隐私承诺 三段需 **全体一致** 才可改）

重大决策的讨论期为 7 天。讨论期内所有 Maintainer 均可发表意见。讨论结束后，最终结论以 Comment 形式记录在对应 Issue 中并 pin 置顶，确保决策可追溯。

### 3.4 RFC 流程（轻量级）

mxsec 采用轻量级 RFC 机制承载"重大决策"的设计讨论，避免决策仅靠 Issue 评论碎片化沉淀。

**步骤**：

1. **提议**：发起人在 `docs/rfcs/`（首次提议时自动建该目录）下新建 `RFC-NNNN-<slug>.md`，包含：
   - 背景与动机（Why）
   - 详细设计（What / How）
   - 与现有模块、运行模式、多租户的兼容性影响
   - 替代方案与拒绝理由
   - 安全 / 隐私 / 合规影响
   - 灰度与回滚方案
2. **挂号**：提 Pull Request，标签 `kind/rfc`；GitHub Issue 同步开 Tracking Issue 收集讨论
3. **讨论期**：≥ 7 天，所有 Maintainer 必须出席至少 1 次评论
4. **决议**：Maintainer 委员会 2/3 投票，结果写入 RFC 文件首部 `status:` 字段（`accepted` / `rejected` / `withdrawn` / `superseded`）
5. **落地**：accepted 的 RFC 在实现 PR 描述中 link 回 RFC 编号，便于 6 个月后回溯
6. **失效**：若 90 天无人推进，RFC 自动转 `withdrawn`，需重启走新 RFC

**适用范围**：

| 变更 | 是否需 RFC |
|------|-----------|
| 新增微服务 / 删除微服务 | ✅ 必须 |
| Kafka Topic schema 不兼容变更 | ✅ 必须 |
| 多租户隔离策略变更 | ✅ 必须 |
| 默认运行模式调整（如改默认值） | ✅ 必须 |
| 新增 LLM 厂商 / ML 模型纳入官方支持 | ✅ 必须 |
| 修改 §0 / §10 / §11 三段 | ✅ 必须（且需全体一致） |
| 新增小型功能、Bug 修复、文档 | ❌ 不需要 |
| 单模块内部重构（不影响接口） | ❌ 不需要 |

模板（首次创建 `docs/rfcs/` 目录时，请用以下骨架建立 `RFC-0000-template.md`）：

```markdown
---
rfc: NNNN
title: <短标题>
authors: ["@github-handle"]
status: draft  # draft / discussion / accepted / rejected / withdrawn / superseded
created: 2026-MM-DD
updated: 2026-MM-DD
target_release: vX.Y
supersedes: []  # 列出被本 RFC 取代的 RFC 编号
---

## 摘要
## 动机
## 设计
## 影响
- 多租户影响
- 运行模式影响（observe / protect）
- 安全 / 隐私影响
- 性能影响
## 替代方案
## 灰度与回滚
## 未解决问题
## 参考
```

---

## 4. 版本管理

### 4.1 版本号规范

遵循 [Semantic Versioning 2.0.0](https://semver.org/)：

```
MAJOR.MINOR.PATCH
```

- **MAJOR**：不兼容的 API 或协议变更
- **MINOR**：向后兼容的功能新增
- **PATCH**：向后兼容的 Bug 修复

### 4.2 发布流程

1. 确认所有计划功能已合并且测试通过
2. 更新版本号和 CHANGELOG
3. 创建 Release Tag
4. 构建并发布安装包和镜像
5. 更新文档

### 4.3 CHANGELOG 生成

CHANGELOG 通过 `git log` 生成，按以下分类组织：

- **feat**：新增功能
- **fix**：Bug 修复
- **refactor**：代码重构

其他类型（docs、test、chore 等）视情况决定是否纳入 CHANGELOG。

### 4.4 RC 版本管理

正式版本发布前需经过 RC（Release Candidate）阶段：

- RC 版本 Tag 格式：`v1.0.0-rc.1`、`v1.0.0-rc.2`，依此类推
- RC 版本需经过内部完整测试流程，所有阻塞性问题修复后方可发布正式版
- RC 阶段仅接受 Bug 修复，不接受新功能合入

### 4.5 跨大版本升级

当发布 MAJOR 版本变更时，必须同步提供迁移指南文档，内容包括：

- 不兼容变更的完整清单
- 逐项迁移步骤和示例
- 数据库 schema 迁移脚本（如适用）
- 配置文件格式变更说明

---

## 5. 代码质量

### 5.1 代码审查标准

所有合并到主分支的代码必须满足：

- 至少一名 Committer 或 Maintainer 审查通过
- 通过 CI 流水线（`make lint` + `make test`）
- 不引入已知安全漏洞
- 遵循项目代码规范（详见 [`contributing.md`](contributing.md)）
- 跨微服务边界改动需对应模块 Committer 同时审查

### 5.2 PR 审查 SLA

| PR 类型 | 审查时限 |
|---------|---------|
| 普通 PR | 2 个工作日 |
| 安全相关 PR | 1 个工作日 |

### 5.3 特殊审查要求

- **数据库 schema 变更**：除常规审查外，需至少一名 Maintainer 额外审查，确认迁移脚本的正确性和向后兼容性，且必须包含 `tenant_id` 列与索引前缀（详见 [`multi-tenant.md`](multi-tenant.md) §3）
- **运行模式相关变更**：涉及 `observe` / `protect` 行为差异的 PR 必须 Engine 模块 Committer + Maintainer 共同审查，且 PR 描述明确列出"哪些动作受 mode 控制"
- **多租户相关变更**：所有涉及 `tenant_id` / `TenantScope` 的改动需附跨租户穿越测试用例
- **安全审查**：涉及认证、授权、数据加密、输入处理等安全敏感模块的变更，需对照 OWASP Top 10 进行检查，并在 PR 中附上安全审查清单
- **LLM / ML 相关变更**：涉及 LLMProxy / 本地 ML 推理 / Embedding 缓存的 PR 必须确认默认 `llm.enabled=false` 且能在纯离网环境运行

### 5.4 测试要求

- 核心路径单元测试覆盖率 >= 85%
- 整体覆盖率 >= 70%
- 新增功能必须包含对应测试
- Bug 修复必须包含回归测试
- 多租户相关功能必须包含**跨租户穿越测试**

---

## 6. 漏洞披露与响应

### 6.1 漏洞报告渠道

如果你发现安全漏洞，**请勿在公开 Issue 中披露**。请通过以下私密渠道之一报告：

- **首选**：邮件至 `security@mxsec.io`
- **备选**：邮件至 `0xkerbos@gmail.com`（历史邮箱仍受理，迁移期保留）
- **加密**：可使用项目 PGP 公钥（指纹见 `https://mxsec.io/.well-known/security.txt`，未来发布前阶段以 README 公示为准）
- **GitHub Security Advisory**：可在 GitHub 仓库 Security 标签下私密创建 Draft Advisory

主题格式建议：`[SECURITY] 简要描述`。

报告应包含：

- 受影响版本（如 `v1.0.0-rc.3`）
- 复现步骤（含最小化 PoC）
- 影响范围评估（数据泄露 / RCE / 提权 / DoS 等）
- 你的联系方式与是否希望署名致谢

### 6.2 漏洞处理 SLA

| 阶段 | 时限 | 性质 |
|------|------|------|
| 确认收到报告 | 48 小时 | 硬性要求 |
| 初步评估与定级 | 7 个工作日 | 目标值 |
| 严重漏洞（CVSS >= 9.0）启动修复 | 24 小时内 | 硬性要求 |
| 高危漏洞（CVSS 7.0-8.9）启动修复 | 7 个工作日 | 目标值 |
| 中危漏洞（CVSS 4.0-6.9）启动修复 | 30 个工作日 | 目标值 |

### 6.3 漏洞处理流程

1. **收件**：Security Responder 收到报告，自动 / 手动回执
2. **48 小时内**：确认漏洞报告已收悉
3. **评估**：确认漏洞真实性和影响范围，按 CVSS v3.1 评分（条件具备时同步 CVSS v4.0）
4. **定级**：CVSS >= 9.0 进入加急通道；CVSS >= 7.0 启动按计划修复
5. **修复**：在私有分支 / Fork 中开发修复方案，引用最少必要的 Committer
6. **预通知**：修复发布前通知所有受影响 KA 商业用户（窗口 ≥ 5 工作日），确保其有时间做好升级准备
7. **协同披露**：申请 CVE 编号（通过 GitHub Security Advisory 或 MITRE），与报告者协商披露时间窗
8. **发布**：作为 PATCH 版本发布，并在 GitHub Security Advisory + Release Notes 中公开
9. **审计**：CHANGELOG 记录条目（不泄露利用细节），保留完整事件日志 ≥ 6 个月

### 6.4 安全更新通知渠道

- GitHub Security Advisory
- Release Notes
- `security-announce@mxsec.io` 邮件列表（KA 商业客户 + 自愿订阅者）
- README / 文档站 Banner（CVSS >= 7.0 时挂出）

### 6.5 致谢与名人堂

- 默认在 Release Notes 致谢报告者（除非报告者要求匿名）
- 项目站点维护 `Hall of Fame` 页面，按报告时间排序
- 暂不提供金钱奖励；KA 商业版可能在未来引入 Bug Bounty，本治理文档届时同步更新

### 6.6 报告者承诺与保护

mxsec 承诺：

- 不就善意安全研究采取法律行动
- 不主动追溯报告者身份，除非报告者主动公开
- 不公开未授权披露的细节

报告者请遵守：

- 不利用漏洞做实际攻击 / 数据获取
- 不向第三方公开漏洞细节直到协同披露完成
- 不要求超出修复进度合理范围的承诺

---

## 7. 行为准则

本项目遵循 [Contributor Covenant 2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) 行为准则。

参与本项目即表示你同意遵守该准则。违反行为准则的行为可以通过 `0xkerbos@gmail.com` 报告（与漏洞报告共用收件渠道，但请在主题中标注 `[COC]` 区分）。

---

## 8. 许可证

本项目使用 [Apache License 2.0](../LICENSE)。所有贡献将在同一许可下发布。提交 PR 即表示你同意将你的贡献以 Apache 2.0 许可发布。

商业版模块的 License 单独说明，详见 §10。

---

## 9. 第三方依赖与许可证合规

mxsec 严格审视第三方依赖的 License 兼容性。新增依赖必须填写 `THIRD-PARTY-LICENSES.md`（变更时同步），并满足：

| License | 接受策略 |
|---------|----------|
| Apache-2.0 / MIT / BSD-3 / ISC | ✅ 直接引入 |
| MPL-2.0 / EPL-2.0 / CDDL | 🟡 仅限非传染边界使用，需 RFC |
| LGPL | 🟡 仅限动态链接，需 RFC |
| GPL-2.0 / GPL-3.0 / AGPL | ❌ 禁止静态/源码集成；如必须使用（如 ClamAV），走**独立子进程 + RPC 隔离**，不污染主程序，需 RFC + Maintainer 全体同意 |
| 其他 / 不明 | ❌ 禁止 |

涉及 fork 自其他 Apache-2.0 项目的代码段（例如内核 hook / RASP / 反勒索的设计借鉴），必须在文件头部保留原项目 NOTICE / 版权声明，并在 `NOTICE` 文件中聚合。

---

## 10. 开源版与商业版边界（Open Core）

mxsec 采用 **Open Core** 商业模式：开源版用 Apache-2.0 提供工业级完整 CWPP 能力；商业版在开源版之上以独立模块形态分发，解决"开源不够卖"的运营、合规、性能、KA 服务能力差距。

### 10.1 开源版（mxsec OSS, Apache-2.0）

开源版承诺**始终**包含以下能力，不会因商业化收回：

| 类别 | 能力 |
|------|------|
| 架构 | 六微服务（Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy）完整代码 |
| Agent | Linux 主机 Agent + 自更新框架 + Plugin 框架 |
| 插件 | baseline / scanner / fim / remediation 全部插件 |
| 容器 | K8s DaemonSet 部署 + 容器富化 + K8s Audit 检测规则 |
| 检测 | CEL 规则引擎 + 序列检测 + 本地 ONNX ML 推理 + Storyline |
| 漏洞 | VulnSync 11+ 源同步 + advisory 仲裁 + 主机指纹关联 |
| LLM | LLMProxy 多厂商适配（OpenAI / Anthropic / Google / 千问 / DeepSeek / Ollama / vLLM 等）+ 客户自带 Key |
| 多租户 | 完整 from-day-1 多租户（行级隔离 + 三段鉴权 + 共享模式） |
| 运行模式 | observe / protect 全套切换机制 |
| 部署 | docker-compose / mxctl 集群部署 / 离网包 |
| 报表 | 基础 CSV / JSON 导出；CHANGELOG / 审计日志 |
| 文档 | 全部 `docs/` 公开内容 |

### 10.2 商业版（mxsec Enterprise, 闭源 / 单独 License）

商业版作为**独立模块**仓库分发，**绝不**对开源版功能"砍刀"。商业版聚焦：

| 类别 | 能力 |
|------|------|
| 高级合规报表 | 等保 2.0 三级 / ISO 27001 / 行业专属 docx 报表模板 + 一键签字版 |
| MSSP 控制台 | 父子租户聚合视图 / 跨子租户报表 / 计费引擎 |
| 灰度与回滚 | CanaryRollout v2 高级策略 + 失败自动回滚 + 审批工作流 |
| 高级响应 | 客户专属 Playbook 编排 + 高级 SOAR 连接器（ServiceNow / Jira / Splunk 等） |
| NPatch 虚拟补丁 | 自研补丁规则 + eBPF 阻断（KA 客户独占） |
| 高级 RASP | 五栈 RASP（Java / Python / Node / PHP / Go）商业规则库 |
| 自研补丁库 | 50,000+ 自维护 CVE 补丁规则（独立于 NVD / OSV 的差异化数据） |
| 信创深度适配 | 信创 OS 数据源稳定性保障 + CCRC 测评配套 |
| 商业 SLA 与支持 | KA 7×24 工单 / 远程协助 / 资深安全顾问 / 应急响应包 |
| 集成认证 | 等保自评签字版 / ISO 27001 / 行业认证文件 |
| 商业 SDK | Go / Python / Java SDK + OpenAPI 3.0 客户端代码生成 |

### 10.3 Open Core 治理规则

为防止"开源诱饵 + 商业版陷阱"反模式，mxsec 设定如下硬约束：

1. **不向下兼容回收**：开源版已发布过的功能**永不**回收到商业版
2. **核心架构开源**：六微服务架构、Agent、Plugin、检测引擎、多租户、运行模式这些"骨架"永久开源
3. **商业模块独立仓库**：商业版代码不进入 mxsec OSS 仓库，避免误用 / 误编译
4. **接口公开**：商业模块通过开源版的公开扩展接口（gRPC / Plugin / Webhook）接入，不依赖私有补丁
5. **数据可迁出**：商业版的所有租户数据用户可一键导出，避免数据锁定
6. **决策双签**：开源版功能想"移到商业版"需 RFC + Maintainer 全体一致同意，且必须在公告中明示边界变更
7. **诚信营销**：宣传材料中"商业版独占"的标注必须真实，禁止"将来会回收"的暗示

### 10.4 Forking 与下游使用

- 任何人可基于 mxsec OSS Apache-2.0 fork 自用 / 改造 / 商用，无需获取额外授权
- 下游 fork 不得使用 `mxsec` / `MxSec` 商标做产品名（详见 §12 商标条款）
- 下游 fork 强烈建议保留 NOTICE 文件与 CHANGELOG 中的上游归属
- 商业版独立 License，不可基于 Apache-2.0 直接 fork 转售；如需 OEM / 渠道授权请联系 `partner@mxsec.io`

---

## 11. 数据隐私承诺

mxsec 是"装在客户机器里看客户数据"的安全产品，对数据隐私的承诺高于一切。

### 11.1 三条不可逾越的红线

1. **不外传客户数据**：mxsec OSS / Enterprise 都**不会**自动把客户的告警 / 漏洞 / 资产 / 日志上报到任何 mxsec 控制的云端
2. **不强制 LLM**：LLMProxy 是可选组件，默认 `llm.enabled=false`；客户可关闭、可换厂商、可换本地模型
3. **不内置后门**：mxsec OSS 代码全公开，可审计；Enterprise 商业版同样接受第三方代码审计（KA 客户合同条款）

### 11.2 本地优先（Local-First）

| 数据类型 | 默认归宿 |
|----------|----------|
| EDR / FIM 事件 | 客户自有 Kafka + ClickHouse |
| 漏洞情报 | VulnSync 同步到客户自有 MySQL |
| 告警与攻击链 | Engine 写客户自有 MySQL + ClickHouse |
| ML 模型推理 | 100% 本地（ONNX Runtime CPU），不外发 |
| 基线扫描结果 | 本地存储 |
| 资产清点 | 本地存储 |

### 11.3 用户可选 LLM（多厂商 / 本地 / 关闭）

- LLM 是**可选增强**，不是必要路径
- 客户可在 `llm.enabled=true` 时选择厂商：
  - 公网厂商：OpenAI / Anthropic / Google / 阿里千问 / DeepSeek / Kimi / 智谱 / 火山方舟
  - **离网部署**：Ollama / vLLM（OpenAI-Compatible，推荐 Qwen 2.5-7B 量化版）
- 客户可设置每月 token 上限、租户级配额、Fallback 策略
- 所有 LLM 调用走客户配置的 API Key + 客户配置的 TLS 证书校验
- LLMProxy 内置 24h 缓存 + 审计日志，所有调用可追溯

详见 [`llmproxy-design.md`](llmproxy-design.md)。

### 11.4 离网模式（Air-Gapped）

mxsec 完整支持离网部署：

- 全部 6 微服务可在内网 Kubernetes / Docker / 物理机部署，无任何"必须出网"的功能
- LLM 选 Ollama / vLLM 本地化运行，CPU/GPU 任选
- 漏洞情报：VulnSync 离网模式下用客户提供的 advisory 离线包（CNNVD / 信创等可订阅同步）
- 部署包 `mxctl` 支持完全离线安装、升级、回滚
- ML 模型随安装包自带，无需联网下载

### 11.5 遥测（Telemetry）

mxsec OSS **默认不开启**任何外发遥测。

- 如未来引入"匿名使用统计"功能，必须默认 OFF，且通过 RFC 流程公开决策
- 商业版的"客户健康度上报"是合同明文条款，客户签字同意才生效

### 11.6 敏感数据脱敏

Consumer 写入存储前对敏感字段做 Sanitize：

- 用户密码 / Token / API Key / 私钥永不入存储
- 文件路径中可能含 PII 的字段（如 `/home/<user>/.ssh/`）做白名单遮罩
- LLMProxy 调用前对告警 payload 做敏感字段剔除（详见 `llmproxy-design.md`）

### 11.7 数据保留

- 默认告警保留 90 天 / 事件 30 天 / 审计 180 天，租户级可覆盖（[`multi-tenant.md`](multi-tenant.md) §7）
- 客户可一键清除全部数据（含商业版）
- 卸载脚本完整清除 Agent 本地缓存、quarantine 目录、配置

---

## 12. 商标与品牌

- `mxsec`、`MxSec`、`矩阵云安全平台` 名称及 Logo 是项目维护方持有的商标
- 任何人可在开源协议下使用、修改、再分发代码
- **未经书面授权**，不得：
  - 用 `mxsec` / `MxSec` 做衍生产品商品名或主网站域名
  - 暗示与官方维护方存在赞助 / 背书关系
  - 在商业宣传中使用 Logo 而无来源标注
- 允许：
  - 在博客、论文、对比测评、社区分享中使用名称作客观引用
  - 在 fork README 中说明 "Based on mxsec"

商业 / 渠道 / OEM 授权联系：`partner@mxsec.io`。

---

## 13. 与对标产品的关系声明

mxsec 在内部战略文档中对标 **青藤万象（CWPP）** 与 **青藤蜂巢（CNAPP）** 作为同型商业产品参考。

| 立场 | 说明 |
|------|------|
| 学习参考 | 我们公开承认参考其产品形态、能力分层、KA 运营经验；这是行业基本素养 |
| 工程独立 | mxsec 全部代码原创或基于明确 Apache-2.0 / MIT 友好的 fork；不抄袭其商业代码 |
| 客观对比 | 对外材料中如需对比，必须基于公开白皮书或客观测试结果，禁止贬损性表述 |

详细对标差距与商业化路线见 `ref/00-总体评估与商业化路线.md`（内部文档）。

---

## 14. 参考文档

### 14.1 公开（`docs/`）

| 主题 | 文档 |
|------|------|
| 架构总图 | [`architecture.md`](architecture.md) |
| 运行模式（监听/防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| 资产模型 | [`asset-model.md`](asset-model.md) |
| 安全目标 | [`security-objectives.md`](security-objectives.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型 | [`ml-models.md`](ml-models.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署指南 | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 贡献指南 | [`contributing.md`](contributing.md) |

### 14.2 内部（`ref/`，不公开发布）

| 主题 | 文档 |
|------|------|
| 总体评估与商业化路线 | `ref/00-总体评估与商业化路线.md` |
| 路线图 | `ref/08-roadmap.md` |
| 模块深度报告 | `ref/01-服务端架构.md` ~ `ref/07-病毒.md` |
| 竞品资料 | `ref/appendix/` |

---

## 15. 修订记录

| 版本 | 日期 | 主要变更 |
|------|------|----------|
| v1.0 | 2026-05-13 | 初版治理章程（角色 / 决策 / 版本 / 漏洞响应 / 代码质量） |
| v1.1 | 2026-06-06 | 加 §0 工业级开源 CWPP 定位声明；加 §1 项目使命；加 §3.4 RFC 流程；加 §6 `security@mxsec.io` 渠道与协同披露；加 §9 第三方依赖合规；加 §10 Open Core 开源/商业边界；加 §11 数据隐私承诺（本地优先 + 用户可选 LLM + 离网模式）；加 §12 商标条款；加 §13 对标关系声明；保留历史社区规范 |
