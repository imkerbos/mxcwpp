# 插件开发指南

> 本文档描述如何开发 Matrix Cloud Security Platform 插件。

---

## 1. 插件概述

Matrix Cloud Security Platform 采用插件化架构，所有功能通过插件实现：

- **Baseline Plugin**: 基线检查插件
- **Collector Plugin**: 资产采集插件
- 未来可扩展更多插件类型

插件作为 Agent 的子进程运行，通过 Pipe + Protobuf 与 Agent 通信。

---

## 2. 插件 SDK

### 2.1 SDK 位置

插件 SDK 位于 `plugins/lib/go/`，提供与 Agent 通信的接口。

### 2.2 核心接口

```go
// Client 是插件与 Agent 通信的客户端
type Client struct {
    // ...
}

// SendRecord 发送数据到 Agent
func (c *Client) SendRecord(record *bridge.Record) error

// ReceiveTask 接收 Agent 下发的任务
func (c *Client) ReceiveTask() (*bridge.Task, error)
```

---

## 3. 开发 Baseline Plugin

### 3.1 项目结构

```
plugins/baseline/
├── main.go              # 插件入口
├── engine/              # 检查引擎
│   ├── engine.go        # 引擎核心
│   ├── checkers.go      # 检查器实现
│   └── models.go        # 数据模型
└── config/              # 策略配置
    └── examples/        # 示例规则
```

### 3.2 插件入口（main.go）

```go
package main

import (
    "context"
    "os"
    
    "github.com/imkerbos/mxsec-platform/plugins/lib/go"
    "github.com/imkerbos/mxsec-platform/api/proto/bridge"
)

func main() {
    // 1. 创建插件客户端
    client, err := plugins.NewClient()
    if err != nil {
        panic(err)
    }
    defer client.Close()
    
    // 2. 主循环：接收任务并处理
    for {
        task, err := client.ReceiveTask()
        if err != nil {
            // 处理错误
            continue
        }
        
        // 3. 执行基线检查
        results := executeBaselineCheck(task)
        
        // 4. 上报结果
        for _, result := range results {
            record := &bridge.Record{
                DataType: 8000, // 基线检查结果
                Data: serializeResult(result),
            }
            client.SendRecord(record)
        }
    }
}
```

### 3.3 检查器实现

检查器实现统一的接口：

```go
type Checker interface {
    Check(ctx context.Context, rule *Rule) (*Result, error)
}
```

**示例：文件键值检查器**

```go
type FileKVChecker struct{}

func (c *FileKVChecker) Check(ctx context.Context, rule *Rule) (*Result, error) {
    // 1. 解析规则参数
    path := rule.Check.Param[0]
    key := rule.Check.Param[1]
    expected := rule.Check.Param[2]
    
    // 2. 读取文件
    content, err := os.ReadFile(path)
    if err != nil {
        return &Result{
            Status: "not_applicable",
            Message: "文件不存在",
        }, nil
    }
    
    // 3. 解析键值
    actual := parseKeyValue(string(content), key)
    
    // 4. 比较结果
    if actual == expected {
        return &Result{
            Status: "pass",
            Actual: actual,
            Expected: expected,
        }, nil
    }
    
    return &Result{
        Status: "fail",
        Actual: actual,
        Expected: expected,
    }, nil
}
```

### 3.4 注册检查器

```go
// 在引擎中注册检查器
engine.RegisterChecker("file_kv", &FileKVChecker{})
engine.RegisterChecker("file_permission", &FilePermissionChecker{})
engine.RegisterChecker("command_exec", &CommandExecChecker{})
// ...
```

---

## 4. 开发 Collector Plugin

### 4.1 项目结构

```
plugins/collector/
├── main.go              # 插件入口
├── engine/              # 采集引擎
│   ├── engine.go        # 引擎核心
│   └── handlers/        # 采集器
│       ├── process.go   # 进程采集
│       ├── port.go      # 端口采集
│       └── user.go      # 用户采集
```

### 4.2 采集器实现

