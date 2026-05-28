package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// StorylineHandler 攻击故事线 API 处理器
type StorylineHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewStorylineHandler 创建攻击故事线 API 处理器
func NewStorylineHandler(db *gorm.DB, logger *zap.Logger) *StorylineHandler {
	return &StorylineHandler{db: db, logger: logger}
}

// ListStorylines 查看攻击故事线列表
func (h *StorylineHandler) ListStorylines(c *gin.Context) {
	hostID := c.Query("host_id")
	severity := c.Query("severity")
	status := c.Query("status")

	query := h.db.Model(&model.Storyline{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	query = query.Order("last_seen_at DESC")

	var total int64
	if err := query.Count(&total).Error; err != nil {
		InternalError(c, "查询故事线失败")
		return
	}

	page, pageSize := parsePagination(c)
	var stories []model.Storyline
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&stories).Error; err != nil {
		InternalError(c, "查询故事线失败")
		return
	}

	SuccessPaginated(c, total, stories)
}

// GetStoryline 获取故事线详情（含事件时间线，分页）
//
// 单 storyline 的 events 可达数万级（EDR ebpf 全量关联），全量返回 JSON
// 体积过大导致浏览器解析+渲染卡死。改用分页：默认 page=1 page_size=100，
// 上限 500；UI 增量加载。
func (h *StorylineHandler) GetStoryline(c *gin.Context) {
	storyID := c.Param("story_id")

	var story model.Storyline
	if err := h.db.Where("story_id = ?", storyID).First(&story).Error; err != nil {
		NotFound(c, "故事线不存在")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}

	var eventsTotal int64
	h.db.Model(&model.StorylineEvent{}).Where("story_id = ?", storyID).Count(&eventsTotal)

	var events []model.StorylineEvent
	h.db.Where("story_id = ?", storyID).
		Order("timestamp ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&events)

	Success(c, gin.H{
		"storyline":        story,
		"events":           events,
		"events_total":     eventsTotal,
		"events_page":      page,
		"events_page_size": pageSize,
	})
}

// ResolveStoryline 标记故事线为已处理
func (h *StorylineHandler) ResolveStoryline(c *gin.Context) {
	storyID := c.Param("story_id")

	result := h.db.Model(&model.Storyline{}).
		Where("story_id = ?", storyID).
		Updates(map[string]any{
			"status":      "resolved",
			"resolved_by": c.GetString("username"),
		})
	if result.RowsAffected == 0 {
		NotFound(c, "故事线不存在")
		return
	}
	SuccessMessage(c, "故事线已标记为已处理")
}

// GetStorylineStats 故事线统计概览
func (h *StorylineHandler) GetStorylineStats(c *gin.Context) {
	var total, active, critical int64

	h.db.Model(&model.Storyline{}).Count(&total)
	h.db.Model(&model.Storyline{}).Where("status = ?", "active").Count(&active)
	h.db.Model(&model.Storyline{}).Where("severity = ? AND status = ?", "critical", "active").Count(&critical)

	Success(c, gin.H{
		"total":           total,
		"active":          active,
		"critical_active": critical,
	})
}
