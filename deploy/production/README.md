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
│   ├── server.yaml     # Server 配置
│   └── nginx.conf      # Nginx 配置
└── certs/              # 证书目录（部署时生成）
```

---

## 管理命令

```bash
./deploy.sh start     # 启动
./deploy.sh stop      # 停止
./deploy.sh restart   # 重启
./deploy.sh status    # 状态
./deploy.sh logs      # 日志
./deploy.sh backup    # 备份
```

---

## 端口

| 端口 | 服务 |
|------|------|
| 80 | Web 控制台 |
| 6751 | AgentCenter (gRPC) |

---

## 部署 Agent

```bash
# 开发机构建 Agent
make package-agent-all VERSION=v1.0.0 SERVER_HOST=YOUR_SERVER_IP:6751

# 目标主机安装
rpm -ivh mxsec-agent-*.rpm
systemctl enable --now mxsec-agent
```
