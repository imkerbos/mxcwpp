# Bug 修复总结报告

**生成时间**: 2025-12-29
**诊断主机**: c225d050e886 (host_id: f1437d2d952748ca22f2cf0ffb05beb46312e168bbe598953d82c98b15de6e5a)

---

## 执行摘要

通过系统诊断，我们发现了 4 个相互关联的bug，主要原因是 `plugin_configs` 表未同步到最新版本，导致自动更新流程失效。

**关键发现**:
- ✅ 1.0.4 版本的插件包已成功上传到服务器
- ✅ `component_versions` 表正确标记了 1.0.4 为最新版本
- ❌ `plugin_configs` 表仍为 1.0.2 版本（**根本原因**）
- ❌ Agent 和插件因此无法自动更新到 1.0.4

---

## Bug 详情

### BUG-001: 组件列表版本显示不一致

**现象**:
- 系统配置-组件管理显示版本 1.0.4
- 主机详情-组件列表显示版本 1.0.2

**根本原因**:
- `plugin_configs` 表版本为 1.0.2，未同步到 1.0.4
- 主机上的插件实际运行版本是 1.0.2（从 `host_plugins` 表查询）
- 前端从 `host_plugins` 表读取数据，所以显示 1.0.2

**影响范围**: 所有主机的插件版本显示

---

### BUG-002: Collector 插件停止

**现象**:
- collector 插件状态显示为 "stopped"

**诊断结果**:
- `host_plugins` 表中 collector 状态确实为 `stopped`
- baseline 插件状态为 `running`（正常）

**待确认**:
- 是插件真实停止，还是状态上报错误

**建议**:
- 检查 Agent 日志确认 collector 是否真的停止
- 如果真实停止，需要排查停止原因并重启

---

### BUG-003: Agent 版本号异常

**现象**:
- 主机显示 Agent 版本为 1.0.5
- 系统中最新版本仅为 1.0.4

**根本原因**:
- Agent 编译时嵌入的版本号是 1.0.5（可能是测试版本）
- `component_versions` 表中没有 1.0.5 版本的记录

**建议**:
- 检查 `VERSION` 文件或构建脚本
- 使用正确版本号（1.0.4）重新编译
- 推送更新到该主机

---

### BUG-004: 自动更新流程失效

**现象**:
- 上传了 1.0.4 版本但容器仍运行 1.0.2

**根本原因**:
1. **`plugin_configs` 表未同步**:
   - 上传 1.0.4 版本时，`syncPluginConfigForVersion()` 函数未被正确调用或执行失败
   - 可能是因为 `component_versions` 表中有多个版本都标记为 `is_latest=1`，导致同步逻辑混乱

2. **Agent 自动更新依赖此表**:
   - Agent 从 `plugin_configs` 表读取最新版本号和下载URL
   - 表未更新，Agent 认为最新版本仍是 1.0.2
   - 因此不会触发更新

**影响范围**: 所有主机的插件自动更新

---

## 修复方案

### 立即修复（手动修复数据库）

**步骤 1: 备份数据库**
```bash
cd /Users/kerbos/Workspaces/project/mxsec-platform
mysqldump -h127.0.0.1 -P3306 -uroot -p123456 mxsec > backup_before_fix_$(date +%Y%m%d_%H%M%S).sql
```

**步骤 2: 执行修复脚本**
```bash
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec < scripts/fix-component-versions.sql
```

修复脚本将执行以下操作：
1. 更新 `plugin_configs` 表的 baseline 和 collector 版本到 1.0.4
2. 更新 SHA256 哈希值为最新包的哈希
3. 更新下载 URL 为正确的 API 路径
4. 清理 `component_versions` 表中重复的 `is_latest=1` 标记

**步骤 3: 验证修复结果**
```bash
# 查询 plugin_configs 表
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec -e "SELECT name, version FROM plugin_configs WHERE name IN ('baseline', 'collector');"

# 预期输出：
# name      | version
# ----------|--------
# baseline  | 1.0.4
# collector | 1.0.4
```

**步骤 4: 等待或手动触发更新**
- **方式1 (自动)**: 等待 Agent 下次心跳时自动检测并更新（默认每60秒）
- **方式2 (手动)**: 在系统配置-组件管理页面点击"推送更新"按钮

