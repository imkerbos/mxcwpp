# 快速开始

如果你只是想把项目先跑起来，按下面步骤执行即可。

## 方式一：Docker 开发环境

```bash
make dev-docker-up
```

默认会启动单节点 Kafka KRaft 开发环境。

启动后重点检查：

- `http://localhost:3000`
- `http://localhost:8080/health`

停止环境：

```bash
make dev-docker-down
```

## 方式二：本地启动核心服务

1. 准备 MySQL。
2. 复制并修改 `configs/server.yaml.example`。
3. 生成证书：`./scripts/generate-certs.sh`
4. 启动服务端：

```bash
go run ./cmd/server/agentcenter
go run ./cmd/server/manager
```

5. 启动前端：

```bash
cd ui
npm install
npm run dev
```

## 下一步

- [开发环境搭建](getting-started.md)
- [故障排查](troubleshooting.md)
