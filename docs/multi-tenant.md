# 多租户设计

> **强约束**：mxsec 是 **from-day-1 多租户**平台。所有业务实体（主机、告警、漏洞、基线、规则、任务、报告、用户、配置）必带 `tenant_id`，单 deploy 支持 **N 个隔离租户**。
>
> 这是工业级开源 CWPP 与"作坊式部署"的核心分水岭。MSSP / 行业云 / 集团子公司 / 银行多账户 全部依赖此能力。

---

## 1. 设计目标

| 目标 | 实现 |
|------|------|
| **行级硬隔离** | 所有业务表 `tenant_id` 列 + 全局中间件强制注入 + 默认 `NOT EXISTS` 防穿越 |
| **逻辑隔离 + 物理隔离可选** | 默认共库共表行隔离；可选独立 schema / 独立 DB 实例（KA 客户） |
| **租户级配置** | mode、ML 开关、LLM 厂商、token 上限、保留期、告警规则全部租户级可覆盖 |
| **计费 + 用量** | 按租户统计 Agent 数、事件量、告警数、LLM token、API 调用 |
| **MSSP 父子租户** | 父租户 read-only 看子租户聚合；子租户独立运营 |
| **租户管理员独立** | 每个租户独立超管 + RBAC，互不干涉 |

---

## 2. 租户模型

### 2.1 实体关系

```
SystemAdmin (平台超管)
   │
   ├─→ Tenant t-bank-a (银行 A)
   │     ├── TenantAdmin (租户超管)
   │     ├── User / Role / Group
   │     ├── Host (N)
   │     ├── Alert / Vuln / Baseline / ...
   │     └── Config (mode / ML / LLM / Quota)
   │
   ├─→ Tenant t-bank-b (银行 B，独立)
   │
   └─→ Tenant t-mssp-group-x (MSSP 集团父)
         ├── TenantChild t-mssp-x-sub1 (子)
         ├── TenantChild t-mssp-x-sub2 (子)
         └── 父租户聚合视图（read-only）
```

### 2.2 租户类型

| 类型 | 标识 | 用途 |
|------|------|------|
| `standalone` | 独立租户 | 普通客户，互不知晓 |
| `mssp_parent` | MSSP 父租户 | 集团 / 服务商，可聚合查看子租户 |
| `mssp_child` | MSSP 子租户 | 子公司 / 客户实体，独立运营 |
| `internal` | 内部租户 | mxsec 团队自用 / 测试用 |

### 2.3 核心表

```sql
CREATE TABLE tenants (
    id              VARCHAR(64)  PRIMARY KEY,         -- t-{slug}
    name            VARCHAR(255) NOT NULL,
    type            ENUM('standalone','mssp_parent','mssp_child','internal') NOT NULL,
    parent_id       VARCHAR(64)  NULL,                 -- mssp_child 才填
    status          ENUM('active','suspended','deleted') NOT NULL DEFAULT 'active',

    -- 模式默认（被全局 / 主机 / 规则级覆盖，见 operating-modes.md）
    default_mode    ENUM('observe','protect') NOT NULL DEFAULT 'observe',

    -- 智能分析开关
    ml_enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    llm_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    llm_provider    JSON,                              -- {primary:"openai", fallback:["claude","qwen"]}

    -- 配额
    quota_agents    INT      NOT NULL DEFAULT 100,
    quota_llm_usd   DECIMAL(10,2) DEFAULT 100.00,      -- 月度 LLM 成本上限
    quota_events_per_day BIGINT DEFAULT 1000000000,    -- 事件量软限

    -- 数据保留
    retention_alerts_days   INT DEFAULT 90,
    retention_events_days   INT DEFAULT 30,
    retention_audit_days    INT DEFAULT 180,

    -- 物理隔离选项（KA 客户）
    isolation_strategy  ENUM('shared','schema','db') DEFAULT 'shared',
    isolated_db_dsn     VARCHAR(512),                  -- isolation_strategy=db 时填

    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,

    INDEX idx_type   (type),
    INDEX idx_parent (parent_id)
);
```

---

## 3. 数据库行级隔离

### 3.1 所有业务表必带 `tenant_id`

