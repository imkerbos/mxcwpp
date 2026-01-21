# 操作系统兼容性说明

> 本文档说明 Matrix Cloud Security Platform 中不同 Linux 发行版之间的兼容性关系。

---

## 1. OS Family 兼容性

### 1.1 RHEL 系列兼容性

以下操作系统基于 Red Hat Enterprise Linux (RHEL)，在基线检查时互相兼容：

| OS Family | 兼容的 OS Family |
|-----------|------------------|
| `rocky` (Rocky Linux) | `rocky`, `centos`, `rhel` |
| `centos` (CentOS / CentOS Stream) | `centos`, `rocky`, `rhel` |
| `rhel` (Red Hat Enterprise Linux) | `rhel`, `rocky`, `centos` |
| `oracle` (Oracle Linux) | `oracle`, `rhel` |

**说明**：
- Rocky Linux 9 和 CentOS Stream 9 都基于 RHEL 9，可以共用相同的基线策略
- 如果策略配置了 `os_family: ["rocky"]`，CentOS 主机也能匹配该策略
- 如果策略配置了 `os_family: ["centos"]`，Rocky Linux 主机也能匹配该策略

### 1.2 Debian 系列

Debian 和 Ubuntu 系列目前不支持跨发行版兼容性，需要分别配置策略。

---

## 2. 策略匹配逻辑

### 2.1 匹配流程

当 Agent 上报主机信息时，Server 会按以下流程匹配策略：

1. **获取主机 OS Family**：Agent 从 `/etc/os-release` 读取 `ID` 字段
   - Rocky Linux 9: `os_family = "rocky"`
   - CentOS Stream 9: `os_family = "centos"`
   - RHEL 9: `os_family = "rhel"`

2. **查找兼容的 OS Family**：Server 根据兼容性映射扩展查询范围
   - 主机 OS Family = `centos`
   - 兼容列表 = `["centos", "rocky", "rhel"]`

3. **匹配策略**：查询策略的 `os_family` 字段是否包含兼容列表中的任一项
   - 策略 A: `os_family: ["rocky"]` → **匹配成功** ✅
   - 策略 B: `os_family: ["debian"]` → 匹配失败 ❌

4. **版本检查**：如果策略配置了 `os_version` 或 `os_requirements`，进一步检查版本约束

5. **运行时类型检查**：如果策略配置了 `runtime_types`，检查是否匹配（vm/docker/k8s）

### 2.2 示例

**场景 1：Rocky Linux 策略匹配 CentOS 主机**

```json
// 策略配置
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "os_family": ["rocky"],
  "os_version": ">=9"
}

// CentOS Stream 9 主机
{
  "os_family": "centos",
  "os_version": "9"
}

// 结果：匹配成功 ✅
// 原因：centos 兼容 rocky
```

**场景 2：CentOS 策略匹配 Rocky Linux 主机**

```json
// 策略配置
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "os_family": ["centos"],
  "os_version": ">=9"
}

// Rocky Linux 9 主机
{
  "os_family": "rocky",
  "os_version": "9.3"
}

// 结果：匹配成功 ✅
// 原因：rocky 兼容 centos
```

**场景 3：多 OS 策略**

```json
// 策略配置（推荐方式）
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "os_family": ["rocky", "centos", "oracle", "debian", "ubuntu"],
  "os_version": ">=7"
}

// 任何 RHEL 系列或 Debian 系列主机都能匹配
```

---

## 3. 最佳实践

### 3.1 策略配置建议

**推荐方式**：在策略中明确列出所有支持的 OS Family

```json
{
  "os_family": ["rocky", "centos", "rhel", "oracle"]
}
```

**优点**：
- 明确表达策略的适用范围
- 便于理解和维护
- 不依赖兼容性映射

**简化方式**：只配置一个 OS Family，依赖兼容性映射

```json
{
  "os_family": ["rocky"]
}
```

**优点**：
- 配置简洁
- 自动支持兼容的 OS

**缺点**：
- 不够直观
- 依赖系统的兼容性映射

### 3.2 版本约束建议

策略支持两种版本约束方式：

