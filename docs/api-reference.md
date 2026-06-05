# API 参考 v2

> **文档版本**：v2.0（六微服务架构）
> **最后更新**：2026-06-06
> **平台定位**：mxsec 是**工业级开源 CWPP**，专精 **Linux 主机 + Kubernetes 容器**，面向 ToB 政企/金融/互联网客户。
> **运行模式**：默认 `observe`（监听）模式，磨合达标后按 6 门槛 + 4 级灰度切到 `protect`（防护）模式，详见 [`operating-modes.md`](operating-modes.md)。
> **多租户**：from-day-1，全平台 `tenant_id` 贯穿，详见 [`multi-tenant.md`](multi-tenant.md)。

---

## 0. 文档说明

### 0.1 本文档覆盖范围

本文档是 mxsec 平台对外暴露 API 的**唯一权威参考**。覆盖六微服务架构下的所有 HTTP / gRPC 接口：

| 微服务 | 对外接口 | 说明 |
|--------|----------|------|
| **Manager** | HTTP REST（业务面） | 浏览器 / CI / SOAR 调用 |
| **AgentCenter** | HTTP（内部管理） + gRPC（Agent 接入） | Manager → AC 任务下发；Agent → AC 数据上行 |
| **Consumer** | 仅 `/metrics` + `/healthz` | 纯内部消费者，无业务 API |
| **Engine** | HTTP（内部管理） + gRPC | Manager / UI 查询；Engine 内部 RPC |
| **VulnSync** | HTTP（内部管理） | 状态查询 + 手动触发同步 |
| **LLMProxy** | gRPC（生产）+ HTTP（管理） | Engine / Manager 调 LLM；运维查 usage |

> Consumer 不对外暴露业务 API；任何"查告警"、"查事件"等读取请求都走 Manager，由 Manager 直查 MySQL / ClickHouse。

### 0.2 与 v1 的关系

- **v1.x** 接口路径前缀 `/api/v1/`，仍可用但**全部标记 `Deprecated`**，预计 **2027-01-01 Sunset**。
- **v2.0** 接口路径前缀 `/api/v2/`，新增多租户 header、模式字段、RBAC scope 等。
- 迁移指南见 §10。

### 0.3 路径前缀规范

```
# 租户作用域（普通用户 token）
/api/v2/{resource}
/api/v2/{resource}/{id}/{sub-resource}

# 平台作用域（仅 SystemAdmin）
/api/v2/admin/{resource}

# MSSP 父租户聚合（仅 mssp_parent 类型租户）
/api/v2/mssp/{resource}

# 微服务内部互调（mTLS + 内部 Bearer Token，禁止外网暴露）
/internal/v2/{resource}
```

---

## 1. 公共约定

### 1.1 鉴权三段

每个外部请求经过 **3 层校验**，缺一即 401/403：

```
[1] JWT 校验
    └── HMAC-SHA256（默认）/ RS256（KA 客户）签名验证 + 未过期
        ↓
[2] Tenant 校验
    ├── 从 JWT claims.tenant_id 或 Header X-Tenant-ID 提取
    ├── 查 tenants 表，status 必须 = active
    └── 注入 context.WithValue(ctx, "tenant_id", tid)
        ↓
[3] RBAC 校验（Casbin）
    └── 校验 user 在该 tenant 内对 (resource, action) 是否有权限
        ↓
执行业务（所有 DB 查询自动 Apply TenantScope）
```

**详细的多租户隔离机制、跨租户 token、MSSP 父子规则**见 [`multi-tenant.md`](multi-tenant.md) §4。

### 1.2 请求头规范

| Header | 必选 | 示例 | 说明 |
|--------|------|------|------|
| `Authorization` | 是 | `Bearer eyJhbG...` | JWT Bearer token；公开接口豁免 |
| `X-Tenant-ID` | 否 | `t-bank-a` | 显式指定租户（JWT 已带，header 可覆写，仅 SystemAdmin / mssp_parent 允许覆写） |
| `X-Request-ID` | 否 | `req-2026...` | 客户端可传，未传则服务端生成，全链路追踪 |
| `Content-Type` | POST/PUT 必填 | `application/json` | 上传文件用 `multipart/form-data` |
| `Accept-Language` | 否 | `zh-CN` / `en-US` | 默认 `zh-CN` |
| `X-Idempotency-Key` | 否 | `idem-abc123` | 写操作幂等，TTL 24h |

### 1.3 统一响应格式

成功响应：

```json
{
  "code": 0,
  "message": "success",
  "data": { ... },
  "request_id": "req-2026060601-abc"
}
```

错误响应：

```json
{
  "code": 40012,
  "message": "tenant_id missing in JWT claims",
  "data": null,
  "request_id": "req-2026060601-abc",
  "details": {
    "field": "tenant_id",
    "hint": "请重新登录或联系管理员检查租户绑定"
  }
}
```

### 1.4 分页参数

| 参数 | 类型 | 默认 | 上限 | 说明 |
|------|------|------|------|------|
| `page` | int | 1 | - | 页码（从 1 开始） |
| `page_size` | int | 20 | 200 | 每页条数 |
| `sort` | string | - | - | 排序字段，前缀 `-` 表降序，如 `-created_at` |
| `q` | string | - | - | 关键字模糊搜索（按资源支持的字段过滤） |

分页响应：

```json
{
  "code": 0,
  "data": {
    "items": [ ... ],
    "page": 1,
    "page_size": 20,
    "total": 1234,
    "total_pages": 62
  }
}
```

### 1.5 错误码总表

| HTTP | 业务码 | 含义 |
|------|--------|------|
| 200 | 0 | 成功 |
| 400 | 40001 | 请求参数缺失 |
| 400 | 40002 | 请求参数格式错误 |
| 400 | 40003 | 请求体过大（> 10MB） |
| 401 | 40101 | Token 缺失 |
| 401 | 40102 | Token 签名错误 |
| 401 | 40103 | Token 已过期 |
| 401 | 40104 | Token 已撤销（登出后再用） |
| 403 | 40301 | 租户已暂停 / 删除 |
| 403 | 40302 | RBAC 权限不足 |
| 403 | 40303 | 跨租户访问 |
| 403 | 40304 | 操作受当前运行模式限制（如 observe 模式下手动执行未鉴权动作） |
| 404 | 40401 | 资源不存在 |
| 404 | 40402 | 租户不存在 |
| 409 | 40901 | 资源冲突（重名 / 状态冲突） |
| 409 | 40902 | 幂等键已使用，结果不同 |
| 422 | 42201 | 业务校验失败（如规则 CEL 语法错） |
| 429 | 42901 | 租户 API QPS 超限 |
| 429 | 42902 | 租户事件配额超限 |
| 429 | 42903 | 租户 LLM token 月度超限 |
| 500 | 50001 | 服务器内部错误 |
| 502 | 50201 | 上游微服务不可用（AC/Engine/LLMProxy） |
| 503 | 50301 | 服务降级中（如 Kafka 不可用） |
| 504 | 50401 | 上游微服务超时 |

### 1.6 API 限流（per-tenant）

| 接入层 | 默认 QPS | 突发 | 配置位置 |
|--------|----------|------|----------|
| 用户 HTTP API（每租户） | 200 QPS | burst 400 | `tenants.quota_qps` |
| Agent gRPC（每 Agent） | 100 msg/s | burst 200 | `manager.yaml` |
| LLMProxy（每租户） | 30 QPS | burst 60 | `tenants.quota_llm_qps` |
| 报表导出 | 5 并发任务 / 租户 | - | `manager.yaml` |
| 漏洞扫描 | 3 并发 / 租户 | - | `manager.yaml` |

超限返回 `429`，Header 含 `X-RateLimit-Remaining` / `X-RateLimit-Reset`。

### 1.7 版本治理

| 状态 | 含义 | 响应头 |
|------|------|--------|
| `Stable` | 正式接口 | - |
| `Beta` | 实验性，签名 / 行为可能微调 | `X-API-Status: beta` |
| `Deprecated` | 已废弃，仍可用 | `Deprecation: true`，`Sunset: <RFC1123 date>`，`Link: </api/v2/...>; rel="successor-version"` |
| `Removed` | 已移除 | 410 Gone |

例：v1 `/api/v1/hosts` 响应：

```
HTTP/1.1 200 OK
Deprecation: true
Sunset: Sat, 01 Jan 2027 00:00:00 GMT
Link: </api/v2/hosts>; rel="successor-version"
Warning: 299 - "API v1 will be removed 2027-01-01, migrate to v2"
```

### 1.8 OpenAPI 3.0 Schema

自动生成的 OpenAPI 3.0 文档：

| 微服务 | 在线文档 | YAML 下载 |
|--------|----------|-----------|
| Manager | `https://<manager-host>/openapi/manager.html` | `/openapi/manager.yaml` |
| AgentCenter | `https://<ac-host>/openapi/ac.html` | `/openapi/ac.yaml` |
| Engine（内部） | `https://<engine-host>/openapi/engine.html` | `/openapi/engine.yaml` |
| VulnSync（内部） | `https://<vulnsync-host>/openapi/vulnsync.html` | `/openapi/vulnsync.yaml` |
| LLMProxy（内部） | `https://<llmproxy-host>/openapi/llmproxy.html` | `/openapi/llmproxy.yaml` |

仓库内 schema 源文件位于 `api/openapi/*.yaml`。

### 1.9 SDK 与 CLI

- Go SDK：`go get github.com/imkerbos/mxsec-sdk-go`
- Python SDK：`pip install mxsec-sdk`
- 集群运维 CLI：`mxctl`（详见 [`deployment.md`](deployment.md)）

---

## 2. 公开接口（无需 JWT）

### 2.1 健康与监控

| 方法 | 路径 | 服务 | 说明 |
|------|------|------|------|
| GET | `/health` | Manager/AC/Engine/VulnSync/LLMProxy | 简单存活探针（K8s liveness） |
| GET | `/healthz` | 同上 | K8s readiness（含依赖检查） |
| GET | `/api/v2/health` | Manager | 富信息健康检查（版本、组件状态、模式） |
| GET | `/metrics` | 所有微服务 | Prometheus 抓取，绑定 9100/9101/9102/9103/9104/9105 端口 |

