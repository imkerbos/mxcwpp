// Package api 提供 HTTP API 处理器
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
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

// TaskReportSummary 任务报告概要
type TaskReportSummary struct {
	TaskID      string     `json:"task_id"`
	TaskName    string     `json:"task_name"`
	PolicyID    string     `json:"policy_id"`    // 兼容旧版本
	PolicyIDs   []string   `json:"policy_ids"`   // 新版本：多策略ID
	PolicyName  string     `json:"policy_name"`  // 策略名称（多策略时显示数量）
	PolicyNames []string   `json:"policy_names"` // 新版本：策略名称列表
	ExecutedAt  *time.Time `json:"executed_at"`
	CompletedAt *time.Time `json:"completed_at"`
	HostCount   int        `json:"host_count"`
	RuleCount   int        `json:"rule_count"`
	Status      string     `json:"status"`
}

// TaskReportStatistics 任务报告统计
type TaskReportStatistics struct {
	TotalChecks   int64            `json:"total_checks"`
	PassedChecks  int64            `json:"passed_checks"`
	FailedChecks  int64            `json:"failed_checks"`
	WarningChecks int64            `json:"warning_checks"`
	NAChecks      int64            `json:"na_checks"`
	PassRate      float64          `json:"pass_rate"`
	BySeverity    map[string]int64 `json:"by_severity"`
	ByCategory    map[string]int64 `json:"by_category"`
}

// HostCheckDetail 主机检查明细
type HostCheckDetail struct {
	HostID        string  `json:"host_id"`
	Hostname      string  `json:"hostname"`
	IP            string  `json:"ip"`
	OSFamily      string  `json:"os_family"`
	PassedCount   int64   `json:"passed_count"`
	FailedCount   int64   `json:"failed_count"`
	WarningCount  int64   `json:"warning_count"`
	NACount       int64   `json:"na_count"`
	Score         float64 `json:"score"`
	Status        string  `json:"status"` // pass/warning/fail
	CriticalFails int64   `json:"critical_fails"`
	HighFails     int64   `json:"high_fails"`
}

// FailedRuleSummary 失败规则汇总
type FailedRuleSummary struct {
	RuleID        string   `json:"rule_id"`
	Title         string   `json:"title"`
	Severity      string   `json:"severity"`
	Category      string   `json:"category"`
	AffectedHosts []string `json:"affected_hosts"`
	AffectedCount int      `json:"affected_count"`
	FixSuggestion string   `json:"fix_suggestion"`
	Expected      string   `json:"expected"`
}

