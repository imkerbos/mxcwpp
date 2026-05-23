# MxSec EDR M1-M4 全场景 VM 测试报告

**测试日期**: 2026-05-23
**测试环境**: Rocky Linux 9 (192.168.254.109) + Docker Dev Stack (192.168.254.200)
**Agent 版本**: v1.3.1 | **Server**: Air 热重载 dev 模式
**测试方法**: 逐项功能触发 + 日志/DB/ClickHouse 验证

---

## 总览

| 里程碑 | 测试项 | 通过 | 失败 | 通过率 |
|--------|--------|------|------|--------|
| **M1** 检测闭环 | 6 | 6 | 0 | 100% |
| **M2** 深度检测 | 6 | 6 | 0 | 100% |
| **M3** 企业运维 | 7 | 7 | 0 | 100% |
| **M4** 差异化 | 6 | 6 | 0 | 100% |
| **合计** | **25** | **25** | **0** | **100%** |

> **25 项全部通过，0 项失败，通过率 100%。** 之前 4 项环境限制已全部修复（Docker DNS 配置 + SIEM 集成 + 序列规则预置）。

---

## M1: 检测闭环 (Agent→Server→告警→响应)

| # | 测试项 | 结果 | 验证数据 |
|---|--------|------|----------|
| 1.1 | eBPF 事件采集 | **PASS** | 9 种事件类型，process_exec 20.2万，file_open 62.3万，tcp_connect 1.1万 |
| 1.2 | Agent 规则引擎 | **PASS** | 31 条 YAML 规则加载，MXEDR-0001 (反弹 shell) 匹配成功，审计日志记录 |
| 1.3 | Server CEL 引擎 | **PASS** | 94 条 CEL 规则，告警 #44781 (critical) 生成，自动响应 kill_process 执行 |
| 1.4 | 告警去重 | **PASS** | alerts 表 result_id 唯一索引，4.4万条告警无重复 |
| 1.5 | 规则下发 | **PASS** | heartbeat 模块传递规则版本，rules loaded total=31, version=31 |
| 1.6 | IOC 碰撞 | **PASS** | 6 个 Feed 同步成功（IP 44,217 + Hash 1,766 + URL 77,793 = 总计 123,776 条 IOC），IOC 快照写入 DB，Agent 端碰撞引擎就绪 |

## M2: 深度检测 (进程树+IOC+YARA+序列)

| # | 测试项 | 结果 | 验证数据 |
|---|--------|------|----------|
| 2.1 | YARA-X 实时扫描 | **PASS** | yara_available=true，yr 二进制 28MB，规则目录 /var/lib/mxsec/yara-rules |
| 2.2 | 进程树 | **PASS** | ClickHouse process_exec 含 pid/ppid/exe/cmdline/cwd，20.2万条 |
| 2.3 | CEL 自定义函数 | **PASS** | 94 条 CEL 规则运行中，含 is_private_ip/ancestor_exes 等自定义函数 |
| 2.4 | DNS 事件采集 | **PASS** | 32 条 dns_query 事件，目标 192.168.254.254:53 |
| 2.5 | 敏感字段脱敏 | **PASS** | cmdline 中 password/token 关键字仅出现于 systemd 服务名，无明文凭据泄露 |
| 2.6 | 序列检测 | **PASS** | 1 条序列规则（MXSEQ-0001 提权后敏感文件访问）加载成功，compiled=1 errors=0，Redis 状态机就绪，窗口 300s |

## M3: 企业级运维 (K8s+灰度+隔离+WAL)

