# Agent 部署

Agent 是部署在每台受管主机上的轻量守护进程，负责与 Server 通信并管理插件。

## 构建

在开发机上交叉编译（输出 Linux amd64/arm64 包）:

```bash
# 构建 Agent（RPM + DEB）
make package-agent-all SERVER_HOST="agentcenter.example.com:6751" VERSION="1.0.0"

# 构建插件（二进制文件）
make package-plugins-all VERSION="1.0.0"
```

**注意**: Agent 版本号**不加** `v` 前缀（如 `1.0.0`），Server 版本号**加** `v` 前缀（如 `v1.0.0`）。

**输出**:
- Agent 包: `dist/packages/` (mxsec-agent-1.0.0.x86_64.rpm, mxsec-agent_1.0.0_amd64.deb 等)
- 插件: `dist/plugins/` (baseline, collector, fim 二进制文件)

## 安装

### RPM (CentOS/Rocky/RHEL)

```bash
sudo rpm -ivh mxsec-agent-1.0.0.x86_64.rpm
```

### DEB (Debian/Ubuntu)

```bash
sudo dpkg -i mxsec-agent_1.0.0_amd64.deb
```

安装后 Agent 自动注册为 systemd 服务并启动。

## 管理

```bash
systemctl status mxsec-agent     # 查看状态
systemctl restart mxsec-agent    # 重启
systemctl stop mxsec-agent       # 停止
journalctl -u mxsec-agent -f     # 查看日志
```

## 插件部署

插件二进制文件需上传到 Server（通过 Manager UI 或 API），Agent 启动后自动下载。

也可手动复制到 Agent 的插件目录:

```bash
# 插件目录
/var/lib/mxsec-agent/plugin/

# 复制插件
scp dist/plugins/baseline-linux-amd64 target-host:/var/lib/mxsec-agent/plugin/baseline
chmod +x /var/lib/mxsec-agent/plugin/baseline
```

## 升级

### Agent 升级

```bash
# RPM
sudo rpm -Uvh mxsec-agent-1.1.0.x86_64.rpm

# DEB
sudo dpkg -i mxsec-agent_1.1.0_amd64.deb
```

### 插件升级

替换插件目录中的二进制文件，Agent 检测到新版本后自动重载。

## 支持的操作系统

| OS | 版本 | 架构 | 包格式 |
|---|---|---|---|
| CentOS | 7, 8, 9 | amd64, arm64 | RPM |
| Rocky Linux | 8, 9 | amd64, arm64 | RPM |
| RHEL | 7, 8, 9 | amd64, arm64 | RPM |
| openEuler | 22.03 | amd64, arm64 | RPM |
| Ubuntu | 18.04, 20.04, 22.04 | amd64, arm64 | DEB |
| Debian | 10, 11, 12 | amd64, arm64 | DEB |

## 卸载

```bash
# RPM
sudo rpm -e mxsec-agent

# DEB
sudo dpkg -r mxsec-agent
```

数据目录 `/var/lib/mxsec-agent/` 和日志 `/var/log/mxsec-agent/` 不会自动删除。

## 故障排查

**Agent 无法连接 Server**:
1. 检查 Server 地址: Agent 包构建时嵌入的 SERVER_HOST 是否正确
2. 检查网络: `nc -zv agentcenter-host 6751`
3. 检查证书: Agent 首次连接会自动获取证书，确认 Server 端 deploy/certs/ 存在且正确
4. 检查防火墙: 开放 6751 端口

**Agent 日志位置**: `/var/log/mxsec-agent/agent.log`

**插件未启动**: 检查插件文件是否存在于 `/var/lib/mxsec-agent/plugin/` 且有执行权限。
