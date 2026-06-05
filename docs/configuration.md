# 配置参考 v2

> **平台定位**：mxsec 是**工业级开源 CWPP**，专精 **Linux 主机与 Kubernetes 容器**，面向 ToB 政企/金融/互联网客户。本文档定义六微服务（Manager / AgentCenter / Consumer / Engine / VulnSync / LLMProxy）与 Agent 的全部配置项，覆盖**监听优先（observe-first）**、**多租户 from-day-1**、**本地 ML 主导 + LLM 可选**三大产品原则的运行时表达。
>
> 上位文档（强制对齐）：
> - 架构总图 [`architecture.md`](architecture.md)
> - 运行模式 [`operating-modes.md`](operating-modes.md)
> - 多租户 [`multi-tenant.md`](multi-tenant.md)
>
> **配置原则**：所有 secret（API Key / DB 密码 / JWT secret / 内部 Bearer Token / mTLS 私钥）**禁止入版本库**，统一走环境变量 / KMS / Kubernetes Secret 注入。配置文件只保留占位符 `${VAR}`。

---

## 0. 总览

### 0.1 六微服务 + Agent 配置文件清单

| 服务 | 配置文件 | 主要职责 | 副本拓扑 |
|------|---------|---------|---------|
| Manager | `/etc/mxsec/manager.yaml` | 业务 API + RBAC + 多租户控制 + 模式编排 | N 副本无状态 |
| AgentCenter | `/etc/mxsec/agentcenter.yaml` | gRPC 接入 + mTLS + 任务下发 + Canary | N 副本无状态 |
| Consumer | `/etc/mxsec/consumer.yaml` | Kafka → MySQL / ClickHouse / Redis 幂等写入 | N 副本 + ConsumerGroup Rebalance |
| Engine | `/etc/mxsec/engine.yaml` | CEL / 序列 / ML / Storyline 检测，产 Alert | N 副本 + CPU 密集 |
| VulnSync | `/etc/mxsec/vulnsync.yaml` | 15 源漏洞情报同步 + advisory 仲裁 | 单副本 + Leader Election |
| LLMProxy | `/etc/mxsec/llmproxy.yaml` | 多 LLM 厂商适配 + 路由 + 缓存 + 计费 | N 副本无状态（可选） |
| Agent | 编译时 ldflags + Server 下发 | 端侧 EDR + 插件管家 + 修复执行 | 每主机/节点 1 实例 |

### 0.2 配置层级与覆盖优先级

mxsec 采用 **4 级覆盖**（同 [`operating-modes.md`](operating-modes.md) §4 / [`multi-tenant.md`](multi-tenant.md) §7）：

```
[最低] 全局默认 (yaml 文件)
   └→ 租户级覆盖 (tenants 表 + tenant_configs JSON)
        └→ 主机标签级覆盖 (hosts.labels + 规则 host_label_filter)
             └→ 规则级覆盖 (engine_rules.mode_override) [最高]
```

**配置项的覆盖能力**矩阵：

| 配置项 | 全局 | 租户 | 主机标签 | 规则 |
|--------|------|------|---------|------|
| `mode` | ✅ | ✅ | ✅ | ✅ |
| `ml.enabled` | ✅ | ✅ | ✅ | — |
| `ml.models[*].enabled` | ✅ | ✅ | ✅ | — |
| `llm.enabled` | ✅ | ✅ | — | — |
| `llm.provider` / `routing` | ✅ | ✅ | — | — |
| `llm.quota.monthly_usd` | ✅ | ✅ | — | — |
| `retention.alerts_days` | ✅ | ✅ | — | — |
| `notification.channels[*]` | ✅ | ✅ | — | — |
| `engine.rules[*].enabled` | ✅ | ✅ | ✅ | ✅ |
| `baseline.policies[*]` | ✅ | ✅ | ✅ | — |

### 0.3 默认档位（Smart 档）

| 档位 | `ml.enabled` | `llm.enabled` | 适用场景 |
|------|-------------|--------------|---------|
| Baseline | `false` | `false` | 离网低配 / 严格信创 / 不信任 AI |
| **Smart（默认推荐）** | **`true`** | **`false`** | 离网政企 / 金融 / 关基 |
| AI-Native | `true` | `true` | 互联网 / 公网可用客户 |

> 默认模式：`mode.default = observe`（监听优先，详见 [`operating-modes.md`](operating-modes.md)）。

---

## 1. Manager 配置

`/etc/mxsec/manager.yaml`

