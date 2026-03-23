package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// KubeClusterHandler 集群管理 API Handler
type KubeClusterHandler struct {
	db         *gorm.DB
	logger     *zap.Logger
	kubeClient *biz.KubeClientManager
}

// NewKubeClusterHandler 创建集群管理 Handler
func NewKubeClusterHandler(db *gorm.DB, logger *zap.Logger, kubeClient *biz.KubeClientManager) *KubeClusterHandler {
	return &KubeClusterHandler{
		db:         db,
		logger:     logger,
		kubeClient: kubeClient,
	}
}

// ListClusters 集群列表
func (h *KubeClusterHandler) ListClusters(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	status := c.Query("status")

	query := h.db.Model(&model.KubeCluster{})

	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("查询集群总数失败", zap.Error(err))
		InternalError(c, "查询集群列表失败")
		return
	}

	var clusters []model.KubeCluster
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&clusters).Error; err != nil {
		h.logger.Error("查询集群列表失败", zap.Error(err))
		InternalError(c, "查询集群列表失败")
		return
	}

	// 计算统计信息
	var totalCount, runningCount int64
	var totalNodes, totalPods int
	h.db.Model(&model.KubeCluster{}).Count(&totalCount)
	h.db.Model(&model.KubeCluster{}).Where("status = ?", "running").Count(&runningCount)
	for _, cl := range clusters {
		totalNodes += cl.NodeCount
		totalPods += cl.PodCount
	}
	// 如果有过滤，从全量数据统计 node/pod 总数
	if search != "" || status != "" {
		var allClusters []model.KubeCluster
		h.db.Select("node_count, pod_count").Find(&allClusters)
		totalNodes = 0
		totalPods = 0
		for _, cl := range allClusters {
			totalNodes += cl.NodeCount
			totalPods += cl.PodCount
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": clusters,
			"total": total,
			"stats": gin.H{
				"total":   totalCount,
				"running": runningCount,
				"nodes":   totalNodes,
				"pods":    totalPods,
			},
		},
	})
}

