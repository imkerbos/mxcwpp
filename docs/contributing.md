# 社区规范

> **平台定位**：mxsec 是一款**工业级开源 CWPP（Cloud Workload Protection Platform）**，专精 **Linux 主机 + Kubernetes 容器**，面向 ToB 政企 / 金融 / 互联网客户。
>
> 在贡献代码、规则、模型、文档之前，请务必通读以下 3 份**权威源文档**，并确保改动与之严格对齐：
>
> 1. [架构总图（六微服务）](architecture.md) — `Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy`
> 2. [运行模式（监听优先 / 防护后置）](operating-modes.md) — 默认监听，磨合达标后切防护
> 3. [多租户设计](multi-tenant.md) — `tenant_id` 全平台贯穿，from-day-1
>
> mxsec 只覆盖 **Linux + K8s**，不支持 Windows / macOS workload；本仓库内任何"Windows agent"、"macOS endpoint"、"默认即阻断"、"三层架构"等说法均与产品定位不符，PR 会被驳回。

感谢你对 mxsec 的关注。本文档说明如何参与项目开发，涵盖代码、规则、ML 模型、安全敏感模块、文档五类贡献，并给出强制的开发工作流与模块负责人列表。

---

## 1. 贡献类型总览

mxsec 接受 6 类贡献，每类有独立的 PR 模板与验收标准：

| 类型 | 路径 | 准入要求 | 章节 |
|------|------|----------|------|
| 代码（六微服务） | `cmd/server/*`、`internal/server/*` | 单测 ≥ 70%、`make fmt lint test` 全过 | §4、§5 |
| 代码（Agent / 插件 / eBPF） | `internal/agent/*`、`plugins/*` | 同上 + 安全敏感审查 + 自保护回归 | §8 |
| 规则（Sigma / Falco / CEL / Tetragon） | `rules/`、`engine/rules/` | 规则单测 + 误报基线 + ATT&CK 映射 | §6 |
| ML 模型（ONNX） | `ml/models/*`、`ml/training/*` | 数据脱敏 + 评估指标达标 + 模型签名 | §7 |
| 文档 | `docs/*`、`ref/*`（内部） | 必须基于权威源、术语对齐 | §9 |
| 部署 / 运维 | `deploy/*`、`cmd/tools/mxctl/*` | 升级路径 + 回滚演练 | §5、§8 |

> **规则**：跨多类贡献（如同时改 Engine 代码 + 规则 + 文档）必须**拆 PR**，每个 PR 单一目的，便于评审与回滚。

---

## 2. 开发环境

### 2.1 前置要求

- Go >= 1.25
- Node.js >= 18（前端开发）
- Docker >= 20.10、Docker Compose >= 2.0
- protoc（Protobuf 编译器，与仓库 `api/proto/` 版本对齐）
- Make
- Python >= 3.11（仅 ML 模型训练贡献者，详见 §7）
- ONNX Runtime CPU >= 1.18（运行时已打包，本地复现训练用）

### 2.2 环境搭建

```bash
# 克隆仓库
git clone https://github.com/mxsec/mxsec-platform.git
cd mxsec-platform

# 启动开发环境（六微服务 + Kafka + MySQL + Redis + ClickHouse + UI 全栈，带热更新）
make dev-docker-up

# 查看日志
make dev-docker-logs

# 一键停掉
make dev-docker-down
```

开发环境访问地址：

| 服务 | 地址 | 说明 |
|------|------|------|
| UI | http://localhost:3000 | Nginx + Vite HMR |
| Manager API | http://localhost:8080 | REST API + SSE |
| AgentCenter gRPC | localhost:6267 | Agent 接入 mTLS |
| Engine gRPC | localhost:6280 | 内部调用（Manager → Engine） |
| VulnSync gRPC | localhost:6281 | 内部调用 |
| LLMProxy gRPC | localhost:6282 | 可选启用 |
| MySQL | localhost:3306 | 业务主数据 |
| Redis | localhost:6379 | SD + 缓存 + 锁 |
| Kafka | localhost:9092 | 数据面 |
| ClickHouse | localhost:8123 | 事件归档 |
| Prometheus | localhost:9090 | 指标 |

### 2.3 常用命令

```bash
make proto                # 生成 Protobuf 代码（改 api/proto/*.proto 后必跑）
make build-server         # 构建六微服务二进制
make build-agent          # 构建 Agent 二进制
make package-agent        # 打包 Agent 为 RPM / DEB
make package-plugins      # 构建所有插件
make fmt                  # 格式化（gofmt + goimports + prettier）
make lint                 # golangci-lint + eslint + vue-tsc
make test                 # 单元 + 集成测试
make e2e                  # 端到端测试（需 dev 环境运行）
```

---

## 3. 代码规范

### 3.1 Go

