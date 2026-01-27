package api

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/agentcenter/service"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// FixHandler 是基线修复 API 处理器
type FixHandler struct {
	db            *gorm.DB
	logger        *zap.Logger
	taskService   *service.TaskService
	policyService *service.PolicyService
}

// NewFixHandler 创建修复处理器
func NewFixHandler(db *gorm.DB, logger *zap.Logger) *FixHandler {
	return &FixHandler{
		db:            db,
		logger:        logger,
		taskService:   service.NewTaskService(db, logger),
		policyService: service.NewPolicyService(db, logger),
	}
}

// FixableItemResponse 可修复项响应
type FixableItemResponse struct {
	ResultID      string `json:"result_id"`
	HostID        string `json:"host_id"`
	Hostname      string `json:"hostname"`
	IP            string `json:"ip"`
	BusinessLine  string `json:"business_line"`
	RuleID        string `json:"rule_id"`
	Title         string `json:"title"`
	Category      string `json:"category"`
	Severity      string `json:"severity"`
	FixSuggestion string `json:"fix_suggestion"`
	FixCommand    string `json:"fix_command"`
	Actual        string `json:"actual"`
	Expected      string `json:"expected"`
	HasFix        bool   `json:"has_fix"`
}

// GetFixableItems 获取可修复项列表
func (h *FixHandler) GetFixableItems(c *gin.Context) {
	// 解析查询参数
	hostIDsStr := c.QueryArray("host_ids[]")
	severitiesStr := c.QueryArray("severities[]")
	businessLine := c.Query("business_line")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}

	// 构建查询
	query := h.db.Model(&model.ScanResult{}).
		Where("scan_results.status IN ?", []string{"fail", "error"})

	// 主机筛选
	if len(hostIDsStr) > 0 {
		query = query.Where("scan_results.host_id IN ?", hostIDsStr)
	}

	// 严重级别筛选
	if len(severitiesStr) > 0 {
		query = query.Where("scan_results.severity IN ?", severitiesStr)
	}

	// 业务线筛选：需要通过 JOIN hosts 表来筛选
	if businessLine != "" {
		query = query.Joins("JOIN hosts ON scan_results.host_id = hosts.host_id").
			Where("hosts.business_line = ?", businessLine)
	}

	// 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询可修复项总数失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 分页查询
	var results []model.ScanResult
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).
		Order("severity DESC, checked_at DESC").
		Find(&results).Error; err != nil {
		h.logger.Error("查询可修复项失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 获取主机信息
	hostIDs := make([]string, 0, len(results))
	for _, r := range results {
		hostIDs = append(hostIDs, r.HostID)
	}
	var hosts []model.Host
	if len(hostIDs) > 0 {
		h.db.Where("host_id IN ?", hostIDs).Find(&hosts)
	}
	type HostInfo struct {
		Hostname     string
		IP           string
		BusinessLine string
	}
	hostMap := make(map[string]HostInfo)
	for _, host := range hosts {
		ip := ""
		if len(host.IPv4) > 0 {
			ip = host.IPv4[0] // 使用第一个 IPv4 地址
		}
		hostMap[host.HostID] = HostInfo{
			Hostname:     host.Hostname,
			IP:           ip,
			BusinessLine: host.BusinessLine,
		}
	}

	// 获取规则信息（包含修复命令）
	ruleIDs := make([]string, 0, len(results))
	for _, r := range results {
		ruleIDs = append(ruleIDs, r.RuleID)
	}
	var rules []model.Rule
	if len(ruleIDs) > 0 {
		h.db.Where("rule_id IN ?", ruleIDs).Find(&rules)
	}
	ruleMap := make(map[string]*model.Rule)
	for i := range rules {
		ruleMap[rules[i].RuleID] = &rules[i]
	}

	// 构建响应
	items := make([]FixableItemResponse, 0, len(results))
	for _, r := range results {
		rule := ruleMap[r.RuleID]
		hostInfo := hostMap[r.HostID]
		item := FixableItemResponse{
			ResultID:      r.ResultID,
			HostID:        r.HostID,
			Hostname:      hostInfo.Hostname,
			IP:            hostInfo.IP,
			BusinessLine:  hostInfo.BusinessLine,
			RuleID:        r.RuleID,
			Title:         r.Title,
			Category:      r.Category,
			Severity:      r.Severity,
			FixSuggestion: r.FixSuggestion,
			Actual:        r.Actual,
			Expected:      r.Expected,
			HasFix:        false,
		}

		// 检查是否有修复命令
		if rule != nil && rule.FixConfig.Command != "" {
			item.HasFix = true
			item.FixCommand = rule.FixConfig.Command
		}

		items = append(items, item)
	}

	Success(c, gin.H{
		"items": items,
		"total": total,
	})
}

// CreateFixTaskRequest 创建修复任务请求
type CreateFixTaskRequest struct {
	HostIDs    []string `json:"host_ids" binding:"required"`
	RuleIDs    []string `json:"rule_ids" binding:"required"`
	Severities []string `json:"severities"`
}

// CreateFixTask 创建修复任务
func (h *FixHandler) CreateFixTask(c *gin.Context) {
	var req CreateFixTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数错误: "+err.Error())
		return
	}

	// 验证主机和规则
	if len(req.HostIDs) == 0 {
		BadRequest(c, "主机列表不能为空")
		return
	}
	if len(req.RuleIDs) == 0 {
		BadRequest(c, "规则列表不能为空")
		return
	}

	// 创建任务
	taskID := uuid.New().String()

	// 查询实际需要修复的项数（只统计失败的记录）
	var actualCount int64
	h.db.Model(&model.ScanResult{}).
		Where("host_id IN ?", req.HostIDs).
		Where("rule_id IN ?", req.RuleIDs).
		Where("status IN ?", []string{"fail", "error"}).
		Count(&actualCount)

	// 如果没有查询到失败记录，使用主机数×规则数作为默认值
	totalCount := int(actualCount)
	if totalCount == 0 {
		totalCount = len(req.HostIDs) * len(req.RuleIDs)
	}

	task := &model.FixTask{
		TaskID:       taskID,
		HostIDs:      req.HostIDs,
		RuleIDs:      req.RuleIDs,
		Severities:   req.Severities,
		Status:       model.FixTaskStatusPending,
		TotalCount:   totalCount,
		SuccessCount: 0,
		FailedCount:  0,
		Progress:     0,
		CreatedBy:    c.GetString("user_id"),
		CreatedAt:    model.Now(),
	}

	if err := h.db.Create(task).Error; err != nil {
		h.logger.Error("创建修复任务失败", zap.Error(err))
		InternalError(c, "创建任务失败")
		return
	}

	// 修复任务将由调度器自动分发到 Agent
	// 调度器会定期检查 pending 状态的修复任务并下发

	h.logger.Info("创建修复任务成功",
		zap.String("task_id", taskID),
		zap.Int("host_count", len(req.HostIDs)),
		zap.Int("rule_count", len(req.RuleIDs)))

	Success(c, gin.H{
		"task_id": taskID,
	})
}

