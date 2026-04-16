# 基线修复功能说明

本文档基于当前修复链路实现整理，不再依赖已删除的旧工具目录。

## 当前能力

项目已经具备基线自动修复能力，核心路径如下：

1. 前端从 `/api/v1/fix/fixable-items` 查询可修复项
2. 通过 `/api/v1/fix-tasks` 创建修复任务
3. AgentCenter 将任务下发给 Baseline 插件，数据类型为 `8002`
4. Baseline 插件执行修复并实时回传 `8003`
5. 修复任务完成后发送 `8004`
6. 服务端更新 `fix_tasks`、`fix_task_host_status` 等状态

## 关键实现位置

- API：`internal/server/manager/api/fix.go`
- 路由：`internal/server/manager/router/router.go`
- 下发：`internal/server/agentcenter/service/task.go`
- 接收：`internal/server/agentcenter/transfer/service.go`
- 消费写入：`internal/server/consumer/writer/mysql.go`
- 插件执行：`plugins/baseline/main.go`
- 修复引擎：`plugins/baseline/engine/fixer.go`

## 数据类型

- `8002`：基线修复任务
- `8003`：单条修复结果
- `8004`：修复任务完成信号

## 修复前提

- 规则必须包含 `fix_config.command`
- Agent 侧 Baseline 插件运行正常
- 目标主机具备执行修复命令和重启服务的权限

## 当前行为特点

- 可按失败结果精确创建修复任务
- 支持按筛选条件批量生成修复任务
- 批量修复时会合并服务重启，避免重复 reload / restart
- 无修复命令的规则会被标记为不可自动修复

## 风险与限制

- 修复命令是直接在目标主机执行的 shell 命令，规则质量直接决定安全性
- 规则中重启服务会影响业务进程，生产环境建议先在测试主机验证
- 部分失败项只有人工修复建议，没有自动修复命令
