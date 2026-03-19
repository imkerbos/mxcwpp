# Go 代码规范

## 命名规范

| 类型 | 规范 | 示例 |
|------|------|------|
| 包名 | 小写单词 | `manager`, `transfer` |
| 函数/方法 | CamelCase | `GetHostByID`, `handleHeartbeat` |
| 变量 | camelCase | `hostID`, `scanResult` |
| 常量 | CamelCase 或 UPPER_CASE | `MaxRetryCount`, `DefaultTimeout` |
| 接口 | 动词+er 或描述性名词 | `Reader`, `HostService` |

## 日志（必须遵循）

**必须使用 Zap 结构化日志，禁止 `fmt.Println` / `log.Println`。**

```go
// 正确
logger.Info("操作成功", zap.String("host_id", id), zap.Int("count", n))
logger.Error("操作失败", zap.Error(err), zap.String("task_id", taskID))
logger.Warn("使用默认值", zap.Any("default", val))

// 错误 - 禁止
fmt.Println("debug:", err)
log.Printf("host: %s", id)
```

## API 响应（必须遵循）

**必须使用 `internal/server/manager/api/response.go` 中的统一响应函数。**

```go
// 成功
api.Success(c, data)
api.SuccessWithMessage(c, "创建成功", data)

// 错误
api.BadRequest(c, "参数错误: "+err.Error())   // 400
api.NotFound(c, "主机不存在")                  // 404
api.InternalError(c, "查询失败")               // 500

// 禁止直接写响应
c.JSON(200, gin.H{"code": 0, "data": xxx})   // 错误
```

## 错误处理

```go
// 返回错误，不要 panic
result, err := db.Find(&hosts)
if err != nil {
    return fmt.Errorf("查询主机失败: %w", err)
}

// 错误包装提供上下文
if err := s.repo.Create(policy); err != nil {
    return fmt.Errorf("创建策略 %s 失败: %w", policy.Name, err)
}
```

## 数据库

```go
// 使用 Preload 避免 N+1
db.Preload("Rules").Find(&policies)

// 使用事务
db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&policy).Error; err != nil {
        return err
    }
    return tx.Create(&rules).Error
})

// 分页查询
db.Offset(offset).Limit(pageSize).Find(&results)
```

## 配置

从配置文件读取，禁止硬编码。使用 Viper:

```go
// 正确
timeout := viper.GetDuration("server.timeout")

// 错误
timeout := 30 * time.Second  // 硬编码
```

## 测试

- 命名: `TestXxx_描述` (如 `TestGetHost_NotFound`)
- 覆盖率目标: >= 70%（核心路径 >= 85%）
- 使用 table-driven tests

```go
func TestCalculateScore(t *testing.T) {
    tests := []struct {
        name     string
        results  []ScanResult
        expected float64
    }{
        {"全部通过", allPass, 100.0},
        {"部分失败", someFail, 75.0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CalculateScore(tt.results)
            assert.Equal(t, tt.expected, got)
        })
    }
}
```

## API Handler 模板

```go
func (h *HostsHandler) GetHost(c *gin.Context) {
    hostID := c.Param("host_id")
    if hostID == "" {
        api.BadRequest(c, "host_id 不能为空")
        return
    }

    host, err := h.service.GetByID(hostID)
    if err != nil {
        h.logger.Error("查询主机失败", zap.Error(err), zap.String("host_id", hostID))
        api.InternalError(c, "查询主机失败")
        return
    }
    if host == nil {
        api.NotFound(c, "主机不存在")
        return
    }

    api.Success(c, host)
}
```
