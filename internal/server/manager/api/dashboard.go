// Package api 提供 HTTP API 处理器
package api

import (
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// DashboardHandler 是 Dashboard API 处理器
type DashboardHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewDashboardHandler 创建 Dashboard 处理器
func NewDashboardHandler(db *gorm.DB, logger *zap.Logger) *DashboardHandler {
	return &DashboardHandler{
		db:     db,
		logger: logger,
	}
}

// GetDashboardStats 获取 Dashboard 统计数据
// GET /api/v1/dashboard/stats
func (h *DashboardHandler) GetDashboardStats(c *gin.Context) {
	stats := gin.H{}

	// 1. 资产概览
	// 统计物理主机（非容器）
	var hostCount int64
	h.db.Model(&model.Host{}).Where("is_container = ?", false).Count(&hostCount)

	// 统计容器
	var containerCount int64
	h.db.Model(&model.Host{}).Where("is_container = ?", true).Count(&containerCount)

	var onlineHostCount int64
	h.db.Model(&model.Host{}).Where("status = ? AND is_container = ?", "online", false).Count(&onlineHostCount)

	var onlineContainerCount int64
	h.db.Model(&model.Host{}).Where("status = ? AND is_container = ?", "online", true).Count(&onlineContainerCount)

	var offlineHostCount int64
	h.db.Model(&model.Host{}).Where("status = ? AND is_container = ?", "offline", false).Count(&offlineHostCount)

	var offlineContainerCount int64
	h.db.Model(&model.Host{}).Where("status = ? AND is_container = ?", "offline", true).Count(&offlineContainerCount)

	stats["hosts"] = hostCount
	stats["clusters"] = 0 // TODO: 后续实现集群统计
	stats["containers"] = containerCount
	stats["onlineAgents"] = onlineHostCount + onlineContainerCount // 在线Agent总数（主机+容器）
	stats["offlineAgents"] = offlineHostCount + offlineContainerCount

	// 计算Agent数量变化（较昨日）
	onlineChange, offlineChange := h.calculateAgentChanges()
	stats["onlineAgentsChange"] = onlineChange
	stats["offlineAgentsChange"] = offlineChange

	// 2. 入侵告警统计（简化实现，后续扩展）
	stats["pendingAlerts"] = 0 // TODO: 实现告警统计

	// 3. 漏洞风险统计（简化实现，后续扩展）
	stats["pendingVulnerabilities"] = 0 // TODO: 实现漏洞统计
	stats["vulnDbUpdateTime"] = ""      // TODO: 实现漏洞库更新时间
	stats["hotPatchCount"] = 0          // TODO: 实现漏洞热补丁统计

	// 4. 基线风险统计
	// 查询最近7天的基线检查结果
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	var baselineFailCount int64
	h.db.Model(&model.ScanResult{}).
		Where("status = ? AND checked_at >= ?", "fail", sevenDaysAgo).
		Count(&baselineFailCount)

	stats["baselineFailCount"] = baselineFailCount

	// 计算基线加固百分比（优化：使用SQL聚合查询）
	baselineHardeningPercent, baselineHostPercent := h.calculateBaselinePercentages()
	stats["baselineHardeningPercent"] = baselineHardeningPercent
	stats["baselineHostPercent"] = baselineHostPercent

	// 5. 基线风险 Top 3
	baselineRisks := h.getBaselineRisksTop3()
	stats["baselineRisks"] = baselineRisks

	// 6. Agent 资源使用统计（暂时返回0，后续从心跳数据中获取）
	stats["avgCpuUsage"] = 0.0
	stats["avgCpuUsageChange"] = 0.0
	stats["avgMemoryUsage"] = 0
	stats["avgMemoryUsageChange"] = 0

	// 7. 主机风险分布百分比
	stats["hostAlertPercent"] = 0.0    // TODO: 存在告警的主机百分比
	stats["vulnHostPercent"] = 0.0     // TODO: 存在高可利用漏洞的主机百分比
	stats["runtimeAlertPercent"] = 0.0 // TODO: 存在运行时安全告警的主机百分比
	stats["virusHostPercent"] = 0.0    // TODO: 存在病毒文件的主机百分比

	// 8. 后端服务状态
	// 注意：基线检查插件在 Agent 端运行，Server 端无法直接检查其状态
	// 如果需要了解基线检查活动情况，可以通过"在线 Agent 数量"或"最近基线检查结果"来判断
	serviceStatus := gin.H{
		"database":    h.checkDatabaseStatus(),
		"agentcenter": h.checkAgentCenterStatus(),
		"manager":     "healthy", // Manager 服务本身，如果这个接口能访问说明服务正常
	}
	stats["serviceStatus"] = serviceStatus

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": stats,
	})
}

