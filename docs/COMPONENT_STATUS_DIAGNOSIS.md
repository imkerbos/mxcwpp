# 组件状态数据流诊断报告

## 问题描述
UI 组件列表页面看不到当前版本和插件/组件的状态，显示为"未安装"。

## 数据流检查结果

### ✅ 1. Agent 上报（正常）
**位置**: `internal/agent/heartbeat/heartbeat.go`

- Agent 版本：通过 `PackagedData.Version` 字段上报（来自 `m.cfg.GetVersion()`）
- 插件状态：通过心跳记录的 `plugin_stats` 字段上报（JSON 格式）
- 日志证据：
  ```
  收到插件状态 {"host_id": "...", "plugin_count": 2}
  更新插件状态成功 {"plugin_name": "baseline", "version": "1.0.2", "status": "running"}
  更新插件状态成功 {"plugin_name": "collector", "version": "1.0.2", "status": "running"}
  ```

### ✅ 2. AgentCenter 接收和入库（正常）
**位置**: `internal/server/agentcenter/transfer/service.go`

- Agent 版本：从 `data.Version` 提取，存储到 `hosts.agent_version`
- 插件状态：从 `fields["plugin_stats"]` 解析，存储到 `host_plugins` 表
- 日志证据：
  ```
  UPDATE hosts SET agent_version='1.0.3' ...
  UPDATE host_plugins SET version='1.0.2', status='running' ...
  ```

**⚠️ 发现的问题**：
- 某些情况下 `data.Version` 可能为空字符串，导致 `agent_version` 被更新为空
- **已修复**：修改了 `handleHeartbeat` 方法，只有当 `data.Version` 非空时才更新 `agent_version`

### ✅ 3. Manager API 读取（已修复）
**位置**: `internal/server/manager/api/components.go`

**修复内容**：
1. 添加了当前版本和状态统计逻辑
2. Agent：从 `hosts` 表统计已安装版本
3. Plugin：从 `host_plugins` 表统计已安装版本（排除软删除记录）
4. 添加了调试日志，便于排查问题

**查询逻辑**：
- Agent：统计所有主机的 `agent_version`，取最常见的版本
- Plugin：统计 `host_plugins` 表中对应插件名称的记录，取最常见的版本和状态

### ✅ 4. UI 显示（已修复）
**位置**: `ui/src/views/System/Components.vue`

**修复内容**：
1. 添加了"当前版本"列
2. 添加了"状态"列
3. 添加了"启动时间"和"更新时间"列
4. 添加了状态颜色和文本的辅助函数

## 可能的问题原因

### 问题 1: Agent 版本为空
**原因**：Agent 构建时没有通过 `-ldflags` 嵌入版本号，导致 `GetVersion()` 返回默认值或空值。

**检查方法**：
```bash
# 检查 Agent 二进制中的版本信息
strings /path/to/mxsec-agent | grep -i version
```

**解决方法**：
确保构建 Agent 时使用正确的 `-ldflags`：
```bash
go build -ldflags "-X main.buildVersion=1.0.3" -o mxsec-agent ./cmd/agent
```

### 问题 2: 插件未启动
**原因**：插件可能没有正确启动，导致 `GetAllPluginStats()` 返回空。

**检查方法**：
- 查看 Agent 日志，确认插件是否启动
- 检查 `plugin_configs` 表，确认插件配置是否启用

### 问题 3: 数据库查询问题
**原因**：查询条件可能不正确，或者数据被软删除。

**已修复**：添加了 `deleted_at IS NULL` 条件，排除软删除记录。

## 验证步骤

1. **检查 Agent 日志**：
   ```bash
   docker logs mxcsec-agent-test | grep -i "plugin\|version"
   ```

2. **检查 AgentCenter 日志**：
   ```bash
   docker logs mxsec-agentcenter-dev | grep -i "插件状态\|agent_version"
   ```

3. **检查 Manager API 响应**：
   ```bash
   # 需要先登录获取 token
   curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/components
   ```

4. **检查数据库数据**：
   ```sql
   -- 检查 Agent 版本
   SELECT host_id, hostname, agent_version FROM hosts WHERE agent_version IS NOT NULL AND agent_version != '';
   
   -- 检查插件版本
   SELECT host_id, name, version, status FROM host_plugins WHERE deleted_at IS NULL;
   ```

## 修复总结

1. ✅ 修复了 AgentCenter 将空版本写入数据库的问题
2. ✅ 修复了 Manager API 查询逻辑（添加软删除过滤）
3. ✅ 添加了调试日志，便于排查问题
4. ✅ 修复了前端 UI 显示（添加当前版本、状态等列）

## 下一步

1. 等待下一次心跳上报（约 30 秒）
2. 刷新组件列表页面
3. 如果仍然显示"未安装"，查看 Manager 日志中的调试信息：
   ```bash
   docker logs mxsec-manager-dev | grep -i "查询.*统计"
   ```
