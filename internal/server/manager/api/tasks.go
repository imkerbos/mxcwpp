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

	"github.com/imkerbos/mxsec-platform/internal/server/agentcenter/service"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
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
	Name      string                 `json:"name" binding:"required"`
	Type      string                 `json:"type" binding:"required"`
	Targets   map[string]interface{} `json:"targets" binding:"required"`
	PolicyID  string                 `json:"policy_id"`  // 兼容旧版本：单策略
	PolicyIDs []string               `json:"policy_ids"` // 新版本：多策略
	RuleIDs   []string               `json:"rule_ids"`
	Schedule  map[string]interface{} `json:"schedule"`
}

// TaskResponse 任务响应（包含计算字段）
type TaskResponse struct {
	model.ScanTask
	TargetHosts        []string `json:"target_hosts"`         // 目标主机 ID 列表
	MatchedHostCount   int      `json:"matched_host_count"`   // 匹配的主机数量（在线）
	TotalHostCount     int      `json:"total_host_count"`     // 总目标主机数量（包括离线）
	TotalRuleCount     int      `json:"total_rule_count"`     // 关联策略的规则总数
	ExpectedCheckCount int      `json:"expected_check_count"` // 预期检查项总数（在线主机数 × 规则数）
}

// enrichTaskWithTargetHosts 为任务添加目标主机信息
func (h *TasksHandler) enrichTaskWithTargetHosts(task *model.ScanTask) *TaskResponse {
	response := &TaskResponse{
		ScanTask:    *task,
		TargetHosts: []string{},
	}

	var hosts []model.Host
	var totalHosts []model.Host

	// 构建运行时类型筛选条件
	runtimeType := task.TargetConfig.RuntimeType
	baseQuery := h.db.Model(&model.Host{})
	onlineQuery := h.db.Model(&model.Host{}).Where("status = ?", model.HostStatusOnline)

	// 如果指定了运行时类型，添加筛选条件
	if runtimeType != "" {
		if runtimeType == model.RuntimeTypeVM {
			// 虚拟机：runtime_type = 'vm' 或为空（兼容旧数据）
			baseQuery = baseQuery.Where("(runtime_type = ? OR runtime_type = '' OR runtime_type IS NULL)", model.RuntimeTypeVM)
			onlineQuery = onlineQuery.Where("(runtime_type = ? OR runtime_type = '' OR runtime_type IS NULL)", model.RuntimeTypeVM)
		} else {
			baseQuery = baseQuery.Where("runtime_type = ?", runtimeType)
			onlineQuery = onlineQuery.Where("runtime_type = ?", runtimeType)
		}
	}

	switch task.TargetType {
	case model.TargetTypeAll:
		// 查询所有主机
		baseQuery.Find(&totalHosts)
		onlineQuery.Find(&hosts)
		for _, host := range totalHosts {
			response.TargetHosts = append(response.TargetHosts, host.HostID)
		}

	case model.TargetTypeHostIDs:
		// 使用指定的主机 ID
		if len(task.TargetConfig.HostIDs) > 0 {
			response.TargetHosts = task.TargetConfig.HostIDs
			baseQuery.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&totalHosts)
			onlineQuery.Where("host_id IN ?", task.TargetConfig.HostIDs).Find(&hosts)
		}

	case model.TargetTypeOSFamily:
		// 查询指定 OS 系列的主机
		if len(task.TargetConfig.OSFamily) > 0 {
			baseQuery.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&totalHosts)
			onlineQuery.Where("os_family IN ?", task.TargetConfig.OSFamily).Find(&hosts)
			for _, host := range totalHosts {
				response.TargetHosts = append(response.TargetHosts, host.HostID)
			}
		}
	}

	response.MatchedHostCount = len(hosts)
	response.TotalHostCount = len(totalHosts)

	// 计算关联策略的规则总数
	policyIDs := task.GetPolicyIDs()
	if len(policyIDs) > 0 {
		var ruleCount int64
		h.db.Model(&model.Rule{}).Where("policy_id IN ? AND enabled = ?", policyIDs, true).Count(&ruleCount)
		response.TotalRuleCount = int(ruleCount)
		// 预期检查项总数 = 在线主机数 × 规则数
		response.ExpectedCheckCount = response.MatchedHostCount * response.TotalRuleCount
	}

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

	// 获取策略ID列表（兼容新旧版本）
	policyIDs := req.PolicyIDs
	if len(policyIDs) == 0 && req.PolicyID != "" {
		policyIDs = []string{req.PolicyID}
	}
	if len(policyIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请至少指定一个策略 (policy_id 或 policy_ids)",
		})
		return
	}

	// 验证所有策略是否存在
	for _, policyID := range policyIDs {
		_, err := h.policyService.GetPolicy(policyID)
		if err != nil {
			if strings.Contains(err.Error(), "不存在") {
				c.JSON(http.StatusNotFound, gin.H{
					"code":    404,
					"message": "策略不存在: " + policyID,
				})
				return
			}
			h.logger.Error("查询策略失败", zap.String("policy_id", policyID), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询策略失败",
			})
			return
		}
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

	// 解析运行时类型（可选）
	if runtimeType, ok := req.Targets["runtime_type"].(string); ok && runtimeType != "" {
		targetConfig.RuntimeType = model.RuntimeType(runtimeType)
	}

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

	// 创建任务（状态为 created，等待用户确认执行）
	task := &model.ScanTask{
		TaskID:       uuid.New().String(),
		Name:         req.Name,
		Type:         model.TaskType(req.Type),
		TargetType:   model.TargetType(targetType),
		TargetConfig: targetConfig,
		PolicyID:     policyIDs[0],                 // 兼容旧版本
		PolicyIDs:    model.StringArray(policyIDs), // 新版本多策略
		RuleIDs:      model.StringArray(req.RuleIDs),
		Status:       model.TaskStatusCreated,
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

	// created 或其他状态的任务都可以执行
	// 重置任务状态为 pending，等待调度器处理
	// 设置 executed_at 为执行请求时间（用于计算超时）
	now := time.Now()
	localNow := model.LocalTime(now)
	if err := h.db.Model(&task).Updates(map[string]interface{}{
		"status":                model.TaskStatusPending,
		"executed_at":           &localNow,
		"dispatched_host_count": 0,
		"completed_host_count":  0,
		"failed_reason":         "",
		"updated_at":            now,
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

	// 检查任务状态，只有 created、pending 或 running 状态的任务可以取消
	if task.Status != model.TaskStatusCreated && task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "任务状态为 " + string(task.Status) + "，无法取消",
		})
		return
	}

	// 更新任务状态为 cancelled
	now := time.Now()
	if err := h.db.Model(&task).Updates(map[string]interface{}{
		"status":       model.TaskStatusCancelled,
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
