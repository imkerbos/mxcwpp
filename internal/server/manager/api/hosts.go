// Package api 提供 HTTP API 处理器
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/manager/biz"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// HostsHandler 是主机管理 API 处理器
type HostsHandler struct {
	db             *gorm.DB
	logger         *zap.Logger
	scoreCache     *biz.BaselineScoreCache
	metricsService *biz.MetricsService
}

// NewHostsHandler 创建主机处理器
func NewHostsHandler(db *gorm.DB, logger *zap.Logger, scoreCache *biz.BaselineScoreCache, metricsService *biz.MetricsService) *HostsHandler {
	return &HostsHandler{
		db:             db,
		logger:         logger,
		scoreCache:     scoreCache,
		metricsService: metricsService,
	}
}

// HostListItem 主机列表项（包含基线得分）
type HostListItem struct {
	model.Host
	BaselineScore    int     `json:"baseline_score"`
	BaselinePassRate float64 `json:"baseline_pass_rate"`
}

// ListHosts 获取主机列表
// GET /api/v1/hosts
func (h *HostsHandler) ListHosts(c *gin.Context) {
	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	osFamily := c.Query("os_family")
	status := c.Query("status")
	businessLine := c.Query("business_line")  // 业务线筛选
	search := c.Query("search")               // 搜索关键词
	isContainerStr := c.Query("is_container") // 容器/主机类型筛选（废弃，使用 runtime_type）
	runtimeType := c.Query("runtime_type")    // 运行环境类型筛选：vm/docker/k8s

	// 构建查询
	query := h.db.Model(&model.Host{})

	// 过滤条件
	if osFamily != "" {
		query = query.Where("os_family = ?", osFamily)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if businessLine != "" {
		query = query.Where("business_line = ?", businessLine)
	}
	// 运行环境类型筛选（优先使用新参数）
	if runtimeType != "" && model.IsValidRuntimeType(runtimeType) {
		query = query.Where("runtime_type = ?", runtimeType)
	} else if isContainerStr != "" {
		// 向后兼容：容器/主机类型筛选
		isContainer := isContainerStr == "true"
		query = query.Where("is_container = ?", isContainer)
	}
	// 搜索功能：支持按主机名、host_id 搜索
	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("hostname LIKE ? OR host_id LIKE ?", searchPattern, searchPattern)
	}

	// 计算总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询主机总数失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机列表失败",
		})
		return
	}

	// 分页查询
	var hosts []model.Host
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("last_heartbeat DESC").Find(&hosts).Error; err != nil {
		h.logger.Error("查询主机列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机列表失败",
		})
		return
	}

	// 计算每个主机的基线得分
	items := make([]HostListItem, 0, len(hosts))
	for _, host := range hosts {
		item := HostListItem{
			Host:             host,
			BaselineScore:    0,
			BaselinePassRate: 0.0,
		}

		// 如果有得分缓存，使用缓存计算得分
		if h.scoreCache != nil {
			score, err := h.scoreCache.GetHostScore(host.HostID)
			if err != nil {
				h.logger.Warn("计算主机基线得分失败", zap.String("host_id", host.HostID), zap.Error(err))
				// 继续处理，使用默认值 0
			} else if score != nil {
				item.BaselineScore = score.BaselineScore
				item.BaselinePassRate = score.PassRate
			}
		}

		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": items,
		},
	})
}

// HostStatusDistribution 主机状态分布统计
type HostStatusDistribution struct {
	Running      int64 `json:"running"`       // 运行中
	Abnormal     int64 `json:"abnormal"`      // 运行异常
	Offline      int64 `json:"offline"`       // 离线
	NotInstalled int64 `json:"not_installed"` // 未安装
	Uninstalled  int64 `json:"uninstalled"`   // 已卸载
}

// HostRiskDistribution 主机基线风险分布统计（按严重程度）
type HostRiskDistribution struct {
	Critical int64 `json:"critical"` // 存在严重风险基线的主机数
	High     int64 `json:"high"`     // 存在高危风险基线的主机数
	Medium   int64 `json:"medium"`   // 存在中危风险基线的主机数
	Low      int64 `json:"low"`      // 存在低危风险基线的主机数
}

