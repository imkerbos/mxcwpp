// Package api 提供 HTTP API 处理器
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/mxcsec-platform/mxcsec-platform/internal/server/agentcenter/service"
	"github.com/mxcsec-platform/mxcsec-platform/internal/server/model"
)

// TasksHandler 是任务管理 API 处理器
type TasksHandler struct {
	taskService   *service.TaskService
	policyService *service.PolicyService
	db            *gorm.DB
	logger        *zap.Logger
}

// NewTasksHandler 创建任务处理器
func NewTasksHandler(db *gorm.DB, logger *zap.Logger) *TasksHandler {
	return &TasksHandler{
		taskService:   service.NewTaskService(db, logger),
		policyService: service.NewPolicyService(db, logger),
		db:            db,
		logger:        logger,
	}
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name     string                 `json:"name" binding:"required"`
	Type     string                 `json:"type" binding:"required"`
	Targets  map[string]interface{} `json:"targets" binding:"required"`
	PolicyID string                 `json:"policy_id" binding:"required"`
	RuleIDs  []string               `json:"rule_ids"`
	Schedule map[string]interface{} `json:"schedule"`
}

// TaskResponse 任务响应（包含计算字段）
type TaskResponse struct {
	model.ScanTask
	TargetHosts      []string `json:"target_hosts"`       // 目标主机 ID 列表
	MatchedHostCount int      `json:"matched_host_count"` // 匹配的主机数量（在线）
	TotalHostCount   int      `json:"total_host_count"`   // 总目标主机数量（包括离线）
}

// enrichTaskWithTargetHosts 为任务添加目标主机信息
func (h *TasksHandler) enrichTaskWithTargetHosts(task *model.ScanTask) *TaskResponse {
	response := &TaskResponse{
		ScanTask:    *task,
		TargetHosts: []string{},
	}

	var hosts []model.Host
	var totalHosts []model.Host

	switch task.TargetType {
	case model.TargetTypeAll:
		// 查询所有主机
		h.db.Find(&totalHosts)
		h.db.Where("status = ?", model.HostStatusOnline).Find(&hosts)
		for _, host := range totalHosts {
			response.TargetHosts = append(response.TargetHosts, host.HostID)
		}

	case model.TargetTypeHostIDs:
		// 使用指定的主机 ID
		if len(task.TargetConfig.HostIDs) > 0 {
			response.TargetHosts = task.TargetConfig.HostIDs
			h.db.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&totalHosts)
			h.db.Where("host_id IN ? AND status = ?", task.TargetConfig.HostIDs, model.HostStatusOnline).Find(&hosts)
		}

	case model.TargetTypeOSFamily:
		// 查询指定 OS 系列的主机
		if len(task.TargetConfig.OSFamily) > 0 {
			h.db.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&totalHosts)
			h.db.Where("os_family IN ? AND status = ?", task.TargetConfig.OSFamily, model.HostStatusOnline).Find(&hosts)
			for _, host := range totalHosts {
				response.TargetHosts = append(response.TargetHosts, host.HostID)
			}
		}
	}

	response.MatchedHostCount = len(hosts)
	response.TotalHostCount = len(totalHosts)

	return response
}

// CreateTask 创建扫描任务
// POST /api/v1/tasks
func (h *TasksHandler) CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 验证策略是否存在
	_, err := h.policyService.GetPolicy(req.PolicyID)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "策略不存在",
			})
			return
		}
		h.logger.Error("查询策略失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询策略失败",
		})
		return
	}

	// 解析目标配置
	targetType, ok := req.Targets["type"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "targets.type 必须为字符串",
		})
		return
	}

	var targetConfig model.TargetConfig
	switch targetType {
	case "all":
		// 不需要额外配置
	case "host_ids":
		hostIDsInterface, ok := req.Targets["host_ids"].([]interface{})
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "targets.host_ids 必须为数组",
			})
			return
		}
		hostIDs := make([]string, 0, len(hostIDsInterface))
		for _, id := range hostIDsInterface {
			if idStr, ok := id.(string); ok {
				hostIDs = append(hostIDs, idStr)
			}
		}
		targetConfig.HostIDs = hostIDs
	case "os_family":
		osFamilyInterface, ok := req.Targets["os_family"].([]interface{})
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "targets.os_family 必须为数组",
			})
			return
		}
		osFamily := make([]string, 0, len(osFamilyInterface))
		for _, os := range osFamilyInterface {
			if osStr, ok := os.(string); ok {
				osFamily = append(osFamily, osStr)
			}
		}
		targetConfig.OSFamily = osFamily
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的 target_type: " + targetType,
		})
		return
	}

	// 创建任务
	task := &model.ScanTask{
		TaskID:       uuid.New().String(),
		Name:         req.Name,
		Type:         model.TaskType(req.Type),
		TargetType:   model.TargetType(targetType),
		TargetConfig: targetConfig,
		PolicyID:     req.PolicyID,
		RuleIDs:      model.StringArray(req.RuleIDs),
		Status:       model.TaskStatusPending,
	}

	if err := h.db.Create(task).Error; err != nil {
		h.logger.Error("创建任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建任务失败",
		})
		return
	}

	h.logger.Info("任务已创建", zap.String("task_id", task.TaskID))

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"data": h.enrichTaskWithTargetHosts(task),
	})
}

