# Matrix Cloud Security Platform - TODO List

> 本文档记录项目的开发任务和进度。

---

## Phase 0: Elkeid 研究与设计

### ✅ 已完成

- [x] 阅读 Elkeid 代码，理解 Agent 架构
- [x] 阅读 Elkeid Baseline 插件代码，理解策略模型和检查引擎
- [x] 阅读 Elkeid Collector 插件代码，理解资产采集机制
- [x] 阅读 Elkeid 插件 SDK，理解 Pipe 通信机制
- [x] 完成 Elkeid 架构分析文档
- [x] 设计我们的策略模型（参考 Elkeid，优化 OS 匹配）
- [x] 设计 Agent 架构（完全参考 Elkeid，采用插件机制）
- [x] 设计 Server API 接口（gRPC + HTTP）
- [x] 设计数据库模型（hosts、policies、rules、scan_results、资产表等）

---

## Phase 1: MVP（最小可行产品）

> **目标**：实现最小可用的 Agent + Server + Baseline Plugin，支持基本的基线检查和资产采集。

### 1.0 基础设施

#### 插件 SDK 开发
- [x] 实现 Go 插件 SDK（`plugins/lib/go/`）
  - [x] `Client` 结构体（Pipe 读写封装）
  - [x] `SendRecord()` 方法（发送数据到 Agent）
  - [x] `ReceiveTask()` 方法（接收 Agent 任务）
  - [x] Protobuf 序列化/反序列化
  - [x] 错误处理与重试

#### Protobuf 定义
- [x] 定义 `bridge.proto`（插件 ↔ Agent 通信协议）
- [x] 定义 `grpc.proto`（Agent ↔ Server 通信协议）
- [x] 生成 Go 代码（protoc）
- [x] 扩展 `grpc.proto` 支持 AgentConfig 和 CertificateBundle

### 1.1 Agent 开发

#### 基础框架
- [x] Agent 主程序入口（main.go）
  - [x] 配置加载（完全依赖构建时嵌入，无需配置文件）
  - [x] 日志初始化（Zap，JSON 输出，按天轮转，保留30天）
  - [x] Agent ID 管理（从文件读取或生成）
  - [x] 信号处理（SIGTERM, SIGINT）
  - [x] 优雅退出

#### 连接管理
- [x] 服务发现（ServiceDiscovery）- 简化实现，直接使用配置的 Server 地址
- [x] mTLS 配置（CA、证书、密钥）- 证书由 Server 下发
- [x] gRPC 连接建立与管理
- [x] 连接重试与故障转移

#### 传输模块
- [x] gRPC 双向流实现
- [x] 数据打包与发送（PackagedData）
- [x] 命令接收与处理（Command）
- [x] Agent 配置更新处理（AgentConfig）
- [x] 证书包更新处理（CertificateBundle）
- [ ] snappy 压缩支持（可选优化）
- [x] 错误处理与重试

#### 心跳模块
- [x] 定时心跳（默认 60 秒，可由 Server 配置）
- [x] Agent 状态采集（CPU、内存、启动时间）- 基础实现
- [x] 主机信息采集（OS、内核、IP、主机名）- 已完成：读取 /etc/os-release、/proc/version、网络接口
- [x] 插件状态采集（已完成：通过接口获取插件状态，包含在心跳数据中）
- [x] gRPC 心跳上报

#### 插件管理
- [x] 插件配置同步（从 Server 接收）- 已完成：通过 transport 模块接收配置
- [x] 插件签名验证与下载 - 已完成：HTTP 下载、SHA256 校验、自动重试、可执行权限设置
- [x] 插件进程启动（Pipe 创建）- 已完成：创建 rx/tx 管道，启动子进程
- [x] 插件数据接收（从 Pipe 读取）- 已完成：receiveData goroutine 实现
- [x] 插件任务发送（写入 Pipe）- 已完成：从 transport 接收任务，按插件名称分发，序列化发送
- [x] 插件生命周期管理（启动、停止、重启、升级）- 已完成：loadPlugin、stopPlugin、SyncPlugins

#### Baseline Plugin
- [x] 插件入口（main.go）- 已完成：任务接收、处理、结果上报
- [x] 插件 SDK 集成（plugins.Client）- 已完成：通过 Pipe 与 Agent 通信
- [x] 策略加载与解析（JSON）- 已完成：支持 JSON 格式的策略配置
- [x] OS 匹配逻辑 - 已完成：MatchOS 方法，支持 os_family 和 os_version 匹配
- [x] 规则执行框架 - 已完成：Engine.Execute、executeRule、executeCheck
- [x] 检查器实现：
  - [x] `file_kv`（配置文件键值检查）- 已完成：支持多种键值格式
  - [x] `file_line_match`（文件行匹配）- 已完成：支持正则匹配、匹配/不匹配检查
  - [x] `file_permission`（文件权限检查）- 已完成：支持 8 进制权限比较
  - [x] `command_exec`（命令执行）- 已完成：支持命令执行和输出匹配
  - [x] `sysctl`（内核参数检查）- 已完成：支持 sysctl 参数值检查和正则匹配
  - [x] `service_status`（服务状态检查）- 已完成：支持 systemd/SysV 服务状态检查
  - [x] `file_owner`（文件属主检查）- 已完成：支持 uid:gid 和 username:groupname 格式，支持用户名/组名解析
  - [x] `package_installed`（软件包检查）- 已完成：支持 RPM 和 DEB 包管理器，支持版本约束（>=、<=、==、>、<）
- [x] 结果生成与上报 - 已完成：生成 Result，通过 bridge.Record 上报

#### Baseline Plugin 示例规则
- [x] SSH 配置检查（PermitRootLogin）- 已完成：ssh-baseline.json（3条规则）
- [x] 密码策略检查（PASS_MAX_DAYS）- 已完成：password-policy.json（2条规则）
- [x] 文件权限检查（/etc/passwd, /etc/shadow）- 已完成：file-permissions.json（3条规则）
- [x] 至少 3 条示例规则用于测试 - 已完成：共5个策略文件，包含多个规则
- [x] 验证和完善示例规则 - 已完成：验证所有规则文件格式、运行端到端测试验证规则执行

#### Collector Plugin（Phase 1 可选，Phase 2 必须）
- [x] 插件入口（main.go）- 已完成：main.go 实现完整，支持任务接收和定时采集
- [x] 插件 SDK 集成（plugins.Client）- 已完成：通过 Pipe 与 Agent 通信
- [x] 采集引擎（engine）- 已完成：Engine 实现完整，支持定时采集和任务触发
- [x] 基础采集器实现（Phase 1 MVP）：
  - [x] 进程采集（ProcessHandler）- 已完成：支持进程信息采集、MD5 计算、容器检测
  - [x] 端口采集（PortHandler）- 已完成：支持 TCP/UDP 端口采集、进程关联
  - [x] 账户采集（UserHandler）- 已完成：支持账户信息采集、密码检测
- [x] 完整采集器实现（Phase 2）：
  - [x] 软件包采集（SoftwareHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 容器采集（ContainerHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 应用采集（AppHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 硬件采集（NetInterfaceHandler, VolumeHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 内核模块采集（KmodHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 系统服务采集（ServiceHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
  - [x] 定时任务采集（CronHandler）- 已完成：代码已实现，但 Server 端暂不存储（Phase 2.3）
- [x] 资产数据上报 - 已完成：通过 bridge.Record 上报，支持所有资产类型（5050-5060）

### 1.2 Server 开发

> **当前阶段重点**：实现 Server 端核心功能，支持 Agent 连接和数据接收。

