# Server 部署指南

本文档是 README 旧链接的兼容入口，面向当前 V2 `UI + Manager + AgentCenter + Consumer + MySQL + Redis + Kafka + ClickHouse` 部署。

## 组件说明

- `manager`：提供 `/api/v1/*` HTTP API
- `agentcenter`：提供 Agent 双向流 gRPC 接入和 HTTP 管理接口
- `consumer`：消费 Kafka 并写入 MySQL / ClickHouse / Redis
- `ui`：Web 管理界面，同时代理 API 和静态资源
- `mysql`：业务主库
- `redis`：缓存、部分在线状态与调度辅助
- `kafka`：业务消息总线
- `clickhouse`：事件与归档分析存储

## 前置要求

- Linux 服务器
- Docker 20.10+
- Docker Compose 2.x
- 可开放 80/443，以及按需额外暴露 6751 等端口

## 部署步骤

1. 复制 `deploy/.env.example` 为 `deploy/.env`。
2. 检查 `SERVER_HOST`、数据库密码、JWT 密钥。
3. 如需 Agent 从宿主机外接入，先规划 `agentcenter` gRPC 的对外暴露方式（host publish、HAProxy、SLB 等）。
4. 生成证书：`./scripts/generate-certs.sh`
5. 启动：`cd deploy && docker compose --env-file .env up -d --scale manager=2 --scale agentcenter=2 --scale consumer=2`
6. 查看状态：`docker compose ps`

## 常见目录

- 配置模板：`deploy/config/server.yaml.tpl`
- 反向代理：`deploy/config/nginx.conf`
- MySQL 调优：`deploy/config/mysql.cnf`
- systemd 示例：`deploy/systemd/`

## 运行验证

```bash
curl http://localhost/health
curl http://localhost/api/v1/health
docker compose logs -f manager
docker compose logs -f agentcenter
docker compose logs -f consumer
```

## 相关文档

- [服务端部署](server.md)
- [Server 配置文档](server-config.md)
- [故障排查](../troubleshooting.md)
