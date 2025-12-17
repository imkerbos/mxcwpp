// Package api 提供 HTTP API 处理器
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// ResultsHandler 是检测结果 API 处理器
type ResultsHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewResultsHandler 创建结果处理器
func NewResultsHandler(db *gorm.DB, logger *zap.Logger) *ResultsHandler {
	return &ResultsHandler{
		db:     db,
		logger: logger,
	}
}

// ListResults 获取检测结果列表
// GET /api/v1/results
func (h *ResultsHandler) ListResults(c *gin.Context) {
	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	hostID := c.Query("host_id")
	ruleID := c.Query("rule_id")
	policyID := c.Query("policy_id")
	taskID := c.Query("task_id")
	status := c.Query("status")
	severity := c.Query("severity")

	// 构建查询
	query := h.db.Model(&model.ScanResult{})

	// 过滤条件
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if ruleID != "" {
		query = query.Where("rule_id = ?", ruleID)
	}
	if policyID != "" {
		query = query.Where("policy_id = ?", policyID)
	}
	if taskID != "" {
		query = query.Where("task_id = ?", taskID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}

	// 计算总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询结果总数失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询检测结果失败",
		})
		return
	}

	// 分页查询
	var results []model.ScanResult
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("checked_at DESC").Find(&results).Error; err != nil {
		h.logger.Error("查询检测结果失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询检测结果失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": results,
		},
	})
}

// GetResult 获取检测结果详情
// GET /api/v1/results/:result_id
func (h *ResultsHandler) GetResult(c *gin.Context) {
	resultID := c.Param("result_id")

	var result model.ScanResult
	if err := h.db.Where("result_id = ?", resultID).Preload("Host").Preload("Rule").First(&result).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "检测结果不存在",
			})
			return
		}
		h.logger.Error("查询检测结果失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询检测结果失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": result,
	})
}

// GetHostBaselineScore 获取主机基线得分
// GET /api/v1/results/host/:host_id/score
func (h *ResultsHandler) GetHostBaselineScore(c *gin.Context) {
	hostID := c.Param("host_id")

	// 查询主机最新的检测结果（按规则分组，取最新的）
	var latestResults []struct {
		RuleID   string
		Status   string
		Severity string
	}

	// 使用子查询获取每个规则的最新结果
	subQuery := h.db.Model(&model.ScanResult{}).
		Select("rule_id, MAX(checked_at) as max_checked_at").
		Where("host_id = ?", hostID).
		Group("rule_id")

	if err := h.db.Table("scan_results").
		Select("scan_results.rule_id, scan_results.status, scan_results.severity").
		Joins("INNER JOIN (?) AS latest ON scan_results.rule_id = latest.rule_id AND scan_results.checked_at = latest.max_checked_at", subQuery).
		Where("scan_results.host_id = ?", hostID).
		Find(&latestResults).Error; err != nil {
		h.logger.Error("查询主机基线得分失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机基线得分失败",
		})
		return
	}

	// 计算得分
	if len(latestResults) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"host_id":        hostID,
				"baseline_score": 0,
				"pass_rate":      0.0,
				"total_rules":    0,
				"pass_count":     0,
				"fail_count":     0,
				"error_count":    0,
				"na_count":       0,
			},
		})
		return
	}

	// 统计
	totalRules := len(latestResults)
	passCount := 0
	failCount := 0
	errorCount := 0
	naCount := 0

	// 严重级别权重
	severityWeights := map[string]float64{
		"critical": 10.0,
		"high":     7.0,
		"medium":   4.0,
		"low":      1.0,
	}

	totalWeight := 0.0
	passWeight := 0.0

	for _, result := range latestResults {
		weight := severityWeights[result.Severity]
		if weight == 0 {
			weight = 1.0 // 默认权重
		}
		totalWeight += weight

		switch result.Status {
		case "pass":
			passCount++
			passWeight += weight
		case "fail":
			failCount++
		case "error":
			errorCount++
		case "na":
			naCount++
		}
	}

	// 计算得分（0-100）
	baselineScore := 0.0
	if totalWeight > 0 {
		baselineScore = (passWeight / totalWeight) * 100.0
	}

	// 计算通过率
	passRate := float64(passCount) / float64(totalRules)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"host_id":        hostID,
			"baseline_score": int(baselineScore),
			"pass_rate":      passRate,
			"total_rules":    totalRules,
			"pass_count":     passCount,
			"fail_count":     failCount,
			"error_count":    errorCount,
			"na_count":       naCount,
			"calculated_at":  time.Now(),
		},
	})
}

// GetHostBaselineSummary 获取主机基线摘要（按严重级别统计）
// GET /api/v1/results/host/:host_id/summary
func (h *ResultsHandler) GetHostBaselineSummary(c *gin.Context) {
	hostID := c.Param("host_id")

	// 查询主机最新的检测结果（按规则分组，取最新的）
	var latestResults []struct {
		RuleID   string
		Status   string
		Severity string
		Category string
	}

	subQuery := h.db.Model(&model.ScanResult{}).
		Select("rule_id, MAX(checked_at) as max_checked_at").
		Where("host_id = ?", hostID).
		Group("rule_id")

	if err := h.db.Table("scan_results").
		Select("scan_results.rule_id, scan_results.status, scan_results.severity, scan_results.category").
		Joins("INNER JOIN (?) AS latest ON scan_results.rule_id = latest.rule_id AND scan_results.checked_at = latest.max_checked_at", subQuery).
		Where("scan_results.host_id = ?", hostID).
		Find(&latestResults).Error; err != nil {
		h.logger.Error("查询主机基线摘要失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机基线摘要失败",
		})
		return
	}

	// 按严重级别和状态统计
	summary := gin.H{
		"host_id": hostID,
		"by_severity": gin.H{
			"critical": gin.H{"pass": 0, "fail": 0, "error": 0, "na": 0},
			"high":     gin.H{"pass": 0, "fail": 0, "error": 0, "na": 0},
			"medium":   gin.H{"pass": 0, "fail": 0, "error": 0, "na": 0},
			"low":      gin.H{"pass": 0, "fail": 0, "error": 0, "na": 0},
		},
		"by_category": make(map[string]gin.H),
	}

	for _, result := range latestResults {
		// 按严重级别统计
		if severityMap, ok := summary["by_severity"].(gin.H)[result.Severity].(gin.H); ok {
			if count, ok := severityMap[result.Status].(int); ok {
				severityMap[result.Status] = count + 1
			}
		}

		// 按类别统计
		if categoryMap, ok := summary["by_category"].(map[string]gin.H); ok {
			if _, exists := categoryMap[result.Category]; !exists {
				categoryMap[result.Category] = gin.H{"pass": 0, "fail": 0, "error": 0, "na": 0}
			}
			if count, ok := categoryMap[result.Category][result.Status].(int); ok {
				categoryMap[result.Category][result.Status] = count + 1
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": summary,
	})
}