#### 1.2.1 数据库模型（**优先实现**）✅
- [x] 定义数据库模型（Gorm）
  - [x] `hosts` 表（主机信息：host_id、hostname、os_family、os_version、kernel_version、arch、ipv4、status、last_heartbeat 等）
  - [x] `policies` 表（策略集：policy_id、name、description、os_family、os_version、enabled、created_at 等）
  - [x] `rules` 表（规则：rule_id、policy_id、category、title、description、severity、check_type、check_param、fix_suggestion 等）
  - [x] `scan_results` 表（检测结果：id、host_id、rule_id、task_id、status、actual、expected、checked_at 等）
  - [x] `scan_tasks` 表（扫描任务：task_id、policy_id、target_hosts、status、created_at、executed_at 等）
  - [x] 资产表（processes、ports、asset_users）- 已完成：Process、Port、AssetUser 模型已定义并注册到 AllModels，数据库迁移会自动创建这些表
- [x] 编写数据库迁移脚本（Gorm AutoMigrate）
- [x] 创建初始化数据（默认策略、示例规则）

#### 1.2.2 AgentCenter（gRPC Server）
- [x] AgentCenter 主程序入口（`cmd/server/agentcenter/main.go`）
- [x] 配置加载（Viper + YAML，`configs/server.yaml.example`）
- [x] 日志初始化（Zap，JSON 输出）
- [x] gRPC Server 启动（监听端口，默认 6751）
- [x] mTLS 配置（CA、证书、密钥）- 支持证书生成脚本
- [x] 数据库连接（Gorm + MySQL/PostgreSQL）
- [x] `Transfer` 服务实现（双向流）
  - [x] 接收 Agent 数据流（`stream PackagedData`）
  - [x] 发送命令流（`stream Command`）
- [x] 接收 Agent 数据（心跳、检测结果、资产数据）
  - [x] 解析 `PackagedData` 和 `EncodedRecord`
  - [x] 根据 `data_type` 路由到不同处理器
  - [x] DataType=1000：心跳数据 → 更新 `hosts` 表
  - [x] DataType=8000：基线检查结果 → 插入 `scan_results` 表
  - [x] DataType=5050-5064：资产数据 → 插入对应资产表（Phase 2）- 已完成：基础三种类型（进程、端口、账户）已实现存储，其他类型暂记录日志（Phase 2.3）
  - [x] 存储到数据库
- [x] 下发任务和配置到 Agent
  - [x] 查询 `scan_tasks` 表，获取待执行任务
  - [x] 封装为 `Command` 并发送到 Agent
  - [ ] 插件配置更新（Config）下发（可选，后续实现）
- [x] mTLS 双向认证（验证 Agent 证书）
- [x] 连接状态管理（Map[agent_id]*Connection）
- [x] 连接断开处理（清理连接状态）

#### 1.2.3 Manager（HTTP API Server）
- [x] Manager 主程序入口（`cmd/server/manager/main.go`）
- [x] 配置加载（Viper + YAML）
- [x] 日志初始化（Zap，JSON 输出）
- [x] HTTP Server（Gin，默认端口 8080）
- [x] 数据库连接（Gorm + MySQL/PostgreSQL）
- [x] 中间件（CORS、日志、Recovery）

#### Manager（HTTP API）
- [x] `GET /api/v1/hosts`：获取主机列表（支持分页、过滤）
- [x] `GET /api/v1/hosts/{host_id}`：获取主机详情（包含基线结果和最新监控数据）
- [x] `GET /api/v1/hosts/{host_id}/metrics`：获取主机监控数据（支持 MySQL 和 Prometheus 查询）
- [x] `GET /api/v1/policies`：获取策略列表
- [x] `POST /api/v1/policies`：创建策略
- [x] `PUT /api/v1/policies/{policy_id}`：更新策略
- [x] `DELETE /api/v1/policies/{policy_id}`：删除策略
- [x] `GET /api/v1/policies/{policy_id}/statistics`：获取策略统计信息（通过率、主机数、检查项数等）- 已完成：返回通过率、主机数、检查项数、风险项数、最近检查时间等统计信息
- [x] `POST /api/v1/tasks`：创建扫描任务
- [x] `GET /api/v1/tasks`：获取任务列表
- [x] `POST /api/v1/tasks/{task_id}/run`：执行任务
- [x] `GET /api/v1/results`：获取检测结果（支持按主机、按规则、按策略过滤）
- [x] `GET /api/v1/results/host/{host_id}/score`：获取主机基线得分
- [x] `GET /api/v1/results/host/{host_id}/summary`：获取主机基线摘要
- [x] `POST /api/v1/auth/login`：用户登录
- [x] `POST /api/v1/auth/logout`：用户登出
- [x] `GET /api/v1/auth/me`：获取当前用户信息
- [x] `GET /api/v1/dashboard/stats`：获取 Dashboard 统计数据
- [x] `GET /api/v1/assets/*`：获取资产数据（Phase 2）- 已完成：`/api/v1/assets/processes`、`/api/v1/assets/ports`、`/api/v1/assets/users` 已实现

#### Manager 业务逻辑
- [x] 主机注册与更新（基于心跳数据自动注册/更新）- 已在 AgentCenter 实现
- [x] 策略匹配（根据 OS 信息匹配适用的策略和规则）- 已在 AgentCenter 实现基础服务
- [x] 检测结果存储与查询（聚合、统计）- 已在 AgentCenter 实现存储
- [x] 扫描任务管理（创建、执行、查询、状态更新）- 已完成
- [x] 基线得分计算和缓存机制 - 已完成
- [x] 任务状态自动更新机制 - 已完成
- [x] 错误处理和重试逻辑 - 已完成
- [x] 资产数据查询（按主机、按类型）- Phase 2 - 已完成：基础三种类型（进程、端口、账户）已实现查询 API

#### ServiceDiscovery（服务发现）- **可选，Phase 1 可简化**
- [ ] ServiceDiscovery 主程序入口
- [ ] 服务注册接口（gRPC 服务注册）
- [ ] 服务发现接口（HTTP API，供 Agent 查询）
- [ ] 服务健康检查
- [ ] 服务列表管理
- [ ] **注意**：Phase 1 可以简化实现，Agent 直接使用配置的 Server 地址

### 1.3 部署与测试

#### 打包与部署
- [x] Agent 构建脚本（支持构建时嵌入 Server 地址）
- [x] Agent 安装脚本（一键安装，自动下载对应架构的安装包）
- [x] Agent 打包脚本（RPM/DEB，使用 nFPM）- 已完成：`scripts/package-agent.sh`
- [ ] Baseline Plugin 打包脚本 - 可选，后续实现
- [ ] Collector Plugin 打包脚本（可选，Phase 2）
- [x] systemd service 文件 - 已完成：`deploy/systemd/`（Agent、AgentCenter、Manager）
- [x] Server Docker Compose 配置 - 已完成：`deploy/`
- [x] 证书生成脚本（mTLS）- Server 端实现（`scripts/generate-certs.sh`）

#### 测试
- [x] Agent 单元测试（配置、日志、ID 管理）- 已完成基础测试
- [x] 插件管理单元测试 - 已完成基础测试
- [x] Baseline Plugin 单元测试（检查器）- 已完成：所有检查器（file_kv、file_permission、file_line_match、command_exec、sysctl、service_status、file_owner、package_installed）测试通过
- [x] Manager API 集成测试 - 已完成（policies、tasks、results API 测试）
- [ ] Server 单元测试（业务逻辑层）- 部分完成
- [x] 端到端测试（Agent + Server + Plugin 完整流程）- 已完成：包括基线检查流程和资产采集流程（进程、端口、账户）

#### 文档
- [x] Agent 部署文档（一键安装、手动安装、源码编译）
- [x] Agent 配置设计文档（构建时嵌入、日志配置）
- [x] Server 部署文档 - 已完成：`docs/deployment/server-deployment.md`
- [x] Server 配置文档 - 已完成：`docs/deployment/server-config.md`
- [x] 开发文档（如何开发插件、如何扩展检查器）- 已完成：`docs/development/plugin-development.md`

