package biz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/imkerbos/mxsec-platform/internal/server/model"
)

// BaselineCheck 单项基线检查定义
type BaselineCheck struct {
	CheckID     string
	CheckName   string
	Category    string
	Severity    string
	Description string
	Remediation string
	Benchmark   string
	Run         func(ctx context.Context, checker *KubeBaselineChecker) (result string, affected model.AffectedResources)
}

// KubeBaselineChecker K8s CIS 基线检查器
type KubeBaselineChecker struct {
	db              *gorm.DB
	logger          *zap.Logger
	kubeClient      *KubeClientManager
	checks          []BaselineCheck
	currentCluster  uint
}

// NewKubeBaselineChecker 创建基线检查器
func NewKubeBaselineChecker(db *gorm.DB, logger *zap.Logger, kubeClient *KubeClientManager) *KubeBaselineChecker {
	c := &KubeBaselineChecker{
		db:         db,
		logger:     logger,
		kubeClient: kubeClient,
	}
	c.registerChecks()
	return c
}

func (c *KubeBaselineChecker) registerChecks() {
	c.checks = []BaselineCheck{
		{
			CheckID: "CIS-K8S-001", CheckName: "匿名用户 ClusterRoleBinding 检查",
			Category: "RBAC", Severity: "critical", Benchmark: "CIS Kubernetes Benchmark 1.8",
			Description: "检查是否存在绑定到 system:anonymous 或 system:unauthenticated 的 ClusterRoleBinding",
			Remediation: "删除绑定到匿名用户的 ClusterRoleBinding",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) {
				return ch.checkAnonymousRBAC(ctx)
			},
		},
		{
			CheckID: "CIS-K8S-002", CheckName: "NetworkPolicy 覆盖率检查",
			Category: "Network", Severity: "high", Benchmark: "CIS Kubernetes Benchmark 1.8",
			Description: "检查所有非系统 Namespace 是否配置了 NetworkPolicy",
			Remediation: "为所有业务 Namespace 配置 NetworkPolicy 限制网络访问",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) {
				return ch.checkNetworkPolicy(ctx)
			},
		},
		{
			CheckID: "CIS-K8S-003", CheckName: "特权容器检查",
			Category: "Pod Security", Severity: "critical", Benchmark: "CIS Kubernetes Benchmark 1.8",
			Description: "检查集群中是否存在运行中的特权容器",
			Remediation: "移除容器的 privileged: true 配置，使用最小权限原则",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) {
				return ch.checkPrivilegedPods(ctx)
			},
		},
		{
			CheckID: "CIS-K8S-004", CheckName: "hostNetwork/hostPID/hostIPC 检查",
			Category: "Pod Security", Severity: "high", Benchmark: "CIS Kubernetes Benchmark 1.8",
			Description: "检查是否存在使用 hostNetwork、hostPID 或 hostIPC 的 Pod",
			Remediation: "移除 Pod 的 hostNetwork/hostPID/hostIPC 配置",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) {
				return ch.checkHostNamespacePods(ctx)
			},
		},
		{
			CheckID: "CIS-K8S-005", CheckName: "默认 ServiceAccount 使用检查",
			Category: "RBAC", Severity: "medium", Benchmark: "CIS Kubernetes Benchmark 1.8",
			Description: "检查是否有 Pod 使用默认 ServiceAccount 且未禁用 token 自动挂载",
			Remediation: "为工作负载创建专用 ServiceAccount，设置 automountServiceAccountToken: false",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) {
				return ch.checkDefaultServiceAccount(ctx)
			},
		},
	}
}

