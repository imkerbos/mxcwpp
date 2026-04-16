# 发行版支持

本文档补齐 README 旧链接，实际兼容性说明以 [OS 兼容性](../os-compatibility.md) 为准。

## 当前已覆盖的主流发行版

RPM 系：

- CentOS 7/8/9
- Rocky Linux 8/9
- RHEL 7/8/9
- openEuler 22.03

DEB 系：

- Ubuntu 18.04/20.04/22.04
- Debian 10/11/12

## 架构

- `amd64`
- `arm64`

## 打包形态

- RPM 系主机使用 `.rpm`
- Debian / Ubuntu 使用 `.deb`

## 注意事项

- Agent 安装包类型由目标主机包管理器决定。
- 规则适配不仅受发行版影响，也受 `os_version`、运行时类型和系统默认配置差异影响。
- 新发行版接入前，建议先用一套最小策略集验证规则命中率和修复命令兼容性。

## 相关文档

- [OS 兼容性](../os-compatibility.md)
- [Agent 部署](agent.md)
