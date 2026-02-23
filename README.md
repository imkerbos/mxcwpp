# Matrix Cloud Security Platform（矩阵云安全平台）

> 面向 Linux 主机与中间件的基线检查平台。
> v1 聚焦 **操作系统基线**，参考 ByteDance 开源的 Elkeid（特别是 baseline 插件和 agent 设计），重新设计一套 **Server + UI + Agent + Baseline 引擎**，兼容新系统版本（如 Rocky Linux 9、Debian 12 等）。

**当前版本**：v1.0.0
**开发状态**：核心功能已完成，生产可用

## 功能亮点

| 功能模块 | 说明 |
|---------|------|
| **基线检查** | 9 种检查器，13 个规则文件，覆盖 SSH、密码策略、文件权限、服务状态等 |
| **基线修复** | 支持单机/批量自动修复，配置化服务重启 |
| **文件完整性监控** | 基于 AIDE 的 FIM 检查，5 套默认策略，支持变更分类和严重级别评估（仅 VM） |
| **资产采集** | 10 种采集器：进程、端口、用户、软件、容器、应用、网卡、磁盘、内核模块、服务 |
| **多 OS 支持** | Rocky Linux 9、CentOS 7/8、Oracle Linux 7/8/9、Debian 10/11/12、Ubuntu 等 |
| **Web 控制台** | 主机管理、策略管理、任务调度、告警管理、报表统计 |
| **插件架构** | Agent + Plugin 架构，支持插件热更新和版本管理 |

---

## 1. 背景与目标

现状 & 问题：

- 现有开源 HIDS / 基线方案（如 Elkeid、Wazuh 等）：
  - 功能强，但社区版维护节奏慢，部分组件对新系统版本兼容差（例如 baseline / driver 对 el9、debian12 支持不完整等）。
  - 全家桶架构较重，不适合在部分业务场景做“只要基线检查”的轻量部署。
- 内部需要一个：
  - **聚焦“基线安全”能力**（系统 + 中间件）的平台；
  - 能按公司规范定制策略、配合现有 CMDB / 告警体系（Prometheus + Nightingale）；
  - 能持续维护并适配业务实际使用的操作系统与中间件。

本项目目标：

1. **v1：操作系统基线检查平台**
   - 面向 Linux 服务器（物理机、虚机、K8s 节点等）做系统层基线检查。
   - 支持多发行版：Rocky Linux 9、Oracle Linux 7、CentOS 7/8、Debian 10/11/12 等。
   - 支持策略配置和自定义基线规则、检测结果展示、导出与告警对接（通过 Webhook / 消息平台等）。
2. **v2：扩展到中间件与容器**
   - 支持 Nginx、Redis、MySQL 等常见服务的基线配置检查。
   - 考虑与容器运行时安全 / 云原生日志、指标等联动。
3. **长期：统一基线安全中台**
   - 与 CMDB、SSO、日志平台、监控平台打通，成为统一“基线安全视图”。

---

## 2. 整体架构（规划）

> 总体思路**完全借鉴 Elkeid 的 Agent + 插件 + Server 架构**，采用相同的设计理念和通信协议。

### 2.1 组件划分

- **mxsec-agent（主进程）**
  - 部署在主机上，作为**插件基座**，负责：
    - 插件生命周期管理（启动、停止、升级）。
    - 与 Server 的双向通信（gRPC + mTLS）。
    - 资源监控与健康检查。
    - 服务发现与连接管理。
  - 技术栈：Golang（单二进制、systemd service）。
  - **不提供具体安全能力**，所有能力通过插件实现。

- **mxsec-baseline（基线检查插件）**
  - 作为 Agent 的子进程运行，负责：
    - 加载基线策略（从 Server 下发或本地文件）。
    - 执行基线检查（文件、命令、权限、sysctl 等）。
    - 执行基线修复（自动修复不合规配置项）。
    - 上报检测结果和修复结果。
  - **已实现 9 种检查器**：
    - `file_kv`：配置文件键值检查
    - `file_exists`：文件存在性检查
    - `file_permission`：文件权限检查
    - `file_line_match`：文件行匹配检查
    - `file_owner`：文件属主检查
    - `command_exec`：命令执行检查
    - `sysctl`：内核参数检查
    - `service_status`：服务状态检查
    - `package_installed`：软件包安装检查
  - 技术栈：Golang，通过 Pipe + Protobuf 与 Agent 通信。

