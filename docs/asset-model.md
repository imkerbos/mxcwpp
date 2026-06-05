# 资产统一模型（11 维资产 + 关系图谱 + 流量拓扑）

> **定位**：mxsec 三大产品目标的第一目标 —— **"知道是什么"** 的具体落地。
>
> mxsec 是工业级开源 CWPP，专精 **Linux 主机 + Kubernetes 容器**，本文档定义**统一资产模型**：
> - 11 维资产清点（主机 / 进程 / 端口 / 账户 / 应用 / 容器 / K8s / 镜像 / 网络 / 南北向流量 / 东西向流量）
> - 跨维度关系图谱（默认 MySQL + 冗余字段，可选 Neo4j / Dgraph）
> - 流量分析模块（eBPF network event 聚合 → 拓扑可视化）
> - 态势感知 dashboard 四视角（资产 / 风险 / 攻击 / 流量）
> - 多租户 `tenant_id` 强行隔离贯穿
>
> **强约束**：
> 1. 不做 Windows / macOS 资产；
> 2. 资产采集永远受**监听模式**保护（不阻断业务），见 [`operating-modes.md`](operating-modes.md) §8.2；
> 3. 所有资产实体表必带 `tenant_id` + `host_id`（或 `cluster_id`），见 [`multi-tenant.md`](multi-tenant.md) §3.1；
> 4. 资产事件落 Kafka `mxsec.agent.asset`（DataType 5050–5060），写入由 Consumer 完成，分析交给 Engine。

---

## 1. 设计目标与原则

### 1.1 三大目标中"知道是什么"的落地

| 阶段 | 目标 | 资产模型角色 |
|------|------|--------------|
| **看清** | 全量、实时、可视化、可溯源 | **本文档** —— 提供统一的资产单一事实源（Single Source of Truth） |
| **算清** | 资产 × 漏洞 × 配置 × 攻击 联立分析 | 资产是漏洞匹配的 NEVRA/PURL 锚点，详见 [`vuln-module-design.md`](vuln-module-design.md) |
| **处清** | 修复闭环（防护模式才动手） | 资产是修复任务的目标，详见 [`security-objectives.md`](security-objectives.md) |

### 1.2 五条硬约束

1. **统一标识**：跨维度引用必须使用全局唯一 ID（`host_id` / `cluster_id` / `container_id` / `image_id` / `pod_uid`），禁止用 hostname / IP 等可变字段。
2. **租户硬隔离**：每条资产记录必带 `tenant_id`，索引前缀强制 `(tenant_id, ...)`；跨租户查询走 `/api/v2/admin/*` 白名单。
3. **采集与分析解耦**：Agent 只采，Consumer 只写，Engine 只算。三者通过 Kafka 异步串联，详见 [`architecture.md`](architecture.md) §3。
4. **去重指纹优先于自增 ID**：所有资产生成稳定的 `fingerprint`，Upsert 用 `(tenant_id, host_id, fingerprint)` 三元组幂等。
5. **关系冗余 + 主表落库**：默认 MySQL + 冗余外键字段；上规模（10W+ 资产）再上图库，本文档给出评估矩阵。

### 1.3 与对标产品对照

| 维度 | 青藤万象 | 青藤蜂巢 | mxsec（本文档） |
|------|----------|----------|-----------------|
| 主机资产清点 | 22 类 + 800 业务应用识别 | — | 11 类大维度 + 800 业务指纹（路线图，依赖 Wappalyzer/CPE 指纹库增量积累） |
| K8s 资产 | — | 15 类（集群/Pod/Workload/Service/Ingress/...） | 7 类（集群/节点/Pod/容器/Service/Ingress/RBAC） |
| 镜像资产 | — | 5w+ 漏洞库 + Dockerfile + License + 敏感信息 | Trivy CLI 接入 + 镜像分层 SBOM + 基础镜像识别 |
| 关系图谱 | 资产-关系-活动-操作（slide19）四元组 | 业务视角网络拓扑（零域） | 7 节点 × 11 关系类型 MySQL 邻接表 + 可选图库 |
| 流量分析 | — | 微隔离零域（自动学习东西向） | eBPF connect/accept 事件聚合 + 拓扑可视化（无微隔离能力） |
| 默认存储 | 自研 | 自研 | MySQL 8 + ClickHouse（事件） |

---

## 2. 总体架构

### 2.1 采集 → 持久化 → 分析 链路

```
┌─────────────────────────────────────────────────────────────────────────┐
│                       Agent 数据面（Linux / K8s Node）                  │
│                                                                         │
│  ┌──────────────────┐   ┌──────────────────┐   ┌────────────────────┐  │
│  │ collector 插件   │   │ EDR 内核事件      │   │ kube probe（DS）   │  │
│  │ 全量周期清点     │   │ eBPF connect/exec │   │ informer 增量      │  │
│  │ (5050–5060)      │   │ (3000–3002)       │   │ (5070–5079)        │  │
│  └────────┬─────────┘   └────────┬─────────┘   └─────────┬──────────┘  │
│           │ Protobuf + Pipe IPC                          │              │
│           └─────────────────────┬────────────────────────┘              │
│                                 │ gRPC BiDi + mTLS + Snappy             │
└─────────────────────────────────┼──────────────────────────────────────-┘
                                  ▼
┌─────────────────────────────────────────────────────────────────────────┐
│   AgentCenter（纯转发）  ─→  Kafka mxsec.agent.asset / mxsec.agent.ebpf │
└─────────────────────────────────┬──────────────────────────────────────-┘
                                  ▼
       ┌──────────────────────────┴──────────────────────────────┐
       │                                                          │
       ▼                                                          ▼
 ┌──────────────────┐                              ┌────────────────────────┐
 │ Consumer ×N       │                              │ Engine ×N              │
 │ ─ 幂等写入        │                              │ ─ 资产风险关联         │
 │ ─ 维护 fingerprint│                              │ ─ 攻击面计算（暴露     │
 │ ─ 维护 relations  │                              │   面、东西向异常）     │
 │ ─ 写 MySQL/CK     │                              │ ─ 产出 alert / story   │
 └────────┬──────────┘                              └─────────┬──────────────┘
          │                                                    │
          ▼                                                    ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │   MySQL（主数据）  + ClickHouse（事件归档 + 拓扑流水）+ Redis（缓存）  │
 └────────────────────────────────────────────────────────────────────────┘
```

### 2.2 与现有源码的关系

| 现有组件 | 现状 | 在新模型中的角色 |
|----------|------|------------------|
| `plugins/collector/` | 11 个 handler（process/port/user/software/container/app/network/volume/kmod/service/cron） | **继续作为主机侧资产采集器**；新增 11 维资产的"主机内部分"全部落在此 |
| `plugins/collector/engine/handlers/container.go` + `container_sbom.go` | 已采容器列表 + 容器内 SBOM | **直接复用**作为容器资产采集源；K8s 元数据由 DaemonSet probe 富化 |
| `internal/server/manager/biz/kube_*.go` | 16 个 K8s 业务文件，包括 audit / detector / sync / baseline | **拆分**：资产采集从 manager 搬到 Engine（kube probe + informer），manager 仅做 CRUD |
| `internal/server/manager/biz/kube_client.go` | KubeClientManager + NodeInfo/PodInfo/WorkloadInfo | **转为标准模型**：NodeInfo → `kube_nodes` 表，PodInfo → `kube_pods` 表（带 fingerprint） |
| `internal/server/model/host.go` 等 | 单一 `Host` 表已覆盖 OS/CPU/Mem/Disk/Network | **扩展**：补 `tenant_id`、`exposure_score`、`asset_criticality` 字段 |
| `image_scanner.go`（Trivy 包装） | 已能扫镜像 + Registry | **保留**作为镜像资产 SBOM 提取器，新增 `image_layers` 表存分层信息 |

---

## 3. 11 维资产模型

每个维度按 **(1) 字段定义 → (2) 采集方式 → (3) 更新频次 → (4) 关联关系** 四段展开。

### 3.1 主机资产（Host）

#### 字段定义

```sql
CREATE TABLE hosts (
    host_id              VARCHAR(64)  PRIMARY KEY,
    tenant_id            VARCHAR(64)  NOT NULL,
    hostname             VARCHAR(255) NOT NULL,
    os_family            VARCHAR(50)  NOT NULL,       -- centos / ubuntu / openeuler / kylin / ...
    os_version           VARCHAR(50)  NOT NULL,
    kernel_version       VARCHAR(100) NOT NULL,
    arch                 VARCHAR(20)  NOT NULL,       -- x86_64 / aarch64 / loongarch64 / sw_64
    cpu_model            VARCHAR(255),
    cpu_cores            INT,
    memory_total_mb      BIGINT,
    disk_total_gb        BIGINT,
    boot_id              VARCHAR(64),                 -- /proc/sys/kernel/random/boot_id（重启检测）
    machine_id           VARCHAR(64),                 -- /etc/machine-id（迁移检测）
    ipv4_list            JSON,                        -- 内网 IPv4
    ipv6_list            JSON,
    public_ipv4          JSON,
    public_ipv6          JSON,
    default_gateway      VARCHAR(45),
    runtime_type         ENUM('vm','docker','k8s') NOT NULL DEFAULT 'vm',
    pod_uid              VARCHAR(64),                 -- runtime_type=k8s 时填
    cluster_id           BIGINT,                      -- runtime_type=k8s 时填
    business_line        VARCHAR(100),                -- 业务线（手工 / 自动标签）
    tags                 JSON,                        -- 用户自定义标签数组
    asset_criticality    ENUM('low','medium','high','critical') DEFAULT 'medium',
    exposure_score       INT,                         -- 0-100，引擎计算
    kernel_livepatch_enabled BOOLEAN DEFAULT FALSE,
    status               ENUM('online','offline') DEFAULT 'offline',
    last_heartbeat       TIMESTAMP,
    fingerprint          VARCHAR(64) NOT NULL,        -- SHA256(machine_id|boot_id|primary_mac)
    created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_tenant_status (tenant_id, status),
    INDEX idx_tenant_runtime (tenant_id, runtime_type),
    INDEX idx_tenant_business (tenant_id, business_line),
    INDEX idx_tenant_cluster (tenant_id, cluster_id),
    UNIQUE KEY uk_tenant_fingerprint (tenant_id, fingerprint)
);
```

