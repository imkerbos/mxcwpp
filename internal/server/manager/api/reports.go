// Package api 提供 HTTP API 处理器
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// ReportsHandler 是报表 API 处理器
type ReportsHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewReportsHandler 创建报表处理器
func NewReportsHandler(db *gorm.DB, logger *zap.Logger) *ReportsHandler {
	return &ReportsHandler{
		db:     db,
		logger: logger,
	}
}

// GetStats 获取报表统计数据
// GET /api/v1/reports/stats
func (h *ReportsHandler) GetStats(c *gin.Context) {
	// 解析查询参数
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02", startTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 start_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	} else {
		// 默认：最近7天
		startTime = time.Now().AddDate(0, 0, -7)
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse("2006-01-02", endTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 end_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 23, 59, 59, 999999999, time.Local)
	} else {
		endTime = time.Now()
	}

	// 1. 主机统计
	var hostStats struct {
		Total   int64
		Online  int64
		Offline int64
	}

	h.db.Model(&model.Host{}).Count(&hostStats.Total)
	h.db.Model(&model.Host{}).Where("status = ?", "online").Count(&hostStats.Online)
	h.db.Model(&model.Host{}).Where("status = ?", "offline").Count(&hostStats.Offline)

	// 按操作系统统计
	var osFamilyStats []struct {
		OSFamily string
		Count    int64
	}
	h.db.Model(&model.Host{}).
		Select("os_family, COUNT(*) as count").
		Group("os_family").
		Find(&osFamilyStats)

	byOsFamily := make(map[string]int64)
	for _, stat := range osFamilyStats {
		if stat.OSFamily != "" {
			byOsFamily[stat.OSFamily] = stat.Count
		}
	}

	// 2. 基线检查统计（在时间范围内）
	var baselineStats struct {
		TotalChecks int64
		Passed      int64
		Failed      int64
		Warning     int64
	}

	baselineQuery := h.db.Model(&model.ScanResult{}).
		Where("checked_at >= ? AND checked_at <= ?", startTime, endTime)

	baselineQuery.Count(&baselineStats.TotalChecks)
	baselineQuery.Where("status = ?", "pass").Count(&baselineStats.Passed)
	baselineQuery.Where("status = ?", "fail").Count(&baselineStats.Failed)
	baselineQuery.Where("status = ?", "error").Count(&baselineStats.Warning) // error 作为 warning

	// 按严重级别统计
	var severityStats []struct {
		Severity string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("severity, COUNT(*) as count").
		Where("checked_at >= ? AND checked_at <= ? AND status = ?", startTime, endTime, "fail").
		Group("severity").
		Find(&severityStats)

	bySeverity := map[string]int64{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, stat := range severityStats {
		if stat.Severity != "" {
			bySeverity[stat.Severity] = stat.Count
		}
	}

	// 按类别统计
	var categoryStats []struct {
		Category string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("category, COUNT(*) as count").
		Where("checked_at >= ? AND checked_at <= ? AND status = ?", startTime, endTime, "fail").
		Group("category").
		Find(&categoryStats)

	byCategory := make(map[string]int64)
	for _, stat := range categoryStats {
		if stat.Category != "" {
			byCategory[stat.Category] = stat.Count
		}
	}

	// 3. 策略统计
	var policyStats struct {
		Total    int64
		Enabled  int64
		Disabled int64
	}

	h.db.Model(&model.Policy{}).Count(&policyStats.Total)
	h.db.Model(&model.Policy{}).Where("enabled = ?", true).Count(&policyStats.Enabled)
	h.db.Model(&model.Policy{}).Where("enabled = ?", false).Count(&policyStats.Disabled)

	// 计算平均通过率
	var avgPassRate float64
	if baselineStats.TotalChecks > 0 {
		avgPassRate = float64(baselineStats.Passed) / float64(baselineStats.TotalChecks) * 100.0
	}

	// 4. 任务统计
	var taskStats struct {
		Total     int64
		Completed int64
		Running   int64
		Failed    int64
	}

	h.db.Model(&model.ScanTask{}).Count(&taskStats.Total)
	h.db.Model(&model.ScanTask{}).Where("status = ?", "completed").Count(&taskStats.Completed)
	h.db.Model(&model.ScanTask{}).Where("status = ?", "running").Count(&taskStats.Running)
	h.db.Model(&model.ScanTask{}).Where("status = ?", "failed").Count(&taskStats.Failed)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"hostStats": gin.H{
				"total":      hostStats.Total,
				"online":     hostStats.Online,
				"offline":    hostStats.Offline,
				"byOsFamily": byOsFamily,
			},
			"baselineStats": gin.H{
				"totalChecks": baselineStats.TotalChecks,
				"passed":      baselineStats.Passed,
				"failed":      baselineStats.Failed,
				"warning":     baselineStats.Warning,
				"bySeverity":  bySeverity,
				"byCategory":  byCategory,
			},
			"policyStats": gin.H{
				"total":       policyStats.Total,
				"enabled":     policyStats.Enabled,
				"disabled":    policyStats.Disabled,
				"avgPassRate": avgPassRate,
			},
			"taskStats": gin.H{
				"total":     taskStats.Total,
				"completed": taskStats.Completed,
				"running":   taskStats.Running,
				"failed":    taskStats.Failed,
			},
		},
	})
}