---

## Phase 2: 功能完善

### 2.1 Agent 增强

- [x] 插件热更新机制 - 已完成：平滑切换、回滚机制、版本检测
- [x] 插件版本管理 - 已完成：语义化版本比较、版本解析、版本历史记录
- [x] 更多检查器实现：
  - [x] `sysctl`（内核参数检查）- 已完成（提前实现）
  - [x] `service_status`（服务状态检查）- 已完成（提前实现）
  - [x] `file_owner`（文件属主检查）- 已完成：支持 uid:gid 和 username:groupname 格式，支持用户名/组名解析
  - [x] `package_installed`（软件包检查）- 已完成：支持 RPM 和 DEB 包管理器，支持版本约束（>=、<=、==、>、<）
- [x] 检查结果本地缓存（断网时暂存）- 已完成：本地文件缓存、自动重试、缓存清理策略
- [x] 资源监控与上报 - 已完成：CPU、内存、磁盘、网络指标采集和上报
- [ ] 完整资产采集（所有类型）- Collector Plugin 已实现基础采集器，完整版待实现

### 2.2 Server 增强

- [x] 策略管理 API（CRUD）- 已完成
- [x] 主机管理 API（列表、详情）- 已完成
- [x] 检测结果查询 API（按主机、按规则、按策略）- 已完成
- [x] 扫描任务管理 API（创建、执行、查询）- 已完成
- [x] 统计与聚合（基线得分、通过率等）- 已完成
- [x] 监控数据查询 API（MySQL 和 Prometheus 混合查询）- 已完成
  - [x] Prometheus 查询客户端实现
  - [x] MySQL 关联查询监控数据（GetHost API）
  - [x] Prometheus + MySQL 混合查询服务
  - [x] GET /api/v1/hosts/:host_id/metrics API 端点

### 2.3 前端 UI ✅

- [x] 主机列表页面 - 已完成：支持筛选、基线得分展示、分页
- [x] 主机详情页面（基线得分、规则列表）- 已完成：基本信息、基线得分、检查结果列表、性能监控数据展示
- [x] 策略管理页面（策略列表、规则编辑）- 已完成：列表、创建、编辑、删除、启用/禁用
- [x] 策略详情页面（基线检查详情）- 已完成：检查概览（通过率、主机数、检查项数）、检查项视角（规则列表和详情）、影响的主机列表、策略统计 API 集成
- [x] 扫描任务管理页面 - 已完成：列表、创建、执行任务
- [x] 资产数据展示 - 已完成：资产指纹统计展示、进程列表、端口列表、用户列表
- [x] Dashboard 页面 - 已完成：统计概览、主机状态分布、主机风险分布、基线风险 Top 3
- [x] 前端 API 集成 - 已完成：主机监控数据 API、主机状态/风险分布 API、策略统计信息 API
- [x] 前端 API 测试和验证 - 已完成：测试脚本、测试文档、代码质量检查
- [x] 用户体验改进 - 已完成：全局错误提示、操作成功提示、改进 API 错误处理
- [x] 开发文档完善 - 已完成：快速开始指南、开发指南、故障排查指南
- [x] 统计报表页面 - 已完成：前端页面和后端 API 均已实现，包含完整的报表统计、基线得分趋势和检查结果趋势功能
- [x] 字段状态显示功能 - 已完成：区分"有值"、"未采集"、"无数据"状态，支持硬件信息、时间字段、IP地址字段的特殊处理
- [x] 系统配置管理 - 已完成：站点配置、Logo上传、Kubernetes镜像配置、系统配置 API 路由
- [x] 告警管理模块 - 已完成：告警列表页面、告警详情、告警数据模型和 API
- [x] 业务线管理模块 - 已完成：业务线列表页面、业务线详情、业务线数据模型和 API
- [x] 通知管理模块 - 已完成：通知列表页面、通知详情、通知数据模型和 API
- [x] 项目结构优化 - 已完成：清理过时文档和脚本，优化项目结构

---

## Phase 2.5: CIS 基线规则完善（Linux VM）

> **目标**：按照 CIS Rocky Linux 9 Benchmark 完善系统安全基线规则，并实现架构区分（虚拟机 vs Docker 容器）。
> **适用范围**：仅适用于 Linux 虚拟机，不适用于 Docker 容器。
> **添加时间**：2025-12-18

### 2.5.0 架构区分功能（优先级：P0）⏳

> **背景**：当前基线检查会应用到所有主机（包括 Docker 容器），导致容器误报（如容器没有 SSH 导致 SSH 规则全部告警）。需要先实现架构区分功能。

- [ ] **主机架构识别**
  - [ ] Agent 端识别运行环境（VM/Docker/K8s Pod）
    - [ ] 检测 `/.dockerenv` 文件是否存在
    - [ ] 检测 `/proc/1/cgroup` 是否包含 docker/containerd
    - [ ] 检测 `KUBERNETES_SERVICE_HOST` 环境变量
  - [ ] 心跳数据增加 `runtime_type` 字段（vm/docker/k8s）
  - [ ] Host 模型增加 `runtime_type` 字段
  - [ ] AgentCenter 处理心跳时更新 `runtime_type`

- [ ] **策略/规则架构适配**
  - [ ] Policy/Rule 模型增加 `runtime_types` 字段（支持多选：["vm", "docker", "k8s"]）
  - [ ] 现有规则默认设置为 `["vm"]`（仅虚拟机适用）
  - [ ] AgentCenter 下发任务时根据主机 `runtime_type` 过滤规则
  - [ ] Baseline Plugin 执行时二次过滤确认

- [ ] **UI 适配**
  - [ ] 主机列表显示运行环境类型
  - [ ] 策略/规则编辑支持选择适用的运行环境
  - [ ] 告警列表可按运行环境过滤

---

### 2.5.1 P0 严重/高危安全基线（优先级：P0）⏳

> **说明**：以下为严重（Critical）和高危（High）级别的安全基线，必须优先完善。

#### 2.5.1.1 访问与认证（Access & Authentication）

- [ ] **SELinux/MAC 强制访问控制** - 新增 `mac-security.json`
  - [ ] MAC_001: SELinux 已安装（libselinux 包存在）
  - [ ] MAC_002: SELinux 未在 bootloader 中禁用（grub 无 selinux=0）
  - [ ] MAC_003: SELinux 策略已配置（SELINUXTYPE=targeted）
  - [ ] MAC_004: SELinux 模式为 enforcing（**Critical**）
  - [ ] MAC_005: 无 unconfined 服务

- [ ] **安全启动（Secure Boot）** - 新增 `secure-boot.json`
  - [ ] BOOT_001: GRUB 引导密码已设置
  - [ ] BOOT_002: 单用户模式需要认证（**Critical**）
  - [ ] BOOT_003: 禁止 Ctrl-Alt-Del 重启
  - [ ] BOOT_004: GRUB 配置文件权限正确（600）

- [ ] **用户环境安全** - 补充到 `account-security.json`
  - [ ] USER_001: 不存在 .rhosts 文件（**Critical**）
  - [ ] USER_002: 不存在 .netrc 文件
  - [ ] USER_003: 不存在 .forward 文件
  - [ ] USER_004: root PATH 不含当前目录 (.)（**Critical**）
  - [ ] USER_005: root PATH 不含组可写目录

#### 2.5.1.2 审计与取证（Audit & Forensics）

