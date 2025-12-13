# Baseline Plugin 开发计划

> 本文档记录 Baseline Plugin 的后续开发计划和任务分解。

---

## 当前状态

### 已实现功能 ✅

1. **插件框架**：
   - ✅ 插件入口（main.go）
   - ✅ 插件 SDK 集成（plugins.Client）
   - ✅ 策略加载与解析（JSON）
   - ✅ OS 匹配逻辑
   - ✅ 规则执行框架

2. **检查器实现**（8种）：
   - ✅ `file_kv`：配置文件键值对检查
   - ✅ `file_exists`：文件存在检查
   - ✅ `file_permission`：文件权限检查
   - ✅ `file_line_match`：文件行正则匹配
   - ✅ `file_owner`：文件属主检查
   - ✅ `command_exec`：命令执行检查
   - ✅ `sysctl`：内核参数检查
   - ✅ `service_status`：服务状态检查
   - ✅ `package_installed`：软件包检查

3. **示例规则**（5个策略文件）：
   - ✅ SSH 基线（3条规则）
   - ✅ 密码策略（2条规则）
   - ✅ 文件权限（3条规则）
   - ✅ sysctl 安全参数（2条规则）
   - ✅ 服务状态（2条规则）

---

## 开发目标

根据 README.md 3.2 节定义的 v1 基线检查维度，我们需要扩展以下8个维度的规则：

1. **账号与认证**（部分完成）
2. **权限与 sudo**（待实现）
3. **SSH 服务安全**（部分完成）
4. **系统服务与守护进程**（部分完成）
5. **日志与审计**（待实现）
6. **内核参数（sysctl）**（部分完成）
7. **文件与目录权限**（部分完成）
8. **时间同步**（待实现）

---

## 任务分解

### Phase 1: 扩展基线规则（优先级：P0）

#### 1.1 账号与认证规则扩展

**目标**：完善账号与认证相关的基线规则

**任务**：
- [ ] 密码复杂度策略检查（最小长度、字符类型要求）
- [ ] 账户锁定策略检查（FAILED_LOGIN_ATTEMPTS、LOCKOUT_TIME）
- [ ] 空密码账号检查（检查 /etc/shadow 中的空密码账户）
- [ ] 密码历史策略检查（PASS_MIN_DAYS、PASS_WARN_DAYS）
- [ ] 默认账户检查（禁用默认账户、检查 UID=0 的账户）

**需要的检查器**：
- 现有检查器已足够（`file_kv`、`file_line_match`、`command_exec`）

**规则文件**：
- `config/examples/account-security.json`（新增）

**预计工作量**：2-3 天

---

#### 1.2 权限与 sudo 规则扩展

**目标**：实现 sudo 配置相关的基线规则

