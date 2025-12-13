# CMDB 对接快速开始

> 这是 CMDB 对接的快速开始指南。详细文档请参考 [CMDB_INTEGRATION.md](./CMDB_INTEGRATION.md)。

---

## 5 分钟快速开始

### 1. 获取 API Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}'

# 响应
# {"code":0,"data":{"token":"eyJhbGc...","expires_at":"2025-12-13T10:00:00Z"}}
```

保存返回的 `token`，用于后续请求。

---

### 2. 获取主机列表

```bash
TOKEN="your_token_here"

curl -X GET "http://localhost:8080/api/v1/hosts?page=1&limit=10" \
  -H "Authorization: Bearer ${TOKEN}"

# 响应包含主机列表，每个主机有 host_id、hostname、os_family 等字段
```

---

### 3. 拉取资产数据

对于每个主机，可以拉取三类资产数据：

```bash
HOST_ID="host-001"

# 获取进程列表
curl -X GET "http://localhost:8080/api/v1/assets/processes?host_id=${HOST_ID}&limit=100" \
  -H "Authorization: Bearer ${TOKEN}"

# 获取端口列表
curl -X GET "http://localhost:8080/api/v1/assets/ports?host_id=${HOST_ID}&limit=100" \
  -H "Authorization: Bearer ${TOKEN}"

# 获取用户列表
curl -X GET "http://localhost:8080/api/v1/assets/users?host_id=${HOST_ID}&limit=100" \
  -H "Authorization: Bearer ${TOKEN}"
```

---

### 4. 执行基线检查任务

```bash
# 创建任务
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{
    "name":"基线检查",
    "type":"baseline",
    "policy_id":"linux-baseline-001",
    "targets":{"type":"all"}
  }'

# 响应包含 task_id

# 执行任务
TASK_ID="task-001"
curl -X POST "http://localhost:8080/api/v1/tasks/${TASK_ID}/run" \
  -H "Authorization: Bearer ${TOKEN}"
```

---

### 5. 查询检查结果

```bash
# 查询所有失败的检查结果
curl -X GET "http://localhost:8080/api/v1/results?status=fail&limit=100" \
  -H "Authorization: Bearer ${TOKEN}"

# 查询主机基线得分
HOST_ID="host-001"
curl -X GET "http://localhost:8080/api/v1/results/host/${HOST_ID}/score" \
  -H "Authorization: Bearer ${TOKEN}"

# 查询主机基线摘要（包括失败规则详情）
curl -X GET "http://localhost:8080/api/v1/results/host/${HOST_ID}/summary" \
  -H "Authorization: Bearer ${TOKEN}"
```

---

## 核心 API 端点速查表

| 操作 | 方法 | 端点 | 说明 |
|------|------|------|------|
| 登录 | POST | `/api/v1/auth/login` | 获取 Token |
| 主机列表 | GET | `/api/v1/hosts` | 获取所有主机 |
| 主机详情 | GET | `/api/v1/hosts/{host_id}` | 获取单个主机信息 |
| 进程列表 | GET | `/api/v1/assets/processes` | 获取主机进程 |
| 端口列表 | GET | `/api/v1/assets/ports` | 获取主机端口 |
| 用户列表 | GET | `/api/v1/assets/users` | 获取主机用户 |
| 创建任务 | POST | `/api/v1/tasks` | 创建基线检查任务 |
| 任务列表 | GET | `/api/v1/tasks` | 获取所有任务 |
| 执行任务 | POST | `/api/v1/tasks/{task_id}/run` | 执行指定任务 |
| 检查结果 | GET | `/api/v1/results` | 查询检查结果 |
| 主机得分 | GET | `/api/v1/results/host/{host_id}/score` | 获取主机基线得分 |
| 主机摘要 | GET | `/api/v1/results/host/{host_id}/summary` | 获取主机基线摘要 |

---

## 数据模型速查表

### 主机对象

```json
{
  "host_id": "string - 唯一标识",
  "hostname": "string - 主机名",
  "os_family": "string - OS族(rocky/centos/debian)",
  "os_version": "string - OS版本",
  "ipv4": "string - IPv4地址",
  "status": "string - 在线状态(online/offline)",
  "baseline_score": "number - 基线得分(0-100)",
  "last_heartbeat": "string - ISO8601时间戳"
}
```

### 进程对象

```json
{
  "host_id": "string",
  "pid": "int - 进程ID",
  "name": "string - 进程名",
  "binary_path": "string - 二进制路径",
  "md5": "string - MD5哈希",
  "cmd_line": "string - 命令行",
  "owner": "string - 进程所有者",
  "is_container": "bool - 是否容器化"
}
```

### 端口对象

```json
{
  "host_id": "string",
  "protocol": "string - tcp/udp",
  "port": "int - 端口号",
  "listen_address": "string - 监听地址",
  "pid": "int - 关联PID",
  "process_name": "string - 进程名",
  "state": "string - 连接状态"
}
```

### 用户对象

```json
{
  "host_id": "string",
  "username": "string - 用户名",
  "uid": "int - 用户ID",
  "gid": "int - 组ID",
  "home_dir": "string - 家目录",
  "shell": "string - 登录Shell",
  "is_login": "bool - 是否允许登录"
}
```

### 任务对象

```json
{
  "task_id": "string - 任务ID",
  "name": "string - 任务名称",
  "type": "string - baseline",
  "policy_id": "string - 关联策略ID",
  "status": "string - 任务状态(created/running/completed/failed)",
  "target_hosts": "array - 目标主机ID列表"
}
```

### 检查结果对象

```json
{
  "result_id": "string - 结果ID",
  "host_id": "string",
  "policy_id": "string",
  "rule_id": "string - 规则ID",
  "rule_title": "string - 规则标题",
  "status": "string - 检查结果(pass/fail/error)",
  "severity": "string - 风险等级(low/medium/high/critical)",
  "expected": "string - 期望值",
  "actual": "string - 实际值"
}
```

---

## Python 集成示例

```python
import requests
import time