// RunChecks 对指定集群执行所有基线检查
func (c *KubeBaselineChecker) RunChecks(clusterID uint) ([]model.KubeBaseline, error) {
	var cluster model.KubeCluster
	if err := c.db.First(&cluster, clusterID).Error; err != nil {
		return nil, fmt.Errorf("集群不存在: %w", err)
	}

	// 确保能连接集群
	if _, err := c.kubeClient.GetClient(clusterID); err != nil {
		return nil, fmt.Errorf("连接集群失败: %w", err)
	}

	c.currentCluster = clusterID

	// 先删除该集群旧的检查结果
	c.db.Where("cluster_id = ?", clusterID).Delete(&model.KubeBaseline{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	now := model.LocalTime(time.Now())
	var results []model.KubeBaseline

	for _, check := range c.checks {
		result, affected := check.Run(ctx, c)

		baseline := model.KubeBaseline{
			ClusterID:         clusterID,
			ClusterName:       cluster.Name,
			Category:          check.Category,
			CheckID:           check.CheckID,
			CheckName:         check.CheckName,
			Title:             check.CheckName,
			Description:       check.Description,
			Severity:          check.Severity,
			Result:            result,
			Remediation:       check.Remediation,
			Benchmark:         check.Benchmark,
			AffectedResources: affected,
			CheckedAt:         now,
		}

		if err := c.db.Create(&baseline).Error; err != nil {
			c.logger.Error("保存基线检查结果失败", zap.String("check_id", check.CheckID), zap.Error(err))
			continue
		}
		results = append(results, baseline)
	}

	// 更新集群健康评分
	c.updateHealthScore(clusterID, results)

	return results, nil
}

func (c *KubeBaselineChecker) updateHealthScore(clusterID uint, results []model.KubeBaseline) {
	if len(results) == 0 {
		return
	}
	passed := 0
	for _, r := range results {
		if r.Result == "pass" {
			passed++
		}
	}
	score := passed * 100 / len(results)
	c.db.Model(&model.KubeCluster{}).Where("id = ?", clusterID).Update("health_score", score)
}

// --- 各检查项实现 ---

func (c *KubeBaselineChecker) checkAnonymousRBAC(ctx context.Context) (string, model.AffectedResources) {
	// 遍历所有集群客户端缓存中的最后一个（RunChecks 保证了连接）
	clientset := c.getLastClient()
	if clientset == nil {
		return "error", nil
	}

	crbs, err := clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "error", nil
	}

	var affected model.AffectedResources
	for _, crb := range crbs.Items {
		for _, subject := range crb.Subjects {
			if subject.Name == "system:anonymous" || subject.Name == "system:unauthenticated" {
				affected = append(affected, model.AffectedResource{
					Kind: "ClusterRoleBinding", Name: crb.Name,
				})
			}
		}
	}

	if len(affected) > 0 {
		return "fail", affected
	}
	return "pass", nil
}

func (c *KubeBaselineChecker) checkNetworkPolicy(ctx context.Context) (string, model.AffectedResources) {
	clientset := c.getLastClient()
	if clientset == nil {
		return "error", nil
	}

	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "error", nil
	}

	systemNS := map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true, "default": true}
	var affected model.AffectedResources

	for _, ns := range namespaces.Items {
		if systemNS[ns.Name] {
			continue
		}
		policies, err := clientset.NetworkingV1().NetworkPolicies(ns.Name).List(ctx, metav1.ListOptions{})
		if err != nil || len(policies.Items) == 0 {
			affected = append(affected, model.AffectedResource{
				Kind: "Namespace", Name: ns.Name,
			})
		}
	}

	if len(affected) > 0 {
		return "fail", affected
	}
	return "pass", nil
}

func (c *KubeBaselineChecker) checkPrivilegedPods(ctx context.Context) (string, model.AffectedResources) {
	clientset := c.getLastClient()
	if clientset == nil {
		return "error", nil
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "error", nil
	}

	var affected model.AffectedResources
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.SecurityContext != nil && container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
				affected = append(affected, model.AffectedResource{
					Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace,
				})
				break
			}
		}
	}

	if len(affected) > 0 {
		return "fail", affected
	}
	return "pass", nil
}

func (c *KubeBaselineChecker) checkHostNamespacePods(ctx context.Context) (string, model.AffectedResources) {
	clientset := c.getLastClient()
	if clientset == nil {
		return "error", nil
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "error", nil
	}

	var affected model.AffectedResources
	for _, pod := range pods.Items {
		if pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC {
			reasons := []string{}
			if pod.Spec.HostNetwork {
				reasons = append(reasons, "hostNetwork")
			}
			if pod.Spec.HostPID {
				reasons = append(reasons, "hostPID")
			}
			if pod.Spec.HostIPC {
				reasons = append(reasons, "hostIPC")
			}
			affected = append(affected, model.AffectedResource{
				Kind: "Pod", Name: pod.Name + " (" + strings.Join(reasons, ",") + ")", Namespace: pod.Namespace,
			})
		}
	}

	if len(affected) > 0 {
		return "fail", affected
	}
	return "pass", nil
}

func (c *KubeBaselineChecker) checkDefaultServiceAccount(ctx context.Context) (string, model.AffectedResources) {
	clientset := c.getLastClient()
	if clientset == nil {
		return "error", nil
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "error", nil
	}

	var affected model.AffectedResources
	for _, pod := range pods.Items {
		if pod.Spec.ServiceAccountName == "default" {
			mount := true
			if pod.Spec.AutomountServiceAccountToken != nil && !*pod.Spec.AutomountServiceAccountToken {
				mount = false
			}
			if mount {
				affected = append(affected, model.AffectedResource{
					Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace,
				})
			}
		}
	}

	if len(affected) > 0 {
		return "fail", affected
	}
	return "pass", nil
}

// getLastClient 获取当前检查集群的客户端
func (c *KubeBaselineChecker) getLastClient() *kubernetes.Clientset {
	clientset, err := c.kubeClient.GetClient(c.currentCluster)
	if err != nil {
		c.logger.Error("获取集群客户端失败", zap.Uint("cluster_id", c.currentCluster), zap.Error(err))
		return nil
	}
	return clientset
}
