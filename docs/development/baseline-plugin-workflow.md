# Baseline Plugin 运行逻辑与工作流程

> 本文档详细说明 Baseline Plugin 的运行逻辑、策略新增流程和检查执行机制。

---

## 1. Baseline Plugin 运行逻辑

### 1.1 插件启动流程

Baseline Plugin 作为 Agent 的子进程运行，通过 Pipe（管道）与 Agent 通信。

**启动流程**：

```go
// plugins/baseline/main.go
func main() {
    // 1. 初始化插件客户端（通过 Pipe 与 Agent 通信）
    client, err := plugins.NewClient()
    
    // 2. 初始化日志
    logger, _ := zap.NewDevelopment()
    
    // 3. 创建检查引擎（注册所有检查器）
    checkEngine := engine.NewEngine(logger)
    
    // 4. 启动任务接收循环（goroutine）
    taskCh := make(chan *bridge.Task, 10)
    go receiveTasks(ctx, client, taskCh, logger)
    
    // 5. 主循环：处理任务
    for {
        select {
        case task := <-taskCh:
            handleTask(ctx, task, checkEngine, client, logger)
        }
    }
}
```

**关键点**：
- 插件通过 `plugins.Client` 与 Agent 通信（使用文件描述符 3/4）
- 检查引擎在启动时注册所有内置检查器（file_kv、file_permission 等）
- 使用 goroutine 异步接收任务，主循环处理任务

---

### 1.2 任务接收流程

**任务接收**：

```go
// receiveTasks 从 Agent 接收任务
func receiveTasks(ctx context.Context, client *plugins.Client, taskCh chan<- *bridge.Task, logger *zap.Logger) {
    for {
        task, err := client.ReceiveTask()  // 从 Pipe 读取任务
        if err != nil {
            // 处理错误
            continue
        }
        taskCh <- task  // 发送到任务通道
    }
}
```

**通信协议**：
- Agent 通过 Pipe（文件描述符 3/4）发送任务
- 任务格式：Protobuf 序列化的 `bridge.Task`
- 插件 SDK 负责反序列化

---

### 1.3 任务处理流程

**任务处理**：

```go
// handleTask 处理任务
func handleTask(ctx context.Context, task *bridge.Task, checkEngine *engine.Engine, client *plugins.Client, logger *zap.Logger) error {
    // 1. 解析任务数据（JSON）
    var taskData map[string]interface{}
    json.Unmarshal([]byte(task.Data), &taskData)
    
    // 2. 根据任务类型处理
    switch task.DataType {
    case 8000:  // 基线检查任务
        return handleBaselineTask(ctx, taskData, checkEngine, client, logger)
    }
}

// handleBaselineTask 处理基线检查任务
func handleBaselineTask(ctx context.Context, taskData map[string]interface{}, checkEngine *engine.Engine, client *plugins.Client, logger *zap.Logger) error {
    // 1. 提取策略配置
    policiesJSON := taskData["policies"].(string)
    var policies []*engine.Policy
    json.Unmarshal([]byte(policiesJSON), &policies)
    
    // 2. 提取主机信息（用于 OS 匹配）
    osFamily := taskData["os_family"].(string)
    osVersion := taskData["os_version"].(string)
    
    // 3. 执行检查
    results := checkEngine.Execute(ctx, policies, osFamily, osVersion)
    
    // 4. 上报结果
    for _, result := range results {
        record := &bridge.Record{
            DataType:  8000,  // 基线检查结果
            Timestamp: time.Now().UnixNano(),
            Data: &bridge.Payload{
                Fields: map[string]string{
                    "rule_id":        result.RuleID,
                    "policy_id":      result.PolicyID,
                    "status":         string(result.Status),
                    "severity":       result.Severity,
                    "category":       result.Category,
                    "title":          result.Title,
                    "actual":         result.Actual,
                    "expected":       result.Expected,
                    "fix_suggestion": result.FixSuggestion,
                    "checked_at":     result.CheckedAt.Format(time.RFC3339),
                },
            },
        }
        client.SendRecord(record)  // 通过 Pipe 发送到 Agent
    }
}
```

**关键点**：
- 任务数据是 JSON 格式，包含策略配置和主机信息
- 检查引擎执行检查并返回结果
- 结果通过 `bridge.Record` 上报到 Agent（Agent 透传到 Server）

---

## 2. 策略新增流程

### 2.1 策略创建（Manager API）

**创建策略**：

