#!/bin/bash
# 诊断组件状态数据流

echo "=== 诊断组件状态数据流 ==="
echo ""

echo "1. 检查 Agent 是否上报版本（从 AgentCenter 日志）"
echo "----------------------------------------"
docker logs mxsec-agentcenter-dev 2>&1 | grep -i "agent_version\|plugin_stats" | tail -5
echo ""

echo "2. 检查数据库中的 Agent 版本数据"
echo "----------------------------------------"
docker exec mysql mysql -umxsec_user -pmxsec_password mxsec -e "SELECT host_id, hostname, agent_version FROM hosts WHERE agent_version IS NOT NULL AND agent_version != '' LIMIT 5;" 2>/dev/null || echo "无法连接数据库或查询失败"
echo ""

echo "3. 检查数据库中的 Plugin 版本数据"
echo "----------------------------------------"
docker exec mysql mysql -umxsec_user -pmxsec_password mxsec -e "SELECT host_id, name, version, status FROM host_plugins WHERE deleted_at IS NULL LIMIT 10;" 2>/dev/null || echo "无法连接数据库或查询失败"
echo ""

echo "4. 检查 Manager API 响应（需要手动测试）"
echo "----------------------------------------"
echo "请访问: curl http://localhost:8080/api/v1/components"
echo ""

echo "5. 检查组件列表 API 日志"
echo "----------------------------------------"
docker logs mxsec-manager-dev 2>&1 | grep -i "查询.*统计\|component" | tail -10
echo ""

echo "=== 诊断完成 ==="
