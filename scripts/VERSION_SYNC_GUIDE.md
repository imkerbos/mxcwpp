# 插件版本同步问题诊断和修复指南

## 问题概述

当用户上传新的插件版本（如 1.0.5）后，可能出现以下三个问题：

1. **组件列表问题**：配置版本显示旧版本（1.0.2），组件包显示新版本（1.0.5），状态显示"不一致"
2. **主机详情问题**：主机详情中的插件版本仍然显示旧版本（1.0.2）
3. **自动更新失败**：Agent 和插件都没有自动升级到新上传的版本（1.0.5）

## 问题根本原因

### 版本管理完整流程

1. **上传新版本到 Server**：   - 调用 `POST /api/v1/components/:id/versions` 创建版本（ReleaseVersion）
   - 调用 `POST /api/v1/components/:id/versions/:version_id/packages` 上传包文件（UploadPackage）
   - **关键点**：只有在包上传时，版本的 `is_latest` 标志为 `true`，才会触发 `syncPluginConfigForVersion` 方法更新 `plugin_configs` 表

2. **自动更新调度器**：
   - `PluginUpdateScheduler` 每 30 秒检查一次 `plugin_configs` 表的 `updated_at` 字段
   - 如果检测到更新，调用 `BroadcastPluginConfigs` 广播到所有在线 Agent

3. **Agent 端接收并更新插件**：
   - Agent 的插件管理器接收到配置更新
   - 比较版本号，如果不同则下载并更新插件

4. **Agent 上报插件版本**：
   - Agent 心跳时上报插件状态和版本
   - Server 的 `storeHostPlugins` 方法存储到 `host_plugins` 表

### 问题原因分析

问题的根本原因是 **`plugin_configs` 表没有更新到 1.0.5 版本**，可能有以下几种情况：

#### 情况 1：上传包时版本的 `is_latest` 标志不是 `true`

**代码位置**：`internal/server/manager/api/components.go:889-905`

```go
// 如果是插件，尝试同步更新插件配置
// 注意：这里需要重新查询版本以获取最新的 is_latest 状态
if component.Category == model.ComponentCategoryPlugin {
    var currentVersion model.ComponentVersion
    if err := h.db.First(&currentVersion, version.ID).Error; err == nil {
        if currentVersion.IsLatest {  // 🔴 关键：只有 is_latest=true 才会同步
            h.logger.Info("上传包后同步插件配置",
                zap.String("name", component.Name),
                zap.String("version", currentVersion.Version),
            )
            h.syncPluginConfigForVersion(&currentVersion, component.Name)
        } else {
            h.logger.Debug("版本不是最新版本，跳过同步",
                zap.String("name", component.Name),
                zap.String("version", currentVersion.Version),
            )
        }
    }
}
```

**可能的原因**：
- 创建版本时未设置 `set_latest=true`（ReleaseVersion 请求中）
- 创建版本时设置了 `set_latest=false`，打算稍后调用 `SetLatestVersion`
- 但在调用 `SetLatestVersion` 之前已经上传了包，导致包上传时 `is_latest` 仍为 `false`

#### 情况 2：上传顺序问题

**错误的操作顺序**：
1. 创建版本（`is_latest=false`）
2. 上传包（此时 `is_latest=false`，**不会触发同步**）
3. 调用 SetLatestVersion（虽然会触发同步，但此时可能包还没上传完，导致同步失败）

**正确的操作顺序**：
1. 创建版本（`set_latest=true`）
2. 上传包（此时 `is_latest=true`，**会触发同步**）

或者：
1. 创建版本（`is_latest=false`）
2. 上传所有架构的包
3. 调用 SetLatestVersion（触发同步）

#### 情况 3：SetLatestVersion 同步失败

**代码位置**：`internal/server/manager/api/components.go:568-583`

```go
// 同步更新插件配置（如果是插件）
var component model.Component
if err := h.db.First(&component, componentID).Error; err == nil {
    if component.Category == model.ComponentCategoryPlugin {
        h.logger.Info("设置最新版本后同步插件配置",
            zap.String("name", component.Name),
            zap.String("version", version.Version),
        )
        h.syncPluginConfigForVersion(&version, component.Name)
    }
} else {
    h.logger.Warn("查询组件失败，无法同步插件配置",
        zap.Uint("component_id", version.ComponentID),
        zap.Error(err),
    )
}
```