```bash
POST /api/v1/policies
Content-Type: application/json

{
  "name": "Rocky Linux 9 基线策略",
  "version": "1.0.0",
  "description": "Rocky Linux 9 操作系统基线检查策略",
  "os_family": ["rocky", "centos"],
  "os_version": ">=9",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_SSH_001",
      "category": "ssh",
      "title": "禁止 root 远程登录",
      "description": "sshd_config 中应设置 PermitRootLogin no",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_kv",
            "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
          }
        ]
      },
      "fix": {
        "suggestion": "修改 /etc/ssh/sshd_config 中的 PermitRootLogin 并重启 sshd"
      }
    }
  ]
}
```

**流程**：
1. 前端调用 Manager API 创建策略
2. Manager 验证策略数据格式
3. Manager 将策略存储到数据库（`policies` 和 `rules` 表）
4. 返回策略 ID

---

### 2.2 任务创建（Manager API）

**创建扫描任务**：

```bash
POST /api/v1/tasks
Content-Type: application/json

{
  "name": "Rocky Linux 9 基线扫描",
  "type": "baseline",
  "targets": {
    "type": "os_family",
    "os_family": ["rocky"]
  },
  "policy_id": "POLICY_001",
  "rule_ids": []  // 空数组表示使用策略中的所有规则
}
```

**流程**：
1. 前端调用 Manager API 创建任务
2. Manager 验证策略是否存在
3. Manager 创建 `ScanTask` 记录（状态为 `pending`）
4. 返回任务 ID

---

### 2.3 任务下发（AgentCenter）

**任务调度**：

```go
// internal/server/agentcenter/service/task.go

// DispatchPendingTasks 分发待执行任务
func (s *TaskService) DispatchPendingTasks(transferService interface {
    SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
    // 1. 查询待执行任务（status = pending）
    var tasks []model.ScanTask
    s.db.Where("status = ?", model.TaskStatusPending).Find(&tasks)
    
    // 2. 处理每个任务
    for _, task := range tasks {
        s.dispatchTask(&task, transferService)
    }
}

// dispatchTask 分发单个任务
func (s *TaskService) dispatchTask(task *model.ScanTask, transferService interface {
    SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
    // 1. 更新任务状态为 running
    s.db.Model(task).Update("status", model.TaskStatusRunning)
    
    // 2. 根据 target_type 匹配主机
    var hosts []model.Host
    switch task.TargetType {
    case model.TargetTypeAll:
        // 查询所有在线主机
        s.db.Where("status = ?", model.HostStatusOnline).Find(&hosts)
    case model.TargetTypeHostIDs:
        // 查询指定主机 ID
        s.db.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&hosts)
    case model.TargetTypeOSFamily:
        // 查询指定 OS 系列的主机
        s.db.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&hosts)
    }
    
    // 3. 查询策略和规则
    policy, _ := policyService.GetPolicy(task.PolicyID)
    rules := policy.Rules  // 或根据 task.RuleIDs 过滤
    
    // 4. 为每个主机下发任务
    for _, host := range hosts {
        s.sendTaskToHost(&host, task, policy, rules, transferService)
    }
}

// sendTaskToHost 向指定主机发送任务
func (s *TaskService) sendTaskToHost(host *model.Host, task *model.ScanTask, policy *model.Policy, rules []model.Rule, transferService interface {
    SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
    // 1. 构建策略配置（转换为 baseline plugin 期望的格式）
    policiesData := s.buildPoliciesData(policy, rules, host.OSFamily, host.OSVersion)
    
    // 2. 构建任务数据（JSON）
    taskData := map[string]interface{}{
        "task_id":    task.TaskID,
        "policy_id":  task.PolicyID,
        "policies":   policiesData,  // JSON 字符串
        "os_family":  host.OSFamily,
        "os_version": host.OSVersion,
    }
    taskDataJSON, _ := json.Marshal(taskData)
    
    // 3. 构建 Task
    grpcTask := &grpcProto.Task{
        DataType:   8000,       // 基线检查任务
        ObjectName: "baseline", // 插件名称
        Data:       string(taskDataJSON),
        Token:      task.TaskID,
    }
    
    // 4. 构建 Command
    cmd := &grpcProto.Command{
        Tasks: []*grpcProto.Task{grpcTask},
    }
    
    // 5. 发送命令到 Agent
    transferService.SendCommand(host.HostID, cmd)
}
```

**关键点**：
- 任务调度器定期（每 30 秒）检查待执行任务
- 根据任务的目标类型匹配主机
- 将策略和规则转换为 baseline plugin 期望的格式
- 通过 gRPC 双向流发送 `Command` 到 Agent