- **Collector Plugin（资产采集插件）**
  - 作为 Agent 的子进程运行，负责：
    - 周期性采集主机资产信息（1-12 小时周期）
    - 上报资产数据
  - **已实现 10 种采集器**：
    - `ProcessHandler`：进程信息（PID、命令行、MD5、容器关联）
    - `PortHandler`：端口信息（TCP/UDP 监听端口、进程关联）
    - `UserHandler`：账户信息（用户列表、弱密码检测、sudoers）
    - `SoftwareHandler`：软件包信息（系统包、Python 包、JAR 包）
    - `ContainerHandler`：容器信息（Docker、containerd）
    - `AppHandler`：应用信息（数据库、消息队列、Web 服务）
    - `NetInterfaceHandler`：网卡信息
    - `VolumeHandler`：磁盘信息
    - `KmodHandler`：内核模块信息
    - `ServiceHandler` / `CronHandler`：系统服务与定时任务
  - 技术栈：Golang，通过 Pipe + Protobuf 与 Agent 通信。

- **FIM Plugin（文件完整性监控插件）**
  - 作为 Agent 的子进程运行，负责：
    - 基于 AIDE（Advanced Intrusion Detection Environment）检测文件变更
    - 支持文件新增、删除、修改检测，自动分类严重级别
  - **仅适用于 VM（物理机/虚拟机）**，容器环境不支持
  - **核心模块**：
    - `engine`：AIDE 配置渲染、报告解析（兼容 AIDE 0.15/0.19）、变更分类器
    - `config_renderer`：策略 JSON → `/etc/aide-mxsec.conf` 渲染（独立配置，不覆盖系统 AIDE）
    - `classifier`：基于文件路径自动分类（binary/auth/ssh/config/other × critical/high/medium/low）
    - `parser`：状态机解析 AIDE 输出，兼容 CentOS 7 和 Rocky 9
  - **5 套默认策略**：通用系统、数据库服务器、Web 服务器、容器宿主机、中间件
  - 技术栈：Golang，通过 Pipe + Protobuf 与 Agent 通信。

- **mxsec-server（后端）**
  - **AgentCenter（gRPC Server）**：
    - Agent 注册与心跳（gRPC 双向流）
    - 插件管理（版本控制、配置下发、升级）
    - 任务下发与状态追踪
    - 后台调度器（插件更新、Agent 更新、任务超时、告警调度）
  - **Manager（HTTP API Server）**：
    - **100+ 个 HTTP 端点**，22 个 API 处理器
    - 主机管理 API（列表、详情、指标、风险统计、插件状态）
    - 策略管理 API（CRUD、批量操作、导入导出）
    - 任务管理 API（创建、运行、取消、查询）
    - 基线修复 API（可修复项查询、修复任务管理）
    - 资产采集 API（进程、端口、用户、软件、容器等 11 种资产）
    - 报表 API（统计、趋势、任务报告、Top 规则/主机）
    - 告警管理 API（告警状态、批量操作）
    - 通知管理 API（Webhook 支持）
    - 组件管理 API（插件/Agent 包上传、版本管理、推送）
    - 用户管理 API（JWT 认证）
    - 系统配置 API（站点、告警、K8s 镜像配置）
  - **数据模型**：30+ 个数据表
  - 技术栈：Golang（gRPC + Gin + Gorm + Viper + Zap）、MySQL、mTLS

- **mxsec-console（前端 UI）**
  - **已实现页面**：
    - **Dashboard**：统计概览、主机状态分布、风险趋势
    - **主机管理**：列表（基线评分、风险等级、业务线筛选）、详情页（概览、基线风险、资产指纹、性能监控）
    - **策略管理**：策略列表、规则编辑器、策略详情（检查概览、检查项视角、影响的主机列表）
    - **策略组管理**：策略组列表、规则关联
    - **任务管理**：任务列表、任务创建、执行详情
    - **基线修复**：可修复项列表、修复任务管理、修复结果查看
    - **告警管理**：告警列表、告警详情、批量处理
    - **资产管理**：进程、端口、用户、软件、容器等资产展示
    - **业务线管理**：业务线列表、主机关联
    - **报表统计**：统计趋势、任务报告、导出功能
    - **系统设置**：安装指南、组件管理、通知配置、安全配置
    - **用户管理**：用户列表、权限管理
  - **组件数量**：45+ 个 Vue 组件
  - 技术栈：Vue 3 + TypeScript + Pinia + Ant Design Vue 4

