# API 参考

## 概览

- **Base URL**: `/api/v1`
- **认证**: JWT Bearer Token（除 login 外所有接口需要）
- **请求头**: `Authorization: Bearer <token>`, `Content-Type: application/json`

**响应格式**:

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

---

## 认证

### 登录

```
POST /api/v1/auth/login
```

请求:
```json
{"username": "admin", "password": "admin123"}
```

响应:
```json
{"code": 0, "data": {"token": "eyJhbG...", "user": {"username": "admin", "role": "admin"}}}
```

### 获取当前用户

```
GET /api/v1/auth/me
```

### 登出

```
POST /api/v1/auth/logout
```

---

## 主机管理

### 主机列表

```
GET /api/v1/hosts?page=1&page_size=20&status=online&search=keyword
```

响应:
```json
{
  "code": 0,
  "data": {
    "items": [
      {
        "host_id": "uuid",
        "hostname": "server-01",
        "os_family": "rocky",
        "os_version": "9.2",
        "status": "online",
        "agent_version": "1.0.0",
        "last_heartbeat": "2025-01-01T00:00:00Z"
      }
    ],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
}
```

### 主机详情

```
GET /api/v1/hosts/:host_id
```

### 主机状态分布

```
GET /api/v1/hosts/status-distribution
```

响应: `{"online": 280, "offline": 20}`

### 主机风险分布

```
GET /api/v1/hosts/risk-distribution
```

### 主机监控指标

```
GET /api/v1/hosts/:host_id/metrics?start_time=xxx&end_time=xxx
```

### 主机插件列表

```
GET /api/v1/hosts/:host_id/plugins
```

### 删除主机

```
DELETE /api/v1/hosts/:host_id
```

---

## 策略管理

### 策略列表

```
GET /api/v1/policies?page=1&page_size=20
```

### 创建策略

```
POST /api/v1/policies
```

请求:
```json
{
  "name": "Linux SSH 基线",
  "description": "SSH 安全配置检查",
  "os_family": ["rocky", "centos"],
  "os_version": ">=7",
  "enabled": true
}
```

### 策略详情

```
GET /api/v1/policies/:policy_id
```

### 更新策略

```
PUT /api/v1/policies/:policy_id
```

### 删除策略

```
DELETE /api/v1/policies/:policy_id
```

### 策略统计

```
GET /api/v1/policies/:policy_id/statistics
```

响应: 通过率、检查主机数、检查项数、风险项数、最近检查时间

### 导入策略

```
POST /api/v1/policies/import
Content-Type: multipart/form-data
```

### 导出策略

```
GET /api/v1/policies/:policy_id/export
```

---

## 规则管理

### 规则列表

```
GET /api/v1/policies/:policy_id/rules
```

### 创建规则

```
POST /api/v1/policies/:policy_id/rules
```

请求:
```json
{
  "rule_id": "SSH_001",
  "category": "ssh",
  "title": "禁止空密码登录",
  "severity": "high",
  "check_config": { "conditions": "all", "checks": [...] },
  "fix_config": { "suggestion": "编辑 /etc/ssh/sshd_config..." }
}
```

### 更新规则

```
PUT /api/v1/rules/:rule_id
```

### 删除规则

```
DELETE /api/v1/rules/:rule_id
```

---

## 扫描任务

### 任务列表

```
GET /api/v1/tasks?page=1&page_size=20&status=completed
```

### 创建任务

```
POST /api/v1/tasks
```

请求:
```json
{
  "name": "全量扫描",
  "policy_id": "LINUX_SSH_BASELINE",
  "target_type": "all"
}
```

`target_type`: `all` | `host_ids` | `os_family`

### 任务详情

```
GET /api/v1/tasks/:task_id
```

### 执行任务

```
POST /api/v1/tasks/:task_id/run
```

---

## 检测结果

### 结果列表

```
GET /api/v1/results?host_id=xxx&task_id=xxx&status=fail&page=1&page_size=20
```

### 主机基线得分

```
GET /api/v1/hosts/:host_id/score
```

响应: `{"score": 85.5, "total": 100, "pass": 85, "fail": 12, "error": 3}`

### 结果摘要

```
GET /api/v1/results/summary?task_id=xxx
```

---

## Dashboard

### 统计数据

```
GET /api/v1/dashboard/stats
```

响应: 主机总数、在线/离线 Agent、基线通过率、风险主机数等

---

## FIM 文件完整性监控

### FIM 事件列表

```
GET /api/v1/fim/events?host_id=xxx&severity=high&page=1&page_size=20
```

### FIM 策略列表

```
GET /api/v1/fim/policies
```

### 创建 FIM 策略

```
POST /api/v1/fim/policies
```

### FIM 任务

```
POST /api/v1/fim/tasks
GET /api/v1/fim/tasks/:task_id
```

---

## 系统配置

### 站点配置

```
GET /api/v1/system-config/site
PUT /api/v1/system-config/site
```

### 上传 Logo

```
POST /api/v1/system-config/upload-logo
Content-Type: multipart/form-data
```

---

## 错误码

| HTTP 状态码 | 说明 |
|------------|------|
| 200 | 成功 |
| 400 | 参数错误 |
| 401 | 未认证 / Token 过期 |
| 403 | 权限不足 |
| 404 | 资源不存在 |
| 500 | 服务器内部错误 |
