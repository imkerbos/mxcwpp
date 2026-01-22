# 基线修复工具

独立的基线检查修复工具，包含所有配置文件，可直接部署到目标服务器使用。

## 系统要求

**支持的操作系统**：
- CentOS 7/8/9
- Rocky Linux 8/9
- Red Hat Enterprise Linux (RHEL) 7/8/9

**注意**：脚本会在启动时自动检测操作系统，如果不是上述系统将拒绝运行。

**其他要求**：
- Python 3.6+
- root 权限（执行修复命令需要）

## 目录结构

```
baseline-fixer/
├── baseline_fix.py          # 修复脚本
├── config/                  # 基线配置文件（13个JSON文件）
│   ├── account-security.json
│   ├── audit-logging.json
│   ├── cron-security.json
│   ├── file-integrity.json
│   ├── file-permissions.json
│   ├── login-banner.json
│   ├── mac-security.json
│   ├── network-protocols.json
│   ├── password-policy.json
│   ├── secure-boot.json
│   ├── service-status.json
│   ├── ssh-baseline.json
│   └── sysctl-security.json
└── README.md                # 本文档
```

## 快速开始

### 1. 部署到目标服务器

```bash
# 打包工具目录
tar czf baseline-fixer.tar.gz baseline-fixer/

# 上传到目标服务器
scp baseline-fixer.tar.gz user@target-server:/tmp/

# 在目标服务器上解压
ssh user@target-server
cd /tmp
tar xzf baseline-fixer.tar.gz
cd baseline-fixer
```

### 2. 安装依赖

```bash
pip3 install inquirer pandas openpyxl
```

### 3. 上传基线报告

将基线检查报告（Excel 文件）上传到 `baseline-fixer` 目录：

```bash
# 从本地上传报告
scp baseline_report.xlsx user@target-server:/tmp/baseline-fixer/
```

### 4. 运行修复

```bash
# 脚本会自动检测操作系统
# 如果不是 CentOS/Rocky/RHEL，将拒绝运行

# 修复 HIGH 和 CRITICAL 等级（默认）
sudo python3 baseline_fix.py -f baseline_report.xlsx

# 包含 MEDIUM 等级
sudo python3 baseline_fix.py -f baseline_report.xlsx -s HIGH CRITICAL MEDIUM

# 仅修复 CRITICAL
sudo python3 baseline_fix.py -f baseline_report.xlsx -s CRITICAL
```

**运行示例**：
```
✓ 检测到操作系统: ROCKY
✓ 已加载 209 条基线修复规则
✓ 成功加载报告: baseline_report.xlsx
...
```

## 使用流程

### 交互式选择

运行脚本后，会显示所有符合条件的检查项：

```
✓ 已加载 209 条基线修复规则
✓ 成功加载报告: baseline_report.xlsx

筛选风险等级: HIGH, CRITICAL
找到 45 个检查项
其中 38 个有自动修复方案

? 选择要修复的项目 (空格选择，a全选，回车确认)
 ❯ ○ [HIGH] LINUX_SSH_001 - 禁止 root 远程登录
   ○ [HIGH] LINUX_SSH_002 - 禁止空密码登录
   ○ [CRITICAL] LINUX_SSH_017 - SSH 使用强加密算法
   ...
```

**操作说明**：
- `↑↓` 键：移动光标
- `空格` 键：选择/取消选择
- `a` 键：全选
- `回车` 键：确认并开始修复

### 执行修复

选择完成后，脚本会依次执行修复：

```
开始修复 10 个项目...

[1/10] LINUX_SSH_001 - 禁止 root 远程登录
  执行: sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && systemctl restart sshd
  ✓ 修复成功

[2/10] LINUX_SSH_002 - 禁止空密码登录
  执行: sed -i 's/^#*PermitEmptyPasswords.*/PermitEmptyPasswords no/' /etc/ssh/sshd_config && systemctl restart sshd
  ✓ 修复成功

...

修复完成: 10/10 成功
```

## 支持的基线类别

工具包含 **13 个基线类别**，共 **209 条修复规则**：

