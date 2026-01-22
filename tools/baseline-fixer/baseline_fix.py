#!/usr/bin/env python3
import argparse
import sys
import os
import json
import re
import pandas as pd
from typing import List, Dict
import subprocess
from pathlib import Path

try:
    import inquirer
except ImportError:
    print("请先安装依赖: pip3 install inquirer pandas openpyxl")
    sys.exit(1)


def check_os_compatibility():
    """检查操作系统兼容性，仅支持 CentOS、Rocky Linux、RedHat"""
    allowed_os = ['centos', 'rocky', 'rhel', 'redhat']

    try:
        # 读取 /etc/os-release
        if os.path.exists('/etc/os-release'):
            with open('/etc/os-release', 'r') as f:
                os_info = f.read().lower()

            for os_name in allowed_os:
                if os_name in os_info:
                    return True, os_name

        # 读取 /etc/redhat-release
        if os.path.exists('/etc/redhat-release'):
            with open('/etc/redhat-release', 'r') as f:
                release_info = f.read().lower()

            for os_name in allowed_os:
                if os_name in release_info:
                    return True, os_name

        return False, None

    except Exception as e:
        print(f"警告: 无法检测操作系统: {e}")
        return False, None


class BaselineFixer:
    def __init__(self, excel_file: str, config_dir: str = None):
        self.excel_file = excel_file
        self.df = None
        self.fixes = {}

        # 默认使用脚本同目录下的 config 目录
        if config_dir is None:
            script_dir = Path(__file__).parent
            config_dir = script_dir / "config"

        self.config_dir = Path(config_dir)
        self._load_baseline_configs()

    def _load_baseline_configs(self):
        """从 JSON 配置文件加载所有基线检查项和修复命令"""
        if not self.config_dir.exists():
            print(f"错误: 配置目录不存在: {self.config_dir}")
            sys.exit(1)

        json_files = list(self.config_dir.glob("*.json"))
        if not json_files:
            print(f"错误: 未找到配置文件: {self.config_dir}")
            sys.exit(1)

        total_rules = 0
        for json_file in json_files:
            try:
                with open(json_file, 'r', encoding='utf-8') as f:
                    config = json.load(f)

                for rule in config.get('rules', []):
                    rule_id = rule.get('rule_id')
                    title = rule.get('title')
                    fix = rule.get('fix', {})
                    command = fix.get('command', '')
                    suggestion = fix.get('suggestion', '')
                    severity = rule.get('severity', 'unknown')

                    if rule_id and command:
                        self.fixes[rule_id] = {
                            'title': title,
                            'command': command,
                            'suggestion': suggestion,
                            'severity': severity
                        }
                        total_rules += 1

            except Exception as e:
                print(f"警告: 加载配置文件失败 {json_file.name}: {e}")

        print(f"✓ 已加载 {total_rules} 条基线修复规则")

    def load_report(self):
        try:
            # 尝试自动检测报告格式
            # 先读取前几行判断格式
            df_raw = pd.read_excel(self.excel_file, header=None, nrows=15)

            # 查找包含 "规则ID" 或 "rule_id" 的行作为表头
            header_row = None
            for idx, row in df_raw.iterrows():
                row_str = ' '.join([str(v).lower() for v in row.values if pd.notna(v)])
                if '规则id' in row_str or 'rule_id' in row_str:
                    header_row = idx
                    break

            if header_row is not None:
                self.df = pd.read_excel(self.excel_file, header=header_row)
                print(f"✓ 成功加载报告: {self.excel_file} (表头在第 {header_row + 1} 行)")
            else:
                # 默认从第一行读取
                self.df = pd.read_excel(self.excel_file)
                print(f"✓ 成功加载报告: {self.excel_file}")

            return True
        except Exception as e:
            print(f"✗ 加载报告失败: {e}")
            return False

    def filter_by_severity(self, severities: List[str]) -> pd.DataFrame:
        severity_col = None
        for col in self.df.columns:
            if '等级' in col or '级别' in col or 'severity' in col.lower() or '风险' in col:
                severity_col = col
                break

        if not severity_col:
            print("警告: 未找到风险等级列，显示所有项")
            return self.df

        filtered = self.df[self.df[severity_col].str.upper().isin([s.upper() for s in severities])]
        return filtered

    def display_items(self, df: pd.DataFrame) -> List[Dict]:
        items = []
        rule_id_col = None
        name_col = None
        severity_col = None

        # 查找列名
        for col in df.columns:
            col_lower = str(col).lower()
            if 'rule_id' in col_lower or '规则id' in col_lower or '规则编号' in col_lower:
                rule_id_col = col
            if '检查项' in col or '名称' in col or 'name' in col_lower or '标题' in col or 'title' in col_lower:
                name_col = col
            if '等级' in col or '级别' in col or 'severity' in col_lower or '风险' in col:
                severity_col = col

        if not rule_id_col and not name_col:
            print("警告: 未找到规则ID或名称列，使用第一列")
            name_col = df.columns[0]

        for idx, row in df.iterrows():
            rule_id = str(row[rule_id_col]) if rule_id_col else None
            name = str(row[name_col]) if name_col else ""
            severity = str(row[severity_col]) if severity_col else "unknown"

            # 从配置中查找修复信息
            fix_info = None
            if rule_id and rule_id in self.fixes:
                fix_info = self.fixes[rule_id]
            elif name:
                # 尝试通过名称匹配
                for rid, finfo in self.fixes.items():
                    if finfo['title'] in name or name in finfo['title']:
                        fix_info = finfo
                        rule_id = rid
                        break

            if fix_info:
                display = f"[{severity.upper()}] {rule_id} - {fix_info['title']}"
                items.append({
                    'index': idx,
                    'rule_id': rule_id,
                    'name': name,
                    'severity': severity,
                    'display': display,
                    'fix_info': fix_info
                })

        return items

    def select_items(self, items: List[Dict]) -> List[Dict]:
        if not items:
            print("没有找到符合条件的检查项")
            return []

        # 先询问是否全选
        select_all_question = [
            inquirer.List(
                'select_mode',
                message="选择修复模式",
                choices=[
                    ('全选所有项目', 'all'),
                    ('手动选择项目', 'manual'),
                    ('取消', 'cancel'),
                ],
            ),
        ]

        mode_answer = inquirer.prompt(select_all_question)
        if not mode_answer or mode_answer['select_mode'] == 'cancel':
            return []

        if mode_answer['select_mode'] == 'all':
            print(f"\n已选择全部 {len(items)} 个项目")
            return items

        # 手动选择模式
        choices = [item['display'] for item in items]

        questions = [
            inquirer.Checkbox(
                'selected',
                message="选择要修复的项目 (空格选择，回车确认)",
                choices=choices,
            ),
        ]

        answers = inquirer.prompt(questions)
        if not answers or not answers['selected']:
            return []

        selected_displays = set(answers['selected'])
        return [item for item in items if item['display'] in selected_displays]

    def fix_item(self, item: Dict) -> bool:
        rule_id = item.get('rule_id')
        fix_info = item.get('fix_info')

        if not fix_info:
            print(f"  ⚠ 未找到 '{rule_id}' 的修复命令，跳过")
            return False

        fix_cmd = fix_info.get('command')
        if not fix_cmd:
            print(f"  ⚠ '{rule_id}' 没有自动修复命令")
            print(f"  建议: {fix_info.get('suggestion', '无')}")
            return False

        # ========== 预检测：检查是否已经是目标状态 ==========

        # 1. SELinux 状态检测
        if 'setenforce' in fix_cmd:
            try:
                result = subprocess.run('getenforce', shell=True, capture_output=True, text=True, timeout=5)
                current_state = result.stdout.strip().lower()
                if current_state == 'enforcing':
                    print(f"  ⏭ SELinux 已是 Enforcing 状态，跳过")
                    return True
                elif current_state == 'disabled':
                    # 只修改配置文件
                    config_cmd = "sed -i 's/^SELINUX=.*/SELINUX=enforcing/' /etc/selinux/config"
                    print(f"  执行: {config_cmd}")
                    subprocess.run(config_cmd, shell=True, timeout=10)
                    print(f"  ✓ 配置文件已修改")
                    print(f"  ⚠ SELinux 当前为 disabled，需要重启系统后生效")
                    return True
            except:
                pass

        # 2. 文件追加类型检测 (echo ... >> file)
        if '>>' in fix_cmd and 'echo' in fix_cmd:
            # 提取所有 echo 命令
            echo_parts = re.findall(r"echo\s+['\"]([^'\"]+)['\"]\s*>>\s*([^\s&]+)", fix_cmd)
            if echo_parts:
                all_exist = True
                for content, target_file in echo_parts:
                    try:
                        # 使用 grep 检查内容是否已存在
                        # 注意：用 -- 防止内容被当作 grep 选项（如 -w）
                        escaped_content = content.replace("'", "'\\''")

                        # 如果是 audit rules，检查整个 rules.d 目录（排除注释行）
                        if 'audit/rules.d' in target_file:
                            check_cmd = f"grep -hF -- '{escaped_content}' /etc/audit/rules.d/*.rules 2>/dev/null | grep -qv '^[[:space:]]*#'"
                        else:
                            check_cmd = f"grep -hF -- '{escaped_content}' {target_file} 2>/dev/null | grep -qv '^[[:space:]]*#'"

                        result = subprocess.run(check_cmd, shell=True, timeout=5)
                        if result.returncode != 0:
                            all_exist = False
                            break
                    except:
                        all_exist = False
                        break

                if all_exist:
                    print(f"  ⏭ 规则已存在，跳过")
                    return True

        # 3. SSH 配置检测 (sed -i ... sshd_config 或 grep -q ... sshd_config)
        if 'sshd_config' in fix_cmd:
            # 提取配置项和期望值
            match = re.search(r"(\w+)\s+(yes|no|[0-9]+)", fix_cmd)
            if match:
                config_key = match.group(1)
                expected_value = match.group(2)
                try:
                    # 检查当前配置（排除注释行）
                    # 只匹配未注释的正确配置
                    check_cmd = f"grep -E '^[[:space:]]*{config_key}[[:space:]]+{expected_value}' /etc/ssh/sshd_config 2>/dev/null | grep -qv '^[[:space:]]*#'"
                    result = subprocess.run(check_cmd, shell=True, timeout=5)
                    if result.returncode == 0:
                        print(f"  ⏭ SSH 配置 {config_key}={expected_value} 已设置，跳过")
                        return True
                except:
                    pass

        # 4. 文件权限检测 (chmod)
        if fix_cmd.startswith('chmod'):
            match = re.search(r'chmod\s+(\d+)\s+(\S+)', fix_cmd)
            if match:
                expected_perm = match.group(1)
                target_file = match.group(2)
                # 处理通配符情况
                if '*' not in target_file:
                    try:
                        result = subprocess.run(f"stat -c '%a' {target_file} 2>/dev/null", shell=True, capture_output=True, text=True, timeout=5)
                        current_perm = result.stdout.strip()
                        if current_perm == expected_perm:
                            print(f"  ⏭ 文件权限已是 {expected_perm}，跳过")
                            return True
                    except:
                        pass

        # 5. 服务状态检测 (systemctl start/enable)
        if 'systemctl' in fix_cmd and ('start' in fix_cmd or 'enable' in fix_cmd):
            match = re.search(r'systemctl\s+(?:start|enable)\s+(\S+)', fix_cmd)
            if match:
                service_name = match.group(1)
                try:
                    # 检查服务是否已运行
                    if 'start' in fix_cmd:
                        result = subprocess.run(f"systemctl is-active {service_name} 2>/dev/null", shell=True, capture_output=True, text=True, timeout=5)
                        if result.stdout.strip() == 'active':
                            # 还要检查是否 enable
                            if 'enable' in fix_cmd:
                                result2 = subprocess.run(f"systemctl is-enabled {service_name} 2>/dev/null", shell=True, capture_output=True, text=True, timeout=5)
                                if result2.stdout.strip() == 'enabled':
                                    print(f"  ⏭ 服务 {service_name} 已运行且已启用，跳过")
                                    return True
                            else:
                                print(f"  ⏭ 服务 {service_name} 已运行，跳过")
                                return True
                except:
                    pass

        # 6. sysctl 配置检测
        if 'sysctl' in fix_cmd:
            match = re.search(r'sysctl\s+-w\s+(\S+)=(\S+)', fix_cmd)
            if match:
                param = match.group(1)
                expected_value = match.group(2)
                try:
                    result = subprocess.run(f"sysctl -n {param} 2>/dev/null", shell=True, capture_output=True, text=True, timeout=5)
                    current_value = result.stdout.strip()
                    if current_value == expected_value:
                        print(f"  ⏭ 内核参数 {param}={expected_value} 已设置，跳过")
                        return True
                except:
                    pass

        # 7. 软件包安装检测
        if 'dnf install' in fix_cmd or 'yum install' in fix_cmd:
            match = re.search(r'(?:dnf|yum)\s+install\s+(\S+)', fix_cmd)
            if match:
                package_name = match.group(1)
                try:
                    result = subprocess.run(f"rpm -q {package_name} 2>/dev/null", shell=True, capture_output=True, text=True, timeout=5)
                    if result.returncode == 0:
                        print(f"  ⏭ 软件包 {package_name} 已安装，跳过")
                        return True
                except:
                    pass

        # ========== 执行修复命令 ==========

        # 根据命令类型设置超时时间
        timeout = 60  # 默认 60 秒
        if 'aide --init' in fix_cmd or 'aide-init' in fix_cmd:
            timeout = 300  # AIDE 初始化需要更长时间（5分钟）
            print(f"  执行: {fix_cmd} (预计需要 1-5 分钟)")
        elif 'dnf install' in fix_cmd or 'yum install' in fix_cmd:
            timeout = 180  # 软件安装 3 分钟
            print(f"  执行: {fix_cmd}")
        else:
            print(f"  执行: {fix_cmd}")

        try:
            result = subprocess.run(
                fix_cmd,
                shell=True,
                capture_output=True,
                text=True,
                timeout=timeout
            )

            if result.returncode == 0:
                print(f"  ✓ 修复成功")
                return True
            else:
                stderr = result.stderr.strip()
                if stderr:
                    print(f"  ✗ 修复失败: {stderr}")
                else:
                    print(f"  ✗ 修复失败 (退出码: {result.returncode})")
                return False
        except subprocess.TimeoutExpired:
            print(f"  ✗ 执行超时 (超过 {timeout} 秒)")
            return False
        except Exception as e:
            print(f"  ✗ 执行失败: {e}")
            return False

    def run(self, severities: List[str]):
        if not self.load_report():
            return

        print(f"\n筛选风险等级: {', '.join(severities)}")
        filtered_df = self.filter_by_severity(severities)
        print(f"找到 {len(filtered_df)} 个检查项")

        items = self.display_items(filtered_df)
        print(f"其中 {len(items)} 个有自动修复方案\n")

        if not items:
            print("没有可修复的项目")
            return

        selected = self.select_items(items)

        if not selected:
            print("未选择任何项目")
            return

        print(f"\n开始修复 {len(selected)} 个项目...\n")

        success_count = 0
        need_reboot = False
        for i, item in enumerate(selected, 1):
            print(f"[{i}/{len(selected)}] {item['rule_id']} - {item['fix_info']['title']}")
            if self.fix_item(item):
                success_count += 1
                # 检查是否需要重启
                if 'selinux' in item['rule_id'].lower() or 'mac' in item['rule_id'].lower():
                    need_reboot = True
            print()

        print(f"修复完成: {success_count}/{len(selected)} 成功")

        # 重启提示
        if need_reboot:
            print("\n" + "=" * 60)
            print("⚠ 重要提示:")
            print("  部分修复项（如 SELinux）需要重启系统后才能完全生效")
            print("  建议在合适的时间执行: reboot")
            print("=" * 60)