```go
type ProcessHandler struct{}

func (h *ProcessHandler) Collect(ctx context.Context) ([]*Asset, error) {
    // 1. 采集进程信息
    processes := []*Asset{}
    
    // 2. 遍历 /proc 目录
    // ...
    
    // 3. 返回资产数据
    return processes, nil
}
```

### 4.3 上报资产数据

```go
// 上报进程数据
record := &bridge.Record{
    DataType: 5050, // 进程数据类型
    Data: serializeProcesses(processes),
}
client.SendRecord(record)
```

---

## 5. 策略配置格式

### 5.1 策略文件结构

```json
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "version": "1.0.0",
  "description": "SSH 服务安全配置检查",
  "os_family": ["rocky", "centos", "oracle"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_SSH_001",
      "category": "ssh",
      "title": "禁止 root 远程登录",
      "description": "sshd_config 中应设置 PermitRootLogin no",
      "severity": "high",
      "check": {
        "type": "file_kv",
        "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
      },
      "fix": {
        "suggestion": "修改 sshd_config 并重启 sshd"
      }
    }
  ]
}
```

### 5.2 规则字段说明

- `rule_id`: 规则唯一标识
- `category`: 规则类别（ssh、password、file 等）
- `title`: 规则标题
- `description`: 规则描述
- `severity`: 严重级别（low、medium、high、critical）
- `check`: 检查配置
  - `type`: 检查器类型
  - `param`: 检查器参数
- `fix`: 修复建议

---

## 6. 测试插件

### 6.1 单元测试

```go
func TestFileKVChecker(t *testing.T) {
    checker := &FileKVChecker{}
    
    rule := &Rule{
        Check: &Check{
            Type: "file_kv",
            Param: []string{"/etc/ssh/sshd_config", "PermitRootLogin", "no"},
        },
    }
    
    result, err := checker.Check(context.Background(), rule)
    assert.NoError(t, err)
    assert.Equal(t, "pass", result.Status)
}
```

### 6.2 集成测试

```go
func TestBaselinePluginE2E(t *testing.T) {
    // 1. 启动 Agent（测试模式）
    // 2. 启动插件
    // 3. 下发任务
    // 4. 验证结果
}
```

---

## 7. 打包插件

### 7.1 构建插件

```bash
# 构建 Baseline Plugin
go build -ldflags "-s -w" -o baseline ./plugins/baseline

# 构建 Collector Plugin
go build -ldflags "-s -w" -o collector ./plugins/collector
```

### 7.2 签名插件

```bash
# 生成 SHA256 校验和
sha256sum baseline > baseline.sha256

# Server 端验证签名
sha256sum -c baseline.sha256
```

---

## 8. 部署插件

### 8.1 Server 端配置

Server 通过 gRPC 下发插件配置：

```yaml
plugins:
  baseline:
    name: "baseline"
    version: "1.0.0"
    sha256: "abc123..."
    download_url: "http://server:8080/plugins/baseline"
```

### 8.2 Agent 端下载

Agent 自动下载并验证插件：

1. 接收插件配置
2. 下载插件文件
3. 验证 SHA256 签名
4. 启动插件进程

---

## 9. 调试技巧

### 9.1 日志输出

```go
import "go.uber.org/zap"

logger, _ := zap.NewDevelopment()
logger.Info("插件启动", zap.String("plugin", "baseline"))
```

### 9.2 本地测试

```bash
# 直接运行插件（测试模式）
./baseline

# 使用测试策略文件
./baseline -config config/examples/ssh-baseline.json
```

---

## 10. 最佳实践

1. **错误处理**：所有错误都要正确处理，不要 panic
2. **日志记录**：关键操作都要记录日志
3. **资源清理**：及时释放资源（文件句柄、网络连接等）
4. **超时控制**：长时间操作要设置超时
5. **单元测试**：每个检查器都要有单元测试

---

## 11. 参考示例

- Baseline Plugin: `plugins/baseline/`
- Collector Plugin: `plugins/collector/`（待实现）
- 插件 SDK: `plugins/lib/go/`
- 示例规则: `plugins/baseline/config/examples/`

---

---

## 12. 扩展检查器详细指南

