# Server 部署指南

> 本文档描述 Matrix Cloud Security Platform Server 的部署方式。

---

## 1. 部署方式概述

Server 支持三种部署方式：

1. **Docker Compose 部署**（推荐，适合开发/测试环境）
2. **二进制部署**（适合生产环境）
3. **Kubernetes 部署**（适合大规模生产环境，Phase 2）

---

## 2. Docker Compose 部署（推荐）

### 2.1 前置要求

- Docker >= 20.10
- Docker Compose >= 2.0

### 2.2 快速开始

```bash
# 1. 进入部署目录
cd deploy/dev

# 2. 生成证书
mkdir -p certs
cp ../../scripts/generate-certs.sh .
bash generate-certs.sh
cp ../../certs/* certs/

# 3. 启动服务
docker-compose up -d

# 4. 查看日志
docker-compose logs -f

# 5. 验证服务
curl http://localhost:8080/api/v1/hosts
```

### 2.3 服务说明

- **MySQL**: 端口 3306，数据库 `mxsec`
- **AgentCenter**: 端口 6751 (gRPC)
- **Manager**: 端口 8080 (HTTP)

详细说明请参考 `deploy/dev/README.md`。

---

## 3. 二进制部署（生产环境）

### 3.1 前置要求

- MySQL >= 8.0 或 PostgreSQL >= 12
- Linux 系统（推荐 Rocky Linux 9、Debian 12）
- systemd（用于服务管理）

### 3.2 安装步骤

#### 3.2.1 准备数据库

```bash
# 创建数据库
mysql -u root -p
CREATE DATABASE mxsec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'mxsec_user'@'%' IDENTIFIED BY 'your_password';
GRANT ALL PRIVILEGES ON mxsec.* TO 'mxsec_user'@'%';
FLUSH PRIVILEGES;
```

**注意**：数据库表结构会在 Server 首次启动时通过 Gorm AutoMigrate 自动创建，无需手动执行 SQL 脚本。

#### 3.2.2 构建 Server

```bash
# 构建 Server 二进制
make build-server

# 或手动构建
go build -ldflags "-s -w" -o agentcenter ./cmd/server/agentcenter
go build -ldflags "-s -w" -o manager ./cmd/server/manager
```

#### 3.2.3 安装 Server

```bash
# 创建目录
sudo mkdir -p /opt/mxcsec-platform
sudo mkdir -p /etc/mxcsec-platform
sudo mkdir -p /var/log/mxcsec-platform

# 复制二进制文件
sudo cp dist/server/agentcenter /opt/mxcsec-platform/
sudo cp dist/server/manager /opt/mxcsec-platform/

# 复制配置文件
sudo cp configs/server.yaml.example /etc/mxcsec-platform/server.yaml
sudo vi /etc/mxcsec-platform/server.yaml  # 修改配置

# 复制证书
sudo mkdir -p /etc/mxcsec-platform/certs
sudo cp certs/* /etc/mxcsec-platform/certs/

# 设置权限
sudo chmod +x /opt/mxcsec-platform/agentcenter
sudo chmod +x /opt/mxcsec-platform/manager
sudo chown -R root:root /opt/mxcsec-platform
sudo chown -R root:root /etc/mxcsec-platform
sudo chown -R root:root /var/log/mxcsec-platform
```

#### 3.2.4 配置 systemd 服务

```bash
# 复制 systemd service 文件
sudo cp deploy/systemd/mxsec-agentcenter.service /etc/systemd/system/
sudo cp deploy/systemd/mxsec-manager.service /etc/systemd/system/

# 修改 service 文件中的路径（如果需要）
sudo vi /etc/systemd/system/mxsec-agentcenter.service
sudo vi /etc/systemd/system/mxsec-manager.service

# 启动服务
sudo systemctl daemon-reload
sudo systemctl enable mxsec-agentcenter
sudo systemctl enable mxsec-manager
sudo systemctl start mxsec-agentcenter
sudo systemctl start mxsec-manager

# 检查状态
sudo systemctl status mxsec-agentcenter
sudo systemctl status mxsec-manager
```

### 3.3 初始化数据库

Server 启动时会自动执行数据库迁移，创建所有必要的表结构。如果需要初始化默认策略数据，可以：