- **Policy Repository（策略仓库）**
  - 将基线规则抽象为统一策略模型
  - **已实现 13 个规则文件**：
    - `ssh-baseline.json`：SSH 安全配置（24 条规则）
    - `password-policy.json`：密码策略
    - `file-permissions.json`：文件权限
    - `account-security.json`：账户安全
    - `service-status.json`：服务状态
    - `sysctl-security.json`：内核参数
    - `audit-logging.json`：审计日志
    - `network-protocols.json`：网络协议
    - `login-banner.json`：登录横幅
    - `secure-boot.json`：安全启动
    - `cron-security.json`：定时任务安全
    - `file-integrity.json`：文件完整性
    - `mac-security.json`：强制访问控制
  - 规则特性：多 OS 版本适配、检查逻辑、修复建议、修复命令、严重级别（Critical/High/Medium/Low）

### 2.2 通信协议

- **Agent ↔ Server**：
  - **gRPC 双向流**（`Transfer` 服务）
  - **mTLS 双向认证**（使用自签名证书）
  - 数据流：Agent → Server（心跳、检测结果、资产数据）
  - 控制流：Server → Agent（任务、配置、插件升级指令）
  - 使用 **Protobuf** 序列化，支持 **snappy 压缩**

- **Plugin ↔ Agent**：
  - **Pipe（管道）** 通信（父子进程）
  - Agent 通过 `os.Pipe()` 创建两个管道：
    - `rx`：Agent 从插件接收数据
    - `tx`：Agent 向插件发送任务
  - 使用 **Protobuf** 序列化
  - 插件数据**不二次解析**，Agent 直接透传到 Server（性能优化）

### 2.3 插件机制

- **插件生命周期**：
  - Server 下发插件配置（`proto.Config`：名称、版本、SHA256、下载地址）
  - Agent 验证签名并下载插件
  - Agent 启动插件进程（子进程）
  - Agent 管理插件生命周期（启动、停止、重启、升级）

- **插件通信**：
  - 插件通过 `plugins.Client` SDK 与 Agent 通信
  - `SendRecord()`：发送数据到 Agent
  - `ReceiveTask()`：接收 Agent 下发的任务
  - 使用文件描述符 3/4 进行 Pipe 通信

- **插件类型**：
  - **Baseline Plugin**：基线检查
  - **Collector Plugin**：资产采集
  - **FIM Plugin**：文件完整性监控（基于 AIDE，仅 VM）
  - 后续可扩展更多插件类型

---

## 3. v1 范围：操作系统基线

### 3.1 支持 OS

优先支持：

- Rocky Linux 9 / 10
- Oracle Linux 7 / 8 / 9 
- CentOS 7 / 8 / 9
- Debian 10 / 11 / 12

（后续可扩展更多发行版，通过 `os_family + os_version` 匹配策略）

### 3.2 基线检查维度

v1 初版建议覆盖：

1. **账号与认证**
   - 禁用无密码账号、禁止 root 远程登录（可配置例外）。
   - 密码复杂度策略（长度、复杂度、过期时间等）。
2. **权限与 sudo**
   - `/etc/sudoers` 与 `/etc/sudoers.d` 安全配置。
   - 禁止使用 NOPASSWD（或可配置白名单）。
3. **SSH 服务安全**
   - `PermitRootLogin`、`PermitEmptyPasswords`、`PasswordAuthentication` 等关键项。
   - SSH 协议版本、加密算法清单。
4. **系统服务与守护进程**
   - 禁用不必要的服务。
   - 核心安全服务（auditd、chrony/ntpd 等）状态。
5. **日志与审计**
   - `rsyslog`/`journald` 基本配置。
   - `auditd` 规则（若适用）。
6. **内核参数（sysctl）**
   - 例如内核网络安全参数、内存 dump 相关参数等。
7. **文件与目录权限**
   - 如 `/etc/passwd`、`/etc/shadow`、`/etc/ssh/*`、日志目录等。
8. **时间同步**
   - NTP/Chrony 配置、时间同步状态。

（具体规则可参考相关 CIS Benchmark、公司内部基线规范等，再抽象为统一策略模型。）

### 3.3 输出结果模型（建议）

- 对每条基线规则输出：