- 日志一律用 Zap 结构化日志，**禁止** `fmt.Println` / `log.Println` / `log.Printf`
- HTTP 响应一律用 `internal/server/manager/api/response.go` 中的 `Success / BadRequest / NotFound / InternalError`，**禁止** 裸 `c.JSON()`
- 错误返回 `error`，用 `fmt.Errorf("xxx: %w", err)` 包装，**禁止** `panic`（除中间件 fail-safe 类场景，需明确注释）
- 数据库查询用 `Preload` 避免 N+1，写操作必须用事务保证一致性
- 配置一律从配置文件读取（Viper），**禁止** 硬编码 host / port / secret
- 单元测试命名 `TestXxx_<场景描述>`，使用 table-driven
- 所有业务 DB 查询必须经过 `tenant.Scope`，**禁止** 裸 `db.Find` / `db.Where`（详见 [`multi-tenant.md`](multi-tenant.md) §3.3）
- 所有跨服务调用走 gRPC + mTLS + 内部 Bearer Token，**禁止** 复用旧 `X-Internal-Secret`

### 3.2 TypeScript / Vue

- 所有 API 调用封装在 `ui/src/api/<module>/`，**禁止** 在组件内直接 `axios.xxx`
- TypeScript 严格模式（`strict: true`），所有接口必须显式声明类型
- 每个 API 调用必须 `try-catch`，错误经统一拦截器吐 toast
- 命名：组件 `PascalCase`、函数 / 变量 `camelCase`、常量 `UPPER_CASE`
- 涉及 `mode`（observe / protect）字段的 UI 必须用统一的 `ModeBadge` 组件显示，**禁止** 复制粘贴样式

### 3.3 通用

- 匹配现有代码风格，**禁止** "顺手优化"无关代码（每行改动必须能追溯到 Issue）
- **禁止** 添加超出需求的功能与抽象（YAGNI）
- 提交前本地必须跑 `make fmt && make lint && make test`，全过才能 push
- 涉及 schema 变更，必须同步更新 `docs/` 下对应文档（架构 / API / DataType 等）

---

## 4. 开发工作流（强制）

mxsec 遵循 **feature branch → dev → main** 三层分支模型 + 7 步流程。**禁止跳步**。

### 4.1 分支模型

| 分支 | 用途 | 规则 |
|------|------|------|
| `main` | 稳定发布（Tag 即 release） | 仅从 `dev` 合并，禁止直接 push |
| `dev` | 集成验证 | 个人功能分支合并到此，跑 CI + E2E |
| `<owner>/<type>-<desc>` | 个人功能分支 | 必须从 `dev` 拉，必须 PR 回 `dev` |

**分支命名格式**：`<owner>/<type>-<desc>`，例：

- `kerbos/feat-engine-cel-rule`
- `kerbos/fix-agentcenter-cert-san`
- `kerbos/refactor-consumer-dlq-retry`
- `zhangsan/docs-operating-modes-faq`

`type` 取值：`feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `perf` / `sec`。

### 4.2 七步流程

1. **评估** — 收到需求后先输出方案（改动范围、步骤、风险、预期结果），等评审 / Issue 三连确认
2. **分支** — 从 `dev` 创建 `<owner>/<type>-<desc>`
3. **实现** — 只做方案内的改动，匹配现有风格，禁止顺手重构
4. **验证** — 本地三连：`make fmt && make lint && make test`，再加场景验证
5. **提交** — Commit 信息按用户视角描述，遵守 §4.3 格式
6. **PR** — 提到 `dev`，至少 1 名 Committer 通过 + CI 全绿
7. **合并** — Committer 走 `--no-ff` 合并到 `dev`，定期由 Maintainer 合 `dev → main`

**简单任务**（读文件、改文档错字、查状态）可直接 PR，无需走完整流程。

### 4.3 Commit 规范

格式：

```
<type>: <简短描述（≤ 50 字符，中文 OK）>

- 详细改动点 1
- 详细改动点 2
- 关联 Issue：Closes #123
```

强约束：

- 禁止 commit 信息中出现 AI / Claude / GPT / Copilot / Sonnet / Opus / Haiku / Gemini / Co-Authored-By 等 AI 相关字眼或署名
- 禁止在 `--no-verify` 或 `--no-gpg-sign` 下提交
- 失败的 pre-commit hook 必须修问题后**新建 commit**，不准 `--amend` 上一次提交（避免覆盖他人改动）

`type` 与分支命名同表（feat / fix / refactor / docs / test / chore / perf / sec）。

### 4.4 PR 模板

```markdown
## 改动目的（Why）
- 一句话说清楚

## 改动范围（What）
- 文件 / 模块 / API 列表

## 验证（How verified）
- [ ] `make fmt && make lint && make test` 全过
- [ ] 本地 dev 环境功能验证（贴截图 / 日志）
- [ ] 涉及 schema 变更，文档同步更新（链接）
- [ ] 涉及多租户的改动，跨租户穿越测试通过
- [ ] 涉及 observe/protect 的改动，两个模式都验证过

