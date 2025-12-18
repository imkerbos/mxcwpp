// Package api 提供 HTTP API 处理器
package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/agentcenter/service"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// PoliciesHandler 是策略管理 API 处理器
type PoliciesHandler struct {
	service *service.PolicyService
	db      *gorm.DB
	logger  *zap.Logger
}

// NewPoliciesHandler 创建策略处理器
func NewPoliciesHandler(db *gorm.DB, logger *zap.Logger) *PoliciesHandler {
	return &PoliciesHandler{
		service: service.NewPolicyService(db, logger),
		db:      db,
		logger:  logger,
	}
}

// ListPolicies 获取策略列表
// GET /api/v1/policies
func (h *PoliciesHandler) ListPolicies(c *gin.Context) {
	// 解析查询参数
	osFamily := c.Query("os_family")
	enabledStr := c.Query("enabled")
	enabledOnly := enabledStr == "true"
	groupID := c.Query("group_id")

	// 查询策略
	policies, err := h.service.ListPolicies(enabledOnly)
	if err != nil {
		h.logger.Error("查询策略列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略列表失败",
		})
		return
	}

	// 过滤策略组（如果指定）
	var filteredPolicies []model.Policy
	if groupID != "" {
		for _, policy := range policies {
			if policy.GroupID == groupID {
				filteredPolicies = append(filteredPolicies, policy)
			}
		}
	} else {
		filteredPolicies = policies
	}

	// 过滤 OS 系列（如果指定）
	if osFamily != "" {
		var osFiltered []model.Policy
		for _, policy := range filteredPolicies {
			// 检查策略的 os_family 是否包含指定的 OS
			for _, pf := range policy.OSFamily {
				if strings.EqualFold(pf, osFamily) {
					osFiltered = append(osFiltered, policy)
					break
				}
			}
		}
		filteredPolicies = osFiltered
	}

	// 构建响应（包含规则数量）
	items := make([]gin.H, 0, len(filteredPolicies))
	for _, policy := range filteredPolicies {
		items = append(items, gin.H{
			"id":          policy.ID,
			"name":        policy.Name,
			"version":     policy.Version,
			"description": policy.Description,
			"os_family":   policy.OSFamily,
			"os_version":  policy.OSVersion,
			"enabled":     policy.Enabled,
			"group_id":    policy.GroupID,
			"rule_count":  len(policy.Rules),
			"created_at":  policy.CreatedAt,
			"updated_at":  policy.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": items,
		},
	})
}

// GetPolicy 获取策略详情
// GET /api/v1/policies/:policy_id
func (h *PoliciesHandler) GetPolicy(c *gin.Context) {
	policyID := c.Param("policy_id")

	policy, err := h.service.GetPolicy(policyID)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "策略不存在",
			})
			return
		}
		h.logger.Error("查询策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": policy,
	})
}

// CreatePolicyRequest 创建策略请求
type CreatePolicyRequest struct {
	ID             string                `json:"id" binding:"required"`
	Name           string                `json:"name" binding:"required"`
	Version        string                `json:"version"`
	Description    string                `json:"description"`
	OSFamily       []string              `json:"os_family"`
	OSVersion      string                `json:"os_version"`
	OSRequirements []model.OSRequirement `json:"os_requirements"` // 详细 OS 版本要求
	RuntimeTypes   []string              `json:"runtime_types"`   // 适用的运行时类型：["vm", "docker", "k8s"]
	Enabled        bool                  `json:"enabled"`
	GroupID        string                `json:"group_id"`
	Rules          []*RuleData           `json:"rules"`
}

// RuleData 规则数据
type RuleData struct {
	RuleID      string            `json:"rule_id" binding:"required"`
	Category    string            `json:"category"`
	Title       string            `json:"title" binding:"required"`
	Description string            `json:"description"`
	Severity    string            `json:"severity"`
	CheckConfig model.CheckConfig `json:"check_config"`
	FixConfig   model.FixConfig   `json:"fix_config"`
}