- [ ] **审计规则增强** - 补充到 `audit-logging.json`
  - [ ] AUDIT_016: 审计文件权限修改 (chmod)
  - [ ] AUDIT_017: 审计文件属主修改 (chown)
  - [ ] AUDIT_018: 审计文件属性修改 (setxattr)
  - [ ] AUDIT_019: 审计失败的文件访问 (EACCES/EPERM)
  - [ ] AUDIT_020: 审计特权命令执行（**Critical**，所有 SUID/SGID）
  - [ ] AUDIT_021: 审计内核模块操作（**Critical**，insmod/rmmod）
  - [ ] AUDIT_022: 审计 MAC 策略修改
  - [ ] AUDIT_023: 审计规则不可变 (-e 2)
  - [ ] AUDIT_024: 审计会话初始化 (utmp/wtmp/btmp)
  - [ ] AUDIT_025: 审计成功的挂载操作

#### 2.5.1.3 系统完整性（System Integrity）

- [ ] **文件完整性检查** - 新增 `file-integrity.json`
  - [ ] AIDE_001: AIDE 已安装
  - [ ] AIDE_002: AIDE 数据库已初始化
  - [ ] AIDE_003: AIDE 完整性检查定期运行（cron）
  - [ ] AIDE_004: AIDE 配置文件权限正确

#### 2.5.1.4 SSH 和远程访问（SSH & Remote Access）

- [ ] **SSH 安全增强** - 补充到 `ssh-baseline.json`
  - [ ] SSH_016: SSH LogLevel 设置为 INFO 或 VERBOSE
  - [ ] SSH_017: SSH 使用强加密算法 Ciphers（**Critical**）
  - [ ] SSH_018: SSH 使用强 MAC 算法（**Critical**）
  - [ ] SSH_019: SSH 使用强密钥交换算法 KexAlgorithms（**Critical**）
  - [ ] SSH_020: SSH PermitUserEnvironment 禁用
  - [ ] SSH_021: SSH GSSAPIAuthentication 禁用
  - [ ] SSH_022: SSH AllowUsers/AllowGroups 已配置
  - [ ] SSH_023: SSH UseDNS 禁用

#### 2.5.1.5 内核和 sysctl 安全

- [ ] **内核参数增强** - 补充到 `sysctl-security.json`
  - [ ] SYSCTL_026: 限制用户命名空间 (user.max_user_namespaces=0)
  - [ ] SYSCTL_027: 限制性能监控 (kernel.perf_event_paranoid)
  - [ ] SYSCTL_028: IPv6 默认禁止源路由
  - [ ] SYSCTL_029: IPv6 默认禁止路由广告

- [ ] **禁用不安全网络协议** - 新增 `network-protocols.json`
  - [ ] NET_001: 禁用 DCCP 协议模块
  - [ ] NET_002: 禁用 SCTP 协议模块
  - [ ] NET_003: 禁用 RDS 协议模块
  - [ ] NET_004: 禁用 TIPC 协议模块
  - [ ] NET_005: 禁用无线接口

#### 2.5.1.6 服务暴露面（Service Exposure）

- [ ] **服务状态增强** - 补充到 `service-status.json`
  - [ ] SERVICE_021: 禁用 rpcbind 服务（除非 NFS 需要）
  - [ ] SERVICE_022: 禁用 postfix 仅本地监听
  - [ ] SERVICE_023: 禁用 ldap 客户端（除非需要）
  - [ ] SERVICE_024: X Window System 未安装（服务器）

---

### 2.5.2 P1 中危安全基线（优先级：P1）⏳

> **说明**：以下为中危（Medium）级别的安全基线，在 P0 完成后实现。

#### 2.5.2.1 账号和密码策略

- [ ] **PAM 认证增强** - 补充到 `password-policy.json`
  - [ ] PAM_001: su 命令限制为 wheel 组 (pam_wheel.so)
  - [ ] PAM_002: 账户非活动锁定时间 (INACTIVE=30)
  - [ ] PAM_003: pam_pwhistory 配置正确
  - [ ] PAM_004: 新用户默认密码过期时间

#### 2.5.2.2 文件权限与卫生

- [ ] **Cron 定时任务安全** - 新增 `cron-security.json`
  - [ ] CRON_001: /etc/cron.hourly 权限 700
  - [ ] CRON_002: /etc/cron.daily 权限 700
  - [ ] CRON_003: /etc/cron.weekly 权限 700
  - [ ] CRON_004: /etc/cron.monthly 权限 700
  - [ ] CRON_005: /etc/cron.d 权限 700
  - [ ] CRON_006: cron.allow 已配置
  - [ ] CRON_007: at.allow 已配置

- [ ] **文件权限补充** - 补充到 `file-permissions.json`
  - [ ] FILE_021: /etc/motd 权限正确
  - [ ] FILE_022: /etc/issue 权限正确
  - [ ] FILE_023: /etc/issue.net 权限正确

#### 2.5.2.3 日志系统

- [ ] **日志配置增强** - 补充到 `audit-logging.json`
  - [ ] LOG_001: journald 配置 Storage=persistent
  - [ ] LOG_002: journald 配置 Compress=yes
  - [ ] LOG_003: rsyslog 默认文件权限 0640
  - [ ] LOG_004: 日志文件不属于其他用户可读

#### 2.5.2.4 时间与基础服务

- [ ] **时间同步增强** - 补充到 `service-status.json`
  - [ ] TIME_001: chrony 或 ntp 服务运行
  - [ ] TIME_002: 时间同步源已配置
  - [ ] TIME_003: 时间同步服务开机自启

- [ ] **警告横幅** - 新增 `login-banner.json`
  - [ ] BANNER_001: /etc/motd 配置正确
  - [ ] BANNER_002: /etc/issue 配置正确
  - [ ] BANNER_003: /etc/issue.net 配置正确

---

### 2.5.3 规则文件汇总

完成后，预计规则文件结构：

```
plugins/baseline/config/examples/
├── account-security.json      # 账户安全（现有 + 补充）
├── audit-logging.json         # 审计日志（现有 + 补充）
├── cron-security.json         # Cron 安全（新增）
├── file-integrity.json        # 文件完整性（新增）
├── file-permissions.json      # 文件权限（现有 + 补充）
├── login-banner.json          # 登录横幅（新增）
├── mac-security.json          # MAC/SELinux（新增）
├── network-protocols.json     # 网络协议（新增）
├── password-policy.json       # 密码策略（现有 + 补充）
├── secure-boot.json           # 安全启动（新增）
├── service-status.json        # 服务状态（现有 + 补充）
├── ssh-baseline.json          # SSH 安全（现有 + 补充）
└── sysctl-security.json       # 内核参数（现有 + 补充）
```

---

## Phase 3: 扩展功能

### 3.1 中间件基线

- [ ] Nginx 基线检查
- [ ] Redis 基线检查
- [ ] MySQL 基线检查
- [ ] 其他中间件基线

### 3.2 高级特性

- [ ] 策略版本管理
- [ ] 规则依赖关系
- [ ] 自定义检查器（脚本/插件）
- [ ] 插件市场（第三方插件支持）
- [ ] 插件权限控制

### 3.3 集成与对接

- [x] Prometheus 指标导出 - 已完成：Prometheus 查询客户端和混合查询服务
- [ ] 告警对接（Webhook、Lark、邮件等）
- [x] CMDB 集成 - 已完成：CMDB 对接文档（`docs/CMDB_INTEGRATION.md`）
- [ ] 日志系统集成（ELK/Loki）

---

## 当前任务

**当前阶段**：v1.0 生产环境 Bug 修复与功能完善

### 🔥 紧急修复：生产环境 Bug（2026-01-28）