**可能的原因**：
- 调用 SetLatestVersion 时包还没上传，导致 `syncPluginConfigForVersion` 找不到包文件
- 数据库操作失败

## 诊断步骤

### 1. 运行诊断脚本

```bash
cd /path/to/mxsec-platform
./scripts/check-version-status.sh
```

该脚本会检查：
- 组件版本表（component_versions）中的最新版本
- 组件包表（component_packages）中的包文件
- 插件配置表（plugin_configs）中的版本
- 主机插件表（host_plugins）中的版本
- 三者之间的一致性

### 2. 查看输出结果

脚本会显示每个插件的版本状态：
- ✓ 绿色：版本一致
- ✗ 红色/黄色：版本不一致

### 3. 分析不一致的原因

检查以下几点：
- `component_versions` 表中版本的 `is_latest` 标志是否为 `true`
- `component_packages` 表中是否有对应版本的包文件
- `plugin_configs` 表中的版本是否与组件最新版本一致

## 修复方案

### 方案 1：运行修复脚本（推荐）

```bash
cd /path/to/mxsec-platform
./scripts/fix-version-sync.sh
```

该脚本会自动：
1. 检测版本不一致的插件
2. 从 `component_versions` 和 `component_packages` 表中读取最新版本信息
3. 更新或创建 `plugin_configs` 表中的记录
4. 验证修复结果

### 方案 2：手动通过 SQL 修复

如果修复脚本失败，可以手动执行以下 SQL：

```sql
-- 1. 查看当前状态
SELECT
    c.name AS component_name,
    cv.version AS latest_version,
    pc.version AS config_version
FROM component_versions cv
JOIN components c ON cv.component_id = c.id
LEFT JOIN plugin_configs pc ON pc.name = c.name
WHERE cv.is_latest = 1 AND c.category = 'plugin';

-- 2. 手动同步插件配置（以 baseline 为例）
UPDATE plugin_configs
SET
    version = (
        SELECT cv.version
        FROM component_versions cv
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline' AND cv.is_latest = 1
    ),
    sha256 = (
        SELECT cp.sha256
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline' AND cv.is_latest = 1 AND cp.arch = 'amd64'
        LIMIT 1
    ),
    updated_at = NOW()
WHERE name = 'baseline';

-- 3. 对 collector 插件执行同样的操作
UPDATE plugin_configs
SET
    version = (
        SELECT cv.version
        FROM component_versions cv
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector' AND cv.is_latest = 1
    ),
    sha256 = (
        SELECT cp.sha256
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector' AND cv.is_latest = 1 AND cp.arch = 'amd64'
        LIMIT 1
    ),
    updated_at = NOW()
WHERE name = 'collector';
```

### 方案 3：通过 API 重新触发同步

如果插件配置已经更新，但 Agent 仍未收到更新，可以：

#### 3.1 重启 AgentCenter 服务

```bash
# Docker 环境
docker-compose restart agentcenter

# Systemd 环境
systemctl restart mxsec-agentcenter
```

#### 3.2 调用手动触发广播 API（如果实现了）

