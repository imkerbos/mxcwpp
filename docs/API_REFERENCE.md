# API 参考文档

本文档包含 Matrix Cloud Security Platform 的完整 HTTP API 参考。

**Base URL**: `/api/v1`
**最后更新**: 2025-12-29

---

## 认证

所有 API 请求（除登录接口外）都需要在 Header 中携带 JWT Token:

```
Authorization: Bearer <token>
```

---

## 认证 API

### 用户登录

**端点**: `POST /api/v1/auth/login`

**请求体**:
```json
{
  "username": "admin",
  "password": "admin"
}
```

**响应**:
```json
{
  "code": 0,
  "data": {
    "token": "eyJhbGc...",
    "expires_at": "2025-12-12T10:00:00Z"
  }
}
```

---

## 主机管理 API

### 获取主机列表

**端点**: `GET /api/v1/hosts`

**查询参数**:
- `page` (int, 可选): 页码，默认 1
- `page_size` (int, 可选): 每页数量，默认 20
- `keyword` (string, 可选): 搜索关键词
- `status` (string, 可选): 主机状态 (online, offline)
- `os_family` (string, 可选): 操作系统族 (rocky, centos, debian)

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 100,
    "items": [
      {
        "id": "host-001",
        "hostname": "web-server-01",
        "ip": "192.168.1.10",
        "os_family": "rocky",
        "os_version": "9.2",
        "baseline_score": 85.5,
        "status": "online",
        "last_heartbeat": "2025-12-29T10:00:00Z"
      }
    ]
  }
}
```

### 获取主机详情

**端点**: `GET /api/v1/hosts/:host_id`

**路径参数**:
- `host_id` (string, 必需): 主机 ID

**响应**:
```json
{
  "code": 0,
  "data": {
    "id": "host-001",
    "hostname": "web-server-01",
    "ip": "192.168.1.10",
    "os_family": "rocky",
    "os_version": "9.2",
    "kernel_version": "5.14.0-284.11.1.el9_2.x86_64",
    "baseline_score": 85.5,
    "status": "online",
    "cpu_cores": 8,
    "memory_total": 16384,
    "disk_total": 512000,
    "created_at": "2025-12-01T10:00:00Z",
    "updated_at": "2025-12-29T10:00:00Z",
    "last_heartbeat": "2025-12-29T10:00:00Z"
  }
}
```

### 获取主机基线得分

**端点**: `GET /api/v1/hosts/:host_id/score`

**响应**:
```json
{
  "code": 0,
  "data": {
    "host_id": "host-001",
    "total_score": 85.5,
    "policy_scores": [
      {
        "policy_id": "linux-baseline-001",
        "policy_name": "Linux 系统基线",
        "score": 85.5,
        "passed_rules": 18,
        "total_rules": 24,
        "last_check": "2025-12-29T10:00:00Z"
      }
    ]
  }
}
```

### 删除主机

**端点**: `DELETE /api/v1/hosts/:host_id`

**路径参数**:
- `host_id` (string, 必需): 主机 ID

**响应**:
```json
{
  "code": 0,
  "message": "主机删除成功"
}
```

---

## 策略管理 API

### 获取策略列表

**端点**: `GET /api/v1/policies`

**查询参数**:
- `page` (int, 可选): 页码
- `page_size` (int, 可选): 每页数量
- `enabled` (bool, 可选): 是否启用

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 10,
    "items": [
      {
        "id": "linux-baseline-001",
        "name": "Linux 系统基线",
        "description": "Linux 操作系统安全基线检查",
        "os_family": ["rocky", "centos"],
        "enabled": true,
        "rule_count": 24,
        "created_at": "2025-12-01T10:00:00Z"
      }
    ]
  }
}
```

### 获取策略详情

**端点**: `GET /api/v1/policies/:policy_id`

**响应**:
```json
{
  "code": 0,
  "data": {
    "id": "linux-baseline-001",
    "name": "Linux 系统基线",
    "description": "Linux 操作系统安全基线检查",
    "os_family": ["rocky", "centos"],
    "enabled": true,
    "rules": [
      {
        "rule_id": "SSH_001",
        "title": "SSH 禁止 root 登录",
        "description": "检查 SSH 配置是否禁止 root 用户登录",
        "severity": "high",
        "check_config": {
          "type": "file_kv",
          "path": "/etc/ssh/sshd_config",
          "key": "PermitRootLogin",
          "expected": "no"
        }
      }
    ],
    "created_at": "2025-12-01T10:00:00Z",
    "updated_at": "2025-12-29T10:00:00Z"
  }
}
```

