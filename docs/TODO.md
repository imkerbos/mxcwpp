# TODO List

> 对标 Elkeid，梳理当前系统缺失/未完成的功能。已完成内容见 git 历史。

---

## Part 1 — 当前迭代（进行中）

### 一、架构区分 UI 补全 — P0

> 后端已完全实现（Agent 检测、心跳字段、模型、任务下发过滤），只差 UI 展示。

- [ ] 主机列表：增加 `runtime_type` 显示标签（vm/docker/k8s Tag）
- [ ] 策略编辑弹窗：确认 `runtime_types` 多选控件是否已实现，未实现则补充
- [ ] 规则编辑弹窗：同上
- [ ] 告警列表：增加按运行环境筛选的过滤条件

---

### 二、告警系统完善 — P0

> 通知发送已实现（Lark/Webhook/离线告警/恢复通知），以下为真正缺失部分。

#### 2.1 告警白名单（完全缺失）
- [ ] 数据模型：`alert_whitelist` 表（匹配字段：rule_id/host_id/category/severity，支持通配）
- [ ] 后端 API：`GET/POST/PUT/DELETE /api/v1/alerts/whitelist`
- [ ] 白名单匹配逻辑：告警生成前先匹配白名单，命中则跳过写入
- [ ] 前端页面：`/whitelist`（当前 DevInProgress）

#### 2.2 Agent 离线告警验证
- [ ] 确认 Agent 离线 → 触发 `agent_offline` 告警 → 发送通知 完整链路是否跑通

---

### 三、操作审计日志 — P1

> 完全缺失，合规需求。

- [ ] 数据模型：`audit_logs` 表（user_id、username、action、resource_type、resource_id、ip、detail、created_at）
- [ ] Gin 中间件：自动记录关键 API 的操作日志（POST/PUT/DELETE 接口）
- [ ] 查询 API：`GET /api/v1/audit-logs`（支持按用户、操作类型、时间段筛选、分页）
- [ ] 前端页面：`/audit-log`（当前 DevInProgress）

---

### 六、CIS 基线规则补全 — P1

> 逐一核查现有规则文件，补充缺失条目。架构区分完成后确认各规则的 `runtime_types`。

| 文件 | 需补充 |
|------|-------|
| sysctl-security.json | SYSCTL_026~029（IPv6 禁止源路由/路由广告、用户命名空间限制、perf_event_paranoid）|
| audit-logging.json | AUDIT_016~025（chmod/chown/setxattr 审计、SUID/SGID 特权命令、内核模块操作审计、审计规则不可变）|
| account-security.json | USER_001~005（禁止 .rhosts/.netrc/.forward、root PATH 不含 `.` 和可写目录）|
| service-status.json | SERVICE_021~024（禁用 rpcbind/postfix/ldap/X Window System）|
| password-policy.json | PAM_001~004（su 限制 wheel 组、历史密码、账户非活动锁定 INACTIVE=30）|
| file-permissions.json | FILE_021~023（/etc/motd、/etc/issue、/etc/issue.net 权限）|
| ssh-baseline.json | 核查 SSH_016~023 是否已覆盖（加密算法 Ciphers/MAC/KexAlgorithms、GSSAPIAuthentication、UseDNS）|
| mac-security.json | 核查 MAC_001~005 完整性（SELinux 安装/模式/策略/enforcing/unconfined）|
| secure-boot.json | 核查 BOOT_001~004 完整性（GRUB 密码、单用户认证、Ctrl-Alt-Del、权限）|
| file-integrity.json | 核查 AIDE_001~004 完整性（安装、初始化、cron 定期检查、权限）|
| network-protocols.json | 核查 NET_001~005 完整性（禁用 DCCP/SCTP/RDS/TIPC/无线）|
| cron-security.json | 核查 CRON_001~007 完整性（各 cron 目录权限 700、cron.allow、at.allow）|
| login-banner.json | 核查 BANNER_001~003 完整性（motd/issue/issue.net 内容配置）|

- [ ] 所有 VM 专属规则确认 `runtime_types: ["vm"]`

---

## Part 2 — 后续迭代

### 四、漏洞管理（VulnList）
> 完全缺失，需新建整个模块。
- [ ] 漏洞数据模型（host_id、cve_id、severity、software_name、version）
- [ ] 后端 API：列表、统计
- [ ] Collector 对接漏洞库
- [ ] 前端 `/vuln-list` 页面

### 五、病毒查杀（VirusScan）
> 需要新 Scanner 插件，复杂度高。
- [ ] Scanner 插件（ClamAV/yara）
- [ ] 病毒扫描结果数据模型 + 隔离管理
- [ ] 后端 API：扫描任务、隔离管理
- [ ] 前端 `/virus/scan` 和 `/virus/quarantine` 页面

### 七、系统监控页面
> 后端数据已有，补充前端页面。
- [ ] 主机监控页面 `/system/host-monitor`
- [ ] 服务监控页面 `/system/service-monitor`
- [ ] 服务告警页面 `/system/service-alert`

### 八、RASP 应用防护
> 最复杂模块，暂缓。
- [ ] 后端 API（应用列表、配置、告警、漏洞、白名单）
- [ ] RASP 插件（Java/Python/Go/PHP/Node.js）
- [ ] 前端四个页面

### 九、Agent 功能
- [ ] 传输层 snappy 压缩（减少带宽）
- [ ] CPU/内存资源限制机制

### 十、配置备份
- [ ] 策略/规则/系统配置导出 API
- [ ] 恢复配置 API
- [ ] 前端备份管理页面 `/system/backup`

### 十一、生产部署
- [ ] 部署 MySQL、Server、UI、插件、Agent
- [ ] 验证完整流程
