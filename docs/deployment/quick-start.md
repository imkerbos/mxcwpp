# 快速部署指南

使用 Docker Compose 一键部署完整环境。

## 前置要求

- Docker >= 20.10
- Docker Compose >= 2.0
- Git

## 部署步骤

```bash
# 1. 克隆仓库
git clone https://github.com/imkerbos/mxsec-platform.git
cd mxsec-platform/deploy

# 2. 配置环境变量
cp .env.example .env
vim .env   # 修改关键配置（密码、服务器地址等）

# 3. 启动所有服务
docker compose --env-file .env up -d

# 4. 查看服务状态
docker compose ps
```

## 关键配置项 (.env)

```bash
VERSION=v1.0.0                  # 镜像版本
MYSQL_ROOT_PASSWORD=xxx         # MySQL root 密码
MYSQL_PASSWORD=xxx              # 应用数据库密码
JWT_SECRET=xxx                  # JWT 签名密钥
SERVER_HOST=your-server:6751    # AgentCenter 对外地址（Agent 连接用）
```

## 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| UI | http://localhost:3000 | 管理界面 |
| Manager API | http://localhost:8080 | HTTP API |
| AgentCenter | localhost:6751 | gRPC（Agent 连接） |
| MySQL | localhost:3306 | 数据库 |

**默认账户**: admin / admin123

## 验证

```bash
# 检查容器状态
docker compose ps

# 测试 API
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 查看日志
docker compose logs -f manager
docker compose logs -f agentcenter
```

## 常见问题

**端口冲突**: 修改 .env 中的端口映射或停止占用端口的服务。

**MySQL 初始化失败**: 清除 data 目录后重启: `docker compose down -v && docker compose up -d`

**服务启动顺序**: MySQL 需要先就绪，AgentCenter/Manager 有健康检查依赖，通常自动处理。

## 停止/清理

```bash
docker compose down        # 停止服务（保留数据）
docker compose down -v     # 停止服务并删除数据卷
```
