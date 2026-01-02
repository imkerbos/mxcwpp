-- ========================================
-- 修复组件包重复记录问题
-- ========================================
-- 1. 删除重复的包记录（保留最新的）
-- 2. 添加唯一索引防止未来重复
-- ========================================

USE mxsec;

-- ========================================
-- Step 1: 查看当前重复记录
-- ========================================
SELECT
    '当前重复记录:' AS info;

SELECT
    cp.id,
    c.name AS component,
    cv.version,
    cp.arch,
    cp.pkg_type,
    cp.sha256,
    cp.uploaded_at
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.category = 'plugin'
  AND cv.version = '1.0.6'
ORDER BY c.name, cp.arch, cp.uploaded_at;

-- ========================================
-- Step 2: 删除重复记录（保留最新的）
-- ========================================
-- 使用子查询找出要保留的记录（每个组合中 uploaded_at 最新的）
-- 然后删除其他记录

SELECT
    '开始删除重复记录...' AS info;

-- 删除 baseline 1.0.6 的旧记录
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline'
          AND cv.version = '1.0.6'
          AND cp.id NOT IN (
              -- 保留每个 (version_id, pkg_type, arch) 组合中最新的记录
              SELECT MAX(cp2.id)
              FROM component_packages cp2
              JOIN component_versions cv2 ON cp2.version_id = cv2.id
              JOIN components c2 ON cv2.component_id = c2.id
              WHERE c2.name = 'baseline'
                AND cv2.version = '1.0.6'
              GROUP BY cp2.version_id, cp2.pkg_type, cp2.arch
          )
    ) AS tmp
);

-- 删除 collector 1.0.6 的旧记录
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector'
          AND cv.version = '1.0.6'
          AND cp.id NOT IN (
              -- 保留每个 (version_id, pkg_type, arch) 组合中最新的记录
              SELECT MAX(cp2.id)
              FROM component_packages cp2
              JOIN component_versions cv2 ON cp2.version_id = cv2.id
              JOIN components c2 ON cv2.component_id = c2.id
              WHERE c2.name = 'collector'
                AND cv2.version = '1.0.6'
              GROUP BY cp2.version_id, cp2.pkg_type, cp2.arch
          )
    ) AS tmp
);

SELECT
    '删除完成！' AS info;

-- ========================================
-- Step 3: 验证清理结果
-- ========================================
SELECT
    '清理后的 1.0.6 版本记录:' AS info;

SELECT
    c.name AS component,
    cv.version,
    cp.arch,
    cp.pkg_type,
    cp.sha256,
    cp.uploaded_at
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.category = 'plugin'
  AND cv.version = '1.0.6'
ORDER BY c.name, cp.arch;

-- ========================================
-- Step 4: 检查是否还有其他版本的重复记录
-- ========================================
SELECT
    '检查所有版本的重复记录:' AS info;

SELECT
    c.name AS component,
    cv.version,
    cp.arch,
    cp.pkg_type,
    COUNT(*) AS count
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE cp.deleted_at IS NULL
GROUP BY c.name, cv.version, cp.arch, cp.pkg_type
HAVING COUNT(*) > 1;

-- ========================================
-- Step 5: 添加唯一索引（防止未来重复）
-- ========================================
SELECT
    '添加唯一索引...' AS info;

-- 检查索引是否已存在
SELECT COUNT(*) INTO @index_exists
FROM information_schema.STATISTICS
WHERE table_schema = 'mxsec'
  AND table_name = 'component_packages'
  AND index_name = 'idx_unique_package';

-- 如果索引不存在，则创建
SET @sql = IF(
    @index_exists = 0,
    'ALTER TABLE component_packages ADD UNIQUE INDEX idx_unique_package (version_id, pkg_type, arch, deleted_at)',
    'SELECT ''唯一索引已存在'' AS info'
);

PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========================================
-- Step 6: 验证最终状态
-- ========================================
SELECT
    '最终验证 - plugin_configs 表:' AS info;

SELECT name, version, sha256, enabled, updated_at
FROM plugin_configs
ORDER BY name;

SELECT
    '完成！' AS info;