// GetHostStatusDistribution 获取主机状态分布
// GET /api/v1/hosts/status-distribution
func (h *HostsHandler) GetHostStatusDistribution(c *gin.Context) {
	var distribution HostStatusDistribution

	// 运行中（在线）
	h.db.Model(&model.Host{}).Where("status = ?", "online").Count(&distribution.Running)

	// 离线
	h.db.Model(&model.Host{}).Where("status = ?", "offline").Count(&distribution.Offline)

	// 运行异常（暂时用离线超过一定时间的主机表示，后续可扩展）
	// TODO: 实现运行异常的逻辑

	// 未安装和已卸载（暂时返回0，后续扩展）
	// TODO: 实现未安装和已卸载的逻辑

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": distribution,
	})
}

// GetHostRiskDistribution 获取主机基线风险分布（按严重程度）
// GET /api/v1/hosts/risk-distribution
func (h *HostsHandler) GetHostRiskDistribution(c *gin.Context) {
	var distribution HostRiskDistribution

	// 统计存在严重(critical)风险基线的主机数
	var hostsWithCritical []string
	h.db.Model(&model.ScanResult{}).
		Select("DISTINCT host_id").
		Where("status = ? AND severity = ?", "fail", "critical").
		Pluck("host_id", &hostsWithCritical)
	distribution.Critical = int64(len(hostsWithCritical))

	// 统计存在高危(high)风险基线的主机数
	var hostsWithHigh []string
	h.db.Model(&model.ScanResult{}).
		Select("DISTINCT host_id").
		Where("status = ? AND severity = ?", "fail", "high").
		Pluck("host_id", &hostsWithHigh)
	distribution.High = int64(len(hostsWithHigh))

	// 统计存在中危(medium)风险基线的主机数
	var hostsWithMedium []string
	h.db.Model(&model.ScanResult{}).
		Select("DISTINCT host_id").
		Where("status = ? AND severity = ?", "fail", "medium").
		Pluck("host_id", &hostsWithMedium)
	distribution.Medium = int64(len(hostsWithMedium))

	// 统计存在低危(low)风险基线的主机数
	var hostsWithLow []string
	h.db.Model(&model.ScanResult{}).
		Select("DISTINCT host_id").
		Where("status = ? AND severity = ?", "fail", "low").
		Pluck("host_id", &hostsWithLow)
	distribution.Low = int64(len(hostsWithLow))

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": distribution,
	})
}