#### Bug 1: 任务主机信息冗余存储 ✅ 已完成
- [x] **问题描述**：任务执行历史中主机信息不完整，主机删除后任务详情丢失信息
- [x] **影响范围**：任务执行页面、任务详情对话框
- [x] **修复方案**：
  - [x] 扩展 `TaskHostStatus` 模型，增加冗余字段：
    - `ip_address`：主机 IP 地址
    - `business_line`：业务线
    - `os_family`：OS 系列
    - `os_version`：OS 版本
    - `runtime_type`：运行时类型（vm/docker/k8s）
  - [x] 更新任务下发代码，保存完整的主机信息
  - [x] 添加 `getHostIPAddress` 辅助函数
- [x] **修复文件**：
  - `internal/server/model/task_host_status.go`：增加冗余字段
  - `internal/server/agentcenter/service/task.go`：更新创建 TaskHostStatus 的代码
- [x] **测试验证**：编译通过
- [x] **注意事项**：需要数据库迁移（新增字段）

#### Bug 2: 基线修复全选仅选择当前页 ✅ 已完成
- [x] **问题描述**：基线修复页面全选只能选择当前页的 100 条，无法选择所有筛选结果
- [x] **影响范围**：基线修复页面
- [x] **修复方案**：
  - [x] 后端：扩展 `CreateFixTask` API，支持传递筛选条件
    - 添加 `use_filters` 参数，当为 true 时使用筛选条件
    - 添加 `business_line` 参数，支持业务线筛选
    - 后端根据筛选条件查询所有符合条件的失败记录
  - [x] 前端：添加"全选所有筛选结果"功能
    - 当用户全选当前页后，显示提示："已选择当前页 X 条，点击选择全部 Y 条"
    - 用户点击"选择全部"后，显示"已选择全部 Y 条筛选结果"
    - 批量修复时，如果选择了全部筛选结果，传递筛选条件而非具体 ID 列表
- [x] **修复文件**：
  - `internal/server/manager/api/fix.go`：扩展 CreateFixTask API
  - `ui/src/api/fix.ts`：更新 API 客户端
  - `ui/src/views/Baseline/Fix.vue`：添加全选功能和提示
- [x] **测试验证**：编译通过
- [x] **用户体验**：
  - 用户可以选择当前页的记录（最多 100 条）
  - 用户可以选择所有筛选结果（不限数量）
  - 清晰的提示告知用户当前选择状态

#### Bug 3: 基线修复筛选顺序不合理 ✅ 已完成
- [x] **问题描述**：筛选器中业务线和主机的顺序不合理，应该先选业务线再选主机
- [x] **影响范围**：基线修复页面筛选器
- [x] **修复方案**：
  - [x] 调整筛选器布局，业务线在左，主机在右
  - [x] 保持现有的联动逻辑（选择业务线后，主机选择器根据业务线过滤）
  - [x] 保持现有的自动清空逻辑（业务线变化时自动清空主机选择）
- [x] **修复文件**：
  - `ui/src/views/Baseline/Fix.vue`：调整筛选器顺序
- [x] **测试验证**：前端代码修改完成
- [x] **用户体验**：
  - 业务线筛选器在最左边（更符合筛选逻辑）
  - 主机选择器在中间（根据业务线动态过滤）
  - 风险等级在右边（保持不变）

#### Bug 4: 批量修复统计数量不准确 ✅ 已完成
- [x] **问题描述**：选择100条基线修复时，总计应该是100条，但显示成功144、失败46，与实际不符
- [x] **影响范围**：批量修复功能的统计数据
- [x] **根本原因**：
  - 前端提取唯一的 host_ids 和 rule_ids 发送给后端
  - 后端使用 `WHERE host_id IN (...) AND rule_id IN (...)` 查询，产生笛卡尔积
  - 例如：选择100条记录（10个主机 × 20个规则的部分组合），但查询会匹配所有10×20=200种组合
- [x] **修复方案**：
  - [x] 修改 API 支持传递 result_ids（精确指定要修复的项）
  - [x] 后端根据 result_ids 查询具体的失败记录
  - [x] 从查询结果中提取 host_ids 和 rule_ids
  - [x] 保留原有的 host_ids + rule_ids 方式（标记为已废弃）
- [x] **修复文件**：
  - `internal/server/manager/api/fix.go`：添加 result_ids 支持，优先使用 result_ids
  - `ui/src/api/fix.ts`：更新 API 类型定义
  - `ui/src/views/Baseline/Fix.vue`：使用 result_ids 而非 host_ids + rule_ids
- [x] **测试验证**：编译通过
- [x] **修复效果**：
  - 选择100条记录，任务总计显示100条
  - 统计数据准确（成功+失败=总计）
  - 不会修复未选择的记录

#### Bug 5: 策略导入报错 Error 1241 ✅ 已完成
- [x] **问题描述**：导入策略时报错 `Error 1241 (21000): Operand should contain 1 column(s)`
- [x] **影响范围**：策略导入功能（`POST /api/v1/policies/import`）
- [x] **根本原因**：
  - `os_family` 字段在数据库中是 JSON 类型（对应 Go 的 `StringArray` 类型）
  - 导入时直接传递 `[]string` 类型给 Gorm 的 Updates 方法
  - MySQL 无法处理 Go 的原生数组类型，导致 SQL 错误
- [x] **修复方案**：
  - 在 `updatePolicy` 函数中，将 `data.OSFamily` 转换为 `model.StringArray` 类型
  - 修改代码：`"os_family": model.StringArray(data.OSFamily)`
- [x] **修复文件**：
  - `internal/server/manager/api/policy_import_export.go`：第 292 行
- [x] **测试验证**：编译通过
- [x] **修复效果**：
  - 策略导入成功，不再报 SQL 错误
  - `os_family` 字段正确存储为 JSON 格式

### 🚀 新需求：批量修复任务化（2026-01-28）

#### 需求 1: 批量修复创建任务并支持进度查看 ✅ 已完成
- [x] **需求描述**：批量修复应该创建一个 task，可以随时查看修复进度和历史详情
- [x] **当前问题**：批量修复关闭后无法查看修复进度和结果
- [x] **实现步骤**：
  - [x] **数据库模型**：FixTask 和 FixResult 模型已存在（`internal/server/model/fix_task.go`）
  - [x] **后端 API**：修复任务 API 已完整实现（`internal/server/manager/api/fix.go`）
    - [x] `POST /api/v1/fix-tasks`：创建批量修复任务（支持筛选条件）
    - [x] `GET /api/v1/fix-tasks/:task_id`：查询修复任务详情
    - [x] `GET /api/v1/fix-tasks`：查询修复任务列表
    - [x] `GET /api/v1/fix-tasks/:task_id/results`：查询修复结果详情（分页）
    - [x] `DELETE /api/v1/fix-tasks/:task_id`：删除修复任务
  - [x] **前端 UI 实现**：
    - [x] 修复历史页面（`ui/src/views/Baseline/FixHistory.vue`）
    - [x] 任务列表展示（状态、进度、结果统计）
    - [x] 任务详情对话框（完整任务信息和修复结果列表）
    - [x] 实时刷新修复进度（任务执行中时自动刷新）
    - [x] 删除任务功能（已完成或失败的任务）
    - [x] 路由配置（`ui/src/router/index.ts`）
    - [x] 导航菜单项（`ui/src/layouts/BasicLayout.vue`）
- [x] **修复文件**：
  - `ui/src/views/Baseline/FixHistory.vue`：修复历史页面
  - `ui/src/router/index.ts`：添加路由配置
  - `ui/src/layouts/BasicLayout.vue`：添加导航菜单项
- [x] **功能特性**：
  - 支持按状态筛选任务（待执行、执行中、已完成、失败）
  - 显示任务进度条和成功/失败统计
  - 查看详细修复结果（主机、规则、状态、输出、错误信息）
  - 自动刷新执行中的任务（5秒间隔）
  - 删除已完成的任务记录

### ✅ 已完成：生产环境部署文档（2025-01-03）

