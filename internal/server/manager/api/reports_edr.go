// Package api 提供 HTTP API 处理器
//
// reports_edr.go 实现 EDR 模块的报告聚合 + 高管摘要 endpoint。
// 与 reports.go 中其他模块同样模式，独立文件避免污染 monolithic reports.go。
//
// 数据源：
//   - MySQL alerts (source=detection/agent, category 维度告警)
//   - MySQL storylines + storyline_events (攻击故事线)
//   - 后续可注入 ClickHouse 查询 ebpf_events 原始事件量
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// mitreTacticByCategory 把 alerts.category 映射到 MITRE ATT&CK Tactic。
// 与 configs/agent-rules/*.yaml 中 mitre.tactic 字段对齐。
var mitreTacticByCategory = map[string]string{
	"reverse_shell":        "initial_access",
	"execution":            "execution",
	"persistence":          "persistence",
	"privilege_escalation": "privilege_escalation",
	"defense_evasion":      "defense_evasion",
	"credential_access":    "credential_access",
	"discovery":            "discovery",
	"lateral_movement":     "lateral_movement",
	"collection":           "collection",
	"exfiltration":         "exfiltration",
	"c2_communication":     "command_and_control",
	"cryptomining":         "impact",
	"impact":               "impact",
	"port_scan":            "discovery",
	"behavior_anomaly":     "discovery",
}

