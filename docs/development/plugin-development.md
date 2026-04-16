# 插件开发指南

本文档是 README 旧路径的兼容入口，当前正式内容见 [plugin-guide.md](plugin-guide.md)。

## 插件开发最小路径

1. 了解插件与 Agent 的 Pipe + Protobuf 通信方式。
2. 参考 `plugins/lib/go/client.go` 使用 SDK。
3. 在插件主循环中实现 `ReceiveTask -> Execute -> SendRecord`。
4. 在 Agent 侧确认插件配置、下载和升级逻辑可用。

## 当前仓库内置插件

- `baseline`
- `collector`
- `fim`

## 关键目录

- `plugins/lib/go/`
- `plugins/baseline/`
- `plugins/collector/`
- `plugins/fim/`

## 深入阅读

- [插件开发指南正式版](plugin-guide.md)
- [传输架构说明](transport-architecture.md)