// ListTasks 获取任务列表
// GET /api/v1/tasks
func (h *TasksHandler) ListTasks(c *gin.Context) {
	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	policyID := c.Query("policy_id")

	// 构建查询
	query := h.db.Model(&model.ScanTask{})

	// 过滤条件
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if policyID != "" {
		query = query.Where("policy_id = ?", policyID)
	}

	// 计算总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询任务总数失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务列表失败",
		})
		return
	}

	// 分页查询
	var tasks []model.ScanTask
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&tasks).Error; err != nil {
		h.logger.Error("查询任务列表失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务列表失败",
		})
		return
	}

	// 为每个任务添加目标主机信息
	enrichedTasks := make([]*TaskResponse, len(tasks))
	for i := range tasks {
		enrichedTasks[i] = h.enrichTaskWithTargetHosts(&tasks[i])
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total": total,
			"items": enrichedTasks,
		},
	})
}

// GetTask 获取任务详情
// GET /api/v1/tasks/:task_id
func (h *TasksHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")

	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "任务不存在",
			})
			return
		}
		h.logger.Error("查询任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": h.enrichTaskWithTargetHosts(&task),
	})
}

// RunTask 执行任务
// POST /api/v1/tasks/:task_id/run
func (h *TasksHandler) RunTask(c *gin.Context) {
	taskID := c.Param("task_id")

	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "任务不存在",
			})
			return
		}
		h.logger.Error("查询任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务失败",
		})
		return
	}

	// 检查任务状态
	if task.Status == model.TaskStatusRunning {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "任务正在执行中，无法重复执行",
		})
		return
	}

	// 重置任务状态为 pending，等待调度器处理
	now := time.Now()
	if err := h.db.Model(&task).Updates(map[string]interface{}{
		"status":      model.TaskStatusPending,
		"executed_at": nil,
		"updated_at":  now,
	}).Error; err != nil {
		h.logger.Error("更新任务状态失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新任务状态失败",
		})
		return
	}

	h.logger.Info("任务已标记为待执行", zap.String("task_id", taskID))

	// 重新查询更新后的任务
	h.db.Where("task_id = ?", taskID).First(&task)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "任务已标记为待执行，等待调度器处理",
		"data":    h.enrichTaskWithTargetHosts(&task),
	})
}

// CancelTask 取消任务
// POST /api/v1/tasks/:task_id/cancel
func (h *TasksHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("task_id")

	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "任务不存在",
			})
			return
		}
		h.logger.Error("查询任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务失败",
		})
		return
	}

	// 检查任务状态，只有 pending 或 running 状态的任务可以取消
	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "任务状态为 " + string(task.Status) + "，无法取消",
		})
		return
	}

	// 更新任务状态为 failed（取消视为失败）
	now := time.Now()
	if err := h.db.Model(&task).Updates(map[string]interface{}{
		"status":       model.TaskStatusFailed,
		"completed_at": now,
		"updated_at":   now,
	}).Error; err != nil {
		h.logger.Error("取消任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "取消任务失败",
		})
		return
	}

	h.logger.Info("任务已取消", zap.String("task_id", taskID))

	// 重新查询更新后的任务
	h.db.Where("task_id = ?", taskID).First(&task)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "任务已取消",
		"data":    h.enrichTaskWithTargetHosts(&task),
	})
}

// DeleteTask 删除任务
// DELETE /api/v1/tasks/:task_id
func (h *TasksHandler) DeleteTask(c *gin.Context) {
	taskID := c.Param("task_id")

	var task model.ScanTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "任务不存在",
			})
			return
		}
		h.logger.Error("查询任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询任务失败",
		})
		return
	}

	// 检查任务状态，running 状态的任务不能删除
	if task.Status == model.TaskStatusRunning {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "任务正在执行中，无法删除",
		})
		return
	}

	// 删除任务
	if err := h.db.Delete(&task).Error; err != nil {
		h.logger.Error("删除任务失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除任务失败",
		})
		return
	}

	h.logger.Info("任务已删除", zap.String("task_id", taskID))

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "任务已删除",
	})
}