---

### 2.4 Agent 接收和转发任务

**Agent 接收任务**（简化流程）：

```go
// Agent 的 transport 模块接收 Command
func receiveCommands(ctx context.Context, stream proto.Transfer_TransferClient) {
    for {
        cmd, err := stream.Recv()  // 从 gRPC 流接收 Command
        
        // 处理任务
        for _, task := range cmd.Tasks {
            // 根据 object_name 路由到对应插件
            plugin.SendTask(task.ObjectName, task)  // "baseline" -> baseline plugin
        }
    }
}

// Agent 的 plugin 模块发送任务到插件
func SendTask(pluginName string, task *grpcProto.Task) {
    plugin := plugins[pluginName]  // 获取插件实例
    
    // 序列化 Task 为 Protobuf
    taskBytes, _ := proto.Marshal(task)
    
    // 写入 Pipe（文件描述符 4）
    writeLength(plugin.txPipe, len(taskBytes))
    writeData(plugin.txPipe, taskBytes)
}
```

**关键点**：
- Agent 通过 gRPC 双向流接收 `Command`
- 根据 `Task.ObjectName`（"baseline"）路由到对应插件
- 将 `Task` 序列化为 Protobuf 并写入 Pipe

---

## 3. 策略检查执行机制

### 3.1 检查引擎执行流程

**引擎执行**：

```go
// plugins/baseline/engine/engine.go

// Execute 执行基线检查
func (e *Engine) Execute(ctx context.Context, policies []*Policy, osFamily, osVersion string) []*Result {
    var results []*Result
    
    for _, policy := range policies {
        // 1. OS 匹配
        if !policy.MatchOS(osFamily, osVersion) {
            continue  // 跳过不匹配的策略
        }
        
        // 2. 执行规则
        for _, rule := range policy.Rules {
            result := e.executeRule(ctx, policy, rule)
            if result != nil {
                results = append(results, result)
            }
        }
    }
    
    return results
}

// executeRule 执行单条规则
func (e *Engine) executeRule(ctx context.Context, policy *Policy, rule *Rule) *Result {
    result := &Result{
        RuleID:        rule.RuleID,
        PolicyID:      policy.ID,
        Severity:      rule.Severity,
        Category:      rule.Category,
        Title:         rule.Title,
        CheckedAt:     time.Now(),
        Status:        StatusPass,
        FixSuggestion: rule.Fix.Suggestion,
    }
    
    // 执行检查
    checkResult, err := e.executeCheck(ctx, rule.Check)
    if err != nil {
        result.Status = StatusError
        result.Actual = fmt.Sprintf("检查执行失败: %v", err)
        return result
    }
    
    // 根据检查结果设置状态
    if checkResult.Pass {
        result.Status = StatusPass
    } else {
        result.Status = StatusFail
    }
    result.Actual = checkResult.Actual
    result.Expected = checkResult.Expected
    
    return result
}
```

---

### 3.2 检查项执行（条件组合）

**条件组合**：

```go
// executeCheck 执行检查项
func (e *Engine) executeCheck(ctx context.Context, check *Check) (*CheckResult, error) {
    switch check.Condition {
    case "all":
        // 所有子检查都通过才通过
        for _, subCheck := range check.Rules {
            result, err := e.executeSingleCheck(ctx, subCheck)
            if err != nil || !result.Pass {
                return result, err
            }
        }
        return &CheckResult{Pass: true}, nil
        
    case "any":
        // 任一子检查通过即通过
        for _, subCheck := range check.Rules {
            result, err := e.executeSingleCheck(ctx, subCheck)
            if err == nil && result.Pass {
                return result, nil
            }
        }
        return &CheckResult{Pass: false}, nil
        
    case "none":
        // 所有子检查都不通过才通过
        for _, subCheck := range check.Rules {
            result, err := e.executeSingleCheck(ctx, subCheck)
            if err == nil && result.Pass {
                return &CheckResult{Pass: false}, nil
            }
        }
        return &CheckResult{Pass: true}, nil
    }
}

// executeSingleCheck 执行单个检查
func (e *Engine) executeSingleCheck(ctx context.Context, checkRule *CheckRule) (*CheckResult, error) {
    // 1. 根据检查器类型查找检查器
    checker, exists := e.checkers[checkRule.Type]
    if !exists {
        return nil, fmt.Errorf("unknown check type: %s", checkRule.Type)
    }
    
    // 2. 调用检查器的 Check 方法
    return checker.Check(ctx, checkRule)
}
```

