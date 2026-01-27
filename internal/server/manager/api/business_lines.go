// Package api 提供 HTTP API 处理器
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// BusinessLinesHandler 是业务线管理 API 处理器
type BusinessLinesHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewBusinessLinesHandler 创建业务线处理器
func NewBusinessLinesHandler(db *gorm.DB, logger *zap.Logger) *BusinessLinesHandler {
	return &BusinessLinesHandler{
		db:     db,
		logger: logger,
	}
}

// BusinessLineListItem 业务线列表项（包含主机数量）
type BusinessLineListItem struct {
	model.BusinessLine
	HostCount int `json:"host_count"`
}

// ListBusinessLines 获取业务线列表
// GET /api/v1/business-lines
func (h *BusinessLinesHandler) ListBusinessLines(c *gin.Context) {
	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	enabled := c.Query("enabled")
	keyword := c.Query("keyword") // 搜索关键词（名称或代码）

	// 构建查询
	query := h.db.Model(&model.BusinessLine{})

	// 过滤条件
	if enabled != "" {
		if enabled == "true" {
			query = query.Where("enabled = ?", true)
		} else if enabled == "false" {
			query = query.Where("enabled = ?", false)
		}
	}

	// 关键词搜索
	if keyword != "" {
		query = query.Where("name LIKE ? OR code LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	// 计算总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询业务线总数失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询业务线列表失败",
		})
		return
	}

	// 分页查询
	var businessLines []model.BusinessLine
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&businessLines).Error; err != nil {
		h.logger.Error("查询业务线列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询业务线列表失败",
		})
		return
	}

	// 计算每个业务线的主机数量
	items := make([]BusinessLineListItem, 0, len(businessLines))
	for _, bl := range businessLines {
		var hostCount int64
		h.db.Model(&model.Host{}).Where("business_line = ?", bl.Name).Count(&hostCount)

		items = append(items, BusinessLineListItem{
			BusinessLine: bl,
			HostCount:    int(hostCount),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": items,
		},
	})
}

// GetBusinessLine 获取业务线详情
// GET /api/v1/business-lines/:id
func (h *BusinessLinesHandler) GetBusinessLine(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的业务线ID",
		})
		return
	}

	var businessLine model.BusinessLine
	if err := h.db.First(&businessLine, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "业务线不存在",
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

	// 计算主机数量
	var hostCount int64
	h.db.Model(&model.Host{}).Where("business_line = ?", businessLine.Name).Count(&hostCount)
	businessLine.HostCount = int(hostCount)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": businessLine,
	})
}

// CreateBusinessLineRequest 创建业务线请求
type CreateBusinessLineRequest struct {
	Name        string `json:"name" binding:"required"` // 业务线名称
	Code        string `json:"code" binding:"required"` // 业务线代码
	Description string `json:"description"`             // 描述
	Owner       string `json:"owner"`                   // 负责人
	Contact     string `json:"contact"`                 // 联系方式
	Enabled     bool   `json:"enabled"`                 // 是否启用
}

// CreateBusinessLine 创建业务线
// POST /api/v1/business-lines
func (h *BusinessLinesHandler) CreateBusinessLine(c *gin.Context) {
	var req CreateBusinessLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 检查代码是否已存在
	var existing model.BusinessLine
	if err := h.db.Where("code = ?", req.Code).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "业务线代码已存在",
		})
		return
	}

	// 检查名称是否已存在
	if err := h.db.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "业务线名称已存在",
		})
		return
	}

	// 创建业务线
	businessLine := model.BusinessLine{
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		Owner:       req.Owner,
		Contact:     req.Contact,
		Enabled:     req.Enabled,
	}

	if err := h.db.Create(&businessLine).Error; err != nil {
		h.logger.Error("创建业务线失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建业务线失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "创建成功",
		"data":    businessLine,
	})
}

// UpdateBusinessLineRequest 更新业务线请求
type UpdateBusinessLineRequest struct {
	Name        string `json:"name"`        // 业务线名称
	Description string `json:"description"` // 描述
	Owner       string `json:"owner"`       // 负责人
	Contact     string `json:"contact"`     // 联系方式
	Enabled     *bool  `json:"enabled"`     // 是否启用
}

// UpdateBusinessLine 更新业务线
// PUT /api/v1/business-lines/:id
func (h *BusinessLinesHandler) UpdateBusinessLine(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的业务线ID",
		})
		return
	}

	var req UpdateBusinessLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 查询业务线
	var businessLine model.BusinessLine
	if err := h.db.First(&businessLine, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "业务线不存在",
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

	// 如果更新名称，检查是否与其他业务线冲突
	if req.Name != "" && req.Name != businessLine.Name {
		var existing model.BusinessLine
		if err := h.db.Where("name = ? AND id != ?", req.Name, id).First(&existing).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "业务线名称已存在",
			})
			return
		}
		businessLine.Name = req.Name
	}

	// 更新字段
	if req.Description != "" {
		businessLine.Description = req.Description
	}
	if req.Owner != "" {
		businessLine.Owner = req.Owner
	}
	if req.Contact != "" {
		businessLine.Contact = req.Contact
	}
	if req.Enabled != nil {
		businessLine.Enabled = *req.Enabled
	}

	if err := h.db.Save(&businessLine).Error; err != nil {
		h.logger.Error("更新业务线失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新业务线失败",
		})
		return
	}

	// 注意：主机的 business_line 字段存储的是业务线代码（code），而不是名称（name）
	// 由于 code 不可变，因此名称变更不需要同步更新主机

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "更新成功",
		"data":    businessLine,
	})
}

// DeleteBusinessLine 删除业务线
// DELETE /api/v1/business-lines/:id
func (h *BusinessLinesHandler) DeleteBusinessLine(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的业务线ID",
		})
		return
	}

	// 查询业务线
	var businessLine model.BusinessLine
	if err := h.db.First(&businessLine, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "业务线不存在",
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

	// 检查是否有主机关联
	var hostCount int64
	h.db.Model(&model.Host{}).Where("business_line = ?", businessLine.Name).Count(&hostCount)
	if hostCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "该业务线下还有主机，无法删除",
		})
		return
	}

	// 删除业务线
	if err := h.db.Delete(&businessLine).Error; err != nil {
		h.logger.Error("删除业务线失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除业务线失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}
