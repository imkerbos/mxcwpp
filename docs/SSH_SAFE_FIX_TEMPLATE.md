# SSH 安全修复命令模板

## 问题说明

SSH 配置错误会导致 sshd 服务无法启动，用户将失去远程访问权限。因此 SSH 相关的基线修复必须包含配置验证机制。

## 安全修复命令模板

### 模板 1：验证后重启（推荐）

```bash
# 备份配置 -> 修改配置 -> 验证配置 -> 重启服务（验证失败则回滚）
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && \
<修改配置的命令> && \
if sshd -t; then \
  systemctl reload sshd || systemctl restart sshd; \
else \
  echo "SSH 配置验证失败，回滚配置" >&2; \
  mv /etc/ssh/sshd_config.bak /etc/ssh/sshd_config; \
  exit 1; \
fi
```

### 模板 2：仅修改配置，不重启（最安全）

```bash
# 备份配置 -> 修改配置 -> 验证配置（验证失败则回滚）
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && \
<修改配置的命令> && \
if sshd -t; then \
  echo "SSH 配置已更新并验证通过，请手动执行: systemctl reload sshd"; \
else \
  echo "SSH 配置验证失败，回滚配置" >&2; \
  mv /etc/ssh/sshd_config.bak /etc/ssh/sshd_config; \
  exit 1; \
fi
```

## 具体示例

### 示例 1：禁止 root 远程登录

**原命令（危险）**：
```bash
sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && systemctl restart sshd
```

**安全命令**：
```bash
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && \
sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && \
if sshd -t; then \
  systemctl reload sshd || systemctl restart sshd; \
else \
  echo "SSH 配置验证失败，回滚配置" >&2; \
  mv /etc/ssh/sshd_config.bak /etc/ssh/sshd_config; \
  exit 1; \
fi
```

### 示例 2：设置 MaxAuthTries

**原命令（危险）**：
```bash
sed -i 's/^#*MaxAuthTries.*/MaxAuthTries 4/' /etc/ssh/sshd_config && systemctl restart sshd
```

**安全命令**：
```bash
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && \
sed -i 's/^#*MaxAuthTries.*/MaxAuthTries 4/' /etc/ssh/sshd_config && \
if sshd -t; then \
  systemctl reload sshd || systemctl restart sshd; \
else \
  echo "SSH 配置验证失败，回滚配置" >&2; \
  mv /etc/ssh/sshd_config.bak /etc/ssh/sshd_config; \
  exit 1; \
fi
```

## 命令说明

1. **`cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak`**
   - 备份原配置文件

2. **`sed -i '...' /etc/ssh/sshd_config`**
   - 修改配置文件

3. **`sshd -t`**
   - 验证配置文件语法
   - 返回 0 表示配置正确，非 0 表示配置错误

4. **`systemctl reload sshd`**
   - 重新加载配置（不中断现有连接）
   - 如果 reload 失败，则 restart

5. **回滚机制**
   - 如果验证失败，恢复备份文件
   - 返回错误码 1，修复任务标记为失败

## 优势

1. **安全性**：配置错误不会导致服务无法启动
2. **可回滚**：验证失败自动恢复原配置
3. **不中断连接**：使用 reload 而非 restart
4. **明确反馈**：验证失败会输出错误信息

## 注意事项

1. **备份文件清理**：成功后可以删除 `.bak` 文件，但建议保留
2. **权限问题**：确保有权限读写 `/etc/ssh/sshd_config`
3. **SELinux**：某些系统可能需要 `restorecon` 恢复 SELinux 上下文
4. **多次修复**：每次修复都会覆盖 `.bak` 文件

## 批量更新脚本

```bash
#!/bin/bash
# 批量更新 SSH 规则的修复命令

JSON_FILE="plugins/baseline/config/examples/ssh-baseline.json"

# 创建临时 Python 脚本来更新 JSON
cat > /tmp/update_ssh_rules.py << 'EOF'
import json
import sys

def wrap_command(cmd):
    """将原命令包装为安全命令"""
    # 移除原命令中的 systemctl restart sshd
    cmd = cmd.replace(' && systemctl restart sshd', '')
    cmd = cmd.replace('; systemctl restart sshd', '')

    # 构建安全命令
    safe_cmd = f"""cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && \\
{cmd} && \\
if sshd -t; then \\
  systemctl reload sshd || systemctl restart sshd; \\
else \\
  echo "SSH 配置验证失败，回滚配置" >&2; \\
  mv /etc/ssh/sshd_config.bak /etc/ssh/sshd_config; \\
  exit 1; \\
fi"""
    return safe_cmd

# 读取 JSON 文件
with open(sys.argv[1], 'r', encoding='utf-8') as f:
    data = json.load(f)

# 更新所有 SSH 规则的修复命令
updated = 0
for rule in data.get('rules', []):
    if rule.get('rule_id', '').startswith('LINUX_SSH_'):
        if 'fix' in rule and 'command' in rule['fix']:
            old_cmd = rule['fix']['command']
            if 'sshd_config' in old_cmd and 'sshd -t' not in old_cmd:
                rule['fix']['command'] = wrap_command(old_cmd)
                updated += 1
                print(f"Updated: {rule['rule_id']}")

# 写回 JSON 文件
with open(sys.argv[1], 'w', encoding='utf-8') as f:
    json.dump(data, f, ensure_ascii=False, indent=2)

print(f"\nTotal updated: {updated} rules")
EOF

# 执行更新
python3 /tmp/update_ssh_rules.py "$JSON_FILE"

# 清理临时文件
rm /tmp/update_ssh_rules.py

echo "Done! Please review the changes and import the updated policy."
```

## 更新日志

- 2026-01-28: 初始版本，添加 SSH 配置验证机制
