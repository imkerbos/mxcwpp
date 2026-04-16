# Server API 设计

本文档基于当前 `internal/server/manager/router/router.go` 和 `docs/api-reference.md` 整理。

## 入口划分

### 公共入口

- `GET /health`
- `GET /metrics`
- `GET /agent/install.sh`
- `GET /agent/uninstall.sh`
- `GET /api/v1/plugins/download/:name`
- `GET /api/v1/agent/download/:pkg_type/:arch`
- `GET /api/v1/agent/update-check`

### 内部入口

- `POST /api/v1/internal/ac/register`
- `POST /api/v1/internal/ac/heartbeat`
- `DELETE /api/v1/internal/ac/deregister`

### 业务入口

统一前缀：`/api/v1`

## 认证模型

- 登录接口获取 JWT
- `auth/me`、`system-config/site` 等少数接口可在较早阶段访问
- 大多数业务接口必须通过 `AuthMiddleware`
- 审计日志中间件挂在认证后的业务路由组上

## 主要资源分组

- `auth`
- `hosts`
- `policy-groups`
- `policies`
- `rules`
- `tasks`
- `results`
- `fix` / `fix-tasks`
- `dashboard`
- `users`
- `assets`
- `reports`
- `business-lines`
- `system-config`
- `notifications`
- `alerts`
- `audit-log`
- `components`
- `inspection`
- `fim`
- `kube`
- `monitor`
- `backups`
- `vulnerabilities`

## 设计特点

- HTTP API 负责配置、查询、管理和控制面操作
- Agent 数据面走 gRPC 双向流
- 异步写入链路可经 Kafka / Consumer / ClickHouse 扩展
- 文件下载、安装脚本和更新检查保留无认证入口，便于 Agent 安装和升级

## 建议阅读

- [API 参考](../api-reference.md)
- `internal/server/manager/router/router.go`
