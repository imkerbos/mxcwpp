#!/bin/bash
# Agent 远程诊断脚本 - 在有问题的服务器上运行
# 使用方法: bash diagnose-remote-agent.sh

echo "========================================"
echo "Agent 远程诊断脚本"
echo "========================================"
echo "服务器: $(hostname)"
echo "时间: $(date)"
echo ""

# 1. 检查 Agent 服务状态
echo "[1/10] 检查 Agent 服务状态..."
systemctl status mxsec-agent --no-pager | head -15
echo ""

# 2. 检查 Agent 进程
echo "[2/10] 检查 Agent 进程..."
ps aux | grep mxsec-agent | grep -v grep
echo ""

# 3. 检查 Agent 版本
echo "[3/10] 检查 Agent 版本..."
if [ -f /usr/bin/mxsec-agent ]; then
    /usr/bin/mxsec-agent -version 2>/dev/null || echo "无法获取版本信息"
else
    echo "Agent 二进制文件不存在: /usr/bin/mxsec-agent"
fi
echo ""

# 4. 检查最近的 Agent 日志（最重要！）
echo "[4/10] 检查最近的 Agent 日志（最后 50 行）..."
if [ -f /var/log/mxsec-agent/agent.log ]; then
    echo "=== 最后 50 行日志 ==="
    tail -50 /var/log/mxsec-agent/agent.log
    echo ""
    echo "=== 过滤关键错误 ==="
    tail -200 /var/log/mxsec-agent/agent.log | grep -i "error\|failed\|timeout\|connection" | tail -20
else
    echo "日志文件不存在: /var/log/mxsec-agent/agent.log"
    echo "尝试从 journald 获取日志..."
    journalctl -u mxsec-agent -n 50 --no-pager
fi
echo ""

# 5. 检查网络连通性
echo "[5/10] 检查网络连通性..."
echo "测试到 AgentCenter 的连接（192.168.8.140:6751）:"
timeout 5 bash -c 'cat < /dev/null > /dev/tcp/192.168.8.140/6751' 2>&1 && echo "✓ TCP 端口 6751 可连接" || echo "✗ TCP 端口 6751 无法连接"
echo ""

# 6. 检查 DNS 解析
echo "[6/10] 检查 DNS 解析..."
nslookup 192.168.8.140 2>/dev/null || echo "DNS 查询失败或 nslookup 未安装"
echo ""

# 7. 检查防火墙
echo "[7/10] 检查本地防火墙..."
if systemctl is-active --quiet firewalld; then
    echo "firewalld: 运行中"
    firewall-cmd --list-all 2>/dev/null | grep -E "services|ports" | head -10
elif systemctl is-active --quiet iptables; then
    echo "iptables: 运行中"
    iptables -L -n | grep -E "6751|ACCEPT|DROP" | head -10
else
    echo "防火墙: 未运行"
fi
echo ""

# 8. 检查 Agent 配置文件（如果存在）
echo "[8/10] 检查 Agent 配置..."
if [ -d /var/lib/mxsec-agent ]; then
    echo "Agent 数据目录存在"
    ls -la /var/lib/mxsec-agent/ 2>/dev/null | head -20
    echo ""
    if [ -f /var/lib/mxsec-agent/agent_id ]; then
        echo "Agent ID: $(cat /var/lib/mxsec-agent/agent_id)"
    fi
else
    echo "Agent 数据目录不存在: /var/lib/mxsec-agent"
fi
echo ""

# 9. 检查证书文件
echo "[9/10] 检查证书文件..."
if [ -d /var/lib/mxsec-agent/certs ]; then
    echo "证书目录存在:"
    ls -lh /var/lib/mxsec-agent/certs/ 2>/dev/null
else
    echo "证书目录不存在（首次连接正常）"
fi
echo ""

# 10. 系统资源检查
echo "[10/10] 检查系统资源..."
echo "内存:"
free -h | grep -E "Mem|Swap"
echo ""
echo "磁盘:"
df -h | grep -E "Filesystem|/var"
echo ""
echo "负载:"
uptime
echo ""

echo "========================================"
echo "诊断完成！"
echo "========================================"
echo ""
echo "请将以上完整输出发送给技术支持。"
echo ""
echo "快速修复建议:"
echo "1. 如果 Agent 未运行，尝试启动:"
echo "   sudo systemctl start mxsec-agent"
echo ""
echo "2. 如果有连接错误，重启 Agent:"
echo "   sudo systemctl restart mxsec-agent"
echo ""
echo "3. 如果端口无法连接，检查服务端是否运行:"
echo "   nc -zv 192.168.8.140 6751"
echo ""
echo "4. 查看实时日志:"
echo "   sudo tail -f /var/log/mxsec-agent/agent.log"
echo ""
