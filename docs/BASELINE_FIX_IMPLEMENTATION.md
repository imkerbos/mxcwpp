# 基线修复功能实现总结

## 已完成部分

### 1. 前端实现 ✅
- **类型定义** (`ui/src/api/types.ts`)
  - `FixTask` - 修复任务
  - `FixResult` - 修复结果
  - `FixableItem` - 可修复项

- **API 封装** (`ui/src/api/fix.ts`)
  - 获取可修复项列表
  - 创建/查询/删除修复任务
  - 获取修复结果
  - **已修复**: API 路径去重问题（移除重复的 `/api/v1` 前缀）

- **UI 页面** (`ui/src/views/Baseline/Fix.vue`)
  - 主机选择（单台/批量）
  - 风险等级筛选
  - 可修复项列表
  - 修复进度展示
  - 修复结果查看

- **路由配置**
  - 路径: `/baseline/fix`
  - 菜单: 基线安全 > 基线修复

### 2. 后端实现 ✅
- **数据模型** (`internal/server/model/fix_task.go`)
  - `FixTask` - 修复任务模型
  - `FixResult` - 修复结果模型

- **API 处理器** (`internal/server/manager/api/fix.go`)
  - `GET /api/v1/fix/fixable-items` - 获取可修复项
  - `POST /api/v1/fix-tasks` - 创建修复任务
  - `GET /api/v1/fix-tasks` - 查询任务列表
  - `GET /api/v1/fix-tasks/:task_id` - 查询任务详情
  - `GET /api/v1/fix-tasks/:task_id/results` - 查询修复结果
  - `POST /api/v1/fix-tasks/:task_id/cancel` - 取消任务
  - `DELETE /api/v1/fix-tasks/:task_id` - 删除任务

- **路由配置** (`internal/server/manager/router/router.go`)
  - 已注册所有修复 API 路由

### 3. 插件实现 ✅
- **修复执行器** (`plugins/baseline/engine/fixer.go`)
  - `Fix()` - 执行单条规则修复
  - `FixBatch()` - 批量执行修复

- **插件扩展** (`plugins/baseline/main.go`)
  - 支持修复任务（data_type = 8002）
  - 上报修复结果（data_type = 8003）
  - 上报任务完成（data_type = 8004）

## 待完成部分

### 全部已完成 ✅

所有核心功能已实现完成：

1. **AgentCenter 任务调度** ✅
   - 已在 `internal/server/agentcenter/service/task.go` 中添加 `DispatchFixTask` 方法
   - 已在 `internal/server/agentcenter/service/task.go` 中添加 `DispatchPendingFixTasks` 方法
   - 已在 `internal/server/agentcenter/scheduler/scheduler.go` 中集成修复任务调度

2. **Transfer 服务结果处理** ✅
   - 已在 `internal/server/agentcenter/transfer/service.go` 中添加 `handleFixResult` 方法（处理 8003）
   - 已在 `internal/server/agentcenter/transfer/service.go` 中添加 `handleFixTaskComplete` 方法（处理 8004）
   - 已在 `handleEncodedRecord` 中注册新的数据类型处理

3. **API 处理器完善** ✅
   - 已更新 `internal/server/manager/api/fix.go` 中的 `CreateFixTask` 方法
   - 修复任务创建后会自动由调度器分发到 Agent

## 实现细节

### 1. 任务调度流程

```
1. 用户通过 UI 创建修复任务 → API 创建 FixTask (status=pending)
2. 调度器每 30 秒检查一次 pending 状态的修复任务
3. 调度器调用 DispatchFixTask 下发任务到 Agent
4. Agent 接收任务并执行修复
5. Agent 上报修复结果（data_type = 8003）
6. Agent 上报任务完成信号（data_type = 8004）
7. Transfer 服务更新任务状态和统计信息
```

### 2. 关键方法说明

#### DispatchFixTask (task.go:639)
- 查询在线主机
- 查询规则信息并按策略组织
- 构建策略数据（包含修复命令）
- 为每个主机下发修复任务（data_type = 8002）
- 更新任务状态为 running

#### DispatchPendingFixTasks (task.go:808)
- 查询所有 pending 状态的修复任务
- 逐个调用 DispatchFixTask 进行分发
- 由调度器定期调用（每 30 秒）

