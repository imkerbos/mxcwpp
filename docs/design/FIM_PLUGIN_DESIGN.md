# FIM 插件（文件完整性监控）设计方案

**版本**: v1.0
**日期**: 2026-02-23
**状态**: 方案评审

---

## 1. 背景与目标

### 1.1 现状

- 234 台主机已安装 AIDE 并初始化数据库（AIDE_001/002 全部 pass）
- **0 台配置了定期检查**（AIDE_003 全部 fail）
- AIDE 产出的文件变更报告无人采集，数据留在本地 `/var/log/aide/aide.log`
- 默认 aide.conf 监控范围过广，业务日志（nginx logs 等）产生大量噪音

### 1.2 目标

| 目标 | 说明 |
|------|------|
| 定期执行 AIDE 检查 | 替代手动 cron，由平台统一调度 |
| 采集变更报告 | 解析 aide --check 产出，结构化上报 |
| 服务端策略管理 | 按业务线/主机组下发不同的 aide.conf |
| 告警联动 | 关键文件变更自动生成安全告警 |
| 态势感知对接 | 为后续态势感知平台提供文件完整性数据源 |

---

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────┐
│                    Manager (HTTP API)                │
│  ┌──────────┐  ┌──────────┐  ┌───────────────────┐  │
│  │ FIM 策略 │  │ FIM 任务 │  │ FIM 变更事件查询  │  │
│  │  管理    │  │  管理    │  │                   │  │
│  └────┬─────┘  └────┬─────┘  └────────┬──────────┘  │
│       │              │                 │             │
│       ▼              ▼                 ▼             │
│  ┌─────────────────────────────────────────────┐    │
│  │              MySQL 数据库                    │    │
│  │  fim_policies | fim_events | fim_tasks      │    │
│  └─────────────────────────────────────────────┘    │
└──────────────────────┬──────────────────────────────┘
                       │
                       │ gRPC
                       ▼
┌─────────────────────────────────────────────────────┐
│                AgentCenter                           │
│  ┌──────────────┐  ┌──────────────────────────┐     │
│  │ FIM 任务调度 │  │ FIM 事件接收 & 告警生成  │     │
│  └──────┬───────┘  └──────────┬───────────────┘     │
└─────────┼──────────────────────┼────────────────────┘
          │ gRPC                 │
          ▼                      │
┌─────────────────────┐          │
│       Agent         │          │
│  ┌───────────────┐  │          │
│  │  FIM Plugin   │  │──────────┘
│  │               │  │  DataType: 6000-6003
│  │  ┌─────────┐  │  │
│  │  │ Scheduler│  │  │
│  │  │ Parser  │  │  │
│  │  │ Reporter│  │  │
│  │  └─────────┘  │  │
│  └───────────────┘  │
│  ┌───────────────┐  │
│  │baseline plugin│  │  (现有，不改动)
│  ├───────────────┤  │
│  │collector plugin│ │  (现有，不改动)
│  └───────────────┘  │
└─────────────────────┘
```

### 2.2 DataType 分配

| DataType | 方向 | 说明 |
|----------|------|------|
| 6000 | Server → Plugin | FIM 检查任务（含策略配置） |
| 6001 | Plugin → Server | FIM 变更事件（检查结果） |
| 6002 | Plugin → Server | FIM 任务完成信号 |
| 6003 | Server → Plugin | FIM 策略更新（aide.conf 同步） |

### 2.3 与现有组件的关系

```
baseline plugin:
  - 继续负责 AIDE_001~004 的合规检查（装没装、配没配）
  - 不做任何改动

fim plugin (新增):
  - 负责实际执行 aide --check
  - 解析变更报告
  - 管理 aide.conf 配置
  - 上报结构化事件

collector plugin:
  - 不做任何改动
