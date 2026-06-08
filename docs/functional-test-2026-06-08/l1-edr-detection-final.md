# L1 EDR 检测覆盖 final (2026-06-08)

PR #258 (4 CEL 规则) + PR #260 (comm 字段 + kthread cmdline 兜底) 后最终重跑.

**触发样本: 30 / PASS: 28 / FAIL: 2 / 命中率: 93%**

| 样本 | 主机 | 结果 | 命中规则 |
|---|---|---|---|
| bash /dev/tcp 反弹 | 192.168.254.109 | PASS | cel-154,cel-392 |
| nc 反弹 shell | 192.168.254.109 | PASS | cel-154 |
| perl 反弹 | 192.168.254.109 | PASS | cel-156 |
| openssl 加密反弹 | 192.168.254.109 | PASS | cel-164 |
| cron 写 (sudo) | 192.168.254.109 | PASS | cel-231 |
| systemd 服务 (sudo) | 192.168.254.109 | PASS | cel-173 |
| rc.local (sudo) | 192.168.254.109 | PASS | cel-173 |
| bashrc 写 ×10 (tee) | 192.168.254.109 | PASS | cel-404 |
| authorized_keys 写 | 192.168.254.109 | PASS | cel-167,cel-172,cel-180 |
| ld.so.preload (sudo) | 192.168.254.109 | PASS | cel-166,cel-175 |
| sudo 失败 5 次 | 192.168.254.109 | FAIL | — |
| su root 失败 | 192.168.254.109 | PASS | cel-210 |
| SUID 创建 + exec (sudo) | 192.168.254.109 | PASS | cel-157,cel-158 |
| setcap (sudo) | 192.168.254.109 | PASS | cel-157,cel-405 |
| ssh 链式登录 | 192.168.254.109 | PASS | cel-167,cel-180 |
| wget+exec | 192.168.254.109 | PASS | cel-167 |
| curl pipe bash | 192.168.254.109 | PASS | cel-167 |
| scp 反弹 | 192.168.254.109 | PASS | cel-167 |
| 用户枚举 | 192.168.254.109 | PASS | cel-159,cel-196,cel-197,cel-199 |
| 网络枚举 | 192.168.254.109 | PASS | cel-197,cel-199,cel-374 |
| kernel info | 192.168.254.109 | PASS | cel-196,cel-197,cel-199 |
| fork bomb 100 进程 | 192.168.254.109 | FAIL | — |
| kthread 伪装 (exec -a) | 192.168.254.109 | PASS | cel-271 |
| PHP webshell | 192.168.254.109 | PASS | cel-224 |
| JSP webshell | 192.168.254.109 | PASS | cel-224 |
| WSO 大马 | 192.168.254.109 | PASS | cel-224 |
| DNS 隧道 30 query | 192.168.254.109 | PASS | cel-167,cel-196,cel-197,cel-199,cel-246 |
| SSH 弱口令暴破 ×8 | 192.168.254.109 | PASS | cel-167,cel-180 |
| memfd_create fileless | 192.168.254.109 | PASS | cel-406 |
| centos7 bash /dev/tcp | 192.168.254.114 | PASS | cel-154,cel-392 |
