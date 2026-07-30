package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/matrixplusio/mxcwpp/internal/server/model"
)

// execCheckerType 是会在目标主机上起 shell 的检查器类型。
const execCheckerType = "command_exec"

// checkConfigHasExec 判断检查配置是否包含 command_exec。
func checkConfigHasExec(cfg model.CheckConfig) bool {
	for _, r := range cfg.Rules {
		if strings.EqualFold(strings.TrimSpace(r.Type), execCheckerType) {
			return true
		}
	}
	return false
}

// fixConfigHasExec 判断修复配置是否携带修复命令。
func fixConfigHasExec(cfg model.FixConfig) bool {
	return strings.TrimSpace(cfg.Command) != ""
}

// guardCustomExecRule 拦截自定义规则携带可执行内容的写入。
//
// command_exec 的参数与 fix.command 都会以 root 在全部目标主机上执行，所以放开自定义
// 规则的这两个字段，等于把"基线配置权限"提权成"全舰队任意代码执行"。这条闸门只作用于
// 写入路径：内置规则（随发布同步、builtin=true）不受限，存量规则也不受影响——本次收敛的
// 是提权路径本身，不是回溯清理既有资产。
//
// allowCustomExec 为 true 时（server.security.allow_custom_exec_rules）放行，供确有
// 自定义可执行规则需求的环境显式启用。
func guardCustomExecRule(allowCustomExec, builtin bool, check model.CheckConfig, fix model.FixConfig) error {
	if allowCustomExec || builtin {
		return nil
	}
	var offending []string
	if checkConfigHasExec(check) {
		offending = append(offending, "check_config 含 command_exec 检查")
	}
	if fixConfigHasExec(fix) {
		offending = append(offending, "fix_config.command 含修复命令")
	}
	if len(offending) == 0 {
		return nil
	}
	return fmt.Errorf(
		"自定义规则不允许携带在主机上执行的命令（%s）。这些内容会以 root 在全部目标主机执行；"+
			"如确需此能力，请由管理员开启 server.security.allow_custom_exec_rules",
		strings.Join(offending, "；"))
}

// guardImportedPolicy 对导入的策略逐条规则套用同一把闸门。
// 导入的规则一律落为自定义规则（builtin=false），因此与逐条创建走同样的判定。
func guardImportedPolicy(allowCustomExec bool, policy *PolicyExportFormat) error {
	if allowCustomExec {
		return nil
	}
	for _, rule := range policy.Rules {
		var check model.CheckConfig
		var fix model.FixConfig
		// Check/Fix 是 map[string]any，转换失败按"无法确认内容安全"处理，不静默放行。
		if b, err := json.Marshal(rule.Check); err == nil {
			if err := json.Unmarshal(b, &check); err != nil {
				return fmt.Errorf("规则 %s 的 check 配置无法解析，拒绝导入: %w", rule.RuleID, err)
			}
		}
		if b, err := json.Marshal(rule.Fix); err == nil {
			if err := json.Unmarshal(b, &fix); err != nil {
				return fmt.Errorf("规则 %s 的 fix 配置无法解析，拒绝导入: %w", rule.RuleID, err)
			}
		}
		if err := guardCustomExecRule(allowCustomExec, false, check, fix); err != nil {
			return fmt.Errorf("策略 %s 的规则 %s：%w", policy.ID, rule.RuleID, err)
		}
	}
	return nil
}