// GetTaskReport 获取任务报告
// GET /api/v1/reports/task/:task_id
func (h *ReportsHandler) GetTaskReport(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "task_id 参数不能为空",
		})
		return
	}

	// 1. 获取任务信息
	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		h.logger.Error("查询任务失败", zap.String("task_id", taskID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	// 2. 获取策略信息
	var policy model.Policy
	policyName := ""
	if task.PolicyID != "" {
		if err := h.db.Where("id = ?", task.PolicyID).First(&policy).Error; err == nil {
			policyName = policy.Name
		}
	}

	// 3. 获取检查结果统计
	var stats struct {
		TotalChecks   int64
		PassedChecks  int64
		FailedChecks  int64
		WarningChecks int64
		NAChecks      int64
	}

	baseQuery := h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID)
	baseQuery.Count(&stats.TotalChecks)

	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "pass").Count(&stats.PassedChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "fail").Count(&stats.FailedChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "error").Count(&stats.WarningChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "na").Count(&stats.NAChecks)

	// 计算通过率
	passRate := 0.0
	if stats.TotalChecks > 0 {
		passRate = float64(stats.PassedChecks) / float64(stats.TotalChecks) * 100.0
	}

	// 按严重级别统计（失败项）
	var severityStats []struct {
		Severity string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("severity, COUNT(*) as count").
		Where("task_id = ? AND status = ?", taskID, "fail").
		Group("severity").
		Find(&severityStats)

	bySeverity := map[string]int64{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, s := range severityStats {
		if s.Severity != "" {
			bySeverity[s.Severity] = s.Count
		}
	}

	// 按类别统计（失败项）
	var categoryStats []struct {
		Category string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("category, COUNT(*) as count").
		Where("task_id = ? AND status = ?", taskID, "fail").
		Group("category").
		Find(&categoryStats)

	byCategory := make(map[string]int64)
	for _, s := range categoryStats {
		if s.Category != "" {
			byCategory[s.Category] = s.Count
		}
	}

	// 4. 获取涉及的主机数和规则数
	var hostCount int64
	h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID).Distinct("host_id").Count(&hostCount)

	var ruleCount int64
	h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID).Distinct("rule_id").Count(&ruleCount)

	// 5. 获取主机检查明细
	type hostStatRow struct {
		HostID        string
		PassedCount   int64
		FailedCount   int64
		WarningCount  int64
		NACount       int64
		CriticalFails int64
		HighFails     int64
	}
	var hostStats []hostStatRow

	h.db.Raw(`
		SELECT
			host_id,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed_count,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as failed_count,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as warning_count,
			SUM(CASE WHEN status = 'na' THEN 1 ELSE 0 END) as na_count,
			SUM(CASE WHEN status = 'fail' AND severity = 'critical' THEN 1 ELSE 0 END) as critical_fails,
			SUM(CASE WHEN status = 'fail' AND severity = 'high' THEN 1 ELSE 0 END) as high_fails
		FROM scan_results
		WHERE task_id = ?
		GROUP BY host_id
	`, taskID).Scan(&hostStats)

	// 获取主机信息
	hostIDs := make([]string, 0, len(hostStats))
	for _, hs := range hostStats {
		hostIDs = append(hostIDs, hs.HostID)
	}

	var hosts []model.Host
	if len(hostIDs) > 0 {
		h.db.Where("host_id IN ?", hostIDs).Find(&hosts)
	}

	hostMap := make(map[string]model.Host)
	for _, host := range hosts {
		hostMap[host.HostID] = host
	}

	// 构建主机明细列表
	hostDetails := make([]HostCheckDetail, 0, len(hostStats))
	for _, hs := range hostStats {
		host := hostMap[hs.HostID]
		ip := ""
		if len(host.IPv4) > 0 {
			ip = host.IPv4[0]
		}

		// 计算得分（加权平均）
		totalChecks := hs.PassedCount + hs.FailedCount + hs.WarningCount
		score := 0.0
		if totalChecks > 0 {
			score = float64(hs.PassedCount) / float64(totalChecks) * 100.0
		}

		// 确定状态
		status := "pass"
		if hs.CriticalFails > 0 || hs.HighFails > 0 {
			status = "fail"
		} else if hs.FailedCount > 0 {
			status = "warning"
		}

		hostDetails = append(hostDetails, HostCheckDetail{
			HostID:        hs.HostID,
			Hostname:      host.Hostname,
			IP:            ip,
			OSFamily:      host.OSFamily,
			PassedCount:   hs.PassedCount,
			FailedCount:   hs.FailedCount,
			WarningCount:  hs.WarningCount,
			NACount:       hs.NACount,
			Score:         score,
			Status:        status,
			CriticalFails: hs.CriticalFails,
			HighFails:     hs.HighFails,
		})
	}

	// 6. 获取失败规则汇总
	type failedRuleRow struct {
		RuleID        string
		Title         string
		Severity      string
		Category      string
		FixSuggestion string
		Expected      string
		AffectedCount int
	}
	var failedRuleStats []failedRuleRow

	h.db.Raw(`
		SELECT
			rule_id,
			title,
			severity,
			category,
			fix_suggestion,
			expected,
			COUNT(DISTINCT host_id) as affected_count
		FROM scan_results
		WHERE task_id = ? AND status = 'fail'
		GROUP BY rule_id, title, severity, category, fix_suggestion, expected
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END,
			affected_count DESC
	`, taskID).Scan(&failedRuleStats)

	// 获取每个规则影响的主机列表
	failedRules := make([]FailedRuleSummary, 0, len(failedRuleStats))
	for _, fr := range failedRuleStats {
		var affectedHostIDs []string
		h.db.Model(&model.ScanResult{}).
			Where("task_id = ? AND rule_id = ? AND status = ?", taskID, fr.RuleID, "fail").
			Distinct("host_id").
			Pluck("host_id", &affectedHostIDs)

		// 将 host_id 转换为 hostname
		affectedHosts := make([]string, 0, len(affectedHostIDs))
		for _, hid := range affectedHostIDs {
			if host, ok := hostMap[hid]; ok && host.Hostname != "" {
				affectedHosts = append(affectedHosts, host.Hostname)
			} else {
				affectedHosts = append(affectedHosts, hid)
			}
		}

		failedRules = append(failedRules, FailedRuleSummary{
			RuleID:        fr.RuleID,
			Title:         fr.Title,
			Severity:      fr.Severity,
			Category:      fr.Category,
			AffectedHosts: affectedHosts,
			AffectedCount: fr.AffectedCount,
			FixSuggestion: fr.FixSuggestion,
			Expected:      fr.Expected,
		})
	}

	// 7. 构建执行和完成时间
	var executedAt, completedAt *time.Time
	if task.ExecutedAt != nil {
		t := time.Time(*task.ExecutedAt)
		executedAt = &t
	}
	if task.CompletedAt != nil {
		t := time.Time(*task.CompletedAt)
		completedAt = &t
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"summary": TaskReportSummary{
				TaskID:      task.TaskID,
				TaskName:    task.Name,
				PolicyID:    task.PolicyID,
				PolicyName:  policyName,
				ExecutedAt:  executedAt,
				CompletedAt: completedAt,
				HostCount:   int(hostCount),
				RuleCount:   int(ruleCount),
				Status:      string(task.Status),
			},
			"statistics": TaskReportStatistics{
				TotalChecks:   stats.TotalChecks,
				PassedChecks:  stats.PassedChecks,
				FailedChecks:  stats.FailedChecks,
				WarningChecks: stats.WarningChecks,
				NAChecks:      stats.NAChecks,
				PassRate:      passRate,
				BySeverity:    bySeverity,
				ByCategory:    byCategory,
			},
			"host_details": hostDetails,
			"failed_rules": failedRules,
		},
	})
}

