// Package service 提供任务管理服务
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	grpcProto "github.com/imkerbos/mxsec-platform/api/proto/grpc"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// TaskService 是任务服务
type TaskService struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewTaskService 创建任务服务实例
func NewTaskService(db *gorm.DB, logger *zap.Logger) *TaskService {
	return &TaskService{
		db:     db,
		logger: logger,
	}
}

// DispatchPendingTasks 分发待执行任务
// 查询 scan_tasks 表中状态为 pending 的任务，匹配主机并下发
func (s *TaskService) DispatchPendingTasks(transferService interface {
	SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
	// 查询待执行任务
	var tasks []model.ScanTask
	if err := s.db.Where("status = ?", model.TaskStatusPending).Find(&tasks).Error; err != nil {
		return fmt.Errorf("查询待执行任务失败: %w", err)
	}

	if len(tasks) == 0 {
		return nil // 没有待执行任务
	}

	s.logger.Info("发现待执行任务", zap.Int("count", len(tasks)))

	// 处理每个任务
	for _, task := range tasks {
		if err := s.dispatchTask(&task, transferService); err != nil {
			s.logger.Error("分发任务失败",
				zap.String("task_id", task.TaskID),
				zap.Error(err),
			)
			// 继续处理下一个任务
			continue
		}
	}

	return nil
}

// dispatchTask 分发单个任务
func (s *TaskService) dispatchTask(task *model.ScanTask, transferService interface {
	SendCommand(agentID string, cmd *grpcProto.Command) error
}) error {
	// 根据 target_type 匹配主机
	var hosts []model.Host

	// 构建基础查询（在线主机）
	baseQuery := s.db.Where("status = ?", model.HostStatusOnline)

	// 如果指定了运行时类型，添加筛选条件
	runtimeType := task.TargetConfig.RuntimeType
	if runtimeType != "" {
		if runtimeType == model.RuntimeTypeVM {
			// 虚拟机：runtime_type = 'vm' 或为空（兼容旧数据）
			baseQuery = baseQuery.Where("(runtime_type = ? OR runtime_type = '' OR runtime_type IS NULL)", model.RuntimeTypeVM)
		} else {
			// Docker 或 K8s：精确匹配
			baseQuery = baseQuery.Where("runtime_type = ?", runtimeType)
		}
		s.logger.Debug("按运行时类型筛选主机",
			zap.String("task_id", task.TaskID),
			zap.String("runtime_type", string(runtimeType)),
		)
	}

	switch task.TargetType {
	case model.TargetTypeAll:
		// 查询所有在线主机（已按 runtime_type 筛选）
		if err := baseQuery.Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	case model.TargetTypeHostIDs:
		// 查询指定主机 ID（已按 runtime_type 筛选）
		if len(task.TargetConfig.HostIDs) == 0 {
			return fmt.Errorf("target_config.host_ids 为空")
		}
		if err := baseQuery.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	case model.TargetTypeOSFamily:
		// 查询指定 OS 系列的主机（已按 runtime_type 筛选）
		if len(task.TargetConfig.OSFamily) == 0 {
			return fmt.Errorf("target_config.os_family 为空")
		}
		if err := baseQuery.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	default:
		return fmt.Errorf("未知的 target_type: %s", task.TargetType)
	}

	if len(hosts) == 0 {
		s.logger.Warn("没有匹配的在线主机，任务保持 pending 状态等待主机上线",
			zap.String("task_id", task.TaskID),
			zap.String("target_type", string(task.TargetType)),
		)
		// 保持 pending 状态，等待主机上线后重新调度
		// 不改变任务状态，让调度器下次继续尝试
		return nil
	}

	s.logger.Info("匹配到主机",
		zap.String("task_id", task.TaskID),
		zap.Int("host_count", len(hosts)),
	)

	// 获取所有策略ID（支持多策略）
	policyIDs := task.GetPolicyIDs()
	if len(policyIDs) == 0 {
		s.logger.Warn("任务没有关联策略，标记为失败",
			zap.String("task_id", task.TaskID),
		)
		s.db.Model(task).Update("status", model.TaskStatusFailed)
		return fmt.Errorf("任务没有关联策略")
	}

	// 查询第一个策略用于OS过滤（多策略场景下，所有策略应该兼容相同的OS）
	policyService := NewPolicyService(s.db, s.logger)
	firstPolicy, err := policyService.GetPolicy(policyIDs[0])
	if err != nil {
		return fmt.Errorf("查询策略失败: %w", err)
	}

	// 过滤不匹配策略 OS 要求的主机
	var matchedHosts []model.Host
	var skippedHosts []string
	for _, host := range hosts {
		if s.matchPolicyOS(firstPolicy, &host) {
			matchedHosts = append(matchedHosts, host)
		} else {
			skippedHosts = append(skippedHosts, host.HostID)
		}
	}

	// 记录被跳过的主机
	if len(skippedHosts) > 0 {
		s.logger.Info("部分主机不匹配策略 OS 要求，已跳过",
			zap.String("task_id", task.TaskID),
			zap.String("policy_os_family", fmt.Sprintf("%v", firstPolicy.OSFamily)),
			zap.String("policy_os_version", firstPolicy.OSVersion),
			zap.Int("skipped_count", len(skippedHosts)),
			zap.Strings("skipped_hosts", skippedHosts),
		)
	}

	// 检查是否还有匹配的主机
	if len(matchedHosts) == 0 {
		s.logger.Warn("没有匹配策略 OS 要求的在线主机，任务保持 pending 状态等待主机上线",
			zap.String("task_id", task.TaskID),
			zap.Strings("policy_ids", policyIDs),
			zap.String("policy_os_family", fmt.Sprintf("%v", firstPolicy.OSFamily)),
			zap.String("policy_os_version", firstPolicy.OSVersion),
		)
		// 保持 pending 状态，等待主机上线后重新调度
		return nil
	}

	s.logger.Info("OS 匹配后的主机数量",
		zap.String("task_id", task.TaskID),
		zap.Int("matched_count", len(matchedHosts)),
		zap.Int("original_count", len(hosts)),
	)

	// 在确认有匹配主机后，更新任务状态为 running
	// 注意：executed_at 已在 API 中设置（用户点击执行时），这里不再更新
	if err := s.db.Model(task).Update("status", model.TaskStatusRunning).Error; err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}

	// 查询所有策略和规则
	var allPolicies []*model.Policy
	var allRules []model.Rule
	var disabledRules []string

	for _, policyID := range policyIDs {
		policy, err := policyService.GetPolicy(policyID)
		if err != nil {
			s.logger.Error("查询策略失败",
				zap.String("task_id", task.TaskID),
				zap.String("policy_id", policyID),
				zap.Error(err),
			)
			continue
		}
		allPolicies = append(allPolicies, policy)

		// 收集启用的规则
		for _, rule := range policy.Rules {
			if rule.Enabled {
				allRules = append(allRules, rule)
			} else {
				disabledRules = append(disabledRules, rule.RuleID)
			}
		}
	}

	if len(allPolicies) == 0 {
		s.logger.Warn("没有有效的策略，标记为失败",
			zap.String("task_id", task.TaskID),
		)
		s.db.Model(task).Update("status", model.TaskStatusFailed)
		return fmt.Errorf("没有有效的策略")
	}

	// 记录被跳过的禁用规则
	if len(disabledRules) > 0 {
		s.logger.Info("部分规则已禁用，已跳过",
			zap.String("task_id", task.TaskID),
			zap.Int("disabled_count", len(disabledRules)),
		)
	}

	if len(allRules) == 0 {
		s.logger.Warn("没有启用的规则可执行，标记为失败",
			zap.String("task_id", task.TaskID),
			zap.Int("policy_count", len(allPolicies)),
			zap.Int("disabled_count", len(disabledRules)),
		)
		s.db.Model(task).Update("status", model.TaskStatusFailed)
		return fmt.Errorf("没有启用的规则可执行")
	}

	s.logger.Info("准备下发任务",
		zap.String("task_id", task.TaskID),
		zap.Int("policy_count", len(allPolicies)),
		zap.Int("rule_count", len(allRules)),
	)

	// 为每个匹配的主机下发任务（带重试）
	successCount := 0
	for _, host := range matchedHosts {
		// 使用重试机制下发任务
		err := Retry(context.Background(), func() error {
			return s.sendTaskToHostMultiPolicy(&host, task, allPolicies, transferService)
		}, DefaultRetryConfig, s.logger)

		if err != nil {
			s.logger.Error("下发任务到主机失败（已重试）",
				zap.String("task_id", task.TaskID),
				zap.String("host_id", host.HostID),
				zap.Error(err),
			)
			continue
		}
		successCount++
	}

	// 更新已下发主机数
	s.db.Model(task).Update("dispatched_host_count", successCount)

	// 如果没有成功下发到任何主机，标记为失败
	if successCount == 0 {
		s.logger.Warn("任务下发失败，没有成功下发到任何主机",
			zap.String("task_id", task.TaskID),
			zap.Int("matched_hosts", len(matchedHosts)),
		)
		s.db.Model(task).Updates(map[string]interface{}{
			"status":        model.TaskStatusFailed,
			"failed_reason": "没有成功下发到任何主机",
		})
		return fmt.Errorf("没有成功下发到任何主机")
	}

	s.logger.Info("任务分发完成",
		zap.String("task_id", task.TaskID),
		zap.Int("matched_hosts", len(matchedHosts)),
		zap.Int("skipped_hosts", len(skippedHosts)),
		zap.Int("success_count", successCount),
	)

	return nil
}