```yaml
service:
  name: mxsec-manager
  instance_id: ""
  http_addr: ":8080"
  grpc_addr: ":9080"
  external_url: "https://mxsec.example.com"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/manager.log
  log_max_age_days: 30
  shutdown_grace_sec: 30
# --- 数据库（业务主数据） ---
database:
  type: mysql
  mysql:
    host: mysql
    port: 3306
    user: mxsec_user
    password: ${MYSQL_PASSWORD}
    database: mxsec
    charset: utf8mb4
    parse_time: true
    loc: Asia/Shanghai
    max_idle_conns: 20
    max_open_conns: 200
    conn_max_lifetime: 1h
# --- Redis（SD / 缓存 / 分布式锁 / LLM cache 共用） ---
redis:
  sentinel: true
  master_name: mymaster
  sentinel_addrs:
    - redis-sentinel-1:26379
    - redis-sentinel-2:26379
    - redis-sentinel-3:26379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 100
  min_idle_conns: 10
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
# --- Kafka（Manager 仅生产 audit / 用量上报，不消费业务 topic） ---
kafka:
  enabled: true
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  topic_prefix: ""
  producer:
    required_acks: -1
    max_message_bytes: 1048576
    flush_messages: 500
    flush_frequency: 500ms
    retry_max: 3
# --- JWT + RBAC（多租户三段鉴权第 1/3 段） ---
jwt:
  secret: ${JWT_SECRET}
  issuer: mxsec-manager
  audience: mxsec-platform
  access_token_ttl: 2h
  refresh_token_ttl: 30d
  algorithm: HS256
  clock_skew_sec: 30
rbac:
  backend: casbin
  model_path: /etc/mxsec/rbac/model.conf
  policy_storage: db
  enforcer_cache: true
  enforcer_cache_ttl: 60s
  default_role: viewer
  system_admin_role: system-admin
mode:
  default: observe
  tenants:
    - id: t-internal
      mode: protect
    - id: t-bank-a
      mode: observe
  host_labels:
    - tenant: t-internal
      label: env=dev
      mode: protect
    - tenant: t-internal
      label: env=prod-core
      mode: observe
  rules:
    - rule_id: BRUTE_FORCE_SSH
      mode: protect
    - rule_id: ML_ANOMALY_PROCESS
      mode: observe
  switch_gates:
    min_observe_days: 90
    max_fp_rate: 0.02
    min_precision: 0.85
    min_recall_replay: 0.85
    require_dual_approval: true
    canary_steps: [5, 25, 100]
    canary_step_observe_hours: 24
    auto_rollback_fp_rate: 0.05
# --- 多租户（详见 docs/multi-tenant.md） ---
tenants:
  default_quota:
    agents: 100
    llm_monthly_usd: 100.0
    events_per_day: 1000000000
    retention_alerts_days: 90
    retention_events_days: 30
    retention_audit_days: 180
  isolation:
    default_strategy: shared
    allow_kafka_dedicated_topic: true
  mssp:
    enabled: true
    max_children_per_parent: 200
    aggregate_read_only: true
    soft_delete_retention_days: 30
  cross_tenant_paths:
    - /api/v2/admin/tenants
    - /api/v2/admin/system/health
    - /api/v2/mssp/children
    - /api/v2/mssp/aggregate/alerts
  guard:
    refuse_missing_tenant_id: true
    panic_on_tenant_scope_missing: true
# --- 模式 / ML / LLM 总开关（被租户级覆盖） ---
ml:
  enabled: true
  models:
    iforest-host:        { enabled: true,  weight: 1.0 }
    markov-process:      { enabled: true,  weight: 1.0 }
    syscall-bloom:       { enabled: true,  weight: 0.8 }
    lgbm-elf:            { enabled: true,  weight: 1.0 }
    lgbm-dga:            { enabled: true,  weight: 1.0 }
    iforest-image:       { enabled: true,  weight: 0.9 }
    xgb-network:         { enabled: true,  weight: 0.8 }
    minilm-dedupe:       { enabled: true,  weight: 1.0 }
    securitybert-cmdline: { enabled: false, weight: 0.7 }
    kmeans-ueba:         { enabled: true,  weight: 1.0 }
llm:
  enabled: false
  proxy_endpoint: llmproxy:18900
  internal_token_env: MXSEC_LLM_INTERNAL_TOKEN
  timeout_ms: 8000
  fail_open: true
  scenes_enabled:
    alert_explain: true
    storyline_summary: true
    nl2query: true
    rule_draft: false
# --- 数据保留（被租户级覆盖） ---
retention:
  alerts_days: 90
  events_days: 30
  audit_days: 180
  reports_days: 365
  metrics_days: 90
# --- 通知（被租户级覆盖） ---
notification:
  enabled: true
  channels:
    - type: inbox                  # 站内信，always on
      enabled: true
    - type: email
      enabled: true
      smtp_host: smtp.example.com
      smtp_port: 465
      smtp_user: noreply@example.com
      smtp_password: ${SMTP_PASSWORD}
      from: "mxsec <noreply@example.com>"
      tls: true
    - type: sms
      enabled: false
      provider: aliyun
      access_key: ${SMS_ACCESS_KEY}
      secret_key: ${SMS_SECRET_KEY}
      sign_name: mxsec
      template_code: SMS_xxx
    - type: webhook
      enabled: true
      url_env: NOTIFY_WEBHOOK_URL
      timeout: 10s
      retry_max: 3
      sign_secret_env: NOTIFY_WEBHOOK_SIGN
    - type: syslog
      enabled: false
      address: syslog.example.com:514
      protocol: udp
      facility: local0
      cef_format: true
    - type: slack
      enabled: false
      webhook_url_env: SLACK_WEBHOOK_URL
      mention_groups:
        critical: ["<!subteam^S01ABC>"]
  throttle:
    enabled: true
    per_rule_per_host:
      window_sec: 300
      max_count: 5
    per_tenant:
      window_sec: 60
      max_count: 100
# --- 速率限制（API gateway） ---
rate_limit:
  enabled: true
  backend: redis
  global:
    qps: 5000
    burst: 10000
  per_tenant:
    qps: 500
    burst: 1000
  per_user:
    qps: 50
    burst: 100
  endpoints:
    "/api/v2/auth/login":
      qps: 5
      burst: 10
    "/api/v2/baseline/scan":
      qps: 2
      burst: 5
    "/api/v2/vulnsync/trigger":
      qps: 1
      burst: 1
# --- 内部服务调用（gRPC + mTLS + Bearer Token） ---
internal:
  bearer_token_env: MXSEC_INTERNAL_BEARER
  mtls:
    ca_cert: /etc/mxsec/certs/ca.crt
    cert: /etc/mxsec/certs/manager.crt
    key: /etc/mxsec/certs/manager.key
    server_name: mxsec.internal
  agentcenter:
    endpoints: ["ac-0:9080", "ac-1:9080"]
    timeout_ms: 5000
  engine:
    endpoints: ["engine-0:9083", "engine-1:9083"]
    timeout_ms: 8000
  vulnsync:
    endpoints: ["vulnsync-0:9085"]
    timeout_ms: 30000
  llmproxy:
    endpoints: ["llmproxy-0:18900", "llmproxy-1:18900"]
    timeout_ms: 60000
# --- 资产 / 任务 / 报表 ---
report:
  pdf_engine: chromium
  chromium_path: /usr/bin/chromium
  template_dir: /etc/mxsec/report/templates
  output_dir: /var/lib/mxsec/reports
  watermark: ""
task:
  default_timeout_sec: 600
  max_concurrent_per_host: 3
  cleanup_finished_days: 30
  retry_max: 3
  retry_backoff_sec: 30
# --- SD（AC 服务发现 Registry，内嵌 Manager） ---
sd_registry:
  enabled: true
  redis_hset_key: "ac:instances"
  pubsub_channel: "ac:sd:changed"
  agent_ac_mapping_ttl: 180s
  ac_heartbeat_ttl: 120s
  active_probe_interval: 30s
  fallback_full_sync_interval: 30s
# --- 可观测性 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
  mysql_fallback:
    enabled: false
    retention_days: 30
    batch_size: 100
    flush_interval: 5s
tracing:
  enabled: false
  otlp_endpoint: otel-collector:4317
  sample_ratio: 0.01
# --- 健康检查 ---
healthcheck:
  liveness_path: /healthz
  readiness_path: /readyz
  deep_check_interval: 30s
  deps:
    - mysql
    - redis
    - kafka
```

### 1.1 Manager 配置要点

- **`mode.default = observe` 是强约束**：CI lint 拒绝 `protect` 全局默认。
- **`tenants.guard.panic_on_tenant_scope_missing = true`** 是 from-day-1 多租户的硬约束（详见 [`multi-tenant.md`](multi-tenant.md) §3.3）。
- **`jwt.secret` / `rbac.*` 必须**通过 KMS / Vault / K8s Secret 注入，不入仓。
- **`internal.bearer_token_env`** 是 v2.0 升级项，原 `X-Internal-Secret` 仅作过渡。

---

## 2. AgentCenter 配置

`/etc/mxsec/agentcenter.yaml`

```yaml
service:
  name: mxsec-agentcenter
  instance_id: ""
  http_addr: ":8082"
  grpc_addr: ":6751"
  internal_grpc_addr: ":9080"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/agentcenter.log
  shutdown_grace_sec: 60
# --- mTLS（Agent 双向认证 + 证书自动下发） ---
mtls:
  ca_cert: /etc/mxsec/certs/ca.crt
  server_cert: /etc/mxsec/certs/agentcenter.crt
  server_key: /etc/mxsec/certs/agentcenter.key
  client_auth: VerifyClientCertIfGiven
  min_tls_version: "1.3"
  cipher_suites:                  
    - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
  auto_issue:
    enabled: true
    ca_key: /etc/mxsec/certs/ca.key
    cert_ttl: 365d
    rotate_before_expire: 30d
# --- Agent gRPC 长连接参数 ---
grpc_server:
  max_concurrent_streams: 10000
  max_recv_msg_bytes: 16777216
  max_send_msg_bytes: 16777216
  keepalive:
    time: 60s
    timeout: 10s
    min_time: 10s
    permit_without_stream: false
  compression: snappy
# --- Kafka（数据转发 + 内存降级队列） ---
kafka:
  enabled: true
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  topic_prefix: ""
  producer:
    required_acks: -1
    compression: snappy
    max_message_bytes: 4194304
    flush_messages: 500
    flush_frequency: 200ms
    retry_max: 5
    partitioner: agent_id_hash
  fallback_queue:
    enabled: true
    capacity: 10000
    ttl: 5m
    retry_max: 5
    retry_backoff: 30s
# --- SD（向 Manager 内嵌的 SD Registry 注册） ---
sd_registry:
  manager_addr: "http://manager:8080"
  register_interval: 60s
  heartbeat_interval: 30s
  graceful_deregister: true
  capabilities:                   
    max_agents: 10000
    canary_capable: true
# --- 任务下发 / 灰度（Canary） ---
scheduler:
  heartbeat:
    timeout: 180s
    check_interval: 30s
  agent_restart:
    enabled: true
    max_concurrent: 20
    delay_jitter_sec: 60
  plugin_update:
    enabled: true
    max_concurrent: 50
    timeout: 300s
  canary:
    enabled: true
    default_steps: [1, 5, 25, 100]
    step_observe_sec: 1800
    failure_threshold_pct: 5
    auto_rollback: true
    dispatch_lock_key: "mxsec:task:dispatch:lock"
    dispatch_lock_ttl: 8s
# --- 多租户（AC 仅做 tenant_id 透传与校验） ---
tenant:
  refuse_missing_tenant_id: true
  inject_to_kafka_key: true
# --- 内部调用（被 Manager / Engine 调用） ---
internal:
  bearer_token_env: MXSEC_INTERNAL_BEARER
  allowed_callers:
    - manager
    - engine
# --- Redis（仅用 agent:ac 映射 + 分布式锁） ---
redis:
  sentinel: true
  master_name: mymaster
  sentinel_addrs:
    - redis-sentinel-1:26379
    - redis-sentinel-2:26379
    - redis-sentinel-3:26379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 50
# --- 可观测性 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
healthcheck:
  liveness_path: /healthz
  readiness_path: /readyz
```

