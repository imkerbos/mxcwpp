# 基线规则编写指南

本文档详细说明如何编写矩阵云安全平台的基线检查规则。

## 目录

1. [概述](#概述)
2. [策略结构](#策略结构)
3. [规则结构](#规则结构)
4. [检查类型详解](#检查类型详解)
5. [条件逻辑](#条件逻辑)
6. [完整示例](#完整示例)
7. [最佳实践](#最佳实践)

---

## 概述

基线规则采用 **JSON 格式**，每个策略文件包含：
- 策略基本信息（ID、名称、版本、适用系统）
- 规则列表（每条规则包含检查配置和修复建议）

规则文件位置：`plugins/baseline/config/examples/`

---

## 策略结构

```json
{
  "id": "POLICY_UNIQUE_ID",
  "name": "策略显示名称",
  "version": "1.0.0",
  "description": "策略描述说明",
  "os_family": ["rocky", "centos", "ubuntu", "debian"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    // 规则列表
  ]
}
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `id` | string | 是 | 策略唯一标识，建议格式：`LINUX_类别_BASELINE` |
| `name` | string | 是 | 策略显示名称 |
| `version` | string | 是 | 版本号，格式：`主版本.次版本.修订` |
| `description` | string | 否 | 策略描述 |
| `os_family` | string[] | 是 | 适用的操作系统列表 |
| `os_version` | string | 否 | 操作系统版本要求，如 `>=7`、`8`、`>=10` |
| `enabled` | boolean | 是 | 是否启用 |
| `rules` | Rule[] | 是 | 规则列表 |

### 支持的 os_family 值

- `rocky` - Rocky Linux
- `centos` - CentOS
- `oracle` - Oracle Linux
- `almalinux` - AlmaLinux
- `debian` - Debian
- `ubuntu` - Ubuntu
- `openeuler` - openEuler
- `alibaba` - Alibaba Cloud Linux

---

## 规则结构

```json
{
  "rule_id": "LINUX_SSH_001",
  "category": "ssh",
  "title": "禁止 root 远程登录",
  "description": "sshd_config 中应设置 PermitRootLogin no，防止 root 账户被暴力破解",
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
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `rule_id` | string | 是 | 规则唯一标识，格式：`LINUX_类别_序号` |
| `category` | string | 是 | 规则分类（ssh、password、file、kernel、service 等） |
| `title` | string | 是 | 规则标题（简短描述检查内容） |
| `description` | string | 是 | 详细描述（说明为什么要检查、风险是什么） |
| `severity` | string | 是 | 严重级别：`critical`、`high`、`medium`、`low` |
| `check` | object | 是 | 检查配置 |
| `fix` | object | 是 | 修复建议 |

### 严重级别说明

| 级别 | 说明 | 示例 |
|------|------|------|
| `critical` | 严重风险，可能导致系统被完全控制 | 空 root 密码、远程执行漏洞 |
| `high` | 高风险，可能导致重要数据泄露或权限提升 | 允许 root SSH 登录、弱密码策略 |
| `medium` | 中等风险，可能导致信息泄露或拒绝服务 | 不安全的文件权限、缺少日志审计 |
| `low` | 低风险，属于安全加固建议 | 未设置登录横幅、未禁用不必要服务 |

---

## 检查类型详解

### 1. file_kv - 配置文件键值对检查

检查配置文件中的键值对设置。

```json
{
  "type": "file_kv",
  "param": ["文件路径", "键名", "期望值"]
}
```

**参数说明**：
- 参数1：配置文件路径
- 参数2：要检查的配置项名称
- 参数3：期望值（支持正则表达式）

**示例**：

```json
// 检查 SSH 禁止 root 登录
{
  "type": "file_kv",
  "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
}

// 检查密码最小长度 >= 8（使用正则）
{
  "type": "file_kv",
  "param": ["/etc/login.defs", "PASS_MIN_LEN", "^([8-9]|[1-9][0-9]+)$"]
}

// 检查密码最大有效期 <= 90 天
{
  "type": "file_kv",
  "param": ["/etc/login.defs", "PASS_MAX_DAYS", "^([1-8]?[0-9]|90)$"]
}
```

---

### 2. file_exists - 文件存在检查

检查文件或目录是否存在。

```json
{
  "type": "file_exists",
  "param": ["文件路径"]
}
```

**示例**：

```json
// 检查 SSH 配置文件存在
{
  "type": "file_exists",
  "param": ["/etc/ssh/sshd_config"]
}

// 检查审计日志目录存在
{
  "type": "file_exists",
  "param": ["/var/log/audit"]
}
```

---

### 3. file_permission - 文件权限检查

检查文件的权限模式。

```json
{
  "type": "file_permission",
  "param": ["文件路径", "期望权限"]
}
```

**参数说明**：
- 参数1：文件路径
- 参数2：期望的权限模式（八进制，如 `0644`、`0600`）

**示例**：

```json
// 检查 /etc/passwd 权限为 644
{
  "type": "file_permission",
  "param": ["/etc/passwd", "0644"]
}

// 检查 /etc/shadow 权限不超过 640
{
  "type": "file_permission",
  "param": ["/etc/shadow", "0640"]
}

// 检查 SSH 私钥权限为 600
{
  "type": "file_permission",
  "param": ["/etc/ssh/ssh_host_rsa_key", "0600"]
}
```

---

### 4. file_owner - 文件属主检查

检查文件的所有者和所属组。

```json
{
  "type": "file_owner",
  "param": ["文件路径", "期望用户", "期望用户组"]
}
```

**示例**：

```json
// 检查 /etc/passwd 属于 root:root
{
  "type": "file_owner",
  "param": ["/etc/passwd", "root", "root"]
}

// 检查 /etc/shadow 属于 root:shadow
{
  "type": "file_owner",
  "param": ["/etc/shadow", "root", "shadow"]
}
```

---

### 5. file_line_match - 文件行匹配检查

使用正则表达式匹配文件内容。

```json
{
  "type": "file_line_match",
  "param": ["文件路径", "正则表达式", "匹配模式"]
}
```

**参数说明**：
- 参数1：文件路径
- 参数2：正则表达式
- 参数3：匹配模式
  - `match` - 期望匹配到（存在则通过）
  - `not_match` - 期望不匹配（不存在则通过）

**示例**：

```json
// 检查不存在 Protocol 1 配置（不安全）
{
  "type": "file_line_match",
  "param": ["/etc/ssh/sshd_config", "^\\s*Protocol\\s+1", "not_match"]
}

// 检查存在密码复杂度配置
{
  "type": "file_line_match",
  "param": ["/etc/pam.d/system-auth", "pam_pwquality\\.so", "match"]
}

// 检查 /etc/passwd 中没有空密码字段
{
  "type": "file_line_match",
  "param": ["/etc/passwd", "^[^:]+::.*$", "not_match"]
}
```

---

### 6. sysctl - 内核参数检查

检查 sysctl 内核参数值。

```json
{
  "type": "sysctl",
  "param": ["参数名", "期望值"]
}
```

**示例**：

```json
// 检查禁用 IP 转发
{
  "type": "sysctl",
  "param": ["net.ipv4.ip_forward", "0"]
}

// 检查启用 SYN Cookie 防护
{
  "type": "sysctl",
  "param": ["net.ipv4.tcp_syncookies", "1"]
}

// 检查启用 ASLR
{
  "type": "sysctl",
  "param": ["kernel.randomize_va_space", "2"]
}

// 检查忽略 ICMP 广播请求
{
  "type": "sysctl",
  "param": ["net.ipv4.icmp_echo_ignore_broadcasts", "1"]
}
```

---

### 7. service_status - 服务状态检查

检查 systemd 服务的运行状态。

```json
{
  "type": "service_status",
  "param": ["服务名", "期望状态"]
}
```

**参数说明**：
- 参数1：服务名称（不含 .service 后缀）
- 参数2：期望状态
  - `active` - 服务正在运行
  - `inactive` - 服务未运行
  - `enabled` - 服务开机自启
  - `disabled` - 服务未设置开机自启

**示例**：

```json
// 检查防火墙服务运行中
{
  "type": "service_status",
  "param": ["firewalld", "active"]
}

// 检查 auditd 审计服务运行中
{
  "type": "service_status",
  "param": ["auditd", "active"]
}

// 检查不安全服务已禁用
{
  "type": "service_status",
  "param": ["telnet.socket", "inactive"]
}

// 检查 rsh 服务已禁用
{
  "type": "service_status",
  "param": ["rsh.socket", "disabled"]
}
```

---

### 8. command_exec - 命令执行检查

执行 Shell 命令并检查输出。

```json
{
  "type": "command_exec",
  "param": ["Shell 命令", "期望输出"]
}
```

**参数说明**：
- 参数1：要执行的 Shell 命令
- 参数2：期望的输出结果（支持正则）

**示例**：

```json
// 检查没有 UID=0 的非 root 用户
{
  "type": "command_exec",
  "param": ["awk -F: '($3 == 0 && $1 != \"root\") {print $1}' /etc/passwd | wc -l", "0"]
}

// 检查没有空密码用户
{
  "type": "command_exec",
  "param": ["awk -F: '($2 == \"\" || $2 == \"!\") {print $1}' /etc/shadow | wc -l", "0"]
}

// 检查 SELinux 状态
{
  "type": "command_exec",
  "param": ["getenforce", "Enforcing"]
}

// 检查 umask 设置
{
  "type": "command_exec",
  "param": ["grep -E '^\\s*umask\\s+0?[0-7][0-7]7' /etc/profile /etc/bashrc | wc -l", "^[1-9]"]
}
```

---

### 9. package_installed - 软件包检查

检查软件包是否安装。

```json
{
  "type": "package_installed",
  "param": ["包名", "期望状态"]
}
```

**参数说明**：
- 参数1：软件包名称
- 参数2：期望状态
  - `installed` - 已安装
  - `not_installed` - 未安装

**示例**：

```json
// 检查 aide 入侵检测工具已安装
{
  "type": "package_installed",
  "param": ["aide", "installed"]
}

// 检查 telnet 未安装
{
  "type": "package_installed",
  "param": ["telnet", "not_installed"]
}

// 检查审计工具已安装
{
  "type": "package_installed",
  "param": ["audit", "installed"]
}
```

---

## 条件逻辑

### condition 字段

`check.condition` 定义多个检查规则之间的逻辑关系：

| 值 | 说明 |
|------|------|
| `all` | 所有规则都必须通过（AND 逻辑） |
| `any` | 任意一个规则通过即可（OR 逻辑） |

### 示例

**AND 逻辑（所有条件都满足）**：

```json
{
  "check": {
    "condition": "all",
    "rules": [
      {
        "type": "file_exists",
        "param": ["/etc/ssh/sshd_config"]
      },
      {
        "type": "file_kv",
        "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
      }
    ]
  }
}
```

**OR 逻辑（满足任一条件）**：

```json
{
  "check": {
    "condition": "any",
    "rules": [
      {
        "type": "service_status",
        "param": ["firewalld", "active"]
      },
      {
        "type": "service_status",
        "param": ["iptables", "active"]
      }
    ]
  }
}
```

---

## 完整示例

### 示例 1: SSH 安全检查

```json
{
  "id": "LINUX_SSH_BASELINE",
  "name": "SSH 安全配置基线",
  "version": "1.0.0",
  "description": "SSH 服务安全配置检查",
  "os_family": ["rocky", "centos", "ubuntu"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_SSH_001",
      "category": "ssh",
      "title": "禁止 root 远程登录",
      "description": "sshd_config 中应设置 PermitRootLogin no，防止 root 账户被暴力破解",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_exists",
            "param": ["/etc/ssh/sshd_config"]
          },
          {
            "type": "file_kv",
            "param": ["/etc/ssh/sshd_config", "PermitRootLogin", "no"]
          }
        ]
      },
      "fix": {
        "suggestion": "修改 /etc/ssh/sshd_config，设置 PermitRootLogin no，然后重启 sshd 服务",
        "command": "sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && systemctl restart sshd"
      }
    },
    {
      "rule_id": "LINUX_SSH_002",
      "category": "ssh",
      "title": "SSH 端口不使用默认 22",
      "description": "建议修改 SSH 默认端口，降低被扫描和攻击的风险",
      "severity": "low",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_line_match",
            "param": ["/etc/ssh/sshd_config", "^\\s*Port\\s+22\\s*$", "not_match"]
          }
        ]
      },
      "fix": {
        "suggestion": "修改 /etc/ssh/sshd_config，将 Port 改为非标准端口（如 2222），注意同时修改防火墙规则",
        "command": "sed -i 's/^#*Port.*/Port 2222/' /etc/ssh/sshd_config && firewall-cmd --add-port=2222/tcp --permanent && firewall-cmd --reload && systemctl restart sshd"
      }
    }
  ]
}
```

### 示例 2: 密码策略检查

```json
{
  "id": "LINUX_PASSWORD_POLICY",
  "name": "密码策略基线",
  "version": "1.0.0",
  "description": "系统密码策略安全检查",
  "os_family": ["rocky", "centos", "oracle"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_PWD_001",
      "category": "password",
      "title": "密码最小长度",
      "description": "密码最小长度应不少于 8 位",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_kv",
            "param": ["/etc/login.defs", "PASS_MIN_LEN", "^([8-9]|[1-9][0-9]+)$"]
          }
        ]
      },
      "fix": {
        "suggestion": "修改 /etc/login.defs，设置 PASS_MIN_LEN 8",
        "command": "sed -i 's/^PASS_MIN_LEN.*/PASS_MIN_LEN 8/' /etc/login.defs"
      }
    },
    {
      "rule_id": "LINUX_PWD_002",
      "category": "password",
      "title": "密码复杂度要求",
      "description": "应配置 pam_pwquality 模块，要求密码包含大小写字母、数字和特殊字符",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "file_line_match",
            "param": ["/etc/security/pwquality.conf", "^\\s*minclass\\s*=\\s*[3-4]", "match"]
          }
        ]
      },
      "fix": {
        "suggestion": "编辑 /etc/security/pwquality.conf，设置 minclass = 3 或 4",
        "command": "sed -i 's/^#*\\s*minclass.*/minclass = 3/' /etc/security/pwquality.conf"
      }
    }
  ]
}
```

### 示例 3: 内核安全参数检查

```json
{
  "id": "LINUX_KERNEL_SECURITY",
  "name": "内核安全参数基线",
  "version": "1.0.0",
  "description": "Linux 内核安全参数检查",
  "os_family": ["rocky", "centos", "ubuntu", "debian"],
  "os_version": ">=7",
  "enabled": true,
  "rules": [
    {
      "rule_id": "LINUX_KERNEL_001",
      "category": "kernel",
      "title": "启用 ASLR 地址空间随机化",
      "description": "ASLR 可以防止缓冲区溢出攻击",
      "severity": "high",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "sysctl",
            "param": ["kernel.randomize_va_space", "2"]
          }
        ]
      },
      "fix": {
        "suggestion": "设置 kernel.randomize_va_space = 2",
        "command": "sysctl -w kernel.randomize_va_space=2 && echo 'kernel.randomize_va_space = 2' >> /etc/sysctl.conf"
      }
    },
    {
      "rule_id": "LINUX_KERNEL_002",
      "category": "kernel",
      "title": "禁用 IP 转发",
      "description": "非路由器设备应禁用 IP 转发功能",
      "severity": "medium",
      "check": {
        "condition": "all",
        "rules": [
          {
            "type": "sysctl",
            "param": ["net.ipv4.ip_forward", "0"]
          }
        ]
      },
      "fix": {
        "suggestion": "设置 net.ipv4.ip_forward = 0",
        "command": "sysctl -w net.ipv4.ip_forward=0 && echo 'net.ipv4.ip_forward = 0' >> /etc/sysctl.conf"
      }
    }
  ]
}
```

---

## 最佳实践

### 1. 规则 ID 命名规范

```
LINUX_<类别>_<序号>

示例：
- LINUX_SSH_001     # SSH 相关第 1 条规则
- LINUX_PWD_015     # 密码策略第 15 条规则
- LINUX_KERNEL_003  # 内核安全第 3 条规则
```

### 2. 类别（category）建议

| 类别 | 说明 |
|------|------|
| `ssh` | SSH 服务配置 |
| `password` | 密码策略 |
| `account` | 账户安全 |
| `file` | 文件权限 |
| `kernel` | 内核参数 |
| `service` | 服务状态 |
| `audit` | 审计日志 |
| `network` | 网络安全 |

### 3. 编写建议

1. **先检查文件存在**：在检查配置项之前，先用 `file_exists` 检查配置文件是否存在
2. **使用正则提高兼容性**：对于数值范围，使用正则表达式（如 `^([8-9]|[1-9][0-9]+)$`）
3. **提供修复命令**：`fix.command` 应该是可直接执行的命令
4. **详细描述风险**：`description` 应说明为什么要检查、不合规的风险是什么
5. **参考标准**：参考 CIS Benchmark、等保 2.0 等标准

### 4. 测试规则

创建规则后，可以在开发环境中测试：

```bash
# 1. 将规则文件放到 plugins/baseline/config/examples/
# 2. 重启服务，规则会自动导入数据库
# 3. 在 UI 创建检查任务，验证规则是否正常工作
```

---

## 常见问题

### Q1: 正则表达式中的特殊字符怎么处理？

JSON 中的反斜杠需要转义，所以 `\s` 需要写成 `\\s`：

```json
// 匹配 "Protocol 1"
"param": ["/etc/ssh/sshd_config", "^\\s*Protocol\\s+1", "not_match"]
```

### Q2: 如何检查配置项不存在或被注释？

使用 `file_line_match` 配合 `not_match`：

```json
{
  "type": "file_line_match",
  "param": ["/etc/ssh/sshd_config", "^\\s*PermitRootLogin\\s+yes", "not_match"]
}
```

### Q3: 如何检查数值在某个范围内？

使用正则表达式：

```json
// 检查值在 1-90 之间
"param": ["/etc/login.defs", "PASS_MAX_DAYS", "^([1-8]?[0-9]|90)$"]

// 检查值 >= 8
"param": ["/etc/login.defs", "PASS_MIN_LEN", "^([8-9]|[1-9][0-9]+)$"]
```

### Q4: 检查失败时如何调试？

1. 查看检测结果中的 `actual` 字段，显示实际检测到的值
2. 在目标主机上手动执行检查命令验证
3. 检查 Agent 日志中的 baseline 插件输出

---

## 参考资料

- [CIS Benchmarks](https://www.cisecurity.org/cis-benchmarks/)
- [等保 2.0 标准](http://www.djbh.net/)
- [STIG - Security Technical Implementation Guides](https://public.cyber.mil/stigs/)
