# API 文档

## 概览

- **Base URL**: `/api/v1`
- **认证**: JWT Bearer Token（login 接口除外）
- **请求头**: `Authorization: Bearer <token>`，`Content-Type: application/json`

**统一响应格式**：

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

**分页参数**（适用于列表接口）：`page`（页码，默认 1）、`page_size`（每页数量，默认 20）

---

## 认证

### 登录

```
POST /api/v1/auth/login
```

请求：
```json
{"username": "admin", "password": "admin123"}
```

响应：
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

## Dashboard

### 统计概览

```
GET /api/v1/dashboard/stats
```

响应：主机总数、在线/离线数量、基线通过率、风险主机数、漏洞数、告警数等。

### 趋势数据

```
GET /api/v1/dashboard/trends?range=7d
```

---

## 主机管理

### 主机列表

```
GET /api/v1/hosts?page=1&page_size=20&status=online&search=keyword
```

响应：
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
        "last_heartbeat": "2026-01-01T00:00:00Z"
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

### 主机风险分布

```
GET /api/v1/hosts/risk-distribution
```

### 主机监控指标

```
GET /api/v1/hosts/:host_id/metrics?start_time=xxx&end_time=xxx
```

数据源为 Prometheus，返回 CPU、内存、磁盘、网络等指标曲线。

### 主机插件列表

```
GET /api/v1/hosts/:host_id/plugins
```

### 主机资产列表

```
GET /api/v1/hosts/:host_id/assets?type=process
```

支持的 type：process / port / user / package / container / app / network / disk / kernel_module / service / crontab

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

请求：
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

响应：通过率、检查主机数、检查项数、风险项数、最近检查时间。

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

请求：
```json
{
  "rule_id": "SSH_001",
  "category": "ssh",
  "title": "禁止空密码登录",
  "severity": "high",
  "check_config": {"conditions": "all", "checks": [...]},
  "fix_config": {"suggestion": "编辑 /etc/ssh/sshd_config..."}
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

请求：
```json
{
  "name": "全量扫描",
  "policy_id": "LINUX_SSH_BASELINE",
  "target_type": "all"
}
```

`target_type` 可选值：`all` / `host_ids` / `os_family`

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

响应：
```json
{"score": 85.5, "total": 100, "pass": 85, "fail": 12, "error": 3}
```

### 结果摘要

```
GET /api/v1/results/summary?task_id=xxx
```

---

## 基线修复

### 可修复项列表

```
GET /api/v1/fix/items?host_id=xxx
```

### 创建修复任务

```
POST /api/v1/fix/tasks
```

### 修复任务详情

```
GET /api/v1/fix/tasks/:task_id
```

---

## 告警管理

### 告警列表

```
GET /api/v1/alarms?severity=high&status=pending&page=1&page_size=20
```

### 告警详情

```
GET /api/v1/alarms/:alarm_id
```

### 告警处置

```
PUT /api/v1/alarms/:alarm_id
```

请求：
```json
{"status": "resolved", "comment": "已确认并处理"}
```

### 批量处置

```
POST /api/v1/alarms/batch
```

### 告警白名单

```
GET /api/v1/alarm-whitelist
POST /api/v1/alarm-whitelist
DELETE /api/v1/alarm-whitelist/:id
```

### 告警溯源

```
GET /api/v1/alarms/:alarm_id/context
```

响应：上下文事件、时间线、进程树、MITRE ATT&CK 映射。

---

## FIM 文件完整性监控

### FIM 事件列表

```
GET /api/v1/fim/events?host_id=xxx&severity=high&page=1&page_size=20
```

### FIM 策略

```
GET /api/v1/fim/policies
POST /api/v1/fim/policies
PUT /api/v1/fim/policies/:policy_id
DELETE /api/v1/fim/policies/:policy_id
```

### FIM 任务

```
POST /api/v1/fim/tasks
GET /api/v1/fim/tasks/:task_id
```

---

## 漏洞管理

### 漏洞列表

```
GET /api/v1/vulnerabilities?severity=critical&page=1&page_size=20
```

### 漏洞详情

```
GET /api/v1/vulnerabilities/:vuln_id
```

### 主机漏洞

```
GET /api/v1/hosts/:host_id/vulnerabilities
```

### SBOM 导出

```
GET /api/v1/hosts/:host_id/sbom?format=json
```

---

## 病毒查杀

### 扫描任务

```
POST /api/v1/antivirus/tasks
GET /api/v1/antivirus/tasks?page=1&page_size=20
GET /api/v1/antivirus/tasks/:task_id
```

### 扫描结果

```
GET /api/v1/antivirus/results?host_id=xxx&page=1&page_size=20
```

### 隔离箱

```
GET /api/v1/antivirus/quarantine?page=1&page_size=20
POST /api/v1/antivirus/quarantine/:id/restore
DELETE /api/v1/antivirus/quarantine/:id
```

---

## 容器安全

### 集群管理

```
GET /api/v1/kube/clusters
POST /api/v1/kube/clusters
GET /api/v1/kube/clusters/:cluster_id
DELETE /api/v1/kube/clusters/:cluster_id
```

### 容器告警

```
GET /api/v1/kube/alarms?cluster_id=xxx&page=1&page_size=20
```

### 容器基线

```
POST /api/v1/kube/baseline/tasks
GET /api/v1/kube/baseline/results?cluster_id=xxx
```

### 容器白名单

```
GET /api/v1/kube/whitelist
POST /api/v1/kube/whitelist
DELETE /api/v1/kube/whitelist/:id
```

---

## 资产管理

### 资产统计

```
GET /api/v1/assets/stats
```

### 资产列表（按类型）

```
GET /api/v1/assets?type=process&page=1&page_size=20
```

### 资产导出

```
GET /api/v1/assets/export?type=package&format=csv
```

---

## 系统监控

### 服务健康

```
GET /api/v1/monitor/services
```

### 主机性能指标

```
GET /api/v1/monitor/host-metrics?host_id=xxx&metric=cpu&range=24h
```

---

## 服务发现

### AC 实例列表

```
GET /api/v1/discovery/agentcenter
```

返回健康的 AgentCenter 实例列表（需认证）。

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

## 审计日志

```
GET /api/v1/audit-logs?page=1&page_size=20&action=login&user_id=xxx
```

---

## 用户管理

```
GET /api/v1/users
POST /api/v1/users
PUT /api/v1/users/:user_id
DELETE /api/v1/users/:user_id
PUT /api/v1/users/:user_id/password
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
