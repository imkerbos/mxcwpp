# MxSec TODO

> 更新：2026-04-16 | 完成度：**MVP 100%** | 阶段：**质量加固 + 能力补齐**

---

## 已完成模块

| 模块 | 状态 | 关键实现 |
|------|------|---------|
| 基线安全 | ✅ | 策略/规则/任务/修复，212 条规则 |
| 告警与白名单 | ✅ | 列表/处置/白名单匹配 |
| 审计日志 | ✅ | 模型/中间件/API/前端 |
| FIM | ✅ | 策略/事件/任务，ClickHouse 归档 |
| 容器集群 | ✅ | 集群/告警/事件/基线/白名单/K8s audit + CIS 80 条规则 |
| 病毒查杀 | ✅ | Scanner 插件（ClamAV + YARA-X）+ API + 前端 + Consumer 路由 |
| 漏洞管理 | ✅ | PURL 采集 + OSV.dev 匹配 + CVSS v3.1 评分 |
| CEL 规则引擎 | ✅ | 20 条内置规则 + MITRE 映射 + 热加载 + Consumer 集成 |
| eBPF Sensor | ✅ | Tetragon 事件采集 → ClickHouse ebpf_events |
| 告警溯源 | ✅ | 上下文 API + 时间线 + 进程树 + MITRE 矩阵 |
| 行为序列检测 | ✅ | 滑动窗口 + 状态机 + Redis |
| LLM 辅助 | ✅ | Claude/Llama 告警摘要 + 处置建议 |
| 威胁情报 | ✅ | MISP IOC → Redis → CEL 碰撞 |
| 自动响应 | ✅ | 规则命中 → AC 下发 kill/隔离 |
| HA 架构 | ✅ | Manager×2 + AC×2 + Consumer×2 + Kafka + Redis SD + ClickHouse |
| 系统管理 | ✅ | 用户/通知/组件/安装/巡检/授权 |
| 运维交付 | ✅ | Tetragon 部署 + freshclam 配置 + 离线告警 + K8s CIS 扩充 |

---

## 近期：高 ROI 任务

> 原则：验证已有功能真正工作 > 新增花哨功能

### 1. ~~自动响应链路端到端验证~~ ✅

- [x] AutoResponder 集成到 Consumer Router（CEL 命中 → 自动响应）
- [x] CommandForwarder 实现（Redis 查 AC → HTTP 转发 /command）
- [x] 单元测试覆盖：critical 触发、非 critical 跳过、nil 安全、分发失败降级、部分字段、多规则

**关键文件**: `consumer/celengine/response.go` + `forwarder.go` → `agentcenter/httptrans/` → `agent/`

### 2. ~~关键链路集成测试~~ ✅

- [x] sensor → protobuf → CEL → 规则匹配 端到端测试（挖矿/反弹Shell）
- [x] scanner → CEL → 告警 端到端测试（Trojan 检测 + DataType 过滤）
- [x] kube audit → detector → alarms 规则匹配测试（8 条 K8S 规则全覆盖）
- [x] Consumer Router 消息路由完整性验证（25+ DataType）
- [x] protobuf 编解码链路测试

**测试文件**: `consumer/router_test.go` + `biz/kube_detector_test.go`

### 3. 漏洞离线缓存

- [ ] 定期下载 OSV.dev 数据到本地 MySQL/文件缓存
- [ ] VulnScanner 优先查本地缓存，缓存 miss 再查 OSV API
- [ ] 支持手动触发全量同步

**解决**: 内网客户无法访问外部 API 的问题

### 4. 病毒扫描白名单 + 隔离箱完善

- [ ] 扫描结果白名单：按文件路径/hash/威胁名称忽略，后续扫描自动跳过
- [ ] 隔离箱补齐：quarantine_files 与 scan_results 关联、恢复审计、批量处置
- [ ] 误报处理流程：标记误报 → 加入白名单 → 后续扫描不再告警

