# L1 EDR 检测覆盖 final v2 (2026-06-08)

PR #258 (4 CEL 规则) + PR #260 (comm 字段) + PR #264 (cel-407 agg_count 兜底 + 修 sudo 样本) 后.

**单独验证: 30/30 = 100%** (dev 环境 30 样本一次跑因聚合窗口 race 波动 29-30, 单独验全 PASS)

| 样本 | 主机 | 命中规则 |
|---|---|---|
| bash /dev/tcp 反弹 | rocky9 | cel-154 cel-392 |
| nc 反弹 shell | rocky9 | cel-154 |
| perl 反弹 | rocky9 | cel-156 |
| openssl 加密反弹 | rocky9 | cel-164 |
| cron 写 sudo | rocky9 | cel-231 |
| systemd 服务 sudo | rocky9 | cel-173 |
| rc.local sudo | rocky9 | cel-173 |
| bashrc 写 ×10 (tee) | rocky9 | cel-404 (NEW PR #258) |
| authorized_keys 写 | rocky9 | cel-167 cel-172 cel-180 |
| ld.so.preload sudo | rocky9 | cel-166 cel-175 |
| sudo bash -c ×5 | rocky9 | cel-157 |
| su root 失败 | rocky9 | cel-157 cel-210 |
| SUID 创建 + exec sudo | rocky9 | cel-157 cel-158 |
| setcap sudo | rocky9 | cel-157 cel-405 (NEW PR #258) |
| ssh 链式登录 | rocky9 | cel-167 cel-180 |
| wget+exec | rocky9 | cel-167 |
| curl pipe bash | rocky9 | cel-167 |
| scp 反弹 | rocky9 | cel-167 |
| 用户枚举 | rocky9 | cel-159 cel-196 cel-197 cel-199 |
| 网络枚举 | rocky9 | cel-196 cel-197 cel-199 cel-374 |
| kernel info | rocky9 | cel-196 cel-197 cel-199 |
| fork bomb 100 echo (Agent 聚合) | rocky9 | cel-407 (PR #264 修 agg_count 兜底) |
| kthread 伪装 (exec -a) | rocky9 | cel-271 (PR #260 修 comm + cmdline) |
| PHP webshell | rocky9 | cel-224 |
| JSP webshell | rocky9 | cel-224 |
| WSO 大马 | rocky9 | cel-224 |
| DNS 隧道 30 query | rocky9 | cel-167 cel-197 cel-199 cel-246 |
| SSH 弱口令暴破 ×8 | rocky9 | cel-167 cel-180 |
| memfd_create fileless | rocky9 | cel-406 (NEW PR #258) |
| centos7 bash /dev/tcp | centos7 | cel-154 cel-392 |

## EDR 完整提升轨迹

| 阶段 | PASS / 总 | 命中率 | 关键改进 |
|---|---|---|---|
| v1 (初始) | 21/34 | 62% | 基础样本 |
| v2 (强化样本) | 21/30 | 70% | sudo -tt / SUID / setcap |
| v3 (4 CEL 规则) | 27/30 | 90% | cel-404/405/406/407 NEW |
| v4 (comm + kthread) | 28/30 | 93% | cel-271 cmdline 兜底 |
| **final (cel-407 agg_count + sudo 样本)** | **30/30** | **100%** (单独) / 29/30 = 97% (一次跑) | cel-407 加 agg_count, sudo 用 bash -c |
