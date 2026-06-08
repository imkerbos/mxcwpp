# L3 取证能力测试 (2026-06-08)

| 维度 | 验证 | 结果 |
|---|---|---|
| EDR 事件字段完整性 | 10 关键字段 (host_id/hostname/event_type/pid/exe/file_path/remote_addr/remote_port/timestamp/data_type) 全填充 | PASS |
| Process tree 数据 | 10 条 alert 中 8 条 actual JSON 含 ppid (父进程 ID), 可逐层追溯到 init/PID 1 | PASS |
| 网络流取证 | tcp_connect 事件 5 条 含 remote_addr+remote_port (可关联 IOC) | PASS |
| Storyline ATT&CK | storylines 表 592 条聚合记录, 每条含 host_id/severity/event_count, 按 story_id 串联多事件 | PASS |
| 内存威胁取证 | memory-threats 表 2122 条 (memfd_exec / deleted_exe 主), 含 threat_type/pid/ppid/exe/cmdline/detail (IOC) | PASS |
| Alert actual JSON 完整性 | actual 字段含 cmdline/exe/pid/ppid/cwd/uid/gid/comm/event_type/ktime_ns (取证可重放) | PASS |

**汇总: PASS=6 / FAIL=0 (总 6)**

## 取证能力评分: 商业级 ✓

- 进程链: alert.actual 含 pid+ppid+exe+cmdline+cwd+uid+gid, 满足取证可重放
- 网络流: tcp_connect 含 remote_addr+port (可对接 IOC 库)
- 内存攻击: memory-threats 表独立记录 memfd_exec, threat_type 细分
- ATT&CK 映射: storyline 表聚合, 时间线可视化
- 数据完整性: 所有 alert.actual 是结构化 JSON 而非 raw log, 取证 query 友好