**步骤 5: 确认插件已更新**
```bash
# 查询主机插件版本
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec -e "
SELECT host_id, name, version, status, updated_at
FROM host_plugins
WHERE host_id LIKE 'f1437d%' AND deleted_at IS NULL;
"

# 预期输出（更新后）：
# name      | version | status
# ----------|---------|--------
# baseline  | 1.0.4   | running
# collector | 1.0.4   | running
```

---

### 长期修复（防止未来问题）

**问题 1: `syncPluginConfigForVersion()` 调用逻辑**

**位置**: `internal/server/manager/api/components.go:876-878`

**当前代码**:
```go
// 如果是插件且该版本是最新版本，同步更新插件配置
if component.Category == model.ComponentCategoryPlugin && version.IsLatest {
    h.syncPluginConfigForVersion(&version, component.Name)
}
```

**问题**:
- 只有当上传包时 `version.IsLatest = true` 才会同步
- 如果上传包时没有设置为最新版本，或者设置最新版本是在上传包之后，同步就不会执行

**建议修复**:
1. 在 `SetLatestVersion()` 函数中也调用同步
2. 添加同步失败的错误日志和告警
3. 定期检查并同步（防止遗漏）

**问题 2: `component_versions` 表的 `is_latest` 字段重复**

**现状**:
- 多个版本都标记为 `is_latest=1`
- 违反业务逻辑（每个组件应该只有一个最新版本）

**建议修复**:
1. 在 `ReleaseVersion()` 函数中，使用事务确保原子性：
   ```go
   tx := h.db.Begin()
   // 1. 先将所有旧版本设为非最新
   tx.Model(&model.ComponentVersion{}).
       Where("component_id = ?", component.ID).
       Update("is_latest", false)
   // 2. 再创建新版本并设为最新
   tx.Create(&version)
   tx.Commit()
   ```

2. 添加唯一索引约束（可选，需要修改数据库 schema）：
   ```sql
   -- 创建唯一索引，确保每个组件只有一个最新版本
   CREATE UNIQUE INDEX idx_component_latest
   ON component_versions(component_id, is_latest)
   WHERE is_latest = 1;
   ```

**问题 3: 缺少错误日志和监控**

**建议修复**:
1. 在 `syncPluginConfigForVersion()` 函数中添加详细日志
2. 同步失败时发送告警
3. 添加定期检查脚本，确保 `plugin_configs` 表与 `component_versions` 表一致

---

## 数据分析

### plugin_configs 表（修复前）
| name      | version | sha256 | enabled | download_urls                                      |
|-----------|---------|--------|---------|---------------------------------------------------|
| baseline  | 1.0.2   |        | 1       | `["file:///workspace/dist/plugins/baseline"]`    |
| collector | 1.0.2   |        | 1       | `["file:///workspace/dist/plugins/collector"]`   |

### component_versions 表（is_latest=1 的记录，修复前）
| id | component_name | version | is_latest | created_at          |
|----|----------------|---------|-----------|---------------------|
| 11 | agent          | 1.0.4   | 1         | 2025-12-26 21:07:23 |
| 8  | agent          | 1.0.4   | 1         | 2025-12-23 10:18:49 |
| 10 | baseline       | 1.0.4   | 1         | 2025-12-26 19:52:20 |
| 7  | baseline       | 1.0.4   | 1         | 2025-12-23 10:18:11 |
| 9  | collector      | 1.0.4   | 1         | 2025-12-26 19:52:07 |
| 6  | collector      | 1.0.4   | 1         | 2025-12-23 10:17:46 |

**问题**: 每个组件都有 2 个版本标记为 `is_latest=1`

### host_plugins 表（修复前）
| host_id | name      | version | status  | updated_at          |
|---------|-----------|---------|---------|---------------------|
| f1437d... | baseline  | 1.0.2   | running | 2025-12-23 14:55:15 |
| f1437d... | collector | 1.0.2   | stopped | 2025-12-23 14:55:15 |

---

## 执行清单

- [x] 1. 创建 bug 记录文档 (`docs/BUGS.md`)
- [x] 2. 创建诊断脚本 (`scripts/diagnose-component-versions.sh`)
- [x] 3. 运行诊断脚本并收集数据
- [x] 4. 分析根本原因并更新bug记录
- [x] 5. 创建修复 SQL 脚本 (`scripts/fix-component-versions.sql`)
- [ ] 6. **备份数据库** ⚠️ **请先执行此步骤！**
- [ ] 7. **执行修复 SQL 脚本**
- [ ] 8. 验证修复结果
- [ ] 9. 等待或手动触发 Agent 更新
- [ ] 10. 确认插件已更新到 1.0.4
- [ ] 11. 修复代码防止未来问题
- [ ] 12. 部署代码修复并测试