def main():
    # 检查操作系统兼容性
    is_compatible, detected_os = check_os_compatibility()
    if not is_compatible:
        print("=" * 60)
        print("错误: 不支持的操作系统")
        print("本工具仅支持以下操作系统:")
        print("  - CentOS 7/8/9")
        print("  - Rocky Linux 8/9")
        print("  - Red Hat Enterprise Linux (RHEL) 7/8/9")
        print("=" * 60)
        sys.exit(1)

    print(f"✓ 检测到操作系统: {detected_os.upper()}")

    parser = argparse.ArgumentParser(
        description='基线检查修复工具 - 从配置文件加载所有修复规则',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog='''
示例:
  # 修复 HIGH 和 CRITICAL 等级（默认）
  python3 baseline_fix.py -f report.xlsx

  # 包含 MEDIUM 等级
  python3 baseline_fix.py -f report.xlsx -s HIGH CRITICAL MEDIUM

  # 指定配置目录
  python3 baseline_fix.py -f report.xlsx -c /path/to/config
        '''
    )
    parser.add_argument('-f', '--file', required=True, help='基线报告Excel文件路径')
    parser.add_argument('-s', '--severity', nargs='+',
                       default=['HIGH', 'CRITICAL'],
                       help='风险等级 (默认: HIGH CRITICAL)')
    parser.add_argument('-c', '--config', help='基线配置目录路径（可选）')

    args = parser.parse_args()

    fixer = BaselineFixer(args.file, args.config)
    fixer.run(args.severity)


if __name__ == '__main__':
    main()