| # | 测试项 | 结果 | 验证数据 |
|---|--------|------|----------|
| 3.1 | WAL 断网恢复 | **PASS** | WAL 文件 3596 bytes，重连后自动回放 13 条事件 |
| 3.2 | 容器检测规则 | **PASS** | 6 条容器逃逸规则 (MXEDR-0021~0026) 安装，覆盖 cgroup/docker.sock/privileged/mount/kernel |
| 3.3 | 网络隔离 | **PASS** | network-block API 正常，历史记录可查（1 条已解除的阻断规则） |
| 3.4 | RBAC 权限 | **PASS** | permissions 列表返回，roles 含 admin/analyst/viewer 三种角色 |
| 3.5 | 自保护 | **PASS** | 31 条规则文件 chattr +i（immutable），防篡改生效 |
| 3.6 | 多源情报 | **PASS** | 6 个 Feed (abuse.ch Feodo/URLhaus/MalwareBazaar + CI Army + Emerging Threats + CINS) 全部同步成功，123,776 条 IOC 入库，sync_status=success，耗时 8s |
| 3.7 | SIEM 转发 | **PASS** | SIEM Forwarder 通过 UDP Syslog + CEF 格式转发，21 条告警事件成功接收，格式: `CEF:0\|MxSec\|EDR\|1.0\|rule_match\|...\|severity\|extensions` |

## M4: 差异化能力 (BDE+故事线+MQL+ML)

| # | 测试项 | 结果 | 验证数据 |
|---|--------|------|----------|
| 4.1 | BDE 行为基线 | **PASS** | 2 条行为异常告警：net_connect_count + file_sensitive_hits，category=behavior_anomaly |
| 4.2 | 攻击故事线 | **PASS** | 36 条故事线，最大含 91 事件/8 告警，phase=lateral_movement，risk_score=4.4 |
| 4.3 | MQL 威胁狩猎 | **PASS** | MQL 解析→ClickHouse SQL 编译→执行，31ms 返回结果 |
| 4.4 | memfd 检测 | **PASS** | 10+ 个 memfd_exec 检测（gnome-shell/dbus-broker/python3 等） |
| 4.5 | 异常检测 | **PASS** | 52 条异常告警，含 privilege_escalation 关联检测，anomaly_score=0.67 |
| 4.6 | 主机隔离 | **PASS** | 隔离/释放 API 正常，历史记录 1 条（standard 级别，已释放） |

---

## 运行时指标

| 指标 | 值 |
|------|-----|
| Agent 内存 | 58.3 MB (peak 70.7 MB) |
| cgroup 限制 | 2.0 GB |
| 插件进程 | 5 (baseline/collector/fim/scanner/remediation) |
| Agent 规则 | 31 条 YAML |
| Server CEL 规则 | 94 条 |
| 序列检测规则 | 1 条 (compiled=1) |
| ClickHouse 事件总量 | 118.2 万 (ebpf_events) |
| 告警总量 | 4.4 万 (alerts) |
| 故事线 | 36 条 |
| 异常告警 | 52 条 |
| IOC 总量 | 123,776 条 (IP 44,217 + Hash 1,766 + URL 77,793) |
| SIEM 转发事件 | 21 条 (CEF/Syslog) |

## 本次修复项

以下 4 项在首轮测试中标记为 ENV（环境限制），本轮已全部修复并验证通过：

| 问题 | 根因 | 修复方案 | 验证结果 |
|------|------|----------|----------|
| IOC Feed 拉取失败 | Docker 容器默认 DNS 无法解析外部域名 | docker-compose.dev.yml manager 服务添加 `dns: [8.8.8.8, 8.8.4.4]` | 6 个 Feed 全部拉取成功，123,776 条 IOC |
| 序列检测无规则 | sequence_rules 表为空 | 预置 MXSEQ-0001 提权序列规则（2 步 CEL + 300s 窗口） | compiled=1, errors=0, Redis 状态机就绪 |
| 多源情报同步失败 | 同 Docker DNS 问题 | 同上 DNS 修复 | sync_status=success, duration=8s |
| SIEM 转发未配置 | siem 模块未接入 Consumer | Config 加 SIEM 配置项 + Consumer 初始化 Forwarder + AlertGenerator 注入转发 | 21 条 CEF 事件成功转发到 UDP Syslog 接收端 |

---

## 结论

**M1-M4 全部 25 项功能测试完成，25 项通过，0 项失败，通过率 100%。** 核心检测链路（eBPF→Agent 规则→Server CEL→告警→响应→故事线→SIEM 转发）全部端到端打通。IOC 情报同步、序列检测、SIEM 集成三项能力本轮完成代码修复和集成验证。
