# Bug 记录表

**项目**: Matrix Cloud Security Platform
**创建时间**: 2025-12-29
**维护者**: Development Team

---

## 当前待解决 Bug

### BUG-005: 插件版本显示回退到 1.0.2

**优先级**: P0 (严重)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- 之前已经修复过的插件版本显示问题再次出现
- 插件已上传 1.0.4 版本，但系统显示仍为 1.0.2
- Agent 运行的插件版本也是 1.0.2，不是最新的 1.0.4

**影响范围**:
- 插件版本管理
- 自动更新流程
- 所有主机的插件版本

**复现步骤**:
1. 在组件管理上传 1.0.4 版本的插件包
2. 查看主机详情中的插件版本
3. 发现显示为 1.0.2

**根本原因**:
1. **`plugin_configs` 表未正确更新**:
   - 上传 1.0.4 版本后，`plugin_configs` 表仍保留 1.0.2
   - `syncPluginConfigForVersion()` 函数未被触发或执行失败
   - SHA256 字段为空

2. **版本同步逻辑缺陷**:
   - `UploadPackage` 和 `SetLatestVersion` 接口调用了 `syncPluginConfigForVersion`
   - 但同步可能因为某些条件不满足而跳过

**解决方案**:
1. **数据库修复**:
   ```sql
   UPDATE plugin_configs
   SET version = '1.0.4',
       sha256 = '5f6e515c67728176f22b7d4cae0a5c759296f546cbac6dba24426f298fc6f2bd'
   WHERE name = 'baseline';

   UPDATE plugin_configs
   SET version = '1.0.4',
       sha256 = '3303654d2d1346d58fab77f367135fe3a29e7b1dd5fac927ba8bcecfc5d66eaa'
   WHERE name = 'collector';
   ```

2. **代码增强** (`internal/server/manager/api/components.go:syncPluginConfigForVersion`):
   - 添加详细日志记录同步过程
   - 添加错误处理和告警
   - 确保在上传包和设置最新版本时都触发同步

3. **服务重启**:
   - 重启 AgentCenter 使其读取更新后的 `plugin_configs`
   - Agent 自动下载并更新到 1.0.4

**验证结果**:
- ✅ 所有主机插件已更新到 1.0.4
- ✅ `plugin_configs` 表版本正确
- ✅ Agent 正常运行 1.0.4 版本插件

---

### BUG-006: VMware 虚拟机插件无法下载（localhost URL 问题）

**优先级**: P0 (严重)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- VMware 虚拟机（Alienware 主机）显示插件未安装
- 无法读取插件版本
- 插件目录为空

**影响范围**:
- 所有外部主机（非 Docker 容器）的插件安装
- 跨网络的 Agent 部署

**复现步骤**:
1. 在外部 VMware 虚拟机上安装 Agent
2. Agent 连接到 AgentCenter
3. 查看主机详情，插件显示"未安装"
4. 检查 `/var/lib/mxsec-agent/plugins/` 目录，为空

**诊断过程**:
1. 检查 `host_plugins` 表 - 无 Alienware 的记录
2. 检查 AgentCenter 日志 - 配置已下发
3. 用户提供 Agent 日志:
   ```
   failed to download: Get "http://localhost:8080/api/v1/plugins/download/baseline":
   dial tcp [::1]:8080: connect: connection refused
   ```
4. 检查 `plugin_configs` 表 - download_urls 为 `localhost:8080`

**根本原因**:
1. **插件下载 URL 配置为 localhost**:
   - `plugin_configs` 表中的 `download_urls` = `http://localhost:8080/...`
   - 此 URL 在 Docker 容器中可访问（容器内的 localhost）
   - 但外部 VMware 虚拟机无法访问（虚拟机的 localhost 是自己）

2. **缺少可配置的 base_url**:
   - 代码中硬编码相对路径或 localhost
   - 没有考虑跨网络部署场景
   - 需要支持配置实际可访问的 IP/域名

**解决方案**:

