# 快速测试 Agent 指南

> 本文档帮助您快速启动一个测试 Agent，用于生成测试数据验证报表功能。

---

## 前置条件

1. **Server 已启动**
   - AgentCenter (gRPC Server) 运行在 `localhost:6751`
   - Manager (HTTP API Server) 运行在 `localhost:8080`
   - 数据库已初始化

2. **证书已生成**
   ```bash
   ./scripts/generate-certs.sh
   ```

---

## 快速启动方式

### 方式1：使用 Docker Compose（推荐，适用于 macOS/Windows/Linux）

```bash
# 进入 Docker Compose 目录
cd deploy/dev

# 启动 Agent 容器
docker-compose up -d agent

# 查看日志
docker-compose logs -f agent

# 停止 Agent
docker-compose stop agent
```

**优点：**
- ✅ 跨平台支持（macOS、Windows、Linux）
- ✅ 无需本地编译 Linux 二进制
- ✅ 环境隔离，不影响本地系统
- ✅ 自动处理依赖和配置

### 方式2：手动构建和启动（Linux 系统）

```bash
# 使用 Makefile 构建 Agent
make build-agent SERVER_HOST=localhost:6751 VERSION=dev-test

### 方式3：手动构建和启动（Linux 系统）

```bash
# 1. 构建 Agent
export BLS_SERVER_HOST=localhost:6751
export BLS_VERSION=dev-test
bash scripts/build-agent.sh

# 2. 创建目录
mkdir -p /tmp/mxcsec-agent-test/{lib,log,certs}

# 3. 复制证书（如果需要）
cp -r certs/* /tmp/mxcsec-agent-test/lib/certs/

# 4. 启动 Agent
export BLS_SERVER_HOST=localhost:6751
dist/agent/mxcsec-agent-linux-amd64
```

---

## 验证 Agent 运行

### 1. 检查 Agent 日志

```bash
# 查看实时日志
tail -f /tmp/mxcsec-agent-test/log/agent.log

# 或查看标准输出（如果使用脚本启动）
```

### 2. 检查 Server 端

- **查看主机列表**：访问 `http://localhost:8080/api/v1/hosts`
- **查看 Dashboard**：访问 `http://localhost:3000` 登录后查看 Dashboard
- **查看报表**：访问 `http://localhost:3000/system/reports`

### 3. 检查数据库

```bash
# 连接数据库
mysql -u root -p mxsec_platform

# 查看主机表
SELECT * FROM hosts;

# 查看心跳数据
SELECT host_id, hostname, status, last_heartbeat FROM hosts ORDER BY last_heartbeat DESC;

# 查看基线检查结果
SELECT COUNT(*) FROM scan_results;
```

---

## 生成测试数据

### 1. 创建扫描任务

通过 UI 或 API 创建扫描任务：

```bash
# 使用 curl 创建任务
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "name": "测试扫描任务",
    "type": "baseline_scan",
    "target_type": "all",
    "policy_id": "your-policy-id"
  }'
```

### 2. 执行任务

```bash
# 执行任务
curl -X POST http://localhost:8080/api/v1/tasks/TASK_ID/run \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### 3. 查看结果

- 在 UI 中查看主机详情页面的基线检查结果
- 在报表页面查看统计数据

---

## 常见问题

### Agent 无法连接 Server

**检查项：**
1. Server 是否正在运行？
   ```bash
   # 检查 AgentCenter
   lsof -i :6751
   
   # 检查 Manager
   lsof -i :8080
   ```

2. 证书是否正确？
   ```bash
   # 检查证书文件
   ls -la certs/
   ```

3. Server 地址是否正确？
   ```bash
   # 检查环境变量
   echo $BLS_SERVER_HOST
   ```

### Agent 启动后立即退出

**可能原因：**
1. Agent 主程序未实现
2. 配置文件错误
3. 证书路径错误

**解决方法：**
- 查看日志文件：`/tmp/mxcsec-agent-test/log/agent.log`
- 检查 Agent 代码实现

### 没有数据生成

**检查项：**
1. Agent 是否成功连接 Server？
   - 查看 Server 日志
   - 查看数据库 `hosts` 表

2. 是否有扫描任务？
   - 查看 `scan_tasks` 表
   - 通过 UI 创建任务

3. 插件是否正常运行？
   - 查看 Agent 日志中的插件状态
   - 检查插件配置

---

## 下一步

- 查看 [Agent 部署指南](../deployment/agent-deployment.md) 了解生产环境部署
- 查看 [开发指南](../development/development-guide.md) 了解如何开发新功能
- 查看 [故障排查指南](../development/troubleshooting.md) 解决常见问题

---

## 提示

### Docker 方式（macOS/Windows）
- Agent 数据存储在 Docker volume `agent_data` 中
- 查看日志：`cd deploy/dev && docker-compose logs -f agent`
- 停止 Agent：`cd deploy/dev && docker-compose stop agent`
- 删除 Agent 容器和数据：`cd deploy/dev && docker-compose rm -f agent && docker volume rm deploy_dev_agent_data`

### 本地方式（Linux）
- 测试 Agent 的数据目录在 `/tmp/mxcsec-agent-test/`，可以随时删除重新开始
- 使用 `Ctrl+C` 停止 Agent
- 建议在单独的终端窗口运行 Agent，方便查看日志