```sql
-- 主机表示例
ALTER TABLE hosts
    ADD COLUMN tenant_id VARCHAR(64) NOT NULL AFTER id,
    ADD INDEX idx_tenant (tenant_id),
    ADD INDEX idx_tenant_status (tenant_id, status);

-- 告警表示例
ALTER TABLE alerts
    ADD COLUMN tenant_id VARCHAR(64) NOT NULL AFTER id,
    ADD INDEX idx_tenant_time (tenant_id, detected_at);
```

### 3.2 所有索引前缀必须是 `tenant_id`

- 单租户查询走 `tenant_id` 前缀索引，避免跨租户扫描
- 大表分区（如告警表）按 `tenant_id` 哈希分区，减少锁争用

### 3.3 GORM 强制注入（中间件层）

```go
// internal/server/common/tenant/scope.go
type TenantScope struct {
    TenantID string
}

func (ts TenantScope) Apply(db *gorm.DB) *gorm.DB {
    if ts.TenantID == "" {
        panic("tenant_id missing — refuse to query")
    }
    return db.Where("tenant_id = ?", ts.TenantID)
}

// 使用：
db.Scopes(tenant.FromContext(ctx).Apply).Find(&hosts)
```

**禁止裸 `db.Find` / `db.Where` 跳过 TenantScope**。代码评审 + lint 规则强制。

### 3.4 跨租户查询白名单

仅 4 类场景允许 `SystemAdmin` 跨租户：

1. 平台运维统计（总主机数 / 总租户数 / 总告警数）
2. 计费汇总
3. MSSP 父租户聚合查看子租户（仅父租户 read-only）
4. 平台健康检查 / 容量监控

跨租户 API 走 **独立 `/api/v2/admin/*` 前缀** + 平台超管 RBAC，普通租户 token 无权调用。

---

## 4. API 层鉴权三段

每个请求经过 **3 层校验**，缺一层即 401/403：

```
[1] JWT 校验（合法签名 + 未过期）
    │
    ↓
[2] Tenant 校验
    ├── JWT claims 中提取 tenant_id
    ├── 查 tenants 表，确认 status=active
    └── 注入 context.WithValue(ctx, "tenant_id", tid)
    │
    ↓
[3] RBAC 校验
    ├── 查 user_roles 表，确认 user 在此 tenant 内的角色
    └── 检查角色对资源 + 动作的权限（基于 Casbin）
    │
    ↓
执行业务逻辑（所有 DB 查询自动 Apply TenantScope）
```

### 4.1 JWT claims 结构

```json
{
  "sub": "u-12345",
  "tenant_id": "t-bank-a",
  "tenant_type": "standalone",
  "roles": ["soc-analyst", "vuln-operator"],
  "exp": 1730000000,
  "iat": 1729996400,
  "iss": "mxsec-manager",
  "aud": "mxsec-platform"
}
```

### 4.2 跨租户 token

`SystemAdmin` 的 JWT 带 `tenant_id: "*"` + `is_platform_admin: true`，仅 `/api/v2/admin/*` 路径放行。

### 4.3 API URL 规范

```
# 租户作用域（必须带 X-Tenant-ID header 或 JWT claim）
GET  /api/v2/hosts
GET  /api/v2/alerts
POST /api/v2/baseline/scan

# 平台作用域（仅 SystemAdmin）
GET  /api/v2/admin/tenants
POST /api/v2/admin/tenants
GET  /api/v2/admin/system/health

# MSSP 父租户聚合（仅 mssp_parent，read-only）
GET  /api/v2/mssp/children
GET  /api/v2/mssp/aggregate/alerts
```

---

## 5. Kafka 多租户

### 5.1 默认共享 Topic + Key 含租户

```
mxsec.agent.ebpf
   ├── Key = "{tenant_id}:{agent_id}"
   └── Message body 含 tenant_id 字段（冗余便于排查）
```

Consumer / Engine 按 `tenant_id` 路由到对应 MySQL / CK 表（或同表行隔离）。

### 5.2 KA 客户独立 Topic（可选）

```
mxsec.{tenant_id}.agent.ebpf
mxsec.{tenant_id}.agent.events
...
```

适用：

- KA 客户要求物理隔离（"我的数据绝不可能出现在别人的 partition"）
- 数据量极大（单租户 > 100k EPS，独占带宽）
- 合规要求（金融行业部分客户）