// GetEDRReport 生成 EDR 模块聚合报告
// GET /api/v1/reports/edr?start_time=&end_time=
//
// 报告含 13 个章节，覆盖告警概览、严重程度分布、规则/主机 Top N、
// MITRE 矩阵、故事线统计、误报抑制统计、周期趋势对比等。
func (h *ReportsHandler) GetEDRReport(c *gin.Context) {
	startTime, endTime, ok := parseReportTimeRange(c)
	if !ok {
		return
	}

	// === 1. 总览 ===
	type summary struct {
		TotalAlerts     int64
		ActiveAlerts    int64
		ResolvedAlerts  int64
		IgnoredAlerts   int64
		AffectedHosts   int64
		TotalStories    int64
		HighRiskStories int64
	}
	var s summary
	timeRange := h.db.Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Where("source IN ?", []string{"detection", "agent"})
	timeRange.Model(&model.Alert{}).Count(&s.TotalAlerts)
	timeRange.Session(&gorm.Session{}).Where("status = ?", model.AlertStatusActive).Model(&model.Alert{}).Count(&s.ActiveAlerts)
	timeRange.Session(&gorm.Session{}).Where("status = ?", model.AlertStatusResolved).Model(&model.Alert{}).Count(&s.ResolvedAlerts)
	timeRange.Session(&gorm.Session{}).Where("status = ?", model.AlertStatusIgnored).Model(&model.Alert{}).Count(&s.IgnoredAlerts)
	timeRange.Session(&gorm.Session{}).Model(&model.Alert{}).Distinct("host_id").Count(&s.AffectedHosts)

	h.db.Model(&model.Storyline{}).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Count(&s.TotalStories)
	h.db.Model(&model.Storyline{}).
		Where("created_at >= ? AND created_at <= ? AND severity IN ?", startTime, endTime, []string{"critical", "high"}).
		Count(&s.HighRiskStories)

	// === 2. 严重程度分布 ===
	severityDistribution := map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}
	var severityRows []struct {
		Severity string
		Count    int64
	}
	h.db.Model(&model.Alert{}).
		Select("severity, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Where("source IN ?", []string{"detection", "agent"}).
		Group("severity").
		Scan(&severityRows)
	for _, r := range severityRows {
		if r.Severity != "" {
			severityDistribution[r.Severity] = r.Count
		}
	}

	// === 3. Category 分布 ===
	type categoryRow struct {
		Category string
		Count    int64
	}
	var categoryRows []categoryRow
	h.db.Model(&model.Alert{}).
		Select("category, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ? AND category <> ''", startTime, endTime).
		Where("source IN ?", []string{"detection", "agent"}).
		Group("category").
		Order("count DESC").
		Scan(&categoryRows)
	categoryDistribution := make([]gin.H, 0, len(categoryRows))
	for _, r := range categoryRows {
		categoryDistribution = append(categoryDistribution, gin.H{
			"category": r.Category,
			"count":    r.Count,
		})
	}

	// === 4. MITRE ATT&CK Tactic 分布（按 category 映射聚合） ===
	tacticDistribution := map[string]int64{}
	for _, r := range categoryRows {
		t, ok := mitreTacticByCategory[r.Category]
		if !ok {
			t = "other"
		}
		tacticDistribution[t] += r.Count
	}

	// === 5. Top 10 触发规则 ===
	type ruleRow struct {
		Title    string
		Category string
		Severity string
		Count    int64
	}
	var ruleRows []ruleRow
	h.db.Model(&model.Alert{}).
		Select("title, category, severity, SUM(hit_count) as count").
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Where("source IN ?", []string{"detection", "agent"}).
		Where("status = ?", model.AlertStatusActive).
		Group("title, category, severity").
		Order("count DESC").
		Limit(10).
		Scan(&ruleRows)
	topRules := make([]gin.H, 0, len(ruleRows))
	for _, r := range ruleRows {
		topRules = append(topRules, gin.H{
			"title":    r.Title,
			"category": r.Category,
			"severity": r.Severity,
			"count":    r.Count,
		})
	}

	// === 6. Top 10 受影响主机 ===
	type hostRow struct {
		HostID   string
		Hostname string
		Count    int64
	}
	var hostRows []hostRow
	h.db.Raw(`
		SELECT a.host_id, h.hostname, COUNT(*) as count
		FROM alerts a LEFT JOIN hosts h ON a.host_id = h.host_id
		WHERE a.created_at >= ? AND a.created_at <= ?
		  AND a.source IN ('detection','agent')
		  AND a.status = 'active'
		GROUP BY a.host_id, h.hostname
		ORDER BY count DESC LIMIT 10
	`, startTime, endTime).Scan(&hostRows)
	topHosts := make([]gin.H, 0, len(hostRows))
	for _, r := range hostRows {
		topHosts = append(topHosts, gin.H{
			"host_id":  r.HostID,
			"hostname": r.Hostname,
			"count":    r.Count,
		})
	}

	// === 7. Top 5 高风险故事线 ===
	type storyRow struct {
		StoryID    string
		HostID     string
		Hostname   string
		Phase      string
		Severity   string
		EventCount int
		AlertCount int
		RiskScore  int
	}
	var storyRows []storyRow
	h.db.Raw(`
		SELECT s.story_id, s.host_id, h.hostname, s.phase, s.severity,
		       s.event_count, s.alert_count, s.risk_score
		FROM storylines s LEFT JOIN hosts h ON s.host_id = h.host_id
		WHERE s.created_at >= ? AND s.created_at <= ?
		ORDER BY s.risk_score DESC LIMIT 5
	`, startTime, endTime).Scan(&storyRows)
	topStories := make([]gin.H, 0, len(storyRows))
	for _, r := range storyRows {
		topStories = append(topStories, gin.H{
			"story_id":    r.StoryID,
			"host_id":     r.HostID,
			"hostname":    r.Hostname,
			"phase":       r.Phase,
			"severity":    r.Severity,
			"event_count": r.EventCount,
			"alert_count": r.AlertCount,
			"risk_score":  r.RiskScore,
		})
	}

	// === 8. 误报抑制统计（基于 resolve_reason 字段）===
	type suppressRow struct {
		Reason string
		Count  int64
	}
	var suppressRows []suppressRow
	h.db.Model(&model.Alert{}).
		Select("resolve_reason as reason, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Where("status = ?", model.AlertStatusIgnored).
		Where("resolve_reason <> ''").
		Group("resolve_reason").
		Order("count DESC").
		Limit(10).
		Scan(&suppressRows)
	suppressionStats := make([]gin.H, 0, len(suppressRows))
	for _, r := range suppressRows {
		suppressionStats = append(suppressionStats, gin.H{
			"reason": r.Reason,
			"count":  r.Count,
		})
	}

	// === 9. 周期趋势对比（与上一同长度周期对比）===
	period := endTime.Sub(startTime)
	prevStart := startTime.Add(-period)
	prevEnd := startTime
	var prevTotal int64
	h.db.Model(&model.Alert{}).
		Where("created_at >= ? AND created_at <= ?", prevStart, prevEnd).
		Where("source IN ?", []string{"detection", "agent"}).
		Count(&prevTotal)
	growthPct := 0.0
	if prevTotal > 0 {
		growthPct = float64(s.TotalAlerts-prevTotal) / float64(prevTotal) * 100
	}

	// === 10. Agent / 规则元数据 ===
	var totalRules, enabledRules int64
	h.db.Model(&model.DetectionRule{}).Count(&totalRules)
	h.db.Model(&model.DetectionRule{}).Where("enabled = ?", true).Count(&enabledRules)
	var onlineHosts int64
	h.db.Model(&model.Host{}).Where("status = ?", "online").Count(&onlineHosts)

	// === 11. ClickHouse ebpf_events 原始事件统计 ===
	// 报告周期内 agent 上报的全部 EDR 事件（含未命中规则的）
	chStats := h.queryEDREventStatsCH(startTime, endTime)

	// === 组装 ===
	reportID := fmt.Sprintf("edr-%d", time.Now().Unix())
	periodStr := fmt.Sprintf("%s ~ %s",
		startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))

	reportData := gin.H{
		"meta": gin.H{
			"reportID":     reportID,
			"period":       periodStr,
			"generatedAt":  time.Now(),
			"onlineHosts":  onlineHosts,
			"totalRules":   totalRules,
			"enabledRules": enabledRules,
		},
		"summary": gin.H{
			"totalAlerts":     s.TotalAlerts,
			"activeAlerts":    s.ActiveAlerts,
			"resolvedAlerts":  s.ResolvedAlerts,
			"ignoredAlerts":   s.IgnoredAlerts,
			"affectedHosts":   s.AffectedHosts,
			"totalStories":    s.TotalStories,
			"highRiskStories": s.HighRiskStories,
		},
		"severityDistribution": severityDistribution,
		"categoryDistribution": categoryDistribution,
		"tacticDistribution":   tacticDistribution,
		"topRules":             topRules,
		"topHosts":             topHosts,
		"topStories":           topStories,
		"suppressionStats":     suppressionStats,
		"trend": gin.H{
			"prevPeriodAlerts": prevTotal,
			"growthPercent":    growthPct,
			"direction":        directionLabel(growthPct),
		},
		"rawEventStats": chStats,
	}

	h.saveGeneratedReport(model.ReportTypeEDR, "EDR 检测专项报告", reportID, periodStr, reportData)
	Success(c, reportData)
}