**条件组合说明**：
- `all`：所有子检查都通过才通过（AND 逻辑）
- `any`：任一子检查通过即通过（OR 逻辑）
- `none`：所有子检查都不通过才通过（NOT 逻辑）

---

### 3.3 检查器执行

**检查器接口**：

```go
// Checker 是检查器接口
type Checker interface {
    Check(ctx context.Context, rule *CheckRule) (*CheckResult, error)
}

// CheckRule 是检查规则
type CheckRule struct {
    Type   string   `json:"type"`   // 检查器类型（如 "file_kv"）
    Param  []string `json:"param"`   // 检查器参数数组
    Result string   `json:"result,omitempty"` // 可选：特殊结果处理
}

// CheckResult 是检查结果
type CheckResult struct {
    Pass     bool   // 是否通过检查
    Actual   string // 实际值（用于显示）
    Expected string // 期望值（用于显示）
}
```

**示例：file_kv 检查器**：

```go
// FileKVChecker 检查配置文件键值对
func (c *FileKVChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
    // 1. 解析参数
    filePath := rule.Param[0]  // 文件路径
    key := rule.Param[1]        // 键名
    expected := rule.Param[2]   // 期望值
    
    // 2. 读取文件
    file, err := os.Open(filePath)
    if err != nil {
        return &CheckResult{
            Pass:     false,
            Actual:   fmt.Sprintf("文件不存在或无法读取: %v", err),
            Expected: fmt.Sprintf("文件存在且包含 %s=%s", key, expected),
        }, nil
    }
    defer file.Close()
    
    // 3. 解析键值对
    scanner := bufio.NewScanner(file)
    var actualValue string
    found := false
    
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        // 跳过注释行和空行
        if strings.HasPrefix(line, "#") || line == "" {
            continue
        }
        // 解析 Key=Value 格式
        if strings.Contains(line, "=") {
            kvParts := strings.SplitN(line, "=", 2)
            if strings.EqualFold(kvParts[0], key) {
                actualValue = strings.TrimSpace(kvParts[1])
                found = true
                break
            }
        }
    }
    
    // 4. 比较值（支持正则匹配）
    if !found {
        return &CheckResult{
            Pass:     false,
            Actual:   fmt.Sprintf("未找到键: %s", key),
            Expected: fmt.Sprintf("%s=%s", key, expected),
        }, nil
    }
    
    matched, _ := regexp.MatchString(expected, actualValue)
    if !matched {
        matched = strings.EqualFold(actualValue, expected)
    }
    
    return &CheckResult{
        Pass:     matched,
        Actual:   fmt.Sprintf("%s=%s", key, actualValue),
        Expected: fmt.Sprintf("%s=%s", key, expected),
    }, nil
}
```

---

### 3.4 结果上报流程

**结果上报**：

```go
// handleBaselineTask 处理基线检查任务
func handleBaselineTask(...) error {
    // 1. 执行检查
    results := checkEngine.Execute(ctx, policies, osFamily, osVersion)
    
    // 2. 上报结果
    for _, result := range results {
        record := &bridge.Record{
            DataType:  8000,  // 基线检查结果
            Timestamp: time.Now().UnixNano(),
            Data: &bridge.Payload{
                Fields: map[string]string{
                    "rule_id":        result.RuleID,
                    "policy_id":      result.PolicyID,
                    "status":         string(result.Status),  // pass/fail/error/na
                    "severity":       result.Severity,
                    "category":       result.Category,
                    "title":          result.Title,
                    "actual":         result.Actual,
                    "expected":       result.Expected,
                    "fix_suggestion": result.FixSuggestion,
                    "checked_at":     result.CheckedAt.Format(time.RFC3339),
                },
            },
        }
        
        // 3. 通过 Pipe 发送到 Agent
        client.SendRecord(record)
    }
}
```

**数据流**：
1. Plugin → Agent：通过 Pipe 发送 `bridge.Record`
2. Agent → Server：Agent 不解析插件数据，直接透传（添加 Agent ID、IP 等 Header）
3. Server → 数据库：AgentCenter 解析 `EncodedRecord`，存储到 `scan_results` 表

---

## 4. 完整流程图