启用通过 `isolation_strategy = schema | db` 自动派生 topic 名。

### 5.3 ConsumerGroup 拓扑

```
ConsumerGroup: mxsec-writers
  ├── 订阅 mxsec.agent.*（共享 topic）
  └── 写入时按 tenant_id 路由表 / 库

ConsumerGroup: mxsec-engine
  ├── 订阅 mxsec.agent.*
  └── 检测时按 tenant_id 加载规则 / 模型 / 配置
```

---

## 6. 物理隔离选项

### 6.1 三档策略

| 策略 | tenants.isolation_strategy | DB | Kafka | 适用 |
|------|---------------------------|-----|-------|------|
| Shared（默认） | `shared` | 共库共表 + tenant_id | 共享 Topic + Key | 中小客户 / 互联网 |
| Schema | `schema` | 同实例独立 schema（如 `mxsec_t_bank_a.hosts`） | 独立 Topic | 中型政企 |
| Dedicated DB | `db` | 独立 MySQL / CK 实例 | 独立 Topic + 独立 Kafka 集群可选 | 金融 KA / 监管要求 |

### 6.2 策略迁移路径

`shared → schema → db` 单向迁移，提供 `mxctl tenant migrate` CLI 工具。

迁移流程：

1. 创建目标 schema / DB
2. 暂停租户写入（API 返回 503）
3. dump + restore（按 `tenant_id` 过滤）
4. 切换 Manager 配置（路由表更新）
5. 恢复租户写入
6. 验证数据一致性
7. 清理源 schema（保留 30 天备份）

---

## 7. 租户级配置覆盖

### 7.1 覆盖矩阵

| 配置项 | 全局默认 | 租户覆盖 | 主机标签覆盖 | 规则覆盖 |
|--------|----------|----------|--------------|----------|
| `mode` | ✅ | ✅ | ✅ | ✅ |
| `ml.enabled` | ✅ | ✅ | - | - |
| `ml.models[*].enabled` | ✅ | ✅ | - | - |
| `llm.enabled` | ✅ | ✅ | - | - |
| `llm.provider` | ✅ | ✅ | - | - |
| `llm.monthly_quota_usd` | ✅ | ✅ | - | - |
| `retention.alerts_days` | ✅ | ✅ | - | - |
| `notification.channels` | ✅ | ✅ | - | - |
| `baseline.policies[*]` | ✅ | ✅ | ✅ | - |
| `engine.rules[*]` | ✅ | ✅ | ✅ | ✅ |

### 7.2 配置存储

- 全局：`/etc/mxsec/manager.yaml`
- 租户：`tenant_configs` 表（GORM model）
- 主机：`hosts.labels` JSON 列 + Engine 启动加载到内存
- 规则：`engine_rules` 表的 `mode_override`、`tenant_filter` 字段

### 7.3 优先级

`规则级 > 主机标签级 > 租户级 > 全局默认`（详见 `operating-modes.md` §4）。

---

## 8. 计费与用量

### 8.1 计量维度

| 维度 | 表 | 频次 |
|------|-----|------|
| Agent 数 | `tenant_metrics_daily` | 每日 0:00 快照 |
| 事件量（events） | `tenant_metrics_daily` | 每日累加（Consumer 计数） |
| 告警量 | 同上 | 每日累加 |
| LLM token in/out | `tenant_metrics_daily` + `tenant_llm_usage` | 每次调用记录 + 每日聚合 |
| API 调用量 | `tenant_metrics_daily` | Prometheus → 每日 ETL |

### 8.2 用量上报

`mxsec.metering.usage` Topic（专用）：

```json
{
  "tenant_id": "t-bank-a",
  "date": "2026-06-01",
  "agents_active": 1234,
  "agents_max": 1245,
  "events_in_total": 87654321,
  "events_in_by_type": {"ebpf": 60000000, "fim": 12000000, "baseline": 5000000, ...},
  "alerts_total": 4321,
  "llm_calls": 250,
  "llm_tokens_in": 125000,
  "llm_tokens_out": 50000,
  "llm_cost_usd": 0.85,
  "api_calls_total": 152340
}
```

