# Go 代码规范

本文档定义了项目中 Go 代码的详细规范和最佳实践。

**最后更新**: 2025-12-29

---

## 1. 项目结构规范

- **遵循 Go 标准项目布局**: `cmd/`, `internal/`, `pkg/` 等目录
- **main.go 保持简洁**: 仅负责启动流程，初始化逻辑提取到 `setup` 包
- **模块隔离**: Agent、AgentCenter、Manager 独立编译，不相互包含

**示例** (`cmd/server/manager/main.go`):
```go
func main() {
    app, err := setup.Initialize()
    if err != nil {
        log.Fatal(err)
    }
    defer app.Cleanup()

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

---

## 2. 命名规范

- **包名**: 小写，无下划线，简短有意义
- **函数名**: 首字母大写（导出），驼峰命名
- **变量名**: 驼峰命名，避免缩写
- **常量名**: PascalCase 或 UPPER_CASE
- **接口名**: 以 `er` 结尾（如 Reader, Writer）

---

## 3. 注释规范

- 每个导出的函数、类型、常量都必须有注释
- 注释以被描述对象的名字开头
- 使用完整的句子，以句号结尾

```go
// Package model 提供数据模型定义
package model

// Host 代表一台受管理的主机
type Host struct {
    ID       string    // 主机唯一标识
    Hostname string    // 主机名
    OSFamily string    // 操作系统族（rocky, centos, debian 等）
}

// GetHost 从数据库查询主机信息
func (h *Host) GetHost(id string) (*Host, error) {
    // 实现
}
```

---

## 4. 错误处理

**正确做法**:
```go
if err != nil {
    logger.Error("数据库查询失败",
        zap.String("host_id", hostID),
        zap.Error(err),
    )
    return err
}
```

**错误做法**:
```go
// ❌ 使用 panic 在业务逻辑中
if err != nil {
    panic(err)
}
```

**错误链式处理**:
```go
return fmt.Errorf("查询主机 %s 失败: %w", hostID, err)
```

---

## 5. 日志规范（必须遵循）

**使用 Zap 结构化日志**，禁止使用 `fmt.Println`、`log.Println` 等。

### 日志级别使用规范

| 级别 | 使用场景 | 示例 |
|------|---------|------|
| `Debug` | 开发调试、详细信息 | 函数参数、中间结果、请求/响应内容 |
| `Info` | 关键业务流程 | 任务开始/完成、连接建立、配置加载 |
| `Warn` | 潜在问题、降级处理 | 配置缺失使用默认值、重试、性能警告 |
| `Error` | 操作失败、需要关注 | 数据库错误、外部服务失败、业务逻辑错误 |

### 正确用法

```go
logger.Info("任务开始执行",
    zap.String("task_id", taskID),
    zap.String("policy_id", policyID),
    zap.Int("host_count", len(hostIDs)),
)

logger.Error("查询主机失败",
    zap.String("host_id", hostID),
    zap.Error(err),
)

logger.Warn("配置不存在，使用默认值",
    zap.String("config_key", key),
    zap.Any("default_value", defaultValue),
)
```

### 必须包含的上下文字段

- 主机相关：`host_id`、`hostname`、`ip`
- 任务相关：`task_id`、`policy_id`
- 告警相关：`alert_id`、`rule_id`、`severity`
- 通知相关：`notification_id`
- 用户相关：`user_id`、`username`

---

## 6. API 请求/响应规范

### 请求体验证

```go
type CreatePolicyRequest struct {
    ID          string    `json:"id" binding:"required"`
    Name        string    `json:"name" binding:"required,min=3,max=100"`
    OSFamily    []string  `json:"os_family"`
    Enabled     bool      `json:"enabled"`
}

// 在处理器中
if err := c.ShouldBindJSON(&req); err != nil {
    BadRequest(c, "请求参数错误: " + err.Error())
    return
}
```

### 响应格式

成功响应:
```json
{
  "code": 0,
  "data": { /* 返回数据 */ }
}
```

错误响应:
```json
{
  "code": 400,
  "message": "详细错误说明"
}
```

### HTTP 状态码规范

| 状态码 | 说明 | 使用场景 |
|--------|------|---------|
| 200 | OK | 成功 |
| 400 | Bad Request | 请求参数错误 |
| 401 | Unauthorized | 未认证 |
| 403 | Forbidden | 无权限 |
| 404 | Not Found | 资源不存在 |
| 409 | Conflict | 资源冲突（如 ID 重复） |
| 500 | Internal Error | 服务器错误 |

---

## 7. 统一响应工具函数（必须使用）

**文件位置**: `internal/server/manager/api/response.go`

所有 HTTP API 返回**必须使用统一的响应工具函数**，禁止直接使用 `c.JSON()`。

### 可用的响应函数列表

| 函数 | HTTP 状态码 | 用途 |
|------|------------|------|
| `Success(c, data)` | 200 | 成功响应，返回数据 |
| `SuccessWithMessage(c, msg, data)` | 200 | 成功响应，带消息和数据 |
| `SuccessMessage(c, msg)` | 200 | 成功响应，仅返回消息 |
| `SuccessPaginated(c, total, items)` | 200 | 成功响应，分页数据 |
| `Created(c, data)` | 201 | 创建成功 |
| `BadRequest(c, msg)` | 400 | 请求参数错误 |
| `Unauthorized(c, msg)` | 401 | 未认证 |
| `Forbidden(c, msg)` | 403 | 无权限 |
| `NotFound(c, msg)` | 404 | 资源不存在 |
| `Conflict(c, msg)` | 409 | 资源冲突（如 ID 重复） |
| `InternalError(c, msg)` | 500 | 服务器内部错误 |

### 正确用法示例

```go
func (h *Handler) GetResource(c *gin.Context) {
    resource, err := h.service.GetResource(id)
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            NotFound(c, "资源不存在")
            return
        }
        h.logger.Error("查询资源失败", zap.String("id", id), zap.Error(err))
        InternalError(c, "查询资源失败")
        return
    }
    Success(c, resource)
}
```

---

## 8. 数据库查询规范

### 使用预加载避免 N+1 问题

```go
// ✅ 正确
var alerts []model.Alert
db.Preload("Host").Preload("Rule").Find(&alerts)