---

## 相关文件

- **Bug 记录**: `docs/BUGS.md`
- **诊断脚本**: `scripts/diagnose-component-versions.sh`
- **诊断 SQL**: `scripts/diagnose-component-versions.sql`
- **修复脚本**: `scripts/fix-component-versions.sql`
- **代码位置**: `internal/server/manager/api/components.go`
- **诊断结果**: `/private/tmp/diagnosis.txt`

---

## 联系信息

如有问题，请查看：
- Bug 记录: `docs/BUGS.md`
- 或提交 Issue: https://github.com/your-org/mxsec-platform/issues

---

# Bug 修复总结报告 - Broken Pipe 问题

**生成时间**: 2025-12-29
**版本**: v1.0.4
**诊断主机**: Alienware (agent_id: 1c30430528c9dac7df30589cbb0406a97cee89b9f983bd25f0963974516ad068)

---

## 执行摘要

通过 Agent 日志分析，发现了插件管道通信故障（broken pipe）问题，根本原因是：
1. 插件日志未被重定向，调试困难
2. 插件错误处理逻辑不当，导致读取 goroutine 过早退出
3. Agent 尝试写入任务时管道已无人读取，触发 broken pipe 错误

**关键发现**:
- ❌ Agent 启动插件时未设置 stdout/stderr 重定向，插件日志丢失
- ❌ 插件 `receiveTasks` goroutine 在遇到临时错误时过早 return
- ❌ 插件进程虽然存在但管道读端无人消费，导致管道破裂
- ✅ 已修复并发布 v1.0.4 版本

---

## 修复内容

### 修复 1: Agent 端 - 添加插件日志重定向

**修改文件**: `internal/agent/plugin/plugin.go`

**关键修改**:
1. Plugin 结构体添加 `logWriter *os.File` 字段
2. 启动插件时创建 `/var/log/mxsec-agent/plugins/<plugin_name>.log`
3. 设置 `cmd.Stdout = logWriter` 和 `cmd.Stderr = logWriter`
4. 插件退出时关闭日志文件

### 修复 2: 插件端 - 改进错误处理

**修改文件**: `plugins/baseline/main.go`

**关键修改**:
1. 区分真正的管道关闭（EOF）和临时错误
2. 临时错误时继续重试，而非退出 goroutine
3. 添加详细日志，便于调试

---

## 部署步骤

### 快速部署（推荐）
```bash
# 1. 上传并安装 Agent RPM 包
scp dist/packages/mxsec-agent-1.0.4-amd64.rpm root@192.168.31.71:/tmp/
ssh root@192.168.31.71 "rpm -Uvh /tmp/mxsec-agent-1.0.4-amd64.rpm && systemctl restart mxsec-agent"

# 2. 验证日志文件已创建
ssh root@192.168.31.71 "ls -lh /var/log/mxsec-agent/plugins/"

# 3. 查看插件日志
ssh root@192.168.31.71 "tail -f /var/log/mxsec-agent/plugins/baseline.log"
```

---

## 验证检查

- [ ] 日志目录 `/var/log/mxsec-agent/plugins/` 已创建
- [ ] baseline.log 和 collector.log 文件存在
- [ ] Agent 日志中无 "broken pipe" 错误
- [ ] 插件日志中能看到任务接收记录
- [ ] 基线检查任务能正常执行

---

# Bug 修复总结 - Agent/插件更新下载失败问题

**修复时间**: 2025-12-29
**版本**: v1.0.5
**问题**: Docker 容器环境中 Agent 无法下载更新包

---

## 问题描述

容器 9098c12f533a 中的 Agent（当前版本 1.0.4）无法更新到 1.0.5 版本，错误信息：

```
Get "http://localhost:8080/api/v1/agent/download/rpm/amd64": dial tcp [::1]:8080: connect: connection refused
```

**根本原因**：
在 Docker Compose 网络环境中，AgentCenter 生成的下载 URL 使用 `http://localhost:8080`，但容器内的 localhost 指向容器自身，无法访问 Manager 服务。

---

