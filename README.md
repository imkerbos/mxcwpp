# MxSec Platform

[![Go Version](https://img.shields.io/github/go-mod/go-version/imkerbos/mxsec-platform)](https://github.com/imkerbos/mxsec-platform)
[![License](https://img.shields.io/github/license/imkerbos/mxsec-platform)](LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/imkerbos/mxsec-platform?style=social)](https://github.com/imkerbos/mxsec-platform/stargazers)
[![GitHub Issues](https://img.shields.io/github/issues/imkerbos/mxsec-platform)](https://github.com/imkerbos/mxsec-platform/issues)
[![Last Commit](https://img.shields.io/github/last-commit/imkerbos/mxsec-platform)](https://github.com/imkerbos/mxsec-platform/commits/main)
[![Go Report Card](https://goreportcard.com/badge/github.com/imkerbos/mxsec-platform)](https://goreportcard.com/report/github.com/imkerbos/mxsec-platform)

企业级主机与容器安全管理平台。覆盖安全基线、资产管理、漏洞扫描、病毒查杀、运行时检测与合规审计，面向安全运营团队提供统一管控视图。

## 功能概览

| 模块 | 说明 |
|------|------|
| 安全基线 | 9 种检查器、212 条规则，覆盖 CIS Benchmark 核心项，支持单机/批量自动修复 |
| 资产中心 | 11 类资产采集（进程、端口、用户、软件包、容器等），关系计算与资产导出 |
| 漏洞管理 | 软件包 PURL 采集 + OSV.dev 匹配 + CVSS v3.1 评分 + SBOM 导出 |
| 病毒查杀 | ClamAV + YARA-X 双引擎扫描，任务管理 + 隔离箱处置 |
| 文件完整性 | 基于 AIDE 的 FIM 检查，策略、事件、任务全链路闭环 |
| 运行时检测 | Tetragon/eBPF 事件采集 + CEL 规则引擎 + MITRE ATT&CK 映射 |
| 容器安全 | K8s 集群管理、容器 CIS 基线（80 条规则）、Audit Webhook 接入 |
| 告警中心 | 告警聚合、白名单、自动响应（kill/隔离）、溯源时间线 |
| 威胁情报 | MISP IOC 导入 + Redis 缓存 + CEL 实时碰撞 |

## 架构概览

```
浏览器 ─→ Nginx ─→ Manager ×N ─→ MySQL / Redis / ClickHouse / Prometheus
Agent ─→ gRPC(mTLS) ─→ AgentCenter ×N ─→ Kafka ─→ Consumer ×N ─→ 存储层
```

控制面（Manager / AgentCenter / Consumer）无状态，支持多实例水平扩展。通过 Kafka 异步解耦数据写入，Redis 实现服务发现与分布式锁，ClickHouse 承载时序分析与事件归档。

详细架构参见 [架构设计文档](docs/architecture.md)。

## 技术栈

| 层面 | 技术 |
|------|------|
| 后端 | Go 1.25+（Gin / gRPC / Gorm / Zap） |
| 前端 | Vue 3 + TypeScript + Pinia + Ant Design Vue 4 |
| 存储 | MySQL 8.0+ / Redis 7 / ClickHouse 24 |
| 消息 | Kafka（KRaft 模式，7 Topic + DLQ） |
| 监控 | Prometheus（主机性能指标唯一数据源） |
| 通信 | gRPC 双向流 + mTLS + Protobuf |
| 部署 | Docker Compose / Systemd + Nginx |

## 支持平台

**主机 OS**: Rocky Linux 9/10, Oracle Linux 7/8/9, CentOS 7/8/9, Debian 10/11/12, Ubuntu 20.04/22.04

**运行时**: 物理机 / 虚拟机、Docker 容器宿主机、Kubernetes 节点与集群

## 快速开始

```bash
git clone https://github.com/imkerbos/mxsec-platform.git
cd mxsec-platform/deploy

cp .env.example .env
vim .env  # 修改 SERVER_IP / JWT_SECRET / 数据库密码

# 启动控制面（HA 模式）
docker compose --env-file .env up -d \
  --scale manager=2 --scale agentcenter=2 --scale consumer=2
```

访问 `http://<SERVER_IP>` 登录管理界面，默认账户 `admin / admin123`。

详细部署方案参见 [安装部署文档](docs/deployment.md)。

## 构建命令

```bash
make build-server                                        # 构建服务端
make build-consumer                                      # 构建 Consumer
make package-agent-all VERSION=1.0.0 SERVER_HOST=IP:6751 # 打包 Agent（RPM/DEB）
make package-plugins-all VERSION=1.0.0                   # 打包插件
make proto                                               # 生成 Protobuf 代码
make test                                                # 运行测试
make lint                                                # 代码检查
```

## 项目结构

```
mxsec-platform/
├── cmd/                    # 入口（agent / manager / agentcenter / consumer）
├── internal/
│   ├── server/             # 服务端（manager / agentcenter / consumer / common）
│   └── agent/              # Agent（connection / transport / plugin / heartbeat）
├── plugins/                # 插件（baseline / collector / fim / scanner / sensor / remediation）
├── api/proto/              # Protobuf 定义
├── ui/                     # 前端工程（Vue 3 + TypeScript）
├── configs/                # 配置文件（server.yaml / agent.yaml / 规则文件）
├── deploy/                 # 部署配置（Docker Compose / Nginx / systemd）
├── scripts/                # 构建与部署脚本
└── docs/                   # 文档
```

## 文档

- [架构设计](docs/architecture.md) - 系统拓扑、组件职责、数据链路、高可用设计
- [安装部署](docs/deployment.md) - 环境准备、单机/集群部署、Agent 安装、升级与备份
- [配置说明](docs/configuration.md) - 服务端配置、Agent 配置、环境变量
- [API 文档](docs/api-reference.md) - REST API 端点、请求/响应格式、认证
- [常见问题](docs/faq.md) - 部署与运行中的常见问题及排查方法
- [发展路线](docs/roadmap.md) - 已完成能力、近期规划、中远期方向
- [开源治理](docs/governance.md) - 项目治理模型、决策流程、安全策略
- [社区规范](docs/contributing.md) - 贡献指南、开发环境、代码规范、提交流程

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=imkerbos/mxsec-platform&type=Date)](https://star-history.com/#imkerbos/mxsec-platform&Date)

## Contributors

见 [CONTRIBUTORS.md](CONTRIBUTORS.md)。

## License

[Apache License 2.0](LICENSE)
