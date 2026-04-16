# Baseline 策略模型

本文档基于当前 `internal/server/model/policy.go`、`internal/server/model/rule.go` 和 `plugins/baseline/engine/*` 整理。

## 模型层次

```text
PolicyGroup -> Policy -> Rule -> CheckConfig / FixConfig
```

## Policy

策略集描述一组面向特定主机范围的规则，核心字段：

- `id`
- `name`
- `version`
- `description`
- `os_family`
- `os_version`
- `os_requirements`
- `runtime_types`
- `enabled`
- `group_id`

说明：

- `os_family` / `os_version` 是兼容旧模型的简化表达
- `os_requirements` 是更细粒度的目标系统约束
- `runtime_types` 用于区分 `vm`、`docker`、`k8s`

## Rule

规则是最小执行单元，核心字段：

- `rule_id`
- `policy_id`
- `category`
- `title`
- `description`
- `severity`
- `runtime_types`
- `check_config`
- `fix_config`

## CheckConfig

当前服务端模型：

```json
{
  "condition": "all",
  "rules": [
    {
      "type": "file_kv",
      "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
    }
  ]
}
```

插件执行时会转换到 `plugins/baseline/engine` 的内部模型。

## FixConfig

```json
{
  "suggestion": "修改配置并重启 sshd",
  "command": "sed -i ...",
  "restart_services": ["sshd"]
}
```

说明：

- `suggestion` 用于前端展示和人工修复参考
- `command` 非空时支持自动修复
- `restart_services` 用于批量修复后统一重启相关服务

## 生命周期

1. Manager 存储 Policy / Rule
2. 创建扫描任务后，策略下发给 Agent
3. Baseline 插件按 OS / Runtime 过滤规则
4. 执行检查器，生成 `pass/fail/error/na`
5. 结果写回 `scan_results`
6. 可修复项可进一步生成 `fix_tasks`

## 设计约束

- Policy 负责主范围过滤，Rule 可做更细粒度覆盖
- 自动修复依赖 `fix_config.command`
- 运行时类型为空时表示“不限制”

## 相关文档

- [规则编写指南](../rule-writing-guide.md)