`/api/v2/health` 响应示例：

```json
{
  "code": 0,
  "data": {
    "status": "ok",
    "version": "v2.0.3",
    "build": "2026-06-05T08:00:00Z",
    "mode_default": "observe",
    "tenants_count": 12,
    "components": {
      "mysql": {"status": "ok", "latency_ms": 2},
      "redis": {"status": "ok", "latency_ms": 1},
      "kafka": {"status": "ok", "lag_p99_sec": 4},
      "clickhouse": {"status": "ok", "latency_ms": 6}
    },
    "ac_instances_healthy": 3,
    "ac_instances_total": 3
  }
}
```

### 2.2 Agent / 插件下载

| 方法 | 路径 | 服务 | 说明 |
|------|------|------|------|
| GET | `/agent/install.sh` | Manager | Agent 一键安装脚本 |
| GET | `/agent/uninstall.sh` | Manager | Agent 卸载脚本 |
| GET | `/api/v2/plugins/download/:name` | Manager | 下载插件包（含 Ed25519 签名） |
| GET | `/api/v2/agent/download/:pkg_type/:arch` | Manager | 下载 Agent 包（pkg_type=deb/rpm/tar；arch=amd64/arm64） |
| GET | `/api/v2/agent/update-check` | Manager | Agent 更新检查（query: cur_version, host_id） |
| GET | `/api/v2/dependency/download/:name` | Manager | 下载依赖二进制（如 `clamav-db`） |

### 2.3 公开站点配置

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/system-config/site` | 站点 Logo / 名称 / 备案号（登录页用） |

### 2.4 K8s 审计 Webhook

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v2/kube/audit-webhook/:cluster_token` | K8s APIServer 审计回调，集群 token 鉴权（每集群独立） |

---

## 3. 认证与会话

### 3.1 登录登出

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v2/auth/login` | 用户登录（用户名 + 密码 + 可选 MFA） |
| POST | `/api/v2/auth/login/sso` | SSO 单点登录（OIDC / SAML 回调） |
| POST | `/api/v2/auth/mfa/verify` | MFA 二次校验（TOTP） |
| POST | `/api/v2/auth/logout` | 登出（token 加入撤销列表） |
| POST | `/api/v2/auth/refresh` | 刷新 access_token（用 refresh_token） |
| GET | `/api/v2/auth/me` | 当前用户信息（含租户、角色、权限） |
| POST | `/api/v2/auth/change-password` | 修改密码 |

**登录请求**：

```http
POST /api/v2/auth/login HTTP/1.1
Content-Type: application/json

{
  "username": "alice",
  "password": "********",
  "tenant_slug": "bank-a",
  "mfa_code": "123456"
}
```

**登录响应**：

```json
{
  "code": 0,
  "data": {
    "access_token": "eyJhbG...",
    "refresh_token": "eyJrZWY...",
    "token_type": "Bearer",
    "expires_in": 3600,
    "user": {
      "id": "u-12345",
      "username": "alice",
      "display_name": "Alice Wang",
      "tenant_id": "t-bank-a",
      "tenant_type": "standalone",
      "roles": ["soc-analyst", "vuln-operator"],
      "is_platform_admin": false
    }
  }
}
```

**JWT claims** 结构：

```json
{
  "sub": "u-12345",
  "tenant_id": "t-bank-a",
  "tenant_type": "standalone",
  "roles": ["soc-analyst", "vuln-operator"],
  "exp": 1730000000,
  "iat": 1729996400,
  "iss": "mxsec-manager",
  "aud": "mxsec-platform",
  "jti": "tk-abc123"
}
```

### 3.2 RBAC

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/rbac/roles` | 角色列表（租户内） |
| POST | `/api/v2/rbac/roles` | 创建自定义角色 |
| PUT | `/api/v2/rbac/roles/:id` | 更新角色权限 |
| DELETE | `/api/v2/rbac/roles/:id` | 删除角色（内置角色不可删） |
| GET | `/api/v2/rbac/permissions` | 全部权限点字典 |
| POST | `/api/v2/rbac/users/:user_id/roles` | 给用户绑定角色 |

内置角色：`tenant-admin`、`soc-analyst`、`vuln-operator`、`baseline-operator`、`kube-operator`、`auditor`、`read-only`。

### 3.3 用户管理（租户作用域）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/users` | 用户列表 |
| GET | `/api/v2/users/:id` | 用户详情 |
| POST | `/api/v2/users` | 创建用户 |
| PUT | `/api/v2/users/:id` | 更新用户 |
| DELETE | `/api/v2/users/:id` | 删除用户（软删除） |
| POST | `/api/v2/users/:id/reset-password` | 重置密码（管理员） |
| POST | `/api/v2/users/:id/enable-mfa` | 启用 MFA |

---

## 4. Manager — 业务控制面

> Manager 是浏览器 / API / CI/CD 唯一对外业务入口。所有路径都在 `/api/v2/*`，并经过鉴权三段。

### 4.1 系统运行模式 API（observe / protect 切换）

`mode` 是 mxsec 的**核心产品语义**。所有动作类响应（IP 封禁 / 进程 kill / Admission deny / 隔离 / 微隔离 / RASP / NPatch）都受 `mode` 控制；用户主动触发的响应不受。

详见 [`operating-modes.md`](operating-modes.md)。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/system/mode` | 查询当前模式配置（含全局 / 租户 / 标签 / 规则四级） |
| POST | `/api/v2/system/mode/switch-request` | 发起模式切换请求（自动校验 6 门槛） |
| GET | `/api/v2/system/mode/switch-request/:id` | 切换请求详情（含审批状态、灰度进度） |
| POST | `/api/v2/system/mode/switch-request/:id/approve` | 审批通过（双人 + 客户签字） |
| POST | `/api/v2/system/mode/switch-request/:id/reject` | 审批拒绝 |
| POST | `/api/v2/system/mode/switch-request/:id/rollback` | 一键回滚到 observe |
| GET | `/api/v2/system/mode/switch-history` | 切换历史（audit_log 来源，保留 6 月） |
| GET | `/api/v2/system/mode/eligibility` | 查询切换准入状态（G1-G6） |

**查询模式响应**：

```http
GET /api/v2/system/mode HTTP/1.1
Authorization: Bearer eyJhbG...
X-Tenant-ID: t-bank-a
```

```json
{
  "code": 0,
  "data": {
    "global_default": "observe",
    "tenant_override": {
      "tenant_id": "t-bank-a",
      "mode": "observe",
      "switched_at": null
    },
    "host_label_overrides": [
      {"label": "env=dev", "mode": "protect", "switched_at": "2026-04-01T00:00:00Z"}
    ],
    "rule_overrides": [
      {"rule_id": "BRUTE_FORCE_SSH", "mode": "protect"}
    ],
    "next_eligible_for_protect_at": "2026-08-15T00:00:00Z",
    "eligibility": {
      "G1_data_warmup_days": {"required": 90, "actual": 76, "pass": false},
      "G2_fp_rate": {"required_max": 0.02, "actual": 0.018, "pass": true},
      "G3_precision": {"required_min": 0.85, "actual": 0.88, "pass": true},
      "G4_replay_recall": {"required_min": 0.85, "actual": 0.92, "pass": true},
      "G5_customer_signoff": {"required": true, "actual": false, "pass": false},
      "G6_canary_ready": {"required": true, "actual": true, "pass": true}
    }
  }
}
```

**发起切换请求**：

```http
POST /api/v2/system/mode/switch-request HTTP/1.1
Authorization: Bearer eyJhbG...
X-Tenant-ID: t-bank-a
Content-Type: application/json