### 创建策略

**端点**: `POST /api/v1/policies`

**请求体**:
```json
{
  "id": "linux-baseline-002",
  "name": "CentOS 7 基线",
  "description": "CentOS 7 安全基线检查",
  "os_family": ["centos"],
  "enabled": true,
  "rules": [
    {
      "rule_id": "SSH_001",
      "title": "SSH 禁止 root 登录",
      "description": "检查 SSH 配置",
      "severity": "high",
      "check_config": {
        "type": "file_kv",
        "path": "/etc/ssh/sshd_config",
        "key": "PermitRootLogin",
        "expected": "no"
      }
    }
  ]
}
```

**响应**:
```json
{
  "code": 0,
  "data": {
    "id": "linux-baseline-002",
    "name": "CentOS 7 基线",
    "enabled": true,
    "created_at": "2025-12-29T10:00:00Z"
  }
}
```

### 更新策略

**端点**: `PUT /api/v1/policies/:policy_id`

**请求体**: 同创建策略

**响应**: 同创建策略

### 删除策略

**端点**: `DELETE /api/v1/policies/:policy_id`

**响应**:
```json
{
  "code": 0,
  "message": "策略删除成功"
}
```

### 获取策略统计信息

**端点**: `GET /api/v1/policies/:policy_id/statistics`

**响应**:
```json
{
  "code": 0,
  "data": {
    "policy_id": "linux-baseline-001",
    "total_hosts": 50,
    "passed_hosts": 35,
    "failed_hosts": 10,
    "pending_hosts": 5,
    "average_score": 82.5,
    "last_updated": "2025-12-29T10:00:00Z"
  }
}
```

---

## 任务管理 API

### 获取任务列表

**端点**: `GET /api/v1/tasks`

**查询参数**:
- `page` (int, 可选): 页码
- `page_size` (int, 可选): 每页数量
- `status` (string, 可选): 任务状态 (pending, running, completed, failed)

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 20,
    "items": [
      {
        "id": "task-001",
        "name": "全量基线扫描",
        "type": "baseline",
        "policy_id": "linux-baseline-001",
        "status": "completed",
        "target_hosts": 50,
        "completed_hosts": 50,
        "created_at": "2025-12-29T09:00:00Z",
        "started_at": "2025-12-29T09:00:01Z",
        "finished_at": "2025-12-29T09:05:30Z"
      }
    ]
  }
}
```

### 创建任务

**端点**: `POST /api/v1/tasks`

**请求体**:
```json
{
  "name": "全量基线扫描",
  "type": "baseline",
  "policy_id": "linux-baseline-001",
  "targets": {
    "type": "all"
  }
}
```

或指定主机:
```json
{
  "name": "部分主机扫描",
  "type": "baseline",
  "policy_id": "linux-baseline-001",
  "targets": {
    "type": "hosts",
    "host_ids": ["host-001", "host-002"]
  }
}
```

**响应**:
```json
{
  "code": 0,
  "data": {
    "id": "task-002",
    "name": "全量基线扫描",
    "status": "pending",
    "created_at": "2025-12-29T10:00:00Z"
  }
}
```

### 执行任务

**端点**: `POST /api/v1/tasks/:task_id/run`

**响应**:
```json
{
  "code": 0,
  "message": "任务已提交执行"
}
```

---

## 结果查询 API

### 获取检测结果

**端点**: `GET /api/v1/results`

**查询参数**:
- `page` (int, 可选): 页码
- `page_size` (int, 可选): 每页数量
- `host_id` (string, 可选): 主机 ID
- `policy_id` (string, 可选): 策略 ID
- `status` (string, 可选): 结果状态 (pass, fail, error)
- `severity` (string, 可选): 严重程度 (high, medium, low)

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 100,
    "items": [
      {
        "id": "result-001",
        "host_id": "host-001",
        "policy_id": "linux-baseline-001",
        "rule_id": "SSH_001",
        "status": "fail",
        "severity": "high",
        "actual_value": "yes",
        "expected_value": "no",
        "check_time": "2025-12-29T10:00:00Z"
      }
    ]
  }
}
```