// CreatePolicy 创建策略
// POST /api/v1/policies
func (h *PoliciesHandler) CreatePolicy(c *gin.Context) {
	var req CreatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证必填字段
	if req.ID == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "策略 ID 和名称为必填项",
		})
		return
	}

	// 检查策略 ID 是否已存在
	_, err := h.service.GetPolicy(req.ID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "策略 ID 已存在",
		})
		return
	}

	// 验证规则数据
	for i, ruleData := range req.Rules {
		if ruleData.RuleID == "" || ruleData.Title == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "第 " + string(rune(i+1)) + " 条规则的 RuleID 和 Title 为必填项",
			})
			return
		}
	}

	// 创建策略
	policy := &model.Policy{
		ID:             req.ID,
		Name:           req.Name,
		Version:        req.Version,
		Description:    req.Description,
		OSFamily:       model.StringArray(req.OSFamily),
		OSVersion:      req.OSVersion,
		OSRequirements: model.OSRequirements(req.OSRequirements),
		RuntimeTypes:   model.StringArray(req.RuntimeTypes),
		Enabled:        req.Enabled,
		GroupID:        req.GroupID,
	}

	// 创建策略
	if err := h.service.CreatePolicy(policy); err != nil {
		h.logger.Error("创建策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建策略失败",
		})
		return
	}

	// 创建规则
	if len(req.Rules) > 0 {
		for _, ruleData := range req.Rules {
			rule := &model.Rule{
				RuleID:      ruleData.RuleID,
				PolicyID:    policy.ID,
				Category:    ruleData.Category,
				Title:       ruleData.Title,
				Description: ruleData.Description,
				Severity:    ruleData.Severity,
				CheckConfig: ruleData.CheckConfig,
				FixConfig:   ruleData.FixConfig,
			}
			if err := h.service.CreateRule(rule); err != nil {
				h.logger.Error("创建规则失败", zap.String("rule_id", rule.RuleID), zap.Error(err))
				// 继续创建其他规则
				continue
			}
		}
	}

	// 重新查询策略（包含规则）
	createdPolicy, err := h.service.GetPolicy(policy.ID)
	if err != nil {
		h.logger.Error("查询创建的策略失败", zap.Error(err))
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": createdPolicy,
	})
}

// UpdatePolicyRequest 更新策略请求
type UpdatePolicyRequest struct {
	Name           string                `json:"name"`
	Version        string                `json:"version"`
	Description    string                `json:"description"`
	OSFamily       []string              `json:"os_family"`
	OSVersion      string                `json:"os_version"`
	OSRequirements []model.OSRequirement `json:"os_requirements"` // 详细 OS 版本要求
	RuntimeTypes   []string              `json:"runtime_types"`   // 适用的运行时类型
	Enabled        *bool                 `json:"enabled"`
	GroupID        *string               `json:"group_id"`
	Rules          []*RuleData           `json:"rules"`
}

// UpdatePolicy 更新策略
// PUT /api/v1/policies/:policy_id
func (h *PoliciesHandler) UpdatePolicy(c *gin.Context) {
	policyID := c.Param("policy_id")

	// 检查策略是否存在
	policy, err := h.service.GetPolicy(policyID)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "策略不存在",
			})
			return
		}
		h.logger.Error("查询策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略失败",
		})
		return
	}

	// 解析请求
	var req UpdatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 更新字段
	if req.Name != "" {
		policy.Name = req.Name
	}
	if req.Version != "" {
		policy.Version = req.Version
	}
	if req.Description != "" {
		policy.Description = req.Description
	}
	if req.OSFamily != nil {
		policy.OSFamily = model.StringArray(req.OSFamily)
	}
	if req.OSVersion != "" {
		policy.OSVersion = req.OSVersion
	}
	if req.OSRequirements != nil {
		policy.OSRequirements = model.OSRequirements(req.OSRequirements)
	}
	if req.RuntimeTypes != nil {
		policy.RuntimeTypes = model.StringArray(req.RuntimeTypes)
	}
	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if req.GroupID != nil {
		policy.GroupID = *req.GroupID
	}

	// 更新策略
	if err := h.service.UpdatePolicy(policy); err != nil {
		h.logger.Error("更新策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新策略失败",
		})
		return
	}

	// 如果提供了规则列表，更新规则
	if req.Rules != nil {
		// 删除现有规则
		existingRules, _ := h.service.ListRules(policyID)
		for _, rule := range existingRules {
			if err := h.service.DeleteRule(rule.RuleID); err != nil {
				h.logger.Warn("删除规则失败", zap.String("rule_id", rule.RuleID), zap.Error(err))
			}
		}

		// 创建新规则
		for _, ruleData := range req.Rules {
			rule := &model.Rule{
				RuleID:      ruleData.RuleID,
				PolicyID:    policy.ID,
				Category:    ruleData.Category,
				Title:       ruleData.Title,
				Description: ruleData.Description,
				Severity:    ruleData.Severity,
				CheckConfig: ruleData.CheckConfig,
				FixConfig:   ruleData.FixConfig,
			}
			if err := h.service.CreateRule(rule); err != nil {
				h.logger.Error("创建规则失败", zap.String("rule_id", rule.RuleID), zap.Error(err))
				continue
			}
		}
	}

	// 重新查询策略
	updatedPolicy, err := h.service.GetPolicy(policyID)
	if err != nil {
		h.logger.Error("查询更新的策略失败", zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": updatedPolicy,
	})
}