## 风险与回滚
- 影响面：xxx
- 回滚方式：xxx

## 关联
- Closes #xxx
```

### 4.5 CI 检查（必过）

| 检查项 | 命令 | 阻塞 |
|--------|------|------|
| Go 格式化 | `gofmt -l` | 是 |
| Go 静态检查 | `golangci-lint run ./...` | 是 |
| Go vet | `go vet ./...` | 是 |
| Go 单元测试 | `go test ./... -race -cover` | 是 |
| 前端 lint | `npm run lint`（在 `ui/`） | 是 |
| 前端类型检查 | `npm run build`（vue-tsc） | 是 |
| Protobuf 一致性 | `make proto && git diff --exit-code` | 是 |
| 多租户穿越扫描 | 自研脚本：业务 model 必带 `tenant_id` 列 | 是 |
| 容器镜像签名 | cosign 验证 | 仅 `main` 分支 |

---

## 5. 六微服务代码贡献

每个微服务**只做一件事**（详见 [`architecture.md`](architecture.md) §2）。新增功能前先对照下表确认服务归属：

| 我要做的事情 | 该改哪个服务 | 严禁改哪 |
|--------------|--------------|----------|
| 新增 REST API、RBAC 规则、报表、通知 | Manager | Engine / Consumer |
| 新增 Agent gRPC 接入、任务下发链路 | AgentCenter | Manager |
| Kafka 消息持久化、DLQ、Sanitize | Consumer | Engine |
| 新规则、ML 推理、序列分析、Storyline | Engine | Manager / Consumer |
| 新漏洞情报源、advisory 仲裁 | VulnSync | Manager |
| 新 LLM 厂商适配、token 计费 | LLMProxy | Manager / Engine |

**典型反模式**（PR 直接驳回）：

- 在 Manager 里写 Kafka 消费循环
- 在 Consumer 里写 CEL / ML 检测逻辑
- 在 Engine 里直接写 MySQL（必须产 alert → Kafka，由 Consumer 持久化）
- 在 AgentCenter 里调用 LLM API
- 任何服务里直接 `http.Post("https://api.openai.com/...")`（必须经 LLMProxy）

### 5.1 新增 REST API（Manager）

1. 在 `internal/server/manager/api/<module>/` 加 handler
2. 路由注册在 `internal/server/manager/router/router.go`，挂三段中间件（JWT + Tenant + RBAC）
3. 所有 DB 查询过 `tenant.Scope(ctx)`
4. 同步更新 `docs/api-reference.md`
5. 前端在 `ui/src/api/<module>/` 加封装 + 类型

### 5.2 新增 gRPC 调用（服务间）

1. 在 `api/proto/<service>.proto` 加 RPC，跑 `make proto`
2. 服务端实现走 mTLS + 内部 Bearer Token 中间件
3. 客户端用 `internal/server/common/grpcpool/` 的连接池，**禁止** 每次都 `grpc.Dial`

### 5.3 新增 Kafka Topic

1. 在 [`datatype-allocation.md`](datatype-allocation.md) 申请 DataType 编号
2. 在 [`architecture.md`](architecture.md) §4.1 加 Topic 一行
3. 在 `internal/server/common/kafka/topic.go` 注册
4. Consumer / Engine 加订阅与处理逻辑
5. 配套 DLQ `{topic}.dlq` 必须开启

---

## 6. 规则贡献（Sigma / Falco / CEL / Tetragon Policies）

mxsec Engine 通过统一**规则中台**整合 4 类规则源（详见 [`falco-sigma-integration.md`](falco-sigma-integration.md)）：

| 规则源 | 转换路径 | 适用场景 |
|--------|----------|----------|
| Sigma | Sigma → CEL 转换器 | 通用日志检测（auditd、syslog、process） |
| Falco | Falco YAML → CEL 转换器 | 系统调用 / 容器异常 |
| Tetragon Policies | TracingPolicy CRD 直采 | eBPF 内核可观测 |
| CEL（原生） | 直接编译 | mxsec 自研规则 |

### 6.1 目录结构

```
rules/
├── sigma/              # 上游 Sigma 规则（git submodule，原汁原味）
├── falco/              # 上游 Falco 规则
├── tetragon/           # Tetragon TracingPolicy YAML
├── cel/                # mxsec 自研 CEL 规则
│   ├── linux/
│   │   ├── persistence/
│   │   ├── privilege-escalation/
│   │   └── lateral-movement/
│   └── k8s/
│       ├── admission/
│       └── runtime/
└── testdata/           # 规则测试用例（输入事件 + 期望输出）
```

### 6.2 CEL 规则格式（mxsec 原生）

```yaml
# rules/cel/linux/persistence/cron-tamper.yaml
id: PERSISTENCE_CRON_TAMPER
title: 通过 cron / at 持久化
description: 检测对 /etc/cron.*、crontab 的异常写入
severity: high
attck:
  - T1053.003  # Scheduled Task/Job: Cron
