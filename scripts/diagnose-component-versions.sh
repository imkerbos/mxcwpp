#!/bin/bash

# 组件版本诊断脚本
# 用于诊断和调查组件版本显示不一致的问题
#
# 使用方法:
#   ./scripts/diagnose-component-versions.sh [host_id]
#
# 如果不提供 host_id，将诊断容器 326abb8cd147

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认主机ID（问题容器）
HOST_ID="${1:-326abb8cd147}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}组件版本诊断脚本${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "诊断主机: ${YELLOW}${HOST_ID}${NC}\n"

# 检查 MySQL 连接
echo -e "${GREEN}[1/7] 检查数据库连接...${NC}"
if ! mysql -h127.0.0.1 -P3306 -uroot -p123456 -e "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}错误: 无法连接到 MySQL 数据库${NC}"
    echo -e "${YELLOW}提示: 请检查 MySQL 是否运行，以及连接参数是否正确${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 数据库连接正常${NC}\n"

# 查询主机信息
echo -e "${GREEN}[2/7] 查询主机基本信息...${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec <<EOF
SELECT
    host_id,
    hostname,
    os_family,
    os_version,
    agent_version,
    status,
    is_container,
    container_id,
    last_heartbeat
FROM hosts
WHERE host_id = '${HOST_ID}';
EOF
echo ""

# 查询组件版本管理表
echo -e "${GREEN}[3/7] 查询组件版本管理表 (component_versions)...${NC}"
echo -e "${BLUE}说明: is_latest=1 的版本应该是最新版本${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec <<EOF
SELECT
    cv.id,
    c.name AS component_name,
    c.category,
    cv.version,
    cv.is_latest,
    cv.created_at
FROM component_versions cv
JOIN components c ON cv.component_id = c.id
WHERE c.name IN ('agent', 'baseline', 'collector')
ORDER BY c.name, cv.created_at DESC;
EOF
echo ""

# 查询插件配置表
echo -e "${GREEN}[4/7] 查询插件配置表 (plugin_configs)...${NC}"
echo -e "${BLUE}说明: 这是 Agent 端用于自动更新的配置${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec <<EOF
SELECT
    name,
    version,
    sha256,
    enabled,
    download_urls
FROM plugin_configs
WHERE name IN ('baseline', 'collector')
ORDER BY name;
EOF
echo ""

# 查询主机插件表
echo -e "${GREEN}[5/7] 查询主机插件表 (host_plugins)...${NC}"
echo -e "${BLUE}说明: 这是从 Agent 心跳上报的插件状态${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec <<EOF
SELECT
    id,
    host_id,
    name,
    version,
    status,
    start_time,
    updated_at,
    deleted_at
FROM host_plugins
WHERE host_id = '${HOST_ID}'
ORDER BY name;
EOF
echo ""

# 查询组件包表
echo -e "${GREEN}[6/7] 查询组件包表 (component_packages)...${NC}"
echo -e "${BLUE}说明: 检查是否有对应版本的包文件${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec <<EOF
SELECT
    cp.id,
    c.name AS component_name,
    cv.version AS component_version,
    cp.arch,
    cp.pkg_type,
    cp.file_size,
    cp.sha256,
    cp.enabled,
    cp.file_path,
    cp.uploaded_at
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.name IN ('agent', 'baseline', 'collector')
  AND cp.enabled = 1
ORDER BY c.name, cv.created_at DESC, cp.arch;
EOF
echo ""

# 检查包文件是否存在
echo -e "${GREEN}[7/7] 检查包文件是否存在...${NC}"
echo -e "${BLUE}说明: 验证数据库中记录的包文件是否真实存在${NC}"
mysql -h127.0.0.1 -P3306 -uroot -p123456 mxsec -N <<EOF | while IFS=$'\t' read -r component_name version file_path; do
SELECT
    c.name,
    cv.version,
    cp.file_path
FROM component_packages cp
JOIN component_versions cv ON cp.version_id = cv.id
JOIN components c ON cv.component_id = c.id
WHERE c.name IN ('agent', 'baseline', 'collector')
  AND cp.enabled = 1
ORDER BY c.name, cv.created_at DESC;
EOF
    if [ -f "$file_path" ]; then
        echo -e "${GREEN}✓${NC} ${component_name} ${version}: ${file_path}"
    else
        echo -e "${RED}✗${NC} ${component_name} ${version}: ${file_path} ${RED}(文件不存在)${NC}"
    fi
done
echo ""

# 总结和建议
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}诊断结果总结${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "${YELLOW}请检查以下几点:${NC}\n"

echo -e "${YELLOW}【BUG-001】组件列表版本显示不一致${NC}"
echo -e "  1. 检查 ${BLUE}component_versions${NC} 表中，${BLUE}is_latest=1${NC} 的版本是否是 1.0.4"
echo -e "  2. 检查 ${BLUE}plugin_configs${NC} 表中，插件的 ${BLUE}version${NC} 字段是否是 1.0.4"
echo -e "  3. 检查 ${BLUE}host_plugins${NC} 表中，插件的 ${BLUE}version${NC} 字段是什么版本"
echo -e "  ${RED}→ 如果 host_plugins 表中版本是 1.0.2，说明心跳数据没有更新${NC}\n"

echo -e "${YELLOW}【BUG-002】插件停止状态${NC}"
echo -e "  1. 检查 ${BLUE}host_plugins${NC} 表中，${BLUE}status${NC} 字段的值"
echo -e "  2. 检查 ${BLUE}deleted_at${NC} 字段是否为 NULL（不为 NULL 表示插件已删除）"
echo -e "  ${RED}→ 如果 status='stopped'，需要确认是真实停止还是状态错误${NC}\n"

echo -e "${YELLOW}【BUG-003】Agent 版本号异常（1.0.5）${NC}"
echo -e "  1. 检查 ${BLUE}hosts${NC} 表中，${BLUE}agent_version${NC} 字段的值"
echo -e "  2. 检查 ${BLUE}component_versions${NC} 表中，是否有 agent 的 1.0.5 版本记录"
echo -e "  ${RED}→ 如果数据库中有 1.0.5 版本，说明之前上传过测试版本${NC}"
echo -e "  ${RED}→ 如果数据库中没有，说明 Agent 编译时嵌入的版本号有误${NC}\n"

echo -e "${YELLOW}【BUG-004】自动更新流程失效${NC}"
echo -e "  1. 检查 ${BLUE}plugin_configs${NC} 表的版本是否已更新为 1.0.4"
echo -e "  2. 检查 ${BLUE}component_packages${NC} 表中是否有 1.0.4 版本的包文件"
echo -e "  3. 检查包文件是否真实存在（上面的文件存在性检查）"
echo -e "  ${RED}→ 如果 plugin_configs 没有更新，说明上传包时没有触发同步${NC}"
echo -e "  ${RED}→ 如果包文件不存在，说明上传失败或文件被误删${NC}\n"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}下一步建议${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "1. 根据上面的诊断结果，更新 ${YELLOW}docs/BUGS.md${NC} 文件"
echo -e "2. 根据问题原因，制定修复方案"
echo -e "3. 如需手动修复数据，请谨慎操作并备份数据库\n"

echo -e "${GREEN}诊断完成！${NC}"