// GetBaselineScoreTrend 获取基线得分趋势
// GET /api/v1/reports/baseline-score-trend
func (h *ReportsHandler) GetBaselineScoreTrend(c *gin.Context) {
	// 解析查询参数
	hostID := c.Query("host_id")
	policyID := c.Query("policy_id")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	interval := c.DefaultQuery("interval", "day") // hour, day, week, month

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02", startTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 start_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	} else {
		// 默认：最近7天
		startTime = time.Now().AddDate(0, 0, -7)
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse("2006-01-02", endTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 end_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 23, 59, 59, 999999999, time.Local)
	} else {
		endTime = time.Now()
	}

	// 确定时间间隔
	var timeStep time.Duration
	switch interval {
	case "hour":
		timeStep = time.Hour
	case "day":
		timeStep = 24 * time.Hour
	case "week":
		timeStep = 7 * 24 * time.Hour
	case "month":
		timeStep = 30 * 24 * time.Hour
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的 interval 参数，应为: hour, day, week, month",
		})
		return
	}

	// 构建查询
	query := h.db.Model(&model.ScanResult{}).
		Where("checked_at >= ? AND checked_at <= ?", startTime, endTime)

	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if policyID != "" {
		query = query.Where("policy_id = ?", policyID)
	}

	// 按时间分组查询结果
	type TimeGroupResult struct {
		Date   string
		Total  int64
		Passed int64
		Failed int64
		Error  int64
		NA     int64
	}

	var timeGroups []TimeGroupResult

	// 使用 SQL 按时间分组统计
	// 注意：不同数据库的日期格式化函数不同，这里使用 MySQL 的 DATE_FORMAT
	// 如果是 PostgreSQL，需要使用 to_char
	dateFormat := "DATE_FORMAT(checked_at, '%Y-%m-%d')"
	if interval == "hour" {
		dateFormat = "DATE_FORMAT(checked_at, '%Y-%m-%d %H:00:00')"
	}

	rawSQL := fmt.Sprintf(`
		SELECT 
			%s as date,
			COUNT(*) as total,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as failed,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error,
			SUM(CASE WHEN status = 'na' THEN 1 ELSE 0 END) as na
		FROM scan_results
		WHERE checked_at >= ? AND checked_at <= ?
	`, dateFormat)

	args := []interface{}{startTime, endTime}
	if hostID != "" {
		rawSQL += " AND host_id = ?"
		args = append(args, hostID)
	}
	if policyID != "" {
		rawSQL += " AND policy_id = ?"
		args = append(args, policyID)
	}

	rawSQL += " GROUP BY date ORDER BY date"

	if err := h.db.Raw(rawSQL, args...).Scan(&timeGroups).Error; err != nil {
		h.logger.Error("查询基线得分趋势失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询基线得分趋势失败",
		})
		return
	}

	// 生成完整的时间序列（填充缺失的日期）
	dates := make([]string, 0)
	scores := make([]float64, 0)
	passRates := make([]float64, 0)

	// 创建时间组映射
	timeGroupMap := make(map[string]TimeGroupResult)
	for _, group := range timeGroups {
		timeGroupMap[group.Date] = group
	}

	// 遍历时间范围，生成完整序列
	currentTime := startTime
	for currentTime.Before(endTime) || currentTime.Equal(endTime) {
		var dateStr string
		if interval == "hour" {
			dateStr = currentTime.Format("2006-01-02 15:00:00")
		} else {
			dateStr = currentTime.Format("2006-01-02")
		}

		dates = append(dates, dateStr)

		// 获取该时间点的统计数据
		group, exists := timeGroupMap[dateStr]
		if !exists {
			// 没有数据，使用默认值
			scores = append(scores, 0.0)
			passRates = append(passRates, 0.0)
		} else {
			// 计算得分和通过率
			// 得分计算：使用加权平均（参考 score.go 的逻辑）
			severityWeights := map[string]float64{
				"critical": 10.0,
				"high":     7.0,
				"medium":   4.0,
				"low":      1.0,
			}

			// 查询该时间点的详细数据以计算得分
			// 注意：这里需要根据 interval 构建正确的查询条件
			var detailQuery *gorm.DB
			if interval == "hour" {
				detailQuery = query.Where("DATE_FORMAT(checked_at, '%Y-%m-%d %H:00:00') = ?", dateStr)
			} else {
				detailQuery = query.Where("DATE_FORMAT(checked_at, '%Y-%m-%d') = ?", dateStr)
			}
			var detailResults []struct {
				Status   string
				Severity string
			}
			detailQuery.Select("status, severity").Find(&detailResults)

			totalWeight := 0.0
			passWeight := 0.0
			for _, result := range detailResults {
				weight := severityWeights[result.Severity]
				if weight == 0 {
					weight = 1.0
				}
				totalWeight += weight
				if result.Status == "pass" {
					passWeight += weight
				}
			}

			var score float64
			if totalWeight > 0 {
				score = (passWeight / totalWeight) * 100.0
			}

			var passRate float64
			if group.Total > 0 {
				passRate = float64(group.Passed) / float64(group.Total) * 100.0
			}

			scores = append(scores, score)
			passRates = append(passRates, passRate)
		}

		// 移动到下一个时间点
		currentTime = currentTime.Add(timeStep)
		if interval == "day" || interval == "week" || interval == "month" {
			// 对于天/周/月，对齐到当天开始
			currentTime = time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, time.Local)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"dates":     dates,
			"scores":    scores,
			"passRates": passRates,
		},
	})
}