// sendTaskToHostMultiPolicy 向指定主机发送多策略任务
func (s *TaskService) sendTaskToHostMultiPolicy(
	host *model.Host,
	task *model.ScanTask,
	policies []*model.Policy,
	transferService interface {
		SendCommand(agentID string, cmd *grpcProto.Command) error
	},
) error {
	// 构建多策略数据
	policiesData := s.buildMultiPoliciesData(policies, host)

	// 构建任务数据（JSON）
	taskData := map[string]interface{}{
		"task_id":    task.TaskID,
		"policy_ids": task.GetPolicyIDs(),
		"policies":   policiesData,
		"os_family":  host.OSFamily,
		"os_version": host.OSVersion,
	}

	taskDataJSON, err := json.Marshal(taskData)
	if err != nil {
		return fmt.Errorf("序列化任务数据失败: %w", err)
	}

	// 构建 Task
	grpcTask := &grpcProto.Task{
		DataType:   8000,       // 基线检查任务
		ObjectName: "baseline", // 插件名称
		Data:       string(taskDataJSON),
		Token:      task.TaskID, // 使用 task_id 作为 token
	}

	// 构建 Command
	cmd := &grpcProto.Command{
		Tasks: []*grpcProto.Task{grpcTask},
	}

	// 发送命令
	if err := transferService.SendCommand(host.HostID, cmd); err != nil {
		return fmt.Errorf("发送命令失败: %w", err)
	}

	s.logger.Debug("多策略任务已下发",
		zap.String("task_id", task.TaskID),
		zap.String("host_id", host.HostID),
		zap.Int("policy_count", len(policies)),
	)

	return nil
}

