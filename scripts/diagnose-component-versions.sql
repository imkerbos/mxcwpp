-- 组件版本诊断 SQL 脚本
-- 用于诊断和调查组件版本显示不一致的问题
--
-- 使用方法:
--   mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec_platform < scripts/diagnose-component-versions.sql
--
-- 或者在 MySQL 客户端中执行:
--   source scripts/diagnose-component-versions.sql;

-- 设置主机ID变量（修改这里来诊断不同的主机）
SET @host_id = '326abb8cd147';

SELECT '========================================' AS '';
SELECT 'Bug 诊断报告' AS '';
SELECT '========================================' AS '';
SELECT CONCAT('诊断主机: ', @host_id) AS '';
SELECT CONCAT('诊断时间: ', NOW()) AS '';
SELECT '' AS '';

-- ========================================
-- [1] 主机基本信息
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[1/7] 主机基本信息' AS '';
SELECT '----------------------------------------' AS '';

SELECT
    host_id AS '主机ID',
    hostname AS '主机名',
    os_family AS '操作系统',
    os_version AS 'OS版本',
    agent_version AS 'Agent版本',
    status AS '状态',
    is_container AS '是否容器',
    container_id AS '容器ID',
    last_heartbeat AS '最后心跳时间'
FROM hosts
WHERE host_id = @host_id;

SELECT '' AS '';

-- ========================================
-- [2] 组件版本管理表 (component_versions)
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[2/7] 组件版本管理表 (component_versions)' AS '';
SELECT '说明: is_latest=1 的版本应该是最新版本' AS '';
SELECT '----------------------------------------' AS '';

SELECT
    cv.id AS 'ID',
    c.name AS '组件名称',
    c.category AS '分类',
    cv.version AS '版本号',
    CASE WHEN cv.is_latest = 1 THEN '是' ELSE '否' END AS '是否最新',
    cv.created_at AS '创建时间'
FROM component_versions cv
JOIN components c ON cv.component_id = c.id
WHERE c.name IN ('agent', 'baseline', 'collector')
ORDER BY c.name, cv.created_at DESC;

SELECT '' AS '';

-- ========================================
-- [3] 插件配置表 (plugin_configs)
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[3/7] 插件配置表 (plugin_configs)' AS '';
SELECT '说明: 这是 Agent 端用于自动更新的配置' AS '';
SELECT '----------------------------------------' AS '';

SELECT
    name AS '插件名称',
    version AS '版本号',
    SUBSTRING(sha256, 1, 16) AS 'SHA256(前16位)',
    CASE WHEN enabled = 1 THEN '是' ELSE '否' END AS '是否启用',
    download_urls AS '下载URL'
FROM plugin_configs
WHERE name IN ('baseline', 'collector')
ORDER BY name;

SELECT '' AS '';

-- ========================================
-- [4] 主机插件表 (host_plugins)
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[4/7] 主机插件表 (host_plugins)' AS '';
SELECT '说明: 这是从 Agent 心跳上报的插件状态' AS '';
SELECT '----------------------------------------' AS '';

SELECT
    id AS 'ID',
    host_id AS '主机ID',
    name AS '插件名称',
    version AS '当前版本',
    status AS '状态',
    start_time AS '启动时间',
    updated_at AS '更新时间',
    CASE WHEN deleted_at IS NULL THEN '否' ELSE '是' END AS '是否删除'
FROM host_plugins
WHERE host_id = @host_id
ORDER BY name;

SELECT '' AS '';

-- ========================================
-- [5] 组件包表 (component_packages)
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[5/7] 组件包表 (component_packages)' AS '';
SELECT '说明: 检查是否有对应版本的包文件' AS '';
SELECT '----------------------------------------' AS '';

SELECT
    cp.id AS 'ID',
    c.name AS '组件名称',
    cv.version AS '版本号',
    cp.arch AS '架构',
    cp.pkg_type AS '包类型',
    ROUND(cp.file_size / 1024 / 1024, 2) AS '文件大小(MB)',
    SUBSTRING(cp.sha256, 1, 16) AS 'SHA256(前16位)',
    CASE WHEN cp.enabled = 1 THEN '是' ELSE '否' END AS '是否启用',
    cp.uploaded_at AS '上传时间'
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.name IN ('agent', 'baseline', 'collector')
  AND cp.enabled = 1
ORDER BY c.name, cv.created_at DESC, cp.arch;

SELECT '' AS '';

-- ========================================
-- [6] 版本对比分析
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[6/7] 版本对比分析' AS '';
SELECT '----------------------------------------' AS '';

-- Agent 版本对比
SELECT
    'Agent' AS '组件',
    (SELECT agent_version FROM hosts WHERE host_id = @host_id) AS '主机当前版本',
    (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
     WHERE c.name = 'agent' AND cv.is_latest = 1 LIMIT 1) AS '系统最新版本',
    CASE
        WHEN (SELECT agent_version FROM hosts WHERE host_id = @host_id) =
             (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
              WHERE c.name = 'agent' AND cv.is_latest = 1 LIMIT 1)
        THEN '一致'
        ELSE '不一致 ⚠️'
    END AS '状态';