```bash
curl -X POST http://localhost:8080/api/v1/components/broadcast-config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

## 验证修复

### 1. 检查插件配置表

```sql
SELECT name, version, sha256, updated_at
FROM plugin_configs
ORDER BY name;
```

应该看到版本已更新到 1.0.5

### 2. 查看 AgentCenter 日志

等待 30 秒（调度器检查周期），查看日志中是否有类似信息：

```
[INFO] 检测到插件配置更新，开始广播 last_check=... latest_update=...
[INFO] 广播插件配置完成 success_count=N failed_agents=[]
```

### 3. 查看 Agent 日志

Agent 应该收到配置更新并开始下载新版本：

```
[INFO] updating plugin name=baseline old_version=1.0.2 new_version=1.0.5
[INFO] downloading plugin name=baseline version=1.0.5
```

### 4. 等待 Agent 心跳上报

Agent 默认每 60 秒发送一次心跳，等待 1-2 分钟后，查看主机详情页面，插件版本应该更新为 1.0.5

### 5. 查看主机插件表

```sql
SELECT host_id, name, version, status, updated_at
FROM host_plugins
ORDER BY host_id, name;
```

应该看到版本已更新到 1.0.5

## 预防措施

### 1. 前端操作流程优化

建议前端在上传新版本时按以下顺序操作：

**方案 A（推荐）**：
1. 创建版本时就设置 `set_latest=true`
2. 上传包文件（自动触发同步）

**方案 B**：
1. 创建版本（`set_latest=false`）
2. 上传所有架构的包（amd64, arm64）
3. 确认包全部上传成功后，调用 SetLatestVersion API（触发同步）

### 2. 后端代码增强

可以考虑以下优化：

#### 优化 1：SetLatestVersion 时检查包是否存在

```go
// SetLatestVersion 中增加包检查
var packageCount int64
h.db.Model(&model.ComponentPackage{}).Where("version_id = ? AND enabled = ?", version.ID, true).Count(&packageCount)
if packageCount == 0 {
    c.JSON(http.StatusBadRequest, gin.H{
        "code":    400,
        "message": "该版本没有可用的包文件，请先上传包",
    })
    return
}
```

#### 优化 2：添加手动触发同步 API

```go
// POST /api/v1/components/:id/sync-config
func (h *ComponentsHandler) SyncPluginConfig(c *gin.Context) {
    componentID := c.Param("id")
    
    var component model.Component
    if err := h.db.First(&component, componentID).Error; err != nil {
        // ...
    }
    
    var latestVersion model.ComponentVersion
    if err := h.db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&latestVersion).Error; err != nil {
        // ...
    }
    
    h.syncPluginConfigForVersion(&latestVersion, component.Name)
    
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "message": "同步成功",
    })
}
```

#### 优化 3：在创建版本时也触发同步

如果用户勾选了 `set_latest=true`，在 ReleaseVersion API 中也触发一次同步（即使此时包还没上传，至少可以创建 plugin_configs 记录）

### 3. 监控和告警

建议添加以下监控：

1. **版本一致性监控**：定期检查 `component_versions` 和 `plugin_configs` 表的版本是否一致
2. **自动更新调度器健康检查**：监控 PluginUpdateScheduler 是否正常运行
3. **Agent 版本分布统计**：统计有多少 Agent 运行在旧版本上

## 常见问题

### Q1: 修复后 Agent 仍然没有更新到新版本？

**可能原因**：
- Agent 离线或网络不通
- Agent 下载新版本失败（检查日志）
- Agent 更新失败后回滚到旧版本

**解决方法**：
1. 检查 Agent 日志：`/var/log/mxsec/agent.log`
2. 手动重启 Agent：`systemctl restart mxsec-agent`
3. 如果下载失败，检查网络连接和下载 URL 配置

### Q2: 修复后组件列表仍显示"不一致"？

**可能原因**：
- 前端缓存未更新
- 浏览器未刷新

**解决方法**：
1. 强制刷新浏览器（Ctrl+F5）
2. 清除浏览器缓存
3. 检查后端 API 返回的数据是否正确

### Q3: 如何确认自动更新调度器是否正常工作？

**检查方法**：
1. 查看 AgentCenter 日志中是否有定期的检查记录（每 30 秒）
2. 手动更新 plugin_configs 表的 `updated_at` 字段，观察是否触发广播
3. 检查 AgentCenter 进程是否正常运行

## 总结

插件版本同步问题的核心在于理解**只有在上传包时版本的 `is_latest` 标志为 `true`，才会自动触发 `plugin_configs` 表的更新**。

修复流程：
1. 运行诊断脚本确认问题
2. 运行修复脚本或手动 SQL 更新 plugin_configs 表
3. 等待自动更新调度器广播配置（30 秒内）
4. 等待 Agent 心跳上报新版本（60 秒内）
5. 验证修复结果

如果仍有问题，请查看 AgentCenter 和 Agent 的日志获取更多诊断信息。