### 5. K8s 基线历史快照

- [ ] 新增 `kube_baseline_snapshots` 表，保留每次检查的完整结果
- [ ] 支持按时间查看历史检查结果和趋势对比
- [ ] 周期报告可引用历史数据

---

## 中期：按需求驱动

### 漏洞优先级排序

- [ ] 简单加权评分：CVSS 基础分 + 是否在运行中 + 是否对外暴露 + 补丁可用性
- [ ] 前端漏洞列表按优先级排序

### 攻击样本回放测试

- [ ] 建立关键场景测试样本：反弹 Shell、提权、挖矿、容器逃逸
- [ ] CEL 规则回放验证脚本

### 等保/审计报表

- [ ] 按客户需求输出等保合规证据链报表

---

## 远期：规划方向

| 方向 | 说明 | 备注 |
|------|------|------|
| K8s 准入控制 | Webhook + 特权容器/hostPath/hostNetwork 拦截 | 工作量大，做 PoC 验证 |
| 漏洞覆盖扩展 | 容器镜像、SBOM 导入、语言依赖 | 按需扩展 |
| FIM 实时化 | eBPF file_open 事件已有，评估是否替代 AIDE | 现状够用 |
| 多租户 | 业务线级别的数据隔离 | 大工程，需明确需求 |
| 规则工程化 | 版本管理、灰度发布、命中率统计 | 需生产数据积累 |

---

## 测试覆盖

| 测试包 | 测试文件数 | 状态 |
|--------|-----------|------|
| `agentcenter/scheduler` | 3 | ✅ agent_update / plugin_update / heartbeat_timeout |
| `agentcenter/transfer` | 1 | ✅ service_test |
| `agentcenter/service` | 1 | ✅ policy_test |
| `manager/api` | 6 | ✅ integration / dashboard / hosts_metrics / assets_stats / assets_export / vulnerabilities |
| `manager/biz` | 4 | ✅ metrics / kube_baseline_check / kube_detector / kube_alarm_filter |
| `consumer` | 1 | ✅ router_test (CEL 链路 + 路由 + protobuf) |
| `consumer/celengine` | 3 | ✅ engine_test / response_test / forwarder_test |
| `migration` | 1 | ✅ init_data_test |
| `agent/buffer` | 1 | ✅ buffer_test |
| `agent/resource` | 1 | ✅ resource_test |
| `plugins/baseline` | 3 | ✅ engine / checkers / e2e |
| `plugins/collector` | 1 | ✅ network_test |
| **合计** | **26** | **13 包全部 PASS** |

---

## 架构决策备忘

1. **Tetragon eBPF**（非自研）— 避免内核兼容性问题，CNCF 生产就绪
2. **CEL-Go 规则引擎**（非 Falco/Sigma）— 嵌入式，无独立进程
3. **OSV.dev 漏洞库**（非自建）— Google 维护，免费 API
4. **YARA-X**（非经典 YARA）— Rust 重写，经典 YARA 已 EOL
5. **放弃 RASP** — OpenRASP 已停维护，Tetragon 替代
6. **SD 用 Redis**（非 etcd）— AC ≤ 500 实例内无需引入

## 关键文件速查

| 用途 | 路径 |
|------|------|
| Consumer 路由 | `internal/server/consumer/router.go` |
| CEL 引擎 | `internal/server/consumer/celengine/engine.go` |
| 内置规则 | `configs/rules/builtin-rules.yaml` |
| 告警生成 | `internal/server/consumer/celengine/alert.go` |
| Scanner 插件 | `plugins/scanner/engine/` |
| Sensor 插件 | `plugins/sensor/engine/` |
| 漏洞扫描 | `internal/server/manager/biz/vuln_scanner.go` |
| ClickHouse DDL | `deploy/init-clickhouse.sql` |
| 任务调度 | `internal/server/manager/biz/task_scheduler.go` |
| Kafka Topic | `internal/server/common/kafka/topics.go` |