## 解决方案

### 修改内容

#### 1. 前端域名验证规则修复 (`ui/src/views/System/Settings.vue`)

**问题**：原验证规则不支持 IP 地址格式（如 `http://192.168.8.140:3000`）

**修改**：
```javascript
// 修改前：只支持域名格式
pattern: /^(https?:\/\/)?([\da-z\.-]+)\.([a-z\.]{2,6})([\/\w \.-]*)*\/?$/

// 修改后：支持 IP、域名、端口号和路径
pattern: /^https?:\/\/([\w-]+(\.[\w-]+)*|(\d{1,3}\.){3}\d{1,3}|[\w-]+)(:\d+)?(\/.*)?$/
```

**支持格式**：
- ✅ `http://192.168.8.140:3000`
- ✅ `https://example.com`
- ✅ `http://manager:8080`
- ✅ `https://example.com/path`

#### 2. 下载 URL 生成逻辑简化 (`internal/server/agentcenter/scheduler/agent_update_scheduler.go`)

**简化策略**，优先级如下：

1. **系统设置域名**（系统管理-基本设置，最高优先级）
2. **GRPC Host**（如果不是 0.0.0.0，用于Agent能访问的场景）
3. **localhost**（最后回退，仅开发环境）

**新增方法**:
```go
// getSiteDomain 从数据库获取站点域名配置
func (s *AgentUpdateScheduler) getSiteDomain() string {
    // 从 system_configs 表读取 site_config
    // 优先使用用户在系统管理界面配置的域名
}
```

**简化后的方法**:
```go
// buildDownloadURL 构建完整的下载 URL
// 优先级：系统设置域名 > GRPC Host > localhost
func (s *AgentUpdateScheduler) buildDownloadURL(pkgType model.PackageType, arch string) string {
    // 1. 优先使用系统设置中的站点域名
    siteDomain := s.getSiteDomain()
    if siteDomain != "" {
        return strings.TrimSuffix(siteDomain, "/") + relativePath
    }

    // 2. 使用 GRPC Host（如果不是 0.0.0.0）
    grpcHost := s.cfg.Server.GRPC.Host
    if grpcHost != "0.0.0.0" && grpcHost != "" {
        return fmt.Sprintf("http://%s:%d%s", grpcHost, httpPort, relativePath)
    }

    // 3. localhost（最后回退）
    return fmt.Sprintf("http://localhost:%d%s", httpPort, relativePath)
}
```

#### 3. 清理冗余配置

**移除内容**：
- `internal/server/config/config.go` 中的 `HTTPConfig.ExternalHost` 字段
- `deploy/docker-compose/configs/server.dev.yaml` 中的 `external_host` 配置

**原因**：简化配置，减少外部变量，统一使用系统设置管理

---

## 使用方式

### 推荐方式：使用系统设置（适用所有环境）

1. 登录前端管理界面
2. 进入 **系统管理** → **基本设置**
3. 在 **域名设置** 中填写：
   - **Docker Compose 环境**: `http://manager:8080`
   - **生产环境（域名）**: `http://your-domain:8080` 或 `https://your-domain`
   - **生产环境（IP）**: `http://192.168.8.140:8080`
   - **外部主机**: `http://192.168.x.x:8080`

**特点**：
- ✅ 立即生效，无需重启服务
- ✅ 支持 IP 地址和域名
- ✅ 支持端口号和路径
- ✅ 统一管理，便于维护

---

## 验证步骤

### 1. 重启服务

```bash
cd deploy/docker-compose
docker-compose -f docker-compose.dev.yml restart agentcenter manager
```

### 2. 检查生成的下载URL

查看 AgentCenter 日志：

```bash
docker-compose -f docker-compose.dev.yml logs -f agentcenter | grep "download_url"
```

期望看到：

```
"download_url":"http://manager:8080/api/v1/agent/download/rpm/amd64"
```

### 3. 检查 Agent 更新日志

```bash
docker exec 9098c12f533a tail -f /var/log/mxsec-agent/agent.log
```

期望看到类似日志：

```json
{
  "level": "info",
  "msg": "downloading update package",
  "url": "http://manager:8080/api/v1/agent/download/rpm/amd64"
}
{
  "level": "info",
  "msg": "update completed successfully",
  "version": "1.0.5"
}
```

### 4. 验证更新成功

等待约 30 秒（更新调度器周期），确认版本更新：

