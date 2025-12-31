#!/bin/bash
# 版本同步修复脚本
# 用于修复 plugin_configs 表和组件版本不一致的问题

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 配置（可根据实际情况修改）
MANAGER_URL="${MANAGER_URL:-http://localhost:8080}"
MYSQL_HOST="${MYSQL_HOST:-localhost}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASS="${MYSQL_PASS:-}"
MYSQL_DB="${MYSQL_DB:-mxsec}"

echo -e "${GREEN}=== 版本同步修复脚本 ===${NC}"
echo ""

# 函数：检查命令是否存在
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo -e "${RED}错误: 未找到命令 '$1'，请先安装${NC}"
        exit 1
    fi
}

# 检查必要的命令
check_command mysql

echo -e "${YELLOW}步骤 1: 诊断当前状态${NC}"
echo "--------------------------------------"

# 查询组件版本表，找出标记为 is_latest=true 的版本
echo "正在查询最新组件版本..."
MYSQL_CMD="mysql -h${MYSQL_HOST} -P${MYSQL_PORT} -u${MYSQL_USER}"
if [ -n "$MYSQL_PASS" ]; then
    MYSQL_CMD="$MYSQL_CMD -p${MYSQL_PASS}"
fi
MYSQL_CMD="$MYSQL_CMD $MYSQL_DB"

# 查询 component_versions 表
LATEST_VERSIONS=$($MYSQL_CMD -N -e "
    SELECT
        c.name AS component_name,
        c.category,
        cv.version,
        cv.id AS version_id,
        cv.is_latest
    FROM component_versions cv
    JOIN components c ON cv.component_id = c.id
    WHERE cv.is_latest = 1 AND c.category = 'plugin'
    ORDER BY c.name;
" | awk '{print $1"|"$2"|"$3"|"$4"|"$5}')

if [ -z "$LATEST_VERSIONS" ]; then
    echo -e "${RED}错误: 未找到任何标记为 is_latest=true 的插件版本${NC}"
    echo "请先确保已正确上传插件版本并设置为最新版本"
    exit 1
fi

echo -e "${GREEN}找到以下最新插件版本:${NC}"
echo "$LATEST_VERSIONS" | while IFS='|' read -r name category version version_id is_latest; do
    echo "  - $name: v$version (version_id=$version_id)"
done
echo ""

# 查询 plugin_configs 表
echo "正在查询插件配置表 (plugin_configs)..."
PLUGIN_CONFIGS=$($MYSQL_CMD -N -e "
    SELECT name, version, sha256, enabled
    FROM plugin_configs
    ORDER BY name;
" | awk '{print $1"|"$2"|"$3"|"$4}')

echo -e "${GREEN}当前插件配置:${NC}"
if [ -z "$PLUGIN_CONFIGS" ]; then
    echo "  (无配置)"
else
    echo "$PLUGIN_CONFIGS" | while IFS='|' read -r name version sha256 enabled; do
        echo "  - $name: v$version (enabled=$enabled)"
    done
fi
echo ""

echo -e "${YELLOW}步骤 2: 检测版本不一致${NC}"
echo "--------------------------------------"

INCONSISTENT=0
echo "$LATEST_VERSIONS" | while IFS='|' read -r name category version version_id is_latest; do
    # 查询 plugin_configs 中的版本
    CONFIG_VERSION=$($MYSQL_CMD -N -e "SELECT version FROM plugin_configs WHERE name='$name'" 2>/dev/null || echo "")

    if [ -z "$CONFIG_VERSION" ]; then
        echo -e "${YELLOW}  警告: 插件 '$name' 在 plugin_configs 中不存在（组件版本: v$version）${NC}"
        INCONSISTENT=1
    elif [ "$CONFIG_VERSION" != "$version" ]; then
        echo -e "${RED}  不一致: 插件 '$name' - 配置版本: v$CONFIG_VERSION，组件版本: v$version${NC}"
        INCONSISTENT=1
    else
        echo -e "${GREEN}  一致: 插件 '$name' - v$version${NC}"
    fi
done
echo ""

if [ $INCONSISTENT -eq 0 ]; then
    echo -e "${GREEN}所有插件版本一致，无需修复${NC}"
    exit 0
fi

echo -e "${YELLOW}步骤 3: 同步插件配置${NC}"
echo "--------------------------------------"
echo "将为每个插件调用同步逻辑..."
echo ""

# 为每个最新版本插件同步配置
echo "$LATEST_VERSIONS" | while IFS='|' read -r name category version version_id is_latest; do
    echo "正在同步插件: $name (v$version)..."

    # 查询该版本的包信息（优先 amd64）
    PACKAGE_INFO=$($MYSQL_CMD -N -e "
        SELECT arch, sha256, file_path
        FROM component_packages
        WHERE version_id = $version_id AND enabled = 1
        ORDER BY CASE arch WHEN 'amd64' THEN 1 WHEN 'arm64' THEN 2 ELSE 3 END
        LIMIT 1;
    " 2>/dev/null || echo "")

    if [ -z "$PACKAGE_INFO" ]; then
        echo -e "${RED}  错误: 未找到版本 $version 的包文件${NC}"
        continue
    fi

    ARCH=$(echo "$PACKAGE_INFO" | awk '{print $1}')
    SHA256=$(echo "$PACKAGE_INFO" | awk '{print $2}')
    FILE_PATH=$(echo "$PACKAGE_INFO" | awk '{print $3}')

    # 构建下载 URL
    DOWNLOAD_URL="/api/v1/plugins/download/$name"

    # 确定插件类型
    case "$name" in
        "baseline")
            PLUGIN_TYPE="baseline"
            ;;
        "collector")
            PLUGIN_TYPE="collector"
            ;;
        *)
            PLUGIN_TYPE="$name"
            ;;
    esac

    # 检查 plugin_configs 中是否存在
    CONFIG_EXISTS=$($MYSQL_CMD -N -e "SELECT COUNT(*) FROM plugin_configs WHERE name='$name'" 2>/dev/null || echo "0")

    UPDATED_AT=$(date '+%Y-%m-%d %H:%M:%S')
    DETAIL="{\"updated_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
    DESCRIPTION="$name 插件 v$version"

    if [ "$CONFIG_EXISTS" -eq 0 ]; then
        # 创建新配置
        echo "  - 创建新配置..."
        $MYSQL_CMD -e "
            INSERT INTO plugin_configs (name, type, version, sha256, download_urls, detail, enabled, description, created_at, updated_at)
            VALUES (
                '$name',
                '$PLUGIN_TYPE',
                '$version',
                '$SHA256',
                JSON_ARRAY('$DOWNLOAD_URL'),
                '$DETAIL',
                1,
                '$DESCRIPTION',
                NOW(),
                NOW()
            );
        " 2>&1

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}  ✓ 成功创建插件配置: $name v$version${NC}"
        else
            echo -e "${RED}  ✗ 创建失败${NC}"
        fi
    else
        # 更新现有配置
        echo "  - 更新现有配置..."
        $MYSQL_CMD -e "
            UPDATE plugin_configs
            SET
                version = '$version',
                sha256 = '$SHA256',
                download_urls = JSON_ARRAY('$DOWNLOAD_URL'),
                detail = '$DETAIL',
                description = '$DESCRIPTION',
                updated_at = NOW()
            WHERE name = '$name';
        " 2>&1

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}  ✓ 成功更新插件配置: $name v$version${NC}"
        else
            echo -e "${RED}  ✗ 更新失败${NC}"
        fi
    fi

    echo ""
