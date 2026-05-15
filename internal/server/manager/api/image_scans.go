package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
)

// ImageScansHandler 镜像扫描 API 处理器
type ImageScansHandler struct {
	db      *gorm.DB
	logger  *zap.Logger
	scanner *biz.ImageScanner
}

// NewImageScansHandler 创建处理器
func NewImageScansHandler(db *gorm.DB, logger *zap.Logger) *ImageScansHandler {
	return &ImageScansHandler{
		db:      db,
		logger:  logger,
		scanner: biz.NewImageScanner(db, logger),
	}
}

// ScanImage 触发镜像扫描
func (h *ImageScansHandler) ScanImage(c *gin.Context) {
	var req struct {
		Image string `json:"image" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数错误: "+err.Error())
		return
	}

	scan, err := h.scanner.ScanImage(req.Image)
	if err != nil {
		h.logger.Warn("镜像扫描失败", zap.Error(err))
		// 即使失败也返回扫描记录（包含错误信息）
		if scan != nil {
			Success(c, scan)
			return
		}
		InternalError(c, "镜像扫描失败: "+err.Error())
		return
	}

	Success(c, scan)
}

// ListScans 扫描记录列表
func (h *ImageScansHandler) ListScans(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	scans, total, err := h.scanner.GetScanHistory(page, pageSize)
	if err != nil {
		InternalError(c, "查询扫描记录失败")
		return
	}

	SuccessPaginated(c, total, scans)
}

// GetScan 扫描详情
func (h *ImageScansHandler) GetScan(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		BadRequest(c, "无效的 ID")
		return
	}

	scan, err := h.scanner.GetScanByID(uint(id))
	if err != nil {
		NotFound(c, "扫描记录不存在")
		return
	}

	Success(c, scan)
}

// GetScanVulns 镜像漏洞列表
func (h *ImageScansHandler) GetScanVulns(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		BadRequest(c, "无效的 ID")
		return
	}

	vulns, err := h.scanner.GetScanVulns(uint(id))
	if err != nil {
		InternalError(c, "查询镜像漏洞失败")
		return
	}

	Success(c, vulns)
}
