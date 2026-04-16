# Agent 部署指南

本文档对应 README 中的旧入口，当前 Agent 部署方式以 [Agent 部署](agent.md) 为主。

## 当前 Agent 特点

- 构建时嵌入 `serverHost`
- 首次连接允许跳过证书校验以换取证书下发
- 后续连接切换为正式 mTLS
- 支持服务端推送更新和 CLI 主动更新

## 构建

```bash
make package-agent-all VERSION=1.0.0 SERVER_HOST=10.0.0.1:6751
make package-plugins-all VERSION=1.0.0
```

## 安装

RPM:

```bash
sudo rpm -ivh mxsec-agent-1.0.0.x86_64.rpm
```

DEB:

```bash
sudo dpkg -i mxsec-agent_1.0.0_amd64.deb
```

## 运行目录

- 工作目录：`/var/lib/mxsec-agent`
- 证书目录：`/var/lib/mxsec-agent/certs`
- 插件目录：`/var/lib/mxsec-agent/plugins` 或插件运行目录下的子目录
- 日志：`/var/log/mxsec-agent/agent.log`

## 常用操作

```bash
systemctl status mxsec-agent
systemctl restart mxsec-agent
journalctl -u mxsec-agent -f
```

## 更新方式

- 服务端推送更新
- `mxsec-agent --update`
- `mxsec-agent --update --server http://server:8080`
- `mxsec-agent --update --file ./mxsec-agent.rpm`

## 相关文档

- [Agent 部署](agent.md)
- [Agent 更新指南](../AGENT_UPDATE.md)
- [Agent 连接故障排查](../AGENT_CONNECTION_TROUBLESHOOTING.md)