// buildMultiPoliciesData 构建多策略数据
func (s *TaskService) buildMultiPoliciesData(policies []*model.Policy, host *model.Host) string {
	policiesArray := make([]map[string]interface{}, 0, len(policies))

	for _, policy := range policies {
		// 检查策略是否匹配主机OS
		if !s.matchPolicyOS(policy, host) {
			continue
		}

		// 检查策略是否匹配主机运行时类型
		if !policy.MatchesRuntimeType(host.RuntimeType) {
			s.logger.Debug("策略不适用于主机运行时类型",
				zap.String("policy_id", policy.ID),
				zap.String("host_id", host.HostID),
				zap.String("host_runtime_type", string(host.RuntimeType)),
				zap.Strings("policy_runtime_types", policy.RuntimeTypes))
			continue
		}

		// 收集启用的规则（并按运行时类型过滤）
		rulesList := make([]map[string]interface{}, 0)
		var skippedRules []string
		for _, rule := range policy.Rules {
			if !rule.Enabled {
				continue
			}
			// 检查规则是否匹配主机运行时类型
			if !rule.MatchesRuntimeType(host.RuntimeType) {
				skippedRules = append(skippedRules, rule.RuleID)
				continue
			}
			ruleData := map[string]interface{}{
				"rule_id":     rule.RuleID,
				"category":    rule.Category,
				"title":       rule.Title,
				"description": rule.Description,
				"severity":    rule.Severity,
				"check":       rule.CheckConfig,
				"fix":         rule.FixConfig,
			}
			rulesList = append(rulesList, ruleData)
		}

		// 记录被运行时类型过滤的规则
		if len(skippedRules) > 0 {
			s.logger.Debug("部分规则不适用于主机运行时类型，已跳过",
				zap.String("policy_id", policy.ID),
				zap.String("host_id", host.HostID),
				zap.String("host_runtime_type", string(host.RuntimeType)),
				zap.Int("skipped_count", len(skippedRules)))
		}

		if len(rulesList) == 0 {
			continue
		}

		policyData := map[string]interface{}{
			"id":          policy.ID,
			"name":        policy.Name,
			"version":     policy.Version,
			"description": policy.Description,
			"os_family":   policy.OSFamily,
			"os_version":  policy.OSVersion,
			"enabled":     policy.Enabled,
			"rules":       rulesList,
		}
		policiesArray = append(policiesArray, policyData)
	}

	policiesJSON, err := json.Marshal(policiesArray)
	if err != nil {
		s.logger.Error("序列化策略数据失败", zap.Error(err))
		return "[]"
	}

	return string(policiesJSON)
}