// GetTaskHostDetail 获取主机在任务中的详细检查结果
// GET /api/v1/reports/task/:task_id/host/:host_id
func (h *ReportsHandler) GetTaskHostDetail(c *gin.Context) {
	taskID := c.Param("task_id")
	hostID := c.Param("host_id")

	if taskID == "" || hostID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "task_id 和 host_id 参数不能为空",
		})
		return
	}

	// 获取主机信息
	var host model.Host
	if err := h.db.Where("host_id = ?", hostID).First(&host).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "主机不存在",
		})
		return
	}

	// 获取该主机在该任务中的所有检查结果
	var results []model.ScanResult
	if err := h.db.Where("task_id = ? AND host_id = ?", taskID, hostID).
		Order("CASE severity WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 WHEN 'low' THEN 4 ELSE 5 END, status DESC").
		Find(&results).Error; err != nil {
		h.logger.Error("查询检查结果失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询检查结果失败",
		})
		return
	}

	// 统计
	var passed, failed, warning, na int64
	for _, r := range results {
		switch r.Status {
		case model.ResultStatusPass:
			passed++
		case model.ResultStatusFail:
			failed++
		case model.ResultStatusError:
			warning++
		case model.ResultStatusNA:
			na++
		}
	}

	ip := ""
	if len(host.IPv4) > 0 {
		ip = host.IPv4[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"host": gin.H{
				"host_id":    host.HostID,
				"hostname":   host.Hostname,
				"ip":         ip,
				"os_family":  host.OSFamily,
				"os_version": host.OSVersion,
			},
			"statistics": gin.H{
				"total":   len(results),
				"passed":  passed,
				"failed":  failed,
				"warning": warning,
				"na":      na,
			},
			"results": results,
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

// TopFailedRule Top 失败检查项
type TopFailedRule struct {
	RuleID        string `json:"rule_id"`
	Title         string `json:"title"`
	Severity      string `json:"severity"`
	Category      string `json:"category"`
	AffectedHosts int    `json:"affected_hosts"`
}

// TopRiskHost Top 风险主机
type TopRiskHost struct {
	HostID        string  `json:"host_id"`
	Hostname      string  `json:"hostname"`
	IP            string  `json:"ip"`
	OSFamily      string  `json:"os_family"`
	Score         float64 `json:"score"`
	FailCount     int     `json:"fail_count"`
	CriticalCount int     `json:"critical_count"`
	HighCount     int     `json:"high_count"`
}

// ExecutiveReportMeta 管理层报告元数据
type ExecutiveReportMeta struct {
	ReportID     string `json:"report_id"`     // 报告编号
	ReportTitle  string `json:"report_title"`  // 报告标题
	GeneratedAt  string `json:"generated_at"`  // 生成时间
	CompanyName  string `json:"company_name"`  // 公司名称
	BaselineType string `json:"baseline_type"` // 基线类型
	CheckTarget  string `json:"check_target"`  // 检查对象描述
}

// ExecutiveSummary 执行摘要
type ExecutiveSummary struct {
	OverallConclusion   string  `json:"overall_conclusion"`   // 总体结论
	CheckScope          string  `json:"check_scope"`          // 检查范围描述
	ComplianceRate      float64 `json:"compliance_rate"`      // 合规率
	HasCriticalRisk     bool    `json:"has_critical_risk"`    // 是否存在严重风险
	HasHighRisk         bool    `json:"has_high_risk"`        // 是否存在高危风险
	ConclusionStatement string  `json:"conclusion_statement"` // 结论陈述
	CoverageNote        string  `json:"coverage_note"`        // 覆盖范围说明
}

// SecurityScore 安全评分
type SecurityScore struct {
	Score            float64 `json:"score"`             // 综合安全评分 (0-100)
	Grade            string  `json:"grade"`             // 安全等级 (优秀/良好/一般/较差)
	GradeColor       string  `json:"grade_color"`       // 等级颜色
	ScoreExplanation string  `json:"score_explanation"` // 评分说明
	SecurityNote     string  `json:"security_note"`     // 安全提示
}

// RiskItem 风险项
type RiskItem struct {
	Category       string `json:"category"`       // 风险类别
	Description    string `json:"description"`    // 风险描述（自然语言）
	Impact         string `json:"impact"`         // 可能影响
	Severity       string `json:"severity"`       // 风险等级
	SeverityLabel  string `json:"severity_label"` // 风险等级中文
	Recommendation string `json:"recommendation"` // 整改建议方向
	AffectedCount  int    `json:"affected_count"` // 影响数量
}

// ComplianceCoverage 合规与基线覆盖说明
type ComplianceCoverage struct {
	BaselineSource  string   `json:"baseline_source"`  // 基线来源
	CoveredAreas    []string `json:"covered_areas"`    // 覆盖领域
	UncoveredAreas  []string `json:"uncovered_areas"`  // 未覆盖领域
	ImprovementNote string   `json:"improvement_note"` // 改进建议
}

// CategoryStats 类别统计（用于报告摘要）
type CategoryStats struct {
	Category     string  `json:"category"`      // 类别英文标识
	CategoryName string  `json:"category_name"` // 类别中文名称
	TotalChecks  int64   `json:"total_checks"`  // 总检查项
	PassedChecks int64   `json:"passed_checks"` // 通过项
	FailedChecks int64   `json:"failed_checks"` // 失败项
	PassRate     float64 `json:"pass_rate"`     // 通过率
}

// getCategoryName 获取类别的中文名称
func getCategoryName(category string) string {
	categoryNames := map[string]string{
		"ssh":              "SSH 安全配置",
		"password":         "密码策略",
		"account":          "账户安全",
		"audit":            "日志审计",
		"file_permissions": "文件权限",
		"file_permission":  "文件权限",
		"service":          "服务配置",
		"network":          "网络安全",
		"sysctl":           "内核参数",
		"kernel":           "内核安全",
		"cron":             "计划任务",
		"banner":           "登录横幅",
		"secure_boot":      "安全启动",
		"mac":              "强制访问控制",
		"integrity":        "文件完整性",
		"access_control":   "访问控制",
		"login":            "登录安全",
	}
	if name, ok := categoryNames[category]; ok {
		return name
	}
	return category
}

// getRiskImpact 根据规则获取真实的风险影响描述
func getRiskImpact(ruleID, title, category string) string {
	// 根据规则ID或关键词生成具体的风险影响
	impactMap := map[string]string{
		// SSH 相关
		"LINUX_SSH_001": "允许 root 远程登录将使系统面临暴力破解攻击风险，攻击者一旦成功可直接获取最高系统权限，控制整个服务器",
		"LINUX_SSH_002": "允许空密码登录意味着无需认证即可访问系统，任何网络可达的攻击者都可直接登录服务器",
		"LINUX_SSH_017": "使用弱加密算法（如 3DES、Arcfour）将导致 SSH 通信内容可能被中间人攻击截获并解密",
		"LINUX_SSH_018": "使用弱 MAC 算法（如 MD5）将导致数据完整性保护不足，可能被篡改而不被发现",
		"LINUX_SSH_019": "使用弱密钥交换算法将导致会话密钥可能被离线破解，历史通信内容面临泄露风险",
		"LINUX_SSH_023": "SSH 私钥权限过大将导致其他用户可读取私钥，可能被用于中间人攻击或服务器身份冒充",

		// 审计相关
		"LINUX_AUDIT_001": "审计服务未运行将导致系统无法记录安全事件，安全事件发生时无法追溯攻击行为",
		"LINUX_AUDIT_003": "审计日志权限过大将导致普通用户可修改或删除日志，攻击者可清除入侵痕迹",
		"LINUX_AUDIT_020": "未审计特权命令将导致攻击者使用 sudo 等提权操作时不会留下记录",
		"LINUX_AUDIT_021": "未审计内核模块操作将导致攻击者植入 rootkit 时不会触发告警",

		// 密码策略
		"LINUX_PWD_001": "密码长度不足将大幅降低暴力破解难度，8位以下密码可在数小时内被破解",
		"LINUX_PWD_002": "无密码复杂度要求将导致用户设置弱密码，容易被字典攻击破解",
		"LINUX_PWD_003": "密码过期时间过长将导致密码泄露后长时间有效，增加被滥用风险",

		// 账户安全
		"LINUX_ACCOUNT_001": "存在 UID 为 0 的非 root 账户将导致存在隐藏的超级用户，可绕过审计机制",
		"LINUX_ACCOUNT_002": "未锁定空密码账户将导致无需认证即可以该身份登录系统",

		// 文件权限
		"LINUX_FILE_PERM_001": "/etc/passwd 权限不当可能导致攻击者修改用户信息，添加后门账户",
		"LINUX_FILE_PERM_002": "/etc/shadow 权限不当可能导致密码哈希泄露，被离线破解",
		"LINUX_FILE_PERM_003": "/etc/sudoers 权限不当可能导致攻击者获取 sudo 权限",

		// 内核参数
		"LINUX_SYSCTL_001": "未启用 ASLR 将导致内存地址可预测，降低漏洞利用难度",
		"LINUX_SYSCTL_002": "允许 IP 转发可能导致服务器被用作网络跳板进行横向攻击",
	}

	if impact, ok := impactMap[ruleID]; ok {
		return impact
	}

	// 根据类别生成通用描述
	categoryImpacts := map[string]string{
		"ssh":              "SSH 配置不当将导致远程访问安全性降低，增加被暴力破解或中间人攻击的风险",
		"password":         "密码策略薄弱将导致用户密码易被破解，系统面临账户被盗用风险",
		"account":          "账户配置不当将导致存在安全隐患账户，可能被攻击者利用进行提权",
		"audit":            "审计配置不当将影响安全事件的追溯能力，无法有效进行事后取证分析",
		"file_permissions": "文件权限不当可能导致敏感信息泄露或被恶意篡改",
		"file_permission":  "文件权限不当可能导致敏感信息泄露或被恶意篡改",
		"service":          "服务配置不当可能存在安全漏洞，增加被远程利用的风险",
		"network":          "网络配置不当可能导致服务器被用作攻击跳板或遭受网络层攻击",
		"sysctl":           "内核参数配置不当将降低系统层面的安全防护能力",
		"kernel":           "内核安全配置不当将导致系统底层防护薄弱",
	}

	if impact, ok := categoryImpacts[category]; ok {
		return impact
	}

	return "此配置项不符合安全基线要求，存在潜在安全风险"
}

// getRiskRecommendation 根据规则获取具体的修复建议
func getRiskRecommendation(ruleID, fixSuggestion, category string) string {
	if fixSuggestion != "" {
		return fixSuggestion
	}

	// 根据类别生成通用修复建议
	categoryRecommendations := map[string]string{
		"ssh":              "建议严格按照 CIS Benchmark 要求配置 SSH 服务，禁用不安全选项，启用强加密算法",
		"password":         "建议配置强密码策略，包括最小长度、复杂度要求、有效期和历史密码限制",
		"account":          "建议清理异常账户，锁定空密码账户，确保只有必要的账户存在",
		"audit":            "建议启用完整的审计功能，配置关键系统操作的审计规则，确保日志安全存储",
		"file_permissions": "建议严格控制关键系统文件的权限，确保敏感文件仅允许特定用户访问",
		"file_permission":  "建议严格控制关键系统文件的权限，确保敏感文件仅允许特定用户访问",
		"service":          "建议禁用不必要的服务，为必要服务配置安全选项",
		"network":          "建议关闭不必要的网络转发功能，配置防火墙限制网络访问",
		"sysctl":           "建议按照安全加固要求配置内核参数，启用 ASLR、禁用危险功能",
	}

	if rec, ok := categoryRecommendations[category]; ok {
		return rec
	}

	return "建议参照安全基线要求进行整改配置"
}

// ManagementRecommendation 管理建议
type ManagementRecommendation struct {
	OverallAssessment string   `json:"overall_assessment"` // 总体评估
	ActionSuggestions []string `json:"action_suggestions"` // 行动建议
	Disclaimer        string   `json:"disclaimer"`         // 声明
}

// ExecutiveTaskReport 管理层任务报告（完整版）
type ExecutiveTaskReport struct {
	Meta           ExecutiveReportMeta      `json:"meta"`
	Summary        ExecutiveSummary         `json:"summary"`
	TaskInfo       TaskReportSummary        `json:"task_info"`
	Statistics     TaskReportStatistics     `json:"statistics"`
	CategoryStats  []CategoryStats          `json:"category_stats"` // 按类别统计（含通过率）
	SecurityScore  SecurityScore            `json:"security_score"`
	HostDetails    []HostCheckDetail        `json:"host_details"`
	RiskItems      []RiskItem               `json:"risk_items"`
	FailedRules    []FailedRuleSummary      `json:"failed_rules"`
	Coverage       ComplianceCoverage       `json:"coverage"`
	Recommendation ManagementRecommendation `json:"recommendation"`
}

// GetTopFailedRules 获取 Top N 失败检查项
// GET /api/v1/reports/top-failed-rules
func (h *ReportsHandler) GetTopFailedRules(c *gin.Context) {
	// 获取 limit 参数，默认 10
	limit := 10
	if l := c.Query("limit"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &limit); err == nil && parsed > 0 {
			if limit > 100 {
				limit = 100
			}
		}
	}

	// 获取最近检查结果中失败次数最多的规则
	type ruleStatRow struct {
		RuleID        string
		Title         string
		Severity      string
		Category      string
		AffectedHosts int
	}
	var ruleStats []ruleStatRow

	h.db.Raw(`
		SELECT
			rule_id,
			title,
			severity,
			category,
			COUNT(DISTINCT host_id) as affected_hosts
		FROM scan_results
		WHERE status = 'fail'
		GROUP BY rule_id, title, severity, category
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END,
			affected_hosts DESC
		LIMIT ?
	`, limit).Scan(&ruleStats)

	topRules := make([]TopFailedRule, 0, len(ruleStats))
	for _, rs := range ruleStats {
		topRules = append(topRules, TopFailedRule{
			RuleID:        rs.RuleID,
			Title:         rs.Title,
			Severity:      rs.Severity,
			Category:      rs.Category,
			AffectedHosts: rs.AffectedHosts,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": topRules,
	})
}

// GetTopRiskHosts 获取 Top N 风险主机
// GET /api/v1/reports/top-risk-hosts
func (h *ReportsHandler) GetTopRiskHosts(c *gin.Context) {
	// 获取 limit 参数，默认 10
	limit := 10
	if l := c.Query("limit"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &limit); err == nil && parsed > 0 {
			if limit > 100 {
				limit = 100
			}
		}
	}

	// 获取失败检查项最多的主机
	type hostStatRow struct {
		HostID        string
		FailCount     int
		CriticalCount int
		HighCount     int
		TotalChecks   int
		PassedChecks  int
	}
	var hostStats []hostStatRow

	h.db.Raw(`
		SELECT
			host_id,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as fail_count,
			SUM(CASE WHEN status = 'fail' AND severity = 'critical' THEN 1 ELSE 0 END) as critical_count,
			SUM(CASE WHEN status = 'fail' AND severity = 'high' THEN 1 ELSE 0 END) as high_count,
			COUNT(*) as total_checks,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed_checks
		FROM scan_results
		GROUP BY host_id
		HAVING fail_count > 0
		ORDER BY critical_count DESC, high_count DESC, fail_count DESC
		LIMIT ?
	`, limit).Scan(&hostStats)

	// 获取主机信息
	hostIDs := make([]string, 0, len(hostStats))
	for _, hs := range hostStats {
		hostIDs = append(hostIDs, hs.HostID)
	}

	var hosts []model.Host
	if len(hostIDs) > 0 {
		h.db.Where("host_id IN ?", hostIDs).Find(&hosts)
	}

	hostMap := make(map[string]model.Host)
	for _, host := range hosts {
		hostMap[host.HostID] = host
	}

	topHosts := make([]TopRiskHost, 0, len(hostStats))
	for _, hs := range hostStats {
		host := hostMap[hs.HostID]
		ip := ""
		if len(host.IPv4) > 0 {
			ip = host.IPv4[0]
		}

		// 计算得分
		score := 0.0
		if hs.TotalChecks > 0 {
			score = float64(hs.PassedChecks) / float64(hs.TotalChecks) * 100.0
		}

		topHosts = append(topHosts, TopRiskHost{
			HostID:        hs.HostID,
			Hostname:      host.Hostname,
			IP:            ip,
			OSFamily:      host.OSFamily,
			Score:         score,
			FailCount:     hs.FailCount,
			CriticalCount: hs.CriticalCount,
			HighCount:     hs.HighCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": topHosts,
	})
}

// GetExecutiveTaskReport 获取管理层任务报告（面向非技术管理者的专业报告）
// GET /api/v1/reports/task/:task_id/executive
func (h *ReportsHandler) GetExecutiveTaskReport(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "task_id 参数不能为空",
		})
		return
	}

	// 1. 获取任务信息
	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		h.logger.Error("查询任务失败", zap.String("task_id", taskID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	// 2. 获取策略信息（支持多策略）
	policyIDs := task.GetPolicyIDs()
	var policyNames []string
	baselineType := "系统基线检查"

	for _, policyID := range policyIDs {
		var policy model.Policy
		if err := h.db.Where("id = ?", policyID).First(&policy).Error; err == nil {
			policyNames = append(policyNames, policy.Name)
		}
	}

	policyName := ""
	if len(policyNames) == 1 {
		policyName = policyNames[0]
		baselineType = policyNames[0]
	} else if len(policyNames) > 1 {
		policyName = fmt.Sprintf("%d 个策略", len(policyNames))
		baselineType = fmt.Sprintf("多策略基线检查（%d个）", len(policyNames))
	}

	// 3. 获取检查结果统计
	var stats struct {
		TotalChecks   int64
		PassedChecks  int64
		FailedChecks  int64
		WarningChecks int64
		NAChecks      int64
	}

	h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID).Count(&stats.TotalChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "pass").Count(&stats.PassedChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "fail").Count(&stats.FailedChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "error").Count(&stats.WarningChecks)
	h.db.Model(&model.ScanResult{}).Where("task_id = ? AND status = ?", taskID, "na").Count(&stats.NAChecks)

	// 计算通过率
	passRate := 0.0
	if stats.TotalChecks > 0 {
		passRate = float64(stats.PassedChecks) / float64(stats.TotalChecks) * 100.0
	}

	// 按严重级别统计（失败项）
	var severityStats []struct {
		Severity string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("severity, COUNT(*) as count").
		Where("task_id = ? AND status = ?", taskID, "fail").
		Group("severity").
		Find(&severityStats)

	bySeverity := map[string]int64{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, s := range severityStats {
		if s.Severity != "" {
			bySeverity[s.Severity] = s.Count
		}
	}

	// 按类别统计（失败项）
	var categoryStats []struct {
		Category string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("category, COUNT(*) as count").
		Where("task_id = ? AND status = ?", taskID, "fail").
		Group("category").
		Find(&categoryStats)

	byCategory := make(map[string]int64)
	coveredAreas := make([]string, 0)
	for _, s := range categoryStats {
		if s.Category != "" {
			byCategory[s.Category] = s.Count
			coveredAreas = append(coveredAreas, s.Category)
		}
	}

	// 获取所有检查的类别（不仅仅是失败的）
	var allCategories []string
	h.db.Model(&model.ScanResult{}).
		Where("task_id = ?", taskID).
		Distinct("category").
		Pluck("category", &allCategories)
	if len(allCategories) > 0 {
		coveredAreas = allCategories
	}

	// 按类别详细统计（包含通过率）
	type categoryStatRow struct {
		Category     string
		TotalChecks  int64
		PassedChecks int64
		FailedChecks int64
	}
	var categoryDetailStats []categoryStatRow
	h.db.Raw(`
		SELECT
			category,
			COUNT(*) as total_checks,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed_checks,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as failed_checks
		FROM scan_results
		WHERE task_id = ?
		GROUP BY category
		ORDER BY failed_checks DESC
	`, taskID).Scan(&categoryDetailStats)

	// 构建类别统计列表
	categoryStatsResult := make([]CategoryStats, 0, len(categoryDetailStats))
	for _, cs := range categoryDetailStats {
		if cs.Category == "" {
			continue
		}
		passRate := 0.0
		if cs.TotalChecks > 0 {
			passRate = float64(cs.PassedChecks) / float64(cs.TotalChecks) * 100.0
		}
		categoryStatsResult = append(categoryStatsResult, CategoryStats{
			Category:     cs.Category,
			CategoryName: getCategoryName(cs.Category),
			TotalChecks:  cs.TotalChecks,
			PassedChecks: cs.PassedChecks,
			FailedChecks: cs.FailedChecks,
			PassRate:     passRate,
		})
	}

	// 4. 获取涉及的主机数和规则数
	var hostCount int64
	h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID).Distinct("host_id").Count(&hostCount)

	var ruleCount int64
	h.db.Model(&model.ScanResult{}).Where("task_id = ?", taskID).Distinct("rule_id").Count(&ruleCount)

	// 5. 获取主机操作系统类型
	var osTypes []string
	h.db.Raw(`
		SELECT DISTINCT h.os_family
		FROM hosts h
		INNER JOIN scan_results sr ON h.host_id = sr.host_id
		WHERE sr.task_id = ?
	`, taskID).Pluck("os_family", &osTypes)

	osTypeStr := "Linux"
	if len(osTypes) > 0 {
		osTypeStr = ""
		for i, os := range osTypes {
			if i > 0 {
				osTypeStr += "、"
			}
			osTypeStr += os
		}
	}

	// 6. 获取主机检查明细
	type hostStatRow struct {
		HostID        string
		PassedCount   int64
		FailedCount   int64
		WarningCount  int64
		NACount       int64
		CriticalFails int64
		HighFails     int64
	}
	var hostStats []hostStatRow

	h.db.Raw(`
		SELECT
			host_id,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as passed_count,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as failed_count,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as warning_count,
			SUM(CASE WHEN status = 'na' THEN 1 ELSE 0 END) as na_count,
			SUM(CASE WHEN status = 'fail' AND severity = 'critical' THEN 1 ELSE 0 END) as critical_fails,
			SUM(CASE WHEN status = 'fail' AND severity = 'high' THEN 1 ELSE 0 END) as high_fails
		FROM scan_results
		WHERE task_id = ?
		GROUP BY host_id
	`, taskID).Scan(&hostStats)

	// 获取主机信息
	hostIDs := make([]string, 0, len(hostStats))
	for _, hs := range hostStats {
		hostIDs = append(hostIDs, hs.HostID)
	}

	var hosts []model.Host
	if len(hostIDs) > 0 {
		h.db.Where("host_id IN ?", hostIDs).Find(&hosts)
	}

	hostMap := make(map[string]model.Host)
	for _, host := range hosts {
		hostMap[host.HostID] = host
	}

	// 构建主机明细列表
	hostDetails := make([]HostCheckDetail, 0, len(hostStats))
	for _, hs := range hostStats {
		host := hostMap[hs.HostID]
		ip := ""
		if len(host.IPv4) > 0 {
			ip = host.IPv4[0]
		}

		totalChecks := hs.PassedCount + hs.FailedCount + hs.WarningCount
		score := 0.0
		if totalChecks > 0 {
			score = float64(hs.PassedCount) / float64(totalChecks) * 100.0
		}

		status := "pass"
		if hs.CriticalFails > 0 || hs.HighFails > 0 {
			status = "fail"
		} else if hs.FailedCount > 0 {
			status = "warning"
		}

		hostDetails = append(hostDetails, HostCheckDetail{
			HostID:        hs.HostID,
			Hostname:      host.Hostname,
			IP:            ip,
			OSFamily:      host.OSFamily,
			PassedCount:   hs.PassedCount,
			FailedCount:   hs.FailedCount,
			WarningCount:  hs.WarningCount,
			NACount:       hs.NACount,
			Score:         score,
			Status:        status,
			CriticalFails: hs.CriticalFails,
			HighFails:     hs.HighFails,
		})
	}

	// 7. 获取失败规则汇总
	type failedRuleRow struct {
		RuleID        string
		Title         string
		Severity      string
		Category      string
		FixSuggestion string
		Expected      string
		AffectedCount int
	}
	var failedRuleStats []failedRuleRow

	h.db.Raw(`
		SELECT
			rule_id,
			title,
			severity,
			category,
			fix_suggestion,
			expected,
			COUNT(DISTINCT host_id) as affected_count
		FROM scan_results
		WHERE task_id = ? AND status = 'fail'
		GROUP BY rule_id, title, severity, category, fix_suggestion, expected
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END,
			affected_count DESC
	`, taskID).Scan(&failedRuleStats)

	// 构建失败规则列表和风险项
	failedRules := make([]FailedRuleSummary, 0, len(failedRuleStats))
	riskItems := make([]RiskItem, 0)

	severityLabels := map[string]string{
		"critical": "严重",
		"high":     "高危",
		"medium":   "中危",
		"low":      "低危",
	}

	for _, fr := range failedRuleStats {
		var affectedHostIDs []string
		h.db.Model(&model.ScanResult{}).
			Where("task_id = ? AND rule_id = ? AND status = ?", taskID, fr.RuleID, "fail").
			Distinct("host_id").
			Pluck("host_id", &affectedHostIDs)

		affectedHosts := make([]string, 0, len(affectedHostIDs))
		for _, hid := range affectedHostIDs {
			if host, ok := hostMap[hid]; ok && host.Hostname != "" {
				affectedHosts = append(affectedHosts, host.Hostname)
			} else {
				affectedHosts = append(affectedHosts, hid)
			}
		}

		failedRules = append(failedRules, FailedRuleSummary{
			RuleID:        fr.RuleID,
			Title:         fr.Title,
			Severity:      fr.Severity,
			Category:      fr.Category,
			AffectedHosts: affectedHosts,
			AffectedCount: fr.AffectedCount,
			FixSuggestion: fr.FixSuggestion,
			Expected:      fr.Expected,
		})

		// 为严重和高危风险生成风险项
		if fr.Severity == "critical" || fr.Severity == "high" {
			// 使用规则特定的影响描述和修复建议
			impact := getRiskImpact(fr.RuleID, fr.Title, fr.Category)
			recommendation := getRiskRecommendation(fr.RuleID, fr.FixSuggestion, fr.Category)

			riskItems = append(riskItems, RiskItem{
				Category:       getCategoryName(fr.Category), // 使用中文类别名称
				Description:    fr.Title,
				Impact:         impact,
				Severity:       fr.Severity,
				SeverityLabel:  severityLabels[fr.Severity],
				Recommendation: recommendation,
				AffectedCount:  fr.AffectedCount,
			})
		}
	}

	// 8. 计算安全评分（加权计算）
	severityWeights := map[string]float64{
		"critical": 10.0,
		"high":     7.0,
		"medium":   4.0,
		"low":      1.0,
	}

	totalWeight := 0.0
	lostWeight := 0.0

	var allResults []struct {
		Status   string
		Severity string
	}
	h.db.Model(&model.ScanResult{}).
		Select("status, severity").
		Where("task_id = ?", taskID).
		Find(&allResults)

	for _, r := range allResults {
		weight := severityWeights[r.Severity]
		if weight == 0 {
			weight = 1.0
		}
		totalWeight += weight
		if r.Status == "fail" {
			lostWeight += weight
		}
	}

	securityScoreValue := 100.0
	if totalWeight > 0 {
		securityScoreValue = ((totalWeight - lostWeight) / totalWeight) * 100.0
	}

	// 确定安全等级
	var grade, gradeColor string
	if securityScoreValue >= 90 {
		grade = "优秀"
		gradeColor = "#52c41a"
	} else if securityScoreValue >= 80 {
		grade = "良好"
		gradeColor = "#73d13d"
	} else if securityScoreValue >= 60 {
		grade = "一般"
		gradeColor = "#faad14"
	} else {
		grade = "较差"
		gradeColor = "#ff4d4f"
	}

	scoreExplanation := fmt.Sprintf("综合安全评分基于 %d 条检查规则的加权计算得出，其中严重级别权重最高，低危级别权重最低。", ruleCount)
	securityNote := "安全评分仅反映当前已检查项的合规情况，不代表系统的绝对安全状态。建议持续完善安全基线覆盖范围。"

	// 9. 生成执行摘要
	hasCritical := bySeverity["critical"] > 0
	hasHigh := bySeverity["high"] > 0

	var overallConclusion string
	if hasCritical {
		overallConclusion = "存在严重风险，需立即整改"
	} else if hasHigh {
		overallConclusion = "存在高危风险，建议尽快整改"
	} else if stats.FailedChecks > 0 {
		overallConclusion = "存在一般风险，建议逐步优化"
	} else {
		overallConclusion = "整体合规，安全状态良好"
	}

	checkScope := fmt.Sprintf("本次检查覆盖 %d 台主机、%d 条安全规则，操作系统类型包括 %s。",
		hostCount, ruleCount, osTypeStr)

	var conclusionStatement string
	if stats.FailedChecks == 0 {
		conclusionStatement = "本次基线检查未发现不合规项，当前检查范围内的安全配置符合基线要求。"
	} else if hasCritical || hasHigh {
		conclusionStatement = fmt.Sprintf("本次基线检查发现 %d 项不合规配置，其中包含 %d 项严重风险和 %d 项高危风险，建议优先处理高危及以上级别的问题。",
			stats.FailedChecks, bySeverity["critical"], bySeverity["high"])
	} else {
		conclusionStatement = fmt.Sprintf("本次基线检查发现 %d 项不合规配置，均为中低风险问题，建议按计划逐步整改。",
			stats.FailedChecks)
	}

	coverageNote := "当前检查项覆盖范围有限，仅包含已配置的基线规则。建议逐步扩大基线覆盖面，增加更多安全检查维度。"
	if len(coveredAreas) > 5 {
		coverageNote = "本次检查覆盖了多个安全配置维度，建议持续完善和更新基线规则库。"
	}

	// 10. 生成管理建议
	var overallAssessment string
	if securityScoreValue >= 80 && !hasCritical && !hasHigh {
		overallAssessment = "当前安全基线状态总体良好，建议维持现有安全配置并持续监控。"
	} else if hasCritical || hasHigh {
		overallAssessment = fmt.Sprintf("当前存在 %d 项严重风险和 %d 项高危风险需要关注，建议安排专人进行整改，并在整改完成后重新执行检查验证。",
			bySeverity["critical"], bySeverity["high"])
	} else {
		overallAssessment = "当前安全配置存在改进空间，建议制定整改计划逐步优化。"
	}

	actionSuggestions := make([]string, 0)

	// 根据检查结果生成具体的建议
	if hasCritical {
		actionSuggestions = append(actionSuggestions, fmt.Sprintf("【紧急】立即处理 %d 项严重风险，这些问题可能导致系统被完全控制", bySeverity["critical"]))
	}
	if hasHigh {
		actionSuggestions = append(actionSuggestions, fmt.Sprintf("【重要】优先整改 %d 项高危风险，降低系统被入侵的可能性", bySeverity["high"]))
	}

	// 根据类别统计生成具体建议
	for _, cs := range categoryStatsResult {
		if cs.FailedChecks > 0 && cs.PassRate < 60 {
			switch cs.Category {
			case "ssh":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【SSH安全】SSH 配置通过率仅 %.0f%%，建议加固 SSH 服务配置，禁用 root 远程登录、空密码登录，启用强加密算法", cs.PassRate))
			case "audit":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【日志审计】日志审计配置通过率仅 %.0f%%，建议启用 auditd 服务，配置关键操作审计规则", cs.PassRate))
			case "password":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【密码策略】密码策略通过率仅 %.0f%%，建议配置密码复杂度、有效期和锁定策略", cs.PassRate))
			case "account":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【账户安全】账户安全配置通过率仅 %.0f%%，建议清理异常账户、锁定空密码账户", cs.PassRate))
			case "file_permissions", "file_permission":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【文件权限】文件权限配置通过率仅 %.0f%%，建议严格控制关键系统文件权限", cs.PassRate))
			case "sysctl", "kernel":
				actionSuggestions = append(actionSuggestions, fmt.Sprintf("【内核参数】内核安全配置通过率仅 %.0f%%，建议启用 ASLR、关闭不必要的网络功能", cs.PassRate))
			}
		}
	}

	if stats.FailedChecks > 0 {
		actionSuggestions = append(actionSuggestions, "制定整改计划，分批次完成所有不合规项的修复")
		actionSuggestions = append(actionSuggestions, "整改完成后重新执行基线检查，验证修复效果")
	}
	actionSuggestions = append(actionSuggestions, "建议定期执行安全基线检查（建议每周一次），持续监控系统安全状态")

	// 11. 生成未覆盖领域
	// 将覆盖领域转换为中文名称
	coveredAreasCN := make([]string, 0, len(coveredAreas))
	coveredMap := make(map[string]bool)
	for _, area := range coveredAreas {
		cnName := getCategoryName(area)
		coveredAreasCN = append(coveredAreasCN, cnName)
		coveredMap[cnName] = true
	}

	uncoveredAreas := []string{}
	allPossibleAreas := []string{"网络安全", "日志审计", "入侵检测", "数据加密", "备份恢复", "访问控制", "漏洞管理", "SSH 安全配置", "密码策略", "账户安全", "文件权限", "内核参数"}
	for _, area := range allPossibleAreas {
		if !coveredMap[area] {
			uncoveredAreas = append(uncoveredAreas, area)
		}
	}

	// 检查是否有未覆盖的重要领域
	if len(uncoveredAreas) > 3 {
		actionSuggestions = append(actionSuggestions, fmt.Sprintf("当前检查未覆盖 %d 个安全领域，建议扩展基线规则库", len(uncoveredAreas)))
	}

	// 12. 获取系统配置中的公司名称
	companyName := "矩阵云安全平台"
	var siteConfig model.SystemConfig
	if err := h.db.Where("`key` = ?", "site_config").First(&siteConfig).Error; err == nil {
		// 尝试解析站点配置获取公司名称
		if siteConfig.Value != "" {
			// 简单处理，实际可以 JSON 解析
			companyName = "矩阵云安全平台"
		}
	}

	// 13. 构建执行和完成时间
	var executedAt, completedAt *time.Time
	if task.ExecutedAt != nil {
		t := time.Time(*task.ExecutedAt)
		executedAt = &t
	}
	if task.CompletedAt != nil {
		t := time.Time(*task.CompletedAt)
		completedAt = &t
	}

	// 14. 生成报告编号
	reportID := fmt.Sprintf("SR-%s-%s", time.Now().Format("20060102"), taskID[:8])

	// 构建最终响应
	report := ExecutiveTaskReport{
		Meta: ExecutiveReportMeta{
			ReportID:     reportID,
			ReportTitle:  "服务器安全基线检查报告",
			GeneratedAt:  time.Now().Format("2006-01-02 15:04:05"),
			CompanyName:  companyName,
			BaselineType: baselineType,
			CheckTarget:  fmt.Sprintf("%d 台服务器（%s 系统）", hostCount, osTypeStr),
		},
		Summary: ExecutiveSummary{
			OverallConclusion:   overallConclusion,
			CheckScope:          checkScope,
			ComplianceRate:      passRate,
			HasCriticalRisk:     hasCritical,
			HasHighRisk:         hasHigh,
			ConclusionStatement: conclusionStatement,
			CoverageNote:        coverageNote,
		},
		TaskInfo: TaskReportSummary{
			TaskID:      task.TaskID,
			TaskName:    task.Name,
			PolicyID:    task.PolicyID,
			PolicyIDs:   policyIDs,
			PolicyName:  policyName,
			PolicyNames: policyNames,
			ExecutedAt:  executedAt,
			CompletedAt: completedAt,
			HostCount:   int(hostCount),
			RuleCount:   int(ruleCount),
			Status:      string(task.Status),
		},
		Statistics: TaskReportStatistics{
			TotalChecks:   stats.TotalChecks,
			PassedChecks:  stats.PassedChecks,
			FailedChecks:  stats.FailedChecks,
			WarningChecks: stats.WarningChecks,
			NAChecks:      stats.NAChecks,
			PassRate:      passRate,
			BySeverity:    bySeverity,
			ByCategory:    byCategory,
		},
		CategoryStats: categoryStatsResult,
		SecurityScore: SecurityScore{
			Score:            securityScoreValue,
			Grade:            grade,
			GradeColor:       gradeColor,
			ScoreExplanation: scoreExplanation,
			SecurityNote:     securityNote,
		},
		HostDetails: hostDetails,
		RiskItems:   riskItems,
		FailedRules: failedRules,
		Coverage: ComplianceCoverage{
			BaselineSource:  "内部安全基线（参考 CIS Benchmark 和等保 2.0）",
			CoveredAreas:    coveredAreasCN,
			UncoveredAreas:  uncoveredAreas,
			ImprovementNote: "建议逐步完善基线规则库，扩大检查覆盖范围，确保系统安全配置的全面性。",
		},
		Recommendation: ManagementRecommendation{
			OverallAssessment: overallAssessment,
			ActionSuggestions: actionSuggestions,
			Disclaimer:        "本报告仅覆盖已配置的检查项，检查结果反映报告生成时刻的系统状态。安全是持续的过程，建议定期进行安全评估。",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": report,
	})
}