#### handleFixResult (service.go:1812)
- 解析修复结果（data_type = 8003）
- 保存到 fix_results 表
- 更新 fix_tasks 表的统计信息（success_count, failed_count, progress）

#### handleFixTaskComplete (service.go:1920)
- 处理任务完成信号（data_type = 8004）
- 统计已完成的主机数
- 当所有主机都完成时，更新任务状态为 completed

### 3. 数据流向

```
Frontend → API → Database (fix_tasks: pending)
                     ↓
              Scheduler (每 30 秒)
                     ↓
              TaskService.DispatchPendingFixTasks
                     ↓
              TaskService.DispatchFixTask
                     ↓
              TransferService.SendCommand (data_type = 8002)
                     ↓
                  Agent
                     ↓
              Baseline Plugin (执行修复)
                     ↓
              Agent (上报结果: 8003, 8004)
                     ↓
              TransferService.handleFixResult
              TransferService.handleFixTaskComplete
                     ↓
              Database (fix_results, fix_tasks: completed)
```

## 待完成部分（已全部完成）

### 1. AgentCenter 任务调度

需要在 `internal/server/agentcenter/service/task.go` 中添加修复任务调度逻辑：

```go
// DispatchFixTask 下发修复任务到 Agent
func (s *TaskService) DispatchFixTask(fixTask *model.FixTask) error {
    // 1. 查询目标主机
    var hosts []model.Host
    s.db.Where("host_id IN ? AND status = ?", fixTask.HostIDs, model.HostStatusOnline).Find(&hosts)

    // 2. 查询规则信息
    var rules []model.Rule
    s.db.Where("rule_id IN ?", fixTask.RuleIDs).Find(&rules)

    // 3. 构建策略数据
    // ... (类似 baseline 检查任务的逻辑)

    // 4. 为每个主机创建修复任务
    for _, host := range hosts {
        taskData := map[string]interface{}{
            "task_id":     uuid.New().String(),
            "fix_task_id": fixTask.TaskID,
            "policies":    policiesJSON,
            "rule_ids":    fixTask.RuleIDs,
            "os_family":   host.OSFamily,
            "os_version":  host.OSVersion,
        }

        // 5. 下发任务到 Agent
        task := &grpcpb.Task{
            DataType:   8002, // 修复任务
            ObjectName: "baseline",
            Data:       string(taskDataJSON),
            Token:      uuid.New().String(),
        }

        // 通过 Transfer 服务发送任务
        s.transfer.SendTaskToAgent(host.HostID, task)
    }

    // 6. 更新任务状态为 running
    s.db.Model(fixTask).Update("status", model.FixTaskStatusRunning)

    return nil
}
```

### 2. Transfer 服务结果处理

需要在 `internal/server/agentcenter/transfer/service.go` 中添加修复结果处理：

```go
// 在 handleRecord 函数中添加：
case 8003: // 基线修复结果
    return s.handleFixResult(ctx, agentID, record)
case 8004: // 修复任务完成信号
    return s.handleFixTaskComplete(ctx, agentID, record)

// handleFixResult 处理修复结果
func (s *TransferService) handleFixResult(ctx context.Context, agentID string, record *grpcpb.Record) error {
    fields := record.Data.Fields

    // 解析结果
    fixResult := &model.FixResult{
        ResultID:  uuid.New().String(),
        TaskID:    fields["fix_task_id"],
        HostID:    agentID,
        RuleID:    fields["rule_id"],
        Status:    model.FixResultStatus(fields["status"]),
        Command:   fields["command"],
        Output:    fields["output"],
        ErrorMsg:  fields["error_msg"],
        Message:   fields["message"],
        FixedAt:   model.Now(),
    }

    // 保存到数据库
    if err := s.db.Create(fixResult).Error; err != nil {
        return err
    }

    // 更新任务统计
    var task model.FixTask
    if err := s.db.Where("task_id = ?", fixResult.TaskID).First(&task).Error; err == nil {
        if fixResult.Status == model.FixResultStatusSuccess {
            task.SuccessCount++
        } else if fixResult.Status == model.FixResultStatusFailed {
            task.FailedCount++
        }
        task.Progress = int(float64(task.SuccessCount+task.FailedCount) / float64(task.TotalCount) * 100)
        s.db.Save(&task)
    }

    return nil
}

// handleFixTaskComplete 处理修复任务完成信号
func (s *TransferService) handleFixTaskComplete(ctx context.Context, agentID string, record *grpcpb.Record) error {
    fields := record.Data.Fields
    fixTaskID := fields["fix_task_id"]

    // 检查所有主机是否都完成
    var task model.FixTask
    if err := s.db.Where("task_id = ?", fixTaskID).First(&task).Error; err != nil {
        return err
    }

    // 统计已完成的主机数
    var completedHosts int64
    s.db.Model(&model.FixResult{}).
        Where("task_id = ?", fixTaskID).
        Distinct("host_id").
        Count(&completedHosts)

    // 如果所有主机都完成，更新任务状态
    if int(completedHosts) >= len(task.HostIDs) {
        now := model.Now()
        task.Status = model.FixTaskStatusCompleted
        task.CompletedAt = &now
        task.Progress = 100
        s.db.Save(&task)
    }

    return nil
}
```