// sendTaskToHost 向指定主机发送任务
func (s *TaskService) sendTaskToHost(
	host *model.Host,
	task *model.ScanTask,
	policy *model.Policy,
	rules []model.Rule,
	transferService interface {
		SendCommand(agentID string, cmd *grpcProto.Command) error
	},
) error {
	// 构建策略配置（转换为 baseline plugin 期望的格式）
	policiesData := s.buildPoliciesData(policy, rules)

	// 构建任务数据（JSON）
	taskData := map[string]interface{}{
		"task_id":    task.TaskID,
		"policy_id":  task.PolicyID,
		"policies":   policiesData,
		"os_family":  host.OSFamily,
		"os_version": host.OSVersion,
	}

	taskDataJSON, err := json.Marshal(taskData)
	if err != nil {
		return fmt.Errorf("序列化任务数据失败: %w", err)
	}

	// 构建 Task
	grpcTask := &grpcProto.Task{
		DataType:   8000,       // 基线检查任务
		ObjectName: "baseline", // 插件名称
		Data:       string(taskDataJSON),
		Token:      task.TaskID, // 使用 task_id 作为 token
	}

	// 构建 Command
	cmd := &grpcProto.Command{
		Tasks: []*grpcProto.Task{grpcTask},
	}

	// 发送命令
	if err := transferService.SendCommand(host.HostID, cmd); err != nil {
		return fmt.Errorf("发送命令失败: %w", err)
	}

	s.logger.Debug("任务已下发",
		zap.String("task_id", task.TaskID),
		zap.String("host_id", host.HostID),
		zap.Int("rule_count", len(rules)),
	)

	return nil
}

// buildPoliciesData 构建策略数据（转换为 baseline plugin 期望的格式）
func (s *TaskService) buildPoliciesData(policy *model.Policy, rules []model.Rule) string {
	return s.buildPoliciesDataWithRuntime(policy, rules, "")
}

