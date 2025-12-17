// Package engine 提供基线检查引擎
package engine

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Engine 是基线检查引擎
type Engine struct {
	logger   *zap.Logger
	checkers map[string]Checker // 检查器注册表
}

// NewEngine 创建新的检查引擎
func NewEngine(logger *zap.Logger) *Engine {
	engine := &Engine{
		logger:   logger,
		checkers: make(map[string]Checker),
	}

	// 注册内置检查器
	engine.RegisterChecker("file_kv", NewFileKVChecker(logger))
	engine.RegisterChecker("file_exists", NewFileExistsChecker(logger))
	engine.RegisterChecker("file_permission", NewFilePermissionChecker(logger))
	engine.RegisterChecker("file_line_match", NewFileLineMatchChecker(logger))
	engine.RegisterChecker("command_exec", NewCommandExecChecker(logger))
	engine.RegisterChecker("sysctl", NewSysctlChecker(logger))
	engine.RegisterChecker("service_status", NewServiceStatusChecker(logger))
	engine.RegisterChecker("file_owner", NewFileOwnerChecker(logger))
	engine.RegisterChecker("package_installed", NewPackageInstalledChecker(logger))

	return engine
}

// RegisterChecker 注册检查器
func (e *Engine) RegisterChecker(name string, checker Checker) {
	e.checkers[name] = checker
}

// Execute 执行基线检查
func (e *Engine) Execute(ctx context.Context, policies []*Policy, osFamily, osVersion string) []*Result {
	var results []*Result

	for _, policy := range policies {
		// OS 匹配
		if !policy.MatchOS(osFamily, osVersion) {
			e.logger.Debug("policy OS mismatch",
				zap.String("policy_id", policy.ID),
				zap.String("os_family", osFamily),
				zap.String("os_version", osVersion))
			continue
		}

		// 执行规则
		for _, rule := range policy.Rules {
			result := e.executeRule(ctx, policy, rule)
			if result != nil {
				results = append(results, result)
			}
		}
	}

	return results
}

// executeRule 执行单条规则
func (e *Engine) executeRule(ctx context.Context, policy *Policy, rule *Rule) *Result {
	result := &Result{
		RuleID:        rule.RuleID,
		PolicyID:      policy.ID,
		Severity:      rule.Severity,
		Category:      rule.Category,
		Title:         rule.Title,
		CheckedAt:     time.Now(),
		Status:        StatusPass,
		FixSuggestion: rule.Fix.Suggestion,
	}

	// 执行检查
	checkResult, err := e.executeCheck(ctx, rule.Check)
	if err != nil {
		result.Status = StatusError
		result.Actual = fmt.Sprintf("检查执行失败: %v", err)
		return result
	}

	// 根据检查结果设置状态
	if checkResult.Pass {
		result.Status = StatusPass
		result.Actual = checkResult.Actual
		result.Expected = checkResult.Expected
	} else {
		result.Status = StatusFail
		result.Actual = checkResult.Actual
		result.Expected = checkResult.Expected
	}

	return result
}

// executeCheck 执行检查项
func (e *Engine) executeCheck(ctx context.Context, check *Check) (*CheckResult, error) {
	// 处理条件组合
	switch check.Condition {
	case "all":
		// 所有子检查都通过才通过
		if len(check.Rules) == 0 {
			return nil, fmt.Errorf("no check rules defined")
		}
		for _, subCheck := range check.Rules {
			result, err := e.executeSingleCheck(ctx, subCheck)
			if err != nil {
				return nil, err
			}
			if !result.Pass {
				return result, nil
			}
		}
		return &CheckResult{Pass: true}, nil

	case "any":
		// 任一子检查通过即通过
		if len(check.Rules) == 0 {
			return nil, fmt.Errorf("no check rules defined")
		}
		for _, subCheck := range check.Rules {
			result, err := e.executeSingleCheck(ctx, subCheck)
			if err != nil {
				continue
			}
			if result.Pass {
				return result, nil
			}
		}
		return &CheckResult{Pass: false, Actual: "所有检查项均未通过"}, nil

	case "none":
		// 所有子检查都不通过才通过
		if len(check.Rules) == 0 {
			return nil, fmt.Errorf("no check rules defined")
		}
		for _, subCheck := range check.Rules {
			result, err := e.executeSingleCheck(ctx, subCheck)
			if err != nil {
				continue
			}
			if result.Pass {
				return &CheckResult{Pass: false, Actual: "存在通过的检查项"}, nil
			}
		}
		return &CheckResult{Pass: true}, nil

	default:
		// 默认：单个检查
		if len(check.Rules) == 0 {
			return nil, fmt.Errorf("no check rules defined")
		}
		return e.executeSingleCheck(ctx, check.Rules[0])
	}
}

// executeSingleCheck 执行单个检查
func (e *Engine) executeSingleCheck(ctx context.Context, checkRule *CheckRule) (*CheckResult, error) {
	checker, exists := e.checkers[checkRule.Type]
	if !exists {
		return nil, fmt.Errorf("unknown check type: %s", checkRule.Type)
	}

	return checker.Check(ctx, checkRule)
}