### 12.1 检查器接口

所有检查器必须实现 `Checker` 接口：

```go
// Checker 是检查器接口
type Checker interface {
    Check(ctx context.Context, rule *CheckRule) (*CheckResult, error)
}
```

**接口说明**：
- `ctx`: 上下文，用于超时控制和取消操作
- `rule`: 检查规则，包含检查器类型和参数
- 返回值：`CheckResult`（检查结果）和 `error`（错误信息）

### 12.2 CheckRule 结构

```go
type CheckRule struct {
    Type   string   `json:"type"`   // 检查器类型（如 "file_kv"）
    Param  []string `json:"param"`   // 检查器参数数组
    Result string   `json:"result,omitempty"` // 可选：特殊结果处理
}
```

**参数说明**：
- `Type`: 检查器类型标识符，用于在引擎中查找对应的检查器
- `Param`: 字符串数组，包含检查所需的所有参数
- `Result`: 可选字段，用于某些特殊检查器（如 `file_line_match`）

### 12.3 CheckResult 结构

```go
type CheckResult struct {
    Pass     bool   // 是否通过检查
    Actual   string // 实际值（用于显示）
    Expected string // 期望值（用于显示）
}
```

**字段说明**：
- `Pass`: `true` 表示检查通过，`false` 表示未通过
- `Actual`: 实际检查到的值，用于结果展示和调试
- `Expected`: 期望的值，用于结果展示和对比

### 12.4 实现新检查器步骤

#### 步骤 1：定义检查器结构体

```go
// MyCustomChecker 是自定义检查器
type MyCustomChecker struct {
    logger *zap.Logger
}

// NewMyCustomChecker 创建自定义检查器
func NewMyCustomChecker(logger *zap.Logger) *MyCustomChecker {
    return &MyCustomChecker{logger: logger}
}
```

#### 步骤 2：实现 Check 方法

```go
// Check 执行检查
func (c *MyCustomChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
    // 1. 参数验证
    if len(rule.Param) < 2 {
        return nil, fmt.Errorf("my_custom_checker requires 2 parameters: [param1, param2]")
    }
    
    param1 := rule.Param[0]
    param2 := rule.Param[1]
    
    // 2. 执行检查逻辑
    // ... 你的检查代码 ...
    
    // 3. 返回结果
    if checkPassed {
        return &CheckResult{
            Pass:     true,
            Actual:   fmt.Sprintf("实际值: %s", actualValue),
            Expected: fmt.Sprintf("期望值: %s", expectedValue),
        }, nil
    }
    
    return &CheckResult{
        Pass:     false,
        Actual:   fmt.Sprintf("实际值: %s", actualValue),
        Expected: fmt.Sprintf("期望值: %s", expectedValue),
    }, nil
}
```

#### 步骤 3：在引擎中注册检查器

在 `plugins/baseline/engine/engine.go` 的 `NewEngine` 函数中注册：

```go
func NewEngine(logger *zap.Logger) *Engine {
    engine := &Engine{
        logger:   logger,
        checkers: make(map[string]Checker),
    }
    
    // 注册内置检查器
    engine.RegisterChecker("file_kv", NewFileKVChecker(logger))
    // ... 其他检查器 ...
    
    // 注册自定义检查器
    engine.RegisterChecker("my_custom_checker", NewMyCustomChecker(logger))
    
    return engine
}
```

#### 步骤 4：编写单元测试

```go
func TestMyCustomChecker(t *testing.T) {
    logger, _ := zap.NewDevelopment()
    checker := NewMyCustomChecker(logger)
    
    // 测试用例 1：正常通过
    rule := &CheckRule{
        Type:  "my_custom_checker",
        Param: []string{"param1_value", "param2_value"},
    }
    
    result, err := checker.Check(context.Background(), rule)
    assert.NoError(t, err)
    assert.True(t, result.Pass)
    
    // 测试用例 2：参数不足
    rule2 := &CheckRule{
        Type:  "my_custom_checker",
        Param: []string{"param1"},
    }
    
    result2, err2 := checker.Check(context.Background(), rule2)
    assert.Error(t, err2)
    assert.Nil(t, result2)
}
```