done

echo -e "${YELLOW}步骤 4: 验证修复结果${NC}"
echo "--------------------------------------"

# 重新查询并对比
ALL_MATCH=1
echo "$LATEST_VERSIONS" | while IFS='|' read -r name category version version_id is_latest; do
    CONFIG_VERSION=$($MYSQL_CMD -N -e "SELECT version FROM plugin_configs WHERE name='$name'" 2>/dev/null || echo "")

    if [ "$CONFIG_VERSION" == "$version" ]; then
        echo -e "${GREEN}  ✓ $name: v$version (一致)${NC}"
    else
        echo -e "${RED}  ✗ $name: 配置v$CONFIG_VERSION, 组件v$version (不一致)${NC}"
        ALL_MATCH=0
    fi
done

echo ""

if [ $ALL_MATCH -eq 1 ]; then
    echo -e "${GREEN}=== 修复完成！所有版本已同步 ===${NC}"
    echo ""
    echo -e "${YELLOW}提示: 自动更新调度器将在 30 秒内检测到配置更新并广播到所有在线 Agent${NC}"
    echo "你可以通过以下方式确认："
    echo "  1. 查看 AgentCenter 日志，确认广播成功"
    echo "  2. 查看主机详情页面，等待 Agent 上报新版本（需要等待心跳周期，默认 60 秒）"
    echo "  3. 如果仍未更新，请重启 AgentCenter 服务"
else
    echo -e "${RED}=== 修复未完全成功，请检查错误信息 ===${NC}"
    exit 1
fi