```
┌─────────────┐
│  Manager    │
│  (HTTP API) │
└──────┬──────┘
       │ 1. 创建策略 (POST /api/v1/policies)
       │ 2. 创建任务 (POST /api/v1/tasks)
       ▼
┌─────────────┐
│  Database   │
│  (MySQL)    │
└──────┬──────┘
       │ 3. 存储策略和任务
       ▼
┌─────────────┐
│ AgentCenter │
│ (gRPC)      │
└──────┬──────┘
       │ 4. 任务调度器查询待执行任务
       │ 5. 匹配主机
       │ 6. 构建 Task 和 Command
       │ 7. 通过 gRPC 双向流发送 Command
       ▼
┌─────────────┐
│   Agent     │
│  (主进程)    │
└──────┬──────┘
       │ 8. 接收 Command
       │ 9. 根据 object_name 路由到插件
       │ 10. 序列化 Task 并写入 Pipe
       ▼
┌─────────────┐
│   Baseline  │
│   Plugin    │
│  (子进程)    │
└──────┬──────┘
       │ 11. 从 Pipe 读取 Task
       │ 12. 解析策略配置
       │ 13. OS 匹配
       │ 14. 执行检查（调用检查器）
       │ 15. 生成结果
       │ 16. 通过 Pipe 发送结果
       ▼
┌─────────────┐
│   Agent     │
│  (主进程)    │
└──────┬──────┘
       │ 17. 接收插件数据（不解析）
       │ 18. 添加 Agent ID、IP 等 Header
       │ 19. 通过 gRPC 双向流发送 PackagedData
       ▼
┌─────────────┐
│ AgentCenter │
│ (gRPC)      │
└──────┬──────┘
       │ 20. 接收 PackagedData
       │ 21. 解析 EncodedRecord
       │ 22. 根据 data_type 路由到处理器
       │ 23. 存储到 scan_results 表
       ▼
┌─────────────┐
│  Database   │
│  (MySQL)    │
└─────────────┘
```

---

## 5. 关键数据结构

### 5.1 策略模型（Policy）

```go
type Policy struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    Description string   `json:"description"`
    OSFamily    []string `json:"os_family"`
    OSVersion   string   `json:"os_version"`
    Enabled     bool     `json:"enabled"`
    Rules       []*Rule  `json:"rules"`
}

type Rule struct {
    RuleID      string   `json:"rule_id"`
    Category    string   `json:"category"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    Severity    string   `json:"severity"`
    Check       *Check   `json:"check"`
    Fix         *Fix    `json:"fix"`
}

type Check struct {
    Condition string       `json:"condition"` // all/any/none
    Rules     []*CheckRule `json:"rules"`
}
```

### 5.2 任务数据（Task Data）

```json
{
  "task_id": "TASK_001",
  "policy_id": "POLICY_001",
  "policies": "[{\"id\":\"POLICY_001\",\"rules\":[...]}]",  // JSON 字符串
  "os_family": "rocky",
  "os_version": "9.3"
}
```

### 5.3 检查结果（Result）

```go
type Result struct {
    RuleID        string
    PolicyID      string
    Status        Status  // pass/fail/error/na
    Severity      string
    Category      string
    Title         string
    Actual        string
    Expected      string
    FixSuggestion string
    CheckedAt     time.Time
}
```

---

## 6. 总结

### 6.1 关键点

1. **插件运行**：
   - 作为 Agent 子进程运行
   - 通过 Pipe（文件描述符 3/4）与 Agent 通信
   - 使用 Protobuf 序列化

2. **策略新增**：
   - Manager API 创建策略和任务
   - AgentCenter 任务调度器定期检查待执行任务
   - 根据目标类型匹配主机并下发任务

3. **检查执行**：
   - 检查引擎执行策略和规则
   - OS 匹配过滤不适用的策略
   - 条件组合（all/any/none）支持复杂检查逻辑
   - 检查器执行具体检查并返回结果

4. **结果上报**：
   - Plugin → Agent：通过 Pipe 发送结果
   - Agent → Server：Agent 透传数据（不解析）
   - Server → Database：存储到 `scan_results` 表

### 6.2 扩展点

- **新增检查器**：实现 `Checker` 接口，在引擎中注册
- **新增规则**：通过 Manager API 创建策略和规则
- **自定义条件组合**：扩展 `executeCheck` 方法支持更多条件逻辑

---

## 参考文档

- [Baseline Plugin 开发计划](./baseline-plugin-plan.md)
- [插件开发指南](./plugin-development.md)
- [Baseline 策略模型设计](../design/baseline-policy-model.md)
- [Agent 架构设计](../design/agent-architecture.md)