// edrEventStats 是 CH 端 ebpf_events 维度聚合结果。
type edrEventStats struct {
	TotalEvents     uint64  `json:"totalEvents"`
	UniqueHosts     uint64  `json:"uniqueHosts"`
	EventsByType    []gin.H `json:"eventsByType"`    // [{event_type, count}]
	EventsByHour    []gin.H `json:"eventsByHour"`    // [{hour, count}] 时序
	TopHostsByEvent []gin.H `json:"topHostsByEvent"` // [{host_id, count}]
	TopExe          []gin.H `json:"topExe"`          // [{exe, count}]
	Available       bool    `json:"available"`       // CH 不可用时返回 false
}

// queryEDREventStatsCH 从 ClickHouse 聚合报告周期内的原始 EDR 事件。
//
// 这是真"事件量"维度，与 alerts 表的"规则命中数"互补：
//   - alerts 反映"检出能力"（规则覆盖 + 命中频率）
//   - ebpf_events 反映"数据采集量"（agent 上报基线 + 主机活跃度）
//
// 用 CH 主键命中 + bloom filter，单次查询通常 < 200ms 即使 1B 行。
func (h *ReportsHandler) queryEDREventStatsCH(startTime, endTime time.Time) edrEventStats {
	stats := edrEventStats{
		EventsByType:    []gin.H{},
		EventsByHour:    []gin.H{},
		TopHostsByEvent: []gin.H{},
		TopExe:          []gin.H{},
	}
	if h.chConn == nil {
		return stats
	}
	stats.Available = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 总事件 + 唯一主机数
	if err := h.chConn.QueryRow(ctx, `
		SELECT count(), uniq(host_id) FROM ebpf_events WHERE timestamp >= ? AND timestamp <= ?
	`, startTime, endTime).Scan(&stats.TotalEvents, &stats.UniqueHosts); err != nil {
		h.logger.Warn("CH ebpf_events 总量查询失败", zap.Error(err))
	}

	// 2. 按 event_type 分布
	if rows, err := h.chConn.Query(ctx, `
		SELECT event_type, count() AS c FROM ebpf_events
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY event_type ORDER BY c DESC
	`, startTime, endTime); err == nil {
		for rows.Next() {
			var et string
			var c uint64
			if err := rows.Scan(&et, &c); err == nil {
				stats.EventsByType = append(stats.EventsByType, gin.H{"event_type": et, "count": c})
			}
		}
		rows.Close()
	}

	// 3. 按小时分布（趋势图）
	if rows, err := h.chConn.Query(ctx, `
		SELECT toStartOfHour(timestamp) AS h, count() AS c FROM ebpf_events
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY h ORDER BY h
	`, startTime, endTime); err == nil {
		for rows.Next() {
			var hour time.Time
			var c uint64
			if err := rows.Scan(&hour, &c); err == nil {
				stats.EventsByHour = append(stats.EventsByHour, gin.H{
					"hour":  hour.Format("2006-01-02 15:00"),
					"count": c,
				})
			}
		}
		rows.Close()
	}

	// 4. Top 10 主机 by 原始事件量
	if rows, err := h.chConn.Query(ctx, `
		SELECT host_id, count() AS c FROM ebpf_events
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY host_id ORDER BY c DESC LIMIT 10
	`, startTime, endTime); err == nil {
		for rows.Next() {
			var hostID string
			var c uint64
			if err := rows.Scan(&hostID, &c); err == nil {
				stats.TopHostsByEvent = append(stats.TopHostsByEvent, gin.H{
					"host_id": hostID, "count": c,
				})
			}
		}
		rows.Close()
		h.attachHostnames(stats.TopHostsByEvent)
	}

	// 5. Top 10 exe（最活跃进程）
	if rows, err := h.chConn.Query(ctx, `
		SELECT exe, count() AS c FROM ebpf_events
		WHERE timestamp >= ? AND timestamp <= ? AND exe != ''
		GROUP BY exe ORDER BY c DESC LIMIT 10
	`, startTime, endTime); err == nil {
		for rows.Next() {
			var exe string
			var c uint64
			if err := rows.Scan(&exe, &c); err == nil {
				stats.TopExe = append(stats.TopExe, gin.H{"exe": exe, "count": c})
			}
		}
		rows.Close()
	}

	return stats
}