// GetHost 获取主机详情
// GET /api/v1/hosts/:host_id
func (h *HostsHandler) GetHost(c *gin.Context) {
	hostID := c.Param("host_id")

	var host model.Host
	if err := h.db.Where("host_id = ?", hostID).First(&host).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "主机不存在",
			})
			return
		}
		h.logger.Error("查询主机失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机失败",
		})
		return
	}

	// 查询基线结果（移除数量限制，返回所有结果）
	var results []model.ScanResult
	h.db.Where("host_id = ?", hostID).
		Order("checked_at DESC").
		Find(&results)

	// 查询最新监控数据
	var latestMetric model.HostMetric
	h.db.Where("host_id = ?", hostID).
		Order("collected_at DESC").
		Limit(1).
		First(&latestMetric)

	// 构建响应数据（扁平化结构，符合前端 HostDetail 接口）
	responseData := gin.H{
		"host_id":          host.HostID,
		"hostname":         host.Hostname,
		"os_family":        host.OSFamily,
		"os_version":       host.OSVersion,
		"kernel_version":   host.KernelVersion,
		"arch":             host.Arch,
		"ipv4":             host.IPv4,
		"ipv6":             host.IPv6,
		"public_ipv4":      host.PublicIPv4,
		"public_ipv6":      host.PublicIPv6,
		"status":           string(host.Status),
		"last_heartbeat":   host.LastHeartbeat,
		"created_at":       host.CreatedAt,
		"updated_at":       host.UpdatedAt,
		"baseline_results": results,
	}

	// 添加监控数据
	if latestMetric.ID > 0 {
		if latestMetric.CPUUsage != nil {
			responseData["cpu_usage"] = formatPercent(*latestMetric.CPUUsage)
		}
		if latestMetric.MemUsage != nil {
			responseData["memory_usage"] = formatPercent(*latestMetric.MemUsage)
		}
	}

	// 添加硬件和系统信息（从 Host 模型获取）
	if host.DeviceModel != "" {
		responseData["device_model"] = host.DeviceModel
	}
	if host.Manufacturer != "" {
		responseData["manufacturer"] = host.Manufacturer
	}
	if host.DeviceSerial != "" {
		responseData["device_serial"] = host.DeviceSerial
	}
	if host.DeviceID != "" {
		responseData["device_id"] = host.DeviceID
	} else {
		// 如果 device_id 为空，使用 host_id
		responseData["device_id"] = host.HostID
	}
	if host.CPUInfo != "" {
		responseData["cpu_info"] = host.CPUInfo
	}
	if host.MemorySize != "" {
		responseData["memory_size"] = host.MemorySize
	}
	if host.SystemLoad != "" {
		responseData["system_load"] = host.SystemLoad
	}
	if host.DefaultGateway != "" {
		responseData["default_gateway"] = host.DefaultGateway
	}
	if host.NetworkMode != "" {
		responseData["network_mode"] = host.NetworkMode
	}
	if len(host.DNSServers) > 0 {
		responseData["dns_servers"] = host.DNSServers
	}
	if host.BusinessLine != "" {
		responseData["business_line"] = host.BusinessLine
	}
	if host.SystemBootTime != nil {
		responseData["system_boot_time"] = host.SystemBootTime
	}
	if host.AgentStartTime != nil {
		responseData["agent_start_time"] = host.AgentStartTime
	}
	if host.LastActiveTime != nil {
		responseData["last_active_time"] = host.LastActiveTime
	} else {
		// 如果 last_active_time 为空，使用 last_heartbeat
		responseData["last_active_time"] = host.LastHeartbeat
	}
	if len(host.Tags) > 0 {
		responseData["tags"] = host.Tags
	}
	// 添加磁盘和网卡信息（JSON 字符串）
	if host.DiskInfo != "" {
		responseData["disk_info"] = host.DiskInfo
	}
	if host.NetworkInterfaces != "" {
		responseData["network_interfaces"] = host.NetworkInterfaces
	}
	// 添加容器标识
	responseData["is_container"] = host.IsContainer
	if host.ContainerID != "" {
		responseData["container_id"] = host.ContainerID
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": responseData,
	})
}

// UpdateHostTags 更新主机标签
// PUT /api/v1/hosts/:host_id/tags
func (h *HostsHandler) UpdateHostTags(c *gin.Context) {
	hostID := c.Param("host_id")

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证标签数量（最多10个）
	if len(req.Tags) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "标签数量不能超过10个",
		})
		return
	}

	// 验证标签长度（每个标签最多50个字符）
	for _, tag := range req.Tags {
		if len(tag) > 50 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "标签长度不能超过50个字符",
			})
			return
		}
	}

	// 更新数据库
	tags := model.StringArray(req.Tags)
	if err := h.db.Model(&model.Host{}).Where("host_id = ?", hostID).Update("tags", tags).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "主机不存在",
			})
			return
		}
		h.logger.Error("更新主机标签失败", zap.Error(err), zap.String("host_id", hostID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新主机标签失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "标签更新成功",
	})
}

// formatPercent 格式化百分比
func formatPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64) + "%"
}

// formatBytes 格式化字节数
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(bytes)/float64(div), 'f', 2, 64) + " " + []string{"KB", "MB", "GB", "TB"}[exp]
}

// GetHostMetrics 获取主机监控数据
// GET /api/v1/hosts/:host_id/metrics
func (h *HostsHandler) GetHostMetrics(c *gin.Context) {
	hostID := c.Param("host_id")

	// 解析查询参数（可选的时间范围）
	var startTime, endTime *time.Time
	if startStr := c.Query("start_time"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = &t
		}
	}
	if endStr := c.Query("end_time"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = &t
		}
	}

	// 如果没有指定时间范围，默认查询最近 1 小时
	if startTime == nil && endTime == nil {
		now := time.Now()
		oneHourAgo := now.Add(-1 * time.Hour)
		startTime = &oneHourAgo
		endTime = &now
	}

	// 查询监控数据
	metrics, err := h.metricsService.GetHostMetrics(c.Request.Context(), hostID, startTime, endTime)
	if err != nil {
		h.logger.Error("查询主机监控数据失败", zap.String("host_id", hostID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机监控数据失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": metrics,
	})
}

