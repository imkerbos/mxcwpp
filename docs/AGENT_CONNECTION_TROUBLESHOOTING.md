# Agent 连接故障排查

本文档基于当前 `internal/agent/connection/connection.go` 与 `transport` 实现整理。

## 连接链路

```text
Agent -> discoverServer -> loadTLSConfig -> gRPC Dial -> Transfer stream
```

## 先检查 4 件事

1. 构建时嵌入的 `SERVER_HOST` 是否正确
2. `6751` 端口是否可达
3. `manager` / `agentcenter` 是否正常运行
4. 证书目录是否存在于 `/var/lib/mxsec-agent/certs/`

## 首次连接失败

首次连接时，Agent 允许在证书不存在的情况下跳过校验以拉取证书。

如果首次连接仍失败，重点检查：

- `SERVER_HOST` 是否写成了无法访问的内网地址
- 目标主机到 AgentCenter 的网络是否畅通
- 防火墙 / 安全组是否放通 `6751/tcp`

## mTLS 重连失败

当前实现里，如果 mTLS 连续失败 3 次，Agent 会临时降级为不安全模式重新连接，等待服务端重新下发证书。

如果依旧失败，检查：

- `ca.crt`
- `client.crt`
- `client.key`
- 服务端证书是否已更换但 AgentCenter 未正确下发新证书

## 常用检查命令

```bash
systemctl status mxsec-agent
journalctl -u mxsec-agent -f
ls -la /var/lib/mxsec-agent/certs
nc -zv <agentcenter-host> 6751
```

## 常见原因

- 构建时嵌入了错误的地址
- 证书文件丢失或权限异常
- 服务端证书更新后客户端证书未同步
- DNS 解析错误
- 反向代理或负载均衡未正确透传 gRPC

## 服务端排查

- 查看 `agentcenter` 日志
- 检查 `deploy/certs/` 或容器内证书挂载
- 确认 `server.yaml` 中 `mtls` 路径正确

## 相关文档

- [故障排查](troubleshooting.md)