> 现有 `internal/server/model/host.go` 已覆盖大部分字段（含 `KernelLivepatchEnabled` / `RuntimeType` / `BusinessLine`）。本模型在其基础上**新增** `tenant_id` / `boot_id` / `machine_id` / `fingerprint` / `asset_criticality` / `exposure_score` / `cluster_id` 七字段。

#### 采集方式

| 字段族 | 采集 | 源 |
|--------|------|-----|
| OS / Kernel / Arch | `uname -a` + `/etc/os-release` | Agent 启动 + 周期 1h 上报 |
| CPU / Memory | `/proc/cpuinfo` + `/proc/meminfo` | 周期 1h |
| Disk | `statfs` + `/proc/mounts` | 周期 30min（与 volume 维度联立） |
| 网卡 IP | `netlink RTM_GETADDR` | 启动 + 网卡 UP/DOWN 事件触发 |
| 公网 IP | 出站到云元数据 / NAT 探测 | 周期 1h |
| boot_id / machine_id | `/proc/sys/kernel/random/boot_id` / `/etc/machine-id` | 启动一次 + 心跳确认 |

#### 更新频次

- **心跳**：60s（仅 `status` / `last_heartbeat`）
- **全量上报**：启动一次 + 周期 1h（DataType `5050` 系列汇总到 `mxsec.agent.asset`）
- **重启检测**：`boot_id` 变化 → Engine 产 audit + 重置部分缓存

#### 关联关系

```
hosts ─1:N─→ processes / ports / asset_users / cron / services / kmod / volumes / apps / containers / network_interfaces
hosts ─N:1─→ kube_clusters（runtime_type=k8s 时）
hosts ─1:N─→ host_vulnerabilities（漏洞匹配产物，见 vuln-module-design.md）
```

---

### 3.2 进程资产（Process + Service + Cron + 启动项）

#### 字段定义

进程快照 `processes`（继承自现有 `internal/server/model/process.go`，补 `tenant_id` / `fingerprint` / `start_time` / `cwd` / `caps` / `effective_caps`）：

```sql
CREATE TABLE processes (
    id           VARCHAR(128) PRIMARY KEY,                          -- SHA1(host_id|pid|start_time)
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    pid          INT NOT NULL,
    ppid         INT,
    cmdline      TEXT,
    exe          VARCHAR(512),
    exe_hash     VARCHAR(64),                                       -- SHA256
    cwd          VARCHAR(512),
    container_id VARCHAR(64),
    uid          INT,
    gid          INT,
    username     VARCHAR(100),
    groupname    VARCHAR(100),
    start_time   TIMESTAMP,
    caps_effective BIGINT UNSIGNED,                                 -- capabilities bitmap
    nspid        INT,                                               -- namespace 内 PID（容器场景）
    pns          BIGINT,                                            -- PID namespace inode
    fingerprint  VARCHAR(64),                                       -- SHA256(exe_hash|cmdline) 用于"持久进程指纹"
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host (tenant_id, host_id),
    INDEX idx_tenant_container (tenant_id, container_id),
    INDEX idx_tenant_exe_hash (tenant_id, exe_hash)
);
```

系统服务 `services`（systemd / sysv，已存在）：`service_name` / `service_type` / `status` / `enabled` / `description`。

定时任务 `crons`（crontab + systemd timer，已存在）：`user` / `schedule` / `command` / `cron_type` / `enabled`。

启动项 `boot_items`（**新增**）：

```sql
CREATE TABLE boot_items (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    kind         ENUM('systemd_unit','init_d','rc_local','profile','xinetd') NOT NULL,
    name         VARCHAR(255) NOT NULL,
    target       VARCHAR(512),                                      -- ExecStart / script path
    enabled      BOOLEAN,
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host_kind (tenant_id, host_id, kind)
);
```

#### 采集方式

| 资产 | 采集器 | 备注 |
|------|--------|------|
| processes | `plugins/collector/engine/handlers/process.go` + EDR `exec` 事件实时补全 | collector 每 60s 全量快照 + EDR 实时增量 |
| services | `plugins/collector/engine/handlers/service.go`（systemd-d-bus / sysv 读 `/etc/init.d`） | 周期 5min |
| crons | `plugins/collector/engine/handlers/cron.go`（crontab + systemd timer） | 周期 5min |
| boot_items | **新增 handler**：解析 `/etc/systemd/system/*.target.wants/`、`/etc/init.d/`、`/etc/rc.local`、`/etc/profile.d/*.sh` | 周期 1h |

#### 更新频次

- 全量快照：60s（process）/ 5min（service/cron）/ 1h（boot_items）
- EDR 增量：实时 exec 事件（`mxsec.agent.ebpf`，DataType 3001）

#### 关联关系

```
processes ─N:1─→ hosts
processes ─N:1─→ containers（container_id 非空时）
processes ─1:N─→ ports（监听端口属于哪个进程，通过 PID 关联）
processes ─1:N─→ apps（中间件应用通过 process_id 关联）
processes ─N:1─→ asset_users（按 uid → username）
services / crons / boot_items ─N:1─→ hosts
```

---

### 3.3 端口资产（Port）

#### 字段定义

监听端口 `ports`（继承现有，补 `tenant_id` / `bind_addr` / `fingerprint`）：

```sql
CREATE TABLE ports (
    id            VARCHAR(128) PRIMARY KEY,
    tenant_id     VARCHAR(64) NOT NULL,
    host_id       VARCHAR(64) NOT NULL,
    protocol      VARCHAR(10) NOT NULL,        -- tcp / tcp6 / udp / udp6
    bind_addr     VARCHAR(45),                 -- 0.0.0.0 / 127.0.0.1 / ::1 / 具体 IP
    port          INT NOT NULL,
    state         VARCHAR(20),                 -- LISTEN
    pid           INT,
    process_name  VARCHAR(255),
    container_id  VARCHAR(64),
    exposure      ENUM('localhost','internal','public','unknown') DEFAULT 'unknown',
    banner        VARCHAR(255),                -- 协议指纹首字节
    protocol_hint VARCHAR(50),                 -- http / ssh / mysql / redis / ...
    fingerprint   VARCHAR(64),                 -- SHA1(host|proto|bind|port)
    collected_at  TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_fp (tenant_id, fingerprint),
    INDEX idx_tenant_host (tenant_id, host_id),
    INDEX idx_tenant_exposure (tenant_id, exposure)
);
```

#### 采集方式

- `plugins/collector/engine/handlers/port.go`：解析 `/proc/net/{tcp,tcp6,udp,udp6}` + 关联 `/proc/<pid>/fd/` socket inode
- 协议指纹（`banner` / `protocol_hint`）：仅对 `bind_addr = 0.0.0.0 / ::` 的端口做一次 TCP probe（5s timeout、读前 64 字节），避免扫所有端口
- 暴露面判定（`exposure`）：
  - `bind=127.0.0.1` → `localhost`
  - `bind=0.0.0.0` 且主机公网 IPv4 非空 → `public`
  - 其他 → `internal`

#### 更新频次

- 周期 60s 全量监听端口快照
- EDR `bind/listen` 系统调用事件实时补全

#### 关联关系

```
ports ─N:1─→ hosts
ports ─N:1─→ processes (host_id + pid)
ports ─N:1─→ containers (container_id 非空时)
ports ─1:N─→ traffic_north_south（南北向流量按 dst_port 关联）
```

---

### 3.4 账户资产（User + Sudo + SSH key + PAM）

#### 字段定义

```sql
-- 系统账户（继承现有 asset_users，补 tenant_id / 风险字段）
ALTER TABLE asset_users
    ADD COLUMN tenant_id VARCHAR(64) NOT NULL AFTER id,
    ADD COLUMN last_login TIMESTAMP NULL,        -- /var/log/lastlog
    ADD COLUMN sudo_nopasswd BOOLEAN DEFAULT FALSE,
    ADD COLUMN ssh_authorized_keys_count INT DEFAULT 0,
    ADD COLUMN password_locked BOOLEAN DEFAULT FALSE,  -- /etc/shadow ! 或 *
    ADD COLUMN password_max_days INT,             -- shadow 字段
    ADD INDEX idx_tenant_host (tenant_id, host_id);

-- SSH 公钥（新增，逐 key 一行）
CREATE TABLE host_ssh_keys (
    id            VARCHAR(128) PRIMARY KEY,
    tenant_id     VARCHAR(64) NOT NULL,
    host_id       VARCHAR(64) NOT NULL,
    username      VARCHAR(100) NOT NULL,
    key_type      VARCHAR(20),                  -- ssh-rsa / ed25519 / ecdsa-sha2-nistp256
    key_bits      INT,
    key_fingerprint VARCHAR(64) NOT NULL,        -- SHA256(base64-decoded key)
    comment       VARCHAR(255),
    source_file   VARCHAR(255),                 -- ~/.ssh/authorized_keys 路径
    collected_at  TIMESTAMP NOT NULL,
    INDEX idx_tenant_host_user (tenant_id, host_id, username),
    INDEX idx_tenant_fp (tenant_id, key_fingerprint)  -- 跨主机同 key 检测
);

-- sudoers 条目（新增）
CREATE TABLE host_sudo_rules (
    id          VARCHAR(128) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    host_id     VARCHAR(64) NOT NULL,
    principal   VARCHAR(255) NOT NULL,         -- 用户 / %组 / +netgroup
    host_spec   VARCHAR(255),
    runas_spec  VARCHAR(255),
    cmd_spec    TEXT,
    tags        VARCHAR(64),                   -- NOPASSWD / SETENV / ...
    source_file VARCHAR(255),                  -- /etc/sudoers / /etc/sudoers.d/*
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host (tenant_id, host_id)
);

-- PAM 配置摘要（新增）
CREATE TABLE host_pam_modules (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    service      VARCHAR(50),                  -- sshd / login / su
    type         VARCHAR(20),                  -- auth / account / session / password
    control      VARCHAR(20),                  -- required / requisite / sufficient
    module_path  VARCHAR(255),
    arguments    VARCHAR(512),
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host_service (tenant_id, host_id, service)
);
```

#### 采集方式

- `asset_users`：`/etc/passwd` + `/etc/group` + `/etc/shadow`（agent 已 root 权限） + `lastlog`
- `host_ssh_keys`：遍历 `/home/*/.ssh/authorized_keys` + `/root/.ssh/authorized_keys`，解析公钥指纹
- `host_sudo_rules`：解析 `/etc/sudoers` + `/etc/sudoers.d/*`（避免 `sudo -ll` 误触发审计）
- `host_pam_modules`：解析 `/etc/pam.d/*` 关键服务

