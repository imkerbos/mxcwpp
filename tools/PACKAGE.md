# 基线修复工具打包说明

## 打包命令

在项目根目录执行：

```bash
cd /Users/kerbos/Workspaces/project/mxsec-platform/tools
tar czf baseline-fixer.tar.gz baseline-fixer/
```

## 部署到目标服务器

### 1. 上传工具包

```bash
scp baseline-fixer.tar.gz user@target-server:/tmp/
```

### 2. 在目标服务器上解压

```bash
ssh user@target-server
cd /tmp
tar xzf baseline-fixer.tar.gz
cd baseline-fixer
```

### 3. 安装依赖

```bash
pip3 install inquirer pandas openpyxl
```

### 4. 上传基线报告

```bash
# 从本地上传
scp baseline_report.xlsx user@target-server:/tmp/baseline-fixer/
```

### 5. 运行修复

```bash
# 脚本会自动检测操作系统（仅支持 CentOS/Rocky/RHEL）
sudo python3 baseline_fix.py -f baseline_report.xlsx
```

## 工具包内容

```
baseline-fixer/
├── baseline_fix.py          # 修复脚本（含系统检测）
├── config/                  # 13个基线配置文件（209条规则）
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
└── README.md                # 使用文档
```

## 系统限制

脚本启动时会自动检测操作系统，仅允许在以下系统运行：
- CentOS 7/8/9
- Rocky Linux 8/9
- Red Hat Enterprise Linux (RHEL) 7/8/9

如果在其他系统上运行，将显示错误并退出。