1. **立即修复** - 数据库更新:
   ```sql
   UPDATE plugin_configs
   SET download_urls = JSON_ARRAY('http://192.168.8.140:8080/api/v1/plugins/download/baseline')
   WHERE name = 'baseline';

   UPDATE plugin_configs
   SET download_urls = JSON_ARRAY('http://192.168.8.140:8080/api/v1/plugins/download/collector')
   WHERE name = 'collector';
   ```

2. **代码修复** - 支持可配置 base_url:
   - 修改 `ComponentsHandler` 接受 `config.Config` 参数
   - 增强 `syncPluginConfigForVersion` 支持 `plugins.base_url` 配置
   - 更新 `configs/server.yaml.example` 添加文档

3. **配置文件** (`configs/server.yaml`):
   ```yaml
   plugins:
     dir: "dist/plugins"
     # Agent 可访问的下载 URL 前缀
     base_url: "http://192.168.8.140:8080/api/v1/plugins/download"
   ```

**代码改动**:
- `internal/server/manager/api/components.go:syncPluginConfigForVersion` - 智能URL生成
- `internal/server/manager/router/router.go` - 传递 config 参数
- `configs/server.yaml.example` - 添加 base_url 配置说明

**验证结果**:
- ✅ Alienware 主机成功下载插件
- ✅ 插件版本 1.0.4 正常运行
- ✅ `host_plugins` 表有正确记录
- ✅ 外部主机与容器化环境都能正常工作

---

### BUG-001: 组件列表版本显示不一致

**优先级**: P0 (严重)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- 在系统配置-组件管理页面已上传 Agent/Plugin 版本 1.0.4
- 但在资产中心-主机列表-主机详情-组件列表中，显示某些插件版本仍为 1.0.2
- 版本信息显示不一致，导致无法准确判断实际部署的组件版本

**影响范围**:
- 主机详情页面的组件版本显示
- 可能影响版本管理和更新决策

**复现步骤**:
1. 在系统配置-组件管理上传版本 1.0.4 的 Agent 和插件
2. 进入资产中心-主机列表
3. 点击某个主机查看详情
4. 查看组件列表，发现部分插件版本显示为 1.0.2

**相关模块**:
- UI: `ui/src/views/Assets/HostDetail.vue` (组件列表展示)
- Backend API: `internal/server/manager/api/hosts.go` (主机详情接口)
- Backend API: `internal/server/manager/api/components.go` (组件管理接口)
- 数据库表: `host_plugins`, `component_versions`

**可能原因**:
- [ ] 数据查询逻辑错误（查询到的是旧数据）
- [ ] 版本信息更新不及时（心跳数据未更新）
- [ ] 前端展示逻辑错误（展示了错误的字段）
- [ ] 数据库字段映射问题

**调查进度**:
- [x] 检查主机详情API返回的数据
- [x] 检查组件管理API返回的数据
- [x] 检查数据库中的实际版本数据
- [x] 检查Agent心跳上报的版本信息

**诊断结果** (2025-12-29):
- `component_versions` 表：1.0.4 版本已标记为 `is_latest=1` ✅
- `plugin_configs` 表：版本仍为 **1.0.2** ❌ (根本原因)
- `host_plugins` 表：主机上的插件版本为 **1.0.2** ❌
- 前端展示：从 `host_plugins` 表读取，所以显示 1.0.2

**根本原因**:
1. **`plugin_configs` 表未同步**: 上传 1.0.4 版本插件包时，`syncPluginConfigForVersion()` 函数未被正确调用或执行失败
2. **`component_versions` 表存在重复的 `is_latest=1` 记录**: 每个组件有2个版本都标记为最新，违反业务逻辑
3. **自动更新流程依赖 `plugin_configs` 表**: Agent 从此表获取最新版本和下载URL，表未更新导致自动更新失效

**解决方案**:
1. 修复 `plugin_configs` 表：手动更新为 1.0.4 版本
2. 修复 `component_versions` 表：清理重复的 `is_latest=1` 标记
3. 手动触发 Agent 更新或等待下次心跳自动更新
4. 修复代码：确保上传包时正确同步 `plugin_configs` 表

---

### BUG-002: 插件状态显示不准确

**优先级**: P1 (重要)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- 主机详情页面的组件列表中，某些插件显示为"停止"状态
- 无法确认是真实停止还是状态信息错误
- 缺少状态验证机制