#### 更新频次

- 全量上报：30min（账户类相对稳定）
- 触发上报：FIM 监控 `/etc/passwd` / `/etc/shadow` / `~/.ssh/authorized_keys` 变化时立即触发

#### 关联关系

```
asset_users ─N:1─→ hosts
host_ssh_keys ─N:1─→ asset_users (host_id + username)
host_ssh_keys 跨主机同 key_fingerprint → 用于检测"同一密钥进多机"风险
host_sudo_rules ─N:1─→ hosts；按 principal 关联 asset_users
```

---

### 3.5 应用资产（中间件 / DB / Web 框架 / JAR / SBOM）

#### 字段定义

软件包资产 `software`（继承现有 `internal/server/model/software.go`，补 `tenant_id`、`purl`、`scope`、`source_handler`）：

```sql
CREATE TABLE software (
    id              VARCHAR(128) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    host_id         VARCHAR(64) NOT NULL,
    container_id    VARCHAR(64),                          -- 容器内包此字段非空
    name            VARCHAR(255) NOT NULL,
    version         VARCHAR(100) NOT NULL,
    epoch           VARCHAR(20),
    release         VARCHAR(100),
    arch            VARCHAR(20),
    package_type    VARCHAR(20) NOT NULL,                  -- rpm / deb / pip / npm / jar / go-module / binary
    vendor          VARCHAR(100),
    install_time    VARCHAR(50),
    purl            VARCHAR(512),                         -- pkg:rpm/redhat/openssl@1.0.2k-25.el7
    scope           ENUM('system','embedded','container') NOT NULL DEFAULT 'system',
    source_handler  VARCHAR(50),                          -- rpm / dpkg / binary_probe / jar_scanner / ...
    host_binary_path VARCHAR(512),                        -- scope=embedded 时记录宿主 binary
    fingerprint     VARCHAR(64),                          -- SHA256(name|version|arch|scope)
    collected_at    TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_host_fp (tenant_id, host_id, container_id, fingerprint),
    INDEX idx_tenant_purl (tenant_id, purl),
    INDEX idx_tenant_host_type (tenant_id, host_id, package_type)
);
```

中间件 / DB 应用 `apps`（继承现有 `internal/server/model/app.go`）：

```sql
ALTER TABLE apps
    ADD COLUMN tenant_id VARCHAR(64) NOT NULL AFTER id,
    ADD COLUMN container_id VARCHAR(64) AFTER process_id,
    ADD COLUMN fingerprint VARCHAR(64),
    ADD INDEX idx_tenant_host_type (tenant_id, host_id, app_type);
```

`app_type` 取值（**白皮书 800+ 业务识别的工程化路径**）：
- 数据库：`mysql` / `postgresql` / `redis` / `mongodb` / `tidb` / `clickhouse` / `elasticsearch` / `oracle` / `sqlserver`
- 中间件：`nginx` / `apache` / `tomcat` / `kafka` / `zookeeper` / `rabbitmq` / `etcd` / `consul`
- Web 框架（容器内 SBOM 衍生）：`spring-boot` / `django` / `flask` / `express` / `gin`
- 运行时：`jdk` / `nodejs` / `python` / `golang`
- 大数据：`hadoop` / `spark` / `flink` / `hive`

#### 采集方式

| 来源 | handler | 说明 |
|------|---------|------|
| RPM | `software.go` | `rpm -qa --qf` |
| DEB | `software.go` | `dpkg-query -W` |
| 嵌入 binary 模块 | `binary_probe.go` | go buildinfo + ELF 解析（NodeJS / Python 嵌入 lib 也走此） |
| JAR | `jar_scanner.go` | 扫 `MANIFEST.MF` + `pom.properties` |
| pip | `python_packages.go` | `site-packages/*.dist-info/METADATA` |
| npm | `node_packages.go` | `node_modules/*/package.json` |
| 容器 SBOM | `container_sbom.go` | 容器内重复以上策略，`scope=container` |
| 中间件识别 | `app.go` | 进程命令行匹配 + 端口探测 + 配置路径推断 |

#### 更新频次

- 软件包全量：周期 6h（包变化频次低）
- 中间件应用：周期 5min（启停频繁，需快速感知）
- 镜像内 SBOM：在容器启动事件触发一次扫描，结果落 `software` 表（`scope=container`）

#### 关联关系

```
software ─N:1─→ hosts
software ─N:1─→ containers（container_id 非空）
software ─→ vulnerabilities（PURL/NEVRA 双索引匹配，详见 vuln-module-design.md）
apps ─N:1─→ hosts
apps ─N:1─→ processes (process_id)
apps ─1:N─→ ports
```

---

### 3.6 容器资产（Container）

#### 字段定义

```sql
CREATE TABLE containers (
    id              VARCHAR(128) PRIMARY KEY,             -- 主键，与 container_id 同
    tenant_id       VARCHAR(64) NOT NULL,
    host_id         VARCHAR(64) NOT NULL,                 -- 宿主机
    container_id    VARCHAR(128) NOT NULL,                -- runtime 原始 ID
    container_name  VARCHAR(255),
    image           VARCHAR(512),                         -- 完整镜像引用（含 registry/repo:tag）
    image_id        VARCHAR(128),                         -- sha256:xxx
    runtime         VARCHAR(20),                          -- docker / containerd / cri-o / podman
    status          VARCHAR(20),                          -- running / exited / paused
    pid             INT,                                  -- 容器 1 号进程的宿主 PID
    pns             BIGINT,                               -- PID namespace inode
    net_namespace   BIGINT,                               -- net namespace inode
    privileged      BOOLEAN,
    cap_add         JSON,                                 -- 添加的 capabilities
    host_network    BOOLEAN,
    host_pid        BOOLEAN,
    host_ipc        BOOLEAN,
    -- K8s 关联（DaemonSet probe 富化）
    pod_uid         VARCHAR(64),
    pod_name        VARCHAR(255),
    pod_namespace   VARCHAR(255),
    cluster_id      BIGINT,
    workload_kind   VARCHAR(50),                          -- Deployment / DaemonSet / StatefulSet / Job
    workload_name   VARCHAR(255),
    created_at      TIMESTAMP,
    fingerprint     VARCHAR(64),                          -- SHA256(host|container_id|image_id)
    collected_at    TIMESTAMP NOT NULL,
    INDEX idx_tenant_host (tenant_id, host_id),
    INDEX idx_tenant_pod (tenant_id, pod_uid),
    INDEX idx_tenant_image (tenant_id, image_id),
    INDEX idx_tenant_cluster_ns (tenant_id, cluster_id, pod_namespace)
);
```

#### 采集方式

- 主机侧（`plugins/collector/engine/handlers/container.go`）：
  - 优先级：containerd CRI socket → cri-dockerd → docker socket → crio
  - 取 PID / PNS / 状态 / image / 安全字段（`Privileged`、`HostNetwork` 等通过 OCI runtime spec）
- K8s 侧（DaemonSet `kube probe`，**新建**）：
  - watch 本节点 Pod，按 `pod.spec.nodeName == os.Hostname()` 过滤
  - 把 Pod 元数据回写到对应 container 行（`pod_uid` / `workload_*` / `pod_namespace`）

#### 更新频次

- 全量：60s
- 增量：runtime 事件流（containerd `subscribe`，docker `events`）实时

#### 关联关系

```
containers ─N:1─→ hosts
containers ─N:1─→ kube_pods（pod_uid）
containers ─N:1─→ images（image_id）
containers ─1:N─→ processes（host_id + container_id）
containers ─1:N─→ software（scope=container）
```

---

### 3.7 K8s 资产（Cluster / Node / Namespace / Pod / Service / Ingress / RBAC）

> **采集主体调整**：现有 `internal/server/manager/biz/kube_*` 把 audit/detect/sync/baseline 全塞在 Manager，**违反"Manager 只做业务编排"原则**。新模型把：
> - 资产采集 → **Engine 内 `kube probe` 模块**（informer/watch）
> - 资产持久化 → **Consumer 消费 `mxsec.agent.kube` topic**
> - 业务 CRUD（集群注册、kubeconfig 管理） → 保留在 Manager
> - audit 检测 → **Engine** 接管（与告警引擎合一）

#### 字段定义