```json
{
  "rule_id": "LINUX_SSH_001",
  "host_id": "host-uuid",
  "os_family": "rocky",
  "os_version": "9.3",
  "severity": "high",
  "category": "ssh",
  "title": "SSH 禁止 root 远程登录",
  "status": "fail",         
  "actual": "PermitRootLogin yes",
  "expected": "PermitRootLogin no",
  "fix_suggestion": "修改 /etc/ssh/sshd_config 中的 PermitRootLogin 并重启 sshd",
  "checked_at": "2025-12-09T12:00:00+08:00"
}
```

- Server 聚合结果，按主机、按规则、按策略集做统计与展示。

---

## 4. 项目结构

> 实际可以随着开发迭代微调，这里作为 v1 初始规划。

### 5.1 目录结构

```text
mxcsec-platform/
├── Elkeid                   # Elkeid项目代码
├── cmd/
│   ├── server/
│   │   ├── manager/         # Manager HTTP API Server 主程序入口
│   │   └── agentcenter/     # AgentCenter gRPC Server 主程序入口
│   ├── agent/               # Agent 主程序入口
│   └── tools/               # 开发辅助工具（如策略转换、导入导出工具）
├── internal/
│   ├── server/
│   │   ├── manager/         # Manager 相关代码
│   │   │   ├── api/         # HTTP API 处理器
│   │   │   ├── biz/         # 业务逻辑（基线得分计算、任务管理等）
│   │   │   ├── router/      # 路由配置（独立维护，main.go 更简洁）
│   │   │   └── middleware/  # HTTP 中间件（日志、CORS 等）
│   │   ├── agentcenter/     # AgentCenter 相关代码
│   │   │   ├── transfer/    # Transfer 服务实现
│   │   │   ├── service/     # 业务逻辑（任务调度、策略匹配等）
│   │   │   ├── server/      # gRPC Server 创建和配置
│   │   │   └── scheduler/   # 任务调度器
│   │   ├── config/          # 配置管理
│   │   ├── database/        # 数据库连接
│   │   ├── logger/          # 日志初始化
│   │   ├── model/           # 数据模型
│   │   └── migration/       # 数据库迁移
│   └── agent/
│       ├── core/            # Agent 核心：配置、插件管理、任务调度
│       ├── plugin/          # 插件管理（生命周期、Pipe 通信）
│       ├── transport/       # 与 Server 通信（gRPC + mTLS）
│       ├── heartbeat/       # 心跳上报
│       └── connection/      # 连接管理（服务发现、mTLS）
├── plugins/
│   ├── baseline/           # 基线检查插件
│   │   ├── main.go         # 插件入口
│   │   ├── src/            # 检查引擎实现
│   │   └── config/         # 策略配置
│   ├── collector/          # 资产采集插件
│   │   ├── main.go         # 插件入口
│   │   ├── engine/         # 采集引擎
│   │   └── ...             # 各类采集器
│   ├── fim/                # 文件完整性监控插件
│   │   ├── main.go         # 插件入口
│   │   └── engine/         # FIM 引擎（AIDE 配置渲染、报告解析、变更分类）
│   └── lib/                # 插件 SDK（Go/Rust）
│       ├── go/             # Go 插件 SDK
│       └── rust/           # Rust 插件 SDK（可选）
├── pkg/                     # 可复用库（日志、配置、通用工具等）
├── api/
│   ├── proto/               # gRPC / Protobuf 定义（如 Agent 通信协议）
│   └── http/                # OpenAPI/Swagger 定义
├── ui/                      # 前端工程（Vue3 + TS）
│   ├── src/
│   └── ...
├── configs/
│   ├── server.yaml          # Server 配置
│   ├── agent.yaml           # Agent 配置
│   └── policies/            # 默认基线策略（按 OS 分类）
├── deploy/
│   ├── docker-compose/      # 本地开发环境
│   ├── k8s/                 # K8s 部署清单（后期）
│   └── systemd/             # systemd service 样例
├── docs/
│   ├── design/              # 设计文档
│   ├── baseline/            # 基线规则说明
│   └── elkeid-notes/        # Elkeid 研究笔记
└── .cursor/
    └── rules/
        └── common.mdc  # 本项目的 Cursor 规则文件
```

### 5.2 代码组织原则

**遵循 Go 最佳实践，保持代码结构清晰：**

