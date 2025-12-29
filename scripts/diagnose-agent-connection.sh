#!/bin/bash
# Agent 连接问题诊断脚本
# 在 Rocky 虚拟机上运行此脚本以诊断网络连接问题

set -e

SERVER_HOST="${1:-192.168.8.140}"
SERVER_PORT="${2:-6751}"

echo "========================================"
echo "Agent 连接诊断脚本"
echo "========================================"
echo "Server: ${SERVER_HOST}:${SERVER_PORT}"
echo ""

# 1. 检查基本网络连通性
echo "[1/6] 检查 ICMP 连通性..."
if ping -c 3 "${SERVER_HOST}" > /dev/null 2>&1; then
    echo "✓ ICMP ping 成功"
else
    echo "✗ ICMP ping 失败 - 网络不可达或防火墙阻止 ICMP"
fi
echo ""

# 2. 检查 TCP 端口连通性
echo "[2/6] 检查 TCP 端口 ${SERVER_PORT} 连通性..."
if timeout 5 bash -c "cat < /dev/null > /dev/tcp/${SERVER_HOST}/${SERVER_PORT}" 2>/dev/null; then
    echo "✓ TCP 端口 ${SERVER_PORT} 可连接"
else
    echo "✗ TCP 端口 ${SERVER_PORT} 无法连接"
    echo "  可能原因："
    echo "  - 防火墙阻止"
    echo "  - AgentCenter 未启动"
    echo "  - 网络路由问题"
fi
echo ""

# 3. 使用 telnet 测试（如果可用）
echo "[3/6] 使用 telnet 测试..."
if command -v telnet > /dev/null 2>&1; then
    timeout 3 telnet "${SERVER_HOST}" "${SERVER_PORT}" 2>&1 | head -5
else
    echo "  telnet 未安装，跳过"
fi
echo ""

# 4. 使用 nc 测试（如果可用）
echo "[4/6] 使用 nc 测试..."
if command -v nc > /dev/null 2>&1; then
    if nc -zv "${SERVER_HOST}" "${SERVER_PORT}" 2>&1; then
        echo "✓ nc 测试成功"
    else
        echo "✗ nc 测试失败"
    fi
else
    echo "  nc 未安装，跳过"
fi
echo ""

# 5. 检查路由
echo "[5/6] 检查路由信息..."
echo "网关："
ip route get "${SERVER_HOST}" 2>/dev/null || echo "  无法获取路由信息"
echo ""
echo "路由表："
ip route | head -10
echo ""

# 6. 检查本地网络配置
echo "[6/6] 检查本地网络配置..."
echo "网络接口："
ip addr show | grep -E "^[0-9]+:|inet " | head -20
echo ""
echo "DNS 配置："
cat /etc/resolv.conf 2>/dev/null | grep -v "^#" | grep -v "^$" || echo "  无法读取 DNS 配置"
echo ""

# 7. 检查防火墙状态
echo "========================================"
echo "防火墙检查"
echo "========================================"
if systemctl is-active --quiet firewalld; then
    echo "firewalld: 运行中"
    echo "防火墙规则："
    firewall-cmd --list-all 2>/dev/null | head -20
elif systemctl is-active --quiet iptables; then
    echo "iptables: 运行中"
    echo "防火墙规则："
    iptables -L -n | head -30
else
    echo "防火墙: 未运行"
fi
echo ""

# 8. 检查 Agent 状态
echo "========================================"
echo "Agent 状态检查"
echo "========================================"
if systemctl is-active --quiet mxsec-agent; then
    echo "mxsec-agent: 运行中"
    echo ""
    echo "最近日志（最后 20 行）："
    journalctl -u mxsec-agent -n 20 --no-pager 2>/dev/null || tail -20 /var/log/mxsec-agent/agent.log 2>/dev/null
else
    echo "mxsec-agent: 未运行"
fi
echo ""

# 9. 诊断建议
echo "========================================"
echo "诊断建议"
echo "========================================"
echo "如果 TCP 端口测试失败，请尝试："
echo "1. 检查 VMware 网络模式（建议使用桥接模式）"
echo "2. 检查服务器端防火墙是否开放 ${SERVER_PORT} 端口"
echo "3. 在服务器端确认 AgentCenter 正常运行："
echo "   docker logs mxsec-agentcenter-dev --tail 50"
echo "4. 在服务器端确认端口监听："
echo "   netstat -tuln | grep ${SERVER_PORT}"
echo "5. 如果使用 NAT 模式，检查端口转发配置"
echo ""
echo "如果 ICMP ping 失败但 TCP 可连接，通常是正常的（防火墙阻止 ICMP）"
echo ""
