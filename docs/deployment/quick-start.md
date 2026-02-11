# 快速部署指南

> 本文档提供 Matrix Cloud Security Platform 的快速部署指南，包括开发环境和生产环境的部署方式。

---

## 1. 开发环境部署（Docker Compose）

### 1.1 一键启动

```bash
# 在项目根目录执行
make dev-up
```

这将自动：
- 构建 Docker 镜像
- 启动 MySQL、AgentCenter、Manager 服务
- 初始化数据库

### 1.2 验证服务

```bash
# 检查服务状态
make docker-ps

# 检查 Manager API
curl http://localhost:8080/health

# 检查 AgentCenter（需要 grpcurl）
grpcurl -plaintext localhost:6751 list
```

### 1.3 查看日志

```bash
# 查看所有服务日志
make docker-logs

# 查看特定服务日志
cd deploy/dev
docker-compose logs -f agentcenter
```

### 1.4 停止服务

```bash
# 停止服务
make dev-down

# 停止并清理数据
make docker-clean
```

详细说明请参考 [Docker Compose 部署指南](../../deploy/dev/README.md)。

---

## 2. 生产环境部署

### 2.1 Agent 部署

#### 方式一：使用安装包（推荐）

```bash
# 1. 构建 Agent 安装包（指定发行版）
# Rocky Linux 9
make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0 DISTRO=rocky9

# Debian 12
make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0 DISTRO=debian12

# 或使用通用包（不指定发行版）
make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0

# 2. 安装（RPM）
sudo rpm -ivh dist/packages/mxsec-agent-1.0.0-*.rpm

# 或安装（DEB）
sudo dpkg -i dist/packages/mxsec-agent_1.0.0_*.deb
```

**支持的发行版：**
- RPM: `centos7`, `centos8`, `rocky8`, `rocky9`, `el7`, `el8`, `el9`
- DEB: `debian10`, `debian11`, `debian12`, `ubuntu20`, `ubuntu22`

详细说明请参考 [发行版支持文档](./distribution-support.md)。

# 3. 启动服务
sudo systemctl start mxsec-agent
sudo systemctl enable mxsec-agent
```

#### 方式二：手动安装

```bash
# 1. 构建 Agent 二进制
make build-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0

# 2. 复制文件
sudo cp dist/agent/mxsec-agent-linux-amd64 /usr/bin/mxsec-agent
sudo chmod +x /usr/bin/mxsec-agent

# 3. 安装 systemd service
sudo cp deploy/systemd/mxsec-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agent
sudo systemctl start mxsec-agent
```

详细说明请参考 [Agent 部署指南](./agent-deployment.md)。

---

### 2.2 Server 部署

#### 方式一：使用安装包（推荐）

```bash
# 1. 构建 Server 安装包（指定发行版）
# Rocky Linux 9
make package-server VERSION=1.0.0 DISTRO=rocky9

# Debian 12
make package-server VERSION=1.0.0 DISTRO=debian12

# 或使用通用包（不指定发行版）
make package-server VERSION=1.0.0

# 2. 安装（RPM）
sudo rpm -ivh dist/packages/mxsec-server-1.0.0-*.rpm

# 或安装（DEB）
sudo dpkg -i dist/packages/mxsec-server_1.0.0_*.deb
```

# 3. 配置
sudo cp /etc/mxcsec-platform/server.yaml.example /etc/mxcsec-platform/server.yaml
sudo vi /etc/mxcsec-platform/server.yaml  # 修改数据库和证书配置

# 4. 生成证书
cd /path/to/mxcsec-platform
sudo ./scripts/generate-certs.sh
sudo cp certs/* /etc/mxcsec-platform/certs/

# 5. 启动服务
sudo systemctl start mxsec-agentcenter mxsec-manager
sudo systemctl enable mxsec-agentcenter mxsec-manager
```

#### 方式二：手动安装

```bash
# 1. 构建 Server 二进制
make build-server

# 2. 创建目录
sudo mkdir -p /opt/mxcsec-platform
sudo mkdir -p /etc/mxcsec-platform
sudo mkdir -p /var/log/mxcsec-platform

# 3. 复制文件
sudo cp dist/server/agentcenter /usr/bin/mxsec-agentcenter
sudo cp dist/server/manager /usr/bin/mxsec-manager
sudo cp configs/server.yaml.example /etc/mxcsec-platform/server.yaml
sudo chmod +x /usr/bin/mxsec-agentcenter
sudo chmod +x /usr/bin/mxsec-manager

# 4. 配置
sudo vi /etc/mxcsec-platform/server.yaml  # 修改配置

# 5. 生成证书
sudo ./scripts/generate-certs.sh
sudo cp certs/* /etc/mxcsec-platform/certs/

# 6. 安装 systemd service
sudo cp deploy/systemd/mxsec-agentcenter.service /etc/systemd/system/
sudo cp deploy/systemd/mxsec-manager.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agentcenter mxsec-manager
sudo systemctl start mxsec-agentcenter mxsec-manager
```

详细说明请参考 [Server 部署指南](./server-deployment.md)。