// GetCheckResultTrend 获取检查结果趋势
// GET /api/v1/reports/check-result-trend
func (h *ReportsHandler) GetCheckResultTrend(c *gin.Context) {
	// 解析查询参数
	hostID := c.Query("host_id")
	policyID := c.Query("policy_id")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	interval := c.DefaultQuery("interval", "day") // hour, day, week, month

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02", startTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 start_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	} else {
		// 默认：最近7天
		startTime = time.Now().AddDate(0, 0, -7)
		startTime = time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, time.Local)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse("2006-01-02", endTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的 end_time 参数，格式应为 YYYY-MM-DD",
			})
			return
		}
		endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 23, 59, 59, 999999999, time.Local)
	} else {
		endTime = time.Now()
	}

	// 确定时间间隔
	var dateFormat string
	var timeStep time.Duration
	switch interval {
	case "hour":
		dateFormat = "DATE_FORMAT(checked_at, '%Y-%m-%d %H:00:00')"
		timeStep = time.Hour
	case "day":
		dateFormat = "DATE_FORMAT(checked_at, '%Y-%m-%d')"
		timeStep = 24 * time.Hour
	case "week":
		dateFormat = "DATE_FORMAT(checked_at, '%Y-%m-%d')"
		timeStep = 7 * 24 * time.Hour
	case "month":
		dateFormat = "DATE_FORMAT(checked_at, '%Y-%m-%d')"
		timeStep = 30 * 24 * time.Hour
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的 interval 参数，应为: hour, day, week, month",
		})
		return
	}

	// 构建查询
	query := h.db.Model(&model.ScanResult{}).
		Where("checked_at >= ? AND checked_at <= ?", startTime, endTime)

	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if policyID != "" {
		query = query.Where("policy_id = ?", policyID)
	}

	// 按时间分组查询结果
	type TimeGroupResult struct {
		Date   string
		Passed int64
		Failed int64
		Error  int64
	}

	var timeGroups []TimeGroupResult

	rawSQL := fmt.Sprintf(`
		SELECT 
			%s as date,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as failed,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error
		FROM scan_results
		WHERE checked_at >= ? AND checked_at <= ?
	`, dateFormat)

	args := []interface{}{startTime, endTime}
	if hostID != "" {
		rawSQL += " AND host_id = ?"
		args = append(args, hostID)
	}
	if policyID != "" {
		rawSQL += " AND policy_id = ?"
		args = append(args, policyID)
	}

	rawSQL += " GROUP BY date ORDER BY date"

	if err := h.db.Raw(rawSQL, args...).Scan(&timeGroups).Error; err != nil {
		h.logger.Error("查询检查结果趋势失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询检查结果趋势失败",
		})
		return
	}

	// 生成完整的时间序列（填充缺失的日期）
	dates := make([]string, 0)
	passed := make([]int64, 0)
	failed := make([]int64, 0)
	errorCount := make([]int64, 0)

	// 创建时间组映射
	timeGroupMap := make(map[string]TimeGroupResult)
	for _, group := range timeGroups {
		timeGroupMap[group.Date] = group
	}

	// 遍历时间范围，生成完整序列
	currentTime := startTime
	for currentTime.Before(endTime) || currentTime.Equal(endTime) {
		var dateStr string
		if interval == "hour" {
			dateStr = currentTime.Format("2006-01-02 15:00:00")
		} else {
			dateStr = currentTime.Format("2006-01-02")
		}

		dates = append(dates, dateStr)

		// 获取该时间点的统计数据
		group, exists := timeGroupMap[dateStr]
		if !exists {
			// 没有数据，使用默认值
			passed = append(passed, 0)
			failed = append(failed, 0)
			errorCount = append(errorCount, 0)
		} else {
			passed = append(passed, group.Passed)
			failed = append(failed, group.Failed)
			errorCount = append(errorCount, group.Error)
		}

		// 移动到下一个时间点
		currentTime = currentTime.Add(timeStep)
		if interval == "day" || interval == "week" || interval == "month" {
			// 对于天/周/月，对齐到当天开始
			currentTime = time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, time.Local)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"dates":   dates,
			"passed":  passed,
			"failed":  failed,
			"warning": errorCount, // error 作为 warning
		},
	})
}