-- Baseline 插件版本对比
SELECT
    'baseline' AS '组件',
    (SELECT version FROM host_plugins WHERE host_id = @host_id AND name = 'baseline' LIMIT 1) AS '主机当前版本',
    (SELECT version FROM plugin_configs WHERE name = 'baseline' LIMIT 1) AS 'plugin_configs版本',
    (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
     WHERE c.name = 'baseline' AND cv.is_latest = 1 LIMIT 1) AS 'component_versions最新版本',
    CASE
        WHEN (SELECT version FROM host_plugins WHERE host_id = @host_id AND name = 'baseline' LIMIT 1) =
             (SELECT version FROM plugin_configs WHERE name = 'baseline' LIMIT 1)
        THEN '一致'
        ELSE '不一致 ⚠️'
    END AS '状态';

-- Collector 插件版本对比
SELECT
    'collector' AS '组件',
    (SELECT version FROM host_plugins WHERE host_id = @host_id AND name = 'collector' LIMIT 1) AS '主机当前版本',
    (SELECT version FROM plugin_configs WHERE name = 'collector' LIMIT 1) AS 'plugin_configs版本',
    (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
     WHERE c.name = 'collector' AND cv.is_latest = 1 LIMIT 1) AS 'component_versions最新版本',
    CASE
        WHEN (SELECT version FROM host_plugins WHERE host_id = @host_id AND name = 'collector' LIMIT 1) =
             (SELECT version FROM plugin_configs WHERE name = 'collector' LIMIT 1)
        THEN '一致'
        ELSE '不一致 ⚠️'
    END AS '状态';

SELECT '' AS '';

-- ========================================
-- [7] 诊断结论
-- ========================================
SELECT '========================================' AS '';
SELECT '诊断结论' AS '';
SELECT '========================================' AS '';

-- 检查 Agent 版本异常（BUG-003）
SELECT
    CASE
        WHEN (SELECT agent_version FROM hosts WHERE host_id = @host_id) >
             (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
              WHERE c.name = 'agent' AND cv.is_latest = 1 LIMIT 1)
        THEN CONCAT('【BUG-003】 ⚠️ Agent 版本异常: 主机版本 ',
                    (SELECT agent_version FROM hosts WHERE host_id = @host_id),
                    ' > 系统最新版本 ',
                    (SELECT version FROM component_versions cv JOIN components c ON cv.component_id = c.id
                     WHERE c.name = 'agent' AND cv.is_latest = 1 LIMIT 1))
        ELSE '【BUG-003】 ✓ Agent 版本正常'
    END AS '诊断结果';

-- 检查插件版本不一致（BUG-001）
SELECT
    CASE
        WHEN (SELECT COUNT(*) FROM host_plugins hp
              WHERE hp.host_id = @host_id
                AND hp.version != (SELECT version FROM plugin_configs pc WHERE pc.name = hp.name LIMIT 1)
                AND hp.deleted_at IS NULL) > 0
        THEN CONCAT('【BUG-001】 ⚠️ 发现 ',
                    (SELECT COUNT(*) FROM host_plugins hp
                     WHERE hp.host_id = @host_id
                       AND hp.version != (SELECT version FROM plugin_configs pc WHERE pc.name = hp.name LIMIT 1)
                       AND hp.deleted_at IS NULL),
                    ' 个插件版本不一致')
        ELSE '【BUG-001】 ✓ 插件版本一致'
    END AS '诊断结果';

-- 检查插件状态（BUG-002）
SELECT
    CASE
        WHEN (SELECT COUNT(*) FROM host_plugins WHERE host_id = @host_id AND status = 'stopped' AND deleted_at IS NULL) > 0
        THEN CONCAT('【BUG-002】 ⚠️ 发现 ',
                    (SELECT COUNT(*) FROM host_plugins WHERE host_id = @host_id AND status = 'stopped' AND deleted_at IS NULL),
                    ' 个插件处于停止状态')
        ELSE '【BUG-002】 ✓ 所有插件运行正常'
    END AS '诊断结果';

-- 检查自动更新流程（BUG-004）
SELECT
    CASE
        WHEN NOT EXISTS (
            SELECT 1 FROM component_packages cp
            JOIN component_versions cv ON cp.version_id = cv.id
            JOIN components c ON cv.component_id = c.id
            WHERE c.name IN ('baseline', 'collector')
              AND cv.is_latest = 1
              AND cp.enabled = 1
        )
        THEN '【BUG-004】 ⚠️ 最新版本缺少安装包文件'
        WHEN (SELECT COUNT(*) FROM plugin_configs pc
              WHERE pc.version != (
                  SELECT cv.version FROM component_versions cv
                  JOIN components c ON cv.component_id = c.id
                  WHERE c.name = pc.name AND cv.is_latest = 1 LIMIT 1
              )) > 0
        THEN '【BUG-004】 ⚠️ plugin_configs 表未同步最新版本'
        ELSE '【BUG-004】 ✓ 自动更新配置正常'
    END AS '诊断结果';

SELECT '' AS '';
SELECT '========================================' AS '';
SELECT '建议操作' AS '';
SELECT '========================================' AS '';
SELECT '1. 检查上述诊断结果中标记为 ⚠️ 的项目' AS '';
SELECT '2. 根据诊断结果更新 docs/BUGS.md 文件' AS '';
SELECT '3. 制定修复方案并执行' AS '';
SELECT '4. 如需手动修复数据，请谨慎操作并备份数据库' AS '';
SELECT '' AS '';