### 2.1 AgentCenter 配置要点

- `mtls.client_auth: VerifyClientCertIfGiven` 是为了支持**首次连接自动签发证书**；不可改 `RequireAndVerifyClientCert`，否则 Agent 注册失败。
- `kafka.fallback_queue` 是数据面韧性核心，**禁止关闭**。
- `scheduler.canary` 仅适用于 **Agent 升级 + 规则同步**两类长任务，业务命令走直接下发。

---

## 3. Consumer 配置

`/etc/mxsec/consumer.yaml`

```yaml
service:
  name: mxsec-consumer
  instance_id: ""
  http_addr: ":8084"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/consumer.log
  shutdown_grace_sec: 60
# --- Kafka ConsumerGroup A: mxsec-writers ---
kafka:
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  topic_prefix: ""
  consumer_group: mxsec-writers
  session_timeout_sec: 30
  heartbeat_interval_sec: 3
  max_poll_interval_sec: 300
  fetch_min_bytes: 1024
  fetch_max_wait_ms: 200
  rebalance_strategy: cooperative-sticky
  topics:
    - mxsec.agent.heartbeat
    - mxsec.agent.asset
    - mxsec.agent.events
    - mxsec.agent.ebpf
    - mxsec.agent.baseline
    - mxsec.agent.scanner
    - mxsec.agent.remediation
    - mxsec.agent.command-ack
    - mxsec.engine.alert
    - mxsec.engine.storyline
    - mxsec.vuln.advisory
    - mxsec.llm.audit
    - mxsec.metering.usage
# --- MySQL 写入器 ---
mysql:
  host: mysql
  port: 3306
  user: mxsec_user
  password: ${MYSQL_PASSWORD}
  database: mxsec
  charset: utf8mb4
  parse_time: true
  loc: Asia/Shanghai
  max_idle_conns: 30
  max_open_conns: 300
  conn_max_lifetime: 1h
  upsert_strategy: on_duplicate_key
# --- ClickHouse 写入器（事件归档） ---
clickhouse:
  enabled: true
  addrs:
    - clickhouse-0:9000
    - clickhouse-1:9000
  database: mxsec
  username: default
  password: ${CLICKHOUSE_PASSWORD}
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 1h
  dial_timeout: 10s
  read_timeout: 30s
  write_timeout: 30s
  batch:
    size: 5000
    flush_interval: 10s
    max_inflight: 16
    backoff_initial: 200ms
    backoff_max: 5s
  dedupe_key: "(tenant_id, event_id)"
# --- Redis（心跳缓存 + agent:ac 映射） ---
redis:
  sentinel: true
  master_name: mymaster
  sentinel_addrs:
    - redis-sentinel-1:26379
    - redis-sentinel-2:26379
    - redis-sentinel-3:26379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 80
# --- DLQ（死信队列） ---
dlq:
  enabled: true
  topic_suffix: ".dlq"
  retry_max: 3
  retry_backoff_initial: 1s
  retry_backoff_max: 30s
  payload_max_bytes: 1048576
  include_error_metadata: true
  replay:
    enabled: true
    batch_size: 100
    rate_limit_qps: 50
# --- Sanitize（敏感字段脱敏） ---
sanitize:
  enabled: true
  rules:
    pii:
      - field: user_email
        action: mask
        pattern: '(.{2}).*(@.*)'
        replacement: '$1***$2'
      - field: phone
        action: mask
        pattern: '(\d{3})\d{4}(\d{4})'
        replacement: '$1****$2'
    secrets:
      - field: command_line
        action: mask
        patterns:                 
          - 'password=\S+'
          - 'token=\S+'
          - 'AKID[A-Za-z0-9]+'
        replacement: '[REDACTED]'
      - field: env_vars
        action: drop_keys
        keys: [PASSWORD, SECRET, TOKEN, API_KEY, AWS_SECRET_ACCESS_KEY]
  per_tenant_override_enabled: true
# --- 多租户路由（按 isolation_strategy 分发） ---
tenant:
  refuse_missing_tenant_id: true
  isolation_router:
    enabled: true
    cache_ttl: 60s
    dedicated_db_pool_size: 20
# --- 性能 / 限流 ---
worker:
  per_topic_concurrency:
    mxsec.agent.ebpf: 16
    mxsec.agent.events: 8
    mxsec.agent.heartbeat: 4
    default: 4
  per_batch_max_bytes: 10485760
  per_batch_max_messages: 1000
# --- 可观测性 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
healthcheck:
  liveness_path: /healthz
  readiness_path: /readyz
  deep_check:
    kafka_lag_threshold: 100000
```

### 3.1 Consumer 配置要点

- **`upsert_strategy: on_duplicate_key`** 是 MySQL 幂等的关键，**禁止改成 insert**，否则重复消费时主键冲突。
- **`dlq.enabled = true` 强约束**：失败消息必须进 DLQ，禁止无限重试阻塞 ConsumerGroup。
- **`sanitize.rules` 与多租户**：租户级覆盖优先级高于全局，KA 客户可加 PII 字段。

---

## 4. Engine 配置

`/etc/mxsec/engine.yaml`