| 类别 | 规则数 | 说明 |
|------|--------|------|
| 账户安全 | 21 | root账户、UID/GID、密码策略等 |
| SSH 安全 | 24 | SSH配置、加密算法、认证方式等 |
| 密码策略 | 15 | 密码长度、复杂度、过期时间等 |
| 文件权限 | 20 | 关键文件权限和所有权 |
| 服务状态 | 25 | 必要服务启用、危险服务禁用 |
| 内核参数 | 30 | sysctl 安全参数配置 |
| 网络协议 | 7 | 禁用不安全的网络协议 |
| 审计日志 | 27 | auditd 配置和日志审计规则 |
| Cron 安全 | 10 | 定时任务权限和访问控制 |
| 文件完整性 | 4 | AIDE 文件完整性检查 |
| MAC 安全 | 7 | SELinux/AppArmor 配置 |
| 登录横幅 | 9 | 登录提示信息配置 |
| 安全启动 | 7 | GRUB 密码、启动参数等 |

## 注意事项

### ⚠️ 重要提醒

1. **需要 root 权限**：所有修复命令都需要 root 权限执行
2. **先在 UAT 测试**：务必先在 UAT 环境测试，确认无影响后再上 PROD
3. **备份配置文件**：修复前务必备份关键配置
4. **分批修复**：建议按风险等级分批修复，不要一次性全部修复
5. **验证服务**：每次修复后验证关键服务是否正常

### 修复前备份

```bash
# 创建备份目录
sudo mkdir -p /root/backup_$(date +%Y%m%d)

# 备份 SSH 配置
sudo cp /etc/ssh/sshd_config /root/backup_$(date +%Y%m%d)/

# 备份 PAM 配置
sudo tar czf /root/backup_$(date +%Y%m%d)/pam.tar.gz /etc/pam.d/

# 备份 sysctl 配置
sudo cp /etc/sysctl.conf /root/backup_$(date +%Y%m%d)/

# 备份 audit 配置
sudo cp /etc/audit/auditd.conf /root/backup_$(date +%Y%m%d)/ 2>/dev/null || true
```

### 回滚方法

如果修复后出现问题：

```bash
# 恢复 SSH 配置
sudo cp /root/backup_YYYYMMDD/sshd_config /etc/ssh/sshd_config
sudo systemctl restart sshd

# 恢复 sysctl 配置
sudo cp /root/backup_YYYYMMDD/sysctl.conf /etc/sysctl.conf
sudo sysctl -p
```

## 推荐修复流程

### UAT 环境

```bash
# 1. 备份配置
sudo mkdir -p /root/backup_$(date +%Y%m%d)
sudo tar czf /root/backup_$(date +%Y%m%d)/config_backup.tar.gz \
    /etc/ssh /etc/pam.d /etc/sysctl.conf /etc/audit 2>/dev/null

# 2. 先修复 CRITICAL
sudo python3 baseline_fix.py -f uat_report.xlsx -s CRITICAL

# 3. 验证服务
sudo systemctl status sshd
sudo systemctl status firewalld
curl http://localhost:8080/health

# 4. 如果正常，继续修复 HIGH
sudo python3 baseline_fix.py -f uat_report.xlsx -s HIGH

# 5. 再次验证
# ...

# 6. 如果正常，修复 MEDIUM
sudo python3 baseline_fix.py -f uat_report.xlsx -s MEDIUM
```

### PROD 环境

确认 UAT 环境无问题后，在 PROD 环境重复上述步骤。

## Excel 报告格式

脚本会自动识别以下列名（不区分大小写）：

- **规则 ID**：`rule_id`、`规则id`、`规则编号`
- **检查项名称**：`检查项`、`名称`、`name`、`标题`、`title`
- **风险等级**：`等级`、`级别`、`severity`、`风险`

## 常见问题

### Q: 提示"未找到修复命令"？

A: 可能原因：
- Excel 报告中的规则 ID 与配置文件不匹配
- 该检查项没有自动修复命令（需要手动修复）

### Q: 修复失败怎么办？

A: 查看错误信息：
- 权限不足：使用 `sudo` 运行
- 服务不存在：检查系统是否安装对应服务
- 配置文件不存在：检查文件路径是否正确

### Q: 如何只查看修复命令而不执行？

A: 可以在选择界面不选择任何项目，或者查看 `config/` 目录下的 JSON 文件，每个规则的 `fix.command` 字段包含修复命令。

## 技术支持

如有问题，请联系安全团队。
