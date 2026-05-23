package api

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// AnomalyHandler handles ML anomaly detection API requests.
type AnomalyHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAnomalyHandler creates a new anomaly detection handler.
func NewAnomalyHandler(db *gorm.DB, logger *zap.Logger) *AnomalyHandler {
	return &AnomalyHandler{db: db, logger: logger}
}

// ListAnomalies returns paginated ML anomaly alerts.
// GET /api/v1/anomalies?host_id=xxx&alert_type=isolation_forest&severity=critical&status=open&page=1&page_size=20
func (h *AnomalyHandler) ListAnomalies(c *gin.Context) {
	hostID := c.Query("host_id")
	alertType := c.Query("alert_type")
	severity := c.Query("severity")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	q := h.db.Model(&model.AnomalyAlert{})
	if hostID != "" {
		q = q.Where("host_id = ?", hostID)
	}
	if alertType != "" {
		q = q.Where("alert_type = ?", alertType)
	}
	if severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}

	var total int64
	q.Count(&total)

	var alerts []model.AnomalyAlert
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&alerts).Error; err != nil {
		InternalError(c, "查询失败")
		return
	}

	Success(c, PaginatedData{Total: total, Items: alerts})
}

// GetAnomalyStats returns anomaly alert statistics.
// GET /api/v1/anomalies/stats
func (h *AnomalyHandler) GetAnomalyStats(c *gin.Context) {
	var total, open, critical int64

	h.db.Model(&model.AnomalyAlert{}).Count(&total)
	h.db.Model(&model.AnomalyAlert{}).Where("status = ?", "open").Count(&open)
	h.db.Model(&model.AnomalyAlert{}).Where("severity = ? AND status = ?", "critical", "open").Count(&critical)

	// By alert type.
	type typeCount struct {
		AlertType string `json:"alert_type"`
		Count     int64  `json:"count"`
	}
	var byType []typeCount
	h.db.Model(&model.AnomalyAlert{}).
		Select("alert_type, count(*) as count").
		Where("status = ?", "open").
		Group("alert_type").
		Find(&byType)

	// By pattern (for correlation alerts).
	var byPattern []typeCount
	h.db.Model(&model.AnomalyAlert{}).
		Select("pattern_name as alert_type, count(*) as count").
		Where("status = ? AND alert_type = ?", "open", "correlation").
		Group("pattern_name").
		Find(&byPattern)

	Success(c, gin.H{
		"total":      total,
		"open":       open,
		"critical":   critical,
		"by_type":    byType,
		"by_pattern": byPattern,
	})
}

type resolveAnomalyReq struct {
	Status string `json:"status" binding:"required"` // confirmed / false_positive
}

// ResolveAnomaly updates the status of an anomaly alert.
// PUT /api/v1/anomalies/:id/resolve
func (h *AnomalyHandler) ResolveAnomaly(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "无效的 ID")
		return
	}

	var req resolveAnomalyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数错误")
		return
	}

	if req.Status != "confirmed" && req.Status != "false_positive" {
		BadRequest(c, "状态必须是 confirmed 或 false_positive")
		return
	}

	var alert model.AnomalyAlert
	if err := h.db.First(&alert, id).Error; err != nil {
		NotFound(c, "异常告警不存在")
		return
	}

	username, _ := c.Get("username")
	h.db.Model(&alert).Updates(map[string]any{
		"status":      req.Status,
		"resolved_by": fmt.Sprintf("%v", username),
	})

	Success(c, alert)
}
