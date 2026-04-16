# 生产集群自动化部署

这份文档对应新的 `cluster.yaml + mxctl` 自动化方案，目标是把生产环境部署收敛成两种模式：

1. **All-in-One**：单机部署，继续使用现有 `deploy/docker-compose.yml`
2. **Cluster**：多机部署，统一使用 `cluster.yaml` 描述节点、入口、镜像和副本数，再由 `mxctl` 自动渲染并下发

## 方案边界

当前 v1 的 cluster 方案重点解决的是：

- 本地一次性描述 3 节点 / 5 节点 / 更多节点拓扑
- 通过 SSH 自动安装依赖、渲染配置、复制产物、启动服务
- 控制平面支持多节点扩展，沿用 `manager / agentcenter / consumer` 多副本模式
- 存储和消息总线先按单节点角色建模，避免第一版复杂度失控

当前 v1 明确限制：

- `storage` 节点只能有 1 个
- `kafka` 节点只能有 1 个，但该节点内会起 3 个 broker
- `manager_replicas` 和 `agentcenter_replicas` 不能少于 control 节点数量，保证每个控制平面节点都有本地 Manager / AC

## 推荐节点分配

### 3 节点标准形态

- Node1: `control`
- Node2: `storage`
- Node3: `kafka`

### 5 节点及以上

推荐先把新增节点给控制平面：

- Node1: `control`
- Node2: `control`
- Node3: `storage`
- Node4: `kafka`
- Node5+: 继续扩控制平面或预留后续存储高可用改造

控制平面节点前面挂 LB，统一转发：

- Web/API 到各 control 节点 `80`
- Agent gRPC 到各 control 节点 `6751`

## 配置文件

示例文件：`deploy/prod/cluster.example.yaml`

核心字段说明：

- `metadata`: 集群名称、环境标识
- `release`: 版本号、远端安装目录、数据目录
- `registry`: 生产镜像仓库信息
- `network.ui / network.grpc`: 对外入口地址，证书 SAN 也会一起生成
- `infrastructure`: MySQL / Redis / ClickHouse / Kafka 端口和口令
- `control_plane`: 控制平面副本数
- `nodes`: 节点 IP、SSH 账号、角色

## 推荐实施流程

正常 prod 部署建议按下面顺序执行，而不是直接 `go run`：

1. 先编译 `mxctl`
2. 再准备 `cluster.yaml`
3. 先做 `check`
4. 再做 `preflight`
5. 最后执行 `deploy`

### 1. 编译 mxctl

```bash
mkdir -p bin
go build -o ./bin/mxctl ./cmd/tools/mxctl
```

### 2. 准备集群配置

- 复制 `deploy/prod/cluster.example.yaml`
- 按真实 prod 环境修改为 `deploy/prod/cluster.prod.yaml`
- 至少确认以下字段已经替换为真实值：
  - `registry.domain / namespace / username / password`
  - `network.ui`
  - `network.grpc`
  - `app.jwt_secret`
  - `infrastructure.mysql.*`
  - `infrastructure.redis.password`
  - `infrastructure.clickhouse.password`
  - `nodes[*].host / ssh_user / ssh_key_path`

## 自动化命令

### 3. check：配置检查

```bash
./bin/mxctl check -f deploy/prod/cluster.prod.yaml
```

这一步只做本地检查，主要验证：

- `cluster.yaml` 结构是否正确
- 角色分配是否符合 v1 约束
- 副本数和节点数量是否匹配

### 4. preflight：预部署检查

```bash
./bin/mxctl preflight -f deploy/prod/cluster.prod.yaml
```

这一步会检查：

- 本地是否存在 `ssh` / `scp`
- 是否能通过 SSH 连通每个节点
- 远端系统是否属于当前支持范围
- 非 root 用户下是否具备 `sudo`
- 远端 `install_dir` / `data_root` 是否可创建

### 5. render：渲染节点 bundle

```bash
./bin/mxctl render -f deploy/prod/cluster.prod.yaml
```

### 6. deploy：正式部署

```bash
./bin/mxctl deploy -f deploy/prod/cluster.prod.yaml
```

部署动作按顺序执行：

1. 把节点 bundle 上传到远端 `install_dir/releases/<version-timestamp>`
2. 远端安装 `git curl openssl jq docker docker compose`
3. 如配置了仓库账号，则执行 `docker login`
4. 先启动 `kafka`
5. 再启动 `storage`
6. 最后启动 `control`
7. 进行基础 `docker compose ps` 和 `curl /health` 检查

`render` 默认输出到：`deploy/prod/out/<cluster-name>/`

渲染结果会按节点生成：

- `compose/docker-compose.control.yml`
- `compose/docker-compose.storage.yml`
- `compose/docker-compose.kafka.yml`
- `config/server.yaml`
- `config/nginx.conf`
- `config/haproxy-agentcenter.cfg`
- `certs/*`
- `scripts/install-deps.sh`

## 镜像策略建议

生产主流程建议使用 **CI 构建 + 推送私有仓库**，本地构建只作为补充。

推荐原因：

- 版本可追溯，便于回滚
- 所有节点拉取同一份镜像，避免本地编译差异
- 更适合后续接入签名、漏洞扫描和发布审批

本仓库也保留本地构建脚本：

```bash
./scripts/build-images.sh --version v1.0.0 --registry harbor.example.com/mxsec --push
```

## 关于共享上传目录

当前控制平面多节点方案有一个必须提前知道的边界：

- Manager 的上传文件、插件包、Agent 安装包默认落本地文件系统
- 如果多个 control 节点同时对外提供下载，最好为 `/data/mxsec/plugins` 和 `/data/mxsec/uploads` 提供共享存储

如果暂时没有共享存储，建议：

- 先用单 control 节点上线 cluster v1
- 或者在 LB 层对上传/下载流量做固定路由

这个问题不影响当前自动化部署落地，但它是后续做严格 HA 时必须补上的生产项。

## All-in-One 与 Cluster 的关系

- **All-in-One**：继续用 `deploy/docker-compose.yml`，适合单机或快速验证
- **Cluster**：用 `cluster.yaml + mxctl`，适合正式生产和多节点扩展

两者不是互斥关系：

- 单机：用 `docker-compose.yml`
- 多机：用 `cluster.yaml`
- 后续节点变多时，只需要继续改 `cluster.yaml`，不需要换部署模型