- [x] 创建生产环境部署方案文档 (`docs/deployment/production-deployment.md`)
- [x] 更新 README.md 文档索引
- [x] 基线规则已扩展到 200+ 条

### 🔄 进行中：v1.0 生产部署

**部署步骤**：
1. [x] 准备部署文档
2. [ ] 部署 MySQL 数据库
3. [ ] 部署 Server（AgentCenter + Manager）
4. [ ] 部署前端 UI
5. [ ] 构建并部署插件
6. [ ] 部署 Agent 到目标主机
7. [ ] 验证完整流程

**待完成功能**（可在部署后迭代）：
- [ ] 告警对接（Webhook、Lark、邮件等）
- [ ] 架构区分功能（VM/Docker/K8s）

---

### ⏳ 待办：Phase 2.5 CIS 基线规则完善

**优先级排序**：
1. **P0-0 架构区分功能**
   - [ ] Agent 端识别运行环境（VM/Docker/K8s）
   - [ ] 心跳数据和 Host 模型增加 `runtime_type` 字段
   - [ ] 策略/规则增加 `runtime_types` 适用范围
   - [ ] 任务下发时按运行环境过滤规则

2. **P0 严重/高危基线**（架构区分完成后）
   - [ ] MAC/SELinux 安全
   - [ ] 安全启动
   - [ ] 审计规则增强
   - [ ] 系统完整性（AIDE）
   - [ ] SSH 加密算法强化
   - [ ] 内核/网络协议禁用
   - [ ] 服务暴露面

3. **P1 中危基线**
   - [ ] PAM 认证增强
   - [ ] Cron 安全
   - [ ] 日志配置
   - [ ] 时间同步
   - [ ] 登录横幅

**背景**：
- 当前基线规则 200+ 条，已覆盖主要安全基线
- 后续按 CIS Rocky Linux 9 Benchmark 继续完善
- Docker 容器不应检测 SSH 等 VM 专属规则，需先实现架构区分

---

**历史阶段**：Phase 1 MVP 基本完成 ✅，Phase 2 功能完善基本完成 ✅

**已完成**：
1. ✅ Phase 0：Elkeid 研究、架构设计、API 设计、数据库设计
2. ✅ Phase 1.0：基础设施（插件 SDK、Protobuf 定义、代码生成）
3. ✅ Phase 1.1 基础框架：Agent 主程序、配置管理、日志系统、Agent ID 管理
4. ✅ Phase 1.1 连接管理：mTLS、gRPC 连接、服务发现（简化实现）
5. ✅ Phase 1.1 传输模块：gRPC 双向流、数据发送接收、配置更新处理
6. ✅ Phase 1.1 心跳模块：定时心跳、状态采集、主机信息采集（已完成：OS、内核、IP、主机名）、插件状态采集
7. ✅ Phase 1.1 插件管理模块：插件生命周期管理、Pipe 通信、配置同步、插件下载和 SHA256 验证、任务队列实现
8. ✅ Phase 1.1 Baseline Plugin：插件入口、策略加载解析、OS 匹配、规则执行框架、检查器（file_kv、file_permission、file_line_match、command_exec、sysctl、service_status）、结果上报
9. ✅ Phase 1.1 测试：Baseline Plugin 单元测试（所有检查器测试通过，代码质量良好）
10. ✅ Phase 1.2.1：数据库模型设计（Host、Policy、Rule、ScanResult、ScanTask）
11. ✅ Phase 1.2.2 AgentCenter 基础框架：主程序、配置加载、日志初始化、数据库连接、gRPC Server、mTLS 配置
12. ✅ Phase 1.2.2 AgentCenter Transfer 服务：双向流、连接管理、心跳数据处理、检测结果处理
13. ✅ Phase 1.2.3 **Baseline 任务执行流程完善** - 2025-12-12 完成
    - ✅ Baseline Plugin 添加 task_id 到检测结果
    - ✅ Baseline Plugin 发送任务完成信号 (DataType=8001)
    - ✅ Server 端处理任务完成信号并更新任务状态
    - ✅ Server 端检测结果去重（UPSERT 机制）
    - ✅ 任务超时控制（默认 1 小时）
    - ✅ 任务状态自动流转（pending → running → completed/failed）
14. ✅ Phase 2.3 **任务执行页面 UI 改进** - 2025-12-12 完成
    - ✅ 将「扫描任务」改名为「任务执行」（路由、菜单、页面标题）
    - ✅ 丰富任务过滤条件（任务类型、状态、任务名称筛选）
    - ✅ 实现「执行」按钮功能（确认弹窗、加载状态、成功/失败提示）
    - ✅ 实现任务详情查看功能（任务信息、目标主机、时间信息）
    - ✅ 实现任务进度显示（执行中显示进度条）
    - ✅ 实现执行结果统计（通过/失败/错误数量）
    - ✅ 自动刷新功能（任务执行中时自动刷新列表）

**下一步**：
1. ✅ Phase 1.1 继续：完善 Baseline Plugin（file_line_match、sysctl、service_status 检查器）- 已完成
2. ✅ Phase 1.1 继续：完善插件管理（插件下载和签名验证、任务队列）- 已完成
3. ✅ Phase 1.1 继续：插件状态采集（心跳模块）- 已完成
4. ✅ Phase 1.1 继续：编写单元测试（Baseline Plugin 检查器测试）- 已完成，所有测试通过
5. ✅ Phase 1.1 继续：实现更多检查器（file_owner、package_installed）- 已完成
6. 🔄 Phase 1.1 继续：Baseline Plugin 示例规则（SSH、密码策略等）- 可选，后续扩展
7. ✅ **Phase 1.2 AgentCenter 基础框架** - 已完成
8. ✅ **Phase 1.2 继续：完善 AgentCenter（任务下发逻辑）** - 已完成
9. ✅ **Phase 1.2 开始：开发 Manager HTTP API Server** - 基础框架已完成
10. ✅ **Phase 1.2 继续：完善 Manager API（policies、tasks、results API）** - 已完成
11. ✅ **Phase 1.2 继续：实现基线得分计算和缓存** - 已完成
12. ✅ **Phase 1.2 继续：实现任务状态自动更新机制** - 已完成
13. ✅ **Phase 1.2 继续：优化错误处理和重试逻辑** - 已完成
14. ✅ **Phase 1.2 继续：编写集成测试和端到端测试** - 已完成（使用 MySQL）
15. ✅ **Phase 1.3 UI 开发：创建前端项目基础结构** - 已完成（Vue3 + TypeScript + Pinia + Ant Design Vue）
16. ✅ **Phase 1.3 UI 开发：实现 API 客户端封装** - 已完成
17. ✅ **Phase 1.3 UI 开发：实现主机列表和详情页面** - 已完成
18. ✅ **Phase 1.3 UI 开发：实现策略管理页面** - 已完成
19. ✅ **Phase 1.3 UI 开发：实现扫描任务管理页面** - 已完成
20. ✅ **Phase 1.3 UI 开发：实现策略详情页面** - 已完成：检查概览、检查项视角、影响的主机列表、策略统计 API（后端已实现 `GET /api/v1/policies/:policy_id/statistics`）

**优先级**：
- **P0（必须）**：Agent 基础框架、插件 SDK、Baseline Plugin、AgentCenter、数据库模型
- **P1（重要）**：连接管理、传输模块、Manager API、检测结果存储
- **P2（可选）**：Collector Plugin、ServiceDiscovery、前端 UI、资产数据存储

---

## 已知问题（待修复）

### API 接口问题

#### 1. POST /api/v1/policies 创建策略接口失败（HTTP 400）✅ 已修复

