# 开发故障排查

本文档补齐 README 中的旧入口，开发和部署通用问题请优先参考上级文档。

## 优先检查

- Go 版本是否满足要求
- `protoc` 及 `protoc-gen-go` 是否可用
- MySQL / Redis / Docker Compose 是否已启动
- 前端依赖是否安装完整

## 常见问题

### Protobuf 代码未更新

```bash
make proto
```

### Docker 开发环境服务不全

```bash
make dev-docker-logs
cd deploy && docker compose -f docker-compose.dev.yml ps
```

- 旧版 `deploy/data/kafka` 或 `deploy/data/zookeeper` 是 Zookeeper 模式遗留目录；当前 dev/pret 已切换到 KRaft，不再使用它们。

### 前端接口 401 / 404

- 检查 `manager` 是否启动
- 检查前端代理配置
- 检查后端是否变更了 `/api/v1/*` 路径

### 本地启动后 Agent 无法接入

- 检查 `6751` 端口监听
- 检查证书目录是否生成
- 检查构建时嵌入的 `SERVER_HOST`

## 相关文档

- [项目故障排查](../troubleshooting.md)
- [Agent 连接故障排查](../AGENT_CONNECTION_TROUBLESHOOTING.md)