```yaml
service:
  name: mxsec-engine
  instance_id: ""
  http_addr: ":8083"
  grpc_addr: ":9083"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/engine.log
  shutdown_grace_sec: 60
# --- 运行模式（全局默认，被 Manager 推送的租户/标签/规则级覆盖） ---
mode:
  global_default: observe
  refresh_interval: 30s
  fail_safe_on_manager_down: observe
# --- Kafka ConsumerGroup B: mxsec-engine ---
kafka:
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  topic_prefix: ""
  consumer_group: mxsec-engine
  session_timeout_sec: 30
  heartbeat_interval_sec: 3
  max_poll_interval_sec: 300
  fetch_min_bytes: 1024
  fetch_max_wait_ms: 200
  topics:
    - mxsec.agent.ebpf
    - mxsec.agent.events
    - mxsec.agent.baseline
    - mxsec.agent.scanner
    - mxsec.agent.asset
    - mxsec.agent.heartbeat
    - mxsec.agent.remediation
    - mxsec.vuln.advisory
  producer:
    acks: all
    compression: snappy
    max_message_bytes: 4194304
    flush_frequency_ms: 200
    flush_max_messages: 256
    topics:
      alert: mxsec.engine.alert
      storyline: mxsec.engine.storyline
      feedback: mxsec.engine.feedback
# --- 规则层（L1 CEL） ---
rule:
  rules_path: /etc/mxsec/engine/rules
  custom_rules_path: /var/lib/mxsec/engine/rules-custom
  reload_interval_sec: 30
  cel_program_cache: 4096
  whitelist_enabled: true
  throttle:
    default_window_sec: 60
    default_max_alerts: 1
    burst_factor: 3
  sigma:
    enabled: true
    rules_dir: /etc/mxsec/engine/sigma
  falco:
    enabled: true
    rules_dir: /etc/mxsec/engine/falco
  tetragon:
    enabled: true
    rules_dir: /etc/mxsec/engine/tetragon
# --- 序列层（L2） ---
sequence:
  enabled: true
  state_backend: redis
  state_redis_key_prefix: "mxsec:seq:"
  markov_min_samples: 1000
  ngram_size: 3
  port_scan:
    window_sec: 60
    unique_ports_threshold: 30
  bruteforce:
    window_sec: 60
    failed_threshold: 5
    auto_block_ttl_sec: 3600
ml:
  enabled: true
  runtime: onnx
  runtime_lib: /usr/lib/onnxruntime/libonnxruntime.so
  models_path: /var/lib/mxsec/engine/models
  inference_timeout_ms: 50
  inference_concurrency: 100
  embedding_cache_ttl: 24h
  embedding_cache_key_prefix: "mxsec:ml:embedding:"
  fallback_on_runtime_error: true
  models:
    iforest-host:
      enabled: true
      weight: 1.0
      file: iforest_host_v3.onnx
      threshold: 0.7
    markov-process:
      enabled: true
      weight: 1.0
      file: markov_process_v2.onnx
      threshold: 0.85
    syscall-bloom:
      enabled: true
      weight: 0.8
      file: syscall_bloom_v1.onnx
    lgbm-elf:
      enabled: true
      weight: 1.0
      file: lgbm_elf_v2.onnx
      threshold: 0.6
    lgbm-dga:
      enabled: true
      weight: 1.0
      file: lgbm_dga_v3.onnx
      threshold: 0.5
    iforest-image:
      enabled: true
      weight: 0.9
      file_pattern: "iforest_image_*.onnx"
    xgb-network:
      enabled: true
      weight: 0.8
      file: xgb_network_v2.onnx
      threshold: 0.7
    minilm-dedupe:
      enabled: true
      weight: 1.0
      file: minilm-l6-v2.onnx
      cosine_threshold: 0.92
    securitybert-cmdline:
      enabled: false
      weight: 0.7
      file: securitybert_cmdline_v1.onnx
    kmeans-ueba:
      enabled: true
      weight: 1.0
      file: kmeans_ueba_v1.onnx
  canary:
    enabled: true
    candidate_traffic_pct: 5
    promote_threshold_p_value: 0.05
    rollback_on_perf_regression: true
# --- 图层（L4 Storyline） ---
storyline:
  enabled: true
  flush_interval_sec: 5
  story_idle_close_sec: 1800
  enable_lateral_movement: true
  attck_mapping_path: /etc/mxsec/engine/attck-mapping.json
  llm_summary:
    enabled: false
    scene: storyline_summary
    max_chars: 240
    only_severity_above: high
# --- K8s 层（L5） ---
k8s:
  enabled: true
  rules_dir: /etc/mxsec/engine/k8s-rules
  admission_dryrun_observe: true
  audit_event_topics:
    - mxsec.k8s.audit
# --- LLMProxy 客户端（可选） ---
llm:
  enabled: false
  proxy_endpoint: llmproxy:18900
  internal_token_env: MXSEC_LLM_INTERNAL_TOKEN
  tls_cert: /etc/mxsec/certs/engine.crt
  tls_key: /etc/mxsec/certs/engine.key
  timeout_ms: 8000
  fail_open: true
  cache_ttl_sec: 86400
  scenes:
    alert_explain: true
    storyline_summary: true
    nl2query: false
    rule_draft: false
# --- 多租户配置 override（运行时从 Manager 拉取） ---
tenant_isolation:
  refuse_missing_tenant_id: true
  default_tenant_id_fallback: ""
  config_pull_interval: 60s
  per_tenant_override_path: /var/lib/mxsec/engine/tenants
# --- 内部调用 ---
internal:
  bearer_token_env: MXSEC_INTERNAL_BEARER
  agentcenter:
    endpoints: ["ac-0:9080", "ac-1:9080"]
    timeout_ms: 5000
  manager:
    endpoints: ["manager-0:9080", "manager-1:9080"]
    timeout_ms: 5000
# --- MySQL 只读（初始化加载规则 / 租户配置） ---
mysql_readonly:
  host: mysql-ro
  port: 3306
  user: mxsec_ro
  password: ${MYSQL_RO_PASSWORD}
  database: mxsec
  max_open_conns: 8
  max_idle_conns: 2
# --- Redis（序列状态 + 缓存） ---
redis:
  sentinel: true
  master_name: mymaster
  sentinel_addrs:
    - redis-sentinel-1:26379
    - redis-sentinel-2:26379
    - redis-sentinel-3:26379
  password: ${REDIS_PASSWORD}
  db: 0
  pool_size: 64
# --- 可观测性 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
```

### 4.1 Engine 租户级 override 文件

Manager 推送至 Engine 本地 `/var/lib/mxsec/engine/tenants/{tenant_id}.yaml`：

```yaml
tenant_id: t-bank-a
mode:
  default: observe
ml:
  enabled: true
  models:
    securitybert-cmdline:
      enabled: true
    kmeans-ueba:
      enabled: true
llm:
  enabled: true
  scenes:
    alert_explain: true
    storyline_summary: true
    nl2query: false
    rule_draft: false
host_label_overrides:
  - label: role=database
    disable_models: [securitybert-cmdline, minilm-dedupe]
  - label: env=prod-critical
    enable_models: [iforest-host, markov-process, lgbm-elf]
rule_overrides:
  - rule_id: BRUTE_FORCE_SSH
    mode: protect
  - rule_id: ML_ANOMALY_PROCESS
    enabled: false
```

### 4.2 Engine 配置要点

- **`mode.fail_safe_on_manager_down: observe`** 是工业级安全网，Manager 故障时绝不升级到 protect。
- **`ml.fallback_on_runtime_error: true`** 保证 ONNX Runtime 崩溃不引起检测中断。
- **`llm.fail_open: true`** 是 LLM 可选化的核心原则，LLMProxy 故障不影响主检测路径。

---

## 5. VulnSync 配置

`/etc/mxsec/vulnsync.yaml`

```yaml
service:
  name: mxsec-vulnsync
  instance_id: vulnsync-1
  http_addr: ":8085"
  grpc_addr: ":9085"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/vulnsync.log
  pprof_addr: ":6060"
# --- Leader Election ---
leader_election:
  backend: redis
  redis_addr: redis-master:6379
  redis_password: ${REDIS_PASSWORD}
  redis_db: 0
  lock_key: "mxsec:vulnsync:leader"
  lock_ttl: 30m
  heartbeat_interval: 10m
# --- 全局调度 ---
schedule:
  incremental_cron: "0 0 * * * *"
  full_cron: "0 30 3 * * *"
  max_parallel_sources: 4
sources:
  nvd:
    enabled: true
    api_key: ${NVD_API_KEY}
    window_days: 14
    concurrency: 4
    timeout: 60s
    proxy: ${VULNSYNC_HTTP_PROXY}
  osv:
    enabled: true
    batch_size: 100
    concurrency: 16
    timeout: 90s
  rhsa:
    enabled: true
    concurrency: 8
    timeout: 2h
    ua: "mxsec-vulnsync/1.0"
  usn:
    enabled: true
    concurrency: 4
    timeout: 60s
  debian-tracker:
    enabled: true
    dump_etag_cache: /var/lib/mxsec/vulnsync/cache/debian.etag
  alpine:
    enabled: true
    branches: [v3.18, v3.19, v3.20, edge]
  suse:
    enabled: true
    concurrency: 8
  cisa-kev:
    enabled: true
  exploit-db:
    enabled: true
  cnnvd:
    enabled: true
    api_url: "https://www.cnnvd.org.cn/web/homePage/cnnvdVulList"
    max_pages_per_run: 200
    max_cve_per_run: 5000
  epss:
    enabled: true
    url: "https://epss.cyentia.com/epss_scores-current.csv.gz"
  openeuler:
    enabled: true
    mirror: "https://gitee.com/openeuler/security-committee"
  anolis:
    enabled: true
  kylin:
    enabled: true
    offline_cache: /var/lib/mxsec/vulnsync/cache/kylin
  uos:
    enabled: true
    offline_cache: /var/lib/mxsec/vulnsync/cache/uos
# --- advisory 仲裁与融合 ---
merge_strategy:
  primary_index: purl
  secondary_index: nevra
  confidence_levels:
    high:
      sources_required: 2
      override_rule: "vendor_advisory_wins"
    medium:
      sources_required: 1
      requires_vendor_or_kev: true
    low:
      sources_required: 1
      allow_single_source: true
  validate:
    cve_id_required: false
    fixed_version_required_for_high: true
    drop_if_invalid: true
  nevra:
    redhat_family: rpmvercmp_official
    debian_family: dpkg_apt
    alpine_family: apk
    suse_family: rpmvercmp_official
# --- Kafka Producer ---
kafka:
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  topic_advisory: "mxsec.vuln.advisory"
  topic_dlq: "mxsec.vuln.advisory.dlq"
  acks: all
  compression: snappy
  max_message_bytes: 10485760
  flush_messages: 100
  flush_frequency: 1s
  partition_key: cve_id
# --- MySQL（读 vuln_data_sources 表 / 写 advisory_packages 备份） ---
db:
  host: mysql
  port: 3306
  user: mxsec_user
  password: ${MYSQL_PASSWORD}
  database: mxsec
  max_open_conns: 20
  max_idle_conns: 5
# --- 缓存（OSV / CSAF 离线） ---
cache:
  redis_addr: redis-master:6379
  redis_password: ${REDIS_PASSWORD}
  osv_detail_ttl: 30d
  rhsa_csaf_ttl: 90d
  cnnvd_ttl: 30d
# --- 失败熔断 ---
circuit_breaker:
  consecutive_failures: 5
  cooldown: 5m
  per_source: true
# --- 网络 ---
http_client:
  global_timeout: 90s
  max_idle_conns: 100
  max_idle_conns_per_host: 10
  proxy: ${VULNSYNC_HTTP_PROXY}
# --- 观测 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
healthcheck:
  liveness_path: /healthz
  readiness_path: /readyz
```