**问题描述**：
- 在 API 测试中发现 `POST /api/v1/policies` 接口返回 HTTP 400 错误
- 测试时间：2025-12-11
- 测试场景：创建新策略时失败
- 根本原因：`RuleData` 结构体中的 `CheckConfig` 字段有 `binding:"required"` 标记，导致缺少该字段时请求体验证失败

**修复方案**：
- ✅ [x] 移除 `CheckConfig` 的 `binding:"required"` 标记（改为可选字段）
- ✅ [x] 在 `CreatePolicy` 方法中添加手动的字段验证，确保规则的 `RuleID` 和 `Title` 必填
- ✅ [x] 添加了测试用例 `TestCreatePolicyAPI_ValidRequest` 和 `TestCreatePolicyAPI_NoCheckConfig`
- ✅ [x] 修复后编译通过，测试验证成功

**修复提交**：
- 文件：`internal/server/manager/api/policies.go`（第 140 行移除 binding 标记，第 156-184 行添加验证逻辑）
- 测试：`internal/server/manager/api/integration_test.go`（添加新测试函数）
- 修复时间：2025-12-11 18:45

---

#### 2. POST /api/v1/tasks/:task_id/run 运行任务接口失败（HTTP 400）✅ 已修复

**问题描述**：
- 在 API 测试中发现 `POST /api/v1/tasks/:task_id/run` 接口返回 HTTP 400 错误
- 测试时间：2025-12-11
- 测试场景：尝试运行一个已经是 `running` 状态的任务时失败
- 根本原因：当任务已在运行中时，API 返回 HTTP 400，这在 RESTful 语义上不正确，应该返回 HTTP 409 Conflict

**修复方案**：
- ✅ [x] 将任务已在运行中时的响应状态码从 `400` 改为 `409 Conflict`（第 264 行）
- ✅ [x] 改进错误消息为"任务正在执行中，无法重复执行"（第 266 行）
- ✅ [x] 添加了测试用例 `TestRunTaskAPI_Running` 和 `TestRunTaskAPI_Success`
- ✅ [x] 修复后编译通过，测试验证成功

**修复提交**：
- 文件：`internal/server/manager/api/tasks.go`（第 264-267 行修改）
- 测试：`internal/server/manager/api/integration_test.go`（添加新测试函数）
- 修复时间：2025-12-11 18:45

---

## Server 端开发详细任务分解

> **当前重点**：Phase 1.2 Server 开发 + API 修复

### 任务 1：数据库模型设计（优先级：P0）✅

**目标**：定义并实现所有数据库表结构，支持主机管理、策略管理、检测结果存储。

**步骤**：
1. ✅ 创建 `internal/server/model/` 目录
2. ✅ 定义 Gorm 模型：
   - ✅ `Host` 模型（hosts 表）
   - ✅ `Policy` 模型（policies 表）
   - ✅ `Rule` 模型（rules 表）
   - ✅ `ScanResult` 模型（scan_results 表）
   - ✅ `ScanTask` 模型（scan_tasks 表）
3. ✅ 编写数据库迁移脚本（`internal/server/migration/`）
4. ✅ 创建初始化数据脚本（默认策略、示例规则）

**完成时间**：已完成

---

### 任务 2：AgentCenter 基础框架（优先级：P0）✅

**目标**：实现 AgentCenter 主程序，支持 gRPC Server 启动、配置加载、数据库连接。

**步骤**：
1. ✅ 创建 `cmd/server/agentcenter/main.go`
2. ✅ 实现配置加载（Viper + YAML）
3. ✅ 实现日志初始化（Zap，JSON 输出）
4. ✅ 实现数据库连接（Gorm）
5. ✅ 实现 gRPC Server 启动
6. ✅ 实现 mTLS 配置（证书加载）
7. ✅ 创建 `configs/server.yaml.example` 配置文件

**完成时间**：已完成

---

### 任务 3：AgentCenter Transfer 服务（优先级：P0）🔄

**目标**：实现 `Transfer` 双向流服务，接收 Agent 数据并下发任务。

**步骤**：
1. ✅ 实现 `Transfer` 服务接口（`internal/server/agentcenter/transfer/service.go`）
2. ✅ 实现连接状态管理（Map[agent_id]*Connection）
3. ✅ 实现数据接收逻辑：
   - ✅ 解析 `PackagedData` 和 `EncodedRecord`
   - ✅ 根据 `data_type` 路由到处理器
   - ✅ 心跳数据处理（更新 hosts 表）
   - ✅ 检测结果处理（插入 scan_results 表）
4. ✅ 实现任务下发逻辑：
   - [x] 查询待执行任务（`internal/server/agentcenter/service/task.go`）
   - [x] 封装为 `Command` 并发送
   - [x] 任务调度器（每 30 秒检查一次待执行任务）
5. ✅ 实现连接断开处理

**完成情况**：数据接收和任务下发功能已完成

**额外完成**：
- ✅ 实现策略和规则管理服务（`internal/server/agentcenter/service/policy.go`）
  - ✅ 策略 CRUD 操作
  - ✅ 规则 CRUD 操作
  - ✅ 根据主机信息查询适用策略

---

### 任务 4：Manager 基础框架（优先级：P1）✅

**目标**：实现 Manager HTTP API Server，支持基础 API 接口。

**步骤**：
1. ✅ 创建 `cmd/server/manager/main.go`
2. ✅ 实现配置加载和日志初始化
3. ✅ 实现 HTTP Server（Gin）
4. ✅ 实现中间件（CORS、日志、Recovery）
5. ✅ 实现基础 API 路由（hosts API）

**完成时间**：已完成

---

### 任务 5：Manager API 实现（优先级：P1）✅

**目标**：实现所有 HTTP API 接口，支持主机管理、策略管理、任务管理、结果查询。

**步骤**：
1. ✅ 实现主机管理 API（`internal/server/manager/api/hosts.go`）
   - ✅ `GET /api/v1/hosts`
   - ✅ `GET /api/v1/hosts/{host_id}`
2. ✅ 实现策略管理 API（`internal/server/manager/api/policies.go`）
   - ✅ `GET /api/v1/policies`
   - ✅ `POST /api/v1/policies`
   - ✅ `PUT /api/v1/policies/{policy_id}`
   - ✅ `DELETE /api/v1/policies/{policy_id}`
3. ✅ 实现任务管理 API（`internal/server/manager/api/tasks.go`）
   - ✅ `POST /api/v1/tasks`
   - ✅ `GET /api/v1/tasks`
   - ✅ `POST /api/v1/tasks/{task_id}/run`
4. ✅ 实现结果查询 API（`internal/server/manager/api/results.go`）
   - ✅ `GET /api/v1/results`
   - ✅ `GET /api/v1/results/host/{host_id}/score`
   - ✅ `GET /api/v1/results/host/{host_id}/summary`
5. ✅ 实现业务逻辑层（`internal/server/manager/biz/score.go`）- 基线得分计算和缓存

**完成时间**：已完成

**额外完成**：
- ✅ 实现基线得分计算和缓存机制
- ✅ 实现任务状态自动更新机制（`internal/server/agentcenter/service/task_status.go`）

---

### 代码结构重构（优先级：P2）✅

**目标**：优化代码结构，遵循最佳实践，使 main.go 更简洁，路由和中间件独立维护。

**步骤**：
1. ✅ 重构 Manager main.go
   - ✅ 创建 `internal/server/manager/router` 包，提取所有路由设置逻辑
   - ✅ 创建 `internal/server/manager/middleware` 包，提取中间件
   - ✅ 创建 `internal/server/manager/setup` 包，提取所有初始化逻辑
   - ✅ 简化 `cmd/server/manager/main.go`（从 171 行减少到约 60 行）