```

---

## 3. 数据模型

### 3.1 数据库表设计

#### fim_policies（FIM 策略表）

```sql
CREATE TABLE fim_policies (
    policy_id     VARCHAR(64) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    description   TEXT,

    -- 监控目录配置
    watch_paths   JSON NOT NULL,       -- 监控路径及检查级别
    exclude_paths JSON,                -- 排除路径列表

    -- 调度配置
    check_interval_hours INT DEFAULT 24,  -- 检查间隔（小时）

    -- 目标范围
    target_type   VARCHAR(20) DEFAULT 'all',  -- all/host_ids/business_line
    target_config JSON,                        -- 目标配置

    enabled       TINYINT(1) DEFAULT 1,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```

**watch_paths 示例**:
```json
[
  {"path": "/bin",  "level": "NORMAL", "comment": "系统命令"},
  {"path": "/sbin", "level": "NORMAL", "comment": "系统管理命令"},
  {"path": "/usr",  "level": "NORMAL", "comment": "用户态程序"},
  {"path": "/etc/ssh/sshd_config", "level": "NORMAL", "comment": "SSH配置"},
  {"path": "/etc/passwd",  "level": "NORMAL", "comment": "用户文件"},
  {"path": "/etc/shadow",  "level": "NORMAL", "comment": "密码文件"},
  {"path": "/etc/sudoers", "level": "NORMAL", "comment": "提权配置"},
  {"path": "/etc/crontab", "level": "NORMAL", "comment": "定时任务"}
]
```

**exclude_paths 示例**:
```json
[
  "/usr/src",
  "/usr/tmp",
  "/var/log",
  "/usr/local/openresty/nginx/logs",
  "/var/lib/mysql",
  "/var/lib/starrocks"
]
```

#### fim_events（FIM 变更事件表）

```sql
CREATE TABLE fim_events (
    event_id      VARCHAR(64) PRIMARY KEY,
    host_id       VARCHAR(64) NOT NULL,
    hostname      VARCHAR(255),
    task_id       VARCHAR(64),

    -- 变更信息
    file_path     VARCHAR(1024) NOT NULL,    -- 变更文件路径
    change_type   VARCHAR(20) NOT NULL,      -- added/removed/changed
    change_detail JSON,                       -- 变更详情

    -- 分类与风险
    severity      VARCHAR(20) DEFAULT 'medium',  -- critical/high/medium/low/info
    category      VARCHAR(50),                    -- binary/config/auth/log/other

    -- 时间
    detected_at   TIMESTAMP NOT NULL,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_host_id (host_id),
    INDEX idx_file_path (file_path(255)),
    INDEX idx_severity (severity),
    INDEX idx_detected_at (detected_at)
);
```

**change_detail 示例**:
```json
{
  "size_before": "3907",
  "size_after": "3931",
  "hash_changed": true,
  "permission_changed": false,
  "owner_changed": false,
  "attributes": "..H...."
}
```

#### fim_tasks（FIM 任务表）

```sql
CREATE TABLE fim_tasks (
    task_id       VARCHAR(64) PRIMARY KEY,
    policy_id     VARCHAR(64),
    status        VARCHAR(20) DEFAULT 'pending',  -- pending/running/completed/failed
    target_type   VARCHAR(20),
    target_config JSON,

    dispatched_host_count INT DEFAULT 0,
    completed_host_count  INT DEFAULT 0,
    total_events          INT DEFAULT 0,

    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    executed_at   TIMESTAMP NULL,
    completed_at  TIMESTAMP NULL
);
```

#### fim_task_host_status（FIM 任务主机状态）

```sql
CREATE TABLE fim_task_host_status (
    id            INT AUTO_INCREMENT PRIMARY KEY,
    task_id       VARCHAR(64) NOT NULL,
    host_id       VARCHAR(64) NOT NULL,
    hostname      VARCHAR(255),
    status        VARCHAR(20) DEFAULT 'dispatched',  -- dispatched/completed/timeout/failed

    total_entries  INT DEFAULT 0,       -- 扫描文件总数
    added_count    INT DEFAULT 0,       -- 新增文件数
    removed_count  INT DEFAULT 0,       -- 删除文件数
    changed_count  INT DEFAULT 0,       -- 变更文件数
    run_time_sec   INT DEFAULT 0,       -- 执行耗时（秒）
    error_message  TEXT,

    dispatched_at  TIMESTAMP NULL,
    completed_at   TIMESTAMP NULL,

    INDEX idx_task_id (task_id),
    INDEX idx_host_id (host_id)
);
```

### 3.2 自动分类与严重等级

插件根据文件路径自动判断 severity 和 category：

```
关键二进制变更 → critical
  /bin/*, /sbin/*, /usr/bin/*, /usr/sbin/*
  特别关注: sshd, bash, sudo, su, passwd, login

认证配置变更 → high
  /etc/passwd, /etc/shadow, /etc/group, /etc/gshadow
  /etc/sudoers, /etc/sudoers.d/*, /etc/pam.d/*

SSH/远程访问变更 → high
  /etc/ssh/sshd_config, /etc/ssh/ssh_config

启动/定时任务变更 → high
  /etc/crontab, /etc/cron.d/*, /etc/cron.daily/*
  /etc/systemd/system/*, /etc/rc.d/*
  /etc/init.d/*

一般配置变更 → medium
  /etc/* (其他配置文件)

新增文件（任何路径） → 比同路径变更高一级
  例: /usr/bin/ 下新增文件 → critical

删除文件 → 与新增同级
```

---

## 4. 插件实现

### 4.1 插件目录结构

```
plugins/fim/
├── main.go                 # 入口：IPC通信、任务路由
├── engine/
│   ├── engine.go           # 核心引擎：调度、执行、上报
│   ├── models.go           # 数据模型：策略、事件
│   ├── parser.go           # AIDE 报告解析器
│   ├── classifier.go       # 变更分类 & 严重等级判定
│   └── config_renderer.go  # 策略 → aide.conf 渲染器
└── go.mod
```

### 4.2 核心流程

```
Plugin 启动
    │
    ├─ 接收 FIM 检查任务 (DataType: 6000)
    │   ├─ 解析策略（watch_paths, exclude_paths）
    │   ├─ 渲染 aide.conf → /etc/aide-mxsec.conf
    │   ├─ 检查 AIDE 数据库是否存在
    │   │   ├─ 不存在 → aide --init -c /etc/aide-mxsec.conf
    │   │   └─ 存在 → 继续
    │   ├─ 执行 aide --check -c /etc/aide-mxsec.conf
    │   ├─ 解析报告（parser.go）
    │   │   ├─ 提取 Summary（added/removed/changed 计数）
    │   │   ├─ 提取 Changed entries（文件路径 + 属性标记）
    │   │   └─ 提取 Detailed information（size/hash/perm 前后对比）
    │   ├─ 分类 & 评级（classifier.go）
    │   ├─ 逐条上报事件 (DataType: 6001)
    │   ├─ 发送完成信号 (DataType: 6002)
    │   └─ 更新 AIDE 数据库快照
    │       aide --update -c /etc/aide-mxsec.conf
    │       mv aide.db.new.gz → aide.db.gz
    │
    └─ 接收策略更新 (DataType: 6003)
        └─ 重新渲染 aide.conf
```

### 4.3 AIDE 报告解析器（parser.go）核心逻辑

基于你那台 CDN 机器的实际产出，解析以下格式：

```
输入（aide --check 标准输出）:

  Summary:
    Total number of entries:      149022
    Added entries:                0
    Removed entries:              0
    Changed entries:              5

  Changed entries:
  f > ...   ..H.... : /etc/ssh/sshd_config
  f > ...   .. .... : /usr/local/openresty/nginx/logs/xxx.access

  Detailed information about changes:
  File: /etc/ssh/sshd_config
   Size      : 3907                             | 3931
   SHA512    : 9oQU2K...                        | TbCDP/...

输出（结构化事件）:

  []FIMEvent{
    {
      FilePath:    "/etc/ssh/sshd_config",
      ChangeType:  "changed",
      Severity:    "high",
      Category:    "ssh",
      ChangeDetail: {
        SizeBefore: "3907",
        SizeAfter:  "3931",
        HashChanged: true,
        Attributes: "..H....",
      },
    },
  }
```

### 4.4 aide.conf 渲染器（config_renderer.go）

将服务端下发的策略 JSON 转为 aide.conf 格式：

```
输入（策略 JSON）:
  watch_paths: [{path: "/bin", level: "NORMAL"}, ...]
  exclude_paths: ["/var/log", "/usr/local/openresty/nginx/logs"]

输出（/etc/aide-mxsec.conf）:

  @@define DBDIR /var/lib/aide
  @@define LOGDIR /var/log/aide
  database_in=file:@@{DBDIR}/aide.db.gz
  database_out=file:@@{DBDIR}/aide.db.new.gz
  ...
  NORMAL = R+sha512
  CONTENT = ftype+sha512
  PERMS = ftype+p+u+g+acl+selinux+xattrs

  /bin    NORMAL
  /sbin   NORMAL
  /etc/passwd  NORMAL
  ...
  !/var/log
  !/usr/local/openresty/nginx/logs
```

注意：使用独立配置文件 `/etc/aide-mxsec.conf` 而非覆盖系统默认的 `/etc/aide.conf`，避免冲突。

### 4.5 IPC 通信（复用现有 SDK）

```go
// 与 baseline/collector 完全相同的模式
client := plugins.NewClient()  // FD 3/4 Pipe

// 接收任务
task, _ := client.ReceiveTask()
switch task.DataType {
case 6000:  // FIM 检查任务
    handleCheckTask(task)
case 6003:  // 策略更新
    handlePolicyUpdate(task)
}

// 上报事件
record := &bridge.Record{
    DataType:  6001,
    Timestamp: time.Now().UnixNano(),
    Data: &bridge.Payload{
        Fields: map[string]string{
            "event_id":      uuid,
            "task_id":       taskID,
            "file_path":     "/etc/ssh/sshd_config",
            "change_type":   "changed",
            "severity":      "high",
            "category":      "ssh",
            "change_detail": `{"size_before":"3907","size_after":"3931",...}`,
        },
    },
}
client.SendRecord(record)
```

---

## 5. 服务端改动

### 5.1 AgentCenter

| 改动点 | 文件 | 说明 |
|--------|------|------|
| 事件接收 | transfer/service.go | 新增 `case 6001` 处理 FIM 事件 |
| 任务完成 | transfer/service.go | 新增 `case 6002` 更新任务状态 |
| 任务调度 | scheduler/scheduler.go | 新增 `DispatchPendingFIMTasks()` |
| 任务下发 | service/task.go | 新增 FIM 任务下发逻辑 |
| 告警生成 | transfer/service.go | severity=critical/high 时自动创建告警 |

### 5.2 Manager API

```
# 策略管理
POST   /api/v1/fim/policies           - 创建 FIM 策略
GET    /api/v1/fim/policies           - 策略列表
PUT    /api/v1/fim/policies/:id       - 更新策略
DELETE /api/v1/fim/policies/:id       - 删除策略

# 任务管理
POST   /api/v1/fim/tasks              - 创建检查任务
GET    /api/v1/fim/tasks              - 任务列表
POST   /api/v1/fim/tasks/:id/run      - 执行任务

# 变更事件查询
GET    /api/v1/fim/events             - 事件列表（支持过滤）
GET    /api/v1/fim/events/stats       - 事件统计（按主机/严重等级/类别）
GET    /api/v1/fim/events/:id         - 事件详情

# 查询参数示例
GET /api/v1/fim/events?host_id=xxx&severity=critical&category=binary&date_from=2026-02-01
```

### 5.3 UI 页面

| 页面 | 功能 |
|------|------|
| FIM 策略管理 | 创建/编辑策略（可视化配置监控目录和排除路径） |
| FIM 任务管理 | 创建检查任务、查看执行状态和进度 |
| FIM 事件列表 | 按主机、严重等级、文件类别筛选变更事件 |
| FIM 仪表盘 | 变更趋势图、Top 变更主机、高危事件统计 |
| 主机详情集成 | 在现有主机详情页增加"文件完整性"标签页 |

---

## 6. 默认策略模板

### 6.1 通用策略（所有主机）

重点监控系统关键文件，排除业务数据和日志：

```json
{
  "name": "通用文件完整性策略",
  "watch_paths": [
    {"path": "/bin",   "level": "NORMAL", "comment": "系统命令"},
    {"path": "/sbin",  "level": "NORMAL", "comment": "系统管理命令"},
    {"path": "/lib",   "level": "NORMAL", "comment": "系统库"},
    {"path": "/lib64", "level": "NORMAL", "comment": "系统库(64位)"},
    {"path": "/usr/bin",  "level": "NORMAL", "comment": "用户命令"},
    {"path": "/usr/sbin", "level": "NORMAL", "comment": "用户管理命令"},
    {"path": "/usr/lib",  "level": "NORMAL", "comment": "用户库"},

    {"path": "/etc/passwd",       "level": "NORMAL", "comment": "用户文件"},
    {"path": "/etc/shadow",       "level": "NORMAL", "comment": "密码文件"},
    {"path": "/etc/group",        "level": "NORMAL", "comment": "组文件"},
    {"path": "/etc/sudoers",      "level": "NORMAL", "comment": "提权配置"},
    {"path": "/etc/sudoers.d",    "level": "NORMAL", "comment": "提权配置目录"},
    {"path": "/etc/ssh/sshd_config", "level": "NORMAL", "comment": "SSH配置"},
    {"path": "/etc/pam.d",        "level": "NORMAL", "comment": "PAM配置"},
    {"path": "/etc/crontab",      "level": "NORMAL", "comment": "定时任务"},
    {"path": "/etc/cron.d",       "level": "NORMAL", "comment": "定时任务目录"},
    {"path": "/etc/systemd",      "level": "NORMAL", "comment": "Systemd配置"},
    {"path": "/etc/audit",        "level": "NORMAL", "comment": "审计配置"},
    {"path": "/etc/firewalld",    "level": "NORMAL", "comment": "防火墙配置"},
    {"path": "/boot",             "level": "NORMAL", "comment": "引导文件"}
  ],
  "exclude_paths": [
    "/usr/src",
    "/usr/tmp",
    "/var/log",
    "/tmp",
    "/boot/grub2/grubenv"
  ],
  "check_interval_hours": 24
}
```

### 6.2 大数据主机策略

通用策略基础上额外排除大数据组件的数据目录：

```json
{
  "name": "大数据主机文件完整性策略",
  "exclude_paths": [
    "/usr/src", "/usr/tmp", "/var/log", "/tmp",
    "/boot/grub2/grubenv",
    "/var/lib/hadoop",
    "/var/lib/starrocks",
    "/data",
    "/dfs",
    "/opt/cloudera"
  ],
  "target_type": "business_line",
  "target_config": {"business_line": "bigdata"}
}
```

### 6.3 CDN/Nginx 主机策略

额外排除 openresty/nginx 日志：

```json
{
  "name": "CDN主机文件完整性策略",
  "exclude_paths": [
    "/usr/src", "/usr/tmp", "/var/log", "/tmp",
    "/boot/grub2/grubenv",
    "/usr/local/openresty/nginx/logs",
    "/usr/local/nginx/logs"
  ],
  "target_type": "business_line",
  "target_config": {"business_line": "cdn"}
}
```

---

## 7. 分阶段实施计划

### Phase 1: 插件核心（最小可用）

**目标**: 能执行 aide --check 并将结构化事件上报到数据库

| 任务 | 说明 |
|------|------|
| 创建 plugins/fim/ 目录 | 参考 baseline 插件结构 |
| 实现 main.go | IPC 通信、任务接收、结果上报 |
| 实现 parser.go | 解析 aide --check 标准输出 |
| 实现 classifier.go | 文件路径 → severity/category 映射 |
| 建表 fim_events | 存储变更事件 |
| AgentCenter 接收 | 处理 DataType 6001/6002 |
| 手动触发 | 通过 Manager API 创建任务下发 |

**验证**: 在 1 台主机上手动触发 FIM 检查，事件写入数据库。

### Phase 2: 策略管理 + 调度

**目标**: 支持按业务线下发不同策略，定时自动执行

| 任务 | 说明 |
|------|------|
| 建表 fim_policies / fim_tasks | 策略和任务表 |
| 实现 config_renderer.go | 策略 JSON → aide.conf 渲染 |
| Manager API: 策略 CRUD | 创建/编辑/删除策略 |
| Manager API: 任务管理 | 创建/执行/查询任务 |
| 调度器 | 按 check_interval_hours 定时触发 |
| 策略下发 | DataType 6003 策略更新通道 |

**验证**: 创建大数据策略和 CDN 策略，分别下发到对应主机组，定时执行。

### Phase 3: UI + 告警

**目标**: 完整的前端体验和告警联动

| 任务 | 说明 |
|------|------|
| UI: FIM 策略管理页 | 可视化配置监控目录 |
| UI: FIM 事件列表页 | 筛选、搜索、导出 |
| UI: FIM 仪表盘 | 统计图表 |
| UI: 主机详情集成 | 增加文件完整性 Tab |
| 告警联动 | critical/high 事件自动生成告警 |
| 事件统计 API | 支持仪表盘数据查询 |

### Phase 4: 态势感知对接

**目标**: 将 FIM 事件作为数据源接入态势感知

| 任务 | 说明 |
|------|------|
| 事件推送接口 | Webhook / Kafka 输出 |
| CEF 格式支持 | 标准安全事件格式 |
| 关联分析 | 结合登录日志判断合法性 |

---

## 8. 工作量评估

| Phase | 范围 | 后端 | 前端 | 合计 |
|-------|------|------|------|------|
| Phase 1 | 插件核心 | 3-4 天 | 0 | 3-4 天 |
| Phase 2 | 策略 + 调度 | 3-4 天 | 0 | 3-4 天 |
| Phase 3 | UI + 告警 | 2-3 天 | 3-4 天 | 5-7 天 |
| Phase 4 | 态势感知对接 | 2-3 天 | 0 | 2-3 天 |
| **合计** | | **10-14 天** | **3-4 天** | **13-18 天** |

Phase 1+2 完成后即可投入使用（无 UI 通过 API 操作），Phase 3 补齐体验。

---

## 9. 风险与注意事项

| 风险 | 应对 |
|------|------|
| aide --check 耗时长（12分钟+） | 插件设置足够的超时（默认 30 分钟），任务异步执行 |
| 首次 check 事件量巨大 | 首次执行先 init 新快照再 check，或提供"仅初始化"模式 |
| 业务日志噪音 | 默认策略排除 /var/log 和常见业务日志路径 |
| CentOS 7 vs Rocky 9 AIDE 版本差异 | parser.go 兼容两种输出格式（AIDE 0.15 vs 0.19） |
| aide --update 后丢失变更历史 | 更新快照前确保事件已上报到服务端 |
| 磁盘空间 | aide.db.gz 约 5-15MB，控制 aide.conf 监控范围 |
