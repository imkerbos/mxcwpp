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

// AssetsHandler 是资产数据 API 处理器
type AssetsHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAssetsHandler 创建资产处理器
func NewAssetsHandler(db *gorm.DB, logger *zap.Logger) *AssetsHandler {
	return &AssetsHandler{
		db:     db,
		logger: logger,
	}
}

// ListProcesses 获取进程列表
// GET /api/v1/assets/processes
func (h *AssetsHandler) ListProcesses(c *gin.Context) {
	// 解析查询参数
	hostID := c.Query("host_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询
	query := h.db.Model(&model.Process{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	// 获取总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count processes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	// 分页查询
	var processes []model.Process
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&processes).Error; err != nil {
		h.logger.Error("failed to query processes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": processes,
		},
	})
}

// ListPorts 获取端口列表
// GET /api/v1/assets/ports
func (h *AssetsHandler) ListPorts(c *gin.Context) {
	// 解析查询参数
	hostID := c.Query("host_id")
	protocol := c.Query("protocol")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询
	query := h.db.Model(&model.Port{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if protocol != "" {
		query = query.Where("protocol = ?", protocol)
	}

	// 获取总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count ports", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	// 分页查询
	var ports []model.Port
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&ports).Error; err != nil {
		h.logger.Error("failed to query ports", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": ports,
		},
	})
}

// ListUsers 获取账户列表
// GET /api/v1/assets/users
func (h *AssetsHandler) ListUsers(c *gin.Context) {
	// 解析查询参数
	hostID := c.Query("host_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建查询
	query := h.db.Model(&model.AssetUser{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	// 获取总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	// 分页查询
	var users []model.AssetUser
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		h.logger.Error("failed to query users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": users,
		},
	})
}

// ListSoftware 获取软件包列表
// GET /api/v1/assets/software
func (h *AssetsHandler) ListSoftware(c *gin.Context) {
	hostID := c.Query("host_id")
	packageType := c.Query("package_type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Software{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if packageType != "" {
		query = query.Where("package_type = ?", packageType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count software", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var software []model.Software
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&software).Error; err != nil {
		h.logger.Error("failed to query software", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": software,
		},
	})
}

// ListContainers 获取容器列表
// GET /api/v1/assets/containers
func (h *AssetsHandler) ListContainers(c *gin.Context) {
	hostID := c.Query("host_id")
	runtime := c.Query("runtime")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Container{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if runtime != "" {
		query = query.Where("runtime = ?", runtime)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count containers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var containers []model.Container
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&containers).Error; err != nil {
		h.logger.Error("failed to query containers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": containers,
		},
	})
}

// ListApps 获取应用列表
// GET /api/v1/assets/apps
func (h *AssetsHandler) ListApps(c *gin.Context) {
	hostID := c.Query("host_id")
	appType := c.Query("app_type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.App{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if appType != "" {
		query = query.Where("app_type = ?", appType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count apps", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var apps []model.App
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&apps).Error; err != nil {
		h.logger.Error("failed to query apps", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": apps,
		},
	})
}

// ListNetInterfaces 获取网络接口列表
// GET /api/v1/assets/network-interfaces
func (h *AssetsHandler) ListNetInterfaces(c *gin.Context) {
	hostID := c.Query("host_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.NetInterface{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count network interfaces", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var netInterfaces []model.NetInterface
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&netInterfaces).Error; err != nil {
		h.logger.Error("failed to query network interfaces", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": netInterfaces,
		},
	})
}

// ListVolumes 获取磁盘列表
// GET /api/v1/assets/volumes
func (h *AssetsHandler) ListVolumes(c *gin.Context) {
	hostID := c.Query("host_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Volume{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count volumes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var volumes []model.Volume
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&volumes).Error; err != nil {
		h.logger.Error("failed to query volumes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": volumes,
		},
	})
}

// ListKmods 获取内核模块列表
// GET /api/v1/assets/kmods
func (h *AssetsHandler) ListKmods(c *gin.Context) {
	hostID := c.Query("host_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Kmod{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count kernel modules", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var kmods []model.Kmod
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&kmods).Error; err != nil {
		h.logger.Error("failed to query kernel modules", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": kmods,
		},
	})
}

// ListServices 获取系统服务列表
// GET /api/v1/assets/services
func (h *AssetsHandler) ListServices(c *gin.Context) {
	hostID := c.Query("host_id")
	serviceType := c.Query("service_type")
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Service{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if serviceType != "" {
		query = query.Where("service_type = ?", serviceType)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count services", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var services []model.Service
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&services).Error; err != nil {
		h.logger.Error("failed to query services", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": services,
		},
	})
}

// ListCrons 获取定时任务列表
// GET /api/v1/assets/crons
func (h *AssetsHandler) ListCrons(c *gin.Context) {
	hostID := c.Query("host_id")
	user := c.Query("user")
	cronType := c.Query("cron_type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := h.db.Model(&model.Cron{})
	if hostID != "" {
		query = query.Where("host_id = ?", hostID)
	}
	if user != "" {
		query = query.Where("user = ?", user)
	}
	if cronType != "" {
		query = query.Where("cron_type = ?", cronType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count crons", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	var crons []model.Cron
	offset := (page - 1) * pageSize
	if err := query.Order("collected_at DESC").Offset(offset).Limit(pageSize).Find(&crons).Error; err != nil {
		h.logger.Error("failed to query crons", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": crons,
		},
	})
}