class CMDB:
    def __init__(self, base_url="http://localhost:8080"):
        self.base_url = base_url
        self.token = None

    def login(self, username="admin", password="admin"):
        resp = requests.post(f"{self.base_url}/api/v1/auth/login",
                           json={"username": username, "password": password})
        if resp.status_code == 200:
            self.token = resp.json()['data']['token']

    def _get(self, endpoint, params=None):
        headers = {"Authorization": f"Bearer {self.token}"}
        return requests.get(f"{self.base_url}{endpoint}", headers=headers, params=params).json()

    def _post(self, endpoint, data=None):
        headers = {"Authorization": f"Bearer {self.token}"}
        return requests.post(f"{self.base_url}{endpoint}", headers=headers, json=data).json()

    # 主机管理
    def get_hosts(self):
        return self._get("/api/v1/hosts", {"limit": 100})['data']['hosts']

    # 资产数据
    def get_processes(self, host_id):
        return self._get("/api/v1/assets/processes", {"host_id": host_id, "limit": 100})['data']['processes']

    def get_ports(self, host_id):
        return self._get("/api/v1/assets/ports", {"host_id": host_id, "limit": 100})['data']['ports']

    def get_users(self, host_id):
        return self._get("/api/v1/assets/users", {"host_id": host_id, "limit": 100})['data']['users']

    # 任务管理
    def create_task(self, name, policy_id):
        return self._post("/api/v1/tasks", {
            "name": name,
            "type": "baseline",
            "policy_id": policy_id,
            "targets": {"type": "all"}
        })['data']

    def run_task(self, task_id):
        return self._post(f"/api/v1/tasks/{task_id}/run")

    def get_results(self, **filters):
        return self._get("/api/v1/results", {**filters, "limit": 100})['data']['results']

    def get_host_score(self, host_id):
        return self._get(f"/api/v1/results/host/{host_id}/score")['data']

# 使用示例
cmdb = CMDB()
cmdb.login()

# 同步所有主机
hosts = cmdb.get_hosts()
for host in hosts:
    print(f"Host: {host['hostname']} ({host['host_id']})")

    # 同步资产
    processes = cmdb.get_processes(host['host_id'])
    ports = cmdb.get_ports(host['host_id'])
    users = cmdb.get_users(host['host_id'])

    print(f"  Processes: {len(processes)}, Ports: {len(ports)}, Users: {len(users)}")

# 执行任务
task = cmdb.create_task("Weekly Scan", "linux-baseline-001")
cmdb.run_task(task['task_id'])

# 等待执行完成
time.sleep(30)

# 查看结果
results = cmdb.get_results(status="fail")
print(f"Failed checks: {len(results)}")

# 查看得分
for host in hosts:
    score = cmdb.get_host_score(host['host_id'])
    print(f"{host['hostname']}: {score['baseline_score']}")
```

---

## 集成建议

### 定时同步任务

推荐配置：
- **资产数据同步**：每 30 分钟执行一次（与 Collector Plugin 采集频率一致）
- **基线检查**：每周执行一次（可选：周一凌晨 02:00）
- **结果查询**：每天早上 08:00 执行一次（生成日报）

### 错误处理

```python
try:
    cmdb.login()
except Exception as e:
    print(f"Login failed: {e}")
    exit(1)

try:
    hosts = cmdb.get_hosts()
except Exception as e:
    print(f"Failed to get hosts: {e}")
    # 重试逻辑

try:
    results = cmdb.get_results(status="fail")
except Exception as e:
    print(f"Failed to get results: {e}")
    # 继续处理其他任务
```

---

## 常见集成问题

**Q: 资产数据什么时候开始可用？**

A: Agent 首次心跳后自动创建主机，Collector Plugin 在 5-10 分钟内完成首次采集。

**Q: 任务执行需要多长时间？**

A: 通常 20-90 秒，取决于检查项数量。建议轮询间隔 10-15 秒。

**Q: 如何处理 Token 过期？**

A: 收到 401 响应时重新登录获取新 Token。

**Q: 数据如何持久化？**

A: 所有数据存储在 MySQL 中，支持长期历史查询。

---

## 更多帮助

- 详细 API 文档：[CMDB_INTEGRATION.md](./CMDB_INTEGRATION.md)
- 项目 README：[README.md](../README.md)
- 开发指南：[CLAUDE.md](../CLAUDE.md)

---

**文档版本**：v1.0
**最后更新**：2025-12-12