### 获取主机基线摘要

**端点**: `GET /api/v1/results/host/:host_id/summary`

**响应**:
```json
{
  "code": 0,
  "data": {
    "host_id": "host-001",
    "total_checks": 24,
    "passed_checks": 18,
    "failed_checks": 5,
    "error_checks": 1,
    "baseline_score": 85.5,
    "last_check": "2025-12-29T10:00:00Z"
  }
}
```

---

## 资产数据 API

### 获取进程列表

**端点**: `GET /api/v1/assets/processes`

**查询参数**:
- `host_id` (string, 必需): 主机 ID
- `page` (int, 可选): 页码
- `page_size` (int, 可选): 每页数量

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 150,
    "items": [
      {
        "pid": 1234,
        "name": "nginx",
        "cmdline": "/usr/sbin/nginx -g daemon off;",
        "user": "nginx",
        "cpu_percent": 2.5,
        "memory_percent": 1.2,
        "status": "running"
      }
    ]
  }
}
```

### 获取端口列表

**端点**: `GET /api/v1/assets/ports`

**查询参数**:
- `host_id` (string, 必需): 主机 ID

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 10,
    "items": [
      {
        "port": 80,
        "protocol": "tcp",
        "state": "listening",
        "process": "nginx"
      }
    ]
  }
}
```

### 获取用户列表

**端点**: `GET /api/v1/assets/users`

**查询参数**:
- `host_id` (string, 必需): 主机 ID

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 25,
    "items": [
      {
        "username": "root",
        "uid": 0,
        "gid": 0,
        "home": "/root",
        "shell": "/bin/bash"
      }
    ]
  }
}
```

---

## Dashboard API

### 获取统计数据

**端点**: `GET /api/v1/dashboard/stats`

**响应**:
```json
{
  "code": 0,
  "data": {
    "total_hosts": 50,
    "online_hosts": 45,
    "offline_hosts": 5,
    "total_policies": 5,
    "enabled_policies": 3,
    "total_tasks": 100,
    "running_tasks": 2,
    "average_baseline_score": 82.5,
    "failed_checks": 125,
    "high_severity_issues": 15,
    "medium_severity_issues": 45,
    "low_severity_issues": 65
  }
}
```

---

## 组件管理 API

### 获取组件列表

**端点**: `GET /api/v1/components`

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 2,
    "items": [
      {
        "id": "agent",
        "name": "Agent",
        "type": "agent",
        "latest_version": "1.0.0",
        "description": "安全检测代理"
      }
    ]
  }
}
```

### 获取组件版本列表

**端点**: `GET /api/v1/components/:component_id/versions`

**响应**:
```json
{
  "code": 0,
  "data": {
    "total": 5,
    "items": [
      {
        "id": "v1.0.0",
        "version": "1.0.0",
        "component_id": "agent",
        "released_at": "2025-12-29T10:00:00Z",
        "packages": [
          {
            "id": "pkg-001",
            "os_family": "rocky",
            "os_version": "9",
            "arch": "x86_64",
            "file_name": "mxsec-agent-1.0.0-1.el9.x86_64.rpm",
            "file_size": 10485760,
            "checksum": "sha256:abc123..."
          }
        ]
      }
    ]
  }
}
```

---

## 错误响应格式

所有错误响应遵循统一格式:

```json
{
  "code": 400,
  "message": "详细错误说明"
}
```

### HTTP 状态码

| 状态码 | 说明 | 使用场景 |
|--------|------|---------|
| 200 | OK | 成功 |
| 201 | Created | 资源创建成功 |
| 400 | Bad Request | 请求参数错误 |
| 401 | Unauthorized | 未认证 |
| 403 | Forbidden | 无权限 |
| 404 | Not Found | 资源不存在 |
| 409 | Conflict | 资源冲突（如 ID 重复） |
| 500 | Internal Server Error | 服务器错误 |

---

**文档维护者**: Claude Code
**最后更新**: 2025-12-29