### 12.5 检查器实现示例

#### 示例 1：文件所有者检查器

```go
// FileOwnerChecker 检查文件所有者
type FileOwnerChecker struct {
    logger *zap.Logger
}

func NewFileOwnerChecker(logger *zap.Logger) *FileOwnerChecker {
    return &FileOwnerChecker{logger: logger}
}

func (c *FileOwnerChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
    if len(rule.Param) < 2 {
        return nil, fmt.Errorf("file_owner requires 2 parameters: [file_path, expected_owner]")
    }
    
    filePath := rule.Param[0]
    expectedOwner := rule.Param[1]
    
    // 获取文件信息
    info, err := os.Stat(filePath)
    if err != nil {
        return &CheckResult{
            Pass:     false,
            Actual:   fmt.Sprintf("文件不存在: %v", err),
            Expected: fmt.Sprintf("文件所有者应为: %s", expectedOwner),
        }, nil
    }
    
    // 获取文件所有者
    stat := info.Sys().(*syscall.Stat_t)
    actualOwner := strconv.FormatUint(uint64(stat.Uid), 10)
    
    // 比较所有者（简化实现，实际应该解析用户名）
    if actualOwner == expectedOwner {
        return &CheckResult{
            Pass:     true,
            Actual:   fmt.Sprintf("所有者: %s", actualOwner),
            Expected: fmt.Sprintf("所有者: %s", expectedOwner),
        }, nil
    }
    
    return &CheckResult{
        Pass:     false,
        Actual:   fmt.Sprintf("所有者: %s", actualOwner),
        Expected: fmt.Sprintf("所有者: %s", expectedOwner),
    }, nil
}
```

#### 示例 2：软件包安装检查器

```go
// PackageInstalledChecker 检查软件包是否安装
type PackageInstalledChecker struct {
    logger *zap.Logger
}

func NewPackageInstalledChecker(logger *zap.Logger) *PackageInstalledChecker {
    return &PackageInstalledChecker{logger: logger}
}

func (c *PackageInstalledChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error) {
    if len(rule.Param) < 1 {
        return nil, fmt.Errorf("package_installed requires 1 parameter: [package_name]")
    }
    
    packageName := rule.Param[0]
    
    // 检测包管理器类型（简化实现）
    var cmd *exec.Cmd
    if _, err := exec.LookPath("rpm"); err == nil {
        // RPM 系统（CentOS/Rocky/Oracle）
        cmd = exec.CommandContext(ctx, "rpm", "-q", packageName)
    } else if _, err := exec.LookPath("dpkg"); err == nil {
        // DEB 系统（Debian/Ubuntu）
        cmd = exec.CommandContext(ctx, "dpkg", "-l", packageName)
    } else {
        return &CheckResult{
            Pass:     false,
            Actual:   "无法检测包管理器",
            Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
        }, nil
    }
    
    output, err := cmd.Output()
    if err != nil {
        return &CheckResult{
            Pass:     false,
            Actual:   fmt.Sprintf("软件包未安装: %v", err),
            Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
        }, nil
    }
    
    return &CheckResult{
        Pass:     true,
        Actual:   fmt.Sprintf("软件包已安装: %s", strings.TrimSpace(string(output))),
        Expected: fmt.Sprintf("软件包 %s 应已安装", packageName),
    }, nil
}
```

### 12.6 检查器最佳实践

#### 1. 参数验证

始终验证参数数量和格式：

```go
if len(rule.Param) < requiredParams {
    return nil, fmt.Errorf("checker_name requires %d parameters: [param1, param2, ...]", requiredParams)
}
```

#### 2. 错误处理

区分不同类型的错误：

- **参数错误**：返回 `error`，不返回 `CheckResult`
- **检查失败**：返回 `CheckResult{Pass: false}`，不返回 `error`
- **系统错误**：返回 `CheckResult{Pass: false}` 和描述性错误信息