platforms:
  - linux
data_sources:
  - mxsec.agent.events    # FIM
  - mxsec.agent.ebpf      # EDR
expression: |
  event.type == "file_modify" &&
  (event.path.startsWith("/etc/cron.") || event.path == "/etc/crontab") &&
  !(event.process.exe in ["/usr/sbin/cron", "/usr/bin/crontab"])
mode_default: observe       # 监听优先，详见 operating-modes.md
would_action:
  type: alert_only
references:
  - https://attack.mitre.org/techniques/T1053/003/
maintainer: rules-team
```

**必填字段**：`id`、`title`、`severity`、`attck`、`platforms`、`data_sources`、`expression`、`mode_default`、`references`。

**`mode_default` 取值**：所有新规则默认 `observe`。要直接出厂 `protect`，必须在 PR 中说明：

- 历史误报率 < 0.5%（贴数据）
- 至少 3 个客户环境 90 天回放命中率 ≥ 95%
- 安全运营负责人书面同意

### 6.3 规则测试用例

每条规则必须配 1 个**正样本** + 1 个**负样本**：

```
rules/testdata/PERSISTENCE_CRON_TAMPER/
├── positive.json     # 应触发
├── negative.json     # 不应触发
└── expected.yaml     # 期望的 alert 字段
```

运行：

```bash
make test-rules                                # 全量回归
make test-rules RULE=PERSISTENCE_CRON_TAMPER   # 单条
```

### 6.4 Sigma / Falco 引入流程

1. 选择 Sigma / Falco 官方仓库的规则（保留原始 ID 与版权头）
2. 跑转换器：`mxctl rules convert --src sigma --in xxx.yml --out rules/cel/...`
3. 人工核对转换结果（CEL 表达式可读、字段映射正确）
4. 补 `mode_default: observe` + ATT&CK 映射 + 测试用例
5. 在 PR 描述里**显式声明上游许可证**与原始 commit hash

### 6.5 规则 PR 流程

1. 分支：`<owner>/feat-rule-<id-slug>`，例：`kerbos/feat-rule-persistence-cron-tamper`
2. 规则 + 测试用例 + 必要的转换器 patch 一并提
3. 跑 `make test-rules`
4. 跑误报基线评估：`make rules-fp-baseline`（在内置数据集上跑，要求新规则不让总误报率上升 > 0.5pp）
5. PR review by `rules-team` Maintainer

### 6.6 命中后链路（必读）

任何新规则上线必须明确：

- 命中后产 `mxsec.engine.alert`，由 Consumer 持久化
- `would_action` / `action` 字段按 [`operating-modes.md`](operating-modes.md) §6 填写
- 严禁规则内嵌"直接调用 iptables / systemd / kill"等动作代码，所有响应走 Engine 响应层 + Agent 插件统一通道

---

## 7. ML 模型贡献（ONNX）

mxsec 走**本地 ML 主导**路线（ONNX Runtime CPU 推理），完整模型清单详见 [`ml-models.md`](ml-models.md)。

### 7.1 模型分类

| 类别 | 典型模型 | 用途 |
|------|----------|------|
| 异常检测 | Isolation Forest、LOF | 进程 / 网络 / 登录行为离群 |
| 序列建模 | LightGBM、n-gram、Markov | 命令序列、登录序列异常 |
| 文本 Embedding | MiniLM、SimCSE | 告警去重、相似攻击链聚类 |
| 分类 | XGBoost、LightGBM | 恶意脚本 / Webshell 静态分类 |

### 7.2 目录结构

```
ml/
├── models/                  # ONNX 模型文件（git LFS）
│   └── <model-id>/
│       ├── model.onnx
│       ├── metadata.yaml    # 模型卡（必填）
│       └── signature.sig    # Ed25519 签名（必填，详见 §8）
├── training/                # 训练脚本（Python）
│   └── <model-id>/
│       ├── train.py
│       ├── requirements.txt
│       ├── data_prep.py
│       └── eval.py
├── datasets/                # 训练 / 评估数据集（脱敏，git LFS）
│   └── <dataset-id>/
│       ├── README.md        # 来源 + 许可证 + 脱敏方法
│       ├── train/
│       └── eval/
└── benchmarks/              # 模型评估结果（每次训练落档）
    └── <model-id>/<date>.json
```

### 7.3 模型卡（`metadata.yaml`）必填

```yaml
id: anomaly-iforest-process-v1
version: 1.2.0
task: anomaly_detection
input_schema:
  - name: process_features
    shape: [1, 24]
    dtype: float32
