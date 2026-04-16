# Server 配置文档

当前服务端配置由两部分组成：

- 开发/裸跑示例：`configs/server.yaml.example`
- 部署模板：`deploy/config/server.yaml.tpl`

## 配置结构

### `server`

- `grpc.host` / `grpc.port`：`AgentCenter` 监听地址
- `http.host` / `http.port`：`Manager` 监听地址
- `jwt_secret`：JWT 签名密钥
- `manager_addr`：AgentCenter 向 Manager 注册使用的地址
- `instance_id`：多实例部署时的实例标识

### `database`

- 当前默认实现使用 `mysql`
- 连接池参数包括 `max_idle_conns`、`max_open_conns`、`conn_max_lifetime`

### `redis`

- 支持单节点模式
- 支持 Sentinel 模式
- 集群模式字段已预留，但当前未见完整实现链路

### `kafka`

- `enabled` 控制是否启用消息总线
- `brokers` 支持多 broker
- `topic_prefix` 用于环境隔离

### `clickhouse`

- `enabled` 控制是否启用 ClickHouse 写入
- 用于监控/分析型数据存储

### `mtls`

- `ca_cert`
- `server_cert`
- `server_key`

### `log`

- `level`
- `format`
- `file`
- `error_file`
- `max_age`

### `agent`

- `heartbeat_interval`
- `work_dir`

这部分配置会下发给 Agent。

### `plugins`

- `dir`：服务端本地插件目录
- `base_url`：Agent 下载插件时使用的 URL 前缀

## `.env` 到模板映射

部署时，`deploy.sh` / 开发脚本会把 `.env` 中的值替换到 `server.yaml.tpl` 的 `__XXX__` 占位符。

常用变量：

- `GRPC_PORT`
- `SERVER_HTTP_PORT`
- `JWT_SECRET`
- `MYSQL_HOST`
- `MYSQL_PORT`
- `MYSQL_USER`
- `MYSQL_PASSWORD`
- `MYSQL_DATABASE`
- `REDIS_ADDR`
- `KAFKA_ENABLED`
- `CLICKHOUSE_ENABLED`
- `PLUGINS_DIR`
- `PLUGINS_BASE_URL`

## 配置建议

- 生产环境不要直接编辑渲染后的 `server.yaml`，应修改 `.env` 并重新生成。
- `manager_addr` 需要保证 AgentCenter 能回连 Manager。
- `plugins.base_url` 不能写成 Agent 无法访问的 `localhost`。
- 启用 Sentinel / Kafka / ClickHouse 时，必须保证对应服务端点可达。

## 相关文档

- [服务端部署](server.md)
- [生产环境部署方案](production-deployment.md)