// attachHostnames 给 [{host_id, count}] 列表附加 hostname (从 MySQL hosts 表查)。
func (h *ReportsHandler) attachHostnames(rows []gin.H) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if id, ok := r["host_id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	type hostRow struct {
		HostID   string
		Hostname string
	}
	var hosts []hostRow
	h.db.Model(&model.Host{}).Select("host_id, hostname").Where("host_id IN ?", ids).Scan(&hosts)
	nameByID := make(map[string]string, len(hosts))
	for _, h := range hosts {
		nameByID[h.HostID] = h.Hostname
	}
	for _, r := range rows {
		if id, ok := r["host_id"].(string); ok {
			r["hostname"] = nameByID[id]
		}
	}
}

// GetEDRExecutiveReport 生成 EDR 高管摘要（精简 1 页）
// GET /api/v1/reports/edr/executive?start_time=&end_time=
func (h *ReportsHandler) GetEDRExecutiveReport(c *gin.Context) {
	startTime, endTime, ok := parseReportTimeRange(c)
	if !ok {
		return
	}

	var (
		totalAlerts, criticalAlerts, highAlerts int64
		totalStories, highRiskStories           int64
		affectedHosts, onlineHosts              int64
	)
	timeRange := h.db.Model(&model.Alert{}).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Where("source IN ?", []string{"detection", "agent"})
	timeRange.Count(&totalAlerts)
	timeRange.Session(&gorm.Session{}).Where("severity = ?", "critical").Count(&criticalAlerts)
	timeRange.Session(&gorm.Session{}).Where("severity = ?", "high").Count(&highAlerts)
	timeRange.Session(&gorm.Session{}).Distinct("host_id").Count(&affectedHosts)
	h.db.Model(&model.Host{}).Where("status = ?", "online").Count(&onlineHosts)
	h.db.Model(&model.Storyline{}).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Count(&totalStories)
	h.db.Model(&model.Storyline{}).
		Where("created_at >= ? AND created_at <= ? AND severity IN ?", startTime, endTime, []string{"critical", "high"}).
		Count(&highRiskStories)

	// 综合风险评分（0-100，简单加权）
	coverage := 100.0
	if onlineHosts > 0 {
		coverage = float64(affectedHosts) / float64(onlineHosts) * 100
	}
	riskScore := scoreEDR(criticalAlerts, highAlerts, highRiskStories, affectedHosts, onlineHosts)

	// 自动结论
	conclusion := edrConclusion(riskScore, criticalAlerts, highRiskStories)
	suggestions := edrSuggestions(criticalAlerts, highAlerts, highRiskStories, affectedHosts, onlineHosts)

	reportID := fmt.Sprintf("edr-exec-%d", time.Now().Unix())
	periodStr := fmt.Sprintf("%s ~ %s",
		startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))

	reportData := gin.H{
		"meta": gin.H{
			"reportID":    reportID,
			"period":      periodStr,
			"generatedAt": time.Now(),
		},
		"keyMetrics": gin.H{
			"totalAlerts":     totalAlerts,
			"criticalAlerts":  criticalAlerts,
			"highAlerts":      highAlerts,
			"totalStories":    totalStories,
			"highRiskStories": highRiskStories,
			"affectedHosts":   affectedHosts,
			"onlineHosts":     onlineHosts,
			"coverage":        coverage,
		},
		"riskScore":   riskScore,
		"conclusion":  conclusion,
		"suggestions": suggestions,
	}

	h.saveGeneratedReport(model.ReportTypeEDR, "EDR 高管摘要报告", reportID, periodStr, reportData)
	Success(c, reportData)
}

