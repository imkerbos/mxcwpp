# 故障排查

## Server 端

### AgentCenter 无法启动

1. 检查端口占用: `lsof -i :6751`
2. 检查证书: `ls -la deploy/certs/`，确保 ca.crt, server.crt, server.key 存在
3. 检查配置: `deploy/config/server.yaml` 中 grpc.port 是否正确
4. 查看日志: `docker compose logs agentcenter`

### Manager API 返回 500

1. 查看日志: `docker compose logs manager | grep ERROR`
2. 检查数据库连接: 确认 MySQL 服务运行且配置正确
3. 检查表结构: 重启 AgentCenter/Manager 触发 Gorm AutoMigrate

### 数据库连接失败

1. 确认 MySQL 服务: `docker compose ps mysql`
2. 检查凭证: `deploy/.env` 中的 MYSQL_USER / MYSQL_PASSWORD
3. 测试连接: `docker compose exec mysql mysql -u mxsec -p mxsec`

## Agent 端

### Agent 无法连接 Server

1. **检查地址**: Agent 构建时嵌入的 SERVER_HOST 是否正确
2. **检查网络**: `nc -zv agentcenter-host 6751`
3. **检查防火墙**: 确保 6751 端口开放
4. **检查证书**:
   - 首次连接: AgentCenter 自动下发证书，确认 Server 端 `deploy/certs/` 完整
   - 后续连接: 检查 `/var/lib/mxsec-agent/certs/` 是否存在 ca.crt, client.crt, client.key
5. **检查 DNS**: Agent 配置的主机名能否正确解析

### 插件未启动

1. 检查插件文件: `ls -la /var/lib/mxsec-agent/plugin/`
2. 检查执行权限: `chmod +x /var/lib/mxsec-agent/plugin/baseline`
3. 查看 Agent 日志: `tail -f /var/log/mxsec-agent/agent.log | grep plugin`
4. 确认 Server 已下发插件配置

### 没有检测数据

1. Agent 是否在线: 查看 UI 主机列表或 `GET /api/v1/hosts`
2. 是否创建了扫描任务: 查看 `GET /api/v1/tasks`
3. 插件是否正常: 查看 Agent 日志中的插件状态
4. AgentCenter 是否收到数据: `docker compose logs agentcenter | grep "baseline\|8000"`

## 前端

### 无法连接 API

1. 检查 Manager 是否运行: `curl http://localhost:8080/api/v1/auth/login`
2. 检查 Nginx 代理配置: `deploy/config/nginx.conf` 中 API 反向代理
3. 检查 CORS: Manager 配置中 CORS 中间件是否正确

### 登录后立即跳回登录页

1. 检查 Token 存储: 浏览器 DevTools → Application → localStorage
2. 检查 API 响应: 确认 login 接口返回了 token
3. 检查路由守卫: 确认 token 验证逻辑正确

### 页面空白

1. 检查 Console: 浏览器 F12 → Console 查看 JavaScript 错误
2. 检查构建: `cd ui && npm run build` 是否成功
3. 检查路由: URL 是否匹配已定义的路由

## 数据库

### 查询慢

1. 检查索引: `SHOW INDEX FROM scan_results`
2. 关键索引:
   - `scan_results`: `(host_id, rule_id, checked_at DESC)`, `(host_id, checked_at DESC)`
   - `scan_tasks`: `(status, created_at)`
3. 检查数据量: `SELECT COUNT(*) FROM scan_results` — 超大表考虑清理历史数据
4. 开启慢查询日志: `SET GLOBAL slow_query_log = 'ON'`

### 表不存在

重启 AgentCenter 或 Manager 触发 Gorm AutoMigrate 自动建表。

## 日志位置

| 组件 | 位置 |
|------|------|
| AgentCenter | `docker compose logs agentcenter` |
| Manager | `docker compose logs manager` |
| Nginx | `docker compose logs ui` |
| MySQL | `docker compose logs mysql` |
| Agent | `/var/log/mxsec-agent/agent.log` |
| Baseline Plugin | `/var/log/mxsec-agent/baseline.log` 或 Agent 日志中 |

## 常见错误码

| 错误 | 原因 | 解决 |
|------|------|------|
| 401 Unauthorized | Token 过期或无效 | 重新登录 |
| 403 Forbidden | 权限不足 | 检查用户角色 |
| 404 Not Found | 资源不存在或 URL 错误 | 检查 API 路径 |
| 500 Internal Error | 服务端异常 | 查看 Manager/AgentCenter 日志 |
| 502 Bad Gateway | Nginx 无法连接后端 | 检查 Manager 是否运行 |
| gRPC UNAVAILABLE | Agent 无法连接 AgentCenter | 检查网络、证书、端口 |