### 8.3 计费模型示例

- 按 Agent 数月费：¥X / Agent / 月
- 按 LLM 实际成本透传（mxsec 不加价）
- 按事件量阶梯（≤ 10亿/日 含基础包，> 10 亿按 GB 计）
- KA 包年包月（按规模档定价）

详细计费引擎见 `ref/08-roadmap.md` M2 阶段。

---

## 9. MSSP 父子租户

### 9.1 父租户能力

- 创建 / 暂停 / 删除子租户
- read-only 看子租户的告警聚合、漏洞汇总、合规状态
- 跨子租户报表（"所有子租户的等保合规率"）
- 不可修改子租户内部数据 / 配置

### 9.2 子租户能力

- 完全独立运营
- 不知晓父租户存在（隐藏）
- 自有 TenantAdmin / RBAC / Quota

### 9.3 API 示例

```
GET /api/v2/mssp/children            # 父租户列出所有子
GET /api/v2/mssp/aggregate/alerts    # 跨子聚合告警
GET /api/v2/mssp/aggregate/compliance # 跨子合规率
```

---

## 10. 实现路径（Phase 1）

### 10.1 Phase 1 Sprint 0（4 周）

- [ ] 设计 + 评审租户模型
- [ ] 建 `tenants` / `user_roles` / `tenant_configs` 表
- [ ] 实现 `TenantScope` + GORM 中间件
- [ ] 实现 JWT 改造（claims 加 tenant_id）+ 3 段鉴权中间件
- [ ] 所有业务 model 批量加 `tenant_id` 列（Migration）
- [ ] Casbin RBAC 模型升级（支持 tenant scope）

### 10.2 Phase 1 后续（4 周）

- [ ] Kafka Key 改造（按 `{tenant}:{agent}`）
- [ ] Consumer / Engine 按 tenant 路由
- [ ] UI 多租户切换器（顶部 selector）
- [ ] 平台超管 `/api/v2/admin/*` API
- [ ] mxctl 租户管理子命令

### 10.3 Phase 2-3 增量

- Phase 2：MSSP 父子租户
- Phase 2：物理隔离 `schema` / `db` 模式
- Phase 3：计费引擎 + 用量上报
- Phase 3：租户级 SLO 监控

详见 `ref/08-roadmap.md`。

---

## 11. 测试矩阵

| 测试 | 目标 |
|------|------|
| 跨租户穿越 | 模拟租户 A token 查询租户 B 数据 → 必须 404 / 0 行 |
| 中间件 fail-safe | 删除 `TenantScope.Apply` 调用 → 单元测试 panic |
| 索引前缀 | 慢查询日志检查 `WHERE tenant_id = ?` 走索引 |
| Kafka 路由 | 1000 租户并发上报 → Consumer 按 tenant 写入正确表 |
| MSSP 边界 | 子租户 token 调 `/api/v2/mssp/*` → 403 |
| Quota | 超过 `quota_agents` → Agent 注册返回 429 |

---

## 12. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 漏加 `tenant_id` 字段 | CI 强制检查 model 必含 `tenant_id` + lint 规则 |
| 跨租户穿越 | 自动化测试 + 代码评审 + Casbin 二次校验 |
| 索引膨胀 | 大表（告警 / 事件）按 `tenant_id` 哈希分区 |
| 单租户挤兑 | Quota + 速率限制 + 单独 ConsumerGroup（KA） |
| MSSP 父租户误删子 | 删除走"软删除 + 30 天回收站" |
| 计量数据丢失 | 双写 Prometheus + MySQL + 每月对账 |

---

## 13. 参考文档

- [`architecture.md`](architecture.md) — 整体架构
- [`operating-modes.md`](operating-modes.md) — 租户级 mode 覆盖
- [`api-reference.md`](api-reference.md) — `/api/v2` 全部 API
- [`llmproxy-design.md`](llmproxy-design.md) — 租户级 LLM 配额
- [`deployment.md`](deployment.md) — 物理隔离部署形态
- [`ml-models.md`](ml-models.md) — 租户级 ML 配置
- [`asset-model.md`](asset-model.md) — 资产模型必带 tenant
- `ref/01-服务端架构.md` §5 — 多租户改造方案原始评估
