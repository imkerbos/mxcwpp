# Matrix Cloud Security Platform - 生产环境部署

## 部署流程

### 1. 开发机：构建镜像

```bash
# 构建镜像到本地
./scripts/build-images.sh --version v1.0.0

# 或构建并推送到私有仓库
./scripts/build-images.sh --version v1.0.0 --registry harbor.example.com/mxsec --push
```

### 2. 开发机：打包部署包

```bash
# 生成部署包
./scripts/package-deploy.sh --version v1.0.0

# 如果使用私有仓库
./scripts/package-deploy.sh --version v1.0.0 --registry harbor.example.com/mxsec

# 输出: dist/deploy/mxsec-platform-v1.0.0.tar.gz
```

### 3. 生产服务器：部署

```bash
# 上传并解压
scp dist/deploy/mxsec-platform-v1.0.0.tar.gz root@server:/opt/
ssh root@server

cd /opt
tar -xzf mxsec-platform-v1.0.0.tar.gz
cd mxsec-platform-v1.0.0

# 交互式部署
./deploy.sh
```

---

## 部署包内容

```
mxsec-platform-v1.0.0/
├── deploy.sh           # 部署脚本
├── docker-compose.yml  # 服务编排
├── init.sql            # 数据库初始化
├── config/
│   ├── server.yaml     # Server 配置（模板）
│   ├── nginx.conf      # Nginx 配置
│   └── mysql.cnf       # MySQL 配置
└── certs/              # 证书目录（部署时生成）
```

---

## 管理命令

```bash
./deploy.sh start       # 启动服务
./deploy.sh stop        # 停止服务
./deploy.sh restart     # 重启全部（或指定服务: restart agentcenter）
./deploy.sh status      # 查看服务状态
./deploy.sh logs        # 查看日志（或指定服务: logs agentcenter）
./deploy.sh backup      # 备份数据库（gzip 压缩，自动清理 30 天前备份）
./deploy.sh upgrade     # 升级服务（自动备份 → 更新版本 → 重启）
./deploy.sh clean-logs  # 清理旧日志（默认保留 7 天）
```

---

## 端口

| 端口 | 服务 |
|------|------|
| 80 | Web 控制台 |
| 6751 | AgentCenter (gRPC) |

---

## 配置文件说明

### .env

所有配置集中在 `.env` 文件中，首次部署时交互式生成，后续可直接编辑：

```bash
# ============ 数据库 ============
MYSQL_ROOT_PASSWORD=xxx        # MySQL root 密码
MYSQL_PASSWORD=xxx             # 应用密码
MYSQL_DATABASE=mxsec           # 数据库名
MYSQL_USER=mxsec_user          # 应用用户名
MYSQL_HOST=mysql               # 数据库地址（Docker 内部用 mysql）
MYSQL_PORT=3306                # 数据库端口

# ============ 数据库连接池 ============
DB_MAX_IDLE_CONNS=20           # 最大空闲连接数
DB_MAX_OPEN_CONNS=200          # 最大打开连接数
DB_CONN_MAX_LIFETIME=1h        # 连接最大生命周期

# ============ 数据目录 ============
DATA_DIR=/data/mxsec           # 持久化数据根目录

# ============ 网络 ============
SERVER_IP=10.0.0.1             # 服务器 IP（用于生成插件下载 URL）
GRPC_PORT=6751                 # AgentCenter gRPC 端口
HTTP_PORT=80                   # Web 控制台端口
HTTPS_PORT=443                 # HTTPS 端口
MANAGER_PORT=8080              # Manager API 端口

# ============ 日志 ============
LOG_LEVEL=info                 # 日志级别: debug, info, warn, error
LOG_FORMAT=json                # 日志格式: json, console
LOG_MAX_AGE=7                  # 日志文件保留天数
LOG_RETENTION_DAYS=7           # 清理日志时的保留天数

# ============ Agent ============
HEARTBEAT_INTERVAL=60          # Agent 心跳间隔（秒）

# ============ 版本 ============
VERSION=v1.0.0
TZ=Asia/Shanghai
```

修改 `.env` 后运行 `./deploy.sh restart` 即可生效（会自动从模板重新生成 `server.yaml`）。

### server.yaml

Server 配置模板，所有 `__XXX__` 占位符由 `deploy.sh` 从 `.env` 替换生成。
不要直接编辑 `server.yaml`，改 `.env` 就行。

### mysql.cnf

MySQL 自定义配置，挂载到容器 `/etc/mysql/conf.d/custom.cnf`。
包含字符集、连接数、InnoDB 参数、慢查询日志等优化配置。
修改后执行 `./deploy.sh restart mysql` 生效。

### nginx.conf

Nginx 反向代理配置，修改后执行 `./deploy.sh restart ui` 生效。

---

## 升级流程

```bash
# 1. 上传新版本镜像（或推送到私有仓库）
# 2. 执行升级命令
./deploy.sh upgrade
# 脚本会自动: 备份数据库 → 更新版本号 → 重新生成配置 → 拉取新镜像 → 重启服务
```

---

## 日志管理

日志存储在 `$DATA_DIR/logs/` 下，按服务分目录：

```
$DATA_DIR/logs/
├── agentcenter/    # AgentCenter 日志（按天轮转）
├── manager/        # Manager API 日志（按天轮转）
├── nginx/          # Nginx 访问/错误日志
└── mysql/          # MySQL 慢查询日志
```

清理旧日志：
```bash
./deploy.sh clean-logs    # 清理超过保留天数的日志
```

建议添加 crontab 定期清理：
```bash
# 每天凌晨 3 点清理旧日志
0 3 * * * /opt/mxsec-platform/deploy.sh clean-logs >> /var/log/mxsec-clean.log 2>&1
```

---

## 部署 Agent

```bash
# 开发机构建 Agent
make package-agent-all VERSION=v1.0.0 SERVER_HOST=YOUR_SERVER_IP:6751

# 目标主机安装
rpm -ivh mxsec-agent-*.rpm
systemctl enable --now mxsec-agent
```
