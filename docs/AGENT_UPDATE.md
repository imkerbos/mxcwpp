# Agent 更新机制文档

## 概述

本文档说明 Agent 的更新策略、更新流程和实现细节。

## 核心原则

### 1. 版本独立性 ✅

**Agent 版本和插件版本完全独立**：

- Agent 存储在 `components` 表，`category = "agent"`
- 插件存储在 `components` 表，`category = "plugin"`
- 版本号独立管理，互不干扰
- **Agent 可以是 2.0.0，插件可以是 1.0.0，完全没问题**

### 2. 更新策略

- **自动检测**：Server 每 30 秒检查一次，发现新版本自动推送
- **手动推送**：支持通过 UI/API 手动触发更新
- **安全更新**：下载、校验 SHA256、安装、重启

---

## 更新流程

### 完整流程图

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent 更新完整流程                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Agent 心跳上报当前版本（PackagedData.version）                    │
│     ↓                                                               │
│  2. Server 存储到 hosts.agent_version 字段                           │
│     ↓                                                               │
│  3. AgentUpdateScheduler 检测到新版本（每30秒）                        │
│     - 查询 components 表获取 Agent 最新版本                           │
│     - 对比 hosts.agent_version 与最新版本                             │
│     ↓                                                               │
│  4. Server 下发 AgentUpdate 命令（包含下载URL、版本、SHA256）          │
│     - 通过 gRPC Command.AgentUpdate 下发                              │
│     ↓                                                               │
│  5. Agent 接收命令，比较版本号                                        │
│     - 如果版本相同且 force=false，跳过                                 │
│     ↓                                                               │
│  6. Agent 下载新版本（HTTP 下载，支持断点续传）                        │
│     - 从 /api/v1/agent/download/{pkg_type}/{arch} 下载                │
│     ↓                                                               │
│  7. Agent 校验 SHA256                                                │
│     - 验证下载文件的完整性                                             │
│     ↓                                                               │
│  8. Agent 安装新版本                                                  │
│     - RPM: rpm -Uvh /tmp/mxsec-agent-xxx.rpm                         │
│     - DEB: dpkg -i /tmp/mxsec-agent-xxx.deb                           │
│     - Binary: 替换二进制文件并设置权限                                 │
│     ↓                                                               │
│  9. Agent 重启（优雅退出，systemd 自动重启）                          │
│     - 发送 SIGTERM 信号，等待进程退出                                  │
│     - systemd 检测到进程退出，自动重启新版本                            │
│     ↓                                                               │
│  10. 新版本 Agent 启动，上报新版本号                                   │
│      - 心跳中包含新的 version 字段                                     │
│      - Server 更新 hosts.agent_version                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 实现细节

### 1. 数据库模型

#### Host 表新增字段

```go
type Host struct {
    // ... 其他字段
    AgentVersion string `gorm:"column:agent_version;type:varchar(32)"` // Agent 当前版本号
}
```

#### Component 表结构（已存在）

- `components`: 组件基本信息（agent、baseline、collector）
- `component_versions`: 组件版本信息
- `component_packages`: 组件包信息（RPM/DEB/Binary，不同架构）

### 2. Protobuf 定义

#### Command.AgentUpdate（新增）

```protobuf
message AgentUpdate {
  string version = 1;        // 新版本号
  string download_url = 2;    // 下载地址
  string sha256 = 3;         // SHA256 校验和
  string pkg_type = 4;       // 包类型 (rpm/deb/binary)
  string arch = 5;           // 架构 (amd64/arm64)
  bool force = 6;            // 是否强制更新
}
```

### 3. Server 端实现

#### AgentUpdateScheduler（自动检测并推送）

**文件**: `internal/server/agentcenter/scheduler/agent_update_scheduler.go`

**功能**:
- 每 30 秒检查一次 `components` 表，获取 Agent 最新版本
- 对比所有在线主机的 `agent_version` 字段
- 如果版本不同，自动推送 `AgentUpdate` 命令

**启动**: 在 `AgentCenter` 启动时自动启动

#### 手动推送 API

**API**: `POST /api/v1/components/agent/push-update`

**请求体**:
```json
{
  "host_ids": ["host-1", "host-2"],  // 可选，空则推送给所有在线主机
  "force": false                     // 是否强制更新
}
```

**响应**:
```json
{
  "code": 0,
  "message": "更新请求已提交，AgentCenter 将在下次检查时推送更新",
  "data": {
    "total": 10,
    "need_update": 5,
    "latest_version": "2.0.0",
    "note": "实际推送由 AgentCenter 调度器完成（每30秒检查一次）"
  }
}
```

### 4. Agent 端实现（待实现）

