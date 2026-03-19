# OS 兼容性

## 支持的操作系统

| OS | 版本 | 架构 | 包格式 | 状态 |
|---|---|---|---|---|
| CentOS | 7, 8, 9 | amd64, arm64 | RPM | 已验证 |
| Rocky Linux | 8, 9 | amd64, arm64 | RPM | 已验证 |
| RHEL | 7, 8, 9 | amd64, arm64 | RPM | 已验证 |
| openEuler | 22.03 | amd64, arm64 | RPM | 已验证 |
| Ubuntu | 18.04, 20.04, 22.04 | amd64, arm64 | DEB | 已验证 |
| Debian | 10, 11, 12 | amd64, arm64 | DEB | 已验证 |

## 基线规则 OS 适配

基线策略通过 `os_family` 和 `os_version` 字段匹配目标主机:

```json
{
  "os_family": ["rocky", "centos", "rhel"],
  "os_version": ">=7"
}
```

### os_family 映射

| Agent 上报值 | 对应系统 |
|-------------|---------|
| centos | CentOS |
| rocky | Rocky Linux |
| rhel | Red Hat Enterprise Linux |
| openeuler | openEuler |
| ubuntu | Ubuntu |
| debian | Debian |

### 版本约束

| 表达式 | 含义 |
|--------|------|
| `>=7` | 版本 7 及以上 |
| `>=8,<10` | 版本 8 到 9 |
| `7` | 仅版本 7 |
| 空 | 所有版本 |

## 已知差异

### RHEL 系 (CentOS/Rocky/RHEL)

| 项目 | CentOS 7 | CentOS 8+ / Rocky 8+ |
|------|---------|----------------------|
| 包管理 | yum | dnf |
| 防火墙 | firewalld / iptables | firewalld |
| 审计 | auditd | auditd |
| SELinux 配置 | `/etc/selinux/config` | `/etc/selinux/config` |
| 密码策略 | `/etc/login.defs` | `/etc/login.defs` + `/etc/security/pwquality.conf` |

### Debian 系 (Ubuntu/Debian)

| 项目 | Ubuntu 18.04 | Ubuntu 20.04+ |
|------|------------|---------------|
| 包管理 | apt | apt |
| 防火墙 | ufw | ufw |
| SSH 配置 | `/etc/ssh/sshd_config` | `/etc/ssh/sshd_config` + `/etc/ssh/sshd_config.d/` |
| 密码策略 | `/etc/login.defs` | `/etc/login.defs` + PAM |

## 基线规则编写注意事项

1. **文件路径差异**: 部分配置文件在不同 OS 上路径不同，规则中使用 `os_family` 区分
2. **命令差异**: `yum` vs `dnf` vs `apt`，通过 `command_output` 检查器配合 OS 条件处理
3. **服务名差异**: 部分服务名在不同发行版不同，使用 `service_status` 检查器时注意
4. **默认值差异**: SSH、密码策略等默认值在不同 OS 版本间可能不同