// HostRiskStatistics 主机风险统计
type HostRiskStatistics struct {
	// 安全告警统计
	Alerts struct {
		Total    int64 `json:"total"`    // 未处理告警总数
		Critical int64 `json:"critical"` // 严重
		High     int64 `json:"high"`     // 高危
		Medium   int64 `json:"medium"`   // 中危
		Low      int64 `json:"low"`      // 低危
	} `json:"alerts"`
	// 漏洞风险统计
	Vulnerabilities struct {
		Total    int64 `json:"total"`    // 未处理高可利用漏洞总数
		Critical int64 `json:"critical"` // 严重
		High     int64 `json:"high"`     // 高危
		Medium   int64 `json:"medium"`   // 中危
		Low      int64 `json:"low"`      // 低危
	} `json:"vulnerabilities"`
	// 基线风险统计
	Baseline struct {
		Total    int64 `json:"total"`    // 待加固基线总数
		Critical int64 `json:"critical"` // 严重（基线中通常没有critical，但保留字段）
		High     int64 `json:"high"`     // 高危
		Medium   int64 `json:"medium"`   // 中危
		Low      int64 `json:"low"`      // 低危
	} `json:"baseline"`
}

// GetHostRiskStatistics 获取主机风险统计
// GET /api/v1/hosts/:host_id/risk-statistics
func (h *HostsHandler) GetHostRiskStatistics(c *gin.Context) {
	hostID := c.Param("host_id")

	var stats HostRiskStatistics

	// 查询基线风险统计（从 scan_results 表）
	var baselineResults []struct {
		Severity string
		Count    int64
	}
	h.db.Model(&model.ScanResult{}).
		Select("severity, COUNT(*) as count").
		Where("host_id = ? AND status = ?", hostID, "fail").
		Group("severity").
		Scan(&baselineResults)

	for _, r := range baselineResults {
		switch r.Severity {
		case "critical":
			stats.Baseline.Critical = r.Count
		case "high":
			stats.Baseline.High = r.Count
		case "medium":
			stats.Baseline.Medium = r.Count
		case "low":
			stats.Baseline.Low = r.Count
		}
		stats.Baseline.Total += r.Count
	}

	// 安全告警和漏洞风险统计暂时返回0（后续扩展）
	// TODO: 实现安全告警统计（需要告警数据表）
	// TODO: 实现漏洞风险统计（需要漏洞数据表）

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": stats,
	})
}

// UpdateHostBusinessLineRequest 更新主机业务线请求
type UpdateHostBusinessLineRequest struct {
	BusinessLine string `json:"business_line"` // 业务线名称（空字符串表示取消绑定）
}

// UpdateHostBusinessLine 更新主机业务线
// PUT /api/v1/hosts/:host_id/business-line
func (h *HostsHandler) UpdateHostBusinessLine(c *gin.Context) {
	hostID := c.Param("host_id")

	var req UpdateHostBusinessLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 查询主机
	var host model.Host
	if err := h.db.First(&host, "host_id = ?", hostID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "主机不存在",
			})
			return
		}
		h.logger.Error("查询主机失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询主机失败",
		})
		return
	}

	// 如果指定了业务线，验证业务线是否存在
	if req.BusinessLine != "" {
		var businessLine model.BusinessLine
		if err := h.db.Where("name = ? AND enabled = ?", req.BusinessLine, true).First(&businessLine).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusBadRequest, gin.H{
					"code":    400,
					"message": "业务线不存在或已禁用",
				})
				return
			}
			h.logger.Error("查询业务线失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询业务线失败",
			})
			return
		}
	}

	// 更新业务线
	host.BusinessLine = req.BusinessLine
	if err := h.db.Save(&host).Error; err != nil {
		h.logger.Error("更新主机业务线失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新主机业务线失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "更新成功",
		"data":    host,
	})
}

// HostPluginResponse 主机插件响应
type HostPluginResponse struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	StartTime     string `json:"start_time,omitempty"`
	UpdatedAt     string `json:"updated_at"`
	LatestVersion string `json:"latest_version"`
	NeedUpdate    bool   `json:"need_update"`
}