```bash
# 方法1：通过 Server 启动时自动初始化（如果数据库为空）
# Server 会在首次启动时检查数据库，如果为空则自动加载默认策略

# 方法2：手动初始化（可选）
# 确保策略文件目录存在
ls -la plugins/baseline/config/examples/

# Server 启动时会自动从该目录加载策略（如果数据库为空）
```

**注意**：默认策略文件位于 `plugins/baseline/config/examples/` 目录，包含 SSH、密码策略、文件权限等示例规则。

### 3.4 验证部署

```bash
# 1. 检查 AgentCenter（gRPC）
grpcurl -plaintext localhost:6751 list

# 2. 检查 Manager（HTTP）健康检查
curl http://localhost:8080/health
# 预期输出：{"status":"ok"}

# 3. 检查 Prometheus metrics
curl http://localhost:8080/metrics

# 4. 检查 API 端点
curl http://localhost:8080/api/v1/hosts
curl http://localhost:8080/api/v1/policies

# 5. 查看日志
sudo journalctl -u mxsec-agentcenter -f
sudo journalctl -u mxsec-manager -f

# 6. 检查数据库表是否创建成功
mysql -u mxsec_user -p mxsec -e "SHOW TABLES;"
# 应该看到：hosts, policies, rules, scan_results, scan_tasks 等表
```

---

## 4. 配置说明

### 4.1 数据库配置

编辑 `configs/server.yaml`：

```yaml
database:
  type: "mysql"  # 或 "postgres"
  mysql:
    host: "localhost"
    port: 3306
    user: "mxsec_user"
    password: "your_password"
    database: "mxsec"
```

### 4.2 mTLS 证书配置

```bash
# 生成证书
./scripts/generate-certs.sh

# 配置路径
mtls:
  ca_cert: "/etc/mxcsec-platform/certs/ca.crt"
  server_cert: "/etc/mxcsec-platform/certs/server.crt"
  server_key: "/etc/mxcsec-platform/certs/server.key"
```

### 4.3 日志配置

```yaml
log:
  level: "info"  # debug, info, warn, error
  format: "json"  # json 或 console
  file: "/var/log/mxcsec-platform/server.log"
  max_age: 30  # 保留天数
```

---

## 5. 防火墙配置

```bash
# 开放 gRPC 端口（AgentCenter）
sudo firewall-cmd --permanent --add-port=6751/tcp
sudo firewall-cmd --reload

# 开放 HTTP 端口（Manager）
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

---

## 6. 故障排查

### 6.1 服务无法启动

```bash
# 查看日志
sudo journalctl -u mxsec-agentcenter -n 100
sudo journalctl -u mxsec-manager -n 100

# 检查配置文件
sudo /opt/mxcsec-platform/agentcenter -config /etc/mxcsec-platform/server.yaml

# 检查端口占用
sudo netstat -tuln | grep -E '6751|8080'
```

### 6.2 数据库连接失败

```bash
# 测试数据库连接
mysql -h localhost -u mxsec_user -p mxsec

# 检查数据库配置
cat /etc/mxcsec-platform/server.yaml | grep -A 10 database
```

### 6.3 证书问题

```bash
# 检查证书文件
ls -la /etc/mxcsec-platform/certs/

# 验证证书
openssl x509 -in /etc/mxcsec-platform/certs/server.crt -text -noout
```

---

## 7. 性能优化

### 7.1 数据库连接池

```yaml
database:
  mysql:
    max_idle_conns: 10
    max_open_conns: 100
    conn_max_lifetime: "1h"
```

### 7.2 日志级别

生产环境建议使用 `info` 级别：

```yaml
log:
  level: "info"  # 生产环境使用 info，开发环境使用 debug
```

---

## 8. 备份与恢复

### 8.1 数据库备份

```bash
# 备份数据库
mysqldump -u mxsec_user -p mxsec > backup_$(date +%Y%m%d).sql

# 恢复数据库
mysql -u mxsec_user -p mxsec < backup_20250115.sql
```

### 8.2 配置文件备份

```bash
# 备份配置和证书
tar -czf server_backup_$(date +%Y%m%d).tar.gz \
  /etc/mxcsec-platform/server.yaml \
  /etc/mxcsec-platform/certs/