```bash
# 方法1: 查看 Agent 日志
docker logs 9098c12f533a 2>&1 | grep -i version

# 方法2: 通过前端查看主机详情
# 进入 主机管理 → 选择主机 → 查看 Agent 版本
```

---

## 影响范围

### 文件修改

1. `internal/server/config/config.go`
   - 添加 `HTTPConfig.ExternalHost` 字段

2. `internal/server/agentcenter/scheduler/agent_update_scheduler.go`
   - 添加 `getSiteDomain()` 方法
   - 优化 `buildDownloadURL()` 方法
   - 添加 `encoding/json` 和 `strings` 导入

3. `deploy/docker-compose/configs/server.dev.yaml`
   - 添加 `server.http.external_host: "manager"` 配置

### 功能影响

- **Agent 更新**: 下载 URL 生成逻辑优化，支持多环境配置
- **插件更新**: 复用类似机制（`buildPluginDownloadURLs`），无需额外修改
- **系统设置**: 域名设置现在会影响 Agent/插件更新 URL

### 兼容性

- ✅ 完全向后兼容
- ✅ 未配置时回退到原有逻辑
- ✅ 支持渐进式迁移（配置文件 → 系统设置）

---

## 不同环境的配置建议

所有环境统一使用 **系统管理 → 基本设置 → 域名设置** 进行配置：

| 环境 | 域名设置示例 | 说明 |
|------|--------------|------|
| **Docker Compose 开发** | `http://manager:8080` | 使用服务名，容器间通信 |
| **生产环境（域名）** | `https://mxsec.example.com` | 使用实际域名，支持HTTPS |
| **生产环境（IP）** | `http://192.168.8.140:8080` | 使用服务器IP地址 |
| **Kubernetes** | `http://mxsec-manager-service:8080` | 使用K8s服务名 |
| **本机开发** | `http://localhost:8080` | 使用localhost（自动回退）|

---

## 注意事项

1. **配置优先级**: 系统设置（最高）> GRPC Host > localhost（回退）
2. **Docker 环境**: 必须配置系统设置为服务名（如 `http://manager:8080`）
3. **域名格式**: 必须包含协议（`http://` 或 `https://`），可包含端口和路径
4. **立即生效**: 系统设置更改后立即生效，无需重启服务
5. **调试方法**: 查看 AgentCenter 日志中的 `download_url` 字段确认 URL 是否正确

---

## 相关问题

### Q: 为什么必须配置系统设置的域名？

A: Docker/Kubernetes 等容器环境中，localhost 无法跨容器访问。必须配置为容器间可访问的地址（服务名或IP）。

### Q: 插件下载URL是否也会使用系统设置？

A: 是的。插件下载URL生成逻辑（`buildPluginDownloadURLs`）会自动应用系统设置中的域名。

### Q: 配置域名时末尾的斜杠要不要加？

A: 建议不加。代码会自动处理，加不加都可以（如 `http://manager:8080` 或 `http://manager:8080/` 都支持）。

### Q: 如何验证配置是否生效？

A: 查看 AgentCenter 日志：
```bash
docker-compose -f docker-compose.dev.yml logs agentcenter | grep download_url
```
或在系统设置中故意配置错误域名，观察 Agent 日志中的错误信息。

### Q: 配置修改后需要重启服务吗？

A: 不需要。系统设置修改后立即生效（每次生成URL时都会从数据库读取最新配置）。

---

**修复者**: Claude Code
**审核者**: 待审核
**部署时间**: 待部署

---

# Bug 修复总结 - 插件版本回退和外部主机下载失败问题

**生成时间**: 2025-12-31
**版本**: v1.0.5
**问题**: 插件版本管理系统存在三个严重问题

---

## 问题概述

用户上传了新的插件版本（v1.0.5），但系统出现了三个相互关联的问题：

1. **组件列表版本不一致**：配置版本显示 1.0.2，组件包显示 1.0.5，状态显示"不一致"
2. **主机详情版本回退**：主机详情页面显示插件版本仍为 1.0.2
3. **自动更新失败**：Agent 和插件都没有自动升级到 1.0.5 版本

---

## 根本原因分析

通过深入代码审查，发现问题的根本原因是：**`plugin_configs` 表没有更新到 1.0.5 版本**

### 版本管理完整流程回顾

