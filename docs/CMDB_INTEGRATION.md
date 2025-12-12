# CMDB 对接指南

> 本文档说明如何将 CMDB（配置管理数据库）与矩阵云安全平台（mxsec-platform）进行对接。
>
> 通过对接，CMDB 可以从 mxsec-platform 获取资产数据和执行安全检查任务。

---

## 目录

1. [概述](#概述)
2. [对接功能](#对接功能)
3. [API 接口](#api-接口)
4. [数据模型](#数据模型)
5. [对接步骤](#对接步骤)
6. [示例代码](#示例代码)
7. [常见问题](#常见问题)
8. [故障排查](#故障排查)

---

## 概述

### 背景

矩阵云安全平台（mxsec-platform）是一个轻量级的 Linux 操作系统基线合规性检查平台，采用 Agent + Plugin + Server 架构，具有以下特点：

- **Agent**：部署在主机上，与 Server 通过 gRPC 进行通信
- **Plugins**：在 Agent 端执行检查和采集任务
  - **Baseline Plugin**：执行基线检查，返回 Pass/Fail 结果
  - **Collector Plugin**：采集主机资产信息（进程、端口、用户等）
- **Server**：接收 Agent 数据，管理策略、任务、结果

### 对接目标

1. **资产数据同步**：CMDB 定期从 mxsec-platform 获取采集的资产数据
2. **任务执行**：CMDB 可以触发 mxsec-platform 执行安全检查任务
3. **结果查询**：CMDB 可以查询检查结果、风险评分等信息

### 对接方式

- 基于 **HTTP REST API**（无需 gRPC）
- **认证方式**：JWT Token（用户名/密码登录）
- **数据格式**：JSON
- **请求/响应**：标准 RESTful 风格

---

## 对接功能

### 功能 1：资产数据获取

**功能描述**：从 mxsec-platform 获取主机的资产采集数据

**包含数据类型**：
- 进程信息（ProcessHandler）
  - 进程 ID、名称、二进制路径、MD5、命令行参数
  - 是否容器化、容器 ID
- 网络端口（PortHandler）
  - TCP/UDP 端口、监听地址、关联进程
  - 进程名、PID、用户
- 账户信息（UserHandler）
  - 用户名、UID、GID、家目录、Shell
  - 密码过期信息、是否可登录

**API 端点**：
- `GET /api/v1/assets/processes` - 获取进程列表
- `GET /api/v1/assets/ports` - 获取端口列表
- `GET /api/v1/assets/users` - 获取用户账户列表
- `GET /api/v1/hosts` - 获取主机列表（基本信息）
- `GET /api/v1/hosts/{host_id}` - 获取主机详情

**使用场景**：
- 定期同步主机资产信息到 CMDB
- 实时查询主机的进程、端口、账户信息
- 用于资产拓扑管理、依赖关系分析

---

### 功能 2：任务执行

**功能描述**：从 CMDB 触发 mxsec-platform 执行安全检查任务

**支持的任务类型**：
- `baseline`：基线检查（基于指定策略）

**执行流程**：
1. CMDB 创建任务，指定要检查的主机和策略
2. mxsec-platform AgentCenter 接收任务，调度给对应主机的 Agent
3. Agent 执行插件任务，返回检查结果
4. CMDB 查询任务执行状态和结果

**API 端点**：
- `POST /api/v1/tasks` - 创建任务
- `GET /api/v1/tasks` - 获取任务列表
- `POST /api/v1/tasks/{task_id}/run` - 执行任务
- `GET /api/v1/results` - 查询检查结果
- `GET /api/v1/results/host/{host_id}/score` - 获取基线得分

**使用场景**：
- 定期执行基线检查，获取合规状态
- 触发临时检查，诊断安全问题
- 追踪检查历史，分析风险演变

---

## API 接口

### 1. 认证接口

#### 1.1 用户登录

**请求**：

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin"
}
```

**响应** (成功 - HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "token": "eyJhbGc...",
    "expires_at": "2025-12-13T10:00:00Z"
  }
}
```

**响应** (失败 - HTTP 401)：

```json
{
  "code": 401,
  "message": "用户名或密码错误"
}
```

**说明**：
- Token 有效期通常为 24 小时
- 后续 API 请求需要在 Header 中添加 `Authorization: Bearer {token}`
- 默认用户：`admin` / `admin`

---

### 2. 主机管理接口

#### 2.1 获取主机列表

**请求**：

```http
GET /api/v1/hosts?page=1&limit=10&os_family=rocky
Authorization: Bearer {token}
```

**查询参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| page | int | 否 | 页码（默认 1） |
| limit | int | 否 | 每页数量（默认 10） |
| os_family | string | 否 | OS 族过滤（rocky, centos, debian 等） |
| status | string | 否 | 主机状态过滤（online, offline） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 100,
    "page": 1,
    "limit": 10,
    "hosts": [
      {
        "host_id": "host-001",
        "hostname": "web-server-01",
        "os_family": "rocky",
        "os_version": "9.0",
        "kernel_version": "5.14.0",
        "arch": "x86_64",
        "ipv4": "10.0.0.1",
        "ipv6": "fe80::1",
        "status": "online",
        "last_heartbeat": "2025-12-12T10:30:00Z",
        "baseline_score": 85.5,
        "created_at": "2025-12-01T00:00:00Z",
        "updated_at": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

---

#### 2.2 获取主机详情

**请求**：

```http
GET /api/v1/hosts/{host_id}
Authorization: Bearer {token}
```

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "host_id": "host-001",
    "hostname": "web-server-01",
    "os_family": "rocky",
    "os_version": "9.0",
    "kernel_version": "5.14.0",
    "arch": "x86_64",
    "ipv4": "10.0.0.1",
    "ipv6": "fe80::1",
    "status": "online",
    "last_heartbeat": "2025-12-12T10:30:00Z",
    "baseline_score": 85.5,
    "agent_version": "v1.0.0",
    "plugin_status": {
      "baseline": {
        "status": "running",
        "version": "v1.0.0",
        "last_heartbeat": "2025-12-12T10:30:00Z"
      },
      "collector": {
        "status": "running",
        "version": "v1.0.0",
        "last_heartbeat": "2025-12-12T10:30:00Z"
      }
    },
    "created_at": "2025-12-01T00:00:00Z",
    "updated_at": "2025-12-12T10:30:00Z"
  }
}
```

---

### 3. 资产数据接口

#### 3.1 获取进程列表

**请求**：

```http
GET /api/v1/assets/processes?host_id=host-001&page=1&limit=20
Authorization: Bearer {token}
```

**查询参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| host_id | string | 是 | 主机 ID |
| page | int | 否 | 页码（默认 1） |
| limit | int | 否 | 每页数量（默认 20） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 150,
    "page": 1,
    "limit": 20,
    "processes": [
      {
        "host_id": "host-001",
        "pid": 1,
        "name": "systemd",
        "binary_path": "/usr/lib/systemd/systemd",
        "md5": "abc123def456...",
        "cmd_line": "/sbin/init",
        "owner": "root",
        "create_time": "2025-12-01T08:00:00Z",
        "is_container": false,
        "container_id": "",
        "collected_at": "2025-12-12T10:30:00Z"
      },
      {
        "host_id": "host-001",
        "pid": 256,
        "name": "nginx",
        "binary_path": "/usr/sbin/nginx",
        "md5": "def456abc123...",
        "cmd_line": "nginx: master process /usr/sbin/nginx",
        "owner": "root",
        "create_time": "2025-12-01T08:05:00Z",
        "is_container": true,
        "container_id": "abc123def456",
        "collected_at": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

**数据字段说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| host_id | string | 主机 ID |
| pid | int | 进程 ID |
| name | string | 进程名 |
| binary_path | string | 二进制文件路径 |
| md5 | string | 二进制文件 MD5 哈希值 |
| cmd_line | string | 进程完整命令行 |
| owner | string | 进程所有者（用户名） |
| create_time | string | 进程创建时间（ISO 8601） |
| is_container | bool | 是否容器化进程 |
| container_id | string | 容器 ID（如果容器化） |
| collected_at | string | 数据采集时间 |

---

#### 3.2 获取端口列表

**请求**：

```http
GET /api/v1/assets/ports?host_id=host-001&page=1&limit=20
Authorization: Bearer {token}
```

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 45,
    "page": 1,
    "limit": 20,
    "ports": [
      {
        "host_id": "host-001",
        "protocol": "tcp",
        "port": 22,
        "listen_address": "0.0.0.0",
        "pid": 256,
        "process_name": "sshd",
        "owner": "root",
        "state": "LISTEN",
        "collected_at": "2025-12-12T10:30:00Z"
      },
      {
        "host_id": "host-001",
        "protocol": "tcp",
        "port": 80,
        "listen_address": "0.0.0.0",
        "pid": 512,
        "process_name": "nginx",
        "owner": "root",
        "state": "LISTEN",
        "collected_at": "2025-12-12T10:30:00Z"
      },
      {
        "host_id": "host-001",
        "protocol": "udp",
        "port": 53,
        "listen_address": "127.0.0.1",
        "pid": 768,
        "process_name": "named",
        "owner": "root",
        "state": "LISTEN",
        "collected_at": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

**数据字段说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| host_id | string | 主机 ID |
| protocol | string | 协议（tcp/udp） |
| port | int | 端口号 |
| listen_address | string | 监听地址 |
| pid | int | 关联进程 ID |
| process_name | string | 关联进程名 |
| owner | string | 进程所有者 |
| state | string | 连接状态（LISTEN、ESTABLISHED 等） |
| collected_at | string | 数据采集时间 |

---

#### 3.3 获取用户列表

**请求**：

```http
GET /api/v1/assets/users?host_id=host-001&page=1&limit=20
Authorization: Bearer {token}
```

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 12,
    "page": 1,
    "limit": 20,
    "users": [
      {
        "host_id": "host-001",
        "username": "root",
        "uid": 0,
        "gid": 0,
        "group": "root",
        "home_dir": "/root",
        "shell": "/bin/bash",
        "real_name": "root",
        "pass_change_days": 0,
        "pass_age": 0,
        "pass_warn_days": 7,
        "pass_max_days": 99999,
        "pass_inactive_days": -1,
        "pass_expire_date": "2000-01-01",
        "is_login": true,
        "collected_at": "2025-12-12T10:30:00Z"
      },
      {
        "host_id": "host-001",
        "username": "nginx",
        "uid": 998,
        "gid": 996,
        "group": "nginx",
        "home_dir": "/var/cache/nginx",
        "shell": "/sbin/nologin",
        "real_name": "",
        "pass_change_days": 19268,
        "pass_age": 0,
        "pass_warn_days": 0,
        "pass_max_days": 99999,
        "pass_inactive_days": -1,
        "pass_expire_date": "2000-01-01",
        "is_login": false,
        "collected_at": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

**数据字段说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| host_id | string | 主机 ID |
| username | string | 用户名 |
| uid | int | 用户 ID |
| gid | int | 组 ID |
| group | string | 所属组 |
| home_dir | string | 家目录 |
| shell | string | 登录 Shell |
| real_name | string | 用户实名 |
| pass_change_days | int | 密码最后修改天数（自 1970-01-01） |
| pass_age | int | 密码存在天数 |
| pass_warn_days | int | 密码过期前警告天数 |
| pass_max_days | int | 密码有效期（天数） |
| pass_inactive_days | int | 账户不活动天数 |
| pass_expire_date | string | 账户过期日期 |
| is_login | bool | 是否允许登录 |
| collected_at | string | 数据采集时间 |

---

### 4. 任务管理接口

#### 4.1 创建任务

**请求**：

```http
POST /api/v1/tasks
Content-Type: application/json
Authorization: Bearer {token}

{
  "name": "全量基线扫描",
  "type": "baseline",
  "policy_id": "linux-baseline-001",
  "targets": {
    "type": "all"
  }
}
```

**或指定主机列表**：

```json
{
  "name": "指定主机基线扫描",
  "type": "baseline",
  "policy_id": "linux-baseline-001",
  "targets": {
    "type": "host_ids",
    "host_ids": ["host-001", "host-002", "host-003"]
  }
}
```

**请求字段说明**：
| 字段 | 类型 | 必需 | 说明 |
|------|------|------|------|
| name | string | 是 | 任务名称 |
| type | string | 是 | 任务类型（baseline） |
| policy_id | string | 是 | 策略 ID |
| targets | object | 是 | 目标主机配置 |
| targets.type | string | 是 | 目标类型（all 或 host_ids） |
| targets.host_ids | array | 条件 | 主机 ID 列表（当 type=host_ids 时必需） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "task_id": "task-001",
    "name": "全量基线扫描",
    "type": "baseline",
    "policy_id": "linux-baseline-001",
    "status": "created",
    "target_hosts": ["host-001", "host-002"],
    "created_at": "2025-12-12T10:30:00Z",
    "updated_at": "2025-12-12T10:30:00Z"
  }
}
```

---

#### 4.2 获取任务列表

**请求**：

```http
GET /api/v1/tasks?page=1&limit=10&status=completed
Authorization: Bearer {token}
```

**查询参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| page | int | 否 | 页码（默认 1） |
| limit | int | 否 | 每页数量（默认 10） |
| status | string | 否 | 任务状态（created, running, completed, failed） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 50,
    "page": 1,
    "limit": 10,
    "tasks": [
      {
        "task_id": "task-001",
        "name": "全量基线扫描",
        "type": "baseline",
        "policy_id": "linux-baseline-001",
        "status": "completed",
        "target_hosts": ["host-001", "host-002"],
        "created_at": "2025-12-12T08:00:00Z",
        "executed_at": "2025-12-12T08:05:00Z",
        "completed_at": "2025-12-12T08:15:00Z"
      }
    ]
  }
}
```

---

#### 4.3 执行任务

**请求**：

```http
POST /api/v1/tasks/{task_id}/run
Authorization: Bearer {token}
```

**响应** (HTTP 200 - 任务开始执行)：

```json
{
  "code": 0,
  "data": {
    "task_id": "task-001",
    "status": "running",
    "executed_at": "2025-12-12T10:30:00Z"
  }
}
```

**响应** (HTTP 409 - 任务已在运行)：

```json
{
  "code": 409,
  "message": "任务正在执行中，无法重复执行"
}
```

**说明**：
- 执行任务会将任务状态改为 `running`
- AgentCenter 会自动下发任务到对应的 Agent
- Agent 执行插件任务后返回结果
- 任务状态自动更新为 `completed` 或 `failed`

---

### 5. 结果查询接口

#### 5.1 获取检测结果

**请求**：

```http
GET /api/v1/results?host_id=host-001&policy_id=linux-baseline-001&status=fail&page=1&limit=20
Authorization: Bearer {token}
```

**查询参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| host_id | string | 否 | 主机 ID 过滤 |
| policy_id | string | 否 | 策略 ID 过滤 |
| status | string | 否 | 检查结果过滤（pass, fail, error） |
| page | int | 否 | 页码（默认 1） |
| limit | int | 否 | 每页数量（默认 20） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "total": 25,
    "page": 1,
    "limit": 20,
    "results": [
      {
        "result_id": "result-001",
        "host_id": "host-001",
        "policy_id": "linux-baseline-001",
        "rule_id": "SSH_001",
        "rule_title": "SSH 禁止 root 登录",
        "status": "fail",
        "severity": "high",
        "expected": "no",
        "actual": "yes",
        "checked_at": "2025-12-12T10:30:00Z"
      },
      {
        "result_id": "result-002",
        "host_id": "host-001",
        "policy_id": "linux-baseline-001",
        "rule_id": "PASS_001",
        "rule_title": "密码过期时间检查",
        "status": "pass",
        "severity": "medium",
        "expected": "90",
        "actual": "90",
        "checked_at": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

**数据字段说明**：
| 字段 | 类型 | 说明 |
|------|------|------|
| result_id | string | 结果 ID |
| host_id | string | 主机 ID |
| policy_id | string | 策略 ID |
| rule_id | string | 规则 ID |
| rule_title | string | 规则标题 |
| status | string | 检查结果（pass/fail/error） |
| severity | string | 风险等级（low/medium/high/critical） |
| expected | string | 期望值 |
| actual | string | 实际值 |
| checked_at | string | 检查时间 |

---

#### 5.2 获取主机基线得分

**请求**：

```http
GET /api/v1/results/host/{host_id}/score?policy_id=linux-baseline-001
Authorization: Bearer {token}
```

**查询参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| policy_id | string | 否 | 策略 ID（如果不指定，返回所有策略的得分） |

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "host_id": "host-001",
    "hostname": "web-server-01",
    "os_family": "rocky",
    "os_version": "9.0",
    "baseline_score": 85.5,
    "policies": [
      {
        "policy_id": "linux-baseline-001",
        "policy_name": "Linux 系统基线",
        "score": 85.5,
        "pass_count": 17,
        "fail_count": 3,
        "total_count": 20,
        "pass_rate": 0.85,
        "last_check_time": "2025-12-12T10:30:00Z"
      }
    ]
  }
}
```

---

#### 5.3 获取主机基线摘要

**请求**：

```http
GET /api/v1/results/host/{host_id}/summary
Authorization: Bearer {token}
```

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "host_id": "host-001",
    "hostname": "web-server-01",
    "baseline_score": 85.5,
    "status_summary": {
      "pass": 17,
      "fail": 3,
      "error": 0,
      "total": 20
    },
    "severity_summary": {
      "critical": 0,
      "high": 2,
      "medium": 1,
      "low": 0
    },
    "failed_rules": [
      {
        "rule_id": "SSH_001",
        "rule_title": "SSH 禁止 root 登录",
        "severity": "high",
        "expected": "no",
        "actual": "yes",
        "policy_id": "linux-baseline-001",
        "policy_name": "Linux 系统基线",
        "checked_at": "2025-12-12T10:30:00Z"
      }
    ],
    "last_check_time": "2025-12-12T10:30:00Z"
  }
}
```

---

### 6. 策略管理接口（可选）

#### 6.1 获取策略列表

**请求**：

```http
GET /api/v1/policies
Authorization: Bearer {token}
```

**响应** (HTTP 200)：

```json
{
  "code": 0,
  "data": {
    "policies": [
      {
        "policy_id": "linux-baseline-001",
        "name": "Linux 系统基线",
        "description": "CIS Linux 基准",
        "os_family": ["rocky", "centos"],
        "enabled": true,
        "rule_count": 20,
        "created_at": "2025-12-01T00:00:00Z"
      }
    ]
  }
}
```

---

## 数据模型

### 主机数据模型

```json
{
  "host_id": "唯一主机标识，Agent 生成时的 UUID",
  "hostname": "主机名",
  "os_family": "操作系统族（rocky、centos、debian 等）",
  "os_version": "操作系统版本号",
  "kernel_version": "内核版本",
  "arch": "CPU 架构（x86_64、arm64 等）",
  "ipv4": "IPv4 地址（首个地址）",
  "ipv6": "IPv6 地址（首个地址）",
  "status": "主机状态（online、offline）",
  "last_heartbeat": "最后心跳时间（ISO 8601）",
  "baseline_score": "基线得分（0-100）",
  "agent_version": "Agent 版本号",
  "plugin_status": {
    "baseline": {
      "status": "running/stopped",
      "version": "v1.0.0",
      "last_heartbeat": "ISO 8601"
    },
    "collector": {
      "status": "running/stopped",
      "version": "v1.0.0",
      "last_heartbeat": "ISO 8601"
    }
  },
  "created_at": "创建时间（ISO 8601）",
  "updated_at": "更新时间（ISO 8601）"
}
```

### 任务数据模型

```json
{
  "task_id": "任务 ID（UUID）",
  "name": "任务名称",
  "type": "任务类型（baseline）",
  "policy_id": "关联的策略 ID",
  "status": "任务状态（created、running、completed、failed）",
  "target_hosts": "目标主机 ID 数组",
  "created_at": "创建时间",
  "executed_at": "执行时间",
  "completed_at": "完成时间"
}
```

### 检查结果数据模型

```json
{
  "result_id": "结果 ID",
  "host_id": "主机 ID",
  "policy_id": "策略 ID",
  "rule_id": "规则 ID",
  "rule_title": "规则标题",
  "status": "检查结果（pass、fail、error）",
  "severity": "风险等级（low、medium、high、critical）",
  "expected": "期望值",
  "actual": "实际值",
  "checked_at": "检查时间"
}
```

---

## 对接步骤

### 步骤 1：获取认证 Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "admin"
  }'
```

将返回的 `token` 保存，用于后续请求。

---

### 步骤 2：拉取主机列表

```bash
curl -X GET "http://localhost:8080/api/v1/hosts?page=1&limit=100" \
  -H "Authorization: Bearer {token}"
```

获取所有主机的基本信息，建立 CMDB 与 mxsec-platform 的主机映射关系。

---

### 步骤 3：拉取资产数据

对于每个主机，拉取资产数据：

```bash
# 拉取进程列表
curl -X GET "http://localhost:8080/api/v1/assets/processes?host_id=host-001&limit=100" \
  -H "Authorization: Bearer {token}"

# 拉取端口列表
curl -X GET "http://localhost:8080/api/v1/assets/ports?host_id=host-001&limit=100" \
  -H "Authorization: Bearer {token}"

# 拉取用户列表
curl -X GET "http://localhost:8080/api/v1/assets/users?host_id=host-001&limit=100" \
  -H "Authorization: Bearer {token}"
```

---

### 步骤 4：创建和执行任务

```bash
# 创建任务
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {token}" \
  -d '{
    "name": "全量基线扫描",
    "type": "baseline",
    "policy_id": "linux-baseline-001",
    "targets": {
      "type": "all"
    }
  }'

# 执行任务
curl -X POST http://localhost:8080/api/v1/tasks/task-001/run \
  -H "Authorization: Bearer {token}"
```

---

### 步骤 5：查询结果

```bash
# 轮询查询任务状态
curl -X GET "http://localhost:8080/api/v1/tasks?status=completed&limit=100" \
  -H "Authorization: Bearer {token}"

# 查询检查结果
curl -X GET "http://localhost:8080/api/v1/results?policy_id=linux-baseline-001&status=fail" \
  -H "Authorization: Bearer {token}"

# 查询主机得分
curl -X GET "http://localhost:8080/api/v1/results/host/host-001/score" \
  -H "Authorization: Bearer {token}"
```

---

## 示例代码

### Python 示例

```python
import requests
import json
from typing import Dict, List, Optional

class MxsecPlatformClient:
    """矩阵云安全平台 API 客户端"""

    def __init__(self, base_url: str, username: str, password: str):
        self.base_url = base_url.rstrip('/')
        self.username = username
        self.password = password
        self.token = None

    def login(self) -> bool:
        """登录获取 Token"""
        try:
            resp = requests.post(
                f"{self.base_url}/api/v1/auth/login",
                json={"username": self.username, "password": self.password}
            )
            if resp.status_code == 200:
                data = resp.json()
                if data.get('code') == 0:
                    self.token = data['data']['token']
                    return True
        except Exception as e:
            print(f"Login failed: {e}")
        return False

    def _request(self, method: str, endpoint: str, **kwargs) -> Optional[Dict]:
        """发送 HTTP 请求"""
        url = f"{self.base_url}{endpoint}"
        headers = kwargs.pop('headers', {})
        headers['Authorization'] = f'Bearer {self.token}'

        try:
            resp = requests.request(method, url, headers=headers, **kwargs)
            return resp.json() if resp.status_code < 400 else None
        except Exception as e:
            print(f"Request failed: {e}")
            return None

    def get_hosts(self, page: int = 1, limit: int = 100) -> List[Dict]:
        """获取主机列表"""
        data = self._request('GET', '/api/v1/hosts', params={'page': page, 'limit': limit})
        return data['data']['hosts'] if data else []

    def get_processes(self, host_id: str, limit: int = 100) -> List[Dict]:
        """获取主机进程列表"""
        data = self._request('GET', '/api/v1/assets/processes',
                           params={'host_id': host_id, 'limit': limit})
        return data['data']['processes'] if data else []

    def get_ports(self, host_id: str, limit: int = 100) -> List[Dict]:
        """获取主机端口列表"""
        data = self._request('GET', '/api/v1/assets/ports',
                           params={'host_id': host_id, 'limit': limit})
        return data['data']['ports'] if data else []

    def get_users(self, host_id: str, limit: int = 100) -> List[Dict]:
        """获取主机用户列表"""
        data = self._request('GET', '/api/v1/assets/users',
                           params={'host_id': host_id, 'limit': limit})
        return data['data']['users'] if data else []

    def create_task(self, name: str, policy_id: str, target_type: str = 'all',
                   host_ids: Optional[List[str]] = None) -> Optional[Dict]:
        """创建任务"""
        targets = {'type': target_type}
        if target_type == 'host_ids' and host_ids:
            targets['host_ids'] = host_ids

        data = self._request('POST', '/api/v1/tasks',
                           json={
                               'name': name,
                               'type': 'baseline',
                               'policy_id': policy_id,
                               'targets': targets
                           })
        return data['data'] if data else None

    def run_task(self, task_id: str) -> bool:
        """执行任务"""
        data = self._request('POST', f'/api/v1/tasks/{task_id}/run')
        return data and data.get('code') == 0

    def get_results(self, host_id: Optional[str] = None,
                   policy_id: Optional[str] = None,
                   status: Optional[str] = None) -> List[Dict]:
        """查询检查结果"""
        params = {}
        if host_id:
            params['host_id'] = host_id
        if policy_id:
            params['policy_id'] = policy_id
        if status:
            params['status'] = status
        params['limit'] = 1000

        data = self._request('GET', '/api/v1/results', params=params)
        return data['data']['results'] if data else []

    def get_host_score(self, host_id: str) -> Optional[Dict]:
        """获取主机基线得分"""
        data = self._request('GET', f'/api/v1/results/host/{host_id}/score')
        return data['data'] if data else None

# 使用示例
if __name__ == '__main__':
    client = MxsecPlatformClient('http://localhost:8080', 'admin', 'admin')

    # 登录
    if not client.login():
        print("Login failed")
        exit(1)

    # 获取主机列表
    hosts = client.get_hosts()
    print(f"Found {len(hosts)} hosts")

    # 获取第一个主机的进程列表
    if hosts:
        host_id = hosts[0]['host_id']
        processes = client.get_processes(host_id)
        print(f"Host {host_id} has {len(processes)} processes")

        # 创建任务
        task = client.create_task('CMDB Scan', 'linux-baseline-001')
        if task:
            print(f"Created task {task['task_id']}")

            # 执行任务
            if client.run_task(task['task_id']):
                print(f"Task {task['task_id']} started")
```

---

### Java 示例

```java
import com.google.gson.*;
import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.util.*;

public class MxsecPlatformClient {
    private final String baseUrl;
    private final HttpClient httpClient;
    private String token;

    public MxsecPlatformClient(String baseUrl) {
        this.baseUrl = baseUrl.replaceAll("/$", "");
        this.httpClient = HttpClient.newHttpClient();
    }

    /**
     * 登录获取 Token
     */
    public boolean login(String username, String password) throws IOException, InterruptedException {
        JsonObject body = new JsonObject();
        body.addProperty("username", username);
        body.addProperty("password", password);

        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create(baseUrl + "/api/v1/auth/login"))
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body.toString()))
            .build();

        HttpResponse<String> response = httpClient.send(request, HttpResponse.BodyHandlers.ofString());
        JsonObject json = JsonParser.parseString(response.body()).getAsJsonObject();

        if (json.get("code").getAsInt() == 0) {
            token = json.getAsJsonObject("data").get("token").getAsString();
            return true;
        }
        return false;
    }

    /**
     * 发送 HTTP 请求
     */
    private String request(String method, String endpoint, String queryString, String body)
            throws IOException, InterruptedException {
        String url = baseUrl + endpoint;
        if (queryString != null && !queryString.isEmpty()) {
            url += "?" + queryString;
        }

        HttpRequest.Builder builder = HttpRequest.newBuilder()
            .uri(URI.create(url))
            .header("Authorization", "Bearer " + token);

        if ("POST".equals(method) || "PUT".equals(method)) {
            builder.header("Content-Type", "application/json");
            if (body != null) {
                builder.method(method, HttpRequest.BodyPublishers.ofString(body));
            } else {
                builder.method(method, HttpRequest.BodyPublishers.ofString(""));
            }
        } else {
            builder.GET();
        }

        HttpResponse<String> response = httpClient.send(builder.build(),
            HttpResponse.BodyHandlers.ofString());
        return response.body();
    }

    /**
     * 获取主机列表
     */
    public List<JsonObject> getHosts(int page, int limit) throws IOException, InterruptedException {
        String query = "page=" + page + "&limit=" + limit;
        String response = request("GET", "/api/v1/hosts", query, null);
        JsonObject json = JsonParser.parseString(response).getAsJsonObject();

        List<JsonObject> hosts = new ArrayList<>();
        if (json.get("code").getAsInt() == 0) {
            json.getAsJsonObject("data").getAsJsonArray("hosts").forEach(
                elem -> hosts.add(elem.getAsJsonObject())
            );
        }
        return hosts;
    }

    /**
     * 获取主机进程列表
     */
    public List<JsonObject> getProcesses(String hostId) throws IOException, InterruptedException {
        String query = "host_id=" + hostId + "&limit=100";
        String response = request("GET", "/api/v1/assets/processes", query, null);
        JsonObject json = JsonParser.parseString(response).getAsJsonObject();

        List<JsonObject> processes = new ArrayList<>();
        if (json.get("code").getAsInt() == 0) {
            json.getAsJsonObject("data").getAsJsonArray("processes").forEach(
                elem -> processes.add(elem.getAsJsonObject())
            );
        }
        return processes;
    }

    /**
     * 创建任务
     */
    public JsonObject createTask(String name, String policyId) throws IOException, InterruptedException {
        JsonObject targets = new JsonObject();
        targets.addProperty("type", "all");

        JsonObject body = new JsonObject();
        body.addProperty("name", name);
        body.addProperty("type", "baseline");
        body.addProperty("policy_id", policyId);
        body.add("targets", targets);

        String response = request("POST", "/api/v1/tasks", "", body.toString());
        JsonObject json = JsonParser.parseString(response).getAsJsonObject();

        return json.get("code").getAsInt() == 0 ? json.getAsJsonObject("data") : null;
    }

    /**
     * 执行任务
     */
    public boolean runTask(String taskId) throws IOException, InterruptedException {
        String response = request("POST", "/api/v1/tasks/" + taskId + "/run", "", "");
        JsonObject json = JsonParser.parseString(response).getAsJsonObject();
        return json.get("code").getAsInt() == 0;
    }
}
```

---

## 常见问题

### Q1: 如何处理 Token 过期？

**答**：Token 的有效期通常为 24 小时。当收到 `401 Unauthorized` 响应时，需要重新登录获取新的 Token。

```python
if response.status_code == 401:
    client.login()
    # 重试请求
```

---

### Q2: 资产数据如何更新？

**答**：资产数据由 Agent 端的 Collector Plugin 定期采集并上报到 Server。采集频率通常为 30 分钟一次。CMDB 可以定期轮询 API 来获取最新数据。

---

### Q3: 如何处理分页数据？

**答**：使用 `page` 和 `limit` 参数分页：

```python
page = 1
limit = 100
while True:
    hosts = client.get_hosts(page=page, limit=limit)
    if not hosts:
        break
    # 处理 hosts
    page += 1
```

---

### Q4: 任务执行需要多长时间？

**答**：
- 任务下发：通常 1-5 秒
- 插件执行：取决于检查项数量，通常 10-60 秒
- 结果上报：通常 1-5 秒

总耗时通常为 20-90 秒，建议轮询时间间隔为 10-15 秒。

---

### Q5: 如何处理高并发请求？

**答**：
- 使用连接池复用 HTTP 连接
- 对于大批量拉取，使用分页和时间间隔
- 避免同时对同一主机执行多个任务

---

## 故障排查

### 问题 1：登录失败

**现象**：`401 Unauthorized`

**排查步骤**：
1. 确认用户名和密码正确（默认：admin/admin）
2. 检查 Server 是否正常运行
3. 查看 Server 日志：`docker logs mxsec-manager-dev`

---

### 问题 2：无法获取资产数据

**现象**：资产列表为空

**排查步骤**：
1. 确认主机已连接到 Server（`GET /api/v1/hosts`）
2. 检查 Collector Plugin 是否运行
3. 等待第一次采集周期完成（通常 5-10 分钟）
4. 查看 Agent 日志

---

### 问题 3：任务执行失败

**现象**：任务状态为 `failed`

**排查步骤**：
1. 确认目标主机状态为 `online`
2. 检查策略 ID 是否有效
3. 查看 Agent 日志获取错误信息
4. 检查主机是否满足策略的 OS 要求

---

### 问题 4：API 响应慢

**现象**：API 请求超时

**排查步骤**：
1. 检查网络连接
2. 查看 Server 资源占用（CPU、内存）
3. 检查数据库性能
4. 考虑增加返回数据的 limit 参数

---

## 总结

通过上述 API，CMDB 可以轻松与 mxsec-platform 进行集成，获取主机的资产数据和基线检查结果。

**关键步骤**：
1. 登录获取 Token
2. 拉取主机列表建立映射
3. 定期拉取资产数据
4. 根据需求创建和执行任务
5. 查询结果并进行后续处理

**推荐集成方案**：
- 每小时定时拉取资产数据
- 每周执行一次全量基线检查
- 实时监控检查结果，自动生成告警

---

**文档版本**：v1.0
**最后更新**：2025-12-12
**维护者**：Claude Code
