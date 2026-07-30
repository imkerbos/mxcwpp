# ML 异常检测安全说明（M0）

本文档说明 ML 异常检测引擎（Isolation Forest + 多指标关联）在 M0 的安全默认、模式语义、
schema 闸、可观测性以及启用/回滚方式。M0 的目标是**线上止血**：让这套尚不成熟的 ML 信号
默认不产生任何业务影响，只在显式开启且前置条件就绪后才逐步落库。

## 四种安全模式

由 feature flag `anomaly.detector_mode` 控制（见 `internal/server/engine/anomaly/mode.go`）：

| 模式 | 行为 | 严重度上限 |
| --- | --- | --- |
| `off` | 完全关闭：不消费、不打分、不产任何信号 | — |
| `shadow`（默认） | 照常打分与关联检测，但**只写日志/指标，不落库** `anomaly_alerts` | high |
| `context` | 落库供 SOC 分析上下文；不进入任何自动响应 | high |
| `alert` | 允许 critical 定罪（正式告警）；需显式配置 **且** schema 就绪 | critical |

**安全默认**：缺配置 / 非法值 / DB 查询失败一律回落 `shadow`（绝不回落 `context` / `alert`），
保证"缺配置绝不默认写正式告警"。落库判定用正向白名单——只有**生效** `context` / `alert` 才 upsert，
`off` / `shadow` 及任何降级都不落库（结构性防止 `SetMode` 并发切 `off` 后快照仍落库）。

## Schema 闸（fail-closed）

落库依赖 `anomaly_alerts` 的 `hit_count` / `last_seen_at` 列与去重唯一索引
`ux_anomaly_alerts_dedup`（`tenant_id, host_id, alert_type, pattern_name, top_metric`）。
启动时 `VerifySchema()` 校验，缺任一项则判 schema 未就绪：

- 进程**保持存活**（不 fatal），其余消费功能不受影响；
- 一切会落库的模式（`context` / `alert`）**fail-closed 降级为 `shadow`**（只观测不落库）——
  因为无去重索引会让 upsert 退化成"每次触发新建一行"刷屏；
- `alert` 即便配置也需 schema 就绪才允许 critical。

## 严重度上限（不把 ML 信号当正式高危定罪）

- 仅**生效** `alert` 模式允许 critical，其余（含 `context`）一律封顶 high；
- `c2_beacon` 关联在 M0 仅有 proc/net/DNS **量级**联合升高，**无周期性（beaconing）特征**，
  pattern 声明上限即为 high，Description 明确为"待研判的 process/network/DNS-volume correlation、
  未验证 C2 周期性"。`pattern_name` 保留 `c2_beacon` 仅为兼容既有查询/UI/反馈数据，不代表已证实 C2。

## DNS M0 限制

agent 侧尚未采集真实 DNS 的 domain / rcode，因此：

- `dns_unique_domain`（idx 11）/ `dns_nx_ratio`（idx 12）两维不可信，不计入关联判定；
- `reconnaissance` 依赖 NXDOMAIN 枚举，M0 **整体禁用**；
- `dns_query` 事件的 `remote_addr` 是 resolver IP，不是被查询域名，**绝不**作为 `SuspiciousDomains` 富化
  （避免"把 resolver IP 称作 domain"）；
- `dns_query_count`（idx 10）只是计数、不依赖 domain/rcode，仍可信。

`dnsValid` 默认 `false`；M1 接通真实 domain/rcode 后再放开（同时置位 `dns_field_ready` 指标）。

## 可观测性：/readyz 与 Prometheus

- **/readyz**（Consumer 独立 HTTP 端口，默认 `:9100`）聚合各组件就绪检查，`anomaly_schema` 未就绪返回 503。
  与 `/healthz`（进程存活）区分：schema 未就绪时进程仍存活（只观测不落库），但 `/readyz` 报未就绪。
- **Prometheus 指标**（低基数，无 `host_id` label，注册在 Consumer 独立 registry，启动初始化并每 30s 刷新）：
  - `mxcwpp_anomaly_detector_mode{mode}` / `mxcwpp_anomaly_detector_effective_mode{mode}`：配置/生效模式 one-hot；
  - `mxcwpp_anomaly_schema_ready` / `mxcwpp_anomaly_dns_field_ready` / `mxcwpp_anomaly_iforest_trained`：0/1；
  - `mxcwpp_anomaly_sample_count` / `mxcwpp_anomaly_host_count`：训练样本数 / 已跟踪主机数。

## 如何显式启用

1. 先确认 schema 就绪：`/readyz` 中 `anomaly_schema=ready`（或指标 `mxcwpp_anomaly_schema_ready=1`）。
   若未就绪，检查迁移 `migrateAnomalyAlertDedup` 是否成功建索引。
2. 灰度落库上下文：把 flag `anomaly.detector_mode` 设为 `context`（经 manager admin API）。
   观察 `mxcwpp_anomaly_detector_effective_mode{mode="context"}=1`，严重度仍封顶 high。
3. 如需正式告警（谨慎）：设为 `alert`。仅在 schema 就绪时才生效为 `alert` 并允许 critical。

## 如何回滚

- 立即止血：把 flag 设回 `shadow`（继续观测但不落库）或 `off`（完全停）。Consumer 每 30 秒重载一次，最迟一个刷新周期生效；重启也会立即加载。
- 无需回滚代码：模式是运行时开关，安全默认恒为 `shadow`。

## M1 仍需补齐（M0 未做，勿高估现状）

M0 只是安全止血，**不代表 ML 达到行业先进水平**。要成为可信的生产检测能力，M1 仍需：

- **feature store**：统一、可回溯的特征存储，替代进程内内存缓冲；
- **模型持久化 / 版本化**：训练产物落盘 + 版本管理，避免重启即冷启动；
- **时间切分验证**：按时间划分训练/验证集，评估真实泛化而非过拟合；
- **漂移 / 质量监控**：数据漂移、误报率、命中质量的持续度量与告警；
- **真实 DNS domain/rcode 采集**：放开 DNS 相关维与 `reconnaissance`。
