# 基线规则导入导出功能使用指南

## 功能概述

基线规则导入导出功能允许您通过前端界面管理基线策略和规则，无需重新编译 Agent 即可更新规则。

## 使用方法

### 1. 访问策略组管理页面

登录系统后，进入 **策略组管理** 页面，选择一个策略组进入策略列表。

### 2. 导出策略

在策略列表页面，点击 **导出** 按钮，选择导出选项：

- **导出所有策略**: 导出系统中的所有策略（包括所有策略组）
- **导出当前策略组**: 仅导出当前策略组中的策略

导出的 JSON 文件格式与 `tools/baseline-fixer/config/` 目录中的配置文件格式完全一致。

### 3. 导入策略

在策略列表页面，点击 **导入** 按钮，选择要导入的 JSON 文件。

**重要**: 导入策略时必须指定目标策略组 ID。系统会将导入的策略添加到指定的策略组中。

**导入模式**:
- 系统使用 `update` 模式导入
- 已存在的策略会被更新（保留未在文件中的规则）
- 不存在的策略会被创建

**导入结果**:
- 显示新增、更新、跳过的策略数量
- 如果有错误，会显示详细的错误信息

## 工作流程

### 场景 1: 更新基线规则

1. 编辑 `tools/baseline-fixer/config/` 目录中的 JSON 文件
2. 在前端页面点击 **导入** 按钮
3. 选择修改后的 JSON 文件
4. 确认导入
5. 规则立即生效，下次任务执行时使用新规则

### 场景 2: 同步规则到其他环境

1. 在生产环境导出策略：点击 **导出所有策略**
2. 下载 JSON 文件
3. 在测试环境导入：点击 **导入**，选择下载的文件
4. 确认导入完成

### 场景 3: 备份和恢复

1. 定期导出所有策略作为备份
2. 如需恢复，直接导入备份的 JSON 文件

## JSON 文件格式

导入/导出的 JSON 文件格式示例：

```json
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "version": "2.0.0",
  "description": "SSH 服务安全配置检查",
  "os_family": ["rocky", "centos", "ubuntu"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_SSH_001",
      "category": "ssh",
      "title": "禁止 root 远程登录",
      "description": "sshd_config 中应设置 PermitRootLogin no",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_kv",
            "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
          }
        ]
      },
      "fix": {
        "suggestion": "修改 /etc/ssh/sshd_config，设置 PermitRootLogin no",
        "command": "sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && systemctl restart sshd"
      }
    }
  ]
}
```

## 注意事项

1. **立即生效**: 导入后的规则立即生效，下次任务执行时会使用新规则
2. **无需重启**: 无需重启服务或重新编译 Agent
3. **权限要求**: 导入/导出操作需要管理员权限
4. **文件格式**: 仅支持 JSON 格式文件
5. **规则 ID**: 策略 ID 和规则 ID 必须唯一

## 常见问题

**Q: 导入后规则何时生效？**
A: 立即生效。下次创建基线检查任务时，Agent 会从服务器获取最新的规则。

**Q: 是否需要重新编译 Agent？**
A: 不需要。规则存储在数据库中，Agent 从服务器动态获取规则。

**Q: 如何回滚到旧版本规则？**
A: 导入旧版本的 JSON 文件即可。

**Q: 导入失败怎么办？**
A: 检查 JSON 格式是否正确，查看错误提示中的详细信息。

## API 端点

后端提供以下 API 端点：

- `GET /api/v1/policies/export` - 导出所有策略
- `GET /api/v1/policies/:id/export` - 导出单个策略
- `POST /api/v1/policies/import?mode=<导入模式>` - 导入策略
  - **必需参数**: `group_id` - 目标策略组 ID（通过 FormData 传递）
  - **可选参数**: `mode` - 导入模式 (skip/update/replace，默认: skip)
  - **请求体**: multipart/form-data，包含 `file` 和 `group_id` 字段

详细 API 文档请参考 [API 参考文档](../docs/API_REFERENCE.md)。