// CreateCluster 接入集群
func (h *KubeClusterHandler) CreateCluster(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		ApiServer  string `json:"apiServer" binding:"required"`
		KubeConfig string `json:"kubeConfig" binding:"required"`
		Remark     string `json:"remark"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求参数错误: "+err.Error())
		return
	}

	// 检查名称唯一性
	var existing model.KubeCluster
	if err := h.db.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		Conflict(c, "集群名称已存在")
		return
	}

	cluster := model.KubeCluster{
		Name:       req.Name,
		ApiServer:  req.ApiServer,
		KubeConfig: req.KubeConfig,
		Status:     model.KubeClusterStatusOffline,
		Remark:     req.Remark,
	}

	// 尝试连接集群获取信息
	clientset, err := h.kubeClient.Connect(req.KubeConfig)
	if err != nil {
		h.logger.Warn("连接 K8s 集群失败，将以离线状态接入", zap.String("name", req.Name), zap.Error(err))
	} else {
		cluster.Status = model.KubeClusterStatusRunning

		// 获取版本信息
		if sv, vErr := clientset.Discovery().ServerVersion(); vErr == nil {
			cluster.Version = sv.GitVersion
		}
	}

	if err := h.db.Create(&cluster).Error; err != nil {
		h.logger.Error("创建集群失败", zap.Error(err))
		InternalError(c, "创建集群失败")
		return
	}

	// 异步更新集群统计（如果已连接）
	if cluster.Status == model.KubeClusterStatusRunning {
		go h.updateClusterStats(cluster.ID)
	}

	h.logger.Info("集群接入成功", zap.String("name", req.Name), zap.Uint("id", cluster.ID))
	SuccessWithMessage(c, "集群接入成功", cluster)
}

// GetCluster 集群详情（含实时 K8s 数据）
func (h *KubeClusterHandler) GetCluster(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	var cluster model.KubeCluster
	if err := h.db.First(&cluster, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "集群不存在")
			return
		}
		h.logger.Error("查询集群失败", zap.Error(err))
		InternalError(c, "查询集群失败")
		return
	}

	// 尝试获取实时数据
	summary := gin.H{
		"nodes":       cluster.NodeCount,
		"pods":        cluster.PodCount,
		"namespaces":  cluster.NamespaceCount,
		"deployments": 0,
		"services":    0,
		"alarms":      0,
	}
	var namespaces []string

	version, nodeCount, podCount, nsCount, deployCount, svcCount, nsList, kErr := h.kubeClient.GetClusterInfo(uint(id))
	if kErr == nil {
		summary["nodes"] = nodeCount
		summary["pods"] = podCount
		summary["namespaces"] = nsCount
		summary["deployments"] = deployCount
		summary["services"] = svcCount
		namespaces = nsList
		if version != "" {
			cluster.Version = version
		}

		// 后台更新 DB 缓存
		go func() {
			h.db.Model(&model.KubeCluster{}).Where("id = ?", id).Updates(map[string]interface{}{
				"node_count":      nodeCount,
				"pod_count":       podCount,
				"namespace_count": nsCount,
				"version":         version,
				"status":          model.KubeClusterStatusRunning,
			})
		}()
	}

	// 查询告警数
	var alarmCount int64
	h.db.Model(&model.KubeAlarm{}).Where("cluster_id = ? AND status = ?", id, "pending").Count(&alarmCount)
	summary["alarms"] = alarmCount

	// 查询风险统计
	var eventCount, baselineFailCount int64
	h.db.Model(&model.KubeEvent{}).Where("cluster_id = ?", id).Count(&eventCount)
	h.db.Model(&model.KubeBaseline{}).Where("cluster_id = ? AND result = ?", id, "fail").Count(&baselineFailCount)

	result := gin.H{
		"id":            cluster.ID,
		"name":          cluster.Name,
		"apiServer":     cluster.ApiServer,
		"status":        cluster.Status,
		"version":       cluster.Version,
		"healthScore":   cluster.HealthScore,
		"remark":        cluster.Remark,
		"createdAt":     cluster.CreatedAt,
		"updatedAt":     cluster.UpdatedAt,
		"uptime":        "",
		"lastHeartbeat": cluster.UpdatedAt,
		"summary":       summary,
		"namespaces":    namespaces,
		"risks": gin.H{
			"alarms":   alarmCount,
			"events":   eventCount,
			"baseline": baselineFailCount,
		},
	}

	Success(c, result)
}

// UpdateCluster 更新集群
func (h *KubeClusterHandler) UpdateCluster(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	var cluster model.KubeCluster
	if err := h.db.First(&cluster, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "集群不存在")
			return
		}
		h.logger.Error("查询集群失败", zap.Error(err))
		InternalError(c, "查询集群失败")
		return
	}

	var req struct {
		Name       string `json:"name"`
		ApiServer  string `json:"apiServer"`
		KubeConfig string `json:"kubeConfig"`
		Remark     string `json:"remark"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求参数错误: "+err.Error())
		return
	}

	// 名称唯一性检查
	if req.Name != "" && req.Name != cluster.Name {
		var existing model.KubeCluster
		if err := h.db.Where("name = ? AND id != ?", req.Name, id).First(&existing).Error; err == nil {
			Conflict(c, "集群名称已存在")
			return
		}
		cluster.Name = req.Name
	}

	if req.ApiServer != "" {
		cluster.ApiServer = req.ApiServer
	}
	if req.Remark != "" {
		cluster.Remark = req.Remark
	}

	// 如果更新了 KubeConfig，重新验证连接
	if req.KubeConfig != "" {
		cluster.KubeConfig = req.KubeConfig
		h.kubeClient.RemoveClient(uint(id))

		if _, cErr := h.kubeClient.Connect(req.KubeConfig); cErr != nil {
			h.logger.Warn("新 KubeConfig 连接失败", zap.Error(cErr))
			cluster.Status = model.KubeClusterStatusOffline
		} else {
			cluster.Status = model.KubeClusterStatusRunning
		}
	}

	if err := h.db.Save(&cluster).Error; err != nil {
		h.logger.Error("更新集群失败", zap.Error(err))
		InternalError(c, "更新集群失败")
		return
	}

	// 更新关联的告警/事件/基线中的集群名称
	if req.Name != "" {
		go func() {
			h.db.Model(&model.KubeAlarm{}).Where("cluster_id = ?", id).Update("cluster_name", cluster.Name)
			h.db.Model(&model.KubeEvent{}).Where("cluster_id = ?", id).Update("cluster_name", cluster.Name)
			h.db.Model(&model.KubeBaseline{}).Where("cluster_id = ?", id).Update("cluster_name", cluster.Name)
			h.db.Model(&model.KubeWhitelist{}).Where("cluster_id = ?", id).Update("cluster_name", cluster.Name)
		}()
	}

	SuccessWithMessage(c, "集群已更新", cluster)
}