// GetHostPlugins 获取主机插件列表
// GET /api/v1/hosts/:host_id/plugins
func (h *HostsHandler) GetHostPlugins(c *gin.Context) {
	hostID := c.Param("host_id")
	if hostID == "" {
		BadRequest(c, "host_id 不能为空")
		return
	}

	// 检查主机是否存在
	var host model.Host
	if err := h.db.Where("host_id = ?", hostID).First(&host).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "主机不存在")
			return
		}
		h.logger.Error("查询主机失败", zap.String("host_id", hostID), zap.Error(err))
		InternalError(c, "查询主机失败")
		return
	}

	// 查询主机插件
	var hostPlugins []model.HostPlugin
	if err := h.db.Where("host_id = ?", hostID).Find(&hostPlugins).Error; err != nil {
		h.logger.Error("查询主机插件失败", zap.String("host_id", hostID), zap.Error(err))
		InternalError(c, "查询主机插件失败")
		return
	}

	// 查询最新插件版本（从 plugin_configs 表）
	var pluginConfigs []model.PluginConfig
	if err := h.db.Where("enabled = ?", true).Find(&pluginConfigs).Error; err != nil {
		h.logger.Warn("查询插件配置失败", zap.Error(err))
	}

	// 构建插件名称到最新版本的映射
	latestVersions := make(map[string]string)
	for _, pc := range pluginConfigs {
		latestVersions[pc.Name] = pc.Version
	}

	// 构建响应
	var response []HostPluginResponse
	for _, hp := range hostPlugins {
		latestVersion := latestVersions[hp.Name]
		needUpdate := latestVersion != "" && hp.Version != latestVersion

		item := HostPluginResponse{
			ID:            hp.ID,
			Name:          hp.Name,
			Version:       hp.Version,
			Status:        string(hp.Status),
			UpdatedAt:     hp.UpdatedAt.Time().Format("2006-01-02 15:04:05"),
			LatestVersion: latestVersion,
			NeedUpdate:    needUpdate,
		}
		if hp.StartTime != nil {
			item.StartTime = hp.StartTime.Time().Format("2006-01-02 15:04:05")
		}
		response = append(response, item)
	}

	// 如果主机没有插件记录，但有可用的插件配置，显示为未安装
	for name, latestVersion := range latestVersions {
		found := false
		for _, hp := range hostPlugins {
			if hp.Name == name {
				found = true
				break
			}
		}
		if !found {
			response = append(response, HostPluginResponse{
				Name:          name,
				Version:       "-",
				Status:        "not_installed",
				LatestVersion: latestVersion,
				NeedUpdate:    true,
			})
		}
	}

	Success(c, response)
}

// DeleteHost 删除主机
// DELETE /api/v1/hosts/:host_id
func (h *HostsHandler) DeleteHost(c *gin.Context) {
	hostID := c.Param("host_id")

	// 查询主机是否存在
	var host model.Host
	if err := h.db.Where("host_id = ?", hostID).First(&host).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "主机不存在")
			return
		}
		h.logger.Error("查询主机失败", zap.String("host_id", hostID), zap.Error(err))
		InternalError(c, "查询主机失败")
		return
	}

	// 使用事务删除主机及其所有关联数据
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// 1. 删除扫描结果
		if err := tx.Where("host_id = ?", hostID).Delete(&model.ScanResult{}).Error; err != nil {
			return err
		}

		// 2. 删除告警
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Alert{}).Error; err != nil {
			return err
		}

		// 3. 删除主机监控数据
		if err := tx.Where("host_id = ?", hostID).Delete(&model.HostMetric{}).Error; err != nil {
			return err
		}

		// 4. 删除主机插件信息
		if err := tx.Where("host_id = ?", hostID).Delete(&model.HostPlugin{}).Error; err != nil {
			return err
		}

		// 5. 删除资产数据（进程、端口、软件、容器等）
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Process{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Port{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Software{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Container{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.AssetUser{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Cron{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Service{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.NetInterface{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Volume{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.Kmod{}).Error; err != nil {
			return err
		}
		if err := tx.Where("host_id = ?", hostID).Delete(&model.App{}).Error; err != nil {
			return err
		}

		// 6. 清除基线得分缓存
		if h.scoreCache != nil {
			h.scoreCache.InvalidateHostScore(hostID)
		}

		// 7. 最后删除主机记录
		if err := tx.Delete(&host).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		h.logger.Error("删除主机失败", zap.String("host_id", hostID), zap.Error(err))
		InternalError(c, "删除主机失败")
		return
	}

	h.logger.Info("主机已删除", zap.String("host_id", hostID), zap.String("hostname", host.Hostname))
	SuccessMessage(c, "主机删除成功")
}