### 5.1 VulnSync 配置要点

- **`leader_election` 是单副本前提**，禁止 N 副本同时抓取（避免 NVD / RHSA 限流惩罚）。
- **`merge_strategy.validate.drop_if_invalid: true`** 是 advisory 质量底线，垃圾数据绝不入 Kafka。
- **`circuit_breaker.per_source: true`** 单源故障不连坐，例如 CNNVD 抖动不影响 NVD 同步。

---

## 6. LLMProxy 配置

`/etc/mxsec/llmproxy.yaml`

```yaml
service:
  name: mxsec-llmproxy
  instance_id: ""
  listen: ":18900"
  health_listen: ":18901"
  log_level: info
  log_format: json
  log_file: /var/log/mxsec/llmproxy.log
  mtls:
    cert: /etc/mxsec/certs/llmproxy.crt
    key: /etc/mxsec/certs/llmproxy.key
    ca: /etc/mxsec/certs/ca.crt
    min_tls_version: "1.3"
  internal_bearer: ${MXSEC_INTERNAL_BEARER}
# --- 离网开关 + 自动降级 ---
air_gapped: false
auto_downgrade_on_offline: true
# --- Redis（缓存 + 配额计数） ---
redis:
  sentinel: true
  master_name: mymaster
  sentinel_addrs:
    - redis-sentinel-1:26379
    - redis-sentinel-2:26379
    - redis-sentinel-3:26379
  password: ${REDIS_PASSWORD}
  db: 3
# --- Kafka（审计 + 计量上报） ---
kafka:
  brokers:
    - kafka-1:9092
    - kafka-2:9092
    - kafka-3:9092
  audit_topic: mxsec.llm.audit
  metering_topic: mxsec.metering.usage
# --- 缓存策略（24h 默认） ---
cache:
  enabled: true
  backend: redis
  ttl: 24h
  max_value_size_bytes: 262144
  key_prefix: "mxsec:llm:cache:"
  bypass_header: "X-LLM-Bypass-Cache"
  key_components:                 
    - model
    - messages
    - temperature
    - top_p
    - max_tokens
    - json_mode
# --- 脱敏 ---
sanitize:
  enabled: true
  on_air_gapped: false
  rules:
    ip: mask
    hostname: mask
    path: mask
    username: mask
    secrets: mask
# --- 厂商凭证池（密钥从 K8s Secret / Vault 注入，绝不入库） ---
providers:
  - name: openai
    driver: openai
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    timeout: 30s
  - name: anthropic
    driver: anthropic
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    timeout: 60s
  - name: gemini
    driver: gemini
    base_url: https://generativelanguage.googleapis.com
    api_key: ${GEMINI_API_KEY}
    timeout: 60s
  - name: dashscope                # 阿里千问
    driver: openai_compat
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: ${DASHSCOPE_API_KEY}
    timeout: 30s
  - name: deepseek
    driver: openai_compat
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
    timeout: 60s
  - name: kimi
    driver: openai_compat
    base_url: https://api.moonshot.cn/v1
    api_key: ${KIMI_API_KEY}
    timeout: 60s
  - name: glm                      # 智谱
    driver: openai_compat
    base_url: https://open.bigmodel.cn/api/paas/v4
    api_key: ${GLM_API_KEY}
    timeout: 30s
  - name: ollama                   # 本地（离网首选）
    driver: openai_compat
    base_url: http://ollama:11434/v1
    api_key: "ollama"
    timeout: 120s
    is_local: true
  - name: vllm                     # 本地高吞吐
    driver: openai_compat
    base_url: http://vllm:8000/v1
    api_key: "vllm"
    timeout: 120s
    is_local: true
routing:
  alert_explain:
    primary: { provider: openai, model: gpt-4o-mini, temperature: 0.2, max_tokens: 800 }
    fallbacks:
      - { provider: dashscope, model: qwen-turbo,    temperature: 0.2, max_tokens: 800 }
      - { provider: ollama,    model: qwen2.5:7b,    temperature: 0.2, max_tokens: 800 }
    json_mode: true
  storyline_summary:
    primary: { provider: anthropic, model: claude-3-5-sonnet-20241022, temperature: 0.3, max_tokens: 1500 }
    fallbacks:
      - { provider: kimi,      model: moonshot-v1-32k,  temperature: 0.3, max_tokens: 1500 }
      - { provider: dashscope, model: qwen-plus,        temperature: 0.3, max_tokens: 1500 }
      - { provider: ollama,    model: qwen2.5:14b,      temperature: 0.3, max_tokens: 1500 }
  nl2query:
    primary: { provider: deepseek, model: deepseek-chat, temperature: 0.0, max_tokens: 600 }
    fallbacks:
      - { provider: openai,    model: gpt-4o-mini,    temperature: 0.0, max_tokens: 600 }
      - { provider: dashscope, model: qwen-plus,      temperature: 0.0, max_tokens: 600 }
      - { provider: ollama,    model: qwen2.5:7b,     temperature: 0.0, max_tokens: 600 }
    json_mode: true
  rule_draft:
    primary: { provider: deepseek, model: deepseek-reasoner, temperature: 0.1, max_tokens: 4096 }
    fallbacks:
      - { provider: openai,    model: gpt-4o,                temperature: 0.1, max_tokens: 4096 }
      - { provider: anthropic, model: claude-3-5-sonnet-20241022, temperature: 0.1, max_tokens: 4096 }
      - { provider: ollama,    model: qwen2.5:14b,           temperature: 0.1, max_tokens: 4096 }
    cache: false
  embedding:
    primary: { provider: openai, model: text-embedding-3-small }
    fallbacks:
      - { provider: dashscope, model: text-embedding-v3 }
# --- 黑名单（Fallback 触发后冷却） ---
blacklist:
  failure_window: 1m
  failure_threshold: 3
  ttl: 5m
  key_prefix: "mxsec:llm:provider:blacklist:"
# --- 兜底文案（last resort，所有 provider 失败时返回） ---
fallback:
  enabled: true
  template_dir: /etc/mxsec/llmproxy/templates
# --- 配额（全局默认 + 租户覆盖） ---
quota:
  default_per_tenant:
    monthly_usd: 100.0
    qps: 5
    single_tokens_max: 16384
    calls_per_month: 100000
    threshold_warn: 0.8
    threshold_block: 1.0
    redis_key_prefix: "mxsec:llm:tenant:cost:"
  global:
    monthly_usd: 5000.0
    qps: 200
    burst: 500
# --- 计费模型（手动同步官方价格，每月更新） ---
pricing:
  update_check_url: ""
  models:
    "gpt-4o":              { input_per_1m: 2.50,  output_per_1m: 10.00 }
    "gpt-4o-mini":         { input_per_1m: 0.150, output_per_1m: 0.600 }
    "claude-3-5-sonnet-20241022": { input_per_1m: 3.00, output_per_1m: 15.00 }
    "qwen-plus":           { input_per_1m: 0.80,  output_per_1m: 2.00 }
    "qwen-turbo":          { input_per_1m: 0.30,  output_per_1m: 0.60 }
    "deepseek-chat":       { input_per_1m: 0.14,  output_per_1m: 0.28 }
    "deepseek-reasoner":   { input_per_1m: 0.55,  output_per_1m: 2.19 }
    "moonshot-v1-32k":     { input_per_1m: 1.66,  output_per_1m: 1.66 }
    "ollama:*":            { input_per_1m: 0,     output_per_1m: 0 }
    "vllm:*":              { input_per_1m: 0,     output_per_1m: 0 }
# --- 审计 ---
audit:
  enabled: true
  log_messages: false
  full_log_for_tenants: []
# --- 观测 ---
metrics:
  prometheus:
    enabled: true
    path: /metrics
healthcheck:
  liveness_path: /healthz
  readiness_path: /readyz
  deep_check:
    check_one_provider_at_least: true
```

