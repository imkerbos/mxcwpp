# L1 EDR 检测覆盖 v3 (PR + 4 新规则后, 2026-06-08)

v3 改进: 测试 keyword 涵盖中文 title (启动项/权限提升/防御绕过/sudo 异常) + 4 新规则 (cel-404 bashrc, cel-405 setcap, cel-406 memfd, cel-407 fork bomb).

| 样本 | 主机 | 结果 | 命中规则 |
|---|---|---|---|
| systemd 服务写入 | rocky9 | PASS | cel-173 |
| rc.local 写 | rocky9 | PASS | cel-173 |
| bashrc 写入 ×10 (tee) | rocky9 | PASS | cel-404 |
| sudo 失败 5 次 | rocky9 | FAIL | — |
| capability setcap | rocky9 | PASS | cel-157,cel-405 |
| curl pipe bash | rocky9 | PASS | cel-167 |
| fork bomb 100 进程 | rocky9 | FAIL | — |
| kthread 伪装 | rocky9 | FAIL | — |
| memfd_create syscall | rocky9 | PASS | cel-406 |

**v3 汇总: PASS=6 / FAIL=3 (总 9)**
