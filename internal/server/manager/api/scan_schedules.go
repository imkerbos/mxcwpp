package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// ScanSchedulesHandler 扫描计划 API 处理器
type ScanSchedulesHandler struct {
	db        *gorm.DB
	logger    *zap.Logger
	scheduler *biz.ScanScheduler
}

// NewScanSchedulesHandler 创建处理器
func NewScanSchedulesHandler(db *gorm.DB, logger *zap.Logger, scheduler *biz.ScanScheduler) *ScanSchedulesHandler {
	return &ScanSchedulesHandler{db: db, logger: logger, scheduler: scheduler}
}

// ListSchedules 扫描计划列表
func (h *ScanSchedulesHandler) ListSchedules(c *gin.Context) {
	var schedules []model.ScanSchedule
	if err := h.db.Order("created_at DESC").Find(&schedules).Error; err != nil {
		InternalError(c, "查询扫描计划失败")
		return
	}
	Success(c, schedules)
}

// CreateSchedule 创建扫描计划
func (h *ScanSchedulesHandler) CreateSchedule(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		ScanType string `json:"scanType" binding:"required"`
		CronExpr string `json:"cronExpr" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数错误: "+err.Error())
		return
	}

	schedule := &model.ScanSchedule{
		Name:      req.Name,
		ScanType:  req.ScanType,
		CronExpr:  req.CronExpr,
		Enabled:   true,
		CreatedBy: c.GetString("username"),
	}

	if err := h.scheduler.AddSchedule(schedule); err != nil {
		BadRequest(c, "创建扫描计划失败: "+err.Error())
		return
	}

	Success(c, schedule)
}

// UpdateSchedule 更新扫描计划
func (h *ScanSchedulesHandler) UpdateSchedule(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		BadRequest(c, "无效的 ID")
		return
	}

	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数错误")
		return
	}

	if err := h.scheduler.UpdateSchedule(uint(id), req); err != nil {
		InternalError(c, "更新失败: "+err.Error())
		return
	}

	SuccessMessage(c, "更新成功")
}

// DeleteSchedule 删除扫描计划
func (h *ScanSchedulesHandler) DeleteSchedule(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		BadRequest(c, "无效的 ID")
		return
	}

	if err := h.scheduler.RemoveSchedule(uint(id)); err != nil {
		InternalError(c, "删除失败: "+err.Error())
		return
	}

	SuccessMessage(c, "删除成功")
}

// ToggleSchedule 启用/禁用扫描计划
func (h *ScanSchedulesHandler) ToggleSchedule(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		BadRequest(c, "无效的 ID")
		return
	}

	if err := h.scheduler.ToggleSchedule(uint(id)); err != nil {
		InternalError(c, "切换状态失败: "+err.Error())
		return
	}

	SuccessMessage(c, "操作成功")
}
