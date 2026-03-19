# 插件开发指南

## 概述

插件以独立子进程运行，通过 OS Pipe + Protobuf 与 Agent 通信。所有插件使用统一 SDK。

| 插件 | 职责 | DataType |
|------|------|----------|
| baseline | 基线合规检查 | 8000 |
| collector | 资产采集（进程/端口/账户等） | 5050-5064 |
| fim | 文件完整性监控 | 6001-6002 |

## 插件 SDK

核心文件: `plugins/lib/go/client.go`

```go
// 创建客户端
client := client.NewClient()

// 接收任务（阻塞）
task := client.ReceiveTask()

// 发送检测结果
client.SendRecord(&client.Record{
    DataType: 8000,
    Body: resultJSON,
})
```

## 生命周期

```
Agent 启动
  → 读取 Server 下发的 Plugin Config 列表
  → exec.Command 启动插件子进程
  → 通过 os.Pipe (FD 3/4) 建立 Pipe 通道
  → 插件循环: ReceiveTask() → 处理 → SendRecord()
  → Agent 透传到 AgentCenter
```

## Baseline 插件结构

```
plugins/baseline/
├── main.go              # 入口: 初始化 → 循环接收任务
├── engine/
│   └── engine.go        # 检查引擎: 执行策略 → 逐条检查规则
└── checkers/
    ├── registry.go      # 检查器注册表
    ├── file_content.go  # file_content 检查器
    ├── file_kv.go       # file_kv 检查器
    ├── command.go       # command_output 检查器
    └── ...              # 其他检查器
```

## Checker 接口

```go
type Checker interface {
    Type() string
    Check(rule CheckRule) CheckResult
}

type CheckRule struct {
    CheckType  string                 // 检查器类型
    Target     string                 // 目标（文件路径/命令等）
    Params     map[string]interface{} // 检查参数
}

type CheckResult struct {
    Status   string // pass / fail / error / na
    Actual   string // 实际值
    Expected string // 期望值
    Message  string // 说明
}
```

## 内置检查器

| 类型 | 说明 | 典型用途 |
|------|------|----------|
| file_content | 检查文件内容是否包含/匹配指定模式 | SSH 配置检查 |
| file_kv | 检查 key=value 配置文件的值 | sshd_config, login.defs |
| file_permission | 检查文件权限 | /etc/shadow 权限 |
| file_owner | 检查文件属主/属组 | 关键文件属主 |
| file_exists | 检查文件是否存在 | 安全配置文件存在性 |
| command_output | 执行命令并检查输出 | 系统配置检查 |
| service_status | 检查 systemd 服务状态 | 防火墙/审计服务 |
| sysctl_check | 检查内核参数 | 网络安全参数 |
| package_installed | 检查软件包是否安装 | 安全工具安装 |

## 添加新检查器

### 第 1 步: 实现接口

```go
// checkers/my_checker.go
package checkers

type MyChecker struct{}

func (c *MyChecker) Type() string {
    return "my_check"
}

func (c *MyChecker) Check(rule CheckRule) CheckResult {
    // 实现检查逻辑
    actual := doCheck(rule.Target, rule.Params)
    expected := rule.Params["expected"].(string)

    if actual == expected {
        return CheckResult{Status: "pass", Actual: actual, Expected: expected}
    }
    return CheckResult{Status: "fail", Actual: actual, Expected: expected}
}
```

### 第 2 步: 注册

```go
// checkers/registry.go
func init() {
    Register(&MyChecker{})
}
```

### 第 3 步: 编写规则

在策略 JSON 中使用新检查器类型（参考 docs/rule-writing-guide.md）。

### 第 4 步: 测试

```go
func TestMyChecker_Pass(t *testing.T) {
    checker := &MyChecker{}
    result := checker.Check(CheckRule{
        Target: "/etc/my.conf",
        Params: map[string]interface{}{"expected": "yes"},
    })
    assert.Equal(t, "pass", result.Status)
}
```

## 任务流转

```
1. Manager: 用户创建扫描任务 → 写入 scan_tasks (status=pending)
2. AgentCenter: 调度器读取 pending 任务
   → 查询目标主机列表
   → 查询关联策略和规则
   → gRPC Command 下发到 Agent
3. Agent: 接收 Command → 路由到 baseline 插件 → Pipe 发送 Task
4. Plugin: ReceiveTask() → engine.Execute(policy)
   → 遍历规则 → 调用对应 Checker
   → 逐条 SendRecord(DataType=8000)
5. Agent → AgentCenter: 透传 PackagedData
6. AgentCenter: 解析 DataType=8000 → 写入 scan_results
7. Manager: GET /api/v1/results → 返回给 UI
```

## 策略配置格式

```json
{
  "policy_id": "LINUX_SSH_BASELINE",
  "rules": [
    {
      "rule_id": "SSH_001",
      "category": "ssh",
      "title": "禁止 SSH 空密码登录",
      "severity": "high",
      "check_config": {
        "conditions": "all",
        "checks": [
          {
            "check_type": "file_kv",
            "target": "/etc/ssh/sshd_config",
            "params": {
              "key": "PermitEmptyPasswords",
              "expected": "no",
              "separator": " "
            }
          }
        ]
      }
    }
  ]
}
```

## 构建

```bash
# 构建所有插件
make package-plugins-all VERSION="1.0.0"

# 输出: dist/plugins/
#   baseline-linux-amd64
#   baseline-linux-arm64
#   collector-linux-amd64
#   ...
```

## 测试

```bash
# 单元测试
go test ./plugins/baseline/... -v

# 手动测试: 启动 Agent + Server，创建扫描任务观察结果
```
