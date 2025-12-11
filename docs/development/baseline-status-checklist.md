# Baseline 流程完善情况检查清单

> 本文档详细列出 Baseline 流程的完成情况和待完善任务。

---

## 1. Baseline Plugin 核心功能 ✅

### 1.1 插件框架 ✅
- [x] 插件入口（main.go）- 已完成
- [x] 插件 SDK 集成（plugins.Client）- 已完成
- [x] Pipe 通信（接收任务、上报结果）- 已完成
- [x] 信号处理和优雅退出 - 已完成

### 1.2 策略处理 ✅
- [x] 策略加载与解析（JSON）- 已完成
- [x] OS 匹配逻辑（MatchOS 方法）- 已完成
  - [x] os_family 匹配
  - [x] os_version 版本约束匹配（>=、<=、==、>、<）

### 1.3 规则执行 ✅
- [x] 规则执行框架（Engine.Execute）- 已完成
- [x] 条件组合（all/any/none）- 已完成
- [x] 检查器注册机制 - 已完成

### 1.4 检查器实现 ✅
- [x] `file_kv` - 已完成
- [x] `file_exists` - 已完成
- [x] `file_permission` - 已完成
- [x] `file_line_match` - 已完成
- [x] `file_owner` - 已完成
- [x] `command_exec` - 已完成
- [x] `sysctl` - 已完成
- [x] `service_status` - 已完成
- [x] `package_installed` - 已完成

### 1.5 结果上报 ✅
- [x] 结果生成（Result）- 已完成
- [x] 结果上报（bridge.Record）- 已完成
- [x] 状态标记（pass/fail/error/na）- 已完成

---

## 2. Server 端策略管理 ⚠️ 部分完成

### 2.1 数据库模型 ✅
- [x] `policies` 表模型 - 已完成
- [x] `rules` 表模型 - 已完成
- [x] `scan_tasks` 表模型 - 已完成
- [x] `scan_results` 表模型 - 已完成

### 2.2 PolicyService（AgentCenter） ⚠️ 部分完成
- [x] `GetPolicy` - 已完成
- [x] `ListPolicies` - 已完成
- [x] `CreatePolicy` - 已完成
- [x] `UpdatePolicy` - 已完成
- [x] `DeletePolicy` - 已完成
- [x] `GetRule` - 已完成
- [x] `ListRules` - 已完成
- [x] `CreateRule` - 已完成
- [x] `UpdateRule` - 已完成
- [x] `DeleteRule` - 已完成
- [x] `GetPoliciesForHost` - 已实现，但**版本约束匹配未完成** ⚠️
  - [x] os_family 匹配（JSON_CONTAINS）
  - [ ] **os_version 版本约束匹配（如 ">=7"）** - **待实现** ❌

### 2.3 PoliciesHandler（Manager API） ✅
- [x] `GET /api/v1/policies` - 已完成
- [x] `GET /api/v1/policies/:policy_id` - 已完成
- [x] `POST /api/v1/policies` - 已完成
- [x] `PUT /api/v1/policies/:policy_id` - 已完成
- [x] `DELETE /api/v1/policies/:policy_id` - 已完成
- [x] `GET /api/v1/policies/:policy_id/statistics` - 已完成

### 2.4 策略匹配逻辑 ⚠️ 待完善
- [x] 根据 os_family 匹配策略 - 已完成
- [ ] **根据 os_version 版本约束匹配策略** - **待实现** ❌
  - 当前实现：`GetPoliciesForHost` 中有 TODO 注释
  - 需要实现：版本约束解析和比较（>=、<=、==、>、<）
  - 参考：`plugins/baseline/engine/models.go` 中的 `matchVersion` 方法

---

## 3. Server 端任务管理 ⚠️ 部分完成

### 3.1 TaskService（AgentCenter） ✅
- [x] `DispatchPendingTasks` - 已完成
- [x] `dispatchTask` - 已完成
- [x] `sendTaskToHost` - 已完成
- [x] `buildPoliciesData` - 已完成（策略格式转换）