```sql
CREATE TABLE kube_clusters (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    api_server   VARCHAR(500),
    audit_token  VARCHAR(64) NOT NULL,           -- per-cluster webhook 鉴权
    kube_config  TEXT,                           -- 加密存储
    version      VARCHAR(50),
    distribution VARCHAR(50),                    -- k8s / openshift / gke / aks / eks / rancher
    node_count   INT,
    pod_count    INT,
    namespace_count INT,
    health_score INT DEFAULT 100,
    status       ENUM('running','warning','offline') DEFAULT 'offline',
    fingerprint  VARCHAR(64),                    -- SHA256(api_server|cluster_uid)
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_tenant_name (tenant_id, name),
    UNIQUE KEY uk_audit_token (audit_token),
    INDEX idx_tenant_status (tenant_id, status)
);

CREATE TABLE kube_nodes (
    id             VARCHAR(128) PRIMARY KEY,           -- SHA1(cluster_id|node_uid)
    tenant_id      VARCHAR(64) NOT NULL,
    cluster_id     BIGINT NOT NULL,
    node_uid       VARCHAR(64) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    status         VARCHAR(20),                        -- Ready / NotReady
    roles          VARCHAR(255),                       -- control-plane / worker
    internal_ip    VARCHAR(45),
    external_ip    VARCHAR(45),
    os_image       VARCHAR(255),
    kernel_version VARCHAR(100),
    kubelet_version VARCHAR(50),
    cri_runtime    VARCHAR(100),
    cpu_capacity   VARCHAR(20),
    memory_capacity VARCHAR(20),
    pod_count      INT,
    labels         JSON,
    taints         JSON,
    -- 关联：节点上跑的 mxsec-agent host_id（DaemonSet 上报）
    host_id        VARCHAR(64),
    fingerprint    VARCHAR(64),
    collected_at   TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_uid (tenant_id, cluster_id, node_uid),
    INDEX idx_tenant_host (tenant_id, host_id)
);

CREATE TABLE kube_namespaces (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    cluster_id   BIGINT NOT NULL,
    ns_uid       VARCHAR(64) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    labels       JSON,
    annotations  JSON,
    status       VARCHAR(20),                       -- Active / Terminating
    pod_count    INT,
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_ns (tenant_id, cluster_id, name)
);

CREATE TABLE kube_pods (
    id             VARCHAR(128) PRIMARY KEY,         -- SHA1(cluster_id|pod_uid)
    tenant_id      VARCHAR(64) NOT NULL,
    cluster_id     BIGINT NOT NULL,
    pod_uid        VARCHAR(64) NOT NULL,
    name           VARCHAR(255),
    namespace      VARCHAR(255),
    node_name      VARCHAR(255),
    status         VARCHAR(20),
    pod_ip         VARCHAR(45),
    host_ip        VARCHAR(45),
    service_account VARCHAR(255),
    host_network   BOOLEAN,
    host_pid       BOOLEAN,
    host_ipc       BOOLEAN,
    privileged     BOOLEAN,                          -- 任一容器 privileged
    workload_kind  VARCHAR(50),                      -- 上溯 OwnerReferences → Deployment / DaemonSet
    workload_name  VARCHAR(255),
    container_count INT,
    restarts       INT,
    qos_class      VARCHAR(20),                      -- Guaranteed / Burstable / BestEffort
    labels         JSON,
    annotations    JSON,
    fingerprint    VARCHAR(64),
    collected_at   TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_uid (tenant_id, cluster_id, pod_uid),
    INDEX idx_tenant_cluster_ns (tenant_id, cluster_id, namespace),
    INDEX idx_tenant_node (tenant_id, node_name)
);

CREATE TABLE kube_workloads (
    id             VARCHAR(128) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    cluster_id     BIGINT NOT NULL,
    namespace      VARCHAR(255),
    kind           VARCHAR(50),                    -- Deployment / DaemonSet / StatefulSet / Job / CronJob
    name           VARCHAR(255),
    workload_uid   VARCHAR(64),
    images         JSON,                           -- ["nginx:1.25.0", "busybox:latest"]
    replicas       INT,
    ready_replicas INT,
    labels         JSON,
    selector       JSON,
    fingerprint    VARCHAR(64),
    collected_at   TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_uid (tenant_id, cluster_id, workload_uid)
);

CREATE TABLE kube_services (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    cluster_id   BIGINT NOT NULL,
    namespace    VARCHAR(255),
    name         VARCHAR(255),
    svc_uid      VARCHAR(64),
    type         VARCHAR(20),                     -- ClusterIP / NodePort / LoadBalancer / ExternalName
    cluster_ip   VARCHAR(45),
    external_ip  VARCHAR(45),
    ports        JSON,                            -- [{port,targetPort,protocol,nodePort}]
    selector     JSON,
    exposure     ENUM('internal','external_lb','nodeport','headless') DEFAULT 'internal',
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_uid (tenant_id, cluster_id, svc_uid),
    INDEX idx_tenant_exposure (tenant_id, exposure)
);

CREATE TABLE kube_ingresses (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    cluster_id   BIGINT NOT NULL,
    namespace    VARCHAR(255),
    name         VARCHAR(255),
    ing_uid      VARCHAR(64),
    class_name   VARCHAR(100),                    -- nginx / traefik / istio
    hosts        JSON,                            -- ["api.example.com"]
    rules        JSON,
    tls          BOOLEAN,
    backend_services JSON,                        -- 关联到的 Service 名
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_uid (tenant_id, cluster_id, ing_uid)
);

CREATE TABLE kube_rbac_bindings (
    id              VARCHAR(128) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    cluster_id      BIGINT NOT NULL,
    kind            VARCHAR(30),                  -- ClusterRoleBinding / RoleBinding
    binding_name    VARCHAR(255),
    role_kind       VARCHAR(30),                  -- ClusterRole / Role
    role_name       VARCHAR(255),
    subjects        JSON,                         -- [{kind:User/Group/ServiceAccount, name, namespace}]
    aggregated_rules JSON,                        -- 直接展开的 (apiGroups,resources,verbs)
    is_cluster_admin BOOLEAN,                     -- 命中 cluster-admin
    risk_score      INT,                          -- 0-100，由 Engine 算
    fingerprint     VARCHAR(64),
    collected_at    TIMESTAMP NOT NULL,
    UNIQUE KEY uk_tenant_cluster_kind_name (tenant_id, cluster_id, kind, binding_name),
    INDEX idx_tenant_admin (tenant_id, is_cluster_admin)
);
```

#### 采集方式

| 方式 | 用途 | 实现位置 |
|------|------|---------|
| **A. Audit Webhook**（per-cluster `audit_token`）| 实时审计事件 → Engine 规则检测 | 现有 `internal/server/manager/api/kube_audit.go`（保留） |
| **B. GCP Pub/Sub**（GKE 场景） | 云上托管集群审计日志 | 现有 `KubeCluster.GCPEnabled` 字段（保留） |
| **C. kubeconfig informer**（**新建在 Engine**） | 全量资产 + watch 增量 | Engine 启动 `cluster.informer` goroutine |
| **D. DaemonSet node probe** | 节点视角（容器 PID/PNS、kubelet 视角） | `plugins/collector/engine/handlers/container.go` + DS 富化 |

#### 更新频次

- informer 模式：本地缓存 + watch 实时（资源变化秒级）
- DaemonSet 周期：60s
- 5min 全量校对（防 watch 断流）

#### 关联关系

```
kube_clusters ─1:N─→ kube_nodes / kube_namespaces / kube_pods / kube_workloads / kube_services / kube_ingresses / kube_rbac_bindings
kube_nodes ─1:1─→ hosts (host_id)
kube_pods ─1:1─→ hosts (主机 runtime_type=k8s 时)
kube_pods ─1:N─→ containers
kube_pods ─N:1─→ kube_workloads（OwnerReferences）
kube_services ─→ kube_pods（selector 解析为 pod set，存关系表）
kube_ingresses ─→ kube_services
```

---

### 3.8 镜像资产（Image / Layer / SBOM / 构建链）

#### 字段定义

```sql
CREATE TABLE images (
    image_id        VARCHAR(128) PRIMARY KEY,          -- sha256:xxx
    tenant_id       VARCHAR(64) NOT NULL,
    repository      VARCHAR(512),                      -- registry/repo
    tags            JSON,                              -- ["1.25.0","latest"]
    registry        VARCHAR(255),                      -- registry.example.com
    os              VARCHAR(50),                       -- alpine / debian / ubuntu / busybox / scratch
    os_version      VARCHAR(50),
    architecture    VARCHAR(20),
    size_bytes      BIGINT,
    config_digest   VARCHAR(128),
    base_image_id   VARCHAR(128),                      -- 上溯到的基础镜像（由 Trivy/Syft 算）
    layer_count     INT,
    created_at      TIMESTAMP,
    first_seen_at   TIMESTAMP,                         -- 第一次在某 container/registry 见到
    last_seen_at    TIMESTAMP,
    fingerprint     VARCHAR(64),
    INDEX idx_tenant_repo (tenant_id, repository),
    INDEX idx_tenant_base (tenant_id, base_image_id)
);

CREATE TABLE image_layers (
    id           VARCHAR(128) PRIMARY KEY,             -- SHA256(image_id|layer_digest)
    tenant_id    VARCHAR(64) NOT NULL,
    image_id     VARCHAR(128) NOT NULL,
    layer_index  INT NOT NULL,                         -- 自上而下 0..N
    layer_digest VARCHAR(128) NOT NULL,
    size_bytes   BIGINT,
    cmd          TEXT,                                 -- Dockerfile 指令（来自 history）
    empty_layer  BOOLEAN,
    INDEX idx_tenant_image (tenant_id, image_id),
    INDEX idx_tenant_digest (tenant_id, layer_digest)  -- 同 layer 跨镜像复用检测
);

CREATE TABLE image_sbom (
    id              VARCHAR(128) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    image_id        VARCHAR(128) NOT NULL,
    package_type    VARCHAR(20),                       -- apk / deb / rpm / jar / pip / npm / go-module
    name            VARCHAR(255),
    version         VARCHAR(100),
    purl            VARCHAR(512),
    license         VARCHAR(255),
    layer_digest    VARCHAR(128),                      -- 出现在哪一层（追溯引入点）
    fingerprint     VARCHAR(64),
    INDEX idx_tenant_image (tenant_id, image_id),
    INDEX idx_tenant_purl (tenant_id, purl)
);

CREATE TABLE image_scan_results (
    id              VARCHAR(128) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    image_id        VARCHAR(128) NOT NULL,
    scanner         VARCHAR(50),                       -- trivy / grype
    scanner_version VARCHAR(20),
    started_at      TIMESTAMP,
    finished_at     TIMESTAMP,
    vuln_critical   INT, vuln_high INT, vuln_medium INT, vuln_low INT,
    secrets_count   INT,                               -- 敏感信息（AK/SK/私钥）
    misconfig_count INT,                               -- Dockerfile/IaC 错配
    raw_json_path   VARCHAR(512),                      -- 结果文件落 minio/本地
    INDEX idx_tenant_image_time (tenant_id, image_id, finished_at)
);
```

#### 采集方式

- **拉取扫**：用户在 UI 配置镜像仓库（Harbor / Registry V2），Manager 调度 `image_scanner.go`（Trivy CLI）扫
- **运行时扫**：DaemonSet 看到新 image_id 出现 → Engine 自动调度首次扫描
- **base_image 推断**：layer hash 链匹配公共基础镜像库（alpine/debian/ubuntu/distroless）
- **SBOM**：Trivy `--format json` 解析 → 写 `image_sbom`

#### 更新频次

- 镜像元数据：首次出现 + 每 24h 校对（tag 变化）
- SBOM / Scan：首次出现 + 每 7d 重扫（漏洞情报新增可能改变结果）

#### 关联关系

```
images ─1:N─→ image_layers / image_sbom / image_scan_results
images ─N:N─→ containers（被实际运行的镜像）
images ─N:1─→ images（base_image_id，递归到根）
image_sbom ─→ vulnerabilities（PURL 匹配）
```

---

### 3.9 网络资产（VPC / CIDR / 防火墙 / 路由）

#### 字段定义

主机网卡 `network_interfaces`（继承现有，补 `tenant_id`、`is_public_facing`）：

```sql
ALTER TABLE network_interfaces
    ADD COLUMN tenant_id VARCHAR(64) NOT NULL AFTER id,
    ADD COLUMN is_public_facing BOOLEAN DEFAULT FALSE,
    ADD INDEX idx_tenant_host (tenant_id, host_id);
```

防火墙规则快照 `host_firewall_rules`（**新增**）：