### 6.1 LLMProxy 租户级覆盖（JSON in `tenants.llm_provider` 列）

```json
{
  "air_gapped": true,
  "routing": {
    "alert_explain":     {"primary_provider": "ollama", "primary_model": "qwen2.5:14b"},
    "storyline_summary": {"primary_provider": "vllm",   "primary_model": "Qwen2.5-14B-Instruct-AWQ"},
    "rule_draft":        {"primary_provider": "ollama", "primary_model": "qwen2.5:14b"}
  },
  "disabled_providers": ["openai", "anthropic", "gemini"],
  "quota": {
    "monthly_usd_override": 0,
    "qps_override": 2
  },
  "sanitize_on_air_gapped": false
}
```

> 加载顺序：全局 yaml → 租户 JSON 覆盖 → 请求 header 覆盖（`X-LLM-Force-Provider`，仅 SystemAdmin）。

### 6.2 LLMProxy 配置要点

- **`air_gapped: true` 是离网客户的硬约束**：所有 public providers 被禁用，仅 `is_local: true` 可路由。
- **`audit.log_messages: false` 默认**：合规要求 prompt 不入审计日志原文；KA 客户可白名单全文+脱敏。
- **`pricing.models` 手动维护**：每月同步官方价格，避免计费偏差。

---

## 7. Agent 配置

Agent 是端侧轻量守护进程（CPU < 3% / RSS < 80MB），**不依赖本地配置文件**。配置来源 2 段：

1. **编译时 ldflags 嵌入**（不可修改）
2. **运行时 Server 下发**（mTLS 通道）

### 7.1 编译时参数

```bash
make package-agent-all \
  SERVER_HOST=ac.mxsec.example.com:6751 \
  VERSION=v2.0.0 \
  UPDATER_CHANNEL=stable
```

| 参数 | 说明 | 默认 |
|------|------|------|
| `SERVER_HOST` | AgentCenter gRPC 入口（IP:Port 或域名:Port） | 无（必填） |
| `VERSION` | Agent 版本号 | `dev` |
| `UPDATER_CHANNEL` | 升级通道（stable / beta / canary） | `stable` |
| `BUILD_COMMIT` | 构建 commit hash | git 自动注入 |

### 7.2 运行时默认值

| 配置 | 默认 |
|------|------|
| Agent ID 文件 | `/var/lib/mxsec-agent/agent_id` |
| 工作目录 | `/var/lib/mxsec-agent/` |
| 证书目录 | `/var/lib/mxsec-agent/certs/`（Server 自动下发） |
| 插件目录 | `/var/lib/mxsec-agent/plugins/` |
| 模型目录 | `/var/lib/mxsec-agent/models/`（仅本地 ML 模型，如 LightGBM ELF） |
| 隔离区目录 | `/var/lib/mxsec-agent/quarantine/`（protect 模式病毒搬迁） |
| 日志路径 | `/var/log/mxsec-agent/agent.log` |
| 日志轮转 | 每天 1 个，保留 7 天 |
| 心跳间隔 | 60s（Server 可下发覆盖，30-300s 区间） |
| 重连退避 | 初始 1s，指数 1.5×，上限 60s |

### 7.3 Server 下发的运行时配置（Agent 内存持有）

通过 gRPC 双向流推送，Agent 收到后立即生效（无需重启）：

```yaml
connection:
  heartbeat_interval: 60s
  reconnect_initial_backoff: 1s
  reconnect_max_backoff: 60s
  rpc_timeout: 30s
  upload_compression: snappy
  upload_max_msg_bytes: 16777216
plugin:
  auto_update: true
  install_timeout: 300s
  health_check_interval: 30s
  enabled_plugins:
    - baseline
    - scanner
    - fim
    - remediation
edr:
  enabled: true
  ebpf:
    enabled: true
    fallback_to_netlink: true
    event_rate_limit_per_sec: 10000
  log_level: warn
updater:
  enabled: true
  channel: stable
  check_interval: 30m
  download_url: "https://mxsec.example.com/api/v1/agent/download"
  verify_signature: true
  pubkey: "<base64-ed25519-pubkey>"
  canary_pct_local_override: 0
cert:
  auto_rotate: true
  rotate_before_expire: 30d
mode:
  current: observe
  mode_check_interval: 30s
```

### 7.4 Agent 配置要点

- **零本地配置文件**：所有运行时参数均通过 Server 下发，禁止 SSH 改 Agent 行为。
- **`mode.current` 由 Server 控制**：Agent 不可本地切换防护模式，避免端侧绕过。
- **`updater.verify_signature: true` 是供应链安全底线**：Ed25519 校验失败拒绝升级。

---

## 8. 配置热重载与重启需求

mxsec 区分 **热重载** 与 **重启** 类配置，避免不必要的服务抖动。

### 8.1 各服务热重载支持矩阵

| 配置项 | Manager | AgentCenter | Consumer | Engine | VulnSync | LLMProxy |
|--------|---------|-------------|----------|--------|----------|----------|
| 日志级别 `log_level` | 热（SIGHUP） | 热 | 热 | 热 | 热 | 热 |
| 运行模式 `mode.*` | 热（API + DB） | — | — | 热（30s 拉取） | — | — |
| 规则 `rule.rules_path` | — | — | — | 热（30s 重载） | — | — |
| ML 模型开关 `ml.models[*].enabled` | 热 | — | — | 热（60s 拉取） | — | — |
| LLM 路由 `routing.*` | — | — | — | — | — | 热（SIGHUP） |
| LLM 配额 `quota.*` | 热 | — | — | — | — | 热 |
| 通知 `notification.*` | 热 | — | — | — | — | — |
| 数据源开关 `sources[*].enabled` | — | — | — | — | 热（DB） | — |
| 速率限制 `rate_limit.*` | 热 | — | — | — | — | — |
| 数据库 `database.*` | 重启 | — | 重启 | 重启 | 重启 | — |
| Kafka `kafka.brokers` | 重启 | 重启 | 重启 | 重启 | 重启 | 重启 |
| Redis `redis.*` | 重启 | 重启 | 重启 | 重启 | 重启 | 重启 |
| mTLS 证书 `mtls.*` | 热（证书文件 watch） | 热 | — | 热 | — | 热 |
| 端口 `*_addr` | 重启 | 重启 | 重启 | 重启 | 重启 | 重启 |
| JWT secret | 重启 | — | — | — | — | — |
| RBAC 策略 | 热（Casbin reload） | — | — | — | — | — |

### 8.2 触发热重载