// buildPoliciesDataWithRuntime 构建策略数据（带运行时类型过滤）
func (s *TaskService) buildPoliciesDataWithRuntime(policy *model.Policy, rules []model.Rule, runtimeType model.RuntimeType) string {
	// 构建规则列表
	rulesList := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		// 如果指定了运行时类型，检查规则是否适用
		if runtimeType != "" && !rule.MatchesRuntimeType(runtimeType) {
			s.logger.Debug("规则不适用于运行时类型，已跳过",
				zap.String("rule_id", rule.RuleID),
				zap.String("runtime_type", string(runtimeType)),
				zap.Strings("rule_runtime_types", rule.RuntimeTypes))
			continue
		}
		// 转换规则格式（CheckConfig 和 FixConfig 已经是正确的格式）
		ruleData := map[string]interface{}{
			"rule_id":     rule.RuleID,
			"category":    rule.Category,
			"title":       rule.Title,
			"description": rule.Description,
			"severity":    rule.Severity,
			"check":       rule.CheckConfig, // CheckConfig 结构已匹配 engine.Check
			"fix":         rule.FixConfig,   // FixConfig 结构已匹配 engine.Fix
		}
		rulesList = append(rulesList, ruleData)
	}

	// 构建策略对象（baseline plugin 期望的格式）
	policyData := map[string]interface{}{
		"id":          policy.ID,
		"name":        policy.Name,
		"version":     policy.Version,
		"description": policy.Description,
		"os_family":   policy.OSFamily, // StringArray 会自动序列化为 JSON 数组
		"os_version":  policy.OSVersion,
		"enabled":     policy.Enabled,
		"rules":       rulesList,
	}

	// 序列化为 JSON 字符串（注意：返回的是单个策略数组的 JSON 字符串）
	// baseline plugin 期望 policies 是一个 JSON 字符串，解析后是 []map[string]interface{}
	policiesArray := []map[string]interface{}{policyData}
	policiesJSON, err := json.Marshal(policiesArray)
	if err != nil {
		s.logger.Error("序列化策略数据失败", zap.Error(err))
		return "[]"
	}

	return string(policiesJSON)
}

// matchPolicyOS 检查主机是否匹配策略的 OS 要求
func (s *TaskService) matchPolicyOS(policy *model.Policy, host *model.Host) bool {
	// 如果策略没有指定 OS Family，则匹配所有主机
	if len(policy.OSFamily) == 0 {
		return true
	}

	// 检查主机的 OS Family 是否在策略的 OS Family 列表中
	familyMatched := false
	for _, family := range policy.OSFamily {
		if strings.EqualFold(family, host.OSFamily) {
			familyMatched = true
			break
		}
	}

	if !familyMatched {
		return false
	}

	// 如果策略指定了 OS Version 约束，检查版本是否匹配
	if policy.OSVersion != "" {
		return matchVersionConstraint(host.OSVersion, policy.OSVersion)
	}

	return true
}

// matchVersionConstraint 检查版本是否满足约束条件
// 支持格式：>=7.0, >7.0, <=9.0, <9.0, 7.0（精确匹配）
func matchVersionConstraint(actual, constraint string) bool {
	if constraint == "" {
		return true
	}

	constraint = strings.TrimSpace(constraint)
	actual = strings.TrimSpace(actual)

	// 支持 >= 前缀
	if strings.HasPrefix(constraint, ">=") {
		version := strings.TrimSpace(constraint[2:])
		return compareVersionNumbers(actual, version) >= 0
	}

	// 支持 > 前缀
	if strings.HasPrefix(constraint, ">") {
		version := strings.TrimSpace(constraint[1:])
		return compareVersionNumbers(actual, version) > 0
	}

	// 支持 <= 前缀
	if strings.HasPrefix(constraint, "<=") {
		version := strings.TrimSpace(constraint[2:])
		return compareVersionNumbers(actual, version) <= 0
	}

	// 支持 < 前缀
	if strings.HasPrefix(constraint, "<") {
		version := strings.TrimSpace(constraint[1:])
		return compareVersionNumbers(actual, version) < 0
	}

	// 精确匹配
	return actual == constraint
}

// compareVersionNumbers 比较两个版本号
// 返回值：-1 表示 v1 < v2, 0 表示 v1 == v2, 1 表示 v1 > v2
func compareVersionNumbers(v1, v2 string) int {
	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var v1Num, v2Num int
		if i < len(v1Parts) {
			v1Num, _ = strconv.Atoi(strings.TrimSpace(v1Parts[i]))
		}
		if i < len(v2Parts) {
			v2Num, _ = strconv.Atoi(strings.TrimSpace(v2Parts[i]))
		}

		if v1Num < v2Num {
			return -1
		}
		if v1Num > v2Num {
			return 1
		}
	}

	return 0
}
