# L1 EDR 检测覆盖 v2 (强化样本, 2026-06-08)

强化点 vs v1: sudo 加 -tt 跑 cron/systemd/SUID/setcap/ld.so.preload; bashrc 10 次 + authorized_keys 3 条; fork bomb 100 进程; sudo 失败 5 次; SSH 暴破 8 个口令; DNS 30 query; memfd_create ctypes 真 syscall.

**触发样本: 30 / PASS: 21 / FAIL: 9 / 命中率: 70%**

| 样本 | 主机 | 结果 | 命中规则 |
|---|---|---|---|
| bash /dev/tcp 反弹 | 192.168.254.109 | PASS | cel-154,cel-392 |
| nc 反弹 shell | 192.168.254.109 | PASS | cel-154 |
| perl 反弹 | 192.168.254.109 | PASS | cel-156 |
| openssl 加密反弹 | 192.168.254.109 | PASS | cel-164 |
| cron 写入 (sudo) | 192.168.254.109 | PASS | cel-231 |
| systemd 服务写入 (sudo) | 192.168.254.109 | FAIL | — |
| rc.local 写 (sudo) | 192.168.254.109 | FAIL | — |
| bashrc 写入 ×10 | 192.168.254.109 | FAIL | — |
| authorized_keys 写 | 192.168.254.109 | PASS | cel-167,cel-172,cel-180 |
| ld.so.preload (sudo) | 192.168.254.109 | PASS | cel-166,cel-175 |
| sudo 失败 5 次 | 192.168.254.109 | FAIL | — |
| su root 失败 | 192.168.254.109 | PASS | cel-210 |
| SUID 创建 (sudo) | 192.168.254.109 | PASS | cel-158 |
| capability setcap (sudo) | 192.168.254.109 | FAIL | — |
| ssh 链式登录 | 192.168.254.109 | PASS | cel-167,cel-180 |
| wget+exec | 192.168.254.109 | PASS | cel-167 |
| curl pipe bash | 192.168.254.109 | FAIL | — |
| scp 反弹 | 192.168.254.109 | PASS | cel-167 |
| 用户枚举 | 192.168.254.109 | PASS | cel-159,cel-197,cel-199 |
| 网络枚举 | 192.168.254.109 | PASS | cel-197,cel-199 |
| kernel info | 192.168.254.109 | PASS | cel-196,cel-197,cel-199 |
| fork bomb 100 进程 | 192.168.254.109 | FAIL | — |
| kthread 伪装 | 192.168.254.109 | FAIL | — |
| PHP webshell | 192.168.254.109 | PASS | cel-224 |
| JSP webshell | 192.168.254.109 | PASS | cel-224 |
| WSO 大马 | 192.168.254.109 | PASS | cel-224 |
| DNS 隧道 30 query | 192.168.254.109 | PASS | cel-167,cel-197,cel-199,cel-246 |
| SSH 弱口令暴破 ×8 | 192.168.254.109 | PASS | cel-167,cel-180 |
| centos7 bash /dev/tcp | 192.168.254.114 | PASS | cel-154,cel-392 |
| memfd_create syscall | 192.168.254.109 | FAIL | — |
