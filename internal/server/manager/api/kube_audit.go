package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/manager/biz"
	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// KubeAuditHandler K8s Audit Webhook 接收端
type KubeAuditHandler struct {
	db           *gorm.DB
	logger       *zap.Logger
	alarmService *biz.KubeAlarmService
	detector     *biz.KubeDetector
}

// NewKubeAuditHandler 创建 Audit Webhook Handler
func NewKubeAuditHandler(db *gorm.DB, logger *zap.Logger, alarmService *biz.KubeAlarmService) *KubeAuditHandler {
	return &KubeAuditHandler{
		db:           db,
		logger:       logger,
		alarmService: alarmService,
		detector:     biz.NewKubeDetector(db, logger, alarmService),
	}
}

// AuditEvent K8s Audit Event 简化结构
type AuditEvent = model.AuditEvent

// AuditUser Audit 事件中的用户信息
type AuditUser = model.AuditUser

// AuditObjectRef Audit 事件中的对象引用
type AuditObjectRef = model.AuditObjectRef

// AuditEventList K8s Audit EventList
type AuditEventList = model.AuditEventList

// ReceiveAuditWebhook 接收 K8s apiserver 的 audit webhook 回调
func (h *KubeAuditHandler) ReceiveAuditWebhook(c *gin.Context) {
	token := c.Param("cluster_token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	// 通过 token 查找集群
	var cluster model.KubeCluster
	if err := h.db.Where("audit_token = ?", token).First(&cluster).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		h.logger.Error("查询集群失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 10<<20)) // 10MB limit
	if err != nil {
		h.logger.Error("读取 audit webhook body 失败", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	var eventList AuditEventList
	if err := json.Unmarshal(body, &eventList); err != nil {
		// 尝试解析为单个事件
		var single AuditEvent
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			h.logger.Error("解析 audit event 失败", zap.Error(err))
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit event"})
			return
		}
		eventList.Items = []AuditEvent{single}
	}

	go h.processAuditEvents(cluster, eventList.Items)

	c.JSON(http.StatusOK, gin.H{"received": len(eventList.Items)})
}

// processAuditEvents 异步处理 audit 事件
func (h *KubeAuditHandler) processAuditEvents(cluster model.KubeCluster, events []AuditEvent) {
	for _, event := range events {
		// 只处理 ResponseComplete 阶段，避免重复
		if event.Stage != "" && event.Stage != "ResponseComplete" {
			continue
		}

		rawData, _ := json.Marshal(event)

		kubeEvent := model.KubeEvent{
			ClusterID:   cluster.ID,
			ClusterName: cluster.Name,
			EventType:   "audit",
			Severity:    h.classifyAuditSeverity(&event),
			Title:       h.buildAuditTitle(&event),
			Message:     h.buildAuditMessage(&event),
			RawData:     model.RawJSON(rawData),
			Status:      model.KubeEventStatusUnhandled,
		}

		if event.ObjectRef != nil {
			kubeEvent.Namespace = event.ObjectRef.Namespace
		}
		if len(event.SourceIPs) > 0 {
			kubeEvent.SourceIP = event.SourceIPs[0]
		}

		if err := h.db.Create(&kubeEvent).Error; err != nil {
			h.logger.Error("保存 audit event 失败", zap.Error(err))
		}

		// 规则引擎检测
		h.detector.DetectAuditEvent(cluster.ID, cluster.Name, &event)
	}
}

func (h *KubeAuditHandler) classifyAuditSeverity(event *AuditEvent) string {
	if event.ObjectRef == nil {
		return "info"
	}
	// 高危操作
	if event.ObjectRef.Subresource == "exec" {
		return "high"
	}
	if event.ObjectRef.Resource == "secrets" && (event.Verb == "get" || event.Verb == "list") {
		return "medium"
	}
	if event.ObjectRef.Resource == "clusterrolebindings" && event.Verb == "create" {
		return "high"
	}
	return "info"
}

func (h *KubeAuditHandler) buildAuditTitle(event *AuditEvent) string {
	if event.ObjectRef == nil {
		return event.Verb + " " + event.RequestURI
	}
	title := event.Verb + " " + event.ObjectRef.Resource
	if event.ObjectRef.Subresource != "" {
		title += "/" + event.ObjectRef.Subresource
	}
	if event.ObjectRef.Name != "" {
		title += " " + event.ObjectRef.Name
	}
	return title
}

func (h *KubeAuditHandler) buildAuditMessage(event *AuditEvent) string {
	msg := "User: " + event.User.Username
	if event.ObjectRef != nil && event.ObjectRef.Namespace != "" {
		msg += ", Namespace: " + event.ObjectRef.Namespace
	}
	if event.UserAgent != "" {
		msg += ", UserAgent: " + event.UserAgent
	}
	return msg
}
