# Engine Schedulers（未接线的占位实现）

> **当前状态（2026-08-01 核实）**：本包**没有任何调用方**。
> 真正在跑的调度器全部位于 `internal/server/agentcenter/scheduler/`，
> 由 `internal/server/agentcenter/init/init.go` 启动。
>
> 本包提供两个基于 Kafka 的实现（`ioc_sync.go` / `rule_sync.go`）与
> `EngineCommander` interface —— 后者被 `agentcenter/commandsub/consumer.go` 实现，
> 但生产端从未被构造启动。

## 原计划与实际

v2.0 微服务拆分原本要把 IOC 同步与规则同步从 AgentCenter 迁到 Engine，
经 Kafka 解耦，避免 AC 既做接入又做检测产物分发。

理由仍然成立：AC 侧的 scheduler 直接持有 `transfer.Service`（Agent 连接池），
经 `stream.Send` 推命令；把这套直接搬进 Engine 会让 Engine 反向 import AC 的
`transfer` 包，或者被迫也维护一份连接池。

但**迁移没有完成**。当前形态是：接口、Kafka 生产端、消费端都写好了，
没有任何地方把它们接起来。

## 现状对照

| 能力 | 实际运行位置 | 本包的对应实现 |
|------|--------------|----------------|
| IOC 同步 | `agentcenter/scheduler/ioc_sync_scheduler.go` | `ioc_sync.go`（未接线）|
| 规则同步 | `agentcenter/scheduler/rule_sync_scheduler.go` | `rule_sync.go`（未接线）|
| 告警通知 | `agentcenter/scheduler/` 下若干 | 无 |

## 要么接上，要么删掉

未接线的代码有两重代价：读代码的人会以为它在工作；
而它一旦真被接上，行为是否与经过生产验证的 AC 实现一致，谁也说不准。

推进方向二选一：

1. **接上** —— 在 Engine 启动路径构造这两个 scheduler，AC 侧对应实现下线，
   并跑通「IOC / 规则真正下发到 agent」的端到端验证；
2. **删掉** —— 连同 `commandsub/consumer.go` 里对应的 `EngineCommander` 实现一并移除。

在做出选择之前，**不要按本包的实现去理解线上行为** —— 线上跑的是 AC 那套。
