# 规则编写指南

## 规则结构

每条规则是一个 JSON 对象，属于某个策略（Policy）:

```json
{
  "rule_id": "LINUX_SSH_001",
  "category": "ssh",
  "title": "禁止 SSH 空密码登录",
  "description": "确保 PermitEmptyPasswords 设置为 no",
  "severity": "high",
  "check_config": {
    "conditions": "all",
    "checks": [
      {
        "check_type": "file_kv",
        "target": "/etc/ssh/sshd_config",
        "params": {
          "key": "PermitEmptyPasswords",
          "expected": "no",
          "separator": " "
        }
      }
    ]
  },
  "fix_config": {
    "suggestion": "编辑 /etc/ssh/sshd_config，设置 PermitEmptyPasswords no，然后重启 sshd"
  }
}
```

## 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| rule_id | string | 唯一标识，建议格式: `LINUX_{CATEGORY}_{序号}` |
| category | string | 分类: ssh, account, permission, sysctl, service, audit, file, network |
| title | string | 简短描述 |
| severity | string | low / medium / high / critical |
| check_config | object | 检查逻辑配置 |
| fix_config | object | 修复建议 |

## 条件组合 (conditions)

| 值 | 逻辑 | 说明 |
|---|---|---|
| `all` | AND | 所有 checks 都通过才算 pass |
| `any` | OR | 任一 check 通过即算 pass |
| `none` | NOT | 所有 checks 都不通过才算 pass |

## 检查器类型

### file_kv — 键值对检查

检查配置文件中 key=value 格式的设置。

```json
{
  "check_type": "file_kv",
  "target": "/etc/ssh/sshd_config",
  "params": {
    "key": "PermitRootLogin",
    "expected": "no",
    "separator": " ",
    "comment_prefix": "#"
  }
}
```

### file_content — 文件内容匹配

检查文件是否包含/不包含指定内容。

```json
{
  "check_type": "file_content",
  "target": "/etc/pam.d/system-auth",
  "params": {
    "pattern": "pam_pwquality.so",
    "match_type": "contains"
  }
}
```

`match_type`: `contains` | `not_contains` | `regex` | `exact`

### file_permission — 文件权限检查

```json
{
  "check_type": "file_permission",
  "target": "/etc/shadow",
  "params": {
    "max_permission": "0640"
  }
}
```

### file_owner — 文件属主检查

```json
{
  "check_type": "file_owner",
  "target": "/etc/passwd",
  "params": {
    "expected_owner": "root",
    "expected_group": "root"
  }
}
```

### file_exists — 文件存在性检查

```json
{
  "check_type": "file_exists",
  "target": "/etc/security/limits.conf",
  "params": {
    "should_exist": true
  }
}
```

### command_output — 命令输出检查

```json
{
  "check_type": "command_output",
  "target": "sysctl net.ipv4.ip_forward",
  "params": {
    "expected": "net.ipv4.ip_forward = 0",
    "match_type": "contains"
  }
}
```

### service_status — 服务状态检查

```json
{
  "check_type": "service_status",
  "target": "firewalld",
  "params": {
    "expected_status": "active",
    "expected_enabled": true
  }
}
```

### sysctl_check — 内核参数检查

```json
{
  "check_type": "sysctl_check",
  "target": "net.ipv4.conf.all.rp_filter",
  "params": {
    "expected": "1"
  }
}
```

### package_installed — 软件包检查

```json
{
  "check_type": "package_installed",
  "target": "aide",
  "params": {
    "should_installed": true
  }
}
```

## 完整示例: SSH 基线策略

```json
{
  "policy_id": "LINUX_SSH_BASELINE",
  "name": "Linux SSH 安全基线",
  "os_family": ["centos", "rocky", "rhel", "ubuntu", "debian"],
  "rules": [
    {
      "rule_id": "SSH_001",
      "category": "ssh",
      "title": "禁止空密码登录",
      "severity": "high",
      "check_config": {
        "conditions": "all",
        "checks": [
          {
            "check_type": "file_kv",
            "target": "/etc/ssh/sshd_config",
            "params": {"key": "PermitEmptyPasswords", "expected": "no", "separator": " "}
          }
        ]
      },
      "fix_config": {
        "suggestion": "编辑 /etc/ssh/sshd_config:\nPermitEmptyPasswords no\n然后: systemctl restart sshd"
      }
    },
    {
      "rule_id": "SSH_002",
      "category": "ssh",
      "title": "禁止 Root 远程登录",
      "severity": "high",
      "check_config": {
        "conditions": "all",
        "checks": [
          {
            "check_type": "file_kv",
            "target": "/etc/ssh/sshd_config",
            "params": {"key": "PermitRootLogin", "expected": "no", "separator": " "}
          }
        ]
      },
      "fix_config": {
        "suggestion": "编辑 /etc/ssh/sshd_config:\nPermitRootLogin no\n然后: systemctl restart sshd"
      }
    },
    {
      "rule_id": "SSH_003",
      "category": "ssh",
      "title": "SSH 协议版本为 2",
      "severity": "medium",
      "check_config": {
        "conditions": "all",
        "checks": [
          {
            "check_type": "file_kv",
            "target": "/etc/ssh/sshd_config",
            "params": {"key": "Protocol", "expected": "2", "separator": " "}
          }
        ]
      },
      "fix_config": {
        "suggestion": "编辑 /etc/ssh/sshd_config:\nProtocol 2\n然后: systemctl restart sshd"
      }
    },
    {
      "rule_id": "SSH_004",
      "category": "ssh",
      "title": "SSH 最大认证尝试次数",
      "severity": "medium",
      "check_config": {
        "conditions": "all",
        "checks": [
          {
            "check_type": "command_output",
            "target": "grep -i '^MaxAuthTries' /etc/ssh/sshd_config | awk '{print $2}'",
            "params": {"expected": "4", "match_type": "exact", "comparator": "<="}
          }
        ]
      },
      "fix_config": {
        "suggestion": "编辑 /etc/ssh/sshd_config:\nMaxAuthTries 4\n然后: systemctl restart sshd"
      }
    }
  ]
}
```

## 最佳实践

1. **一条规则检查一个配置项** — 避免在一条规则中检查多个不相关的配置
2. **标题清晰** — 用户能从标题理解检查内容
3. **修复建议具体** — 包含具体的配置修改命令和重启命令
4. **severity 合理分级**:
   - `critical`: 直接导致安全风险（如允许空密码）
   - `high`: 重要安全配置缺失
   - `medium`: 推荐的安全加固
   - `low`: 最佳实践建议
5. **OS 适配** — 涉及不同 OS 差异的配置，使用策略的 `os_family` / `os_version` 限定
6. **测试** — 新规则先在测试环境验证 pass/fail 两种情况
