# Agent 连接问题排查指南

## 问题症状

Rocky 虚拟机上的 Agent 日志显示：
```
{"level":"error","ts":"...","msg":"failed to get connection","error":"connection timeout after 30s: context deadline exceeded"}
```

## 快速诊断

### 步骤1：在 Rocky 虚拟机上运行诊断脚本

```bash
# 复制诊断脚本到虚拟机
curl -o diagnose.sh http://192.168.8.140:8080/path/to/diagnose-agent-connection.sh

# 或手动创建脚本（见下方）
chmod +x diagnose.sh

# 运行诊断
./diagnose.sh 192.168.8.140 6751
```

### 步骤2：测试基本连通性

```bash
# 1. 测试 ICMP（可能被防火墙阻止，失败不一定有问题）
ping -c 3 192.168.8.140

# 2. 测试 TCP 端口（最重要的测试）
telnet 192.168.8.140 6751
# 或
nc -zv 192.168.8.140 6751

# 3. 检查路由
ip route get 192.168.8.140
```

## 常见原因与解决方案

### 原因1：VMware 网络配置问题（最可能）

**问题**：虚拟机使用 NAT 模式，无法直接访问宿主机的 Docker 容器

**解决方案A：切换到桥接模式（推荐）**
1. 关闭虚拟机
2. VMware 设置 → 网络适配器 → 选择"桥接模式"
3. 启动虚拟机
4. 检查虚拟机 IP 是否与宿主机在同一网段
   ```bash
   ip addr show
   # 应该显示类似 192.168.8.x 的地址
   ```

**解决方案B：使用宿主机 IP（如果是 NAT）**
- 在 NAT 模式下，虚拟机应使用宿主机网关 IP
- 通常是 `192.168.x.1` 或 `10.0.x.1`
- 检查宿主机在 VMware 虚拟网络中的 IP：
  ```bash
  # 在 Rocky 虚拟机上
  ip route | grep default
  # 使用 gateway IP 替换 192.168.8.140
  ```

**解决方案C：端口映射（NAT 模式）**
- VMware → 编辑 → 虚拟网络编辑器 → NAT 设置
- 添加端口转发：宿主机 6751 → 虚拟机 192.168.8.140:6751

---

### 原因2：Docker 网络隔离

**问题**：AgentCenter 容器监听 `0.0.0.0:6751`，但端口只在 Docker 网络内可访问

**检查方法**：
```bash
# 在宿主机（Mac）上检查端口映射
docker port mxsec-agentcenter-dev
# 应该显示: 6751/tcp -> 0.0.0.0:6751

# 检查宿主机是否监听端口
netstat -an | grep 6751
# 或
lsof -i :6751
```

**解决方案**：
如果端口映射正确但仍无法连接，检查 Docker Compose 配置：
```yaml
# deploy/dev/docker-compose.dev.yml
services:
  agentcenter:
    ports:
      - "6751:6751"  # 确保这一行存在
```

---

### 原因3：防火墙阻止

**在 Rocky 虚拟机上检查**：
```bash
# 检查 firewalld 状态
systemctl status firewalld

# 如果运行中，添加允许规则（临时测试）
sudo firewall-cmd --zone=public --add-rich-rule='rule family="ipv4" source address="192.168.8.140" port port="6751" protocol="tcp" accept'

# 或暂时关闭防火墙测试
sudo systemctl stop firewalld
```

**在宿主机（Mac）上检查**：
```bash
# 检查 Mac 防火墙设置
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate

# 临时允许 Docker（如果被阻止）
```

---

### 原因4：gRPC "too_many_pings" 错误

从容器 Agent 日志看到：
```
error: "too_many_pings" - closing transport
```

**原因**：gRPC 客户端 ping 频率过高

**解决方案**：调整 Agent 连接参数（已在代码中）
- 当前心跳间隔：60秒
- gRPC keepalive 配置应该合理

---

## 逐步排查流程

### 1️⃣ 确认服务端正常

在宿主机（Mac）上：
```bash
# 检查 AgentCenter 运行状态
docker ps | grep agentcenter

# 检查日志
docker logs mxsec-agentcenter-dev --tail 50

# 测试本地连接
nc -zv localhost 6751
# 或
telnet localhost 6751
```

### 2️⃣ 确认网络可达

在 Rocky 虚拟机上：
```bash
# 基本连通性
ping -c 3 192.168.8.140

# TCP 端口测试
telnet 192.168.8.140 6751

# 如果 telnet 失败，检查路由
traceroute 192.168.8.140
```

### 3️⃣ 确认 Agent 配置

在 Rocky 虚拟机上：
```bash
# 检查 Agent 编译的 serverHost
/usr/bin/mxsec-agent -version
# 应该显示: Server: 192.168.8.140:6751

# 检查 Agent 日志
tail -f /var/log/mxsec-agent/agent.log

# 检查 Agent 状态
systemctl status mxsec-agent
```

### 4️⃣ 验证证书问题

Agent 首次连接时会使用"不安全模式"（跳过证书验证），所以证书不应该是问题。但可以检查：
```bash
# 检查证书目录
ls -la /var/lib/mxsec-agent/certs/

# 如果证书存在但连接失败，尝试删除证书重新连接
sudo rm -rf /var/lib/mxsec-agent/certs/*
sudo systemctl restart mxsec-agent
```

---

## 快速修复建议

### 方案1：使用桥接网络（最推荐）

1. VMware 设置虚拟机为桥接模式
2. 重启虚拟机，获取新 IP（应该与宿主机同网段）
3. 重新编译 Agent（如果需要改 IP）或者直接测试连接

### 方案2：修改 Server 地址

如果虚拟机在 NAT 网络，使用网关 IP：
```bash
# 在虚拟机上查看网关
ip route | grep default
# 输出类似: default via 192.168.x.1 dev ens33

# 重新编译 Agent，使用网关 IP
make package-agent-all SERVER_HOST="192.168.x.1:6751" VERSION="1.0.4"
```

### 方案3：使用宿主机真实 IP

如果 192.168.8.140 是 Docker 虚拟 IP，使用宿主机在物理网络的 IP：
```bash
# 在宿主机查看真实 IP
ifconfig en0 | grep "inet " | awk '{print $2}'

# 使用真实 IP 重新编译 Agent
```

---

## 调试模式

如果需要更详细的日志，修改 Agent 日志级别：
```bash
# 编辑 Agent 代码，临时启用 debug 日志
# internal/agent/logger/logger.go
Level: "debug"  # 改为 debug

# 重新编译并部署
make build-agent
```

---

## 成功标志

连接成功后，Agent 日志应该显示：
```
{"level":"info","msg":"gRPC stream established successfully","agent_id":"..."}
```

服务端日志应该显示：
```
{"level":"info","msg":"new agent connected","agent_id":"...","hostname":"Alienware"}
```

前端主机列表应该显示主机状态为"在线"。

---

## 联系支持

如果以上方法都无法解决，请提供：
1. 诊断脚本输出
2. Agent 完整日志（最近 100 行）
3. VMware 网络配置截图
4. 宿主机网络配置（`ifconfig` 输出）
