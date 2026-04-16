# Agent 更新指南

本文档基于当前 `cmd/agent/main.go`、`internal/agent/updater/` 和组件管理接口整理。

## 当前支持的更新方式

- 服务端推送更新
- Agent CLI 主动检查并更新
- 本地离线包安装更新

## CLI 更新

```bash
sudo mxsec-agent --update
sudo mxsec-agent --update --server http://10.0.0.1:8080
sudo mxsec-agent --update --force
sudo mxsec-agent --update --file ./mxsec-agent_1.0.0_amd64.deb
```

说明：

- `--server` 指定 Manager HTTP 地址
- `--force` 即使版本相同也强制安装
- `--file` 走本地离线包路径，不请求服务端

## 服务端推送更新

服务端推送依赖两部分：

1. 组件中心中存在 `agent` 组件和可下载的版本包
2. AgentCenter 调度器向在线 Agent 下发 `AgentUpdate`

相关入口：

- `GET /api/v1/agent/update-check`
- 组件管理与上传接口
- `/components/agent/push-update`

## 更新流程

1. 检查目标版本、架构、包类型
2. 下载 RPM / DEB 包
3. 校验 SHA256
4. 调用 `rpm -Uvh --force` 或 `dpkg -i`
5. 延迟执行 `systemctl restart mxsec-agent`

## 约束

- 更新必须以 root 身份执行
- 当前只支持 `rpm` 和 `deb`
- 默认禁止无 `force` 的降级更新
- 架构不一致会被拒绝

## 建议操作

- 先在一台测试主机验证组件包和安装脚本
- 推送更新前确认组件中心中的 `sha256` 与包内容匹配
- 如果是内网环境，确保 Agent 可访问下载 URL