```

---

## 9. 升级指南

### 9.1 备份数据

```bash
# 备份数据库
mysqldump -u mxsec_user -p mxsec > upgrade_backup.sql

# 备份配置
cp /etc/mxcsec-platform/server.yaml /etc/mxcsec-platform/server.yaml.bak
```

### 9.2 停止服务

```bash
sudo systemctl stop mxsec-agentcenter
sudo systemctl stop mxsec-manager
```

### 9.3 更新二进制

```bash
# 备份旧版本
sudo cp /opt/mxcsec-platform/agentcenter /opt/mxcsec-platform/agentcenter.bak
sudo cp /opt/mxcsec-platform/manager /opt/mxcsec-platform/manager.bak

# 复制新版本
sudo cp dist/server/agentcenter /opt/mxcsec-platform/
sudo cp dist/server/manager /opt/mxcsec-platform/
```

### 9.4 启动服务

```bash
sudo systemctl start mxsec-agentcenter
sudo systemctl start mxsec-manager
sudo systemctl status mxsec-agentcenter
sudo systemctl status mxsec-manager
```

---

## 10. 监控建议

### 10.1 健康检查

#### AgentCenter（gRPC）

```bash
# 检查 gRPC 服务是否运行
grpcurl -plaintext localhost:6751 list

# 预期输出：grpc.Transfer（如果服务正常）
```

#### Manager（HTTP）

```bash
# 健康检查端点
curl http://localhost:8080/health
# 预期输出：{"status":"ok"}

# Prometheus metrics 端点
curl http://localhost:8080/metrics

# API 端点检查
curl http://localhost:8080/api/v1/hosts
curl http://localhost:8080/api/v1/policies
```

#### 数据库连接检查

```bash
# 检查数据库连接
mysql -u mxsec_user -p mxsec -e "SELECT COUNT(*) FROM hosts;"

# 检查表结构
mysql -u mxsec_user -p mxsec -e "DESCRIBE hosts;"
```

### 10.2 日志监控

```bash
# 实时查看日志
sudo journalctl -u mxsec-agentcenter -f
sudo journalctl -u mxsec-manager -f

# 查看错误日志
sudo journalctl -u mxsec-agentcenter -p err
sudo journalctl -u mxsec-manager -p err
```

---

## 11. 安全建议

1. **使用强密码**：数据库密码、证书密钥
2. **限制访问**：使用防火墙限制访问来源
3. **定期更新**：及时更新 Server 版本
4. **证书管理**：定期轮换 mTLS 证书
5. **日志审计**：定期检查日志，发现异常

---

## 12. 数据库初始化说明

### 12.1 自动迁移

Server 启动时会自动执行数据库迁移（Gorm AutoMigrate），创建以下表：

- `hosts`: 主机信息表
- `policies`: 策略集表
- `rules`: 规则表
- `scan_results`: 检测结果表
- `scan_tasks`: 扫描任务表

### 12.2 默认数据初始化

如果数据库为空（`policies` 表无数据），Server 会自动从 `plugins/baseline/config/examples/` 目录加载默认策略文件：

- `ssh-baseline.json`: SSH 安全配置基线
- `password-policy.json`: 密码策略基线
- `file-permissions.json`: 文件权限基线
- 其他策略文件...

**注意**：如果数据库中已有策略数据，则不会重复初始化。

### 12.3 手动初始化（可选）

如果需要手动初始化或重新初始化策略数据：

```bash
# 1. 备份现有数据（如果重要）
mysqldump -u mxsec_user -p mxsec > backup.sql

# 2. 清空策略表（谨慎操作）
mysql -u mxsec_user -p mxsec -e "TRUNCATE TABLE rules; TRUNCATE TABLE policies;"

# 3. 重启 Server，会自动重新初始化
sudo systemctl restart mxsec-agentcenter
sudo systemctl restart mxsec-manager
```

---

## 13. 参考文档

- [Server 配置文档](./server-config.md)
- [Agent 部署指南](./agent-deployment.md)
- [开发文档](../development/plugin-development.md)
- [数据库迁移说明](../../internal/server/migration/README.md)