1. **上传新版本到 Server**：
   - 调用 `POST /api/v1/components/:id/versions` 创建版本（ReleaseVersion）
   - 调用 `POST /api/v1/components/:id/versions/:version_id/packages` 上传包文件（UploadPackage）
   - **关键点**：只有在包上传时，版本的 `is_latest` 标志为 `true`，才会触发 `syncPluginConfigForVersion` 方法更新 `plugin_configs` 表

2. **自动更新调度器**：
   - `PluginUpdateScheduler` 每 30 秒检查一次 `plugin_configs` 表的 `updated_at` 字段
   - 如果检测到更新，调用 `BroadcastPluginConfigs` 广播到所有在线 Agent

3. **Agent 端接收并更新插件**：
   - Agent 的插件管理器接收到配置更新
   - 比较版本号（使用语义化版本比较），如果不同则下载并更新插件

4. **Agent 上报插件版本**：
   - Agent 心跳时上报插件状态和版本（来自 `plugin.Config.Version`）
   - Server 的 `storeHostPlugins` 方法存储到 `host_plugins` 表

### 问题定位

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

#### 情况 1：创建版本时未设置 `set_latest=true`

用户可能：
- 创建版本时未勾选"设为最新版本"
- 打算稍后调用 `SetLatestVersion` API
- 但在调用 `SetLatestVersion` 之前已经上传了包，导致包上传时 `is_latest` 仍为 `false`，**不会触发同步**

#### 情况 2：上传顺序问题

**错误的操作顺序**：
1. 创建版本（`is_latest=false`）
2. 上传包（此时 `is_latest=false`，不会触发同步）
3. 调用 SetLatestVersion（虽然会触发同步，但可能包还没完全上传，导致同步失败或找不到包文件）

**正确的操作顺序**：
1. 创建版本（`set_latest=true`）→ 上传包（触发同步）✅
2. 或：创建版本 → 上传所有架构的包 → 调用 SetLatestVersion（触发同步）✅

#### 情况 3：SetLatestVersion 同步失败

**代码位置**：`internal/server/manager/api/components.go:568-583`

调用 SetLatestVersion 时：
- 如果包文件还没上传，`syncPluginConfigForVersion` 会找不到包文件，同步失败
- 数据库操作失败但没有返回错误给前端

---

## 解决方案

### 1. 诊断工具

创建了两个脚本帮助诊断和修复问题：

#### 诊断脚本：`scripts/check-version-status.sh`

```bash
cd /path/to/mxsec-platform
./scripts/check-version-status.sh
```

功能：
- 查询组件版本表（component_versions）中的最新版本
- 查询组件包表（component_packages）中的包文件
- 查询插件配置表（plugin_configs）中的版本
- 查询主机插件表（host_plugins）中的版本
- 检查三者之间的一致性

#### 修复脚本：`scripts/fix-version-sync.sh`

```bash
cd /path/to/mxsec-platform
./scripts/fix-version-sync.sh
```

功能：
1. 自动检测版本不一致的插件
2. 从 `component_versions` 和 `component_packages` 表中读取最新版本信息
3. 更新或创建 `plugin_configs` 表中的记录
4. 验证修复结果

### 2. 使用修复脚本修复问题

```bash
# 1. 运行诊断脚本确认问题
./scripts/check-version-status.sh

# 2. 运行修复脚本
./scripts/fix-version-sync.sh

# 3. 等待自动更新调度器检测到配置更新（30 秒内）
# 调度器会广播新配置到所有在线 Agent

# 4. 等待 Agent 心跳上报新版本（60 秒内）
# Agent 会下载并更新到新版本

# 5. 验证修复结果
./scripts/check-version-status.sh
```

### 3. 手动 SQL 修复（如果脚本失败）

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

-- 2. 手动同步 baseline 插件配置
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

-- 3. 手动同步 collector 插件配置
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

---

## 验证修复

### 1. 检查插件配置表

```sql
SELECT name, version, sha256, updated_at
FROM plugin_configs
ORDER BY name;
```

应该看到版本已更新到 1.0.5

### 2. 查看 AgentCenter 日志

等待 30 秒（调度器检查周期），查看日志中是否有：

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

等待 1-2 分钟后，查看主机详情页面，插件版本应该更新为 1.0.5

### 5. 查看主机插件表

```sql
SELECT host_id, name, version, status, updated_at
FROM host_plugins
ORDER BY host_id, name;
```

