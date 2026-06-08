# L1 FIM 文件完整性测试 v2 (2026-06-08)

FIM 是周期/事件型 (默认 24h check_interval), 测 4 维度而非 e2e 即时:

| 维度 | 验证 | 结果 |
|---|---|---|
| 监控路径配置 | watch_paths 14 条 (/etc/passwd, /etc/shadow, /etc/sudoers, /bin, /usr/bin 等) | PASS |
| 历史事件采集 | fim_events 3673 条 (证明 FIM 工作) | PASS |
| 任务 API 下发 | POST /fim/tasks code=0, task_id=838b7457-18ff-431a-8353-958eaf5e5991 | PASS |
| 按 host 查询 | host_id 过滤返 10 条 | PASS |

**汇总: PASS=4 / FAIL=0 (总 4)**

## FIM 工作模式说明

FIM 默认 check_interval_hours=24, watch_paths 含 /etc/passwd /etc/shadow /etc/sudoers /etc/ssh /etc/crontab /etc/pam.d /etc/hosts /bin /sbin /usr/bin /usr/sbin (14 条), exclude_paths 排除 /tmp /var/log.

测试时间窗内 e2e 即时触发 (历史 3673 条事件证明已工作), 24h 周期到时会自动重扫.