**方式 1：简单版本约束（向后兼容）**

使用 `os_version` 字段，适用于所有 OS Family：

```json
{
  "os_family": ["rocky", "centos", "rhel"],
  "os_version": ">=9"
}
```

**方式 2：详细版本要求（推荐）**

使用 `os_requirements` 字段，为每个 OS Family 单独配置版本范围：

```json
{
  "os_family": ["rocky", "centos"],
  "os_requirements": [
    {
      "os_family": "rocky",
      "min_version": "9.0",
      "max_version": "9.9"
    },
    {
      "os_family": "centos",
      "min_version": "9",
      "max_version": ""
    }
  ]
}
```

**版本约束规则**：
- `min_version`: 最小版本（含），留空表示不限制
- `max_version`: 最大版本（含），留空表示不限制
- 版本比较使用数字比较，例如 `9.3` > `9.2`
- 如果同时配置了 `os_requirements` 和 `os_version`，优先使用 `os_requirements`

**前端编辑**：
1. 在策略编辑界面，选择"适用OS"
2. 系统会自动为每个选中的 OS 创建版本要求条目
3. 填写最小版本和最大版本（可留空）
4. 保存后立即生效

---

## 4. 故障排查

### 4.1 主机无法匹配策略

**问题**：创建任务时提示"没有符合检查类型的主机"

**排查步骤**：

1. **检查主机 OS Family**
   ```bash
   # 在主机上执行
   cat /etc/os-release | grep "^ID="
   ```

2. **检查策略配置**
   - 登录管理界面
   - 查看策略的"适用OS"字段
   - 确认是否包含主机的 OS Family 或其兼容项

3. **检查版本约束**
   - 查看策略的"OS版本要求"
   - 确认主机版本是否满足约束

4. **检查运行时类型**
   - 确认主机的运行时类型（vm/docker/k8s）
   - 确认策略的"适用环境"是否包含该类型

### 4.2 常见问题

**Q1: 为什么 CentOS 9 主机无法匹配 Rocky Linux 策略？**

A: 请确认以下几点：
1. Server 版本是否包含 OS 兼容性支持（v1.0.0+）
2. 策略是否启用
3. 主机是否在线
4. 版本约束是否正确

**Q2: 如何让策略同时支持 Rocky Linux 和 CentOS？**

A: 有两种方式：
1. 在策略的 `os_family` 中同时添加 `rocky` 和 `centos`（推荐）
2. 只添加其中一个，系统会自动匹配兼容的 OS（需要 v1.0.0+）

**Q3: Oracle Linux 能匹配 Rocky Linux 策略吗？**

A: 不能。Oracle Linux 只兼容 RHEL，不兼容 Rocky Linux 和 CentOS。
如果需要支持 Oracle Linux，请在策略中明确添加 `oracle`。

---

## 5. 技术实现

### 5.1 兼容性映射

兼容性映射定义在 `internal/server/agentcenter/service/policy.go` 中：

```go
func getCompatibleOSFamilies(osFamily string) []string {
    compatibilityMap := map[string][]string{
        "rocky":  {"rocky", "centos", "rhel"},
        "centos": {"centos", "rocky", "rhel"},
        "rhel":   {"rhel", "rocky", "centos"},
        "oracle": {"oracle", "rhel"},
    }

    if compatible, ok := compatibilityMap[osFamily]; ok {
        return compatible
    }

    return []string{osFamily}
}
```

### 5.2 查询逻辑

策略匹配使用 MySQL 的 `JSON_CONTAINS` 函数：

```sql
SELECT * FROM policies
WHERE enabled = true
AND (
    JSON_CONTAINS(os_family, '"centos"') OR
    JSON_CONTAINS(os_family, '"rocky"') OR
    JSON_CONTAINS(os_family, '"rhel"')
);
```

---

## 6. 参考文档

- [发行版支持说明](./deployment/distribution-support.md)
- [策略配置指南](./RULE_WRITING_GUIDE.md)
- [Agent 部署指南](./deployment/agent-deployment.md)

---

**文档维护者**: Claude Code
**最后更新**: 2026-01-21
**版本**: v1.0.0