| 方式 | 适用 | 命令 |
|------|------|------|
| SIGHUP | log_level / mTLS 证书 / Casbin 策略 | `kill -HUP $(pidof mxsec-manager)` |
| Manager API | mode / rule / ml / llm / notification / rate_limit | `POST /api/v2/admin/config/reload` |
| 自动轮询 | Engine 拉 Manager 配置 / Agent 拉 Server 模式 | 默认 30-60s |
| Watch 文件 | 规则文件 / 模型文件 / 证书 | inotify 实时 |

---

## 9. 环境变量覆盖

所有 yaml 项均可通过环境变量覆盖，规则：

```
yaml 路径 a.b.c  →  环境变量 MXSEC_A__B__C
```

示例：

```bash
export MXSEC_MODE__DEFAULT=observe
export MXSEC_ML__ENABLED=true
export MXSEC_QUOTA__DEFAULT_PER_TENANT__MONTHLY_USD=200.0
```

### 9.1 强制环境变量（不可写入 yaml）

| 变量 | 用途 | 服务 |
|------|------|------|
| `JWT_SECRET` | JWT 签名密钥 | Manager |
| `MYSQL_PASSWORD` | MySQL 密码 | Manager / Consumer / VulnSync |
| `MYSQL_RO_PASSWORD` | MySQL 只读密码 | Engine |
| `CLICKHOUSE_PASSWORD` | ClickHouse 密码 | Consumer |
| `REDIS_PASSWORD` | Redis 密码 | All |
| `MXSEC_INTERNAL_BEARER` | 内部 gRPC Bearer Token | All |
| `MXSEC_LLM_INTERNAL_TOKEN` | LLMProxy 调用 Token | Engine / Manager |
| `MXSEC_AC_INTERNAL_TOKEN` | AgentCenter 内部 Token | Engine / Manager |
| `OPENAI_API_KEY` | OpenAI Key | LLMProxy |
| `ANTHROPIC_API_KEY` | Anthropic Key | LLMProxy |
| `GEMINI_API_KEY` | Google Gemini Key | LLMProxy |
| `DASHSCOPE_API_KEY` | 阿里千问 Key | LLMProxy |
| `DEEPSEEK_API_KEY` | DeepSeek Key | LLMProxy |
| `KIMI_API_KEY` | Kimi Key | LLMProxy |
| `GLM_API_KEY` | 智谱 Key | LLMProxy |
| `NVD_API_KEY` | NVD API Key（推荐配） | VulnSync |
| `SMTP_PASSWORD` | 邮件 SMTP 密码 | Manager |
| `SMS_ACCESS_KEY` / `SMS_SECRET_KEY` | 短信 | Manager |
| `NOTIFY_WEBHOOK_URL` / `NOTIFY_WEBHOOK_SIGN` | Webhook | Manager |
| `SLACK_WEBHOOK_URL` | Slack | Manager |
| `VULNSYNC_HTTP_PROXY` | VulnSync 出网代理 | VulnSync |

---

## 10. 配置验证 (`mxctl config validate`)

`mxctl` 提供配置验证子命令，**部署前强制运行**。

```bash
mxctl config validate --service manager --file /etc/mxsec/manager.yaml
mxctl config validate --all --dir /etc/mxsec/
mxctl config validate --all --strict
mxctl config validate --all --format json
```

### 10.1 校验内容

| 类别 | 规则 |
|------|------|
| **必填项** | DB host / Kafka brokers / mTLS 证书路径 / JWT secret 等 |
| **类型 / 范围** | 端口 1-65535 / 超时 > 0 / 副本 ≥ 1 |
| **依赖** | `mtls.cert` 存在则 `mtls.key` 也必须存在 |
| **互斥** | `ml.enabled=false` 时 `ml.models[*].enabled` 警告 |
| **多租户** | `tenants.guard.refuse_missing_tenant_id` 必须 `true`（lint 强制） |
| **模式** | `mode.default = protect` 时打 warning（建议从 observe 起步） |
| **机密** | 检测 yaml 中是否有看似 password / api_key 的明文 → 拒绝 |
| **DataType** | 引用的 DataType 必须在 [`datatype-allocation.md`](datatype-allocation.md) 注册 |
| **网络** | 可选 `--probe` 探活 DB / Kafka / Redis |
| **证书** | TTL 剩余 < 30d 警告，过期或域名不匹配拒绝 |
| **租户隔离** | `isolation_strategy=db` 时 `isolated_db_dsn` 必填 |

### 10.2 退出码

| 码 | 含义 |
|----|------|
| 0 | 全部通过 |
| 1 | 有 error |
| 2 | 仅 warning（非 strict 模式不算失败） |
| 3 | 文件不存在 / IO 错误 |

---

## 11. 多租户配置层次（实操示例）

以一个银行 KA 客户 `t-bank-a` 为例，展示 **全局 < 租户 < 主机标签 < 规则** 4 级覆盖如何落地。

### 11.1 全局默认（manager.yaml）

```yaml
mode:
  default: observe
ml:
  enabled: true
llm:
  enabled: false
retention:
  alerts_days: 90
```

### 11.2 租户级覆盖（tenants 表）

```sql
UPDATE tenants
SET default_mode = 'observe',
    ml_enabled = true,
    llm_enabled = true,                      -- 银行 A 开 LLM（离网本地 Qwen）
    llm_provider = '{"air_gapped": true, ...}',
    retention_alerts_days = 365,             -- 银行合规要求 1 年
    isolation_strategy = 'db',               -- 独立 DB 实例
    isolated_db_dsn = 'mxsec_user:***@tcp(mysql-bank-a:3306)/mxsec_t_bank_a'
WHERE id = 't-bank-a';
```

### 11.3 主机标签级覆盖（engine 内存）

```yaml
host_label_overrides:
  - label: role=database
    mode: observe
    disable_models: [securitybert-cmdline]
  - label: env=staging
    mode: protect
```

### 11.4 规则级覆盖（engine_rules 表）

```sql
UPDATE engine_rules
SET mode_override = 'protect',
    tenant_filter = 't-bank-a',
    host_label_filter = 'env!=prod-core'
WHERE rule_id = 'BRUTE_FORCE_SSH';
```

### 11.5 最终决策（运行时）

某事件触发 `BRUTE_FORCE_SSH` 规则：

```
事件 host_id=h-12345 (tenant=t-bank-a, labels={role=app, env=staging})
   ↓
查规则级 override: rule=protect (tenant_filter 命中, host_label_filter 命中)
   ↓
查主机标签 override: env=staging → protect
   ↓
查租户级 mode: observe (银行 A 全局监听)
   ↓
查全局默认: observe
   ↓
最终决策: protect（规则级最高优先级）
```

详细优先级规则见 [`operating-modes.md`](operating-modes.md) §4 与 [`multi-tenant.md`](multi-tenant.md) §7。

---

## 12. 安全配置最佳实践

### 12.1 Secret 管理（强约束）

| 反模式 | 推荐做法 |
|--------|---------|
| `api_key: "sk-xxx"` 写入 yaml | `api_key: ${OPENAI_API_KEY}` + K8s Secret / Vault 注入 |
| 密码硬编码 docker-compose | `.env` 文件 + `.gitignore` + 部署机 KMS |
| JWT secret 默认值 | 部署前强制生成 32+ 字节高熵随机 (`openssl rand -base64 48`) |
| mTLS 私钥入仓 | CA + 证书生成走 `scripts/generate-certs.sh`，私钥永不入仓 |
| 共享 `X-Internal-Secret` | v2.0 升级到 mTLS + Bearer Token，原方案仅过渡 |

### 12.2 KMS / Vault 集成模式

```yaml
jwt:
  secret: ${vault:secret/mxsec/jwt#secret}     # vault-agent 渲染
database:
  mysql:
    password: ${aws-secretsmanager:mxsec-prod-mysql:password}
```

启动器（systemd / k8s sidecar）负责将 `${vault:...}` / `${aws-secretsmanager:...}` 解析为环境变量再 exec mxsec 进程。