```go
// 参数错误 - 返回 error
if len(rule.Param) < 2 {
    return nil, fmt.Errorf("invalid parameters")
}

// 检查失败 - 返回 CheckResult，Pass=false
if !checkPassed {
    return &CheckResult{
        Pass:     false,
        Actual:   "实际值",
        Expected: "期望值",
    }, nil
}

// 系统错误 - 返回 CheckResult，Pass=false，包含错误信息
if err != nil {
    return &CheckResult{
        Pass:     false,
        Actual:   fmt.Sprintf("检查失败: %v", err),
        Expected: "期望值",
    }, nil
}
```

#### 3. 上下文使用

使用 `ctx` 进行超时控制和取消操作：

```go
// 设置超时
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

// 在命令执行中使用
cmd := exec.CommandContext(ctx, "command", "args")
```

#### 4. 日志记录

记录关键操作和错误：

```go
c.logger.Debug("开始检查", zap.String("file", filePath))
c.logger.Error("检查失败", zap.Error(err))
```

#### 5. 结果信息

提供清晰的 `Actual` 和 `Expected` 信息，便于调试和展示：

```go
return &CheckResult{
    Pass:     true,
    Actual:   fmt.Sprintf("文件权限: %o", filePerm),  // 清晰的实际值
    Expected: fmt.Sprintf("文件权限应 <= %s", expectedPerm), // 清晰的期望值
}, nil
```

### 12.7 在策略中使用新检查器

注册检查器后，可以在策略文件中使用：

```json
{
  "rule_id": "LINUX_CUSTOM_001",
  "category": "custom",
  "title": "自定义检查示例",
  "description": "使用自定义检查器进行检查",
  "severity": "medium",
  "check": {
    "type": "my_custom_checker",
    "param": ["param1_value", "param2_value"]
  },
  "fix": {
    "suggestion": "修复建议"
  }
}
```

---

## 13. 常见问题

### Q: 如何添加新的检查器？

A: 按照以下步骤：

1. 实现 `Checker` 接口
2. 在 `engine.go` 的 `NewEngine` 函数中注册检查器
3. 编写单元测试
4. 在策略文件中使用新检查器

详细步骤请参考 [12. 扩展检查器详细指南](#12-扩展检查器详细指南)。

### Q: 如何处理文件不存在的情况？

A: 返回 `CheckResult`，`Pass=false`，并在 `Actual` 中说明原因：

```go
if err != nil {
    return &CheckResult{
        Pass:     false,
        Actual:   fmt.Sprintf("文件不存在: %v", err),
        Expected: "文件应存在",
    }, nil
}
```

**注意**：不要返回 `error`，因为文件不存在是检查结果的一部分，不是系统错误。

### Q: 如何上报自定义数据类型？

A: 使用 `DataType` 字段：

```go
record := &bridge.Record{
    DataType: 9000, // 自定义数据类型（建议使用 9000+）
    Data: &bridge.Payload{
        Fields: map[string]string{
            "custom_field": "custom_value",
        },
    },
}
client.SendRecord(record)
```

### Q: 检查器执行超时怎么办？

A: 使用 `context` 设置超时：

```go
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

// 在命令执行中使用
cmd := exec.CommandContext(ctx, "command")
```

### Q: 如何支持条件检查（all/any/none）？

A: 条件检查由引擎自动处理，检查器只需实现单个检查逻辑。在策略文件中配置：

```json
{
  "check": {
    "condition": "all",  // all/any/none
    "rules": [
      {"type": "file_kv", "param": [...]},
      {"type": "file_permission", "param": [...]}
    ]
  }
}
```

### Q: 检查器可以访问主机信息吗？

A: 可以。主机信息（OS、版本等）通过任务数据传递，检查器可以通过上下文或全局变量访问。但建议保持检查器无状态，通过参数传递所需信息。

---

## 14. 参考文档

- [Baseline 策略模型设计](../design/baseline-policy-model.md)
- [Agent 架构设计](../design/agent-architecture.md)
- [插件 SDK 文档](../../plugins/lib/go/README.md)
- [检查器实现示例](../../plugins/baseline/engine/checkers.go)
- [检查器单元测试示例](../../plugins/baseline/engine/engine_test.go)
