# Agent 架构设计

本文档基于当前 `cmd/agent/main.go` 和 `internal/agent/*` 实现整理。

## 模块组成

- `config`：本地默认配置与服务端远程配置合并
- `connection`：服务发现、TLS 装载、gRPC 连接建立
- `transport`：与 `AgentCenter` 的双向流通信
- `heartbeat`：周期性上报主机信息和状态
- `plugin`：插件下载、启动、升级、健康检查
- `updater`：Agent 自更新

## 启动顺序

1. 校验构建时嵌入的 `serverHost`
2. 初始化日志
3. 初始化 Agent ID
4. 创建连接管理器
5. 创建传输管理器
6. 创建插件管理器
7. 注册配置和证书更新回调
8. 并发启动 `heartbeat`、`transport`、`plugin`、`updater`

## 连接模型

- 首次接入：如果本地证书不存在，允许 `InsecureSkipVerify`
- 建立连接后：服务端回传证书包
- 后续重连：使用正式 mTLS
- 若 mTLS 连续失败 3 次：暂时降级为不安全模式以重新取证

## 插件模型

- 每个插件是独立子进程
- Agent 使用 `os.Pipe` 与插件通信
- 插件配置来自服务端下发
- 当版本或 `sha256` 变化时，Agent 会执行热更新或重载

## 数据流

```text
Plugin -> Agent ring buffer -> gRPC stream -> AgentCenter
AgentCenter command -> transport -> plugin / updater / config callback
```

## 可靠性设计

- 传输层使用环形缓冲
- 支持缓存重试
- 插件有 watchdog 健康检查
- 更新模块避免并发更新

## 相关代码

- `cmd/agent/main.go`
- `internal/agent/connection/connection.go`
- `internal/agent/transport/transport.go`
- `internal/agent/plugin/plugin.go`
- `internal/agent/updater/`
