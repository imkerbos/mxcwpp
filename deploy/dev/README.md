# Docker Compose 部署指南

> 使用 Docker Compose 快速启动 Matrix Cloud Security Platform 开发环境。

## 前置要求

- Docker >= 20.10
- Docker Compose >= 2.0

## 快速开始

### 方式一：使用 Makefile（推荐）

```bash
# 在项目根目录执行
# 一键启动开发环境（自动构建镜像并启动）
make dev-up

# 查看日志
make docker-logs

# 停止服务
make dev-down
```

### 方式二：手动执行

#### 1. 准备证书

```bash
# 在项目根目录执行
cd deploy/dev
mkdir -p certs
../../scripts/generate-certs.sh
```

#### 2. 启动服务

```bash
# 启动所有服务
docker-compose up -d

# 查看日志
docker-compose logs -f

# 查看服务状态
docker-compose ps
```

### 3. 验证服务

```bash
# 检查 MySQL
docker-compose exec mysql mysql -u mxsec_user -pmxsec_password -e "SHOW DATABASES;"

# 检查 AgentCenter（gRPC）
grpcurl -plaintext localhost:6751 list

# 检查 Manager（HTTP）
curl http://localhost:8080/health

# 检查 UI（前端）
curl http://localhost/
```

### 4. 停止服务

```bash
# 停止所有服务
docker-compose down

# 停止并删除数据卷（注意：会删除数据库数据）
docker-compose down -v
```

## 服务说明

### MySQL

- **端口**: 3306
- **数据库**: mxsec
- **用户**: mxsec_user
- **密码**: mxsec_password
- **数据卷**: mysql_data

### AgentCenter

- **端口**: 6751 (gRPC)
- **配置**: `configs/server.yaml`
- **证书**: `certs/`
- **日志**: `agentcenter_logs` 卷

### Manager

- **端口**: 8080 (HTTP)
- **配置**: `configs/server.yaml`
- **日志**: `manager_logs` 卷

### UI（前端）

- **端口**: 80 (HTTP)
- **访问地址**: http://localhost
- **API代理**: 自动代理 `/api` 请求到 Manager 服务
- **日志**: `ui_logs` 卷

## 配置说明

### 修改数据库密码

编辑 `docker-compose.yml` 中的 MySQL 环境变量：

```yaml
environment:
  MYSQL_ROOT_PASSWORD: your_password
  MYSQL_PASSWORD: your_password
```

同时修改 `configs/server.yaml` 中的数据库配置。

### 修改端口

编辑 `docker-compose.yml` 中的端口映射：

```yaml
ports:
  - "8080:8080"  # Manager API 端口
  - "80:80"      # UI 前端端口
```

**注意**：修改 UI 端口后，需要更新 `nginx.conf` 中的配置。

## 开发建议

1. **数据持久化**: 数据库数据存储在 `mysql_data` 卷中，删除容器不会丢失数据
2. **日志查看**: 使用 `make docker-logs` 或 `docker-compose logs -f` 实时查看日志
3. **重启服务**: 修改代码后，使用 `make docker-restart` 或 `docker-compose restart agentcenter` 重启服务
4. **重新构建**: 修改 Dockerfile 后，使用 `make docker-build` 或 `docker-compose build` 重新构建镜像
5. **清理环境**: 使用 `make docker-clean` 清理所有 Docker 资源（包括数据卷）

## 故障排查

### 服务无法启动

```bash
# 查看详细日志
docker-compose logs service_name

# 检查端口占用
netstat -tuln | grep -E '3306|6751|8080'
```

### 数据库连接失败

```bash
# 检查 MySQL 健康状态
docker-compose ps mysql

# 进入 MySQL 容器
docker-compose exec mysql mysql -u root -p
```

### 证书问题

```bash
# 重新生成证书
bash generate-certs.sh
cp ../../certs/* certs/
docker-compose restart agentcenter
```
