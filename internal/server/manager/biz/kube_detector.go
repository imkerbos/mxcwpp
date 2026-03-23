package biz

import (
	"encoding/json"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// DetectionRule 检测规则定义
type DetectionRule struct {
	ID          string
	Name        string
	Severity    string
	AlarmType   model.KubeAlarmType
	Description string
	Match       func(event *model.AuditEvent) bool
}

// KubeDetector Audit Event 规则检测引擎
type KubeDetector struct {
	db           *gorm.DB
	logger       *zap.Logger
	alarmService *KubeAlarmService
	rules        []DetectionRule
}

// NewKubeDetector 创建检测引擎
func NewKubeDetector(db *gorm.DB, logger *zap.Logger, alarmService *KubeAlarmService) *KubeDetector {
	d := &KubeDetector{
		db:           db,
		logger:       logger,
		alarmService: alarmService,
	}
	d.registerRules()
	return d
}

func (d *KubeDetector) registerRules() {
	d.rules = []DetectionRule{
		{
			ID: "K8S-001", Name: "kubectl exec 进入容器",
			Severity: "high", AlarmType: model.KubeAlarmTypeAbnormalProcess,
			Description: "检测到通过 kubectl exec 进入容器的操作",
			Match: func(e *model.AuditEvent) bool {
				return e.ObjectRef != nil && e.Verb == "create" && e.ObjectRef.Subresource == "exec"
			},
		},
		{
			ID: "K8S-002", Name: "创建 hostNetwork/hostPID Pod",
			Severity: "critical", AlarmType: model.KubeAlarmTypeContainerEscape,
			Description: "检测到创建使用 hostNetwork 或 hostPID 的 Pod",
			Match: func(e *model.AuditEvent) bool {
				if e.ObjectRef == nil || e.Verb != "create" || e.ObjectRef.Resource != "pods" {
					return false
				}
				return containsHostAccess(e.RequestObj)
			},
		},
		{
			ID: "K8S-003", Name: "ClusterRole 绑定高权限",
			Severity: "critical", AlarmType: model.KubeAlarmTypePrivilegeEscalation,
			Description: "检测到创建 ClusterRoleBinding 绑定高权限角色",
			Match: func(e *model.AuditEvent) bool {
				return e.ObjectRef != nil && e.Verb == "create" && e.ObjectRef.Resource == "clusterrolebindings"
			},
		},
		{
			ID: "K8S-004", Name: "访问 Secret 资源",
			Severity: "medium", AlarmType: model.KubeAlarmTypeAbnormalProcess,
			Description: "检测到读取 Secret 资源的操作",
			Match: func(e *model.AuditEvent) bool {
				return e.ObjectRef != nil && e.ObjectRef.Resource == "secrets" &&
					(e.Verb == "get" || e.Verb == "list")
			},
		},
		{
			ID: "K8S-005", Name: "创建特权容器",
			Severity: "critical", AlarmType: model.KubeAlarmTypeContainerEscape,
			Description: "检测到创建特权容器",
			Match: func(e *model.AuditEvent) bool {
				if e.ObjectRef == nil || e.Verb != "create" || e.ObjectRef.Resource != "pods" {
					return false
				}
				return containsPrivileged(e.RequestObj)
			},
		},
		{
			ID: "K8S-006", Name: "ServiceAccount Token 异常使用",
			Severity: "high", AlarmType: model.KubeAlarmTypeAbnormalProcess,
			Description: "检测到非标准工具使用 ServiceAccount Token 访问 API",
			Match: func(e *model.AuditEvent) bool {
				if e.UserAgent == "" {
					return false
				}
				knownAgents := []string{"kubectl/", "kube-", "kubelet/", "Go-http-client/", "argo", "helm"}
				for _, agent := range knownAgents {
					if strings.HasPrefix(e.UserAgent, agent) {
						return false
					}
				}
				// 非标准 UserAgent + ServiceAccount 用户
				return strings.HasPrefix(e.User.Username, "system:serviceaccount:")
			},
		},
		{
			ID: "K8S-007", Name: "容器内反弹 Shell 迹象",
			Severity: "critical", AlarmType: model.KubeAlarmTypeReverseShell,
			Description: "检测到容器内可能的反弹 Shell 操作",
			Match: func(e *model.AuditEvent) bool {
				if e.ObjectRef == nil || e.Verb != "create" || e.ObjectRef.Subresource != "exec" {
					return false
				}
				reqStr := string(e.RequestObj)
				return strings.Contains(reqStr, "/bin/bash") || strings.Contains(reqStr, "/bin/sh") ||
					strings.Contains(reqStr, "nc ") || strings.Contains(reqStr, "ncat")
			},
		},
		{
			ID: "K8S-008", Name: "挂载宿主机路径（容器逃逸迹象）",
			Severity: "critical", AlarmType: model.KubeAlarmTypeContainerEscape,
			Description: "检测到 Pod 挂载宿主机敏感路径",
			Match: func(e *model.AuditEvent) bool {
				if e.ObjectRef == nil || e.Verb != "create" || e.ObjectRef.Resource != "pods" {
					return false
				}
				return containsHostPathMount(e.RequestObj)
			},
		},
	}
}

// DetectAuditEvent 对单个 audit event 执行所有规则检测
func (d *KubeDetector) DetectAuditEvent(clusterID uint, clusterName string, event *model.AuditEvent) {
	for _, rule := range d.rules {
		if !rule.Match(event) {
			continue
		}

		rawData, _ := json.Marshal(event)
		alarm := model.KubeAlarm{
			ClusterID:   clusterID,
			ClusterName: clusterName,
			Severity:    rule.Severity,
			AlarmType:   rule.AlarmType,
			Title:       "[" + rule.ID + "] " + rule.Name,
			Description: rule.Description,
			Message:     "User: " + event.User.Username + ", Rule: " + rule.ID,
			RawData:     model.RawJSON(rawData),
			Status:      model.KubeAlarmStatusPending,
		}

		if event.ObjectRef != nil {
			alarm.Namespace = event.ObjectRef.Namespace
			alarm.PodName = event.ObjectRef.Name
		}

		created, err := d.alarmService.CreateAlarmWithFilter(&alarm)
		if err != nil {
			d.logger.Error("规则引擎创建告警失败",
				zap.String("rule", rule.ID), zap.Error(err))
		}
		if created {
			d.logger.Info("规则引擎触发告警",
				zap.String("rule", rule.ID),
				zap.String("cluster", clusterName),
			)
		}
	}
}

// 辅助函数：检查 Pod spec 中是否包含 hostNetwork/hostPID
func containsHostAccess(requestObj json.RawMessage) bool {
	var pod struct {
		Spec struct {
			HostNetwork bool `json:"hostNetwork"`
			HostPID     bool `json:"hostPID"`
			HostIPC     bool `json:"hostIPC"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(requestObj, &pod); err != nil {
		return false
	}
	return pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC
}

// 辅助函数：检查 Pod spec 中是否包含特权容器
func containsPrivileged(requestObj json.RawMessage) bool {
	var pod struct {
		Spec struct {
			Containers []struct {
				SecurityContext *struct {
					Privileged *bool `json:"privileged"`
				} `json:"securityContext"`
			} `json:"containers"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(requestObj, &pod); err != nil {
		return false
	}
	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
			return true
		}
	}
	return false
}

// 辅助函数：检查 Pod spec 中是否挂载宿主机敏感路径
func containsHostPathMount(requestObj json.RawMessage) bool {
	var pod struct {
		Spec struct {
			Volumes []struct {
				HostPath *struct {
					Path string `json:"path"`
				} `json:"hostPath"`
			} `json:"volumes"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(requestObj, &pod); err != nil {
		return false
	}
	sensitivePaths := []string{"/", "/etc", "/proc", "/sys", "/var/run/docker.sock", "/run/containerd"}
	for _, vol := range pod.Spec.Volumes {
		if vol.HostPath == nil {
			continue
		}
		for _, sp := range sensitivePaths {
			if vol.HostPath.Path == sp || strings.HasPrefix(vol.HostPath.Path, sp+"/") {
				return true
			}
		}
	}
	return false
}