// ❌ 错误 - 循环中查询（N+1 问题）
for _, alert := range alerts {
    var host model.Host
    db.First(&host, "host_id = ?", alert.HostID)
}
```

### 使用事务保证数据一致性

```go
err := db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&policy).Error; err != nil {
        return err
    }
    for _, rule := range rules {
        rule.PolicyID = policy.ID
        if err := tx.Create(&rule).Error; err != nil {
            return err
        }
    }
    return nil
})
```

### 分页查询

```go
var total int64
var items []model.Host
db.Model(&model.Host{}).Count(&total)
db.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
```

---

## 9. 配置管理规范

### 从配置文件读取

```go
// ✅ 正确
dbHost := viper.GetString("database.host")
dbPort := viper.GetInt("database.port")
timeout := viper.GetDuration("server.timeout")
```

### 使用常量定义默认值

```go
const (
    DefaultPageSize    = 20
    DefaultTimeout     = 30 * time.Second
    DefaultMaxRetries  = 3
)
```

### 配置结构体

```go
type ServerConfig struct {
    Host    string        `mapstructure:"host"`
    Port    int           `mapstructure:"port"`
    Timeout time.Duration `mapstructure:"timeout"`
}
```

### 错误做法

```go
// ❌ 硬编码配置
db, _ := gorm.Open(mysql.Open("root:password@tcp(localhost:3306)/mxsec"))
http.ListenAndServe(":8080", router)
```

---

## 10. 单元测试规范

### 测试函数命名

`Test{FunctionName}_{Scenario}_{Expected}`

```go
package api

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestCreatePolicy_ValidRequest_Success(t *testing.T) {
    // Arrange: 准备测试数据
    req := &CreatePolicyRequest{
        ID:   "test-policy",
        Name: "Test Policy",
    }

    // Act: 执行被测试的代码
    policy, err := handler.CreatePolicy(req)

    // Assert: 验证结果
    assert.NoError(t, err)
    assert.NotNil(t, policy)
    assert.Equal(t, "test-policy", policy.ID)
}

func TestCreatePolicy_DuplicateID_Conflict(t *testing.T) {
    // 测试重复 ID 情况
}

func TestCreatePolicy_InvalidRequest_BadRequest(t *testing.T) {
    // 测试无效请求
}
```

### 测试覆盖场景

- ✅ 正常请求
- ✅ 边界值（最小值、最大值）
- ✅ 无效输入（空值、错误类型）
- ✅ 异常情况（DB 错误、超时）

### 测试覆盖率目标

- 总体覆盖率: >= 70%
- 关键路径: >= 85%

---

## API Handler 代码模板

### 获取资源详情

```go
func (h *XxxHandler) GetXxx(c *gin.Context) {
    id := c.Param("id")

    var item model.Xxx
    if err := h.db.First(&item, "id = ?", id).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            NotFound(c, "资源不存在")
            return
        }
        h.logger.Error("查询失败", zap.String("id", id), zap.Error(err))
        InternalError(c, "查询失败")
        return
    }

    Success(c, item)
}
```

### 获取资源列表

```go
func (h *XxxHandler) ListXxx(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

    var total int64
    var items []model.Xxx

    query := h.db.Model(&model.Xxx{})

    if err := query.Count(&total).Error; err != nil {
        h.logger.Error("查询总数失败", zap.Error(err))
        InternalError(c, "查询失败")
        return
    }

    offset := (page - 1) * pageSize
    if err := query.Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
        h.logger.Error("查询列表失败", zap.Error(err))
        InternalError(c, "查询失败")
        return
    }

    SuccessPaginated(c, total, items)
}
```

### 创建资源

```go
func (h *XxxHandler) CreateXxx(c *gin.Context) {
    var req CreateXxxRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        BadRequest(c, "请求参数错误: " + err.Error())
        return
    }

    item := model.Xxx{
        // 字段映射
    }

    if err := h.db.Create(&item).Error; err != nil {
        h.logger.Error("创建失败", zap.Error(err))
        InternalError(c, "创建失败")
        return
    }

    h.logger.Info("资源创建成功", zap.Uint("id", item.ID))
    Created(c, item)
}
```

---

**文档维护者**: Claude Code
**最后更新**: 2025-12-29