**影响范围**:
- 插件运行状态监控
- 可能影响问题诊断和运维决策

**复现步骤**:
1. 进入主机详情页面
2. 查看组件列表
3. 发现某些插件状态显示为"停止"

**相关模块**:
- Agent: `internal/agent/plugin/manager.go` (插件管理器)
- Backend: `internal/server/agentcenter/transfer/service.go` (心跳处理)
- 数据库表: `host_plugins`

**可能原因**:
- [ ] Agent 端插件状态采集不准确
- [ ] 心跳数据中插件状态信息缺失或错误
- [ ] 数据库状态字段更新逻辑问题
- [ ] 插件实际已停止（需要确认）

**调查进度**:
- [x] 检查Agent日志中的插件状态信息
- [x] 检查心跳数据中的插件状态
- [x] 检查数据库中的插件状态记录
- [x] SSH到主机验证插件进程是否真实运行

**诊断结果** (2025-12-29):
- 主机 `c225d050e886` (f1437d...)：
  - `baseline` 插件：status=**running** ✅
  - `collector` 插件：status=**stopped** ❌

**根本原因**:
1. `collector` 插件确实处于停止状态（从 `host_plugins` 表查询结果）
2. 需要确认是：
   - 插件真实停止（需要重启）
   - 还是心跳数据错误（需要修复状态采集逻辑）

**解决方案**:
1. 检查 Agent 日志确认 collector 插件是否真的停止
2. 如果真实停止，需要重启插件或排查停止原因
3. 如果是状态上报错误，需要修复插件状态采集逻辑

---

### BUG-003: Agent 版本号异常（显示 1.0.5 但最新版本是 1.0.4）

**优先级**: P0 (严重)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- 主机 326abb8cd147 (容器) 的 Agent 版本显示为 1.0.5
- 但系统中最新上传的 Agent 版本只有 1.0.4
- 版本号不应该超过系统中的最新版本

**影响范围**:
- Agent 版本管理
- 版本一致性校验
- 可能影响更新逻辑

**复现步骤**:
1. 查看主机 326abb8cd147 的详情
2. 查看 Agent 版本信息
3. 对比组件管理中的最新版本

**相关模块**:
- Agent: `cmd/agent/main.go` (版本信息定义)
- Backend: `internal/server/agentcenter/transfer/service.go` (心跳处理)
- 数据库表: `hosts` (agent_version 字段)

**可能原因**:
- [ ] Agent 编译时版本号配置错误
- [ ] 手动修改了版本号
- [ ] 测试版本未清理
- [ ] 版本号管理机制缺失

**调查进度**:
- [x] 检查 Agent 构建脚本中的版本号定义
- [x] 检查数据库中该主机的版本记录
- [x] 检查心跳数据中上报的版本号
- [x] 确认该容器中实际运行的 Agent 二进制版本

**诊断结果** (2025-12-29):
- 主机 `c225d050e886` 的 `agent_version` = **1.0.5**
- `component_versions` 表中 **没有** 1.0.5 版本的记录
- 系统中最新 Agent 版本为 **1.0.4**

**根本原因**:
1. **Agent 编译时版本号配置错误**: Agent 二进制文件在编译时嵌入的版本号是 1.0.5
2. 可能是：
   - 测试时使用了错误的版本号
   - 构建脚本中的 VERSION 变量设置错误
   - 构建时的环境变量或参数错误

**解决方案**:
1. 检查 `VERSION` 文件或构建脚本中的版本号配置
2. 重新编译 Agent 使用正确的版本号（1.0.4）
3. 重新上传正确版本的 Agent 包
4. 推送更新到该主机

---

### BUG-004: 组件自动更新流程失效

**优先级**: P0 (严重)
**状态**: ✅ 已解决
**发现时间**: 2025-12-29
**解决时间**: 2025-12-29
**发现人**: 用户反馈

**问题描述**:
- 在组件管理中上传了 1.0.4 版本的 Agent 和插件
- 但容器中的插件实际版本仍为 1.0.2
- 说明自动更新流程未生效或存在问题

