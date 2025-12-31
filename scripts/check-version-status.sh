#!/bin/bash
# 版本状态检查脚本
# 用于诊断组件版本、插件配置和主机插件版本的一致性

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
MYSQL_HOST="${MYSQL_HOST:-localhost}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASS="${MYSQL_PASS:-}"
MYSQL_DB="${MYSQL_DB:-mxsec}"

echo -e "${BLUE}=== 版本状态诊断工具 ===${NC}"
echo ""

# MySQL 连接命令
MYSQL_CMD="mysql -h${MYSQL_HOST} -P${MYSQL_PORT} -u${MYSQL_USER}"
if [ -n "$MYSQL_PASS" ]; then
    MYSQL_CMD="$MYSQL_CMD -p${MYSQL_PASS}"
fi
MYSQL_CMD="$MYSQL_CMD $MYSQL_DB"

echo -e "${YELLOW}=== 1. 组件版本表 (component_versions) ===${NC}"
echo "显示所有组件的最新版本（is_latest=true）："
echo ""
$MYSQL_CMD -t -e "
    SELECT
        c.name AS '组件名称',
        c.category AS '分类',
        cv.version AS '版本',
        cv.is_latest AS '最新',
        cv.created_by AS '创建者',
        cv.created_at AS '创建时间'
    FROM component_versions cv
    JOIN components c ON cv.component_id = c.id
    WHERE cv.is_latest = 1
    ORDER BY c.category, c.name;
"
echo ""

echo -e "${YELLOW}=== 2. 组件包表 (component_packages) ===${NC}"
echo "显示所有最新版本的组件包："
echo ""
$MYSQL_CMD -t -e "
    SELECT
        c.name AS '组件名称',
        cv.version AS '版本',
        cp.arch AS '架构',
        cp.pkg_type AS '包类型',
        ROUND(cp.file_size / 1024 / 1024, 2) AS '大小(MB)',
        cp.enabled AS '启用',
        cp.uploaded_by AS '上传者',
        cp.uploaded_at AS '上传时间'
    FROM component_packages cp
    JOIN component_versions cv ON cp.version_id = cv.id
    JOIN components c ON cv.component_id = c.id
    WHERE cv.is_latest = 1
    ORDER BY c.name, cp.arch;
"
echo ""

echo -e "${YELLOW}=== 3. 插件配置表 (plugin_configs) ===${NC}"
echo "显示所有插件配置："
echo ""
$MYSQL_CMD -t -e "
    SELECT
        name AS '插件名称',
        type AS '类型',
        version AS '版本',
        LEFT(sha256, 16) AS 'SHA256(前16位)',
        enabled AS '启用',
        description AS '描述',
        updated_at AS '更新时间'
    FROM plugin_configs
    ORDER BY name;
"
echo ""

echo -e "${YELLOW}=== 4. 主机插件版本表 (host_plugins) ===${NC}"
echo "显示所有主机上的插件版本："
echo ""
$MYSQL_CMD -t -e "
    SELECT
        hp.host_id AS '主机ID',
        h.hostname AS '主机名',
        hp.name AS '插件名称',
        hp.version AS '版本',
        hp.status AS '状态',
        hp.start_time AS '启动时间',
        hp.updated_at AS '更新时间'
    FROM host_plugins hp
    LEFT JOIN hosts h ON hp.host_id = h.host_id
    ORDER BY hp.host_id, hp.name;
"
echo ""

echo -e "${YELLOW}=== 5. 版本一致性检查 ===${NC}"
echo ""

# 获取所有最新版本的插件
LATEST_PLUGINS=$($MYSQL_CMD -N -e "
    SELECT
        c.name,
        cv.version
    FROM component_versions cv
    JOIN components c ON cv.component_id = c.id
    WHERE cv.is_latest = 1 AND c.category = 'plugin'
    ORDER BY c.name;
" | awk '{print $1"|"$2}')

if [ -z "$LATEST_PLUGINS" ]; then
    echo -e "${RED}未找到任何最新的插件版本${NC}"
    exit 1
fi

ALL_CONSISTENT=1

echo "$LATEST_PLUGINS" | while IFS='|' read -r plugin_name latest_version; do
    # 查询 plugin_configs 中的版本
    CONFIG_VERSION=$($MYSQL_CMD -N -e "SELECT version FROM plugin_configs WHERE name='$plugin_name'" 2>/dev/null || echo "")

    # 查询 host_plugins 中的版本（取最常见的版本）
    HOST_VERSIONS=$($MYSQL_CMD -N -e "
        SELECT version, COUNT(*) as count
        FROM host_plugins
        WHERE name='$plugin_name'
        GROUP BY version
        ORDER BY count DESC
        LIMIT 3;
    " 2>/dev/null | awk '{print $1" (主机数: "$2")"}' || echo "")

    echo -e "${BLUE}插件: $plugin_name${NC}"
    echo "  组件最新版本: v$latest_version"

    if [ -z "$CONFIG_VERSION" ]; then
        echo -e "  ${RED}✗ 插件配置: (不存在)${NC}"
        ALL_CONSISTENT=0
    elif [ "$CONFIG_VERSION" != "$latest_version" ]; then
        echo -e "  ${RED}✗ 插件配置: v$CONFIG_VERSION (不一致)${NC}"
        ALL_CONSISTENT=0
    else
        echo -e "  ${GREEN}✓ 插件配置: v$CONFIG_VERSION (一致)${NC}"
    fi

    if [ -z "$HOST_VERSIONS" ]; then
        echo "  主机版本: (无主机)"
    else
        echo "  主机版本分布:"
        echo "$HOST_VERSIONS" | while read -r line; do
            version=$(echo "$line" | awk '{print $1}')
            if [[ "$version" == v* ]]; then
                version=${version:1}  # 移除 v 前缀
            fi
            if [ "$version" == "$latest_version" ]; then
                echo -e "    ${GREEN}✓ v$line${NC}"
            else
                echo -e "    ${YELLOW}⚠ v$line${NC}"
                ALL_CONSISTENT=0
            fi
        done
    fi
    echo ""
done

echo -e "${YELLOW}=== 6. 诊断建议 ===${NC}"
echo ""

if [ $ALL_CONSISTENT -eq 1 ]; then
    echo -e "${GREEN}✓ 所有版本一致，系统正常${NC}"
else
    echo -e "${RED}✗ 检测到版本不一致问题${NC}"
    echo ""
    echo "可能的原因："
    echo "  1. 上传新版本时未正确设置 is_latest 标志"
    echo "  2. 上传包文件的顺序有问题（先创建版本，后上传包）"
    echo "  3. 自动更新调度器未正常工作"
    echo "  4. Agent 未正常接收或应用更新"
    echo ""
    echo "修复建议："
    echo "  1. 运行修复脚本: ./scripts/fix-version-sync.sh"
    echo "  2. 检查 AgentCenter 日志，确认调度器是否正常运行"
    echo "  3. 检查 Agent 日志，确认是否收到配置更新"
    echo "  4. 如果问题持续，尝试重启 AgentCenter 和 Agent"
fi

echo ""
echo -e "${BLUE}=== 诊断完成 ===${NC}"
