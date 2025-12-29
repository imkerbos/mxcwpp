-- 组件版本问题修复脚本
-- 修复 BUG-001 和 BUG-004：plugin_configs 表版本不一致和自动更新流程失效
--
-- 使用方法:
--   mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec < scripts/fix-component-versions.sql
--
-- 警告: 此脚本会修改数据库，请先备份！
-- 备份命令: mysqldump -h127.0.0.1 -P3306 -uroot -p123456 mxsec > backup_before_fix.sql

SELECT '========================================' AS '';
SELECT '组件版本问题修复脚本' AS '';
SELECT '========================================' AS '';
SELECT CONCAT('执行时间: ', NOW()) AS '';
SELECT '' AS '';

-- ========================================
-- Step 1: 备份当前数据
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[Step 1/4] 显示当前数据（修复前）' AS '';
SELECT '----------------------------------------' AS '';

SELECT '当前 plugin_configs 表数据:' AS '';
SELECT name, version, SUBSTRING(sha256, 1, 16) AS 'sha256(前16位)', enabled, download_urls
FROM plugin_configs
WHERE name IN ('baseline', 'collector');

SELECT '' AS '';
SELECT '当前 component_versions 表中 is_latest=1 的记录:' AS '';
SELECT cv.id, c.name, cv.version, cv.is_latest, cv.created_at
FROM component_versions cv
JOIN components c ON cv.component_id = c.id
WHERE cv.is_latest = 1
ORDER BY c.name, cv.created_at DESC;

SELECT '' AS '';

-- ========================================
-- Step 2: 修复 plugin_configs 表
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[Step 2/4] 修复 plugin_configs 表' AS '';
SELECT '----------------------------------------' AS '';

-- 更新 baseline 插件配置
UPDATE plugin_configs
SET version = '1.0.4',
    sha256 = (
        SELECT cp.sha256
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline'
          AND cv.version = '1.0.4'
          AND cp.arch = 'amd64'
          AND cp.enabled = 1
        ORDER BY cv.created_at DESC
        LIMIT 1
    ),
    download_urls = JSON_ARRAY('/api/v1/plugins/download/baseline'),
    detail = CONCAT('{"updated_at": "', NOW(), '", "updated_by": "fix_script"}')
WHERE name = 'baseline';

-- 更新 collector 插件配置
UPDATE plugin_configs
SET version = '1.0.4',
    sha256 = (
        SELECT cp.sha256
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector'
          AND cv.version = '1.0.4'
          AND cp.arch = 'amd64'
          AND cp.enabled = 1
        ORDER BY cv.created_at DESC
        LIMIT 1
    ),
    download_urls = JSON_ARRAY('/api/v1/plugins/download/collector'),
    detail = CONCAT('{"updated_at": "', NOW(), '", "updated_by": "fix_script"}')
WHERE name = 'collector';

SELECT CONCAT('✓ 已更新 plugin_configs 表，受影响行数: ', ROW_COUNT()) AS '';
SELECT '' AS '';

-- ========================================
-- Step 3: 清理重复的 is_latest 标记
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[Step 3/4] 清理重复的 is_latest=1 标记' AS '';
SELECT '----------------------------------------' AS '';

-- 为每个组件保留最新的一个版本，其他的 is_latest 设为 0
UPDATE component_versions cv1
SET is_latest = 0
WHERE cv1.is_latest = 1
  AND cv1.id NOT IN (
      -- 对于每个组件，选择最新创建的版本（ID 最大的）
      SELECT latest.id FROM (
          SELECT MAX(cv2.id) AS id
          FROM component_versions cv2
          WHERE cv2.is_latest = 1
          GROUP BY cv2.component_id
      ) AS latest
  );

SELECT CONCAT('✓ 已清理重复的 is_latest 标记，受影响行数: ', ROW_COUNT()) AS '';
SELECT '' AS '';

-- ========================================
-- Step 4: 验证修复结果
-- ========================================
SELECT '----------------------------------------' AS '';
SELECT '[Step 4/4] 验证修复结果' AS '';
SELECT '----------------------------------------' AS '';

SELECT '修复后 plugin_configs 表数据:' AS '';
SELECT name, version, SUBSTRING(sha256, 1, 16) AS 'sha256(前16位)', enabled, download_urls
FROM plugin_configs
WHERE name IN ('baseline', 'collector');

SELECT '' AS '';
SELECT '修复后 component_versions 表中 is_latest=1 的记录:' AS '';
SELECT cv.id, c.name, cv.version, cv.is_latest, cv.created_at
FROM component_versions cv
JOIN components c ON cv.component_id = c.id
WHERE cv.is_latest = 1
ORDER BY c.name, cv.created_at DESC;

SELECT '' AS '';

-- 验证修复是否成功
SELECT
    CASE
        WHEN (SELECT COUNT(*) FROM plugin_configs
              WHERE name IN ('baseline', 'collector') AND version = '1.0.4') = 2
        THEN '✓ plugin_configs 表已成功更新到 1.0.4'
        ELSE '✗ plugin_configs 表更新失败，请检查错误'
    END AS '修复状态';

SELECT
    CASE
        WHEN (SELECT COUNT(DISTINCT component_id) FROM component_versions WHERE is_latest = 1) =
             (SELECT COUNT(*) FROM component_versions WHERE is_latest = 1)
        THEN '✓ component_versions 表 is_latest 标记已修复（每个组件只有一个最新版本）'
        ELSE '✗ component_versions 表仍存在重复的 is_latest 标记'
    END AS '修复状态';

SELECT '' AS '';
SELECT '========================================' AS '';
SELECT '修复完成！' AS '';
SELECT '========================================' AS '';
SELECT '下一步操作:' AS '';
SELECT '1. 等待 Agent 下次心跳时自动更新插件（默认每60秒）' AS '';
SELECT '2. 或手动推送更新: 在系统配置-组件管理页面点击"推送更新"' AS '';
SELECT '3. 查看主机详情页面的组件列表，验证版本是否更新到 1.0.4' AS '';
SELECT '' AS '';