```sql
CREATE TABLE host_firewall_rules (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    backend      ENUM('iptables','nftables','firewalld','ufw') NOT NULL,
    chain        VARCHAR(50),                          -- INPUT / OUTPUT / FORWARD / 自定义链
    rule_index   INT,
    target       VARCHAR(20),                          -- ACCEPT / DROP / REJECT / LOG / 自定义
    protocol     VARCHAR(10),
    src_cidr     VARCHAR(50),
    dst_cidr     VARCHAR(50),
    src_port     VARCHAR(30),
    dst_port     VARCHAR(30),
    raw          TEXT,                                 -- 完整规则原文
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host (tenant_id, host_id)
);
```

路由表快照 `host_routes`（**新增**）：

```sql
CREATE TABLE host_routes (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    destination  VARCHAR(50),                          -- 0.0.0.0/0 / 10.0.0.0/8 / ...
    gateway      VARCHAR(45),
    interface    VARCHAR(50),
    metric       INT,
    table_name   VARCHAR(50),                          -- main / local / 自定义
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_host (tenant_id, host_id)
);
```

云 VPC / CIDR `cloud_networks`（**新增**，仅 Phase 2+ 接入云元数据时启用）：

```sql
CREATE TABLE cloud_networks (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    provider     VARCHAR(20),                          -- aliyun / aws / tencent / huawei / private
    vpc_id       VARCHAR(64),
    cidr         VARCHAR(50),
    region       VARCHAR(50),
    az           VARCHAR(50),
    name         VARCHAR(255),
    extra        JSON,
    fingerprint  VARCHAR(64),
    collected_at TIMESTAMP NOT NULL,
    INDEX idx_tenant_provider (tenant_id, provider)
);
```

#### 采集方式

- 网卡：`netlink RTM_GETLINK / RTM_GETADDR`（现有 `network.go`），增量 watch
- 防火墙：`iptables-save` / `nft list ruleset` / `firewall-cmd --list-all` / `ufw status numbered`，周期 5min
- 路由：`ip -j route show table all`，周期 5min
- 云 VPC：可选 — 用户配置云厂商 AK/SK，Manager 调云 SDK；不强制

#### 更新频次

- 网卡 IP 变化：增量 watch
- 防火墙 / 路由：周期 5min
- 云 VPC：1h

#### 关联关系

```
network_interfaces ─N:1─→ hosts
host_firewall_rules ─N:1─→ hosts
host_routes ─N:1─→ hosts
cloud_networks ─→ hosts（按 ipv4 ∈ cidr 软关联）
```

---

### 3.10 流量南北向（出入站连接 / DNS 出站 / 公网暴露面）

> "南北向" = 主机 ↔ **公网或租户外部网络** 的连接。
> 这是攻击面最大、运营优先级最高的流量类。

#### 字段定义

```sql
-- 南北向连接快照（来自 EDR connect/accept 事件聚合）
CREATE TABLE traffic_north_south (
    id              VARCHAR(128) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    host_id         VARCHAR(64) NOT NULL,
    direction       ENUM('outbound','inbound') NOT NULL,
    protocol        VARCHAR(10),
    src_ip          VARCHAR(45),
    src_port        INT,
    dst_ip          VARCHAR(45),
    dst_port        INT,
    pid             INT,
    process_name    VARCHAR(255),
    container_id    VARCHAR(64),
    -- 公网富化
    dst_asn         INT,
    dst_country     VARCHAR(8),
    dst_org         VARCHAR(255),
    is_threat_ip    BOOLEAN,                          -- 命中 IOC 时为真
    -- 聚合维度
    bucket_5m       TIMESTAMP NOT NULL,               -- 按 5 分钟桶聚合
    flow_count      INT,
    bytes_total     BIGINT,
    first_seen      TIMESTAMP,
    last_seen       TIMESTAMP,
    INDEX idx_tenant_host_time (tenant_id, host_id, bucket_5m),
    INDEX idx_tenant_dst_ip (tenant_id, dst_ip),
    INDEX idx_tenant_threat (tenant_id, is_threat_ip)
);

-- DNS 出站
CREATE TABLE traffic_dns (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    qname        VARCHAR(255),
    qtype        VARCHAR(10),                        -- A / AAAA / TXT / SRV / DGA?
    rcode        VARCHAR(10),
    answers      JSON,
    pid          INT,
    container_id VARCHAR(64),
    bucket_5m    TIMESTAMP NOT NULL,
    query_count  INT,
    is_dga_suspect BOOLEAN,                          -- ML 模型标记
    INDEX idx_tenant_host_time (tenant_id, host_id, bucket_5m),
    INDEX idx_tenant_qname (tenant_id, qname)
);

-- 公网暴露面（按主机/服务粒度，由 Engine 算）
CREATE TABLE exposure_surface (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    host_id      VARCHAR(64) NOT NULL,
    port_id      VARCHAR(128),                       -- 关联 ports.id
    public_ip    VARCHAR(45),
    port         INT,
    protocol     VARCHAR(10),
    protocol_hint VARCHAR(50),                       -- 来自 ports 表
    severity     ENUM('info','low','medium','high','critical'),
    exposure_reason VARCHAR(255),                    -- "bind 0.0.0.0" / "iptables 放通" / "云 SG 放通"
    last_checked TIMESTAMP,
    INDEX idx_tenant_severity (tenant_id, severity)
);
```

#### 采集方式

- **EDR connect/accept**（DataType 3000）：`tcp_v4_connect` / `inet_csk_accept` kprobe 全量上报到 Kafka `mxsec.agent.ebpf`
- **Consumer 聚合**：5 分钟桶按 `(host_id, src_ip, src_port, dst_ip, dst_port, protocol)` 聚合写 `traffic_north_south`
- **IP 富化**：dst_ip 进入聚合时查 GeoIP + ASN + IOC（Redis Set `mxsec:ioc:ip`）
- **DNS 出站**：UDP 53 / TCP 53 报文 eBPF 钩子（getaddrinfo 替代方案见 `edr-agent-design.md`）
- **暴露面**：Engine 周期扫 `ports` 表 + 公网 IP，对 `exposure='public'` 端口写 `exposure_surface`

#### 更新频次

- 流水：实时（5min 聚合落库）
- 暴露面计算：5min
- DGA 检测：实时（ML 推理 IForest / n-gram）

#### 关联关系

```
traffic_north_south ─N:1─→ hosts / processes (host_id + pid) / containers
traffic_dns ─N:1─→ hosts
exposure_surface ─N:1─→ hosts / ports
```

---

### 3.11 流量东西向（Pod 间 / 主机间访问拓扑）

> "东西向" = **集群 / VPC 内部**主机或 Pod 之间的连接。
> 是横向移动、勒索蠕虫扩散、内鬼数据外泄的检测面。

#### 字段定义

```sql
-- 东西向连接聚合（边）
CREATE TABLE traffic_east_west_edges (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    -- 源
    src_host_id      VARCHAR(64),
    src_container_id VARCHAR(64),
    src_pod_uid      VARCHAR(64),
    src_namespace    VARCHAR(255),
    src_workload     VARCHAR(255),                   -- Deployment/xxx 形式
    -- 目的
    dst_host_id      VARCHAR(64),                    -- 内网 IP 反解到的主机
    dst_container_id VARCHAR(64),
    dst_pod_uid      VARCHAR(64),
    dst_namespace    VARCHAR(255),
    dst_workload     VARCHAR(255),
    dst_service      VARCHAR(255),                   -- ClusterIP 反解
    -- 协议
    protocol         VARCHAR(10),
    dst_port         INT,
    protocol_hint    VARCHAR(50),                    -- 由 banner / port 推断
    -- 时间窗
    bucket_15m       TIMESTAMP NOT NULL,
    flow_count       INT,
    bytes_total      BIGINT,
    first_seen       TIMESTAMP,
    last_seen        TIMESTAMP,
    -- 学习产物
    is_baseline      BOOLEAN DEFAULT FALSE,          -- 是否纳入"基线流量"
    novelty_score    INT,                            -- 0-100 新颖度（高=异常）
    INDEX idx_tenant_src (tenant_id, src_host_id, bucket_15m),
    INDEX idx_tenant_dst (tenant_id, dst_host_id, bucket_15m),
    INDEX idx_tenant_pod_pair (tenant_id, src_pod_uid, dst_pod_uid)
);

-- 拓扑节点表（缓存，UI 直查用）
CREATE TABLE traffic_topology_nodes (
    id           VARCHAR(128) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    node_kind    ENUM('host','pod','workload','service','external') NOT NULL,
    ref_id       VARCHAR(128),                       -- host_id / pod_uid / workload_id / svc_id / cidr
    label        VARCHAR(255),                       -- UI 显示
    namespace    VARCHAR(255),
    cluster_id   BIGINT,
    in_degree    INT,
    out_degree   INT,
    risk_score   INT,
    last_seen    TIMESTAMP,
    INDEX idx_tenant_kind (tenant_id, node_kind)
);
```

#### 采集方式

- 同 §3.10 EDR `connect/accept`，按 `dst_ip ∈ 内网 CIDR` 过滤
- Pod IP 反解：DaemonSet probe 维护本节点 `pod_ip → pod_uid` 映射，写 Redis Hash `mxsec:k8s:podip:{cluster_id}`（TTL 24h）；其他节点查表
- Service ClusterIP 反解：Engine 维护内存 LRU 缓存（5min 失效）
- 拓扑节点表：Engine 增量物化（每 5min）

#### 更新频次

- 流水：实时 + 15min 桶聚合
- 拓扑节点：5min 增量
- 基线学习：每周 1 次离线训练（详见 [`ml-models.md`](ml-models.md) 序列模型）

#### 关联关系

```
traffic_east_west_edges
    ├── src/dst ─→ hosts / containers / kube_pods / kube_workloads / kube_services
    └── novelty_score 高 → 触发 Engine 告警 + Storyline

traffic_topology_nodes
    └── 由 edges 物化，作为 dashboard 流量拓扑视图的数据源
```

---

## 4. 跨维度关系图谱

### 4.1 图 schema 草案