#### 接收更新命令

**文件**: `internal/agent/transport/transport.go`

需要在 `receiveCommands` 中处理 `Command.AgentUpdate`：

```go
if cmd.AgentUpdate != nil {
    // 调用更新处理器
    go m.handleAgentUpdate(cmd.AgentUpdate)
}
```

#### 更新处理器（需要实现）

**文件**: `internal/agent/update/updater.go`（新建）

**功能**:
1. 比较版本号（如果相同且 force=false，跳过）
2. 下载新版本（HTTP 下载，支持断点续传）
3. 校验 SHA256
4. 安装新版本（根据 pkg_type 选择安装方式）
5. 优雅退出（systemd 自动重启）

**安装方式**:
- **RPM**: `rpm -Uvh /tmp/mxsec-agent-xxx.rpm`
- **DEB**: `dpkg -i /tmp/mxsec-agent-xxx.deb`
- **Binary**: 
  - 备份旧版本: `cp /usr/bin/mxsec-agent /usr/bin/mxsec-agent.bak`
  - 替换新版本: `cp /tmp/mxsec-agent /usr/bin/mxsec-agent`
  - 设置权限: `chmod +x /usr/bin/mxsec-agent`

---

## 使用方式

### 1. 上传新版本 Agent

1. 登录 UI → **系统管理** → **组件管理**
2. 找到 **agent** 组件
3. 点击 **发布版本**，填写版本号（如 `2.0.0`）
4. 上传对应架构的包（RPM/DEB/Binary）
5. 标记为 **最新版本**

### 2. 自动更新（推荐）

- AgentCenter 的 `AgentUpdateScheduler` 每 30 秒检查一次
- 发现新版本后自动推送给所有需要更新的在线 Agent
- Agent 收到命令后自动下载、安装、重启

### 3. 手动推送更新

**方式一：通过 API**

```bash
curl -X POST http://localhost:8080/api/v1/components/agent/push-update \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "host_ids": ["host-1", "host-2"],
    "force": false
  }'
```

**方式二：通过 UI（待实现）**

- 在组件管理页面，Agent 组件详情中显示"推送更新"按钮
- 点击后选择目标主机，确认推送

---

## 版本独立性验证

### 场景示例

| Agent 版本 | Baseline 插件版本 | Collector 插件版本 | 状态 |
|-----------|------------------|-------------------|------|
| 2.0.0     | 1.0.0            | 1.0.0             | ✅ 正常 |
| 1.5.0     | 2.0.0            | 1.0.0             | ✅ 正常 |
| 2.0.0     | 1.0.0            | 2.0.0             | ✅ 正常 |

**结论**: Agent 版本和插件版本完全独立，可以任意组合。

---

## 注意事项

### 1. Agent 端更新逻辑需要实现

当前 Server 端已实现：
- ✅ Agent 版本上报和存储
- ✅ 自动检测新版本
- ✅ 推送更新命令

**待实现**（Agent 端）:
- ⏳ 接收 `AgentUpdate` 命令
- ⏳ 下载新版本
- ⏳ 校验 SHA256
- ⏳ 安装新版本（RPM/DEB/Binary）
- ⏳ 优雅退出和重启

### 2. 部署方式差异

- **RPM/DEB 安装**: 使用包管理器安装，systemd 自动管理
- **Binary 安装**: 需要手动管理二进制文件和 systemd 服务

### 3. 更新失败处理

- 下载失败: 记录日志，下次检查时重试
- SHA256 校验失败: 删除下载文件，记录错误日志
- 安装失败: 保持旧版本运行，记录错误日志

---

## 相关文件

### Server 端
- `internal/server/model/host.go` - Host 模型（添加 agent_version 字段）
- `internal/server/agentcenter/transfer/service.go` - 心跳处理（存储 Agent 版本）
- `internal/server/agentcenter/scheduler/agent_update_scheduler.go` - Agent 更新调度器
- `internal/server/manager/api/components.go` - 手动推送 API
- `api/proto/grpc.proto` - Protobuf 定义（AgentUpdate 消息）

### Agent 端（待实现）
- `internal/agent/transport/transport.go` - 接收更新命令
- `internal/agent/update/updater.go` - 更新处理器（新建）

---

## 总结

✅ **已实现**:
1. Agent 版本上报和存储
2. 自动检测新版本并推送
3. 手动推送 API
4. Protobuf 定义和代码生成

⏳ **待实现**:
1. Agent 端接收和处理更新命令
2. Agent 端下载、校验、安装逻辑
3. UI 手动推送界面

**版本独立性**: ✅ 已确认，Agent 版本和插件版本完全独立，可以任意组合。