{
  "scope": "tenant",
  "tenant_id": "t-bank-a",
  "target_mode": "protect",
  "rollout": {
    "stages": [5, 25, 100],
    "stage_interval_hours": 24,
    "auto_rollback_threshold": 0.05
  },
  "reason": "完成 90 天磨合，误报率 0.018，客户授权"
}
```

响应：

```json
{
  "code": 0,
  "data": {
    "request_id": "mreq-2026060601",
    "status": "pending_approval",
    "eligibility_check": "passed",
    "approvers_required": ["security_lead", "customer_signoff"]
  }
}
```

### 4.2 多租户管理 API（仅 SystemAdmin）

详见 [`multi-tenant.md`](multi-tenant.md)。这组 API 强制走 `/api/v2/admin/*` 前缀，仅平台超管 token (`is_platform_admin=true`) 可访问。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/admin/tenants` | 全租户列表（分页） |
| POST | `/api/v2/admin/tenants` | 创建租户 |
| GET | `/api/v2/admin/tenants/:id` | 租户详情 |
| PUT | `/api/v2/admin/tenants/:id` | 更新租户（配额、隔离策略、保留期） |
| DELETE | `/api/v2/admin/tenants/:id` | 软删除租户（30 天回收站） |
| POST | `/api/v2/admin/tenants/:id/suspend` | 暂停租户（API 全部返回 403） |
| POST | `/api/v2/admin/tenants/:id/resume` | 恢复租户 |
| POST | `/api/v2/admin/tenants/:id/migrate-isolation` | 迁移隔离策略（shared → schema → db） |
| GET | `/api/v2/admin/tenants/:id/metrics` | 租户用量与计费汇总 |
| GET | `/api/v2/admin/tenants/:id/usage` | 详细用量明细 |
| GET | `/api/v2/admin/system/health` | 平台健康（跨租户） |
| GET | `/api/v2/admin/system/capacity` | 容量水位（Agent 数 / Kafka lag / DB 大小） |

**创建租户**：

```http
POST /api/v2/admin/tenants HTTP/1.1
Authorization: Bearer eyJhbG...
Content-Type: application/json

{
  "id": "t-bank-c",
  "name": "Bank C 安全运营",
  "type": "standalone",
  "default_mode": "observe",
  "ml_enabled": true,
  "llm_enabled": false,
  "quota_agents": 5000,
  "quota_llm_usd": 0,
  "retention_alerts_days": 180,
  "isolation_strategy": "shared",
  "admin": {
    "username": "bank-c-admin",
    "email": "admin@bank-c.example.com",
    "initial_password": "*****"
  }
}
```

### 4.3 MSSP 父租户聚合 API

仅 `tenant_type=mssp_parent` 的租户可访问，全部为 **read-only**。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/mssp/children` | 子租户列表 |
| GET | `/api/v2/mssp/children/:tenant_id` | 子租户摘要 |
| GET | `/api/v2/mssp/aggregate/alerts` | 跨子告警聚合 |
| GET | `/api/v2/mssp/aggregate/vulnerabilities` | 跨子漏洞汇总 |
| GET | `/api/v2/mssp/aggregate/baseline` | 跨子合规率 |
| GET | `/api/v2/mssp/aggregate/agents` | 跨子 Agent 健康 |
| GET | `/api/v2/mssp/reports/monthly` | MSSP 月度运营报告 |

### 4.4 资产 API

mxsec 的资产采集聚焦 Linux 主机 + K8s 容器（不含 Windows / macOS）。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/assets/overview` | 资产概览（总主机数 / 容器数 / SBOM 覆盖率） |
| GET | `/api/v2/assets/history` | 资产历史快照 |
| GET | `/api/v2/assets/statistics` | 资产统计（按 OS / 业务线 / 区域） |
| GET | `/api/v2/assets/relations` | 资产关系图（主机 → 容器 → 服务） |
| GET | `/api/v2/assets/status` | 各 Agent 采集器健康状态 |
| GET | `/api/v2/assets/top` | TopN 资产（CPU/RSS/进程数等） |
| GET | `/api/v2/assets/processes` | 进程清单 |
| GET | `/api/v2/assets/ports` | 端口清单 |
| GET | `/api/v2/assets/users` | OS 账户 |
| GET | `/api/v2/assets/software` | 软件包（rpm/dpkg/pip/npm/...） |
| GET | `/api/v2/assets/containers` | 容器清单（含镜像 digest） |
| GET | `/api/v2/assets/apps` | 应用进程拓扑 |
| GET | `/api/v2/assets/network-interfaces` | 网卡 |
| GET | `/api/v2/assets/volumes` | 卷 |
| GET | `/api/v2/assets/kmods` | 内核模块 |
| GET | `/api/v2/assets/services` | systemd 服务 |
| GET | `/api/v2/assets/crons` | 计划任务 |
| GET | `/api/v2/assets/export` | 资产导出（CSV/Excel） |
| GET | `/api/v2/assets/sbom` | SBOM 导出（SPDX / CycloneDX） |

**示例**：

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://manager.example.com/api/v2/assets/processes?host_id=h-12345&page=1&page_size=50"
```

资产模型详见 [`asset-model.md`](asset-model.md)。

### 4.5 主机管理 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/hosts` | 主机列表（filter: status / risk / business_line / tag / mode） |
| GET | `/api/v2/hosts/status-distribution` | 状态分布 |
| GET | `/api/v2/hosts/risk-distribution` | 风险分布 |
| POST | `/api/v2/hosts/restart-agent` | 重启 Agent |
| GET | `/api/v2/hosts/restart-records` | Agent 重启记录 |
| GET | `/api/v2/hosts/:host_id` | 主机详情（含当前生效模式） |
| GET | `/api/v2/hosts/:host_id/metrics` | 主机监控指标（来源 Prometheus） |
| GET | `/api/v2/hosts/:host_id/risk-statistics` | 主机风险统计 |
| GET | `/api/v2/hosts/:host_id/plugins` | 主机插件列表与版本 |
| PUT | `/api/v2/hosts/:host_id/tags` | 更新主机标签 |
| PUT | `/api/v2/hosts/:host_id/business-line` | 更新主机业务线 |
| PUT | `/api/v2/hosts/:host_id/mode-override` | 主机标签级模式覆盖（详见 §4.1） |
| DELETE | `/api/v2/hosts/:host_id` | 删除主机 |
| POST | `/api/v2/hosts/batch-delete` | 批量删除（上限 100，支持 `force=true`） |
| POST | `/api/v2/hosts/batch-update-tags` | 批量更新标签（append/replace） |
| POST | `/api/v2/hosts/batch-update-business-line` | 批量更新业务线 |
| POST | `/api/v2/hosts/:host_id/isolate` | 主动隔离主机（micro-isolation；任何模式都允许，需 RBAC） |
| POST | `/api/v2/hosts/:host_id/unisolate` | 解除隔离 |

### 4.6 告警 API

> 所有告警均由 Engine 产生并经 Kafka `mxsec.engine.alert` → Consumer 入库，Manager 仅做读取与状态变更。
> 每条告警都带 `mode` 字段，UI 据此区分 "would_action"（observe）与 "action"（protect）。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/alerts` | 告警列表（filter: severity, mode, status, rule_id, host_id, att_ck, since, until） |
| GET | `/api/v2/alerts/statistics` | 告警统计 |
| GET | `/api/v2/alerts/edr-statistics` | EDR 告警统计 |
| GET | `/api/v2/alerts/:id` | 告警详情（含 storyline / would_action / action） |
| GET | `/api/v2/alerts/:id/context` | 溯源上下文（前后 5min 关联事件） |
| GET | `/api/v2/alerts/:id/storyline` | 攻击链可视化数据 |
| POST | `/api/v2/alerts/:id/resolve` | 标记已处置 |
| POST | `/api/v2/alerts/:id/ignore` | 忽略（加白名单可选） |
| POST | `/api/v2/alerts/:id/feedback` | 反馈：真威胁 / 误报 / 不确定（写入 `mxsec.engine.feedback` Topic） |
| POST | `/api/v2/alerts/:id/execute-would-action` | observe 模式下手动触发 would_action（需 RBAC + audit） |
| POST | `/api/v2/alerts/batch/resolve` | 批量解决 |
| POST | `/api/v2/alerts/batch/ignore` | 批量忽略 |
| POST | `/api/v2/alerts/batch/delete` | 批量删除 |
| GET | `/api/v2/alerts/whitelist` | 白名单 |
| POST | `/api/v2/alerts/whitelist` | 创建白名单 |
| PUT | `/api/v2/alerts/whitelist/:id` | 更新白名单 |
| DELETE | `/api/v2/alerts/whitelist/:id` | 删除白名单 |
| GET | `/api/v2/alerts/sse` | SSE 实时推送（每租户独立通道） |

**告警详情**（observe 模式示例）：

```json
{
  "code": 0,
  "data": {
    "alert_id": "alrt-2026060100001",
    "tenant_id": "t-bank-a",
    "host_id": "h-12345",
    "rule_id": "BRUTE_FORCE_SSH",
    "severity": "high",
    "mode": "observe",
    "status": "open",
    "detected_at": "2026-06-01T10:23:45Z",
    "att_ck": ["T1110.001"],
    "storyline": [
      {"ts": "2026-06-01T10:18:00Z", "event": "ssh_login_failed", "src_ip": "192.0.2.45"},
      {"ts": "2026-06-01T10:23:45Z", "event": "ssh_login_failed_5times", "src_ip": "192.0.2.45"}
    ],
    "would_action": {
      "type": "ip_block",
      "target": "192.0.2.45",
      "duration_sec": 3600,
      "reason": "5 次 SSH 登录失败"
    },
    "action": null,
    "ml_score": 0.92,
    "llm_summary": "来自 192.0.2.45 的 SSH 暴力破解尝试，符合 ATT&CK T1110.001 模式"
  }
}
```

**反馈接口**：

```http
POST /api/v2/alerts/alrt-2026060100001/feedback HTTP/1.1
Authorization: Bearer eyJhbG...
Content-Type: application/json

{
  "label": "true_positive",
  "comment": "确认是攻击，来自已知 IP",
  "tag": "external_brute_force"
}
```

`label` 枚举：`true_positive` / `false_positive` / `uncertain`，反馈数据写入 `mxsec.engine.feedback` Topic，Engine 用于 ML 增量训练与规则白名单建议。

### 4.7 漏洞 API

漏洞情报由 VulnSync 服务从 11+ 外部源融合后推 Kafka `mxsec.vuln.advisory`；主机漏洞由 Engine 关联匹配产出。Manager 提供读取与处置入口。

详见 [`vuln-module-design.md`](vuln-module-design.md)、[`vulnsync-design.md`](vulnsync-design.md)。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/vulnerabilities` | 漏洞列表（filter: cve, severity, host_id, kev, exploit, fix_status） |
| GET | `/api/v2/vulnerabilities/:id` | 漏洞详情（含 advisory 多源融合结果） |
| POST | `/api/v2/vulnerabilities/:id/ignore` | 忽略漏洞（指定原因 + 过期日） |
| POST | `/api/v2/vulnerabilities/sync` | 触发漏洞库增量同步（异步） |
| POST | `/api/v2/vulnerabilities/scan` | 触发漏洞扫描（scope=global/hosts/business_line） |
| GET | `/api/v2/vulnerabilities/scan-status` | 当前扫描状态 |
| GET | `/api/v2/vulnerabilities/scan-history` | 扫描历史 |
| GET | `/api/v2/vulnerabilities/scan-tasks` | 定向扫描任务列表 |
| GET | `/api/v2/vulnerabilities/scan-tasks/:task_id` | 定向扫描任务进度 |
| GET | `/api/v2/vulnerabilities/:id/advice` | 修复建议（LLM 增强可选） |
| POST | `/api/v2/vulnerabilities/:id/patch` | 触发修复（生成修复任务，protect 模式下可自动执行） |
| POST | `/api/v2/vulnerabilities/:id/verify` | 验证修复结果 |
| GET | `/api/v2/vulnerabilities/stats/remediation` | 修复统计 |
| GET | `/api/v2/vulnerabilities/stats/trend` | 修复趋势 |
| GET | `/api/v2/vuln-bulletins` | 漏洞公告订阅列表 |
| GET | `/api/v2/vuln-data-sources` | VulnSync 数据源状态（代理 VulnSync 内部 API） |

**扫描请求**：

```http
POST /api/v2/vulnerabilities/scan HTTP/1.1
Authorization: Bearer eyJhbG...
Content-Type: application/json

{
  "scope": "business_line",
  "business_line_id": "bl-payment",
  "include_oval": true,
  "include_sbom": true,
  "priority": "high"
}
```

响应：

```json
{
  "code": 0,
  "data": {
    "task_id": "vstk-2026060601",
    "estimated_hosts": 1234,
    "estimated_duration_sec": 1800
  }
}
```

### 4.8 漏洞修复任务

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v2/remediation-tasks` | 创建修复任务 |
| GET | `/api/v2/remediation-tasks` | 修复任务列表 |
| GET | `/api/v2/remediation-tasks/stats` | 修复任务统计 |
| GET | `/api/v2/remediation-tasks/:id` | 修复任务详情 |
| POST | `/api/v2/remediation-tasks/:id/confirm` | 人工确认（即便 protect 模式仍需用户审批） |
| POST | `/api/v2/remediation-tasks/:id/cancel` | 取消修复 |
| POST | `/api/v2/remediation-tasks/:id/retry` | 重试 |
| POST | `/api/v2/remediation-tasks/:id/verify` | 验证修复结果 |
| POST | `/api/v2/remediation-tasks/:id/rollback` | 回滚修复 |
| POST | `/api/v2/remediation-tasks/batch` | 批量创建 |
| POST | `/api/v2/remediation-tasks/batch-confirm` | 批量确认 |
| POST | `/api/v2/remediation-tasks/batch-retry` | 批量重试 |
| POST | `/api/v2/remediation-tasks/batch-cancel` | 批量取消 |
| GET | `/api/v2/remediation-policies` | 修复策略 |
| POST | `/api/v2/remediation-policies` | 创建修复策略 |
| PUT | `/api/v2/remediation-policies/:id` | 更新修复策略 |
| DELETE | `/api/v2/remediation-policies/:id` | 删除修复策略 |

### 4.9 基线 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/policy-groups` | 策略组列表（CIS / 等保 / ISO 等） |
| POST | `/api/v2/policy-groups` | 创建策略组 |
| GET | `/api/v2/policy-groups/:id` | 策略组详情 |
| PUT | `/api/v2/policy-groups/:id` | 更新策略组 |
| DELETE | `/api/v2/policy-groups/:id` | 删除策略组 |
| GET | `/api/v2/policy-groups/:id/statistics` | 策略组统计 |
| GET | `/api/v2/policies` | 策略列表 |
| GET | `/api/v2/policies/:policy_id` | 策略详情 |
| POST | `/api/v2/policies` | 创建策略 |
| PUT | `/api/v2/policies/:policy_id` | 更新策略 |
| DELETE | `/api/v2/policies/:policy_id` | 删除策略 |
| POST | `/api/v2/policies/batch/enable` | 批量启用/禁用 |
| POST | `/api/v2/policies/batch/delete` | 批量删除 |
| POST | `/api/v2/policies/batch/export` | 批量导出 |
| POST | `/api/v2/policies/import` | 导入策略 |
| GET | `/api/v2/policies/:policy_id/rules` | 策略下的规则 |
| POST | `/api/v2/policies/:policy_id/rules` | 在策略下创建规则 |
| GET | `/api/v2/rules/:rule_id` | 规则详情 |
| PUT | `/api/v2/rules/:rule_id` | 更新规则 |
| DELETE | `/api/v2/rules/:rule_id` | 删除规则 |
| GET | `/api/v2/results` | 检查结果列表 |
| GET | `/api/v2/results/detail` | 检查结果详情 |
| GET | `/api/v2/results/host/:host_id/score` | 主机基线得分 |
| GET | `/api/v2/results/host/:host_id/summary` | 主机基线摘要 |
| GET | `/api/v2/results/host/:host_id/export` | 导出主机基线结果 |

### 4.10 基线修复 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/fix/fixable-items` | 可修复项列表 |
| POST | `/api/v2/fix-tasks` | 创建基线修复任务 |
| GET | `/api/v2/fix-tasks` | 基线修复任务列表 |
| GET | `/api/v2/fix-tasks/:task_id` | 任务详情 |
| GET | `/api/v2/fix-tasks/:task_id/results` | 修复结果 |
| GET | `/api/v2/fix-tasks/:task_id/host-status` | 任务主机状态 |
| POST | `/api/v2/fix-tasks/:task_id/cancel` | 取消任务 |
| DELETE | `/api/v2/fix-tasks/:task_id` | 删除任务 |

### 4.11 扫描任务（基线 / 漏洞通用）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/tasks` | 任务列表 |
| GET | `/api/v2/tasks/:task_id` | 任务详情 |
| GET | `/api/v2/tasks/:task_id/host-status` | 任务主机执行状态 |
| POST | `/api/v2/tasks` | 创建扫描任务 |
| POST | `/api/v2/tasks/:task_id/run` | 执行任务 |
| POST | `/api/v2/tasks/:task_id/cancel` | 取消任务 |
| DELETE | `/api/v2/tasks/:task_id` | 删除任务 |
| GET | `/api/v2/scan-schedules` | 周期扫描计划 |
| POST | `/api/v2/scan-schedules` | 创建周期计划 |
| PUT | `/api/v2/scan-schedules/:id` | 更新周期计划 |
| DELETE | `/api/v2/scan-schedules/:id` | 删除周期计划 |

### 4.12 报表 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/reports/stats` | 报表统计 |
| GET | `/api/v2/reports/baseline-score-trend` | 基线评分趋势 |
| GET | `/api/v2/reports/check-result-trend` | 检查结果趋势 |
| GET | `/api/v2/reports/task/:task_id` | 任务报告 |
| GET | `/api/v2/reports/task/:task_id/host/:host_id` | 任务主机详情报告 |
| GET | `/api/v2/reports/task/:task_id/executive` | 基线执行摘要 |
| GET | `/api/v2/reports/top-failed-rules` | 失败最多的规则 |
| GET | `/api/v2/reports/top-risk-hosts` | 风险最高的主机 |
| GET | `/api/v2/reports/antivirus` | 病毒查杀报告 |
| GET | `/api/v2/reports/vulnerability` | 漏洞报告 |
| GET | `/api/v2/reports/kube` | 容器安全报告 |
| GET | `/api/v2/reports/edr` | EDR 检测报告 |
| GET | `/api/v2/reports/compliance/dengbao` | 等保自评报告 |
| GET | `/api/v2/reports/compliance/iso27001` | ISO 27001 报告 |
| GET | `/api/v2/reports/compliance/monthly` | 月度合规报告 |
| GET | `/api/v2/reports/antivirus/:task_id/executive` | 病毒查杀执行摘要 |
| GET | `/api/v2/reports/vulnerability/executive` | 漏洞执行摘要 |
| GET | `/api/v2/reports/remediation/executive` | 修复执行摘要 |
| GET | `/api/v2/reports/kube/executive` | 容器安全执行摘要 |
| GET | `/api/v2/reports/edr/executive` | EDR 检测执行摘要 |
| GET | `/api/v2/reports/generated` | 已生成报告列表 |
| GET | `/api/v2/reports/generated/:id` | 已生成报告详情 |
| GET | `/api/v2/reports/generated/:id/download` | 下载报告（PDF/Word/CSV） |
| DELETE | `/api/v2/reports/generated/:id` | 删除已生成报告 |

### 4.13 病毒查杀 API（含隔离箱）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/antivirus/tasks` | 扫描任务列表 |
| POST | `/api/v2/antivirus/tasks` | 创建扫描任务 |
| GET | `/api/v2/antivirus/tasks/:id` | 扫描任务详情 |
| DELETE | `/api/v2/antivirus/tasks/:id` | 删除扫描任务 |
| POST | `/api/v2/antivirus/tasks/:id/cancel` | 取消扫描任务 |
| GET | `/api/v2/antivirus/results` | 扫描结果列表 |
| GET | `/api/v2/antivirus/results/:id` | 扫描结果详情 |
| POST | `/api/v2/antivirus/results/:id/quarantine` | 隔离文件（任何模式都允许，需 RBAC） |
| POST | `/api/v2/antivirus/results/:id/ignore` | 忽略 |
| POST | `/api/v2/antivirus/results/:id/delete-file` | 删除文件（双确认） |
| GET | `/api/v2/antivirus/statistics` | 病毒查杀统计 |
| GET | `/api/v2/antivirus/virus-db/status` | 病毒库状态 |
| GET | `/api/v2/antivirus/virus-db/history` | 病毒库历史 |
| POST | `/api/v2/antivirus/virus-db/sync` | 触发病毒库同步 |
| GET | `/api/v2/quarantine/files` | 隔离文件列表 |
| GET | `/api/v2/quarantine/files/:id` | 隔离文件详情 |
| POST | `/api/v2/quarantine/files/:id/restore` | 恢复文件 |
| DELETE | `/api/v2/quarantine/files/:id` | 删除隔离文件 |
| POST | `/api/v2/quarantine/files/batch-delete` | 批量删除 |
| GET | `/api/v2/quarantine/statistics` | 隔离箱统计 |

### 4.14 EDR 事件 / 异常 / Storyline / 威胁狩猎

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/edr/events` | EDR 内核事件列表（ClickHouse 直查） |
| GET | `/api/v2/edr/events/:id` | EDR 事件详情 |
| GET | `/api/v2/anomaly` | 异常检测结果（Engine 序列层产出） |
| GET | `/api/v2/storyline` | 攻击链列表 |
| GET | `/api/v2/storyline/:id` | 攻击链详情（含 ATT&CK 战术映射） |
| POST | `/api/v2/hunting/query` | 威胁狩猎查询（SQL 子集 + 资产语义） |
| GET | `/api/v2/hunting/queries` | 保存的狩猎语句 |
| POST | `/api/v2/hunting/queries` | 保存狩猎语句 |

### 4.15 FIM 文件完整性监控

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/fim/policies` | FIM 策略列表 |
| POST | `/api/v2/fim/policies` | 创建 FIM 策略 |
| GET | `/api/v2/fim/policies/:id` | FIM 策略详情 |
| PUT | `/api/v2/fim/policies/:id` | 更新 FIM 策略 |
| DELETE | `/api/v2/fim/policies/:id` | 删除 FIM 策略 |
| GET | `/api/v2/fim/baselines` | FIM 基线列表（快照） |
| POST | `/api/v2/fim/baselines` | 创建 FIM 基线 |
| GET | `/api/v2/fim/tasks` | FIM 任务列表 |
| POST | `/api/v2/fim/tasks` | 创建 FIM 任务 |
| GET | `/api/v2/fim/tasks/:id` | FIM 任务详情 |
| POST | `/api/v2/fim/tasks/:id/run` | 执行 FIM 任务 |
| GET | `/api/v2/fim/events` | FIM 事件列表 |
| GET | `/api/v2/fim/events/stats` | FIM 事件统计 |
| GET | `/api/v2/fim/events/:id` | FIM 事件详情 |

### 4.16 网络阻断 / 主机隔离

> 这两组 API **创建** / **触发** 动作受 `mode` 控制：observe 模式仅写 audit 不下发到 Agent；protect 模式下发 iptables / PAM / NetworkPolicy。
> 但**查询**与**手动用户触发**任何模式都允许。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/network-block/rules` | 阻断规则列表 |
| POST | `/api/v2/network-block/rules` | 创建阻断规则 |
| POST | `/api/v2/network-block/rules/:id/remove` | 移除阻断 |
| DELETE | `/api/v2/network-block/rules/:id` | 删除规则 |
| GET | `/api/v2/host-isolation` | 主机隔离列表 |
| POST | `/api/v2/host-isolation` | 隔离主机（仅放行管控通道） |
| POST | `/api/v2/host-isolation/:id/release` | 解除隔离 |

### 4.17 威胁情报

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/threat-intel/stats` | IOC 统计 |
| GET | `/api/v2/threat-intel/iocs` | IOC 列表 |
| POST | `/api/v2/threat-intel/check` | IOC 查询 |
| POST | `/api/v2/threat-intel/sync` | 触发情报同步 |
| GET | `/api/v2/threat-intel/sync-status` | 同步状态 |
| GET | `/api/v2/threat-intel/sync-history` | 同步历史 |

### 4.18 检测规则管理（UI 侧；Engine 内部规则见 §6）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/detection-rules` | 检测规则列表（含 CEL / Sigma / Falco / Tetragon 转换后） |
| GET | `/api/v2/detection-rules/categories` | 规则分类 |
| GET | `/api/v2/detection-rules/mitre-ids` | MITRE ATT&CK ID 列表 |
| GET | `/api/v2/detection-rules/statistics` | 规则统计（命中 / 误报 / 准确率） |
| GET | `/api/v2/detection-rules/:id` | 规则详情 |
| POST | `/api/v2/detection-rules` | 创建检测规则 |
| PUT | `/api/v2/detection-rules/:id` | 更新检测规则 |
| DELETE | `/api/v2/detection-rules/:id` | 删除（内置规则不可删） |
| POST | `/api/v2/detection-rules/:id/toggle` | 启用 / 禁用 |
| POST | `/api/v2/detection-rules/:id/test` | 规则 dry-run 测试 |
| PUT | `/api/v2/detection-rules/:id/mode-override` | 规则级 mode 覆盖（详见 §4.1） |

### 4.19 通知 / 业务线 / 系统配置 / 审计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/notifications` | 通知渠道列表 |
| POST | `/api/v2/notifications` | 创建通知渠道（站内信/邮件/短信/Syslog/Webhook） |
| PUT | `/api/v2/notifications/:id` | 更新通知渠道 |
| DELETE | `/api/v2/notifications/:id` | 删除通知渠道 |
| POST | `/api/v2/notifications/test` | 测试发送 |
| GET | `/api/v2/business-lines` | 业务线列表 |
| POST | `/api/v2/business-lines` | 创建业务线 |
| PUT | `/api/v2/business-lines/:id` | 更新业务线 |
| DELETE | `/api/v2/business-lines/:id` | 删除业务线 |
| GET | `/api/v2/system-config/site` | 站点配置 |
| PUT | `/api/v2/system-config/site` | 更新站点配置 |
| POST | `/api/v2/system-config/upload-logo` | 上传 Logo |
| GET | `/api/v2/system-config/alert` | 告警配置 |
| PUT | `/api/v2/system-config/alert` | 更新告警配置 |
| GET | `/api/v2/system-config/kubernetes-image` | K8s 镜像配置 |
| PUT | `/api/v2/system-config/kubernetes-image` | 更新 K8s 镜像配置 |
| GET | `/api/v2/audit-logs` | 审计日志列表（filter: user, action, resource, time） |
| GET | `/api/v2/system/backups` | 备份列表 |
| POST | `/api/v2/system/backups` | 创建备份 |
| GET | `/api/v2/system/backup-config` | 备份配置 |
| PUT | `/api/v2/system/backup-config` | 更新备份配置 |
| GET | `/api/v2/system/backups/:id/download` | 下载备份 |
| POST | `/api/v2/system/backups/:id/restore` | 恢复备份 |
| DELETE | `/api/v2/system/backups/:id` | 删除备份 |
| POST | `/api/v2/system/migration/test-connection` | 测试迁移连接 |
| POST | `/api/v2/system/migration/jobs` | 启动迁移任务 |
| GET | `/api/v2/system/migration/jobs` | 迁移任务列表 |
| GET | `/api/v2/system/migration/jobs/:id` | 迁移任务详情 |
| POST | `/api/v2/system/migration/jobs/:id/cancel` | 取消迁移任务 |

### 4.20 Kubernetes 容器安全

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/kube/clusters` | 集群列表 |
| POST | `/api/v2/kube/clusters` | 创建集群 |
| GET | `/api/v2/kube/clusters/:id` | 集群详情 |
| PUT | `/api/v2/kube/clusters/:id` | 更新集群 |
| DELETE | `/api/v2/kube/clusters/:id` | 删除集群 |
| GET | `/api/v2/kube/clusters/:id/nodes` | 节点列表 |
| GET | `/api/v2/kube/clusters/:id/pods` | Pod 列表 |
| GET | `/api/v2/kube/clusters/:id/workloads` | 工作负载列表 |
| POST | `/api/v2/kube/clusters/:id/regenerate-token` | 重新生成审计 Token |
| PUT | `/api/v2/kube/clusters/:id/gcp-config` | 更新 GCP 配置 |
| DELETE | `/api/v2/kube/clusters/:id/gcp-config` | 删除 GCP 配置 |
| GET | `/api/v2/kube/alarms` | 容器告警 |
| POST | `/api/v2/kube/alarms/:id/process` | 处理告警 |
| POST | `/api/v2/kube/alarms/batch-process` | 批量处理 |
| POST | `/api/v2/kube/alarms/batch-ignore` | 批量忽略 |
| GET | `/api/v2/kube/events` | 安全事件 |
| POST | `/api/v2/kube/events/:id/handle` | 处理事件 |
| GET | `/api/v2/kube/baseline` | 基线检查 |
| GET | `/api/v2/kube/baseline/:id` | 基线详情 |
| POST | `/api/v2/kube/baseline/detect` | 执行基线检查 |
| GET | `/api/v2/kube/baseline-rules` | 基线规则 |
| GET | `/api/v2/kube/baseline-rules/export` | 导出基线规则 |
| POST | `/api/v2/kube/baseline-rules/import` | 导入基线规则 |
| POST | `/api/v2/kube/baseline-rules/validate-expression` | 验证检查表达式 |
| GET | `/api/v2/kube/baseline-rules/expression-templates` | 表达式模板 |
| POST | `/api/v2/kube/baseline-rules/expression-templates` | 创建模板 |
| PUT | `/api/v2/kube/baseline-rules/expression-templates/:id` | 更新模板 |
| DELETE | `/api/v2/kube/baseline-rules/expression-templates/:id` | 删除模板 |
| GET | `/api/v2/kube/baseline-rules/:id` | 基线规则详情 |
| POST | `/api/v2/kube/baseline-rules` | 创建基线规则 |
| PUT | `/api/v2/kube/baseline-rules/:id` | 更新基线规则 |
| DELETE | `/api/v2/kube/baseline-rules/:id` | 删除基线规则 |
| PUT | `/api/v2/kube/baseline-rules/:id/toggle` | 启用 / 禁用 |
| GET | `/api/v2/kube/baseline-alerts` | 基线告警 |
| POST | `/api/v2/kube/baseline-alerts/:id/ignore` | 忽略基线告警 |
| POST | `/api/v2/kube/baseline-alerts/batch-ignore` | 批量忽略 |
| GET | `/api/v2/kube/whitelist` | 容器白名单 |
| POST | `/api/v2/kube/whitelist` | 创建白名单 |
| PUT | `/api/v2/kube/whitelist/:id` | 更新白名单 |
| DELETE | `/api/v2/kube/whitelist/:id` | 删除白名单 |
| GET | `/api/v2/kube/stats/summary` | 统计摘要 |
| GET | `/api/v2/kube/stats/alarm-trend` | 告警趋势 |
| POST | `/api/v2/kube/admission/policies` | Admission Webhook 策略（observe = dry-run，protect = enforce） |

### 4.21 组件 / 插件管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/components` | 组件列表 |
| POST | `/api/v2/components` | 创建组件 |
| GET | `/api/v2/components/plugin-status` | 插件同步状态 |
| GET | `/api/v2/components/:id` | 组件详情 |
| DELETE | `/api/v2/components/:id` | 删除组件 |
| GET | `/api/v2/components/:id/versions` | 组件版本 |
| POST | `/api/v2/components/:id/versions` | 发布新版本 |
| GET | `/api/v2/components/:id/versions/:version_id` | 版本详情 |
| PUT | `/api/v2/components/:id/versions/:version_id/set-latest` | 设为最新版 |
| DELETE | `/api/v2/components/:id/versions/:version_id` | 删除版本 |
| POST | `/api/v2/components/:id/versions/:version_id/packages` | 上传安装包（Ed25519 签名校验） |
| DELETE | `/api/v2/packages/:id` | 删除安装包 |
| POST | `/api/v2/components/agent/push-update` | 推送 Agent 更新（Canary 灰度） |
| POST | `/api/v2/components/plugins/sync-latest` | 同步全部插件到最新版 |
| POST | `/api/v2/components/plugins/broadcast` | 广播插件配置 |
| GET | `/api/v2/components/push-records` | 推送记录 |
| GET | `/api/v2/components/push-records/:id` | 推送记录详情 |

### 4.22 SBOM / 监控 / 巡检 / 服务发现

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/sbom/jobs` | SBOM 生成任务 |
| POST | `/api/v2/sbom/jobs` | 触发 SBOM 生成 |
| POST | `/api/v2/sbom/import` | 导入第三方 SBOM（SPDX / CycloneDX） |
| GET | `/api/v2/monitor/host` | 主机监控 |
| GET | `/api/v2/monitor/services` | 服务监控 |
| GET | `/api/v2/monitor/service-alerts` | 服务告警列表 |
| POST | `/api/v2/monitor/service-alerts/:id/ack` | 确认服务告警 |
| GET | `/api/v2/prometheus/alerts` | Prometheus 告警代理 |
| GET | `/api/v2/inspection/overview` | 运维巡检概览 |
| GET | `/api/v2/discovery/agentcenter` | 健康的 AC 实例列表（供 Agent 引导） |
| POST | `/api/v2/dashboard/stats` | Dashboard 统计（聚合缓存） |

### 4.23 依赖管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v2/hosts/dependency/install` | 安装依赖（ClamAV DB / FFI lib） |
| POST | `/api/v2/hosts/dependency/status` | 查询依赖状态 |

### 4.24 内存威胁

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v2/memory-threat/events` | 内存威胁事件 |
| GET | `/api/v2/memory-threat/events/:id` | 事件详情 |
| POST | `/api/v2/memory-threat/events/:id/handle` | 处置 |

---

## 5. AgentCenter — 数据面接入层

> AgentCenter 仅做 **gRPC 数据接入** + **任务下发**，无业务 API。
> 它对外暴露两类接口：
> 1. **gRPC**（Agent ↔ AC）：双向流，mTLS 强认证。
> 2. **HTTP（内部管理）**：Manager 调用 AC 做任务下发、健康探测、连接统计。**不应外网暴露**，仅 Service Mesh 内网放行。

### 5.1 gRPC 服务（Agent ↔ AC）

Proto 定义见 `api/proto/grpc/transfer.proto`。

| RPC | 模式 | 说明 |
|-----|------|------|
| `Transfer(stream PackagedData) returns (stream Command)` | BiDi Stream | 主数据通道；Agent 推数据，AC 推命令 |
| `FileUpload(stream Chunk) returns (UploadAck)` | Client Stream | 文件上传（病毒样本 / quarantine / 大日志） |
| `FileDownload(DownloadReq) returns (stream Chunk)` | Server Stream | 文件下发（病毒库 / 规则包） |
| `Heartbeat(PingReq) returns (PongResp)` | Unary | 备用心跳（主链路用 stream） |

**连接参数**：

```
mTLS:
  ClientAuth: VerifyClientCertIfGiven
  RootCAs: /etc/mxsec/certs/ca.crt
  ServerCert: /etc/mxsec/certs/server.crt
  ServerKey:  /etc/mxsec/certs/server.key
Keepalive:
  Time: 60s
  Timeout: 10s
  MinTime: 10s
  PermitWithoutStream: false
Compression: Snappy
MaxRecvMsgSize: 32 MiB
```

### 5.2 内部 HTTP 管理接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | AC 存活探针 + 在线 Agent 数 |
| GET | `/conn/stat` | 连接统计（在线 / 离线 / 协议版本分布） |
| GET | `/conn/list` | 在线 Agent 连接列表 |
| POST | `/command` | 单 Agent 下发命令 |
| POST | `/command/batch` | 批量下发命令 |
| POST | `/dependency/install` | 触发依赖安装 |
| POST | `/internal/v2/ac/register` | AC 启动后向 Manager SD 注册（由 Manager 调用 AC 也可由 AC 调用 Manager，本接口供 Manager 同步） |
| POST | `/internal/v2/ac/heartbeat` | AC 周期心跳（30s） |
| DELETE | `/internal/v2/ac/deregister` | AC 优雅退出 |

**鉴权**：内部 Bearer Token（`Authorization: Bearer <internal_token>`，与外部 JWT 完全独立）+ mTLS（Phase 1 强制启用，过渡期保留 `X-Internal-Secret`）。

#### 5.2.1 GET `/health`

```bash
curl -sk https://ac-1.svc.local/health
```

```json
{"status": "ok", "online_connections": 1247}
```

#### 5.2.2 GET `/conn/stat`

```json
{
  "online_total": 1247,
  "by_arch": {"amd64": 1100, "arm64": 147},
  "by_os": {"linux/ubuntu22.04": 800, "linux/centos7.9": 300, "linux/openeuler22.03": 147},
  "by_version": {"v1.5.2": 1200, "v1.5.3": 47}
}
```

#### 5.2.3 GET `/conn/list`

支持分页：`?page=1&page_size=100`，仅返回 AC 本实例持有的连接。

```json
{
  "items": [
    {
      "agent_id": "agt-abc",
      "tenant_id": "t-bank-a",
      "host_id": "h-12345",
      "remote_addr": "10.0.1.5:54321",
      "connected_at": "2026-06-05T03:00:00Z",
      "last_seen_at": "2026-06-06T08:00:00Z",
      "protocol_version": "v2"
    }
  ],
  "total": 1247
}
```

#### 5.2.4 POST `/command`

```http
POST /command HTTP/1.1
Authorization: Bearer <ac_internal_token>
Content-Type: application/json

{
  "agent_id": "agt-abc",
  "command": {
    "task_id": "task-2026060601",
    "data_type": 9100,
    "data": "<protobuf-base64>",
    "timeout_sec": 60
  }
}
```

响应：

```json
{
  "code": 0,
  "data": {
    "queued": true,
    "queue_position": 2,
    "estimated_dispatch_at": "2026-06-06T08:00:01Z"
  }
}
```

错误：

| Code | 含义 |
|------|------|
| 50201 | Agent 离线（不在该 AC 实例） |
| 50301 | sendCh 满（流控） |
| 40402 | agent_id 未注册 |

#### 5.2.5 POST `/command/batch`

```json
{
  "commands": [
    {"agent_id": "agt-abc", "command": {...}},
    {"agent_id": "agt-def", "command": {...}}
  ]
}
```

响应附每条结果：

```json
{
  "code": 0,
  "data": {
    "results": [
      {"agent_id": "agt-abc", "queued": true},
      {"agent_id": "agt-def", "queued": false, "error": "agent_offline"}
    ],
    "success": 1,
    "failed": 1
  }
}
```

#### 5.2.6 POST `/dependency/install`

下发依赖安装任务（ClamAV 病毒库 / FFI 二进制等）。

```json
{
  "agent_id": "agt-abc",
  "name": "clamav-db",
  "version": "v2026060501",
  "force": false
}
```

---

## 6. Engine — 检测分析引擎

> Engine 是 Phase 1 新增的核心微服务。Manager / UI 通过 HTTP 间接调用，Engine 与 LLMProxy 通过 gRPC 互调。
> **设计原则**：Engine **不直写 MySQL**，告警全部发到 Kafka `mxsec.engine.alert`，由 Consumer 持久化。

详见 [`engine-design.md`](engine-design.md)、[`engine-detection-design.md`](engine-detection-design.md)。

### 6.1 内部 HTTP 管理接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 存活 + 依赖状态（Kafka / Redis / ONNX runtime） |
| GET | `/healthz` | K8s readiness |
| GET | `/metrics` | Prometheus |
| GET | `/internal/v2/engine/info` | Engine 实例信息（version / 加载规则数 / 模型数） |

### 6.2 规则 CRUD

> 规则改动经由 Manager 持久化到 MySQL，Engine 监听 Redis Pub/Sub `engine:rules:changed` 热加载。
> 本节是 Engine 直接暴露的内部 API，给运维 / 调试使用。Manager UI 调 §4.18。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/engine/rules` | 当前加载的规则列表（含 CEL 表达式 / 模式覆盖） |
| GET | `/internal/v2/engine/rules/:id` | 规则详情 |
| POST | `/internal/v2/engine/rules` | 注入临时规则（仅本实例，不入库，重启失效） |
| PUT | `/internal/v2/engine/rules/:id` | 更新临时规则 |
| DELETE | `/internal/v2/engine/rules/:id` | 删除临时规则 |
| POST | `/internal/v2/engine/rules/:id/dry-run` | 规则 dry-run（取历史事件回放） |
| POST | `/internal/v2/engine/rules/reload` | 强制全量重载（紧急运维） |

### 6.3 告警查询（内部）

> 业务侧告警查询走 Manager `/api/v2/alerts`。Engine 直查仅用于诊断与运维。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/engine/alerts/recent` | 最近 1h 告警（内存 ring buffer，不读 DB） |
| GET | `/internal/v2/engine/alerts/stats` | 实时告警速率（per rule / per tenant） |

### 6.4 用户反馈（误报标记）

> 用户在 UI 标记"真威胁 / 误报 / 不确定"时，Manager 接收后写 Kafka `mxsec.engine.feedback`，Engine 消费做 ML 增量训练与规则白名单建议。
> Engine 也直接暴露 HTTP 接口接收反馈（供 SOC SDK 直传，或离线脚本回灌历史标签）。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/internal/v2/engine/feedback` | 单条反馈 |
| POST | `/internal/v2/engine/feedback/batch` | 批量反馈（≤ 1000） |
| GET | `/internal/v2/engine/feedback/stats` | 反馈统计（按 rule_id 聚合） |

请求示例：

```json
{
  "tenant_id": "t-bank-a",
  "alert_id": "alrt-2026060100001",
  "label": "false_positive",
  "comment": "运维例行重启服务，非攻击",
  "reporter": "u-12345",
  "ts": "2026-06-06T08:30:00Z"
}
```

### 6.5 ML 模型管理

详见 [`ml-models.md`](ml-models.md)。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/engine/models` | 已加载 ONNX 模型列表（版本 / 哈希 / 推理延迟） |
| GET | `/internal/v2/engine/models/:id` | 模型详情 + 最近 1h 推理指标 |
| POST | `/internal/v2/engine/models/:id/reload` | 重载模型（运维灰度推送后强制） |
| POST | `/internal/v2/engine/models/:id/disable` | 临时禁用模型 |
| POST | `/internal/v2/engine/models/:id/enable` | 启用模型 |
| GET | `/internal/v2/engine/models/:id/metrics` | 模型 precision / recall / fp_rate |

### 6.6 模式 / 准入查询

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/engine/mode/effective?tenant_id=&host_id=&rule_id=` | 给定 tenant/host/rule 的生效模式（含优先级回溯）|
| GET | `/internal/v2/engine/mode/stats` | Engine 实例统计 observe / protect 告警比例 |

### 6.7 健康 / 自检

```bash
curl -sk https://engine-1.svc.local/health | jq
```

```json
{
  "status": "ok",
  "version": "v2.0.3",
  "loaded_rules": 1247,
  "loaded_models": 10,
  "kafka_consumer_lag_p99_sec": 4,
  "redis_status": "ok",
  "onnx_runtime": "1.17.0"
}
```

---

## 7. VulnSync — 漏洞情报融合

> VulnSync 单副本 + Leader Election，从 11+ 外部源（NVD / OSV / RHSA / USN / DSA / Alpine / SUSE / CISA KEV / ExploitDB / CNNVD / 信创 4 源 / EPSS）抓取漏洞情报，融合仲裁后推 Kafka `mxsec.vuln.advisory`。
> 不对外业务暴露，仅暴露内部状态查询与手动触发。

详见 [`vulnsync-design.md`](vulnsync-design.md)。

### 7.1 内部 HTTP

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 存活 + Leader 状态 |
| GET | `/healthz` | K8s readiness |
| GET | `/metrics` | Prometheus |

### 7.2 数据源状态

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/vulnsync/sources` | 全部数据源状态（最后同步时间 / 失败次数 / 条目数） |
| GET | `/internal/v2/vulnsync/sources/:name` | 单源详情 |
| POST | `/internal/v2/vulnsync/sources/:name/enable` | 启用源 |
| POST | `/internal/v2/vulnsync/sources/:name/disable` | 临时禁用源 |
| PUT | `/internal/v2/vulnsync/sources/:name/config` | 调整源配置（API key / 抓取间隔 / 代理） |

响应示例：

```json
{
  "code": 0,
  "data": {
    "sources": [
      {
        "name": "nvd",
        "enabled": true,
        "last_sync_at": "2026-06-06T07:00:00Z",
        "last_sync_status": "success",
        "items_total": 245678,
        "items_added_24h": 84,
        "items_updated_24h": 312,
        "next_sync_at": "2026-06-06T08:00:00Z"
      },
      {
        "name": "openeuler-csa",
        "enabled": true,
        "last_sync_at": "2026-06-06T07:00:00Z",
        "items_total": 4321
      }
    ]
  }
}
```

### 7.3 手动触发同步

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/internal/v2/vulnsync/sync/trigger` | 触发全量 / 增量同步（异步） |
| POST | `/internal/v2/vulnsync/sync/trigger-source/:name` | 单源触发 |
| GET | `/internal/v2/vulnsync/sync/jobs` | 当前同步任务 |
| GET | `/internal/v2/vulnsync/sync/jobs/:id` | 同步任务详情 |

请求：

```json
{
  "mode": "incremental",
  "since": "2026-06-05T00:00:00Z",
  "force_overwrite": false
}
```

### 7.4 Advisory 查询（内部）

> 业务侧漏洞查询走 Manager `/api/v2/vulnerabilities`，由 Manager 直查 MySQL 持久化结果。VulnSync 仅给运维 / 诊断暴露原始 advisory。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/vulnsync/advisories` | advisory 列表（filter: cve, source, since） |
| GET | `/internal/v2/vulnsync/advisories/:cve` | 多源融合后的单 CVE 详情（含每源原始数据） |
| GET | `/internal/v2/vulnsync/advisories/:cve/raw/:source` | 单源原始数据（诊断用） |
| GET | `/internal/v2/vulnsync/stats` | 整体统计（KEV 数 / 1day exploit 数 / 高危新增） |

---

## 8. LLMProxy — 多 LLM 厂商适配

> LLMProxy 是可选组件（`llm.enabled=false` 时整个 mxsec 不依赖它）。
> 对**内部微服务（Engine / Manager）暴露 gRPC**，对**运维/UI 暴露 HTTP** 用于配置 / 用量查询。
> 所有调用走 LLMProxy，不允许业务代码直接调外部 LLM SDK。

详见 [`llmproxy-design.md`](llmproxy-design.md)。

### 8.1 gRPC 服务（生产路径）

Proto 定义见 `api/proto/llmproxy/llmproxy.proto`。

| RPC | 模式 | 说明 |
|-----|------|------|
| `Complete(CompleteReq) returns (CompleteResp)` | Unary | 一次性补全（告警分析 / 规则起草） |
| `Stream(CompleteReq) returns (stream Chunk)` | Server Stream | 流式补全（前端打字机效果） |
| `Embed(EmbedReq) returns (EmbedResp)` | Unary | 文本 Embedding（异常检测 / 检索） |
| `Count(CountReq) returns (CountResp)` | Unary | Token 计数（前置预估） |
| `Providers(Empty) returns (ProvidersResp)` | Unary | 厂商可用性状态（被 Engine 选路使用） |

#### 8.1.1 Complete 请求 schema

```protobuf
message CompleteReq {
  string tenant_id = 1;
  string scenario  = 2;   // alert_summary / rule_drafting / vuln_advice / soc_chatbot ...
  string model     = 3;   // 可选（默认按 scenario 路由）
  repeated Message messages = 4;
  float  temperature = 5;
  int32  max_tokens  = 6;
  bool   stream      = 7;
  string idempotency_key = 8;
  map<string, string> metadata = 9;  // for audit
}

message Message {
  string role    = 1;     // system / user / assistant / tool
  string content = 2;
}
```

#### 8.1.2 等价 HTTP（仅管理调试，不建议生产用）

```http
POST /internal/v2/llmproxy/complete HTTP/1.1
Authorization: Bearer <internal_token>
Content-Type: application/json
X-Tenant-ID: t-bank-a

{
  "scenario": "alert_summary",
  "messages": [
    {"role": "system", "content": "你是安全分析师..."},
    {"role": "user", "content": "请用三句话总结告警 alrt-2026060100001"}
  ],
  "temperature": 0.2,
  "max_tokens": 512
}
```

响应：

```json
{
  "code": 0,
  "data": {
    "model": "qwen-plus-2025-04",
    "provider": "dashscope",
    "content": "...",
    "usage": {
      "tokens_in": 432,
      "tokens_out": 128,
      "tokens_total": 560,
      "cost_usd": 0.0021
    },
    "cached": false,
    "request_id": "req-llm-2026060601-abc"
  }
}
```

#### 8.1.3 Stream HTTP（SSE）

```http
POST /internal/v2/llmproxy/stream HTTP/1.1
Accept: text/event-stream

{
  "scenario": "soc_chatbot",
  "messages": [...]
}
```

```
event: chunk
data: {"content":"今天 "}

event: chunk
data: {"content":"共 5 起..."}

event: done
data: {"usage": {...}}
```

#### 8.1.4 Embed

```json
{
  "scenario": "anomaly_detect",
  "texts": ["sshd: failed password for root", "..."],
  "model": "bge-m3"
}
```

响应：

```json
{
  "code": 0,
  "data": {
    "model": "bge-m3",
    "provider": "local-ollama",
    "embeddings": [[0.012, -0.034, ...], [...]],
    "dim": 1024,
    "usage": {"tokens_in": 24, "cost_usd": 0.0}
  }
}
```

### 8.2 厂商管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/llmproxy/providers` | 厂商状态（健康 / 黑名单 / 失败率） |
| GET | `/internal/v2/llmproxy/providers/:name` | 单厂商详情 |
| PUT | `/internal/v2/llmproxy/providers/:name/config` | 更新厂商配置（API Key / Endpoint / TLS） |
| POST | `/internal/v2/llmproxy/providers/:name/test` | 测试连通性 |
| POST | `/internal/v2/llmproxy/providers/:name/enable` | 启用 |
| POST | `/internal/v2/llmproxy/providers/:name/disable` | 禁用（进黑名单 5min） |

支持厂商列表：`openai` / `anthropic` / `gemini` / `dashscope` / `deepseek` / `kimi` / `zhipu` / `volc-ark` / `ollama` / `vllm-local`。

### 8.3 租户 LLM 用量与配额

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/llmproxy/usage` | 全部租户用量（仅 SystemAdmin） |
| GET | `/internal/v2/llmproxy/usage/:tenant_id` | 单租户用量（含本月成本与配额） |
| GET | `/internal/v2/llmproxy/usage/:tenant_id/daily` | 单租户日级明细 |
| PUT | `/internal/v2/llmproxy/quota/:tenant_id` | 调整租户配额 |

响应示例：

```json
{
  "code": 0,
  "data": {
    "tenant_id": "t-bank-a",
    "month": "2026-06",
    "quota_usd": 500.0,
    "used_usd": 87.32,
    "remaining_usd": 412.68,
    "tokens_in": 12500000,
    "tokens_out": 4200000,
    "calls_total": 23156,
    "cache_hit_rate": 0.42,
    "by_provider": {
      "dashscope": {"calls": 18000, "cost_usd": 65.0},
      "openai": {"calls": 5000, "cost_usd": 22.32},
      "ollama": {"calls": 156, "cost_usd": 0.0}
    },
    "by_scenario": {
      "alert_summary": {"calls": 18000, "cost_usd": 50.0},
      "rule_drafting": {"calls": 3000, "cost_usd": 25.0},
      "vuln_advice": {"calls": 2156, "cost_usd": 12.32}
    }
  }
}
```

### 8.4 审计查询

每次 LLM 调用入 `mxsec.llm.audit` Topic，Consumer 持久化。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/internal/v2/llmproxy/audit` | LLM 调用审计列表 |
| GET | `/internal/v2/llmproxy/audit/:id` | 调用明细（含 prompt / response 全文，敏感字段已 Sanitize） |

### 8.5 健康

```bash
curl -sk https://llmproxy.svc.local/health | jq
```

```json
{
  "status": "ok",
  "version": "v2.0.3",
  "providers_healthy": 8,
  "providers_blacklisted": 1,
  "cache_hit_rate_1h": 0.41,
  "p95_latency_ms": 820
}
```

---

## 9. Consumer — 仅指标暴露

> Consumer 是纯 Kafka → 存储写入器，**无业务 API**。
> 任何"查告警 / 查事件 / 查资产"的请求都走 Manager，由 Manager 直查 MySQL / ClickHouse。
> Consumer 唯一对外的端点是 K8s 探针与 Prometheus 指标。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 存活 |
| GET | `/healthz` | readiness（Kafka 连接 / DB 连接） |
| GET | `/metrics` | Prometheus（含 consumer lag / 写入速率 / DLQ 计数） |

关键指标：

```
mxsec_consumer_lag{topic, partition}
mxsec_consumer_msg_processed_total{topic, result}   # result=success/dlq
mxsec_consumer_write_duration_seconds{sink, op}     # sink=mysql/ch/redis, op=insert/upsert
mxsec_consumer_dlq_total{topic, reason}
mxsec_consumer_tenant_events_total{tenant, topic}    # 计费用
```

---

## 10. 版本治理与 v1 迁移指南

### 10.1 Sunset 时间表

| 版本 | 发布 | Deprecated | Sunset（移除） |
|------|------|-----------|---------------|
| `/api/v1` | 2024-Q4 | 2026-06-01（v2 GA 起） | **2027-01-01** |
| `/api/v2` | 2026-06-01 | v3 GA 起（不早于 2028-06-01） | v3 Sunset 后 12 月（不早于 2029-06-01） |

> v1 在 Sunset 日之前全程可用，所有响应自动带 `Deprecation` / `Sunset` / `Link` Header。
> 2026-12-01 起，Manager 会对 v1 调用以 1% 概率注入 200ms 延迟（"软引导"）；Sunset 日起返回 410。

### 10.2 接口对照表（v1 → v2 核心变更）

| v1 路径 | v2 路径 | 变更要点 |
|---------|---------|----------|
| `/api/v1/hosts` | `/api/v2/hosts` | 必带 `X-Tenant-ID`；响应新增 `mode` 字段 |
| `/api/v1/alerts/:id` | `/api/v2/alerts/:id` | 新增 `mode` / `would_action` / `action`；attack_chain 改名 `storyline` |
| `/api/v1/alerts/:id/resolve` | `/api/v2/alerts/:id/resolve` | 仅状态字段（无 schema 破坏） |
| `/api/v1/vulnerabilities/sync` | `/api/v2/vulnerabilities/sync` | 内部走 VulnSync gRPC，Manager 仅作代理 |
| `/api/v1/internal/ac/register` | `/internal/v2/ac/register` | 鉴权改 mTLS + 内部 Bearer，路径前缀 `/internal/v2` |
| 无 | `/api/v2/system/mode` | 全新（v1 时代无监听 / 防护模式区分） |
| 无 | `/api/v2/admin/tenants/*` | 全新（v1 无多租户） |
| 无 | `/api/v2/mssp/*` | 全新（v1 无 MSSP） |
| 无 | `/api/v2/alerts/:id/feedback` | 全新（误报反馈通道） |
| 无 | `/internal/v2/llmproxy/*` | 全新（LLMProxy 微服务） |
| 无 | `/internal/v2/vulnsync/*` | 全新（VulnSync 微服务） |
| 无 | `/internal/v2/engine/*` | 全新（Engine 微服务） |

### 10.3 迁移检查清单

- [ ] 客户端调用路径全部替换 `/api/v1/` → `/api/v2/`
- [ ] 登录响应 `roles` 字段为数组（v1 为字符串）
- [ ] 凡涉及主机 / 告警 / 漏洞 / 基线的请求必带 `Authorization`，租户从 JWT 取，不需要手动加 `X-Tenant-ID`
- [ ] 告警列表过滤增加 `mode` 维度可选
- [ ] 自动化脚本（脱敏 / SIEM 对接 / SOAR）切换响应 schema，处理 `mode=observe` 时 `action=null` 的场景
- [ ] CI / SDK 升级（`go.mod` 升 `github.com/imkerbos/mxsec-sdk-go@v2`）
- [ ] 内部互调（AC / Engine / LLMProxy / VulnSync）改 mTLS

### 10.4 Webhook 接收方变更

外发 Webhook（通知 / SIEM 推送）的 payload **从 v1 起新增字段**（向前兼容）：

```json
{
  "version": "v2",
  "tenant_id": "t-bank-a",
  "mode": "observe",
  "would_action": { ... },
  "action": null,
  ...
}
```

老订阅者忽略未知字段即可平滑升级。

---

## 11. 安全注意事项

### 11.1 内部端点必须隔离

- `/internal/v2/*` 路径**严禁**经 Nginx / Ingress 暴露公网
- 部署形态：Service Mesh 内网调用 + mTLS + 内部 Bearer
- 防御措施：Manager / Engine / VulnSync / LLMProxy 启动时绑定 `127.0.0.1:9081/9082/9083/9084`，仅 Sidecar / Envoy 代理走加密通道

### 11.2 token / API Key 存储

| 类型 | 存储位置 | 加密方式 |
|------|----------|----------|
| JWT signing secret | `manager.yaml` 配置项 + envsubst | HSM / KMS（KA 客户） |
| 内部 Bearer Token | `secrets/*.token` | 文件权限 0600 |
| 外部 LLM API Key | `tenant_configs.llm_keys` 表（AES-256-GCM） | tenant-specific DEK，主密钥 KMS |
| Agent 证书 | 自动下发，每 Agent 独立 | mTLS + 30 天滚动续签 |

### 11.3 跨域

仅 `Manager` 暴露 CORS：

```
Access-Control-Allow-Origin: https://soc.example.com
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Authorization, X-Tenant-ID, X-Request-ID, Content-Type
Access-Control-Max-Age: 600
```

其他微服务**禁止 CORS**（仅服务间通信，浏览器不应直连）。

### 11.4 审计与可追溯

所有写操作（POST/PUT/DELETE）入 `audit_logs` 表，保留至少 180 天（KA 默认 6 月，可调）：

```json
{
  "audit_id": "aud-2026060601",
  "tenant_id": "t-bank-a",
  "user_id": "u-12345",
  "action": "alerts:resolve",
  "resource_type": "alert",
  "resource_id": "alrt-2026060100001",
  "ts": "2026-06-06T08:00:00Z",
  "ip": "10.0.1.99",
  "user_agent": "...",
  "request_id": "req-...",
  "diff_before": {...},
  "diff_after": {...}
}
```

---

## 12. 调用示例汇总

### 12.1 登录 + 拉告警 + 反馈

```bash
# 1. 登录
TOKEN=$(curl -s -X POST https://manager.example.com/api/v2/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"********","tenant_slug":"bank-a"}' \
  | jq -r '.data.access_token')

# 2. 列高危告警
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://manager.example.com/api/v2/alerts?severity=high&status=open&page=1" \
  | jq '.data.items[0:3]'

# 3. 反馈误报
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"label":"false_positive","comment":"维护窗口正常重启"}' \
  https://manager.example.com/api/v2/alerts/alrt-2026060100001/feedback
```

### 12.2 触发漏洞扫描

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"scope":"business_line","business_line_id":"bl-payment","priority":"high"}' \
  https://manager.example.com/api/v2/vulnerabilities/scan
```

### 12.3 发起 observe → protect 切换

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d @- https://manager.example.com/api/v2/system/mode/switch-request <<'EOF'
{
  "scope": "tenant",
  "tenant_id": "t-bank-a",
  "target_mode": "protect",
  "rollout": {"stages": [5, 25, 100], "stage_interval_hours": 24, "auto_rollback_threshold": 0.05},
  "reason": "已完成 92 天监听，误报率 0.018，客户签字"
}
EOF
```

### 12.4 SystemAdmin 列租户用量

```bash
ADMIN_TOKEN=$(<admin.token)
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" \
  "https://manager.example.com/api/v2/admin/tenants/t-bank-a/usage?month=2026-06" \
  | jq '.data'
```

### 12.5 Engine 内部 reload 规则（运维场景）

```bash
curl -sk -X POST \
  -H "Authorization: Bearer $ENGINE_INTERNAL_TOKEN" \
  --cert client.crt --key client.key --cacert ca.crt \
  https://engine-1.svc.local/internal/v2/engine/rules/reload
```

### 12.6 LLMProxy 用量监控（运维）

```bash
curl -sk -H "Authorization: Bearer $LLM_ADMIN_TOKEN" \
  --cert client.crt --key client.key --cacert ca.crt \
  https://llmproxy.svc.local/internal/v2/llmproxy/usage/t-bank-a \
  | jq '.data | {month, used_usd, cache_hit_rate}'
```

---

## 13. 参考文档

| 主题 | 文档 |
|------|------|
| 平台架构（六微服务总图） | [`architecture.md`](architecture.md) |
| 运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| Engine 检测细节 | [`engine-detection-design.md`](engine-detection-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| 漏洞模块设计 | [`vuln-module-design.md`](vuln-module-design.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| 资产统一模型 | [`asset-model.md`](asset-model.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| 部署指南 | [`deployment.md`](deployment.md) |
| 配置参考 | [`configuration.md`](configuration.md) |
| 战略路线 | `ref/00-总体评估与商业化路线.md` |