---

## 3. Makefile 命令参考

### 构建命令

```bash
# 构建 Agent
make build-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0

# 构建 Server
make build-server

# 打包 Agent（RPM/DEB）
make package-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0

# 打包 Server（RPM/DEB）
make package-server VERSION=1.0.0

# 打包所有
make package-all SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0
```

### Docker 命令

```bash
# 构建镜像
make docker-build

# 启动服务
make docker-up

# 停止服务
make docker-down

# 查看日志
make docker-logs

# 重启服务
make docker-restart

# 清理资源
make docker-clean

# 一键启动开发环境
make dev-up

# 停止开发环境
make dev-down
```

### 其他命令

```bash
# 生成 Protobuf 代码
make proto

# 运行测试
make test

# 格式化代码
make fmt

# 代码检查
make lint

# 生成证书
make certs

# 查看帮助
make help
```

---

## 4. 配置说明

### 4.1 Agent 配置

Agent 配置通过构建时嵌入，无需配置文件：

```bash
# 构建时指定 Server 地址
make build-agent SERVER_HOST=10.0.0.1:6751 VERSION=1.0.0
```

### 4.2 Server 配置

Server 配置文件：`/etc/mxcsec-platform/server.yaml`

主要配置项：
- **数据库配置**：MySQL/PostgreSQL 连接信息
- **mTLS 证书**：CA、Server 证书路径
- **日志配置**：日志级别、输出格式、文件路径
- **服务端口**：gRPC（6751）、HTTP（8080）

详细说明请参考 [Server 配置文档](./server-config.md)。

---

## 5. 验证部署

### 5.1 验证 Agent

```bash
# 检查服务状态
sudo systemctl status mxsec-agent

# 查看日志
sudo journalctl -u mxsec-agent -f

# 检查连接（需要 Server 已启动）
# Agent 会自动连接到 Server 并上报心跳
```

### 5.2 验证 Server

```bash
# 检查服务状态
sudo systemctl status mxsec-agentcenter
sudo systemctl status mxsec-manager

# 检查 AgentCenter（gRPC）
grpcurl -plaintext localhost:6751 list

# 检查 Manager（HTTP）
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/hosts

# 查看日志
sudo journalctl -u mxsec-agentcenter -f
sudo journalctl -u mxsec-manager -f
```

---

## 6. 故障排查

### 6.1 Agent 无法连接 Server

```bash
# 检查网络连接
ping 10.0.0.1
telnet 10.0.0.1 6751

# 检查防火墙
sudo firewall-cmd --list-ports

# 查看 Agent 日志
sudo journalctl -u mxsec-agent -n 100
```

### 6.2 Server 无法启动

```bash
# 检查配置文件
sudo /usr/bin/mxsec-agentcenter -config /etc/mxcsec-platform/server.yaml

# 检查数据库连接
mysql -h localhost -u mxsec_user -p mxsec

# 检查证书
ls -la /etc/mxcsec-platform/certs/
openssl x509 -in /etc/mxcsec-platform/certs/server.crt -text -noout

# 查看日志
sudo journalctl -u mxsec-agentcenter -n 100
```

### 6.3 数据库连接失败

```bash
# 测试数据库连接
mysql -h localhost -u mxsec_user -p mxsec

# 检查数据库配置
cat /etc/mxcsec-platform/server.yaml | grep -A 10 database

# 检查数据库服务
sudo systemctl status mysql
```

---

## 7. 升级指南

### 7.1 Agent 升级

```bash
# 1. 停止服务
sudo systemctl stop mxsec-agent

# 2. 备份旧版本
sudo cp /usr/bin/mxsec-agent /usr/bin/mxsec-agent.bak

# 3. 安装新版本
sudo rpm -Uvh mxsec-agent-1.1.0-*.rpm

# 或手动替换
sudo cp dist/agent/mxsec-agent-linux-amd64 /usr/bin/mxsec-agent

# 4. 启动服务
sudo systemctl start mxsec-agent
```

### 7.2 Server 升级

```bash
# 1. 备份数据库
mysqldump -u mxsec_user -p mxsec > backup_$(date +%Y%m%d).sql

# 2. 停止服务
sudo systemctl stop mxsec-agentcenter mxsec-manager

# 3. 备份配置和二进制
sudo cp /etc/mxcsec-platform/server.yaml /etc/mxcsec-platform/server.yaml.bak
sudo cp /usr/bin/mxsec-agentcenter /usr/bin/mxsec-agentcenter.bak
sudo cp /usr/bin/mxsec-manager /usr/bin/mxsec-manager.bak

# 4. 安装新版本
sudo rpm -Uvh mxsec-server-1.1.0-*.rpm

# 5. 启动服务
sudo systemctl start mxsec-agentcenter mxsec-manager
```

---

## 8. 参考文档

- [Agent 部署指南](./agent-deployment.md)
- [Server 部署指南](./server-deployment.md)
- [Server 配置文档](./server-config.md)
- [发行版支持文档](./distribution-support.md)
- [Docker Compose 部署指南](./docker-compose/README.md)