// DeletePolicy 删除策略
// DELETE /api/v1/policies/:policy_id
func (h *PoliciesHandler) DeletePolicy(c *gin.Context) {
	policyID := c.Param("policy_id")

	// 检查策略是否存在
	_, err := h.service.GetPolicy(policyID)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "策略不存在",
			})
			return
		}
		h.logger.Error("查询策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略失败",
		})
		return
	}

	// 删除策略（会级联删除规则）
	if err := h.service.DeletePolicy(policyID); err != nil {
		h.logger.Error("删除策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除策略失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "策略已删除",
	})
}

// GetPolicyStatistics 获取策略统计信息
// GET /api/v1/policies/:policy_id/statistics
func (h *PoliciesHandler) GetPolicyStatistics(c *gin.Context) {
	policyID := c.Param("policy_id")

	// 检查策略是否存在
	policy, err := h.service.GetPolicy(policyID)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "策略不存在",
			})
			return
		}
		h.logger.Error("查询策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略失败",
		})
		return
	}

	// 查询该策略的所有检查结果
	var results []model.ScanResult
	if err := h.db.Where("policy_id = ?", policyID).Find(&results).Error; err != nil {
		h.logger.Error("查询策略统计失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略统计失败",
		})
		return
	}

	// 统计信息
	ruleCount := len(policy.Rules)
	hostIDs := make(map[string]bool)
	rulePassMap := make(map[string]map[string]int) // rule_id -> status -> count

	passCount := 0
	failCount := 0
	totalResults := len(results)

	for _, result := range results {
		hostIDs[result.HostID] = true

		if rulePassMap[result.RuleID] == nil {
			rulePassMap[result.RuleID] = make(map[string]int)
		}
		statusStr := string(result.Status)
		rulePassMap[result.RuleID][statusStr]++

		switch result.Status {
		case model.ResultStatusPass:
			passCount++
		case model.ResultStatusFail:
			failCount++
		}
	}

	hostCount := len(hostIDs)
	passRate := 0.0
	if totalResults > 0 {
		passRate = float64(passCount) / float64(totalResults) * 100.0
	}

	// 计算每个规则的通过率
	rulePassRates := make(map[string]float64)
	for ruleID, statusMap := range rulePassMap {
		total := 0
		pass := 0
		for status, count := range statusMap {
			total += count
			if status == "pass" {
				pass = count
			}
		}
		if total > 0 {
			rulePassRates[ruleID] = float64(pass) / float64(total) * 100.0
		} else {
			rulePassRates[ruleID] = 0.0
		}
	}

	// 计算风险项数量（有失败结果的规则数）
	riskRuleCount := 0
	for _, statusMap := range rulePassMap {
		if statusMap["fail"] > 0 {
			riskRuleCount++
		}
		// 如果没有结果，也认为是风险项（未检查）
		if len(statusMap) == 0 {
			riskRuleCount++
		}
	}

	// 查询最近检查时间
	var lastCheckTime *model.LocalTime
	if len(results) > 0 {
		// 找到最新的检查时间
		latest := results[0].CheckedAt
		for _, result := range results {
			if result.CheckedAt.After(latest) {
				latest = result.CheckedAt
			}
		}
		lastCheckTime = &latest
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"policy_id":       policyID,
			"pass_rate":       passRate,
			"host_count":      hostCount,
			"host_pass_count": 0, // TODO: 计算通过的主机数
			"rule_count":      ruleCount,
			"risk_rule_count": riskRuleCount,
			"last_check_time": lastCheckTime,
			"rule_pass_rates": rulePassRates,
		},
	})
}