// directionLabel 返回趋势方向（涨/跌/持平）的人类可读标签。
func directionLabel(growthPct float64) string {
	if growthPct > 5 {
		return "up"
	}
	if growthPct < -5 {
		return "down"
	}
	return "stable"
}

// scoreEDR 计算 EDR 综合风险评分（0-100，越高越糟）。
func scoreEDR(critical, high, highStories, affected, online int64) int {
	score := 0.0
	score += float64(critical) * 5
	score += float64(high) * 2
	score += float64(highStories) * 8
	if online > 0 {
		score += float64(affected) / float64(online) * 30
	}
	if score > 100 {
		score = 100
	}
	return int(score)
}

// edrConclusion 自动生成结论文案。
func edrConclusion(riskScore int, critical, highStories int64) string {
	switch {
	case riskScore >= 80 || critical > 50:
		return fmt.Sprintf("当前 EDR 风险等级：严重。报告周期内产生 %d 条严重告警 + %d 条高危故事线，存在活跃攻击迹象，建议立即启动应急响应。", critical, highStories)
	case riskScore >= 50 || critical > 10:
		return fmt.Sprintf("当前 EDR 风险等级：高。报告周期内产生 %d 条严重告警，存在多起需排查事件，建议安全团队 24h 内完成研判。", critical)
	case riskScore >= 25:
		return fmt.Sprintf("当前 EDR 风险等级：中。报告周期内 EDR 检测能力正常运行，发现 %d 条高风险故事线，建议定期复核。", highStories)
	default:
		return "当前 EDR 风险等级：低。报告周期内未发现明显异常活动，EDR 检测覆盖正常。"
	}
}

// edrSuggestions 自动生成行动建议清单。
func edrSuggestions(critical, high, highStories, affected, online int64) []string {
	tips := []string{}
	if critical > 0 {
		tips = append(tips, fmt.Sprintf("处置 %d 条严重告警，确认是否为真实攻击并执行响应动作。", critical))
	}
	if highStories > 0 {
		tips = append(tips, fmt.Sprintf("调查 %d 条高危攻击故事线，关联各阶段事件复盘攻击链。", highStories))
	}
	if online > 0 && float64(affected)/float64(online) > 0.5 {
		tips = append(tips, "受影响主机比例 > 50%，建议复核检测规则是否过度敏感或存在面级威胁。")
	}
	if high > 100 {
		tips = append(tips, "高危告警量较大，建议使用告警白名单 / 频率节流功能减少噪音。")
	}
	if len(tips) == 0 {
		tips = append(tips, "持续监控 EDR 告警，保持规则与威胁情报同步更新。")
	}
	return tips
}
