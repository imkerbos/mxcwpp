-- 更新基线规则脚本
-- 修复问题：
-- 1. 删除 Rocky Linux 9 已废弃的 hosts.allow/hosts.deny 规则
-- 2. 服务检查器逻辑已在代码中修复（auditd 状态匹配、xinetd 服务未安装）
-- 
-- 执行方式：mysql -h 127.0.0.1 -u mxsec_user -p mxsec < scripts/update-baseline-rules.sql

-- ============================================
-- 1. 删除已废弃的 TCP Wrappers 规则
-- Rocky Linux 9 / CentOS 9 / RHEL 9 已废弃 TCP Wrappers
-- hosts.allow 和 hosts.deny 文件不再使用
-- ============================================

-- 删除 /etc/hosts.allow 文件权限检查规则
DELETE FROM rules WHERE rule_id = 'LINUX_FILE_008';

-- 删除 /etc/hosts.deny 文件权限检查规则  
DELETE FROM rules WHERE rule_id = 'LINUX_FILE_009';

-- 查看删除结果
SELECT COUNT(*) AS remaining_rules FROM rules WHERE rule_id IN ('LINUX_FILE_008', 'LINUX_FILE_009');

-- ============================================
-- 2. 验证删除结果
-- ============================================

-- 显示当前文件权限策略的规则数量
SELECT p.id AS policy_id, p.name AS policy_name, COUNT(r.rule_id) AS rule_count
FROM policies p
LEFT JOIN rules r ON p.id = r.policy_id
WHERE p.id = 'LINUX_FILE_PERMISSIONS'
GROUP BY p.id, p.name;

-- ============================================
-- 提示信息
-- ============================================
-- 注意：服务状态检查的修复（auditd、xinetd）是在代码层面完成的
-- 需要重新构建并重启服务才能生效：
-- make dev-docker-down && make dev-docker-up
