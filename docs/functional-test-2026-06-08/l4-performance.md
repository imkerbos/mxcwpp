# L4 性能 SLO 测试 (2026-06-08)

环境: dev (mac M-series + docker), 2 主机数据, MySQL 8 + ClickHouse 24 + Redis 7.
SLO 目标: 500 主机 ms 级 / 1k 主机 < 3s / 3k 主机 < 5s / 1w 主机 < 10s.
本次仅 2 主机, **验证算法链路, 非真实负载**.

## 测试 1: 关键 list API 50 次延迟分布

| 端点 | min(ms) | avg(ms) | p50(ms) | p90(ms) | p99(ms) | max(ms) |
|---|---|---|---|---|---|---|
| /hosts (list 50) | 1.27 | 1.73 | 1.61 | 2.39 | 2.74 | 4.73 |
| /dashboard/stats | 0.79 | 7.77 | 1.06 | 2.83 | 3.03 | 319.94 |
| /alerts (list 80 含 actual) | 4.55 | 5.71 | 5.52 | 6.62 | 7.51 | 10.58 |
| /vulnerabilities (50) | 3.96 | 4.84 | 4.79 | 5.77 | 6.37 | 7.12 |
| /edr/events (lite 50) | 8.96 | 11.80 | 10.17 | 16.69 | 20.04 | 20.05 |
| /assets/processes (50) | 1.62 | 2.93 | 2.50 | 4.47 | 5.04 | 6.74 |
| /memory-threats (50) | 2.47 | 3.19 | 3.13 | 4.21 | 4.97 | 5.37 |
| /storylines (50) | 1.66 | 2.69 | 2.33 | 4.19 | 5.62 | 6.11 |

## 测试 2: 50 并发请求 (/hosts) 总耗时

- 50 并发请求 /hosts: 62 ms
- 平均 QPS: 801.4

## 测试 3: Agent 心跳新鲜度

```
centos7: 2026-06-08 14:55:17 (status=online)
rocky9: 2026-06-08 14:55:14 (status=online)
c325b3109eaf: 2026-06-08 14:55:14 (status=online)
```

心跳间隔: agent 默认 30s, 上面 last_heartbeat 是最近一次时间.

## 测试 4: 事件归档量 (ClickHouse)

- EDR 事件历史总量: **36485** 条
- 说明 dev 环境 Agent→AC→Kafka→Consumer→ClickHouse 链路通畅