```
                          ┌─────────────┐
                          │  Tenant     │
                          └──────┬──────┘
                                 │ owns
                ┌────────────────┼────────────────┐
                │                │                │
          ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼──────┐
          │   Host    │    │  Cluster  │    │  Image     │
          └─┬─┬─┬─┬───┘    └──┬─┬─┬────┘    └─────┬──────┘
            │ │ │ │           │ │ │                │
   runs ──┐ │ │ │ └── exposes │ │ └── contains     │
          ▼ ▼ ▼ ▼             │ │                  │
       ┌──────────────┐       │ │                  ▼
       │ Process / Port│      │ │            ┌──────────┐
       │ User / App   │       │ │            │   SBOM   │
       └───┬──────────┘       │ │            └────┬─────┘
           │ runs-in          │ └── has-node        │
           ▼                  │      ┌──────┐       │
       ┌──────────┐           │      │ Node │       │
       │Container │◄──────────┤      └──┬───┘       │
       └────┬─────┘           │         │ same-as   │
            │ part-of         │         ▼           │
            ▼                 │      ┌──────┐       │ matches
       ┌──────────┐           │      │ Host │       ▼
       │   Pod    │◄──────────┘      └──────┘    ┌─────────┐
       └──┬───────┘                              │ Vuln    │
          │ owned-by                             └────┬────┘
          ▼                                           │
       ┌──────────────┐                               │ affects
       │  Workload    │                               ▼
       └──────────────┘                          ┌──────────┐
                                                 │  Host /  │
                                                 │Container │
                                                 └──────────┘
```

### 4.2 节点与边类型

| 节点 kind | 来源表 | ref_id |
|-----------|--------|--------|
| `Tenant` | `tenants` | `tenant_id` |
| `Host` | `hosts` | `host_id` |
| `Cluster` | `kube_clusters` | `id` |
| `Node` | `kube_nodes` | `id` |
| `Namespace` | `kube_namespaces` | `id` |
| `Pod` | `kube_pods` | `id` |
| `Workload` | `kube_workloads` | `id` |
| `Container` | `containers` | `id` |
| `Image` | `images` | `image_id` |
| `Process` | `processes` | `id` |
| `Port` | `ports` | `id` |
| `Service`（K8s） | `kube_services` | `id` |
| `Ingress` | `kube_ingresses` | `id` |
| `Vulnerability` | `vulnerabilities` | `cve_id` |
| `Package`（SBOM） | `software` / `image_sbom` | `id` |
| `External`（公网 IP / 域名） | 不落表，按需展示 | — |

| 边 type | 起点 → 终点 | 含义 |
|---------|-------------|------|
| `RUNS_ON` | Process → Host | 进程跑在主机上 |
| `RUNS_IN` | Process → Container | 进程在容器内 |
| `EXPOSES` | Host → Port | 主机暴露端口 |
| `PART_OF` | Container → Pod | 容器属于 Pod |
| `SCHEDULED_ON` | Pod → Node | Pod 调度到节点 |
| `OWNED_BY` | Pod → Workload | OwnerReferences |
| `USES_IMAGE` | Container → Image | 使用镜像 |
| `BASED_ON` | Image → Image | 基础镜像链 |
| `CONTAINS_PKG` | Image / Host → Package | 含 SBOM 包 |
| `AFFECTED_BY` | Package → Vulnerability | PURL/NEVRA 匹配 |
| `BOUND_TO` | Service → Pod | Service selector 命中 Pod |
| `ROUTED_BY` | Ingress → Service | Ingress backend |
| `BINDING_TO` | RBACBinding → ServiceAccount | RBAC 绑定 |
| `TALKS_TO` | Pod/Host → Pod/Host/External | 流量边（聚合自 traffic_east_west_edges / traffic_north_south） |

### 4.3 三条核心查询路径（产品价值锚点）

```
[Q1 漏洞影响面] "OpenSSL CVE-2024-XXXX 影响哪些主机/容器？"
    Vuln -- AFFECTED_BY^-1 --> Package -- CONTAINS_PKG^-1 --> {Host, Image}
                                                              Image -- USES_IMAGE^-1 --> Container -- PART_OF --> Pod

[Q2 K8s 集群血缘] "这个集群 / 这个 Pod 跑了什么？暴露了什么？"
    Cluster --> Node --> {Pod -> Container -> Process; Port; Service -> Ingress}

[Q3 横向移动] "从这台失陷主机出发，能打到哪些资产？"
    Host -- TALKS_TO* (3 跳) --> {Host, Pod, External}
    + 高 novelty_score 的边优先
```

### 4.4 存储选型矩阵

| 存储 | 资产规模 | 关系深度 | 写入吞吐 | 查询语法 | 工程成本 | 决策 |
|------|----------|----------|----------|----------|----------|------|
| **MySQL + 冗余字段**（默认） | ≤ 100k 资产 | ≤ 3 跳 | 1k-10k QPS | SQL JOIN | 极低 | **Phase 1 默认** |
| ClickHouse 物化视图（边表） | ≥ 100k 资产 | ≤ 2 跳 | 100k+ EPS | SQL | 低 | **Phase 2 流量边规模上来后启用** |
| Neo4j Community | ≤ 1M 节点 | 无限跳 | 1k QPS | Cypher | 中 | **Phase 3 KA 客户**：图分析必要时 |
| Dgraph / NebulaGraph | ≥ 1M 节点 | 无限跳 | 10k+ QPS | GraphQL+/nGQL | 高（集群运维） | **Phase 4 极限规模**，按需 |

**默认方案（MySQL）落地**：
- 关系表 `asset_relations`（一张大表，含所有边类型）：

```sql
CREATE TABLE asset_relations (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    src_kind    VARCHAR(20) NOT NULL,
    src_id      VARCHAR(128) NOT NULL,
    rel_type    VARCHAR(30) NOT NULL,                  -- RUNS_ON / PART_OF / ...
    dst_kind    VARCHAR(20) NOT NULL,
    dst_id      VARCHAR(128) NOT NULL,
    weight      INT DEFAULT 1,                         -- 边权重（流量 flow_count 或 1）
    first_seen  TIMESTAMP,
    last_seen   TIMESTAMP,
    extra       JSON,
    UNIQUE KEY uk_tenant_edge (tenant_id, src_kind, src_id, rel_type, dst_kind, dst_id),
    INDEX idx_tenant_src (tenant_id, src_kind, src_id),
    INDEX idx_tenant_dst (tenant_id, dst_kind, dst_id),
    INDEX idx_tenant_type (tenant_id, rel_type)
);
```

- 路径查询用**递归 CTE**（MySQL 8 支持）实现 3 跳以内，超过 3 跳直接拒绝（防 fan-out 爆炸）

---

## 5. 流量分析模块（eBPF → 拓扑）

### 5.1 数据链路

```
Agent EDR
   ├─ kprobe tcp_v4_connect      → DataType 3000 (connect)
   ├─ kprobe inet_csk_accept     → DataType 3000 (accept)
   ├─ tracepoint sock:inet_sock_set_state → 长连接状态变化
   ├─ getaddrinfo / DNS UDP 53   → DataType 3002 (dns)
   └─ Pod IP 反解（节点内 LRU 缓存）
        │ Snappy + Protobuf
        ▼
AgentCenter → Kafka mxsec.agent.ebpf (12 分区，3d 保留)
        │
        ▼
ConsumerGroup B (Engine x N)
   ├─ Stream 聚合（5min / 15min 桶）
   ├─ Geo / ASN / IOC 富化
   ├─ Pod IP → Pod UID 反解（查 Redis）
   ├─ 写 traffic_north_south / traffic_dns / traffic_east_west_edges (Kafka mxsec.engine.traffic)
   └─ 异常检测：DGA / 反弹 shell / 横向移动 → mxsec.engine.alert

ConsumerGroup A (Consumer x N)
   └─ 消费 mxsec.engine.traffic → 写 MySQL / ClickHouse
```

### 5.2 聚合策略

- **5min 桶**（南北向）：`(host_id, dst_ip, dst_port, protocol)` 聚合，存 7d
- **15min 桶**（东西向）：`(src_pod_uid, dst_pod_uid, dst_port)` 聚合，存 14d
- 原始流水：ClickHouse `traffic_raw` 表 3d 后过期；用于回溯

### 5.3 拓扑可视化（Dashboard）

- 后端 API：`GET /api/v2/topology?scope={host|pod|workload|namespace}&id=...&depth=2`
- 节点：从 `traffic_topology_nodes` 拉
- 边：从 `traffic_east_west_edges` + `traffic_north_south` 拉，按 `bucket_15m >= now()-24h` 过滤
- 默认 **2 跳**，最大 **3 跳**；超出直接降级为聚合视图
- 颜色：`novelty_score` 高的边标红 / `is_threat_ip` 节点标红

### 5.4 与微隔离的边界

mxsec **不做** NetworkPolicy 下发 / eBPF 拦截规则（青藤蜂巢"零域微隔离"是独立产品）。本模块**只观察、只可视化**，符合 mxsec **监听优先**原则（[`operating-modes.md`](operating-modes.md)）。
未来若需"东西向阻断"，将作为 `protect` 模式下的独立 milestone 规划，本文档不展开。

---

## 6. 态势感知 Dashboard（四视角）

### 6.1 资产视角

- 当前租户的 11 维资产总览（数量、变化趋势、Top N）
- "新发现 24h" 卡片：新主机 / 新容器 / 新镜像 / 新公网暴露端口
- "资产缺口" 卡片：30 天无心跳的 Host / 镜像扫描超期的容器 / 无 RBAC 绑定的 ServiceAccount
- Drill-down：点击主机 → 11 维全资产清单 + 关系图

### 6.2 风险视角

- 漏洞 Top（按 EPSS / CVSS 排序，详见 [`vuln-module-design.md`](vuln-module-design.md)）
- 基线不合规 Top（CIS / 等保）
- 高危 RBAC 绑定数
- 暴露面：`exposure_surface` Critical/High 列表
- "资产 × 风险" 矩阵：高价值资产（asset_criticality=critical）× 高危漏洞 → 红格

### 6.3 攻击视角

- 攻击链（Storyline）实时流：`mxsec.engine.storyline` Topic
- ATT&CK 矩阵热力图（按租户）
- IOC 命中 Top（IP / Hash / Domain）
- 异常东西向边 Top（novelty_score）

### 6.4 流量视角

- 公网出站 Top 域名 / IP / ASN
- DNS 异常（DGA suspect）
- 东西向拓扑（默认 namespace 维度，可下钻 workload / pod）
- 入站异常源（首次见到的公网 IP）

### 6.5 数据源汇总

