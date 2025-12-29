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
