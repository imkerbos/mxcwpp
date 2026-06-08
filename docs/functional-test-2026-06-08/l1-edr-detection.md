# L1 EDR 检测覆盖测试 (2026-06-08)

rocky9 (kernel 5.14, cgroup_skb eBPF) + centos7 (kernel 3.10, AF_PACKET fallback)

**触发样本: 34 / PASS: 21 / FAIL: 13 / 命中率: 61%**

| 样本 | 主机 | 结果 | 命中规则 |
|---|---|---|---|
| bash /dev/tcp 反弹 | 192.168.254.109 | PASS | cel-154,cel-392 |
| nc 反弹 shell | 192.168.254.109 | PASS | cel-154 |
| python pty 反弹 | 192.168.254.109 | PASS | cel-154,cel-156 |
| perl 反弹 shell | 192.168.254.109 | PASS | cel-156 |
| openssl 加密反弹 | 192.168.254.109 | PASS | cel-164 |
| cron 写入 | 192.168.254.109 | FAIL | — |
| bashrc 写入 | 192.168.254.109 | FAIL | — |
| authorized_keys 写 | 192.168.254.109 | PASS | cel-167,cel-172,cel-180 |
| systemd 服务 | 192.168.254.109 | FAIL | — |
| rc.local 写 | 192.168.254.109 | FAIL | — |
| ld.so.preload | 192.168.254.109 | PASS | cel-166,cel-175 |
| sudo 失败 5 次 | 192.168.254.109 | FAIL | — |
| su root 失败 | 192.168.254.109 | PASS | cel-210 |
| SUID 文件创建 | 192.168.254.109 | PASS | cel-158 |
| capability 添加 | 192.168.254.109 | FAIL | — |
| ssh 链式登录 | 192.168.254.109 | PASS | cel-167,cel-180 |
| wget+exec | 192.168.254.109 | PASS | cel-167 |
| curl+pipe-bash | 192.168.254.109 | FAIL | — |
| scp 反弹 | 192.168.254.109 | PASS | cel-167 |
| 用户枚举 | 192.168.254.109 | PASS | cel-159,cel-197,cel-199 |
| 网络枚举 | 192.168.254.109 | PASS | cel-197,cel-199 |
| kernel info | 192.168.254.109 | PASS | cel-196,cel-197,cel-199 |
| memfd_exec 模拟 | 192.168.254.109 | FAIL | — |
| fork bomb (受控) | 192.168.254.109 | FAIL | — |
| kthread 伪装 | 192.168.254.109 | FAIL | — |
| 隐藏端口模拟 | 192.168.254.109 | FAIL | — |
| lsmod 异常 module | 192.168.254.109 | FAIL | — |
| PHP webshell 写 | 192.168.254.109 | PASS | cel-224 |
| JSP webshell 写 | 192.168.254.109 | PASS | cel-224 |
| WebShell 大马 (wso) | 192.168.254.109 | PASS | cel-224 |
| DNS 隧道模拟 | 192.168.254.109 | PASS | cel-167,cel-197,cel-199,cel-246 |
| ICMP 大包 | 192.168.254.109 | FAIL | — |
| SSH 弱口令暴破 | 192.168.254.109 | PASS | cel-167,cel-180 |
| centos7 bash /dev/tcp | 192.168.254.114 | PASS | cel-154,cel-392 |
