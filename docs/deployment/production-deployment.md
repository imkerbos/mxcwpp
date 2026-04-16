# 生产环境部署方案

本文档补齐 README 中的旧链接入口，内容以当前仓库中的 [服务端部署](server.md)、[快速部署指南](quick-start.md)、[`deploy/README.md`](../../deploy/README.md) 和 [生产集群自动化部署](prod-cluster-deployment.md) 为准。

## 适用场景

- 单机或小规模内网生产部署
- 需要通过 Docker Compose 统一管理 V2 控制面组件
- 需要启用 `manager`、`agentcenter`、`consumer` 多副本控制面
- 需要启用 `kafka`、`clickhouse` 以承接异步消费与归档分析链路

## 当前部署形态

核心服务：

- `ui`：前端静态资源和反向代理入口
- `manager`：HTTP API 服务，支持多实例
- `agentcenter`：Agent gRPC 接入服务，支持多实例
- `consumer`：Kafka 消费写入链路，支持多实例
- `mysql`：核心业务数据
- `redis`：缓存、服务发现同步状态、调度锁

推荐服务：

- `kafka`：消息总线
- `clickhouse`：监控和分析类时序/大数据存储

## 当前高可用边界

- `manager / agentcenter / consumer` 已支持多副本部署
- `Kafka` 已按 3 Broker 形态提供 Compose 方案
- `MySQL / Redis / ClickHouse` 在仓库默认部署里仍偏单节点，若要求严格容灾，需要额外建设主从、Sentinel 或集群方案

## 推荐部署流程

生产环境现在推荐按两条路径实施：

1. **All-in-One**
   - 在构建机构建镜像或打包部署包
   - 在目标服务器准备 `.env`
   - 用 `deploy/config/server.yaml.tpl` 渲染服务端配置
   - 生成 mTLS 证书
   - 启动 `docker compose`
2. **Cluster**
   - 先编译 `mxctl`
   - 再准备 `cluster.yaml`
   - 先执行 `mxctl check`
   - 再执行 `mxctl preflight`
   - 最后执行 `mxctl deploy`

多机生产部署的具体步骤见：`docs/deployment/prod-cluster-deployment.md`

## 关键文件

- `deploy/docker-compose.yml`
- `deploy/.env.example`
- `deploy/config/server.yaml.tpl`
- `deploy/config/nginx.conf`
- `deploy/config/mysql.cnf`
- `deploy/deploy.sh`

## 配置重点

- `SERVER_HOST` 是 **Agent 构建参数**（通过 `make package-agent-all SERVER_HOST=...` 编译时嵌入），不是服务端 `.env` 配置项。它决定 Agent 连接哪个 AC gRPC 地址，生产环境应指向四层 LB 或 AC 的外部入口。
- `JWT_SECRET` 生产环境必须替换默认值。
- `PLUGINS_BASE_URL` 需要能被 Agent 直接访问。
- V2 架构下建议同时启用 `kafka` / `clickhouse` / `consumer`。
- 默认 compose 下管理 API 通过 `ui` 的 `/api/*` 访问，`manager` 不直接暴露宿主机端口。
- 默认 compose 下 `agentcenter` 不直接 publish 宿主机 `6751`，生产环境需要额外做端口映射，或前置 HAProxy / SLB / Nginx stream。
- 如果启用多副本控制面，建议显式使用 `--scale manager=2 --scale agentcenter=2 --scale consumer=2`，并确认 `Nginx upstream`、AC 注册发现和内部访问地址一致。

## 验证项

- `GET /health` 返回正常（匿名）
- `POST /api/v1/auth/login` 可通过 `ui` 代理成功登录（匿名，返回 JWT）
- 若已额外暴露 AC gRPC，`agentcenter` 入口可连通 `6751`
- `GET /api/v1/discovery/agentcenter` 能返回健康 AC 列表
  - **此接口挂在 `apiV1Auth` 下，必须先登录并携带 `Authorization: Bearer <token>` 调用**，匿名请求会返回 401，不代表服务异常
  - 验证示例：`curl -H "Authorization: Bearer $TOKEN" https://<host>/api/v1/discovery/agentcenter`
- `consumer` 正常消费 Kafka Topic，DLQ 无持续堆积
- 新安装 Agent 能完成首次不校验证书接入，并在后续连接切换到正式 mTLS

推荐启动示例：

```bash
cd deploy
docker compose --env-file .env up -d \
  --scale manager=2 \
  --scale agentcenter=2 \
  --scale consumer=2
```

## 相关文档

- [服务端部署](server.md)
- [快速部署指南](quick-start.md)
- [Server 配置文档](server-config.md)
- [`deploy/README.md`](../../deploy/README.md)