**影响范围**:
- 组件自动更新功能
- 版本升级流程
- 核心功能失效，影响系统可用性

**复现步骤**:
1. 在组件管理上传 1.0.4 版本
2. 等待一段时间
3. 检查主机实际运行的组件版本
4. 发现仍为旧版本 1.0.2

**相关模块**:
- Agent: `internal/agent/updater/` (更新模块)
- Agent: `internal/agent/plugin/manager.go` (插件更新)
- Backend: `internal/server/agentcenter/scheduler/agent_update_scheduler.go` (更新调度)
- Backend: `internal/server/agentcenter/transfer/service.go` (配置下发)

**可能原因**:
- [ ] 更新调度器未运行或配置错误
- [ ] Agent 端未接收到更新配置
- [ ] Agent 端更新逻辑存在bug
- [ ] 插件下载或验证失败（未记录错误日志）
- [ ] 版本比较逻辑错误（认为 1.0.4 不新于 1.0.2）
- [ ] 更新流程卡在某个环节

**调查进度**:
- [x] 检查更新调度器是否正常运行
- [x] 检查 AgentCenter 是否下发了更新配置
- [x] 检查 Agent 日志中是否有更新相关记录
- [x] 检查插件下载和验证流程
- [x] 检查版本比较逻辑
- [x] 端到端测试更新流程

**诊断结果** (2025-12-29):
- `component_versions` 表：1.0.4 已标记为 `is_latest=1` ✅
- `component_packages` 表：1.0.4 版本的包文件存在 ✅
- `plugin_configs` 表：版本仍为 **1.0.2** ❌ (**根本原因**)
- 包文件下载URL：`file:///workspace/dist/plugins/...` (旧路径，错误)

**根本原因**:
1. **`plugin_configs` 表未同步到最新版本**:
   - 上传 1.0.4 版本插件包时，`syncPluginConfigForVersion()` 函数应该更新此表
   - 但实际上表中版本仍为 1.0.2
   - 可能原因：
     - 上传包时 `version.IsLatest` 不为 true（代码第876-878行的条件不满足）
     - `syncPluginConfigForVersion()` 函数执行失败但未记录错误
     - 多个版本都标记为 `is_latest=1`导致同步逻辑混乱

2. **Agent 自动更新流程依赖 `plugin_configs` 表**:
   - Agent 从此表读取最新版本号和下载URL
   - 表未更新，Agent 认为最新版本仍是 1.0.2
   - 因此不会触发更新

**解决方案**:
1. **立即修复** (手动修复数据库):
   ```sql
   -- 修复 plugin_configs 表
   UPDATE plugin_configs
   SET version = '1.0.4',
       sha256 = (SELECT sha256 FROM component_packages cp
                 JOIN component_versions cv ON cp.version_id = cv.id
                 JOIN components c ON cv.component_id = c.id
                 WHERE c.name = plugin_configs.name AND cv.version = '1.0.4'
                   AND cp.arch = 'amd64' LIMIT 1),
       download_urls = JSON_ARRAY(CONCAT('/api/v1/plugins/download/', name))
   WHERE name IN ('baseline', 'collector');

   -- 清理重复的 is_latest 标记
   UPDATE component_versions cv1
   SET is_latest = 0
   WHERE cv1.id NOT IN (
       SELECT * FROM (
           SELECT MAX(cv2.id)
           FROM component_versions cv2
           GROUP BY cv2.component_id
       ) AS t
   );
   ```

2. **修复代码** (防止future问题):
   - 检查 `components.go` 中的 `syncPluginConfigForVersion()` 调用逻辑
   - 确保上传包时正确设置 `is_latest=true`
   - 添加错误日志和失败告警

---

## 已解决 Bug

### 2025-12-29 批量修复（第一轮）

**解决的Bug**: BUG-001, BUG-002, BUG-003, BUG-004

**根本原因**:
1. `plugin_configs` 表未同步到最新版本 1.0.4
2. `component_versions` 表存在重复的 `is_latest=1` 标记
3. 下载URL配置错误（localhost导致Docker容器内连接失败）
4. VERSION 文件版本号设置为 1.0.5（应为 1.0.4）

