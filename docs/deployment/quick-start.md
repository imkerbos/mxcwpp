# 快速部署指南

使用 Docker Compose 启动当前 V2 控制面和依赖组件。

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

# 3. 启动所有服务（推荐显式指定控制面副本数）
docker compose --env-file .env up -d \
  --scale manager=2 \
  --scale agentcenter=2 \
  --scale consumer=2

# 4. 查看服务状态
docker compose ps
```

## 关键配置项 (.env)

```bash
VERSION=v1.0.0                  # 镜像版本
MYSQL_ROOT_PASSWORD=xxx         # MySQL root 密码
MYSQL_PASSWORD=xxx              # 应用数据库密码
JWT_SECRET=xxx                  # JWT 签名密钥
SERVER_IP=10.0.0.1              # 宿主机 IP
MANAGER_REPLICAS=2              # 控制面副本数（生产建议 2+）
AGENTCENTER_REPLICAS=2
CONSUMER_REPLICAS=2
```

**注意**：
- `SERVER_HOST` 不是 `.env` 配置项，而是 Agent 构建参数（`make package-agent-all SERVER_HOST=<AC外部入口>:6751`），编译时嵌入 Agent 二进制。
- 默认 compose 不直接暴露 `agentcenter:6751`，生产环境需额外做端口映射或前置四层负载均衡，再将该地址作为 `SERVER_HOST` 编译进 Agent。

## 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| UI | http://localhost | 管理界面 |
| Manager API | http://localhost/api/v1 | 通过 Nginx 代理访问 |
| MySQL | localhost:13306 | 数据库 |
| ClickHouse | localhost:8123 | 分析查询接口 |
| Kafka | localhost:9092 | Broker 1（其余为 9094/9095） |

**默认账户**: admin / admin123

## 验证

```bash
# 检查容器状态
docker compose ps

# 测试 API
curl -X POST http://localhost/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 查看日志
docker compose logs -f manager
docker compose logs -f agentcenter
docker compose logs -f consumer
```

## 常见问题

**端口冲突**: 修改 .env 中的端口映射或停止占用端口的服务。

**MySQL 初始化失败**: 清除 data 目录后重启: `docker compose down -v && docker compose up -d`

**服务启动顺序**: MySQL/Redis/Kafka/ClickHouse 需要先就绪，AgentCenter/Manager/Consumer 有健康检查依赖，通常自动处理。

## 停止/清理

```bash
docker compose down        # 停止服务（保留数据）
docker compose down -v     # 停止服务并删除数据卷
```