**任务**：
- [ ] 实现 `sudoers` 检查器（解析 /etc/sudoers 和 /etc/sudoers.d/*）
- [ ] NOPASSWD 检查（禁止或白名单）
- [ ] sudoers 文件权限检查
- [ ] sudoers 语法验证
- [ ] 限制 sudo 命令范围

**需要的检查器**：
- 新增：`sudoers_checker`（解析 sudoers 配置）

**规则文件**：
- `config/examples/sudo-security.json`（新增）

**预计工作量**：3-4 天

---

#### 1.3 SSH 服务安全规则扩展

**目标**：完善 SSH 配置相关的基线规则

**任务**：
- [ ] SSH 协议版本检查（禁用 SSH v1）
- [ ] SSH 加密算法检查（禁用弱加密算法）
- [ ] SSH 密钥配置检查（HostKey、AuthorizedKeysFile）
- [ ] SSH 超时配置检查（ClientAliveInterval、ClientAliveCountMax）
- [ ] SSH 日志配置检查

**需要的检查器**：
- 现有检查器已足够（`file_kv`、`file_line_match`）

**规则文件**：
- 扩展 `config/examples/ssh-baseline.json`

**预计工作量**：2 天

---

#### 1.4 日志与审计规则扩展

**目标**：实现日志和审计相关的基线规则

**任务**：
- [ ] 实现 `log_config` 检查器（解析 rsyslog.conf、journald.conf）
- [ ] rsyslog 配置检查（日志轮转、远程日志）
- [ ] journald 配置检查（日志保留时间、存储限制）
- [ ] auditd 规则检查（auditd 服务状态、规则文件）
- [ ] 日志文件权限检查

**需要的检查器**：
- 新增：`log_config_checker`（解析日志配置文件）

**规则文件**：
- `config/examples/log-audit.json`（新增）

**预计工作量**：3-4 天

---

#### 1.5 时间同步规则扩展

**目标**：实现时间同步相关的基线规则

**任务**：
- [ ] 实现 `ntp_config` 检查器（解析 chrony.conf、ntp.conf）
- [ ] NTP/Chrony 服务状态检查
- [ ] 时间同步服务器配置检查
- [ ] 时间同步状态检查（ntpq、chronyc）

**需要的检查器**：
- 新增：`ntp_config_checker`（解析时间同步配置）

**规则文件**：
- `config/examples/time-sync.json`（新增）

**预计工作量**：2-3 天

---

#### 1.6 系统服务规则扩展

**目标**：完善系统服务相关的基线规则

**任务**：
- [ ] 不必要服务检查（telnet、ftp、rsh 等）
- [ ] 核心安全服务检查（auditd、firewalld/iptables）
- [ ] 服务启动模式检查（enabled/disabled）
- [ ] 服务依赖检查

**需要的检查器**：
- 现有检查器已足够（`service_status`、`command_exec`）

**规则文件**：
- 扩展 `config/examples/service-status.json`

**预计工作量**：2 天

---

#### 1.7 内核参数规则扩展

**目标**：完善内核参数相关的基线规则

**任务**：
- [ ] 网络安全参数（net.ipv4.ip_forward、net.ipv4.conf.all.accept_redirects）
- [ ] 内存 dump 参数（kernel.core_pattern、kernel.dmesg_restrict）
- [ ] 文件系统参数（fs.protected_hardlinks、fs.protected_symlinks）
- [ ] 进程参数（kernel.yama.ptrace_scope）

**需要的检查器**：
- 现有检查器已足够（`sysctl`）

**规则文件**：
- 扩展 `config/examples/sysctl-security.json`

**预计工作量**：2 天

---

#### 1.8 文件与目录权限规则扩展

**目标**：完善文件权限相关的基线规则

**任务**：
- [ ] 关键目录权限检查（/tmp、/var/tmp、/home）
- [ ] 日志目录权限检查（/var/log）
- [ ] 配置文件目录权限检查（/etc）
- [ ] SUID/SGID 文件检查

**需要的检查器**：
- 现有检查器已足够（`file_permission`、`file_owner`、`command_exec`）

**规则文件**：
- 扩展 `config/examples/file-permissions.json`

**预计工作量**：2 天

---

### Phase 2: 新检查器实现（优先级：P1）

#### 2.1 sudoers 检查器

**目标**：实现 sudoers 配置文件解析和检查

**实现要点**：
- 解析 `/etc/sudoers` 和 `/etc/sudoers.d/*`
- 支持 sudoers 语法（User_Alias、Host_Alias、Cmnd_Alias）
- 检查 NOPASSWD、PASSWD 配置
- 检查 sudoers 文件权限

**接口设计**：
```go
type SudoersChecker struct {
    logger *zap.Logger
}

func (c *SudoersChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error)
```

**参数格式**：
- `["/etc/sudoers", "NOPASSWD", "deny"]`：检查是否禁止 NOPASSWD
- `["/etc/sudoers.d/*", "NOPASSWD", "allow_list"]`：检查 NOPASSWD 白名单

**预计工作量**：2-3 天

---

#### 2.2 日志配置检查器

**目标**：实现日志配置文件解析和检查

**实现要点**：
- 解析 `rsyslog.conf`（支持 include、模块配置）
- 解析 `journald.conf`（systemd-journald 配置）
- 检查日志轮转配置
- 检查远程日志配置

**接口设计**：
```go
type LogConfigChecker struct {
    logger *zap.Logger
}

func (c *LogConfigChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error)
```

**参数格式**：
- `["/etc/rsyslog.conf", "MaxFileSize", "100M"]`：检查日志文件大小限制
- `["/etc/systemd/journald.conf", "SystemMaxUse", "500M"]`：检查 journald 存储限制

**预计工作量**：2-3 天

---

#### 2.3 时间同步配置检查器

**目标**：实现时间同步配置文件解析和检查

**实现要点**：
- 解析 `chrony.conf`（Chrony 配置）
- 解析 `ntp.conf`（NTP 配置）
- 检查时间服务器配置
- 检查时间同步状态（通过命令）

**接口设计**：
```go
type NTPConfigChecker struct {
    logger *zap.Logger
}

func (c *NTPConfigChecker) Check(ctx context.Context, rule *CheckRule) (*CheckResult, error)
```

**参数格式**：
- `["/etc/chrony.conf", "server", "required"]`：检查是否配置了时间服务器
- `["chronyc", "tracking", "synced"]`：检查时间同步状态

**预计工作量**：2 天

---

### Phase 3: 多 OS 适配与测试（优先级：P1）

#### 3.1 OS 适配验证

**目标**：验证所有规则在不同 OS 上的执行情况

**任务**：
- [ ] Rocky Linux 9/10 适配测试
- [ ] Oracle Linux 7/8/9 适配测试
- [ ] CentOS 7/8/9 适配测试
- [ ] Debian 10/11/12 适配测试

**测试内容**：
- 规则执行正确性
- OS 匹配逻辑正确性
- 文件路径差异处理
- 服务名称差异处理（systemd vs SysV）

**预计工作量**：3-4 天

---

#### 3.2 单元测试和集成测试

**目标**：为新规则和检查器编写测试

**任务**：
- [ ] 新检查器的单元测试
- [ ] 新规则文件的集成测试
- [ ] 端到端测试（Agent + Server + Plugin）

**预计工作量**：2-3 天

---

### Phase 4: 优化与文档（优先级：P2）

#### 4.1 代码优化

**目标**：优化现有检查器的性能和可维护性

**任务**：
- [ ] 增强错误处理和日志记录
- [ ] 性能优化（并发检查、缓存）
- [ ] 代码重构（提取公共逻辑）

**预计工作量**：2-3 天

---

#### 4.2 文档更新

**目标**：更新相关文档

**任务**：
- [ ] 更新插件开发指南（新检查器示例）
- [ ] 编写规则编写指南
- [ ] 更新检查器扩展指南
- [ ] 更新示例规则文档

**预计工作量**：1-2 天

---

## 优先级排序

### P0（必须，立即开始）
1. 账号与认证规则扩展（1.1）
2. 权限与 sudo 规则扩展（1.2）
3. SSH 服务安全规则扩展（1.3）
4. 日志与审计规则扩展（1.4）
5. 时间同步规则扩展（1.5）

### P1（重要，Phase 1 完成后开始）
1. 系统服务规则扩展（1.6）
2. 内核参数规则扩展（1.7）
3. 文件与目录权限规则扩展（1.8）
4. 新检查器实现（Phase 2）
5. 多 OS 适配与测试（Phase 3）

### P2（可选，Phase 2 完成后开始）
1. 代码优化（Phase 4.1）
2. 文档更新（Phase 4.2）

---

## 预计总工作量

- **Phase 1**：15-20 天
- **Phase 2**：6-8 天
- **Phase 3**：5-7 天
- **Phase 4**：3-5 天

**总计**：29-40 天（约 1.5-2 个月）

---

## 下一步行动

1. **立即开始**：Phase 1.1（账号与认证规则扩展）
2. **并行进行**：Phase 1.2（权限与 sudo 规则扩展）的检查器实现
3. **持续进行**：编写单元测试和集成测试

---

## 参考资源

- [Baseline 策略模型设计](../design/baseline-policy-model.md)
- [插件开发指南](./plugin-development.md)
- [Elkeid Baseline 插件参考](../../Elkeid/plugins/baseline/)
- CIS Benchmark（Linux 基线标准）
