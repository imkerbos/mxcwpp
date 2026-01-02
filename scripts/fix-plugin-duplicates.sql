-- 修复插件 1.0.6 版本重复记录问题
-- 删除旧版本的重复记录，保留新编译的插件（正确的版本号）

USE mxsec;

-- 1. 查看当前的重复记录
SELECT
    cp.id,
    c.name,
    cv.version,
    cp.arch,
    cp.sha256,
    cp.file_path,
    cp.created_at
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.category = 'plugin'
  AND cv.version = '1.0.6'
  AND cp.arch IN ('amd64', 'arm64')
ORDER BY c.name, cp.arch, cp.created_at;

-- 2. 删除 baseline 1.0.6 amd64 的旧版本（SHA256: f99c39604a9be65c2400d242f2cafa0f2b36c7a6cc03ba084da0ca847da66b17）
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline'
          AND cv.version = '1.0.6'
          AND cp.arch = 'amd64'
          AND cp.sha256 = 'f99c39604a9be65c2400d242f2cafa0f2b36c7a6cc03ba084da0ca847da66b17'
    ) AS tmp
);

-- 3. 删除 baseline 1.0.6 arm64 的旧版本（SHA256: 739aea042e92e4609cfbb9dd886c6bcbcbcd7881c91db9345c6489ca7a3fa628）
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'baseline'
          AND cv.version = '1.0.6'
          AND cp.arch = 'arm64'
          AND cp.sha256 = '739aea042e92e4609cfbb9dd886c6bcbcbcd7881c91db9345c6489ca7a3fa628'
    ) AS tmp
);

-- 4. 删除 collector 1.0.6 amd64 的旧版本（SHA256: b0b7b497091c00c48af2a85a6d86986dc6ae25a87ac135cc404424f884b6563b）
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector'
          AND cv.version = '1.0.6'
          AND cp.arch = 'amd64'
          AND cp.sha256 = 'b0b7b497091c00c48af2a85a6d86986dc6ae25a87ac135cc404424f884b6563b'
    ) AS tmp
);

-- 5. 删除 collector 1.0.6 arm64 的旧版本（SHA256: c5bd0799bc89f320b369ba7de12bacda34371877d02c35a2e7dedb5623230e00）
DELETE FROM component_packages
WHERE id IN (
    SELECT id FROM (
        SELECT cp.id
        FROM component_packages cp
        JOIN component_versions cv ON cp.version_id = cv.id
        JOIN components c ON cv.component_id = c.id
        WHERE c.name = 'collector'
          AND cv.version = '1.0.6'
          AND cp.arch = 'arm64'
          AND cp.sha256 = 'c5bd0799bc89f320b369ba7de12bacda34371877d02c35a2e7dedb5623230e00'
    ) AS tmp
);

-- 6. 验证清理结果 - 应该每个插件每个架构只有一条 1.0.6 记录
SELECT
    c.name,
    cv.version,
    cp.arch,
    cp.sha256,
    cp.file_path,
    cp.created_at
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.category = 'plugin'
  AND cv.version = '1.0.6'
ORDER BY c.name, cp.arch;

-- 7. 验证 plugin_configs 表的版本和 SHA256
SELECT name, version, sha256, enabled, updated_at
FROM plugin_configs
ORDER BY name;
