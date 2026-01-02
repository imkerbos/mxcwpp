# 性能优化说明

本文档说明 Matrix Cloud Security Platform 的性能优化功能。

## 1. SQL 查询优化

### 1.1 数据库索引

为了优化基线得分计算的 SQL 查询性能，我们添加了以下索引：

- `idx_scan_results_host_rule_checked`: 复合索引 `(host_id, rule_id, checked_at DESC)`
  - 用于优化"获取每个规则的最新结果"查询
- `idx_scan_results_host_checked`: 复合索引 `(host_id, checked_at DESC)`
  - 用于优化"获取主机最新检测结果"查询
- `idx_scan_tasks_status_created`: 复合索引 `(status, created_at)`
  - 用于优化"查询待执行任务"查询

### 1.2 索引创建

索引会在数据库迁移时自动创建。如果需要手动创建，可以运行：

```go
import "github.com/imkerbos/mxsec-platform/internal/server/migration"

err := migration.AddPerformanceIndexes(db, logger)
```

### 1.3 SQL 查询优化

基线得分计算查询已优化为使用窗口函数（如果数据库支持）：

```sql
SELECT rule_id, status, severity
FROM (
    SELECT 
        rule_id, 
        status, 
        severity,
        ROW_NUMBER() OVER (PARTITION BY rule_id ORDER BY checked_at DESC) as rn
    FROM scan_results
    WHERE host_id = ?
) AS ranked
WHERE rn = 1
```

如果数据库不支持窗口函数（如 MySQL < 8.0），会自动回退到子查询方式。

## 2. 缓存优化

### 2.1 内存缓存（默认）

使用内存缓存存储主机基线得分，默认 TTL 为 5 分钟。

```go
import "github.com/imkerbos/mxsec-platform/internal/server/manager/biz"

cache := biz.NewBaselineScoreCache(db, logger, 5*time.Minute)
score, err := cache.GetHostScore(hostID)
```

### 2.2 Redis 缓存（可选）

使用 Redis 作为外部缓存，适合多实例部署场景。

#### 2.2.1 实现 Redis 客户端接口

首先需要实现 `RedisClient` 接口：

```go
import "github.com/imkerbos/mxsec-platform/internal/server/manager/biz"

type MyRedisClient struct {
    // 实现 RedisClient 接口
}

func (c *MyRedisClient) Get(ctx context.Context, key string) (string, error) {
    // 实现 Get 方法
}

func (c *MyRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
    // 实现 Set 方法
}

func (c *MyRedisClient) Del(ctx context.Context, keys ...string) error {
    // 实现 Del 方法
}

func (c *MyRedisClient) Exists(ctx context.Context, key string) (bool, error) {
    // 实现 Exists 方法
}
```

#### 2.2.2 使用 Redis 缓存

```go
redisClient := &MyRedisClient{} // 你的 Redis 客户端实现
cache := biz.NewBaselineScoreCacheRedis(db, logger, redisClient, 5*time.Minute)
score, err := cache.GetHostScore(ctx, hostID)
```

#### 2.2.3 缓存键格式

Redis 缓存键格式：`baseline:score:{host_id}`

示例：`baseline:score:host-uuid-123`

## 3. Prometheus 指标导出

### 3.1 启用指标导出

Manager HTTP API Server 默认在 `/metrics` 端点导出 Prometheus 指标。

### 3.2 可用指标

#### Agent 连接指标

- `mxsec_agent_connections_total`: 当前连接的 Agent 数量
  - Labels: `status` (online, offline)

#### 心跳指标

- `mxsec_heartbeat_total`: 心跳总数
  - Labels: `host_id`

#### 基线检查结果指标

- `mxsec_baseline_results_total`: 基线检查结果总数
  - Labels: `host_id`, `status` (pass, fail, error, na), `severity` (low, medium, high, critical)

#### 基线得分指标

- `mxsec_baseline_score`: 主机基线得分（0-100）
  - Labels: `host_id`

#### 任务指标

- `mxsec_tasks_total`: 扫描任务总数
  - Labels: `status` (pending, running, completed, failed)
- `mxsec_task_duration_seconds`: 任务执行时间（秒）
  - Labels: `task_id`, `status`

#### HTTP 请求指标

- `mxsec_http_requests_total`: HTTP 请求总数
  - Labels: `method`, `endpoint`, `status_code`
- `mxsec_http_request_duration_seconds`: HTTP 请求延迟（秒）
  - Labels: `method`, `endpoint`

#### 数据库查询指标

- `mxsec_db_query_duration_seconds`: 数据库查询延迟（秒）
  - Labels: `operation`, `table`

### 3.3 使用示例

#### 3.3.1 记录指标

```go
import "github.com/imkerbos/mxsec-platform/internal/server/metrics"

// 记录心跳
metrics.RecordHeartbeat(hostID)

// 记录基线检查结果
metrics.RecordBaselineResult(hostID, "fail", "high")

// 记录基线得分
metrics.RecordBaselineScore(hostID, 85.0)

// 记录任务
metrics.RecordTask("completed")

// 记录 HTTP 请求
metrics.RecordHTTPRequest("GET", "/api/v1/hosts", "200")
metrics.RecordHTTPRequestDuration("GET", "/api/v1/hosts", 0.123)
```

#### 3.3.2 查询指标

```bash
# 查询所有指标
curl http://localhost:8080/metrics

# 使用 Prometheus 查询
# 查询所有主机的基线得分
mxsec_baseline_score

# 查询失败结果数
sum(mxsec_baseline_results_total{status="fail"})

# 查询平均基线得分
avg(mxsec_baseline_score)
```

### 3.4 Prometheus 配置示例

```yaml
scrape_configs:
  - job_name: 'mxcsec-platform'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

## 4. 性能建议

### 4.1 数据库优化

1. **定期清理旧数据**：定期清理 `scan_results` 表中的旧数据，避免表过大影响查询性能
2. **分区表**：如果数据量很大，考虑使用分区表（按时间分区）
3. **读写分离**：使用主从复制，将查询请求分发到从库

### 4.2 缓存优化

1. **合理设置 TTL**：根据业务需求设置缓存 TTL，平衡数据新鲜度和性能
2. **预热缓存**：系统启动时预热热点数据
3. **缓存穿透防护**：对于不存在的数据，也缓存空结果，避免频繁查询数据库

### 4.3 监控和告警

1. **监控查询延迟**：使用 Prometheus 指标监控数据库查询延迟
2. **监控缓存命中率**：监控缓存命中率，如果命中率低，考虑调整缓存策略
3. **设置告警**：当查询延迟超过阈值或缓存命中率过低时，发送告警

## 5. 端到端测试

### 5.1 运行端到端测试

```bash
# 运行所有端到端测试
go test -tags=e2e ./tests/e2e/...

# 运行特定测试
go test -tags=e2e ./tests/e2e/... -run TestAgentServerPluginE2E
```

### 5.2 测试覆盖

端到端测试覆盖以下场景：

1. **心跳上报**：Agent 连接 Server 并上报心跳
2. **任务下发和执行**：Server 下发任务到 Agent，Agent 执行任务
3. **检测结果上报和存储**：Agent 上报检测结果，Server 存储到数据库
4. **基线得分计算**：验证基线得分计算的正确性

### 5.3 测试环境要求

- Go 1.21+
- SQLite（用于测试数据库）
- 无需外部依赖（Redis、MySQL 等）
