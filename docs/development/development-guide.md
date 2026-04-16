# 开发指南

本文档补齐 README 中的旧链接入口，面向当前仓库开发方式。

## 建议阅读顺序

1. [开发环境搭建](getting-started.md)
2. [快速开始](quick-start.md)
3. [插件开发指南](plugin-development.md)
4. [Go 代码风格](go-style-guide.md)
5. [前端风格指南](frontend-style-guide.md)
6. [传输架构说明](transport-architecture.md)

## 当前开发模式

- 后端：Go 多服务工程
- 前端：Vue 3 + TypeScript
- 协议：Protobuf + gRPC
- 本地一体开发：Docker Compose 或本地多进程启动

## 常用命令

```bash
make proto
make build-server
make build-consumer
make test
make fmt
make lint
make dev-docker-up
make dev-docker-down
```

## 开发建议

- 修改接口或传输协议时，优先检查 `api/proto/`
- 修改 Agent 与插件交互时，同时检查 `internal/agent/plugin/` 和 `plugins/lib/go/`
- 修改异步数据链路时，同时检查 `internal/server/agentcenter/`、`internal/server/consumer/` 和 `internal/server/common/kafka/`
- 文档命名优先使用小写加中划线，避免 README 再次出现大小写路径漂移