output_schema:
  - name: anomaly_score
    shape: [1]
    dtype: float32
inference_runtime: onnxruntime>=1.18 (CPU)
target_throughput_qps: 5000
target_latency_p95_ms: 5
mode_default: observe          # 模型默认监听
training_data:
  - dataset: dataset-process-2026q1
    samples: 1200000
    license: CC-BY-4.0
    pii_scrubbing: applied     # 强约束
metrics_eval:
  precision: 0.962
  recall: 0.881
  fp_rate: 0.013
  auroc: 0.974
  dataset_used: eval-2026q2-internal
maintainer: ml-team
signing_key_id: mxsec-ml-2026
references:
  - 论文 / 上游模型 / Sigma 同类规则
```

### 7.4 训练数据要求（强约束）

- **PII 强制脱敏**：用户名、邮箱、IP（公网保留前 16 位 / 内网保留前 24 位）、URL Query、Cookie、Token 全部脱敏后才能入仓
- **数据来源声明**：必须在 `datasets/<id>/README.md` 标注上游来源、许可证、采集时间窗、行业领域
- **租户隔离**：训练数据**严禁**跨租户混合（除非全部脱敏 + 客户书面授权 + Maintainer 双签）
- **大小限制**：单数据集 ≤ 5 GB（git LFS）；超出需走外部对象存储 + checksum 入仓

### 7.5 特征工程

- 特征必须在 `ml/training/<model-id>/data_prep.py` 完整可复现
- 特征 schema 与 Engine 推理时的 `internal/server/engine/ml/features/` 字段**严格对齐**
- 新增特征必须同步改 Engine 端的 feature extractor + 单测
- 禁止使用"在线穿越特征"（如告警 label 本身）

### 7.6 评估指标（达标才允许进 main）

| 模型类别 | Precision | Recall | FP Rate | 备注 |
|----------|-----------|--------|---------|------|
| 异常检测（IForest 类） | ≥ 0.90 | ≥ 0.80 | ≤ 0.02 | 离线评估集 |
| 序列建模 | ≥ 0.92 | ≥ 0.82 | ≤ 0.02 | 同上 |
| Embedding（聚类质量） | NMI ≥ 0.75 | - | - | - |
| 分类（Webshell 等） | ≥ 0.97 | ≥ 0.92 | ≤ 0.005 | 必须 |

每次训练在 `ml/benchmarks/<model-id>/<date>.json` 落档评估结果，PR 描述贴对比表（vs 上一版本）。

### 7.7 推理性能门槛

- 在 4 vCore Intel Xeon Silver 标准盒里：
  - 单次推理 P95 ≤ 5 ms
  - 单实例稳态吞吐 ≥ `target_throughput_qps`
- 不达标的模型不允许进 Engine 默认开启列表（可作为可选高级功能保留）

### 7.8 模型签名

所有进入 `ml/models/` 的 `.onnx` 必须带 Ed25519 签名（详见 §8.4），未签名模型 Engine 启动时拒绝加载。

### 7.9 模型 PR 流程

1. 分支：`<owner>/feat-ml-<model-id>`
2. 提交：训练脚本 + 评估报告 + 模型卡 + 签名 + ONNX 文件（LFS）
3. 数据集脱敏审计（PR review 必查）
4. CI 跑 `make test-ml`（含特征 schema 一致性、推理性能、达标阈值）
5. PR review by `ml-team` Maintainer 双签

---

## 8. 安全敏感代码贡献（Agent / eBPF / 插件）

Agent 直接运行在客户主机上，权限高、影响面大。**任何 Agent / eBPF / 插件相关 PR 都按强约束流程评审**。

### 8.1 Agent / 插件代码原则

- **最小权限**：插件子进程能用普通用户跑就不要 root；必须 root 的操作（如 eBPF 加载）显式声明
- **零外联**：除 AgentCenter（mTLS gRPC）外，**禁止**任何 outbound 网络连接（DNS、HTTP、第三方）
- **资源自限**：单核 CPU 稳态 < 3%、RSS < 80 MB；超限触发本地自降级（详见 [`architecture.md`](architecture.md) §8.3）
- **崩溃隔离**：插件 panic 不能拖垮 Agent，Agent panic 不能影响业务进程
- **数据脱敏**：上行事件先经 Sanitize 过 PII / Secret，再进 Kafka

### 8.2 eBPF 程序原则

- 内核版本兼容矩阵：4.18+（CentOS 8）、5.4+（Ubuntu 20.04）、5.10+（推荐）
- CO-RE（BTF）优先，无 BTF 主机走 libbpfgo + 兼容回退
- 严禁阻塞型 helper 调用（`bpf_skb_load_bytes` 等需谨慎）
- 提交前必须跑：
  ```bash
  make test-ebpf-matrix  # 多内核版本回归
  make bench-ebpf        # 性能基线（开销 < 1% CPU @ 标准负载）
  ```
- 新增 eBPF 必须配套：内核兼容矩阵报告、性能基线、回滚开关（运行时可关）

### 8.3 mTLS / 通信安全（强约束）

| 链路 | 强约束 |
|------|--------|
| Agent ↔ AgentCenter | mTLS（`VerifyClientCertIfGiven`），证书自动下发，禁止明文 gRPC |
| 服务间（Manager / Engine / VulnSync / LLMProxy / AC） | mTLS + 内部 Bearer Token，禁止复用旧 `X-Internal-Secret` |
| LLMProxy ↔ 外部 LLM API | HTTPS + 客户 API Key + TLS 证书校验，禁止 `InsecureSkipVerify` |
| 浏览器 ↔ Manager | HTTPS + JWT + RBAC + Tenant 三段，禁止明文 HTTP |

证书生成走 `scripts/generate-certs.sh`，生产环境用 ACME / 私有 CA。**严禁** 在仓库提交 `.key` / `.pem` 私钥（CI 检查 + git-secrets 扫描）。

### 8.4 二进制 / 模型 / 规则签名

mxsec 在以下分发物上启用 Ed25519 签名（`internal/common/signing/`）：

| 分发物 | 签名 | 校验时机 |
|--------|------|----------|
| Agent 二进制 | 是 | Agent 自更新前 |
| 插件二进制 | 是 | Agent 加载插件前 |
| ONNX 模型 | 是 | Engine 加载前 |
| 规则包（CEL/Sigma） | 是 | Engine 加载前 |
| 集群部署包（mxctl） | 是 | mxctl deploy 时 |

签名 key 管理：

- 生产 key 由 Maintainer 离线保管（HSM / 离线机）
- 开发 / 测试用专用 dev key（`scripts/dev-signing-key.sh`）
- CI 流水线用 ephemeral key，仅校验签名格式

### 8.5 Agent 自保护

任何修改 Agent 自保护逻辑（防卸载、防停止、防注入）的 PR 必须：

- 通过自保护回归测试集（`tests/agent-self-protect/`）
- 涵盖 Linux 主流发行版（CentOS 7/8、Ubuntu 20/22、Debian 11/12、openEuler、Anolis、Kylin、UOS）
- 失败回滚演练 ≥ 1 次

### 8.6 默认监听 / 防护一致性

Agent 与插件的所有"动作类响应"必须读 `MODE` 配置：

- `MODE=observe`：仅采集 + 上报，不执行 kill / quarantine / iptables / PAM 等动作
- `MODE=protect`：执行实际动作

新增任何"响应类"代码路径必须：

- 单元测试覆盖两种模式
- 集成测试两种模式都验证一次（PR 描述贴证据）
- 详见 [`operating-modes.md`](operating-modes.md) §8

### 8.7 安全审查（Security Review）必触发场景

以下 PR 必须由 `security-team` 至少 1 名 Maintainer 单独 review（除常规 review 外）：

- 任何 Agent / 插件 / eBPF 代码
- 任何 mTLS / 签名 / 鉴权 / 加解密代码
- 任何 `cmd/tools/mxctl/` 部署 CLI 改动
- 任何动作类响应（kill / quarantine / iptables / PAM / NetworkPolicy）
- 任何涉及 `tenant_id` 中间件 / Scope 的改动

---

## 9. 文档贡献

mxsec 的产品语义由权威源决定，文档贡献必须严格对齐。

### 9.1 权威源（不可绕过）

1. [`architecture.md`](architecture.md) — 六微服务架构总图
2. [`operating-modes.md`](operating-modes.md) — 监听 / 防护双模式哲学
3. [`multi-tenant.md`](multi-tenant.md) — 多租户设计

新增 / 修改任何文档前必须**先通读这 3 份**，并确保术语严格对齐。

### 9.2 术语红线（违反 PR 驳回）

| 禁用表达 | 必须替换为 |
|----------|-----------|
| "三层架构"、"Manager + AC + Consumer 架构" | "六微服务：Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy" |
| "默认即阻断"、"实时拦截威胁" | "默认监听（observe），磨合达标后切防护（protect）" |
| "支持 Windows endpoint" / "macOS workload 防护" | "Linux 主机 + Kubernetes 容器专精" |
| "EDR 插件" | "EDR 已内置于 Agent，不再作为独立 plugin" |
| "X-Internal-Secret" 共享密钥 | "mTLS + 内部 Bearer Token"（过渡期可标注） |

### 9.3 对比对标产品

允许在文档中对比的友商：青藤万象、青藤蜂巢、CrowdStrike Falcon、SentinelOne、Wazuh、Falco、Tetragon、CISA KEV。

**禁止** 在公开文档中：

- 对比 Elkeid（架构已落后，避免误导新用户）
- 贬低任何竞品，只陈述差异化与适用场景

### 9.4 文档目录结构

```
docs/
├── architecture.md            # 权威源 1：六微服务
├── operating-modes.md         # 权威源 2：监听/防护
├── multi-tenant.md            # 权威源 3：多租户
├── api-reference.md           # REST API 参考
├── deployment.md              # 部署
├── configuration.md           # 配置参考
├── contributing.md            # 本文件
├── engine-design.md           # Engine 详设
├── engine-detection-design.md # 检测细节
├── edr-agent-design.md        # Agent 采集
├── vulnsync-design.md         # VulnSync
├── vuln-module-design.md      # 漏洞模块
├── llmproxy-design.md         # LLMProxy
├── ml-models.md               # 本地 ML 清单
├── falco-sigma-integration.md # 规则中台
├── asset-model.md             # 资产统一模型
├── security-objectives.md     # 三大产品目标
└── datatype-allocation.md     # DataType 分配