| 视角 | 主表 | 缓存 | 刷新 |
|------|------|------|------|
| 资产 | hosts / containers / kube_* / images | `mxsec:cache:dashboard:assets` 30s | 实时 |
| 风险 | vulnerabilities / baseline_results / exposure_surface | `mxsec:cache:dashboard:risk` 60s | 30s |
| 攻击 | alerts / storyline | — | SSE 推送 |
| 流量 | traffic_topology_nodes / traffic_east_west_edges | `mxsec:cache:dashboard:topology:{ns}` 60s | 5min |

---

## 7. 资产指纹与去重

### 7.1 指纹算法

| 资产 | 指纹算法 | 用途 |
|------|---------|------|
| Host | `SHA256(machine_id \| boot_id \| primary_mac)` | 唯一识别；boot_id 变化触发重启检测 |
| Process | `SHA256(host_id \| exe_hash \| cmdline_normalized)` | 持久进程识别（开机自启脚本 vs 一次性进程） |
| Port | `SHA1(host_id \| protocol \| bind_addr \| port)` | 端口绑定身份 |
| Container | `SHA256(host_id \| container_id \| image_id)` | 容器实例唯一 |
| Image | 直接用 `image_id`（sha256:xxx） | 已是内容寻址 |
| SBOM 包 | `SHA256(name \| version \| arch \| scope)` | 同 host/container 内去重 |
| RBAC 绑定 | `SHA256(cluster_id \| kind \| binding_name \| role_name)` | 绑定身份 |
| 流量边 | `SHA256(src_id \| dst_id \| protocol \| dst_port)` | 边幂等 |

### 7.2 去重策略

- **Upsert**：所有资产写入用 `ON DUPLICATE KEY UPDATE fingerprint UNIQUE`
- **软删除**：资产消失（如容器退出）不立即删行，更新 `last_seen` + 状态字段
- **过期清理**：`last_seen < now()-30d` 的资产移到 `*_archived` 表（保留 90d），不参与告警
- **跨主机相同 fingerprint**：作为"风险信号"上报（如同一 SSH key 出现在 5 台主机）

### 7.3 IP / Hostname 变化容忍

- Host：以 `machine_id` 为锚，IP / hostname 变化只更新字段，不重新生成 host_id
- Pod：以 `pod_uid` 为锚（K8s 原生不变）
- Container：以 `container_id`（runtime 生成的 UUID）为锚

---

## 8. 多租户隔离

### 8.1 数据库层

- 所有资产表 `tenant_id NOT NULL`
- 所有索引前缀 `(tenant_id, ...)`
- GORM 中间件 `TenantScope`（见 [`multi-tenant.md`](multi-tenant.md) §3.3）强制注入

### 8.2 Kafka 层

- 默认共享 Topic，message body 含 `tenant_id`
- 大客户可选独立 Topic `mxsec.{tenant_id}.agent.asset`
- Partition Key：`{tenant_id}:{host_id}`，保证同主机资产事件有序

### 8.3 关系图谱层

- `asset_relations` 表带 `tenant_id`，查询前缀强制
- 跨租户关系**禁止**（即便 MSSP 父租户聚合查看也是 read-only 走 `/api/v2/mssp/*` 单独接口）

### 8.4 缓存层

- Redis Key 格式：`mxsec:asset:{tenant_id}:host:{host_id}` / `mxsec:k8s:podip:{tenant_id}:{cluster_id}`
- LLM / ML 推理缓存按 `(tenant_id, input_hash)` 索引

### 8.5 跨租户穿越测试

| 测试 | 期望 |
|------|------|
| 租户 A token 查 `/api/v2/hosts` 返回包含租户 B 主机 | **必须失败** |
| 租户 A token POST `/api/v2/topology` 指定 host_id ∈ 租户 B | **404** |
| Engine 读 Kafka 跨租户事件后查不到对方租户的 fingerprint 缓存 | **必须** |

---

## 9. GORM Model 示例

以容器资产为例，展示 Go 端模型与现有源码风格对齐：

```go
// internal/server/model/container.go
package model

import "time"

// Container 容器资产
type Container struct {
    ID             string    `gorm:"primaryKey;column:id;type:varchar(128);not null" json:"id"`
    TenantID       string    `gorm:"column:tenant_id;type:varchar(64);not null;index:idx_tenant_host" json:"tenant_id"`
    HostID         string    `gorm:"column:host_id;type:varchar(64);not null;index:idx_tenant_host" json:"host_id"`
    ContainerID    string    `gorm:"column:container_id;type:varchar(128);not null" json:"container_id"`
    ContainerName  string    `gorm:"column:container_name;type:varchar(255)" json:"container_name"`
    Image          string    `gorm:"column:image;type:varchar(512)" json:"image"`
    ImageID        string    `gorm:"column:image_id;type:varchar(128);index:idx_tenant_image" json:"image_id"`
    Runtime        string    `gorm:"column:runtime;type:varchar(20)" json:"runtime"`
    Status         string    `gorm:"column:status;type:varchar(20)" json:"status"`
    PID            int       `gorm:"column:pid;type:int" json:"pid"`
    PNS            int64     `gorm:"column:pns;type:bigint" json:"pns"`
    NetNamespace   int64     `gorm:"column:net_namespace;type:bigint" json:"net_namespace"`
    Privileged     bool      `gorm:"column:privileged;type:tinyint(1)" json:"privileged"`
    CapAdd         StringArray `gorm:"column:cap_add;type:json" json:"cap_add"`
    HostNetwork    bool      `gorm:"column:host_network;type:tinyint(1)" json:"host_network"`
    HostPID        bool      `gorm:"column:host_pid;type:tinyint(1)" json:"host_pid"`
    HostIPC        bool      `gorm:"column:host_ipc;type:tinyint(1)" json:"host_ipc"`
    PodUID         string    `gorm:"column:pod_uid;type:varchar(64);index:idx_tenant_pod" json:"pod_uid"`
    PodName        string    `gorm:"column:pod_name;type:varchar(255)" json:"pod_name"`
    PodNamespace   string    `gorm:"column:pod_namespace;type:varchar(255)" json:"pod_namespace"`
    ClusterID      uint      `gorm:"column:cluster_id;type:bigint;index:idx_tenant_cluster_ns" json:"cluster_id"`
    WorkloadKind   string    `gorm:"column:workload_kind;type:varchar(50)" json:"workload_kind"`
    WorkloadName   string    `gorm:"column:workload_name;type:varchar(255)" json:"workload_name"`
    CreatedTime    time.Time `gorm:"column:created_at;type:timestamp" json:"created_at"`
    Fingerprint    string    `gorm:"column:fingerprint;type:varchar(64);not null" json:"fingerprint"`
    CollectedAt    LocalTime `gorm:"column:collected_at;type:timestamp;not null;index" json:"collected_at"`
}

func (Container) TableName() string { return "containers" }
```

```go
// internal/server/common/asset/fingerprint.go
package asset

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"
)

// FingerprintHost 生成主机指纹
func FingerprintHost(machineID, bootID, primaryMAC string) string {
    h := sha256.New()
    _, _ = h.Write([]byte(strings.Join([]string{machineID, bootID, primaryMAC}, "|")))
    return hex.EncodeToString(h.Sum(nil))
}

// FingerprintContainer 生成容器实例指纹
func FingerprintContainer(hostID, containerID, imageID string) string {
    h := sha256.New()
    _, _ = h.Write([]byte(hostID + "|" + containerID + "|" + imageID))
    return hex.EncodeToString(h.Sum(nil))
}
```

---

## 10. API 设计

### 10.1 资产 CRUD（标准 REST + 多租户）

所有路径在 `/api/v2/`，JWT + Tenant + RBAC 三段鉴权（详见 [`multi-tenant.md`](multi-tenant.md) §4）。

```
# 主机
GET    /api/v2/hosts                    列表 + 过滤（business_line/tag/runtime_type/status）
GET    /api/v2/hosts/:host_id           主机详情（含 11 维子资产计数）
PATCH  /api/v2/hosts/:host_id           更新业务线 / 标签 / 资产重要性
DELETE /api/v2/hosts/:host_id           软删除（30d 回收）

# 11 维子资产
GET /api/v2/hosts/:host_id/processes
GET /api/v2/hosts/:host_id/ports
GET /api/v2/hosts/:host_id/users
GET /api/v2/hosts/:host_id/software
GET /api/v2/hosts/:host_id/containers
GET /api/v2/hosts/:host_id/network/interfaces
GET /api/v2/hosts/:host_id/network/firewall
GET /api/v2/hosts/:host_id/network/routes
GET /api/v2/hosts/:host_id/services
GET /api/v2/hosts/:host_id/crons
GET /api/v2/hosts/:host_id/ssh-keys
GET /api/v2/hosts/:host_id/sudo
GET /api/v2/hosts/:host_id/boot-items

# K8s
GET /api/v2/kube/clusters
GET /api/v2/kube/clusters/:id/nodes
GET /api/v2/kube/clusters/:id/namespaces
GET /api/v2/kube/clusters/:id/pods?namespace=
GET /api/v2/kube/clusters/:id/workloads
GET /api/v2/kube/clusters/:id/services
GET /api/v2/kube/clusters/:id/ingresses
GET /api/v2/kube/clusters/:id/rbac

# 镜像
GET /api/v2/images
GET /api/v2/images/:image_id
GET /api/v2/images/:image_id/sbom
GET /api/v2/images/:image_id/layers
GET /api/v2/images/:image_id/scans

# 关系图谱
GET /api/v2/assets/:kind/:id/relations?depth=2&types=RUNS_ON,PART_OF

# 流量
GET /api/v2/traffic/north-south?host_id=&from=&to=
GET /api/v2/traffic/dns?host_id=&from=&to=
GET /api/v2/traffic/topology?scope=namespace&id=default&depth=2
GET /api/v2/exposure-surface?severity=high

# 暴露面 / 风险评分
GET /api/v2/dashboard/asset
GET /api/v2/dashboard/risk
GET /api/v2/dashboard/attack
GET /api/v2/dashboard/traffic

# 平台管理（仅 SystemAdmin）
GET /api/v2/admin/assets/stats          全租户资产统计
```

### 10.2 资产查询响应示例