// calculateAgentChanges 计算Agent数量变化（较昨日）
// 简化实现：由于没有历史快照数据，使用24小时前的心跳时间来判断
// 如果主机在24-48小时前有心跳，认为24小时前是在线的
func (h *DashboardHandler) calculateAgentChanges() (int, int) {
	now := time.Now()
	oneDayAgo := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	// 查询24小时前在线的主机（通过last_heartbeat判断）
	// 如果last_heartbeat在24-48小时前之间，说明24小时前可能是在线的
	var yesterdayOnlineCount int64
	h.db.Model(&model.Host{}).
		Where("last_heartbeat >= ? AND last_heartbeat < ?", twoDaysAgo, oneDayAgo).
		Count(&yesterdayOnlineCount)

	// 查询当前在线的主机数量
	var currentOnlineCount int64
	h.db.Model(&model.Host{}).Where("status = ?", "online").Count(&currentOnlineCount)

	// 查询当前离线的主机数量
	var currentOfflineCount int64
	h.db.Model(&model.Host{}).Where("status = ?", "offline").Count(&currentOfflineCount)

	// 查询24小时前的主机总数（创建时间在24小时前）
	var yesterdayTotalCount int64
	h.db.Model(&model.Host{}).
		Where("created_at <= ?", oneDayAgo).
		Count(&yesterdayTotalCount)

	// 计算24小时前的离线数（总主机数 - 在线数）
	var yesterdayOfflineCount int64
	if yesterdayTotalCount > yesterdayOnlineCount {
		yesterdayOfflineCount = yesterdayTotalCount - yesterdayOnlineCount
	}

	// 计算变化
	onlineChange := int(currentOnlineCount) - int(yesterdayOnlineCount)
	offlineChange := int(currentOfflineCount) - int(yesterdayOfflineCount)

	// 如果数据不足（没有历史数据），返回0
	if yesterdayTotalCount == 0 {
		onlineChange = 0
		offlineChange = 0
	}

	return onlineChange, offlineChange
}

// calculateBaselinePercentages 计算基线合规率和存在高危基线问题的主机百分比
func (h *DashboardHandler) calculateBaselinePercentages() (float64, float64) {
	var totalHosts int64
	h.db.Model(&model.Host{}).Count(&totalHosts)

	if totalHosts == 0 {
		return 100.0, 0.0 // 没有主机时，合规率为100%，高危主机为0%
	}

	// 统计有基线检查结果的主机（作为检查过的主机）
	var hostsWithResults int64
	h.db.Model(&model.ScanResult{}).
		Distinct("host_id").
		Count(&hostsWithResults)

	if hostsWithResults == 0 {
		return 100.0, 0.0 // 没有检查结果时，默认合规
	}

	// 统计检查通过的结果数
	var passCount int64
	h.db.Model(&model.ScanResult{}).
		Where("status = ?", "pass").
		Count(&passCount)

	// 统计检查失败的结果数
	var failCount int64
	h.db.Model(&model.ScanResult{}).
		Where("status = ?", "fail").
		Count(&failCount)

	// 基线合规率 = 通过的检查项数 / 总检查项数 * 100
	totalResults := passCount + failCount
	var complianceRate float64
	if totalResults > 0 {
		complianceRate = float64(passCount) / float64(totalResults) * 100.0
	} else {
		complianceRate = 100.0
	}

	// 存在高危基线的主机百分比（查询有high或critical级别失败结果的主机）
	var hostsWithHighRiskBaseline int64
	h.db.Model(&model.ScanResult{}).
		Where("status = ? AND severity IN (?)", "fail", []string{"high", "critical"}).
		Distinct("host_id").
		Count(&hostsWithHighRiskBaseline)

	highRiskHostPercent := float64(hostsWithHighRiskBaseline) / float64(totalHosts) * 100.0

	// 确保百分比在合理范围内
	if complianceRate > 100.0 {
		complianceRate = 100.0
	}
	if highRiskHostPercent > 100.0 {
		highRiskHostPercent = 100.0
	}

	return complianceRate, highRiskHostPercent
}

