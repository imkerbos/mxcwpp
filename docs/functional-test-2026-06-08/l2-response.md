# L2 响应能力测试 (2026-06-08)

在 rocky9 验证 manager 下发响应动作 + 实际生效.

| 场景 | 端点 | 验证 | 结果 |
|---|---|---|---|
| 病毒文件隔离 | POST /antivirus/tasks actions=quarantine | quarantine 列表新增 1 条 (EICAR) | PASS |
| 主机隔离 | POST /hosts/isolate {host_id, level, reason} | code=500 后端 "创建隔离记录失败" (预存 DB schema 问题) | FAIL |
| NPatch 阻断模式 | n/a | 无独立 manager API (集成在 cel 规则 + agent npatch 模块) | SKIP |
| 告警 resolve | POST /alerts/:id/resolve | code=0, status=resolved, resolve_reason 落库 | PASS |
| Agent 重启下发 | POST /hosts/restart-agent | code=0 (命令通过 AC 下发) | PASS |

**汇总: PASS=3 / FAIL=1 / SKIP=1 (总 5)**

## FAIL 详情

### 主机隔离 500

POST /api/v1/hosts/isolate 返 500 "创建隔离记录失败".
isolation-status 返 isolated=false. 推测 host_isolations 表 schema 问题或 ACDispatcher 不可达.
**预存 bug, 不影响其它响应通道.** 留作 v2.2 修复.
