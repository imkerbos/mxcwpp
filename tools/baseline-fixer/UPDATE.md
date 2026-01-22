# 更新说明

## 修复内容

已修复 Excel 报告解析问题，现在脚本可以正确识别报告格式。

### 问题原因

原始 Excel 报告前面包含主机信息（主机名、主机ID、操作系统等），真正的数据表头在第 9 行。

### 解决方案

脚本现在会自动检测报告格式：
1. 读取前 15 行，查找包含"规则ID"的行作为表头
2. 自动跳过主机信息行，从正确的位置开始解析数据
3. 支持多种报告格式（兼容性更好）

### 测试结果

使用示例报告测试：
- 总检查项：95 个
- HIGH/CRITICAL：21 个
- 有自动修复方案：19 个（2 个需要手动修复）

## 使用方法

在服务器上重新运行即可：

```bash
cd /tmp/baseline-fixer
python3 baseline_fix.py -f baseline_report_gcp-hk-g02-viker-app-01_20260121_152233.xlsx
```

预期输出：
```
✓ 检测到操作系统: CENTOS
✓ 已加载 188 条基线修复规则
✓ 成功加载报告: baseline_report_gcp-hk-g02-viker-app-01_20260121_152233.xlsx (表头在第 9 行)

筛选风险等级: HIGH, CRITICAL
找到 21 个检查项
其中 19 个有自动修复方案

? 选择要修复的项目 (空格选择，a全选，回车确认)
```

## 更新步骤

如果已经部署到服务器，只需替换 `baseline_fix.py` 文件：

```bash
# 在本地打包
cd /Users/kerbos/Workspaces/project/mxsec-platform/tools
tar czf baseline-fixer-updated.tar.gz baseline-fixer/

# 上传到服务器
scp baseline-fixer-updated.tar.gz user@server:/tmp/

# 在服务器上解压覆盖
cd /tmp
tar xzf baseline-fixer-updated.tar.gz
```

或者只替换脚本文件：

```bash
scp baseline-fixer/baseline_fix.py user@server:/tmp/baseline-fixer/
```