// getBaselineRisksTop3 获取基线风险 Top 3（优化：使用更好的排序算法）
func (h *DashboardHandler) getBaselineRisksTop3() []gin.H {
	// 查询所有策略，统计每个策略的风险数量
	type PolicyRisk struct {
		PolicyID string
		Name     string
		Critical int64
		Medium   int64
		Low      int64
		Score    int64 // 风险评分
	}

	var policyRisks []PolicyRisk

	// 查询所有策略
	var policies []model.Policy
	h.db.Find(&policies)

	for _, policy := range policies {
		var criticalCount, mediumCount, lowCount int64

		// 查询该策略下失败的基线检查结果，按严重程度统计
		h.db.Model(&model.ScanResult{}).
			Where("policy_id = ? AND status = ? AND severity = ?", policy.ID, "fail", "critical").
			Count(&criticalCount)

		h.db.Model(&model.ScanResult{}).
			Where("policy_id = ? AND status = ? AND severity = ?", policy.ID, "fail", "medium").
			Count(&mediumCount)

		h.db.Model(&model.ScanResult{}).
			Where("policy_id = ? AND status = ? AND severity = ?", policy.ID, "fail", "low").
			Count(&lowCount)

		// 只包含有风险的策略
		if criticalCount > 0 || mediumCount > 0 || lowCount > 0 {
			// 风险评分：critical * 3 + medium * 2 + low
			score := criticalCount*3 + mediumCount*2 + lowCount
			policyRisks = append(policyRisks, PolicyRisk{
				PolicyID: policy.ID,
				Name:     policy.Name,
				Critical: criticalCount,
				Medium:   mediumCount,
				Low:      lowCount,
				Score:    score,
			})
		}
	}

	// 按风险评分排序（降序）
	sort.Slice(policyRisks, func(i, j int) bool {
		return policyRisks[i].Score > policyRisks[j].Score
	})

	// 取 Top 3
	top3 := make([]gin.H, 0, 3)
	for i := 0; i < len(policyRisks) && i < 3; i++ {
		top3 = append(top3, gin.H{
			"name":     policyRisks[i].Name,
			"critical": policyRisks[i].Critical,
			"medium":   policyRisks[i].Medium,
			"low":      policyRisks[i].Low,
		})
	}

	return top3
}

// checkDatabaseStatus 检查数据库连接状态
func (h *DashboardHandler) checkDatabaseStatus() string {
	if h.db == nil {
		return "error"
	}

	sqlDB, err := h.db.DB()
	if err != nil {
		return "error"
	}

	// 执行 ping 操作（带超时）
	done := make(chan error, 1)
	go func() {
		done <- sqlDB.Ping()
	}()

	select {
	case err := <-done:
		if err != nil {
			return "error"
		}
		return "healthy"
	case <-time.After(2 * time.Second):
		return "warning"
	}
}

// checkAgentCenterStatus 检查 AgentCenter 服务状态
// 通过检查 AgentCenter gRPC 端口是否可访问来判断服务本身是否健康
func (h *DashboardHandler) checkAgentCenterStatus() string {
	// AgentCenter 默认端口是 6751
	// 尝试多个可能的地址（支持 Docker 环境和本地环境）
	addresses := []string{
		"localhost:6751",   // 本地开发环境
		"agentcenter:6751", // Docker Compose 环境（服务名）
		"127.0.0.1:6751",   // 本地回环地址
	}

	for _, addr := range addresses {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			// 端口可访问，服务运行正常
			return "healthy"
		}
	}

	// 所有地址都不可访问，服务可能未运行
	return "error"
}

// 注意：基线检查插件在 Agent 端运行，Server 端无法直接检查其状态
// 如需了解基线检查活动情况，可以通过以下方式：
// 1. 检查在线 Agent 数量（有在线 Agent 说明 Agent 和插件可能在工作）
// 2. 检查最近的基线检查结果（有结果说明插件最近执行过检查）
// 3. 检查扫描任务状态（有运行中的任务说明插件可能在工作）