```json
GET /api/v2/hosts/h-12345
{
  "host_id": "h-12345",
  "tenant_id": "t-bank-a",
  "hostname": "app-svc-01",
  "os": {"family": "openeuler", "version": "22.03", "kernel": "5.10.0-153"},
  "hardware": {"cpu_cores": 16, "memory_mb": 65536, "disk_gb": 512},
  "network": {
    "ipv4": ["10.0.1.5"],
    "public_ipv4": ["203.0.113.5"],
    "default_gateway": "10.0.1.1"
  },
  "runtime_type": "vm",
  "business_line": "core-payment",
  "asset_criticality": "critical",
  "exposure_score": 78,
  "status": "online",
  "stats": {
    "processes": 234, "ports_listen": 12, "users": 18,
    "containers": 0, "vulnerabilities": {"critical": 2, "high": 9}
  },
  "fingerprint": "9e7f...c1b3",
  "last_heartbeat": "2026-06-06T08:23:45Z"
}
```

### 10.3 关系查询示例

```json
GET /api/v2/assets/host/h-12345/relations?depth=2&types=RUNS_ON,PART_OF,USES_IMAGE
{
  "root": {"kind": "Host", "id": "h-12345", "label": "app-svc-01"},
  "nodes": [
    {"kind": "Container", "id": "c-abc", "label": "nginx-ingress"},
    {"kind": "Image", "id": "sha256:def...", "label": "nginx:1.25.0"},
    {"kind": "Process", "id": "p-xyz", "label": "/usr/sbin/nginx"}
  ],
  "edges": [
    {"src": "h-12345", "dst": "c-abc", "type": "RUNS_ON"},
    {"src": "c-abc",   "dst": "sha256:def...", "type": "USES_IMAGE"},
    {"src": "h-12345", "dst": "p-xyz", "type": "RUNS_ON"}
  ],
  "truncated": false
}
```

---

## 11. 与现有 collector / kube_* 子模块的关系

### 11.1 collector 插件（保留 + 扩展）

| 现有 handler | 现状 | 新模型动作 |
|--------------|------|-----------|
| `process.go` | 进程快照 | **保留**，补 `caps_effective` / `start_time` |
| `port.go` | 监听端口 | **保留**，补 `bind_addr` / `protocol_hint` / `exposure` |
| `user.go` | 系统账户 | **保留**，补 `last_login` / `password_locked` |
| `software.go` | RPM/DEB | **保留** |
| `binary_probe.go` / `python_packages.go` / `node_packages.go` / `jar_scanner.go` / `go_buildinfo.go` | SBOM 类 | **保留**，已带 `purl` / `scope` |
| `container.go` | 容器列表 | **保留**，补 `pns` / `net_namespace` / 安全字段（privileged 等） |
| `container_sbom.go` | 容器内 SBOM | **保留** |
| `app.go` | 中间件识别 | **保留**，扩展白皮书 800+ 应用指纹（路线图） |
| `network.go` | 网卡 | **保留** |
| `volume.go` | 磁盘 | **保留** |
| `kmod.go` | 内核模块 | **保留** |
| `service.go` | systemd / sysv | **保留** |
| `cron.go` | crontab + timer | **保留** |
| **新增** `boot_items.go` | — | **新增**：启动项 |
| **新增** `ssh_keys.go` | — | **新增**：SSH 公钥指纹 |
| **新增** `sudoers.go` | — | **新增**：sudoers 解析 |
| **新增** `pam.go` | — | **新增**：PAM 模块清单 |
| **新增** `firewall.go` | — | **新增**：iptables/nft/firewalld/ufw 快照 |
| **新增** `routes.go` | — | **新增**：路由表 |

DataType 映射（沿用 `plugins/collector/engine/models.go::GetDataType`，扩展 5061-5067）：

```go
case "process":      return 5050
case "port":         return 5051
case "user":         return 5052
case "software", "binary_probe", "python_packages", "node_packages",
     "jar_scanner", "go_buildinfo", "container_sbom":
                     return 5053
case "container":    return 5054
case "app":          return 5055
case "network":      return 5056
case "volume":       return 5057
case "kmod":         return 5058
case "service":      return 5059
case "cron":         return 5060
// 新增（DataType 分配在 docs/datatype-allocation.md 登记）
case "boot_items":   return 5061
case "ssh_keys":     return 5062
case "sudoers":      return 5063
case "pam":          return 5064
case "firewall":     return 5065
case "routes":       return 5066
case "kube_probe":   return 5070 // DaemonSet 富化 Pod/Container 元数据
```

### 11.2 kube_* 子模块（拆分搬迁）

| 现有文件 | 现状归属 | 新归属 |
|----------|----------|--------|
| `kube_client.go`（KubeClientManager） | Manager biz | **保留在 Manager**（业务 CRUD 用） |
| `kube_sync.go`（5min 周期同步） | Manager biz | **搬到 Engine**：`internal/server/engine/kube/informer.go`（informer 模式取代轮询） |
| `kube_audit_processor.go` | Manager biz | **保留 webhook 入口**（kube_audit.go API），处理逻辑搬到 Engine |
| `kube_detector.go`（8 条 hard-coded 规则） | Manager biz | **搬到 Engine**：纳入统一 CEL 规则集，详见 [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| `kube_baseline_check*.go`（80 CIS 项） | Manager biz | **搬到 baseline 插件**或 Engine K8s 子层；详见基线模块 |
| `kube_rule_engine.go`（cel-go） | Manager biz | **搬到 Engine**：与主 CEL 引擎合并 |
| `kube_alarm_filter.go` | Manager biz | **搬到 Engine**：告警去重统一在 Engine |
| `pdf_render_kube.go` | Manager biz | **保留在 Manager**（报表生成是 Manager 职责） |

> 拆分原则与 [`architecture.md`](architecture.md) §2 一致：Manager 只做业务编排 + CRUD + 报表，**所有检测分析必须搬到 Engine**。

---

## 12. 后续流量分析模块 spec（Phase 2 落地）

### 12.1 Sprint 拆解

| Sprint | 周期 | 交付物 |
|--------|------|--------|
| S1 | 2 周 | EDR `tcp_v4_connect` / `inet_csk_accept` 事件全量上报 + Kafka topic 就绪 |
| S2 | 2 周 | Engine Stream 聚合（5min/15min 桶）+ Geo/ASN/IOC 富化 + Pod IP 反解 |
| S3 | 2 周 | `traffic_north_south` / `traffic_dns` / `traffic_east_west_edges` 表 + Consumer 写入器 |
| S4 | 2 周 | `traffic_topology_nodes` 物化 + `/api/v2/topology` API + 前端拓扑图组件 |
| S5 | 2 周 | DGA 检测（n-gram + IForest） + 反弹 shell（出站 + cmdline 关联）+ 横向移动告警 |
| S6 | 2 周 | 基线学习（每周离线训练 + 写 `is_baseline=true`） + 异常 novelty_score |

### 12.2 SLO

| 指标 | 目标 |
|------|------|
| EDR 事件 P95 端到端延迟（Agent → Engine） | ≤ 5s |
| 拓扑 API 单租户 P95 响应 | ≤ 500ms（depth=2，节点 ≤ 500） |
| Engine 聚合丢弃率 | ≤ 0.1%（背压时优先丢"非新颖"边） |
| DGA 检测准确率 | ≥ 95%（90d 磨合后） |

### 12.3 容量边界

| 指标 | 单租户上限 |
|------|-----------|
| 主机数 | 10k（中规模） / 100k（KA） |
| 东西向 15min 桶活跃边数 | 1M（超出走采样） |
| ClickHouse `traffic_raw` 3d 体量 | 估算 1 主机 100 events/s × 86400 × 3 × 100B = 2.5 GB |

### 12.4 与 microsegmentation 的边界

mxsec **不下发** NetworkPolicy / eBPF 阻断规则。模块仅产出"如果按此基线下策略，会阻断 X% 流量"的**模拟报告**，符合 `observe` 模式哲学。`protect` 模式下是否启用东西向阻断是独立产品决策，本模块不绑定。

---

## 13. 验收 checklist

- [ ] 所有 11 维资产表带 `tenant_id` + 索引前缀 `(tenant_id, ...)`
- [ ] 跨租户穿越测试通过（详见 [`multi-tenant.md`](multi-tenant.md) §11）
- [ ] `fingerprint` 算法稳定（重启 / 升级 Agent 不变）
- [ ] collector 新增 6 个 handler（boot_items / ssh_keys / sudoers / pam / firewall / routes）落地
- [ ] DaemonSet `kube probe` 富化 `pod_uid` / `workload_kind` 落到 `containers` 表
- [ ] `asset_relations` 表 + 递归 CTE 查询 3 跳以内 P95 ≤ 200ms
- [ ] `/api/v2/topology` API 支持 depth=2 默认、depth=3 受限
- [ ] Dashboard 四视角全部可用（资产 / 风险 / 攻击 / 流量）
- [ ] 流量分析 EDR connect 事件 → Engine 聚合 → MySQL / CK 落库链路通
- [ ] `mxsec.engine.traffic` topic 上线（**Phase 2 落地**，目前预留在 [`datatype-allocation.md`](datatype-allocation.md) §4 "Engine 子模块扩展预留 11200-11899" 段内；流量模块开工时需在 datatype-allocation.md §3 主表与 [`architecture.md`](architecture.md) §4.1 Topic 总览中正式登记 DataType 11201-11299）
- [ ] 镜像 SBOM + base_image 上溯逻辑验证（Trivy 结果对齐）

---

## 14. 参考文档

| 主题 | 链接 |
|------|------|
| 平台总架构 | [`architecture.md`](architecture.md) |
| 监听 / 防护双模式 | [`operating-modes.md`](operating-modes.md) |
| 多租户硬隔离 | [`multi-tenant.md`](multi-tenant.md) |
| 三大产品目标 | [`security-objectives.md`](security-objectives.md) |
| DataType 分配 | [`datatype-allocation.md`](datatype-allocation.md) |
| 漏洞模块（PURL/NEVRA 匹配） | [`vuln-module-design.md`](vuln-module-design.md) |
| Engine 检测设计 | [`engine-design.md`](engine-design.md) / [`engine-detection-design.md`](engine-detection-design.md) |
| 本地 ML 模型清单 | [`ml-models.md`](ml-models.md) |
| Falco / Sigma 集成 | [`falco-sigma-integration.md`](falco-sigma-integration.md) |
| API 参考 | [`api-reference.md`](api-reference.md) |
| 内部 — 容器 / K8s 模块对标 | `ref/05-容器K8s.md` |
| 内部 — Agent 模块对标 | `ref/02-Agent.md` |
| 内部 — 青藤万象资产清单 | `ref/appendix/青藤万象-能力清单.md` |
| 内部 — 青藤蜂巢容器能力 | `ref/appendix/蜂巢-能力清单.md` |
