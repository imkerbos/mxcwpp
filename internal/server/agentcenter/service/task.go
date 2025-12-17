// Package service 提供任务管理服务
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	grpcProto "github.com/mxcsec-platform/mxcsec-platform/api/proto/grpc"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
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
	// 更新任务状态为 running
	now := time.Now()
	if err := s.db.Model(task).Updates(map[string]interface{}{
		"status":      model.TaskStatusRunning,
		"executed_at": &now,
	}).Error; err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}

	// 根据 target_type 匹配主机
	var hosts []model.Host
	switch task.TargetType {
	case model.TargetTypeAll:
		// 查询所有在线主机
		if err := s.db.Where("status = ?", model.HostStatusOnline).Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	case model.TargetTypeHostIDs:
		// 查询指定主机 ID
		if len(task.TargetConfig.HostIDs) == 0 {
			return fmt.Errorf("target_config.host_ids 为空")
		}
		if err := s.db.Where("host_id IN ? AND status = ?", task.TargetConfig.HostIDs, model.HostStatusOnline).Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	case model.TargetTypeOSFamily:
		// 查询指定 OS 系列的主机
		if len(task.TargetConfig.OSFamily) == 0 {
			return fmt.Errorf("target_config.os_family 为空")
		}
		if err := s.db.Where("os_family IN ? AND status = ?", task.TargetConfig.OSFamily, model.HostStatusOnline).Find(&hosts).Error; err != nil {
			return fmt.Errorf("查询主机失败: %w", err)
		}

	default:
		return fmt.Errorf("未知的 target_type: %s", task.TargetType)
	}

	if len(hosts) == 0 {
		s.logger.Warn("没有匹配的主机",
			zap.String("task_id", task.TaskID),
			zap.String("target_type", string(task.TargetType)),
		)
		// 更新任务状态为 completed（因为没有主机需要执行）
		s.db.Model(task).Update("status", model.TaskStatusCompleted)
		return nil
	}

	s.logger.Info("匹配到主机",
		zap.String("task_id", task.TaskID),
		zap.Int("host_count", len(hosts)),
	)

	// 查询策略和规则
	policyService := NewPolicyService(s.db, s.logger)
	policy, err := policyService.GetPolicy(task.PolicyID)
	if err != nil {
		return fmt.Errorf("查询策略失败: %w", err)
	}

	// 过滤不匹配策略 OS 要求的主机
	var matchedHosts []model.Host
	var skippedHosts []string
	for _, host := range hosts {
		if s.matchPolicyOS(policy, &host) {
			matchedHosts = append(matchedHosts, host)
		} else {
			skippedHosts = append(skippedHosts, host.HostID)
		}
	}

	// 记录被跳过的主机
	if len(skippedHosts) > 0 {
		s.logger.Info("部分主机不匹配策略 OS 要求，已跳过",
			zap.String("task_id", task.TaskID),
			zap.String("policy_os_family", fmt.Sprintf("%v", policy.OSFamily)),
			zap.String("policy_os_version", policy.OSVersion),
			zap.Int("skipped_count", len(skippedHosts)),
			zap.Strings("skipped_hosts", skippedHosts),
		)
	}

	// 检查是否还有匹配的主机
	if len(matchedHosts) == 0 {
		s.logger.Warn("没有匹配策略 OS 要求的主机",
			zap.String("task_id", task.TaskID),
			zap.String("policy_id", task.PolicyID),
			zap.String("policy_os_family", fmt.Sprintf("%v", policy.OSFamily)),
			zap.String("policy_os_version", policy.OSVersion),
		)
		// 更新任务状态为 completed（因为没有匹配的主机需要执行）
		s.db.Model(task).Update("status", model.TaskStatusCompleted)
		return nil
	}

	s.logger.Info("OS 匹配后的主机数量",
		zap.String("task_id", task.TaskID),
		zap.Int("matched_count", len(matchedHosts)),
		zap.Int("original_count", len(hosts)),
	)

	// 如果指定了 rule_ids，过滤规则
	var rules []model.Rule
	var disabledRules []string
	if len(task.RuleIDs) > 0 {
		for _, ruleID := range task.RuleIDs {
			for _, rule := range policy.Rules {
				if rule.RuleID == ruleID {
					// 检查规则是否启用
					if rule.Enabled {
						rules = append(rules, rule)
					} else {
						disabledRules = append(disabledRules, rule.RuleID)
					}
					break
				}
			}
		}
	} else {
		// 使用策略中的所有启用的规则
		for _, rule := range policy.Rules {
			if rule.Enabled {
				rules = append(rules, rule)
			} else {
				disabledRules = append(disabledRules, rule.RuleID)
			}
		}
	}

	// 记录被跳过的禁用规则
	if len(disabledRules) > 0 {
		s.logger.Info("部分规则已禁用，已跳过",
			zap.String("task_id", task.TaskID),
			zap.Int("disabled_count", len(disabledRules)),
			zap.Strings("disabled_rules", disabledRules),
		)
	}

	if len(rules) == 0 {
		s.logger.Warn("没有启用的规则可执行",
			zap.String("task_id", task.TaskID),
			zap.String("policy_id", task.PolicyID),
			zap.Int("disabled_count", len(disabledRules)),
		)
		// 更新任务状态为 completed（因为没有规则需要执行）
		s.db.Model(task).Update("status", model.TaskStatusCompleted)
		return nil
	}

	s.logger.Info("准备下发的规则数量",
		zap.String("task_id", task.TaskID),
		zap.Int("enabled_count", len(rules)),
		zap.Int("disabled_count", len(disabledRules)),
	)

	// 为每个匹配的主机下发任务（带重试）
	successCount := 0
	for _, host := range matchedHosts {
		// 使用重试机制下发任务
		err := Retry(context.Background(), func() error {
			return s.sendTaskToHost(&host, task, policy, rules, transferService)
		}, DefaultRetryConfig, s.logger)

		if err != nil {
			s.logger.Error("下发任务到主机失败（已重试）",
				zap.String("task_id", task.TaskID),
				zap.String("host_id", host.HostID),
				zap.Error(err),
			)
			// 标记任务为失败（可选：可以只标记部分失败）
			// 这里我们继续处理其他主机，不立即标记任务失败
			continue
		}
		successCount++
	}

	s.logger.Info("任务分发完成",
		zap.String("task_id", task.TaskID),
		zap.Int("matched_hosts", len(matchedHosts)),
		zap.Int("skipped_hosts", len(skippedHosts)),
		zap.Int("success_count", successCount),
	)

	return nil
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
	// 构建规则列表
	rulesList := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
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