### 3. API 处理器完善

在 `internal/server/manager/api/fix.go` 的 `CreateFixTask` 函数中，添加任务调度调用：

```go
// CreateFixTask 创建修复任务
func (h *FixHandler) CreateFixTask(c *gin.Context) {
    // ... 现有代码 ...

    // 创建任务
    if err := h.db.Create(task).Error; err != nil {
        h.logger.Error("创建修复任务失败", zap.Error(err))
        InternalError(c, "创建任务失败")
        return
    }

    // 异步执行修复任务
    go func() {
        if err := h.taskService.DispatchFixTask(task); err != nil {
            h.logger.Error("下发修复任务失败",
                zap.String("task_id", taskID),
                zap.Error(err))
            // 更新任务状态为失败
            h.db.Model(task).Updates(map[string]interface{}{
                "status": model.FixTaskStatusFailed,
                "completed_at": model.Now(),
            })
        }
    }()

    // ... 返回响应 ...
}
```

## 数据类型定义

| Data Type | 说明 | 方向 |
|-----------|------|------|
| 8000 | 基线检查任务 | Server → Agent → Plugin |
| 8001 | 基线检查完成信号 | Plugin → Agent → Server |
| 8002 | 基线修复任务 | Server → Agent → Plugin |
| 8003 | 基线修复结果 | Plugin → Agent → Server |
| 8004 | 修复任务完成信号 | Plugin → Agent → Server |

## 测试步骤

### 1. 数据库迁移
```bash
# 启动服务后，数据库会自动创建 fix_tasks 和 fix_results 表
make dev-docker-up
```

### 2. 前端测试
1. 访问 http://localhost:3000/baseline/fix
2. 选择主机和风险等级
3. 查看可修复项列表
4. 选择要修复的项
5. 点击"批量修复"或"立即修复"
6. 查看修复进度和结果

### 3. API 测试
```bash
# 获取可修复项
curl -X GET "http://localhost:8080/api/v1/fix/fixable-items?severities[]=critical&severities[]=high" \
  -H "Authorization: Bearer <token>"

# 创建修复任务
curl -X POST "http://localhost:8080/api/v1/fix-tasks" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "host_ids": ["host-123"],
    "rule_ids": ["LINUX_SSH_001", "LINUX_SSH_002"],
    "severities": ["critical", "high"]
  }'

# 查询任务状态
curl -X GET "http://localhost:8080/api/v1/fix-tasks/<task_id>" \
  -H "Authorization: Bearer <token>"

# 查询修复结果
curl -X GET "http://localhost:8080/api/v1/fix-tasks/<task_id>/results" \
  -H "Authorization: Bearer <token>"
```

## 注意事项

1. **权限控制**: 修复操作需要高权限，建议添加权限检查
2. **安全性**: 修复命令从配置文件加载，不接受用户输入
3. **回滚机制**: 建议在修复前备份关键配置文件
4. **超时控制**: 每个修复命令默认超时 30 秒
5. **错误处理**: 单个规则修复失败不影响其他规则
6. **审计日志**: 所有修复操作都应记录审计日志

## 后续优化

1. **修复预览**: 在执行前显示将要执行的命令
2. **回滚功能**: 支持修复失败后的回滚
3. **定时修复**: 支持定时自动修复
4. **修复报告**: 生成详细的修复报告（PDF/Excel）
5. **修复模板**: 支持自定义修复脚本模板
6. **批量回滚**: 支持批量回滚已执行的修复
