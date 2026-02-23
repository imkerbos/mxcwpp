package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// FIMEventsHandler FIM 事件处理器
type FIMEventsHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewFIMEventsHandler 创建 FIM 事件处理器
func NewFIMEventsHandler(db *gorm.DB, logger *zap.Logger) *FIMEventsHandler {
	return &FIMEventsHandler{db: db, logger: logger}
}

// ListFIMEvents 获取 FIM 事件列表
func (h *FIMEventsHandler) ListFIMEvents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}

	query := h.db.Model(&model.FIMEvent{})

	// 筛选条件
	if hostID := c.Query("host_id"); hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if hostname := c.Query("hostname"); hostname != "" {
		query = query.Where("hostname LIKE ?", "%"+hostname+"%")
	}
	if filePath := c.Query("file_path"); filePath != "" {
		query = query.Where("file_path LIKE ?", "%"+filePath+"%")
	}
	if changeType := c.Query("change_type"); changeType != "" {
		query = query.Where("change_type = ?", changeType)
	}
	if severity := c.Query("severity"); severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}
	if taskID := c.Query("task_id"); taskID != "" {
		query = query.Where("task_id = ?", taskID)
	}
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		query = query.Where("detected_at >= ?", dateFrom)
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		query = query.Where("detected_at <= ?", dateTo+" 23:59:59")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询 FIM 事件总数失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	var events []model.FIMEvent
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("detected_at DESC").Find(&events).Error; err != nil {
		h.logger.Error("查询 FIM 事件列表失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	SuccessPaginated(c, total, events)
}

// GetFIMEvent 获取单个 FIM 事件详情
func (h *FIMEventsHandler) GetFIMEvent(c *gin.Context) {
	eventID := c.Param("id")

	var event model.FIMEvent
	if err := h.db.Where("event_id = ?", eventID).First(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "事件不存在")
			return
		}
		h.logger.Error("查询 FIM 事件失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	Success(c, event)
}

// FIMEventStats FIM 事件统计响应
type FIMEventStats struct {
	Total    int64 `json:"total"`
	Critical int64 `json:"critical"`
	High     int64 `json:"high"`
	Medium   int64 `json:"medium"`
	Low      int64 `json:"low"`
	// 按变更类型统计
	Added   int64 `json:"added"`
	Removed int64 `json:"removed"`
	Changed int64 `json:"changed"`
	// 按分类统计
	ByCategory map[string]int64 `json:"by_category"`
	// Top 主机
	TopHosts []FIMHostEventCount `json:"top_hosts"`
	// 趋势数据
	Trend []FIMEventTrendPoint `json:"trend"`
}

// FIMHostEventCount 主机事件数统计
type FIMHostEventCount struct {
	HostID   string `json:"host_id"`
	Hostname string `json:"hostname"`
	Count    int64  `json:"count"`
}

// FIMEventTrendPoint 事件趋势数据点
type FIMEventTrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// GetFIMEventStats 获取 FIM 事件统计
func (h *FIMEventsHandler) GetFIMEventStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	stats := FIMEventStats{
		ByCategory: make(map[string]int64),
	}

	// 总数
	h.db.Model(&model.FIMEvent{}).Count(&stats.Total)

	// 按严重等级统计
	h.db.Model(&model.FIMEvent{}).Where("severity = ?", "critical").Count(&stats.Critical)
	h.db.Model(&model.FIMEvent{}).Where("severity = ?", "high").Count(&stats.High)
	h.db.Model(&model.FIMEvent{}).Where("severity = ?", "medium").Count(&stats.Medium)
	h.db.Model(&model.FIMEvent{}).Where("severity = ?", "low").Count(&stats.Low)

	// 按变更类型统计
	h.db.Model(&model.FIMEvent{}).Where("change_type = ?", "added").Count(&stats.Added)
	h.db.Model(&model.FIMEvent{}).Where("change_type = ?", "removed").Count(&stats.Removed)
	h.db.Model(&model.FIMEvent{}).Where("change_type = ?", "changed").Count(&stats.Changed)

	// 按分类统计
	type CategoryCount struct {
		Category string `json:"category"`
		Count    int64  `json:"count"`
	}
	var categoryCounts []CategoryCount
	h.db.Model(&model.FIMEvent{}).
		Select("category, COUNT(*) as count").
		Where("category IS NOT NULL AND category != ''").
		Group("category").
		Find(&categoryCounts)
	for _, cc := range categoryCounts {
		stats.ByCategory[cc.Category] = cc.Count
	}

	// Top 10 主机
	h.db.Model(&model.FIMEvent{}).
		Select("host_id, hostname, COUNT(*) as count").
		Group("host_id, hostname").
		Order("count DESC").
		Limit(10).
		Find(&stats.TopHosts)

	// 趋势数据
	h.db.Model(&model.FIMEvent{}).
		Select("DATE(detected_at) as date, COUNT(*) as count").
		Where("detected_at >= DATE_SUB(NOW(), INTERVAL ? DAY)", days).
		Group("DATE(detected_at)").
		Order("date ASC").
		Find(&stats.Trend)

	Success(c, stats)
}