// DeleteCluster 删除集群
func (h *KubeClusterHandler) DeleteCluster(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	var cluster model.KubeCluster
	if err := h.db.First(&cluster, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			NotFound(c, "集群不存在")
			return
		}
		h.logger.Error("查询集群失败", zap.Error(err))
		InternalError(c, "查询集群失败")
		return
	}

	// 事务删除集群和相关数据
	txErr := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("cluster_id = ?", id).Delete(&model.KubeAlarm{}).Error; err != nil {
			return err
		}
		if err := tx.Where("cluster_id = ?", id).Delete(&model.KubeEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("cluster_id = ?", id).Delete(&model.KubeBaseline{}).Error; err != nil {
			return err
		}
		if err := tx.Where("cluster_id = ?", id).Delete(&model.KubeWhitelist{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&cluster).Error; err != nil {
			return err
		}
		return nil
	})

	if txErr != nil {
		h.logger.Error("删除集群失败", zap.Error(txErr))
		InternalError(c, "删除集群失败")
		return
	}

	h.kubeClient.RemoveClient(uint(id))
	h.logger.Info("集群已删除", zap.String("name", cluster.Name), zap.Uint("id", cluster.ID))
	SuccessMessage(c, "集群已移除")
}

// GetClusterNodes Node 列表（实时查 K8s API）
func (h *KubeClusterHandler) GetClusterNodes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	nodes, err := h.kubeClient.GetNodes(uint(id))
	if err != nil {
		h.logger.Error("查询节点列表失败", zap.Uint64("cluster_id", id), zap.Error(err))
		InternalError(c, "查询节点列表失败: "+err.Error())
		return
	}

	Success(c, gin.H{"items": nodes})
}

// GetClusterPods Pod 列表（实时查 K8s API，支持分页和过滤）
func (h *KubeClusterHandler) GetClusterPods(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	namespace := c.Query("namespace")
	search := c.Query("search")
	status := c.Query("status")

	pods, total, pErr := h.kubeClient.GetPods(uint(id), namespace, search, status, page, pageSize)
	if pErr != nil {
		h.logger.Error("查询 Pod 列表失败", zap.Uint64("cluster_id", id), zap.Error(pErr))
		InternalError(c, "查询 Pod 列表失败: "+pErr.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"items": pods,
			"total": total,
		},
	})
}

// GetClusterWorkloads Workload 列表（实时查 K8s API）
func (h *KubeClusterHandler) GetClusterWorkloads(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		BadRequest(c, "无效的集群 ID")
		return
	}

	workloads, wErr := h.kubeClient.GetWorkloads(uint(id))
	if wErr != nil {
		h.logger.Error("查询 Workload 列表失败", zap.Uint64("cluster_id", id), zap.Error(wErr))
		InternalError(c, "查询 Workload 列表失败: "+wErr.Error())
		return
	}

	Success(c, gin.H{"items": workloads})
}

// updateClusterStats 后台更新集群统计信息
func (h *KubeClusterHandler) updateClusterStats(clusterID uint) {
	version, nodeCount, podCount, nsCount, _, _, _, err := h.kubeClient.GetClusterInfo(clusterID)
	if err != nil {
		h.logger.Debug("更新集群统计失败", zap.Uint("cluster_id", clusterID), zap.Error(err))
		return
	}

	h.db.Model(&model.KubeCluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
		"version":         version,
		"node_count":      nodeCount,
		"pod_count":       podCount,
		"namespace_count": nsCount,
	})
}