// GetFixTask 获取修复任务详情
func (h *FixHandler) GetFixTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		BadRequest(c, "任务ID不能为空")
		return
	}

	var task model.FixTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "任务不存在")
			return
		}
		h.logger.Error("查询修复任务失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	Success(c, task)
}

// ListFixTasks 获取修复任务列表
func (h *FixHandler) ListFixTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}

	query := h.db.Model(&model.FixTask{})

	// 状态筛选
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询修复任务总数失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 分页查询
	var tasks []model.FixTask
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		h.logger.Error("查询修复任务列表失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	Success(c, gin.H{
		"items": tasks,
		"total": total,
	})
}

// FixResultResponse 修复结果响应
type FixResultResponse struct {
	model.FixResult
	Hostname string `json:"hostname"`
	Title    string `json:"title"`
}

// GetFixResults 获取修复结果
func (h *FixHandler) GetFixResults(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		BadRequest(c, "任务ID不能为空")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}

	query := h.db.Model(&model.FixResult{}).Where("task_id = ?", taskID)

	// 状态筛选
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询修复结果总数失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 分页查询
	var results []model.FixResult
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).
		Order("fixed_at DESC").
		Find(&results).Error; err != nil {
		h.logger.Error("查询修复结果失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 获取主机信息
	hostIDs := make([]string, 0, len(results))
	for _, r := range results {
		hostIDs = append(hostIDs, r.HostID)
	}
	var hosts []model.Host
	if len(hostIDs) > 0 {
		h.db.Where("host_id IN ?", hostIDs).Find(&hosts)
	}
	hostMap := make(map[string]string)
	for _, host := range hosts {
		hostMap[host.HostID] = host.Hostname
	}

	// 获取规则标题
	ruleIDs := make([]string, 0, len(results))
	for _, r := range results {
		ruleIDs = append(ruleIDs, r.RuleID)
	}
	var rules []model.Rule
	if len(ruleIDs) > 0 {
		h.db.Where("rule_id IN ?", ruleIDs).Find(&rules)
	}
	ruleMap := make(map[string]string)
	for _, rule := range rules {
		ruleMap[rule.RuleID] = rule.Title
	}

	// 构建响应
	items := make([]FixResultResponse, 0, len(results))
	for _, r := range results {
		items = append(items, FixResultResponse{
			FixResult: r,
			Hostname:  hostMap[r.HostID],
			Title:     ruleMap[r.RuleID],
		})
	}

	Success(c, gin.H{
		"items": items,
		"total": total,
	})
}

// CancelFixTask 取消修复任务
func (h *FixHandler) CancelFixTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		BadRequest(c, "任务ID不能为空")
		return
	}

	var task model.FixTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "任务不存在")
			return
		}
		h.logger.Error("查询修复任务失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 只能取消待执行或执行中的任务
	if task.Status != model.FixTaskStatusPending && task.Status != model.FixTaskStatusRunning {
		BadRequest(c, fmt.Sprintf("任务状态为 %s，无法取消", task.Status))
		return
	}

	// TODO: 通知 Agent 取消任务

	// 更新任务状态
	if err := h.db.Model(&task).Updates(map[string]interface{}{
		"status":       model.FixTaskStatusFailed,
		"completed_at": model.Now(),
	}).Error; err != nil {
		h.logger.Error("取消修复任务失败", zap.Error(err))
		InternalError(c, "取消任务失败")
		return
	}

	h.logger.Info("取消修复任务成功", zap.String("task_id", taskID))
	Success(c, nil)
}

// DeleteFixTask 删除修复任务
func (h *FixHandler) DeleteFixTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		BadRequest(c, "任务ID不能为空")
		return
	}

	var task model.FixTask
	if err := h.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "任务不存在")
			return
		}
		h.logger.Error("查询修复任务失败", zap.Error(err))
		InternalError(c, "查询失败")
		return
	}

	// 只能删除已完成或失败的任务
	if task.Status == model.FixTaskStatusRunning {
		BadRequest(c, "执行中的任务无法删除")
		return
	}

	// 删除任务和相关结果
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		// 删除修复结果
		if err := tx.Where("task_id = ?", taskID).Delete(&model.FixResult{}).Error; err != nil {
			return err
		}
		// 删除任务
		if err := tx.Delete(&task).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		h.logger.Error("删除修复任务失败", zap.Error(err))
		InternalError(c, "删除任务失败")
		return
	}

	h.logger.Info("删除修复任务成功", zap.String("task_id", taskID))
	Success(c, nil)
}