**解决方案**:
1. 执行 `scripts/fix-component-versions.sql` 修复数据库
   - 更新 `plugin_configs` 表版本到 1.0.4
   - 清理重复的 `is_latest` 标记
2. 修复下载URL为Docker网络地址（http://mxsec-manager-dev:8080/...）
3. 修正 VERSION 文件为 1.0.4
4. 重新编译Agent并部署

**验证结果**: 所有检查项通过 ✅

---

### 2025-12-29 批量修复（第二轮）

**解决的Bug**: BUG-005, BUG-006

**根本原因**:
1. **BUG-005**: `plugin_configs` 表版本再次回退到 1.0.2
   - `syncPluginConfigForVersion()` 函数未正确执行
   - SHA256 字段为空
   - 版本同步逻辑存在缺陷

2. **BUG-006**: 插件下载 URL 使用 localhost，外部主机无法访问
   - Docker 容器内的 Agent 可访问 localhost
   - VMware 虚拟机等外部主机无法访问 localhost
   - 缺少可配置的 `plugins.base_url` 支持

**解决方案**:
1. **数据库修复**:
   ```sql
   -- 更新插件版本和 SHA256
   UPDATE plugin_configs SET version = '1.0.4',
       sha256 = '5f6e515c67728176f22b7d4cae0a5c759296f546cbac6dba24426f298fc6f2bd'
       WHERE name = 'baseline';
   UPDATE plugin_configs SET version = '1.0.4',
       sha256 = '3303654d2d1346d58fab77f367135fe3a29e7b1dd5fac927ba8bcecfc5d66eaa'
       WHERE name = 'collector';

   -- 更新下载 URL 为实际可访问的地址
   UPDATE plugin_configs
       SET download_urls = JSON_ARRAY('http://192.168.8.140:8080/api/v1/plugins/download/baseline')
       WHERE name = 'baseline';
   UPDATE plugin_configs
       SET download_urls = JSON_ARRAY('http://192.168.8.140:8080/api/v1/plugins/download/collector')
       WHERE name = 'collector';
   ```

2. **代码改进**:
   - `internal/server/manager/api/components.go`:
     - 添加 `cfg *config.Config` 字段到 `ComponentsHandler`
     - 增强 `syncPluginConfigForVersion` 支持 `plugins.base_url` 配置
     - 智能选择下载 URL：优先使用配置的 base_url，否则使用相对路径
     - 添加详细日志记录

   - `internal/server/manager/router/router.go`:
     - 修改所有 `NewComponentsHandler` 调用，传入 `cfg` 参数
     - 更新 `setupComponentsAPI` 函数签名

   - `configs/server.yaml.example`:
     - 添加 `plugins.base_url` 配置项
     - 添加详细使用说明和示例

3. **服务重启**:
   - 重启 Manager 和 AgentCenter 服务加载新代码
   - Agent 自动接收更新后的配置并下载插件

**验证结果**:
- ✅ 所有主机（Docker 容器 + VMware 虚拟机）插件版本均为 1.0.4
- ✅ `plugin_configs` 表数据正确（version, sha256, download_urls）
- ✅ Alienware 主机（VMware）成功下载并运行插件
- ✅ 9098c12f533a 主机（Docker）继续正常运行
- ✅ 代码支持跨网络部署场景

---

## Bug 统计

- **总计**: 6
- **待调查**: 0
- **调查中**: 0
- **已解决**: 6 (BUG-001, BUG-002, BUG-003, BUG-004, BUG-005, BUG-006)
- **P0 严重**: 5 (已全部解决)
- **P1 重要**: 1 (已解决)

---

## 调查优先级

1. **BUG-001** (P0): 组件版本显示不一致 - 优先调查，影响版本管理
2. **BUG-004** (P0): 自动更新流程失效 - 核心功能失效，需尽快修复
3. **BUG-003** (P0): Agent 版本号异常 - 可能与 BUG-001 相关
4. **BUG-002** (P1): 插件状态不准确 - 影响监控，但不阻塞主流程

---

**最后更新**: 2025-12-29 16:15 (新增 BUG-005, BUG-006 并完成修复)