### 3.2 TasksHandler（Manager API） ✅
- [x] `POST /api/v1/tasks` - 已完成
- [x] `GET /api/v1/tasks` - 已完成
- [x] `POST /api/v1/tasks/:task_id/run` - 已完成

### 3.3 任务状态管理 ✅
- [x] `TaskStatusUpdater` - 已完成
- [x] 任务状态自动更新机制 - 已完成
  - [x] 每 30 秒检查 running 状态的任务
  - [x] 根据检测结果自动更新任务状态为 completed

### 3.4 任务调度 ⚠️ 待完善
- [x] 任务调度器（scheduler）- 已完成
- [x] 定期检查待执行任务 - 已完成
- [ ] **任务重试机制** - **待完善** ⚠️
  - 当前：有重试机制，但可能需要优化
- [ ] **任务超时处理** - **待实现** ❌
  - 需要：长时间未完成的任务标记为 failed

---

## 4. Agent 端任务转发 ✅

### 4.1 任务接收 ✅
- [x] gRPC 双向流接收 Command - 已完成
- [x] 任务解析和路由 - 已完成

### 4.2 任务转发 ✅
- [x] 根据 object_name 路由到插件 - 已完成
- [x] Pipe 通信（序列化和发送） - 已完成

---

## 5. 数据流完整性 ✅

### 5.1 策略创建流程 ✅
```
Manager API → Database → AgentCenter → Agent → Plugin
```
- [x] 策略创建和存储 - 已完成
- [x] 策略查询和匹配 - 已完成（除版本约束）

### 5.2 任务创建和下发流程 ✅
```
Manager API → Database → AgentCenter Scheduler → Agent → Plugin
```
- [x] 任务创建和存储 - 已完成
- [x] 任务调度和下发 - 已完成
- [x] 任务状态更新 - 已完成

### 5.3 结果上报和存储流程 ✅
```
Plugin → Agent → AgentCenter → Database
```
- [x] 结果上报 - 已完成
- [x] 结果存储 - 已完成
- [x] 结果查询 API - 已完成

---

## 6. 待完善任务清单 ❌

### 6.1 策略匹配逻辑完善（优先级：P0）

#### 任务 1：实现 OS 版本约束匹配
**位置**：`internal/server/agentcenter/service/policy.go::GetPoliciesForHost`

**当前状态**：
```go
// TODO: 实现版本约束匹配（如 ">=7"）
```

**需要实现**：
1. 解析版本约束字符串（>=、<=、==、>、<）
2. 比较主机 OS 版本与策略版本约束
3. 参考 `plugins/baseline/engine/models.go` 中的 `matchVersion` 方法

**预计工作量**：1-2 天

---

#### 任务 2：优化策略匹配性能
**当前问题**：
- `GetPoliciesForHost` 使用 JSON_CONTAINS 查询，可能性能不佳
- 需要优化数据库查询

**需要实现**：
1. 考虑添加索引
2. 优化查询逻辑
3. 添加缓存机制（可选）

**预计工作量**：1 天

---

### 6.2 任务管理完善（优先级：P1）

#### 任务 3：任务超时处理
**当前问题**：
- 长时间未完成的任务没有超时机制
- 可能导致任务一直处于 running 状态

**需要实现**：
1. 定义任务超时时间（如 1 小时）
2. 检查 running 状态的任务是否超时
3. 超时任务标记为 failed

**预计工作量**：1 天

---

#### 任务 4：任务重试机制优化
**当前状态**：
- 有重试机制，但可能需要优化

**需要实现**：
1. 检查重试逻辑是否完善
2. 添加重试次数限制
3. 添加重试间隔配置

**预计工作量**：1 天

---

### 6.3 规则管理完善（优先级：P2）

#### 任务 5：规则批量操作
**当前状态**：
- 支持单个规则的 CRUD
- 不支持批量操作

**需要实现**：
1. 批量创建规则 API
2. 批量更新规则 API
3. 批量删除规则 API