1. **main.go 保持简洁**：
   - `cmd/server/manager/main.go`：只负责启动逻辑（调用初始化包、设置路由、启动服务、信号处理）
   - `cmd/server/agentcenter/main.go`：只负责启动逻辑（调用初始化包、启动后台服务、启动 gRPC Server、信号处理）
   - 所有初始化逻辑提取到独立的 `setup` 包中

2. **模块化设计**：
   - `internal/server/manager/setup/`：Manager 服务初始化逻辑（配置加载、日志、数据库、业务服务等）
   - `internal/server/manager/router/`：所有 HTTP 路由配置
   - `internal/server/manager/middleware/`：HTTP 中间件
   - `internal/server/agentcenter/setup/`：AgentCenter 服务初始化逻辑（配置加载、日志、数据库、gRPC Server 等）
   - `internal/server/agentcenter/server/`：gRPC Server 创建和配置
   - `internal/server/agentcenter/scheduler/`：任务调度器

3. **编译隔离**：
   - Agent、AgentCenter、Manager 独立编译，不会相互包含代码
   - 每个组件只包含自己需要的依赖

---

## 5. 开发建议与工作流

> 具体的细节规则在 `.cursor/rules/baseline-security.mdc` 中，这里只做概要。

- 使用 Git 分支模型（`main` + feature 分支）：
  - `main`：稳定、可部署版本；
  - `feat/*`：新功能；
  - `fix/*`：缺陷修复。

- Commit 规范（简化版）：
  - `feat: add baseline ssh check`
  - `fix: handle agent heartbeat timeout`
  - `refactor: split baseline engine modules`

- 每个功能开发前：
  - 先在 Issue / Task / TODO 里列出子任务；
  - Cursor 中先阅读 `.mdc` 文件，确认本次开发目标；
  - 完成一个子任务勾掉一个，保持改动集中且可回溯。

---

## 6. Roadmap

> 后续可以拆到 GitHub Project / 自研任务系统里。

### v0.1 – Elkeid 研究 & PoC ✅

- [x] 完成本 README 与 .mdc 规则文件。
- [x] 通读 Elkeid 文档与 Agent 文档，输出架构与插件机制笔记。
- [x] 阅读 baseline 插件核心代码，输出策略模型 & 检测流程示意图。
- [x] 设计自己的 Policy 模型 & Agent 通信协议草案。

### v0.2 – 基本 Agent + Server 通路 ✅

- [x] 实现最小可用的 Agent（定期上报心跳 + 主机基本信息）。
- [x] 实现 Baseline Plugin（策略加载、规则执行、6 种检查器）。
- [x] 实现插件管理（生命周期管理、Pipe 通信、配置同步）。
- [x] 完成 Baseline Plugin 单元测试（所有检查器测试通过）。
- [x] 实现 Server API（AgentCenter gRPC Server、Manager HTTP API）。
- [x] 实现数据库模型（hosts、policies、rules、scan_results、scan_tasks）。
- [x] 实现检测结果存储与查询。
- [x] 实现基线得分计算和缓存机制。
- [x] 实现任务状态自动更新机制。

### v0.3 – Console & 策略管理 ✅

- [x] 实现 UI 主机列表与简单基线结果展示。
- [x] 实现策略管理页面（规则列表 + 简单编辑）。
- [x] 实现策略详情页面（检查概览、检查项视角、影响的主机列表）。
- [x] 实现基线扫描任务管理（手动触发 / 定时扫描）。
- [x] 实现 Dashboard 页面（统计概览）。
- [x] 实现用户认证系统（JWT Token）。
- [x] 完善开发文档和故障排查指南。
- [x] 改进用户体验（错误提示、操作反馈）。
- [x] 实现字段状态显示功能（区分"有值"、"未采集"、"无数据"状态）。
- [x] 实现系统配置管理（站点配置、Logo上传、Kubernetes镜像配置）。
- [x] 实现告警管理模块（告警列表、告警详情）。
- [x] 实现业务线管理模块（业务线列表、业务线详情）。
- [x] 实现通知管理模块（通知列表、通知详情）。
- [x] 清理过时文档和脚本，优化项目结构。

### v1.0 – OS 基线稳定版本 ✅

**核心功能已完成，生产可用**

