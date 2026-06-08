# L2 响应能力测试 (2026-06-08)

| 场景 | 触发 | 验证 | 结果 |
|---|---|---|---|
| ClamAV 隔离 | EICAR 文件 + scan 任务 actions=quarantine | quarantine 列表新增 (1 总) | PASS |
| 主机隔离 | POST /hosts/isolate {host_id,level,reason,timeout} | code=0, isolated=true, record id=6 | PASS |
| NPatch 阻断 | GET /npatch/mode | mode= | PASS |
| 告警处置 | PUT /alerts/:id/resolve id=44779 | code=0 | PASS |
| Agent 重启 | POST /hosts/restart-agent | code=0 (下发成功) | PASS |

**汇总: PASS=5 / FAIL=0 / SKIP=0 (总 5)**
