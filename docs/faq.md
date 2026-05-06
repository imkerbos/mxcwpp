# 常见问题

## Server 端

### AgentCenter 无法启动

1. 检查端口占用：`lsof -i :6751`
2. 检查证书文件：`ls -la deploy/certs/`，确认 `ca.crt`、`server.crt`、`server.key` 存在且权限正确
3. 检查配置：`deploy/config/server.yaml` 中 `grpc.port` 是否与 `.env` 一致
4. 查看日志：`docker compose logs agentcenter`

### Manager API 返回 500

1. 查看日志定位错误：`docker compose logs manager | grep ERROR`
2. 检查 MySQL 连接：确认 MySQL 服务运行且 `.env` 中的凭证正确
3. 检查表结构：重启 Manager 触发 Gorm AutoMigrate 自动建表/补字段

### Consumer 持续报错

1. 检查 Kafka 连通性：确认 Broker 地址与 `.env` 配置一致
2. 检查 DLQ 堆积：观察 `*.dlq` Topic 是否有大量失败消息
3. 检查 ClickHouse 连接：Consumer 写入 ClickHouse 失败时会进 DLQ
4. 查看日志：`docker compose logs consumer | grep ERROR`

### 数据库连接失败

1. 确认 MySQL 服务运行：`docker compose ps mysql`
2. 检查凭证：`deploy/.env` 中的 `MYSQL_USER` / `MYSQL_PASSWORD`
3. 测试连接：`docker compose exec mysql mysql -u mxsec -p mxsec`

### 服务启动顺序异常

MySQL / Redis / Kafka / ClickHouse 需要先就绪，控制面组件有健康检查依赖。如果数据库尚未初始化完成，Manager / AgentCenter / Consumer 会自动重启重试。

如果持续失败，手动检查依赖服务状态：

```bash
docker compose ps
docker compose logs mysql | tail -20
docker compose logs redis | tail -20
docker compose logs kafka-1 | tail -20
```

## Agent 端

### Agent 无法连接 Server

1. **检查地址**：Agent 构建时嵌入的 `SERVER_HOST` 是否指向正确的 AC 入口（生产环境应为 L4 LB 地址）
2. **检查网络**：`nc -zv <agentcenter-host> 6751`
3. **检查防火墙**：确认 6751 端口开放
4. **检查证书**：
   - 首次连接：AgentCenter 自动下发证书，确认服务端 `deploy/certs/` 完整
   - 后续连接：检查 `/var/lib/mxsec-agent/certs/` 下 `ca.crt`、`client.crt`、`client.key`
   - mTLS 连续失败 3 次后 Agent 会暂时降级为不安全模式重新取证
5. **检查 DNS**：Agent 配置的主机名能否正确解析

### 插件未启动

1. 检查插件文件是否存在：`ls -la /var/lib/mxsec-agent/plugin/`
2. 检查执行权限：`chmod +x /var/lib/mxsec-agent/plugin/baseline`
3. 查看 Agent 日志：`tail -f /var/log/mxsec-agent/agent.log | grep plugin`
4. 确认 Server 已下发插件配置（插件版本和 sha256 需匹配）

### 没有检测数据上报

1. 确认 Agent 在线：查看管理界面主机列表或 `GET /api/v1/hosts`
2. 确认已创建扫描任务：查看 `GET /api/v1/tasks`
3. 检查插件运行状态：Agent 日志中搜索插件名称
4. 检查 AgentCenter 是否收到数据：`docker compose logs agentcenter | grep "baseline\|8000"`
5. 检查 Consumer 是否正常消费：`docker compose logs consumer | grep "write"`

### Agent 更新方式

Agent 支持三种更新方式：

```bash
# 服务端推送更新（管理界面触发）
# CLI 主动更新
mxsec-agent --update
mxsec-agent --update --server http://manager:8080
# 本地文件更新
mxsec-agent --update --file ./mxsec-agent-1.1.0.rpm
```

## 前端

### 无法连接 API

1. 确认 Manager 运行：`curl http://localhost/api/v1/health`
2. 检查 Nginx 代理配置：`deploy/config/nginx.conf` 中 `/api/*` 的 upstream 是否指向 Manager
3. 检查 CORS 配置

### 登录后立即跳回登录页

1. 检查 Token 存储：浏览器 DevTools → Application → localStorage
2. 检查 login 接口响应是否返回了 token
3. 检查 Nginx 是否正确代理了 API 请求（注意 `/api/` 结尾的斜杠）

### 页面空白

1. 检查浏览器 Console（F12）是否有 JavaScript 错误
2. 确认前端构建成功：`cd ui && npm run build`
3. 检查 Nginx 静态文件路径配置

## 数据库

### 查询慢

1. 检查关键索引：
   - `scan_results`：`(host_id, rule_id, checked_at DESC)`、`(host_id, checked_at DESC)`
   - `scan_tasks`：`(status, created_at)`
2. 检查数据量：`SELECT COUNT(*) FROM scan_results`，超大表考虑清理历史数据
3. 开启慢查询日志：`SET GLOBAL slow_query_log = 'ON'`

### 表不存在

重启 Manager 或 AgentCenter 触发 Gorm AutoMigrate 自动建表。

### ClickHouse 写入积压

1. 检查 ClickHouse 磁盘空间
2. 检查 parts 数量：`SELECT count() FROM system.parts WHERE active AND database = 'mxsec'`
3. 如果 parts 过多，等待后台 merge 完成或适当调大 Consumer 的批量写入间隔

## 日志位置

| 组件 | 位置 |
|------|------|
| Manager | `docker compose logs manager` |
| AgentCenter | `docker compose logs agentcenter` |
| Consumer | `docker compose logs consumer` |
| Nginx | `docker compose logs ui` |
| MySQL | `docker compose logs mysql` |
| Agent | `/var/log/mxsec-agent/agent.log` |
| 插件 | `/var/log/mxsec-agent/<plugin-name>.log` |

## 常见错误码

| 错误 | 原因 | 处理 |
|------|------|------|
| 401 Unauthorized | Token 过期或无效 | 重新登录获取 Token |
| 403 Forbidden | 权限不足 | 检查用户角色 |
| 404 Not Found | 资源不存在或 URL 错误 | 检查 API 路径 |
| 500 Internal Error | 服务端异常 | 查看 Manager 日志 |
| 502 Bad Gateway | Nginx 无法连接后端 | 检查 Manager / AgentCenter 是否运行 |
| gRPC UNAVAILABLE | Agent 无法连接 AC | 检查网络、证书、端口 |