- [x] Agent + Plugin 架构完整实现
- [x] 9 种检查器、10 种采集器
- [x] 13 个规则文件，覆盖主要安全基线
- [x] 基线自动修复功能（单机/批量修复、配置化服务重启）
- [x] 100+ 个 HTTP API 端点、30+ 个数据表
- [x] 完整的 Web 控制台（45+ 个 Vue 组件）
- [x] 多 OS 适配（Rocky 9、CentOS 7/8、Oracle Linux、Debian、Ubuntu）
- [x] Docker Compose 和 Systemd 两种部署方式
- [x] 完善的部署文档与操作手册
- [x] FIM 文件完整性监控插件（基于 AIDE，5 套默认策略，仅 VM）

### v1.1 – 架构区分与告警对接（规划中）

- [ ] **架构区分功能**：识别主机运行环境（VM/Docker/K8s），按环境过滤基线规则
- [ ] **告警对接增强**：支持 Lark、Teams、邮件等更多告警通道
- [ ] **CIS 基线完善**：按 CIS Rocky Linux 9 Benchmark 补充 P0/P1 级别规则

### v2.0 – 中间件基线（规划中）

- [ ] Nginx 基线检查
- [ ] Redis 基线检查
- [ ] MySQL 基线检查
- [ ] 其他中间件基线

---

## 7. 文档索引

### 7.1 部署文档

- [生产环境部署方案](docs/deployment/production-deployment.md) - 完整的生产环境部署指南（推荐）
- [Server 部署指南](docs/deployment/server-deployment.md) - Server 部署方式（Docker Compose、二进制部署）
- [Server 配置文档](docs/deployment/server-config.md) - Server 配置选项详解
- [Agent 部署指南](docs/deployment/agent-deployment.md) - Agent 部署和配置
- [发行版支持](docs/deployment/distribution-support.md) - 支持的 Linux 发行版列表
- [快速开始](docs/deployment/quick-start.md) - 快速部署指南

### 7.2 开发文档

- [快速开始指南](docs/development/quick-start.md) - 快速搭建开发环境
- [开发指南](docs/development/development-guide.md) - 开发流程和规范
- [故障排查指南](docs/development/troubleshooting.md) - 常见问题解决方案
- [插件开发指南](docs/development/plugin-development.md) - 如何开发插件和扩展检查器
- [Agent 架构设计](docs/design/agent-architecture.md) - Agent 架构和设计
- [Baseline 策略模型](docs/design/baseline-policy-model.md) - 策略模型设计
- [Server API 设计](docs/design/server-api.md) - Server API 接口设计

### 7.3 测试文档

- [前端 API 集成测试](docs/testing/frontend-api-integration-test.md) - 前端 API 集成测试指南
- [验证清单](docs/testing/verification-checklist.md) - 功能验证清单

### 7.4 功能文档

- [字段状态显示说明](docs/features/field-status-display.md) - 字段状态显示功能说明
- [系统配置路由说明](docs/deployment/system-config-routes.md) - 系统配置 API 路由注册说明
- [基线修复功能说明](docs/BASELINE_FIX_IMPLEMENTATION.md) - 基线自动修复功能实现说明

### 7.5 规则编写

- [规则编写指南](docs/RULE_WRITING_GUIDE.md) - 如何编写基线检查规则
- [基线导入导出指南](docs/BASELINE_IMPORT_EXPORT_GUIDE.md) - 基线策略导入导出操作指南

### 7.6 其他文档

- [TODO 列表](docs/TODO.md) - 项目开发任务和进度
- [Agent 连接故障排查](docs/AGENT_CONNECTION_TROUBLESHOOTING.md) - Agent 连接问题排查指南
- [Agent 更新指南](docs/AGENT_UPDATE.md) - Agent 升级和更新说明

---

## 8. 项目统计

| 模块 | 规模 |
|------|------|
| Agent | 15 个 Go 文件 |
| Server API | 22 个处理器，13,000+ 行代码 |
| 数据模型 | 30+ 个数据表 |
| Baseline Plugin | 9 种检查器，13 个规则文件 |
| Collector Plugin | 10 种采集器 |
| FIM Plugin | 基于 AIDE 的文件完整性监控，5 套默认策略 |
| 前端 UI | 45+ 个 Vue 组件 |
| HTTP 端点 | 100+ 个 |
| 单元测试 | 15 个测试文件 |

---

## 9. License & 备注

- 本项目为独立实现，仅在设计理念上借鉴 Elkeid 的架构与插件思想，不直接复制其代码。
- 实际使用时需注意：
  - 遵守 Elkeid 及其相关组件的开源协议；
  - 公司内部基线规则可能涉及敏感信息，应放在私有仓库或私有策略库中。