ref/                           # 内部资料（不外发）
├── 00-总体评估与商业化路线.md
├── 01-07 各模块深度报告
├── 08-roadmap.md
└── appendix/                  # 友商参考资料
```

### 9.5 文档引用规范

- 引用同级 docs：相对路径 `[xxx](xxx.md)`
- 引用 ref（内部）：绝对路径 `ref/xx-xxx.md`
- 引用代码：相对仓库根路径 `internal/server/engine/...`
- 引用外部资料：标注访问日期 + 优先 archive.org 镜像

### 9.6 文档 PR 流程

1. 分支：`<owner>/docs-<topic>`
2. 改完跑：`make docs-lint`（标题层级、链接有效性、术语红线扫描）
3. 涉及权威源（architecture / operating-modes / multi-tenant）的修改必须由 `core-team` Maintainer 评审
4. 涉及对外 API（`api-reference.md`）必须同步给前端 / SDK Maintainer 评审
5. 涉及对外承诺（SLO、定价、Roadmap）必须 PM 签字

### 9.7 文档实时同步原则（强约束）

- 每完成一个功能 / 任务，**立即更新**相关文档（路线图、架构、API、DataType 等）
- 进度与文档实时同步，**不允许**"代码已合，文档未更"的状态
- PR 描述里必须显式列出已同步的文档清单（或显式声明"无文档影响"，由 reviewer 复核）

---

## 10. 测试要求

| 类型 | 路径 | 覆盖率 / 门槛 |
|------|------|---------------|
| 单元测试 | `*_test.go` 同包 | 核心路径 ≥ 85%、整体 ≥ 70% |
| 集成测试 | `tests/integration/` | 关键链路全覆盖（Agent → Kafka → Engine → Alert） |
| 端到端测试 | `tests/e2e/` | 安装 + 注册 + 任务 + 告警 + 修复全流程 |
| 多租户穿越 | `tests/tenant-isolation/` | 100% 通过 |
| 模式切换 | `tests/mode-switch/` | observe ↔ protect 双向 |
| 规则回归 | `make test-rules` | 全部规则正负样本通过 |
| ML 评估 | `make test-ml` | 达 §7.6 指标 |
| eBPF 矩阵 | `make test-ebpf-matrix` | 4 个内核版本均通过 |
| 自保护回归 | `tests/agent-self-protect/` | 主流发行版全覆盖 |

运行：

```bash
go test ./... -v                         # 单元
go test ./... -cover                     # 含覆盖率
go test ./internal/server/engine/... -v  # 指定包
make integration                         # 集成
make e2e                                 # E2E
```

**Bug 修复必须附带回归测试**，PR 描述贴失败 → 修复后通过的对比。

---

## 11. Issue 规范

### 11.1 Bug 报告

请包含：

- 环境（OS、内核、Go 版本、Docker 版本、mxsec 版本 / commit）
- 部署形态（dev / 单机 / 集群 / 多租户）
- 当前 `MODE`（observe / protect）
- 复现步骤
- 预期 vs 实际
- 相关日志（脱敏后）

### 11.2 功能建议

请说明：

- 使用场景与动机（哪类客户、哪个阶段）
- 期望行为
- 与权威源（架构 / 模式 / 多租户）的对齐方式
- 是否愿意参与实现

### 11.3 安全漏洞

**严禁** 在公开 Issue 中披露安全漏洞。请按 [`SECURITY.md`](../SECURITY.md) 流程私下报告 Maintainer。

---

## 12. 沟通渠道

| 渠道 | 用途 |
|------|------|
| GitHub Issue | Bug 报告 / 功能建议 |
| GitHub Discussion | 技术讨论 / 问题咨询 |
| Pull Request | 代码 / 规则 / 模型 / 文档贡献 |
| 邮件 security@（私下） | 安全漏洞 |

---

## 13. 模块负责人（MAINTAINERS）

> 每个模块至少 1 名 Maintainer + 1 名 Reviewer。Maintainer 负责合 PR、评 RFC、把方向；Reviewer 负责日常 PR 评审。
>
> 内部成员花名以 GitHub handle 记录；OSS 贡献者按本章 §13 末尾"升级路径"通过核心团队提名 + Maintainer 投票晋升。

| 模块 / 路径 | Maintainer | Reviewer | 涉及权威源 |
|-------------|------------|----------|------------|
| **core-team（架构 / 文档权威源）** | @kerbos | @kerbos | architecture / operating-modes / multi-tenant |
| Manager（`internal/server/manager/`） | @kerbos | @kerbos | architecture §2.1 |
| AgentCenter（`internal/server/agentcenter/`） | @kerbos | @kerbos | architecture §2.2 |
| Consumer（`internal/server/consumer/`） | @kerbos | @kerbos | architecture §2.3 |
| Engine（`internal/server/engine/`） | @kerbos | @kerbos | architecture §2.4 / engine-design / engine-detection-design |
| VulnSync（`internal/server/vulnsync/`） | @kerbos | @kerbos | architecture §2.5 / vulnsync-design / vuln-module-design |
| LLMProxy（`internal/server/llmproxy/`） | @kerbos | @kerbos | architecture §2.6 / llmproxy-design |
| Agent / EDR / eBPF（`internal/agent/`） | @kerbos | @kerbos | edr-agent-design |
| 插件 baseline（`plugins/baseline/`） | @kerbos | @kerbos | - |
| 插件 scanner（`plugins/scanner/`） | @kerbos | @kerbos | - |
| 插件 fim（`plugins/fim/`） | @kerbos | @kerbos | - |
| 插件 remediation（`plugins/remediation/`） | @kerbos | @kerbos | vuln-module-design |
| 插件 av-scanner（Phase 4） | @kerbos | @kerbos | - |
| 插件 rasp（Phase 4） | @kerbos | @kerbos | - |
| **rules-team（规则中台）** | @kerbos | @kerbos | falco-sigma-integration |
| **ml-team（本地 ML）** | @kerbos | @kerbos | ml-models |
| **security-team（mTLS / 签名 / 鉴权 / Agent 自保护）** | @kerbos | @kerbos | - |
| 部署 / mxctl（`cmd/tools/mxctl/`、`internal/deploy/`） | @kerbos | @kerbos | deployment |
| 前端 UI（`ui/`） | @kerbos | @kerbos | - |
| Protobuf（`api/proto/`） | @kerbos | @kerbos | datatype-allocation |
| API 文档（`docs/api-reference.md`） | @kerbos | @kerbos | - |
| Roadmap（内部 `ref/08-roadmap.md`） | @kerbos | @kerbos | - |

> **当前阶段**：v1.0 开发中，Maintainer 均为内部团队，对外开放贡献后会在本表逐步补充社区 Reviewer / Committer。
>
> **升级路径**：贡献者（首次合 PR）→ Reviewer（≥ 5 个高质量 PR 被合）→ Committer（≥ 20 个 PR + 持续 3 个月）→ Maintainer（核心团队提名 + 投票）。

---

## 14. 行为准则（Code of Conduct）

mxsec 社区遵循 Contributor Covenant v2.1。所有贡献者、Reviewer、Maintainer 在 Issue、PR、Discussion 中均须：

- 互相尊重，对事不对人
- 用数据 / 事实讨论技术分歧
- 不发布、贬低、骚扰个人或群体的言论
- 不分享他人的私人信息（含客户脱敏前的真实环境数据）
- 不在公开渠道讨论安全漏洞细节（走 §11.3）

违反者由 Maintainer 团队仲裁，措施包括但不限于：警告、临时禁言、永久封禁。

---

## 15. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构（权威源） | [`architecture.md`](architecture.md) |
| 运行模式（权威源） | [`operating-modes.md`](operating-modes.md) |
| 多租户（权威源） | [`multi-tenant.md`](multi-tenant.md) |
| Engine 详设 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| EDR Agent 采集 | [`edr-agent-design.md`](edr-agent-design.md) |
| VulnSync 服务 | [`vulnsync-design.md`](vulnsync-design.md) |
| 漏洞模块 | [`vuln-module-design.md`](vuln-module-design.md) |
| LLMProxy | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| 资产统一模型 | [`asset-model.md`](asset-model.md) |
| 三大产品目标 | [`security-objectives.md`](security-objectives.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署 | [`deployment.md`](deployment.md) |
| 配置 | [`configuration.md`](configuration.md) |
| 路线图（内部） | `ref/08-roadmap.md` |
| 商业化路线（内部） | `ref/00-总体评估与商业化路线.md` |

---

感谢每一位贡献者，让 mxsec 成为真正的工业级开源 CWPP。