### 12.3 mTLS 证书生命周期

| 阶段 | 操作 | 工具 |
|------|------|------|
| 初始化 | 生成 CA + 各服务证书 | `scripts/generate-certs.sh` |
| 部署 | 私钥仅文件系统 0600 权限 | systemd `ProtectSystem=strict` |
| 轮换 | 30 天前自动重签 | `mxctl cert rotate` |
| 吊销 | CRL + OCSP（可选） | `mxctl cert revoke` |
| 监控 | TTL < 30d 告警 | Prometheus `mxsec_cert_expire_seconds` |

### 12.4 网络分层（推荐）

```
公网 → Nginx (TLS 终结 + WAF)
     → Manager / LLMProxy (业务面 / AI 面)
     → Engine / Consumer / VulnSync (内部，不暴露公网)
     → MySQL / Redis / ClickHouse / Kafka (数据面，仅内网)
Agent → AgentCenter (mTLS 6751，可专线 / VPN)
```

### 12.5 配置文件权限

```bash
chown root:mxsec /etc/mxsec/*.yaml
chmod 0640 /etc/mxsec/*.yaml          # mxsec 组只读
chmod 0600 /etc/mxsec/certs/*.key      # 私钥仅 root
```

### 12.6 审计与不可篡改

- 所有配置变更走 API（`/api/v2/admin/config/*`），落 `audit_log` 表
- `audit_log` 保留 ≥ 180 天，KA 合规要求 1 年
- 关键操作（mode 切换 / 租户隔离策略变更 / LLM provider 变更）双人审批
- 审计日志可外发到 SIEM（syslog / webhook）

---

## 13. 默认值清单（Smart 档汇总）

下表汇总 mxsec 在**默认 Smart 档**（`ml=on`, `llm=off`, `mode=observe`）下各服务的关键默认值。一份新部署只需修改 Secret + 端点地址即可上线。

| 服务 | 配置项 → 默认值 |
|------|----------------|
| Manager | `mode.default=observe` / `ml.enabled=true` / `llm.enabled=false` / `retention.alerts_days=90` / `retention.events_days=30` / `retention.audit_days=180` / `jwt.access_token_ttl=2h` / `jwt.refresh_token_ttl=30d` / `rbac.default_role=viewer` / `tenants.guard.refuse_missing_tenant_id=true` / `tenants.guard.panic_on_tenant_scope_missing=true` / `rate_limit.per_tenant.qps=500` / `notification.throttle.per_rule_per_host=300s/5` |
| AgentCenter | `mtls.client_auth=VerifyClientCertIfGiven` / `mtls.auto_issue.enabled=true` / `mtls.auto_issue.cert_ttl=365d` / `grpc_server.keepalive=(60s/10s/10s)` / `kafka.fallback_queue.capacity=10000` / `kafka.fallback_queue.ttl=5m` / `scheduler.heartbeat.timeout=180s` / `scheduler.canary.default_steps=[1,5,25,100]` |
| Consumer | `mysql.upsert_strategy=on_duplicate_key` / `clickhouse.batch.size=5000` / `clickhouse.batch.flush_interval=10s` / `dlq.retry_max=3` / `sanitize.enabled=true` / `worker.per_topic_concurrency.mxsec.agent.ebpf=16` |
| Engine | `mode.global_default=observe` / `mode.fail_safe_on_manager_down=observe` / `rule.reload_interval_sec=30` / `rule.cel_program_cache=4096` / `rule.throttle=(60s/1)` / `ml.enabled=true` / `ml.runtime=onnx` / `ml.inference_timeout_ms=50` / `ml.inference_concurrency=100` / `ml.embedding_cache_ttl=24h` / `ml.fallback_on_runtime_error=true` / `ml.canary.candidate_traffic_pct=5` / `ml.models.securitybert-cmdline.enabled=false` / `storyline.flush_interval_sec=5` / `storyline.story_idle_close_sec=1800` / `k8s.admission_dryrun_observe=true` / `llm.enabled=false` / `llm.fail_open=true` / `tenant_isolation.refuse_missing_tenant_id=true` |
| VulnSync | `leader_election.lock_ttl=30m` / `leader_election.heartbeat_interval=10m` / `schedule.incremental_cron=0 0 * * * *` / `schedule.full_cron=0 30 3 * * *` / `schedule.max_parallel_sources=4` / `merge_strategy.primary_index=purl` / `merge_strategy.confidence_levels.high.sources_required=2` / `merge_strategy.validate.drop_if_invalid=true` / `kafka.acks=all` / `kafka.compression=snappy` / `kafka.max_message_bytes=10485760` / `circuit_breaker=(5/5m/per_source)` |
| LLMProxy | `air_gapped=false` / `auto_downgrade_on_offline=true` / `cache.enabled=true` / `cache.ttl=24h` / `sanitize.enabled=true` / `routing.alert_explain.primary=openai/gpt-4o-mini` / `routing.storyline_summary.primary=anthropic/claude-3-5-sonnet` / `routing.nl2query.primary=deepseek/deepseek-chat` / `routing.rule_draft.primary=deepseek/deepseek-reasoner` / `routing.rule_draft.cache=false` / `blacklist=(3/5m)` / `quota.default_per_tenant.monthly_usd=100.0` / `quota.default_per_tenant.qps=5` / `quota.default_per_tenant.threshold_warn=0.8` / `quota.default_per_tenant.threshold_block=1.0` / `quota.global.monthly_usd=5000.0` / `audit.enabled=true` / `audit.log_messages=false` |
| Agent | `connection.heartbeat_interval=60s` / `connection.upload_compression=snappy` / `plugin.auto_update=true` / `plugin.health_check_interval=30s` / `edr.enabled=true` / `edr.ebpf.enabled=true` / `edr.ebpf.fallback_to_netlink=true` / `edr.ebpf.event_rate_limit_per_sec=10000` / `updater.channel=stable` / `updater.check_interval=30m` / `updater.verify_signature=true` / `cert.auto_rotate=true` / `cert.rotate_before_expire=30d` / `mode.current=observe` / `mode.mode_check_interval=30s` |

---

## 14. 与对标产品对比

| 维度 | mxsec | 青藤万象 | 青藤蜂巢 |
|------|-------|---------|---------|
| 默认运行模式 | `observe`（监听优先，硬编码） | 检测 + 部分自动响应（出厂） | 同上 |
| 多租户 | from-day-1（`tenants.guard.refuse_missing_tenant_id`） | 商业版有，社区版无 | 同上 |
| 本地 ML 配置粒度 | 模型级 enable + weight + threshold | 黑盒 | 黑盒 |
| LLM 厂商适配 | 9 厂商 + Ollama / vLLM（统一 yaml） | 单一厂商 | 单一厂商 |
| 离网模式 | `air_gapped: true` 一键禁公网 | 商业版 | 商业版 |
| 配置热重载 | mode / rule / ml / llm 全部热加载 | 部分 | 部分 |
| 配置验证 | `mxctl config validate` 强制 CI | 商业版控制台 | 商业版控制台 |
| 4 级覆盖 | 全局 < 租户 < 主机标签 < 规则 | 租户 + 策略组 | 集群 + 策略组 |
| Secret 管理 | 环境变量 + KMS / Vault | 控制台输入 | 控制台输入 |

---

## 15. 参考文档

| 主题 | 文档 |
|------|------|
| 架构总图 | [`architecture.md`](architecture.md) |
| 运行模式（监听 / 防护） | [`operating-modes.md`](operating-modes.md) |
| 多租户设计 | [`multi-tenant.md`](multi-tenant.md) |
| Engine 服务设计 | [`engine-design.md`](engine-design.md) |
| VulnSync 服务设计 | [`vulnsync-design.md`](vulnsync-design.md) |
| LLMProxy 设计 | [`llmproxy-design.md`](llmproxy-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 部署指南 | [`deployment.md`](deployment.md) |
| 路线图（内部） | `ref/08-roadmap.md` |
| 服务端架构评估（内部） | `ref/01-服务端架构.md` |
