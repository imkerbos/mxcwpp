-- 更新 SELinux/AppArmor 检测规则
-- 执行方式：mysql -u mxsec_user -p mxsec < scripts/update-selinux-rule.sql

-- 更新 LINUX_SERVICE_018 规则的检查配置
UPDATE rules 
SET check_config = '{
  "condition": "any",
  "rules": [
    {
      "type": "command_exec",
      "param": ["if command -v getenforce >/dev/null 2>&1; then getenforce; else echo ''getenforce_not_found''; fi", "^(Enforcing|Permissive)$"]
    },
    {
      "type": "command_exec",
      "param": ["if [ -f /sys/kernel/security/apparmor/profiles ]; then cat /sys/kernel/security/apparmor/profiles 2>/dev/null | wc -l; else echo 0; fi", "^[1-9][0-9]*$"]
    },
    {
      "type": "service_status",
      "param": ["apparmor", "active"]
    },
    {
      "type": "command_exec",
      "param": ["aa-status --enabled 2>/dev/null && echo ''enabled'' || echo ''disabled''", "^enabled$"]
    }
  ]
}'
WHERE rule_id = 'LINUX_SERVICE_018';

-- 查看更新结果
SELECT rule_id, title, check_config FROM rules WHERE rule_id = 'LINUX_SERVICE_018';