**预计工作量**：2 天

---

#### 任务 6：规则导入导出
**当前状态**：
- 不支持规则导入导出

**需要实现**：
1. 规则导出为 JSON/YAML
2. 规则导入（验证格式）
3. 批量导入规则

**预计工作量**：2-3 天

---

### 6.4 策略管理完善（优先级：P2）

#### 任务 7：策略版本管理
**当前状态**：
- 策略有 version 字段，但没有版本管理机制

**需要实现**：
1. 策略版本历史记录
2. 策略版本回滚
3. 策略版本比较

**预计工作量**：3-4 天

---

#### 任务 8：策略模板
**当前状态**：
- 不支持策略模板

**需要实现**：
1. 策略模板创建
2. 基于模板创建策略
3. 模板管理 API

**预计工作量**：2-3 天

---

## 7. 测试覆盖情况 ⚠️

### 7.1 单元测试 ✅
- [x] Baseline Plugin 检查器单元测试 - 已完成
- [x] 检查器测试覆盖所有场景 - 已完成

### 7.2 集成测试 ⚠️
- [x] 端到端测试（E2E）- 已完成
- [ ] **策略匹配逻辑测试** - **待补充** ❌
- [ ] **任务下发流程测试** - **待补充** ❌
- [ ] **任务状态更新测试** - **待补充** ❌

### 7.3 API 测试 ⚠️
- [x] Manager API 集成测试 - 已完成
- [ ] **策略 API 边界测试** - **待补充** ❌
- [ ] **任务 API 边界测试** - **待补充** ❌

---

## 8. 文档完善情况 ✅

### 8.1 开发文档 ✅
- [x] Baseline Plugin 开发计划 - 已完成
- [x] Baseline Plugin 工作流程 - 已完成
- [x] 插件开发指南 - 已完成

### 8.2 API 文档 ⚠️
- [x] Server API 设计文档 - 已完成
- [ ] **API 使用示例** - **待补充** ⚠️
- [ ] **错误码说明** - **待补充** ⚠️

---

## 9. 总结

### 9.1 已完成功能 ✅
1. **Baseline Plugin 核心功能**：100% 完成
2. **策略管理基础功能**：90% 完成（缺少版本约束匹配）
3. **任务管理基础功能**：90% 完成（缺少超时处理）
4. **数据流完整性**：100% 完成

### 9.2 待完善功能 ❌
1. **P0（必须）**：
   - OS 版本约束匹配（策略匹配逻辑）
   
2. **P1（重要）**：
   - 任务超时处理
   - 任务重试机制优化

3. **P2（可选）**：
   - 规则批量操作
   - 规则导入导出
   - 策略版本管理
   - 策略模板

### 9.3 下一步行动建议

**立即开始**：
1. 实现 OS 版本约束匹配（`GetPoliciesForHost`）
2. 实现任务超时处理

**后续完善**：
1. 补充集成测试
2. 完善 API 文档
3. 实现规则批量操作

---

## 10. 代码位置参考

### 10.1 策略匹配逻辑
- **位置**：`internal/server/agentcenter/service/policy.go::GetPoliciesForHost`
- **问题**：第 158 行有 TODO 注释
- **参考**：`plugins/baseline/engine/models.go::matchVersion`

### 10.2 任务状态更新
- **位置**：`internal/server/agentcenter/service/task_status.go`
- **状态**：已实现，但可能需要优化

### 10.3 任务调度
- **位置**：`internal/server/agentcenter/scheduler/scheduler.go`
- **状态**：已实现

### 10.4 策略格式转换
- **位置**：`internal/server/agentcenter/service/task.go::buildPoliciesData`
- **状态**：已实现

---

## 11. 相关文档

- [Baseline Plugin 开发计划](./baseline-plugin-plan.md)
- [Baseline Plugin 工作流程](./baseline-plugin-workflow.md)
- [Baseline 策略模型设计](../design/baseline-policy-model.md)
- [TODO 列表](../TODO.md)