2. ✅ 重构 AgentCenter main.go
   - ✅ 创建 `internal/server/agentcenter/server` 包，提取 gRPC Server 创建逻辑
   - ✅ 创建 `internal/server/agentcenter/scheduler` 包，提取任务调度器
   - ✅ 创建 `internal/server/agentcenter/setup` 包，提取所有初始化逻辑
   - ✅ 简化 `cmd/server/agentcenter/main.go`（从 129 行减少到约 50 行）
3. ✅ 验证编译隔离：确保 agent、agentcenter、manager 独立编译，不会相互包含代码

**完成时间**：2024-12-11（首次重构），2024-12-XX（初始化逻辑提取）

**重构结果**：
- Manager: main.go 约 60 行，setup/init.go 约 144 行，router.go 219 行，middleware.go 48 行
- AgentCenter: main.go 约 50 行，setup/init.go 约 139 行，server.go 63 行，scheduler.go 31 行
- 代码结构更清晰，符合 Go 最佳实践
- 初始化逻辑完全独立，main.go 只负责启动流程和信号处理
- ✅ 实现错误处理和重试逻辑（`internal/server/agentcenter/service/retry.go`）
- ✅ 编写 Manager API 集成测试（`internal/server/manager/api/integration_test.go`）

---

### 任务 6：证书生成脚本（优先级：P0）✅

**目标**：提供 mTLS 证书生成脚本，支持 Server 和 Agent 证书生成。

**步骤**：
1. ✅ 创建 `scripts/generate-certs.sh`
2. ✅ 实现 CA 证书生成
3. ✅ 实现 Server 证书生成
4. ✅ 实现 Agent 证书生成
5. ✅ 编写证书使用说明（脚本内包含）

**完成时间**：已完成

---

### 任务 7：集成测试（优先级：P1）✅

**目标**：实现 Agent + Server + Plugin 集成测试，验证完整流程。

**步骤**：
1. ✅ 编写集成测试框架（`tests/e2e/e2e_test.go`）
2. ✅ 测试 Agent 连接 Server
3. ✅ 测试心跳上报
4. ✅ 测试任务下发和执行
5. ✅ 测试检测结果上报和存储
6. ✅ 测试基线得分计算
7. ✅ 使用 MySQL 数据库（符合项目标准）
8. ✅ 添加测试运行说明文档（`tests/e2e/README.md`）

**完成时间**：已完成

**说明**：
- E2E 测试使用 MySQL 数据库（通过环境变量配置）
- 测试覆盖了完整的 Agent + Server + Plugin 流程
- 包含测试运行说明文档

---

**总计预计时间**：10-15 天

---

### 任务 8：UI 改进和后端 API 实现（优先级：P1）✅

**目标**：基于 Elkeid 参考实现完整的 UI 界面和后端 API 支持。

**步骤**：
1. ✅ 实现登录界面和安全认证
   - ✅ 前端登录页面（`ui/src/views/Login.vue`）
   - ✅ 认证状态管理（Pinia store）
   - ✅ 路由守卫
   - ✅ 后端认证 API（`internal/server/manager/api/auth.go`）
   - ✅ JWT Token 认证机制
2. ✅ 实现 Dashboard 页面
   - ✅ Dashboard 前端页面（参考 console0.png）
   - ✅ Dashboard 统计 API（`internal/server/manager/api/dashboard.go`）
3. ✅ 改进 Layout 布局
   - ✅ 左侧导航栏（参考 Elkeid 样式）
   - ✅ 顶部栏（用户信息和退出登录）
4. ✅ 改进主机详情页
   - ✅ 多标签页实现（参考 console3.png）
   - ✅ 主机概览组件（基本信息、安全告警、漏洞风险、基线风险、资产指纹）
   - ✅ 基线风险组件
5. ✅ 改进基线检查详情页
   - ✅ 检查概览（通过率、主机数、检查项数）
   - ✅ 检查项视角（检查项列表和详情）
   - ✅ 影响的主机列表（显示受影响的主机及其检查结果）
   - ✅ 策略统计 API（`internal/server/manager/api/policies.go` - `GetPolicyStatistics`）
6. ✅ 更新文档
   - ✅ UI 改进需求文档（`docs/features/ui-improvements.md`）
   - ✅ Server API 文档更新（添加认证、Dashboard、策略统计 API）

**完成时间**：已完成

**说明**：
- 所有 UI 改进参考 Elkeid 控制台设计
- 实现了完整的认证系统（JWT Token）
- Dashboard 和主机详情页已实现主要功能
- 基线检查详情页（策略详情页）已实现检查项视角和影响的主机列表
- 后端 API 已实现并集成到路由中，包括策略统计 API（`GET /api/v1/policies/:policy_id/statistics`）

**待实现功能**：
- 用户管理（用户表、角色权限）
- 告警系统（告警数据模型和统计）
- 漏洞管理（漏洞数据模型和统计）
- 资产指纹详情（资产数据采集和展示）
- 图表展示（趋势图）
- 主机视角（基线检查详情页的另一个视角）
- 立即检查功能（策略详情页的"立即检查"按钮）
- 批量重新检查功能
- 白名单功能

---

### 任务 9：主机管理和组件版本显示修复（优先级：P1）✅

**目标**：修复主机删除功能和组件列表版本显示问题。

**步骤**：
1. ✅ 实现主机删除功能
   - ✅ 后端 API：`DELETE /api/v1/hosts/:host_id`（级联删除所有关联数据）
   - ✅ 前端 UI：添加删除按钮和确认对话框
   - ✅ 删除范围：扫描结果、告警、监控数据、插件信息、资产数据等
2. ✅ 修复组件列表版本显示问题
   - ✅ 后端 API：`ListComponents` 添加当前版本和状态统计
   - ✅ Agent 版本：从 `hosts` 表统计已安装版本
   - ✅ Plugin 版本：从 `host_plugins` 表统计已安装版本
   - ✅ 前端 UI：添加"当前版本"、"状态"、"启动时间"、"更新时间"列
3. ✅ 修复 AgentCenter 空版本更新问题
   - ✅ 修改 `handleHeartbeat`，只有当 `data.Version` 非空时才更新 `agent_version`
   - ✅ 避免空版本覆盖已有版本数据
4. ✅ 添加调试日志
   - ✅ Manager API 添加组件统计查询的调试日志
   - ✅ 便于排查版本显示问题

**完成时间**：2025-12-19

**说明**：
- 主机删除功能已完整实现，支持级联删除所有关联数据
- 组件列表现在可以正确显示当前版本和安装状态
- 修复了 AgentCenter 将空版本写入数据库的问题
- 添加了诊断文档（`docs/COMPONENT_STATUS_DIAGNOSIS.md`）

---

## 参考文档

### 设计文档
- [Elkeid 架构分析](./elkeid-notes/elkeid-architecture-analysis.md)
- [Elkeid 研究总结](./elkeid-notes/summary.md)
- [策略模型设计](./design/baseline-policy-model.md)
- [Agent 架构设计](./design/agent-architecture.md)
- [Agent 配置设计](./design/agent-config-design.md)
- [Server API 设计](./design/server-api.md)
- [UI 改进需求文档](./features/ui-improvements.md)
- [Agent 部署指南](../deployment/agent-deployment.md)

### 参考代码
- Elkeid Agent：`Elkeid/agent/`
- Elkeid Baseline Plugin：`Elkeid/plugins/baseline/`
- Elkeid Collector Plugin：`Elkeid/plugins/collector/`
- Elkeid Plugin SDK：`Elkeid/plugins/lib/`
- Elkeid AgentCenter：`Elkeid/server/agent_center/`
- Elkeid Manager：`Elkeid/server/manager/`
- Elkeid ServiceDiscovery：`Elkeid/server/service_discovery/`

### 项目文档
- [项目 README](../README.md)
- [Cursor 规则](../.cursor/rules/common.mdc)

