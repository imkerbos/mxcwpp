# L1 采集器测试 (11 类资产, 2026-06-08)

| 类型 | API | 数据条数 | 结果 |
|---|---|---|---|
| 主机 | `/api/v1/hosts?page=1&page_size=1` | 3 | PASS |
| 进程 | `/api/v1/assets/processes?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 3844 | PASS |
| 端口 | `/api/v1/assets/ports?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 1103 | PASS |
| 用户 | `/api/v1/assets/users?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 35 | PASS |
| 软件 | `/api/v1/assets/software?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 1433 | PASS |
| 容器 | `/api/v1/assets/containers?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 0 | FAIL |
| cron | `/api/v1/assets/crons?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 3 | PASS |
| 服务 | `/api/v1/assets/services?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 142 | PASS |
| 挂载 | `/api/v1/assets/volumes?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 2 | PASS |
| 内核 | `/api/v1/assets/kmods?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 86 | PASS |
| 网卡 | `/api/v1/assets/network-interfaces?host_id=7f780116d31f110feb783600eb2fa7029743f3f6ed066e078557e87434870654&page=1&page_size=1` | 1 | PASS |

**采集器汇总: PASS=10 / FAIL=1 (总 11)**