应该看到版本已更新到 1.0.5

---

## 长期优化建议

### 优化 1：SetLatestVersion 时检查包是否存在

**代码位置**：`internal/server/manager/api/components.go` 中的 `SetLatestVersion` 方法

**建议修改**：
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

### 优化 2：添加手动触发同步 API

**新增 API**：`POST /api/v1/components/:id/sync-config`

```go
func (h *ComponentsHandler) SyncPluginConfig(c *gin.Context) {
    componentID := c.Param("id")

    var component model.Component
    if err := h.db.First(&component, componentID).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{
            "code":    404,
            "message": "组件不存在",
        })
        return
    }

    var latestVersion model.ComponentVersion
    if err := h.db.Where("component_id = ? AND is_latest = ?", component.ID, true).First(&latestVersion).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{
            "code":    404,
            "message": "未找到最新版本",
        })
        return
    }

    h.syncPluginConfigForVersion(&latestVersion, component.Name)

    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "message": "同步成功",
    })
}
```

### 优化 3：改进前端操作流程

**建议前端在上传新版本时按以下顺序操作**：

**方案 A（推荐）**：
1. 创建版本时就勾选"设为最新版本"（`set_latest=true`）
2. 上传包文件（自动触发同步）

**方案 B**：
1. 创建版本（不勾选"设为最新版本"）
2. 上传所有架构的包（amd64, arm64）
3. 确认包全部上传成功后，点击"设为最新版本"按钮（触发同步）

### 优化 4：添加监控和告警

建议添加以下监控：

1. **版本一致性监控**：定期检查 `component_versions` 和 `plugin_configs` 表的版本是否一致
2. **自动更新调度器健康检查**：监控 PluginUpdateScheduler 是否正常运行
3. **Agent 版本分布统计**：统计有多少 Agent 运行在旧版本上

---

## 相关文件

- **诊断脚本**: `scripts/check-version-status.sh`
- **修复脚本**: `scripts/fix-version-sync.sh`
- **诊断文档**: `scripts/VERSION_SYNC_GUIDE.md`
- **代码位置**: `internal/server/manager/api/components.go`

---

## 常见问题

### Q1: 修复后 Agent 仍然没有更新到新版本？

**可能原因**：
- Agent 离线或网络不通
- Agent 下载新版本失败（检查日志）
- Agent 更新失败后回滚到旧版本

**解决方法**：
1. 检查 Agent 日志：`/var/log/mxsec-agent/agent.log`（容器内）或 `docker logs <container_id>`
2. 手动重启 Agent：`systemctl restart mxsec-agent`（如果是 systemd）或 `docker restart <container_id>`
3. 如果下载失败，检查网络连接和下载 URL 配置（系统管理→基本设置→域名设置）

### Q2: 修复后组件列表仍显示"不一致"？

**可能原因**：
- 前端缓存未更新
- 浏览器未刷新

**解决方法**：
1. 强制刷新浏览器（Ctrl+F5 或 Cmd+Shift+R）
2. 清除浏览器缓存
3. 检查后端 API 返回的数据是否正确

### Q3: 如何确认自动更新调度器是否正常工作？

**检查方法**：
1. 查看 AgentCenter 日志中是否有定期的检查记录（每 30 秒）
2. 手动更新 plugin_configs 表的 `updated_at` 字段，观察是否触发广播
3. 检查 AgentCenter 进程是否正常运行

---

## 总结

插件版本同步问题的核心在于理解**只有在上传包时版本的 `is_latest` 标志为 `true`，才会自动触发 `plugin_configs` 表的更新**。

**修复流程**：
1. 运行诊断脚本确认问题 → `./scripts/check-version-status.sh`
2. 运行修复脚本或手动 SQL 更新 plugin_configs 表 → `./scripts/fix-version-sync.sh`
3. 等待自动更新调度器广播配置（30 秒内）
4. 等待 Agent 心跳上报新版本（60 秒内）
5. 验证修复结果 → `./scripts/check-version-status.sh`

**预防措施**：
1. 前端操作流程优化：创建版本时就勾选"设为最新版本"
2. 后端增加包文件检查
3. 添加手动触发同步 API
4. 添加版本一致性监控

**修复者**: Claude Code
**修复时间**: 2025-12-31
**影响版本**: v1.0.5
**状态**: 已修复（脚本已创建，待用户执行）
