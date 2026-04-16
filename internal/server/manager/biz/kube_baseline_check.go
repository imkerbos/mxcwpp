package biz

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
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
	db             *gorm.DB
	logger         *zap.Logger
	kubeClient     *KubeClientManager
	checks         []BaselineCheck
	currentCluster uint
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

// isSystemNamespace 判断是否为系统 Namespace
func isSystemNamespace(ns string) bool {
	switch ns {
	case "kube-system", "kube-public", "kube-node-lease":
		return true
	}
	return false
}

const cisBenchmark = "CIS Kubernetes Benchmark 1.8"

func (c *KubeBaselineChecker) registerChecks() {
	c.checks = []BaselineCheck{
		// ===== RBAC 安全 (001, 005, 006-012) =====
		{CheckID: "CIS-K8S-001", CheckName: "匿名用户 ClusterRoleBinding 检查",
			Category: "RBAC", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查是否存在绑定到 system:anonymous 或 system:unauthenticated 的 ClusterRoleBinding",
			Remediation: "删除绑定到匿名用户的 ClusterRoleBinding",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkAnonymousRBAC(ctx) }},
		{CheckID: "CIS-K8S-005", CheckName: "默认 ServiceAccount 使用检查",
			Category: "RBAC", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否有 Pod 使用默认 ServiceAccount 且未禁用 token 自动挂载",
			Remediation: "为工作负载创建专用 ServiceAccount，设置 automountServiceAccountToken: false",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDefaultServiceAccount(ctx) }},
		{CheckID: "CIS-K8S-006", CheckName: "cluster-admin ClusterRoleBinding 审计",
			Category: "RBAC", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查非必要的 cluster-admin ClusterRoleBinding 绑定",
			Remediation: "移除不必要的 cluster-admin 绑定，使用最小权限原则",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkClusterAdminBinding(ctx) }},
		{CheckID: "CIS-K8S-007", CheckName: "通配符 RBAC 权限检查",
			Category: "RBAC", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 ClusterRole 中是否使用通配符 (*) 资源或动词",
			Remediation: "将通配符权限替换为具体的资源和动词列表",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkWildcardRBAC(ctx) }},
		{CheckID: "CIS-K8S-008", CheckName: "Pod exec/attach 权限检查",
			Category: "RBAC", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否有角色授予 pods/exec 或 pods/attach 权限",
			Remediation: "限制 pods/exec 和 pods/attach 权限，仅授予必要的管理员角色",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkExecAttachRBAC(ctx) }},
		{CheckID: "CIS-K8S-009", CheckName: "Secrets 访问权限检查",
			Category: "RBAC", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否有角色授予 secrets 的 list/get/watch 权限",
			Remediation: "限制 secrets 访问权限，仅授予必要的服务账户",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSecretsAccessRBAC(ctx) }},
		{CheckID: "CIS-K8S-010", CheckName: "权限提升 RBAC 检查",
			Category: "RBAC", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查是否有角色授予 escalate 或 bind 权限",
			Remediation: "移除 escalate 和 bind 权限，防止权限提升",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkEscalateRBAC(ctx) }},
		{CheckID: "CIS-K8S-011", CheckName: "ServiceAccount 自动挂载 Token 检查",
			Category: "RBAC", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 ServiceAccount 是否启用了自动挂载 Token",
			Remediation: "在 ServiceAccount 上设置 automountServiceAccountToken: false",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSAAutoMount(ctx) }},
		{CheckID: "CIS-K8S-012", CheckName: "system:masters 绑定检查",
			Category: "RBAC", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查是否有自定义绑定到 system:masters 组",
			Remediation: "避免将用户或 ServiceAccount 绑定到 system:masters 组",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSystemMastersBinding(ctx) }},

		// ===== Pod 安全 (003, 004, 013-028) =====
		{CheckID: "CIS-K8S-003", CheckName: "特权容器检查",
			Category: "Pod Security", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查集群中是否存在运行中的特权容器",
			Remediation: "移除容器的 privileged: true 配置，使用最小权限原则",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPrivilegedPods(ctx) }},
		{CheckID: "CIS-K8S-004", CheckName: "hostNetwork/hostPID/hostIPC 检查",
			Category: "Pod Security", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否存在使用 hostNetwork、hostPID 或 hostIPC 的 Pod",
			Remediation: "移除 Pod 的 hostNetwork/hostPID/hostIPC 配置",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkHostNamespacePods(ctx) }},
		{CheckID: "CIS-K8S-013", CheckName: "以 Root 运行容器检查",
			Category: "Pod Security", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查容器是否以 root 用户运行（runAsNonRoot 未设置或 runAsUser=0）",
			Remediation: "设置 securityContext.runAsNonRoot: true 或 runAsUser 为非零值",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkRunAsRoot(ctx) }},
		{CheckID: "CIS-K8S-014", CheckName: "危险 Capabilities 检查",
			Category: "Pod Security", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查容器是否拥有危险 Capabilities (NET_RAW, SYS_ADMIN, ALL)",
			Remediation: "移除危险 Capabilities，仅保留必要的最小权限",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDangerousCapabilities(ctx) }},
		{CheckID: "CIS-K8S-015", CheckName: "只读根文件系统检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否设置了只读根文件系统",
			Remediation: "设置 securityContext.readOnlyRootFilesystem: true",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkReadOnlyRootFilesystem(ctx) }},
		{CheckID: "CIS-K8S-016", CheckName: "AllowPrivilegeEscalation 检查",
			Category: "Pod Security", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查容器是否允许权限提升",
			Remediation: "设置 securityContext.allowPrivilegeEscalation: false",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkAllowPrivilegeEscalation(ctx) }},
		{CheckID: "CIS-K8S-017", CheckName: "hostPath 卷挂载检查",
			Category: "Pod Security", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 Pod 是否挂载了 hostPath 卷",
			Remediation: "避免使用 hostPath 卷，改用 PersistentVolume 或 emptyDir",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkHostPathVolumes(ctx) }},
		{CheckID: "CIS-K8S-018", CheckName: "Docker Socket 挂载检查",
			Category: "Pod Security", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查容器是否挂载了 Docker Socket (/var/run/docker.sock)",
			Remediation: "移除 Docker Socket 挂载，避免容器逃逸风险",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDockerSocketMount(ctx) }},
		{CheckID: "CIS-K8S-019", CheckName: "Seccomp Profile 检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 Pod 是否配置了 Seccomp Profile",
			Remediation: "设置 securityContext.seccompProfile.type 为 RuntimeDefault 或 Localhost",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSeccompProfile(ctx) }},
		{CheckID: "CIS-K8S-020", CheckName: "CPU 资源限制检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否设置了 CPU 资源限制",
			Remediation: "为所有容器设置 resources.limits.cpu",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkCPULimits(ctx) }},
		{CheckID: "CIS-K8S-021", CheckName: "内存资源限制检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否设置了内存资源限制",
			Remediation: "为所有容器设置 resources.limits.memory",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkMemoryLimits(ctx) }},
		{CheckID: "CIS-K8S-022", CheckName: "资源请求检查",
			Category: "Pod Security", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查容器是否设置了资源请求 (requests)",
			Remediation: "为所有容器设置 resources.requests.cpu 和 resources.requests.memory",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkResourceRequests(ctx) }},
		{CheckID: "CIS-K8S-023", CheckName: "存活探针检查",
			Category: "Pod Security", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查容器是否配置了存活探针 (livenessProbe)",
			Remediation: "为长时间运行的容器配置 livenessProbe",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLivenessProbe(ctx) }},
		{CheckID: "CIS-K8S-024", CheckName: "就绪探针检查",
			Category: "Pod Security", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查容器是否配置了就绪探针 (readinessProbe)",
			Remediation: "为提供服务的容器配置 readinessProbe",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkReadinessProbe(ctx) }},
		{CheckID: "CIS-K8S-025", CheckName: "镜像 :latest 标签检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否使用 :latest 标签或未指定标签的镜像",
			Remediation: "使用明确的版本标签替代 :latest",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLatestImageTag(ctx) }},
		{CheckID: "CIS-K8S-026", CheckName: "镜像拉取策略检查",
			Category: "Pod Security", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查容器是否设置了 imagePullPolicy: Always",
			Remediation: "设置 imagePullPolicy: Always 确保使用最新镜像",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkImagePullPolicy(ctx) }},
		{CheckID: "CIS-K8S-027", CheckName: "hostPort 使用检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否使用了 hostPort",
			Remediation: "避免使用 hostPort，改用 Service 暴露端口",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkHostPort(ctx) }},
		{CheckID: "CIS-K8S-028", CheckName: "额外 Capabilities 添加检查",
			Category: "Pod Security", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否添加了额外的 Linux Capabilities",
			Remediation: "移除不必要的 Capabilities，使用 drop ALL + 仅添加必需的方式",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkAddedCapabilities(ctx) }},

		// ===== 网络安全 (002, 029-035) =====
		{CheckID: "CIS-K8S-002", CheckName: "NetworkPolicy 覆盖率检查",
			Category: "Network", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查所有非系统 Namespace 是否配置了 NetworkPolicy",
			Remediation: "为所有业务 Namespace 配置 NetworkPolicy 限制网络访问",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNetworkPolicy(ctx) }},
		{CheckID: "CIS-K8S-029", CheckName: "默认拒绝入站 NetworkPolicy 检查",
			Category: "Network", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查非系统 Namespace 是否配置了默认拒绝入站的 NetworkPolicy",
			Remediation: "为每个 Namespace 创建默认拒绝入站流量的 NetworkPolicy",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDefaultDenyIngress(ctx) }},
		{CheckID: "CIS-K8S-030", CheckName: "默认拒绝出站 NetworkPolicy 检查",
			Category: "Network", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查非系统 Namespace 是否配置了默认拒绝出站的 NetworkPolicy",
			Remediation: "为每个 Namespace 创建默认拒绝出站流量的 NetworkPolicy",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDefaultDenyEgress(ctx) }},
		{CheckID: "CIS-K8S-031", CheckName: "NodePort 类型 Service 检查",
			Category: "Network", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在 NodePort 类型的 Service",
			Remediation: "使用 ClusterIP + Ingress 替代 NodePort，减少攻击面",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodePortServices(ctx) }},
		{CheckID: "CIS-K8S-032", CheckName: "LoadBalancer 类型 Service 检查",
			Category: "Network", Severity: "low", Benchmark: cisBenchmark,
			Description: "审计 LoadBalancer 类型的 Service（可能暴露到外网）",
			Remediation: "审查 LoadBalancer Service 是否必要，考虑使用 Ingress 替代",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLoadBalancerServices(ctx) }},
		{CheckID: "CIS-K8S-033", CheckName: "ExternalIPs Service 检查",
			Category: "Network", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否存在配置了 ExternalIPs 的 Service",
			Remediation: "移除 Service 的 externalIPs 配置，使用 LoadBalancer 或 Ingress 替代",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkExternalIPsServices(ctx) }},
		{CheckID: "CIS-K8S-034", CheckName: "Ingress TLS 配置检查",
			Category: "Network", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 Ingress 资源是否配置了 TLS",
			Remediation: "为所有 Ingress 配置 TLS 证书，启用 HTTPS",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkIngressTLS(ctx) }},
		{CheckID: "CIS-K8S-035", CheckName: "无 Selector 的 Service 检查",
			Category: "Network", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在没有 selector 的 Service（可能指向外部端点）",
			Remediation: "确认无 selector 的 Service 是否必要，添加说明注解",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkServiceWithoutSelector(ctx) }},

		// ===== 密钥与配置 (036-043) =====
		{CheckID: "CIS-K8S-036", CheckName: "环境变量中的 Secret 引用检查",
			Category: "Secrets & Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器是否通过环境变量引用 Secret（建议使用 volume 挂载）",
			Remediation: "使用 volume 挂载方式注入 Secret，而非环境变量",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSecretsInEnv(ctx) }},
		{CheckID: "CIS-K8S-037", CheckName: "默认 Namespace 使用检查",
			Category: "Secrets & Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否有工作负载运行在 default Namespace",
			Remediation: "将工作负载部署到专用 Namespace，避免使用 default",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDefaultNamespaceUsage(ctx) }},
		{CheckID: "CIS-K8S-038", CheckName: "Tiller (Helm v2) 检测",
			Category: "Secrets & Config", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查集群中是否存在已弃用的 Tiller (Helm v2) 组件",
			Remediation: "升级到 Helm v3，移除 Tiller 部署",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkTillerDeployment(ctx) }},
		{CheckID: "CIS-K8S-039", CheckName: "大容量 Secret 检查",
			Category: "Secrets & Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 Secret 大小是否超过 1MB（可能影响 etcd 性能）",
			Remediation: "拆分大型 Secret 或使用外部密钥管理系统",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLargeSecrets(ctx) }},
		{CheckID: "CIS-K8S-040", CheckName: "大容量 ConfigMap 检查",
			Category: "Secrets & Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 ConfigMap 大小是否超过 1MB",
			Remediation: "拆分大型 ConfigMap 或使用外部配置服务",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLargeConfigMaps(ctx) }},
		{CheckID: "CIS-K8S-041", CheckName: "Namespace 标签规范检查",
			Category: "Secrets & Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 Namespace 是否缺少标准标签（如 team、environment）",
			Remediation: "为 Namespace 添加标准化标签以便管理和审计",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNamespaceLabels(ctx) }},
		{CheckID: "CIS-K8S-042", CheckName: "ServiceAccount Secret 类型检查",
			Category: "Secrets & Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在遗留的 ServiceAccount Token Secret",
			Remediation: "使用 TokenRequest API 替代持久化 ServiceAccount Token",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSATokenSecrets(ctx) }},
		{CheckID: "CIS-K8S-043", CheckName: "Pod 环境变量明文密码检查",
			Category: "Secrets & Config", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查容器环境变量中是否包含明文密码（通过关键字匹配）",
			Remediation: "使用 Secret 资源管理敏感信息，避免明文存储",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPlaintextPasswords(ctx) }},

		// ===== 工作负载安全 (044-055) =====
		{CheckID: "CIS-K8S-044", CheckName: "单副本 Deployment 检查",
			Category: "Workload", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 Deployment 是否只有单副本（影响可用性）",
			Remediation: "为生产 Deployment 设置至少 2 个副本",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkSingleReplicaDeployments(ctx) }},
		{CheckID: "CIS-K8S-045", CheckName: "PodDisruptionBudget 覆盖检查",
			Category: "Workload", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 Deployment 是否配置了 PodDisruptionBudget",
			Remediation: "为关键 Deployment 创建 PodDisruptionBudget",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPDBCoverage(ctx) }},
		{CheckID: "CIS-K8S-046", CheckName: "CronJob 无超时限制检查",
			Category: "Workload", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 CronJob 是否设置了 activeDeadlineSeconds",
			Remediation: "为 CronJob 设置 activeDeadlineSeconds 防止任务永不超时",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkCronJobDeadline(ctx) }},
		{CheckID: "CIS-K8S-047", CheckName: "不可信镜像仓库检查",
			Category: "Workload", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查容器镜像是否来自可信的镜像仓库",
			Remediation: "仅使用企业内部镜像仓库或白名单镜像仓库",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkUntrustedRegistries(ctx) }},
		{CheckID: "CIS-K8S-048", CheckName: "HPA 最小副本数检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 HPA 的 minReplicas 是否大于 1",
			Remediation: "设置 HPA minReplicas >= 2 保证高可用",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkHPAMinReplicas(ctx) }},
		{CheckID: "CIS-K8S-049", CheckName: "DaemonSet 资源限制检查",
			Category: "Workload", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 DaemonSet 容器是否设置了资源限制",
			Remediation: "为 DaemonSet 容器设置 CPU 和内存资源限制",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDaemonSetResourceLimits(ctx) }},
		{CheckID: "CIS-K8S-050", CheckName: "Job 无重试限制检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 Job 是否设置了 backoffLimit",
			Remediation: "为 Job 设置合理的 backoffLimit 防止无限重试",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkJobBackoffLimit(ctx) }},
		{CheckID: "CIS-K8S-051", CheckName: "Deployment 更新策略检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 Deployment 是否使用 RollingUpdate 策略",
			Remediation: "使用 RollingUpdate 策略确保零停机部署",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDeploymentStrategy(ctx) }},
		{CheckID: "CIS-K8S-052", CheckName: "StatefulSet 持久化存储检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 StatefulSet 是否配置了 volumeClaimTemplates",
			Remediation: "为 StatefulSet 配置持久化存储保证数据安全",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkStatefulSetStorage(ctx) }},
		{CheckID: "CIS-K8S-053", CheckName: "Pod 反亲和性检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查多副本 Deployment 是否配置了 Pod 反亲和性",
			Remediation: "配置 podAntiAffinity 确保 Pod 分散在不同节点",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPodAntiAffinity(ctx) }},
		{CheckID: "CIS-K8S-054", CheckName: "Pod 拓扑分布约束检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查多副本 Deployment 是否配置了拓扑分布约束",
			Remediation: "配置 topologySpreadConstraints 确保跨区域分布",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkTopologySpreadConstraints(ctx) }},
		{CheckID: "CIS-K8S-055", CheckName: "Deployment 自动扩展检查",
			Category: "Workload", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 Deployment 是否配置了 HPA 自动扩展",
			Remediation: "为关键 Deployment 配置 HPA 以应对流量波动",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkDeploymentHPA(ctx) }},

		// ===== 节点安全 (056-064) =====
		{CheckID: "CIS-K8S-056", CheckName: "节点 NotReady 状态检查",
			Category: "Node", Severity: "critical", Benchmark: cisBenchmark,
			Description: "检查是否存在 NotReady 状态的节点",
			Remediation: "排查 NotReady 节点的问题并恢复",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeNotReady(ctx) }},
		{CheckID: "CIS-K8S-057", CheckName: "节点压力条件检查",
			Category: "Node", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查节点是否存在内存/磁盘/PID 压力",
			Remediation: "扩容节点资源或迁移工作负载",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodePressure(ctx) }},
		{CheckID: "CIS-K8S-058", CheckName: "节点内核版本检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查节点内核版本是否过旧（< 4.19）",
			Remediation: "升级节点内核版本至 4.19+",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeKernelVersion(ctx) }},
		{CheckID: "CIS-K8S-059", CheckName: "节点容器运行时检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查节点容器运行时类型和版本",
			Remediation: "使用推荐的容器运行时 (containerd/CRI-O)",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeContainerRuntime(ctx) }},
		{CheckID: "CIS-K8S-060", CheckName: "节点资源分配率检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查节点资源分配率是否超过 90%",
			Remediation: "增加节点或迁移工作负载以降低资源使用率",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeResourceUtilization(ctx) }},
		{CheckID: "CIS-K8S-061", CheckName: "节点不可调度检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在被标记为不可调度的节点",
			Remediation: "恢复节点可调度状态或增加新节点",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeUnschedulable(ctx) }},
		{CheckID: "CIS-K8S-062", CheckName: "节点 Taint 检查",
			Category: "Node", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查节点 Taint 配置是否合理",
			Remediation: "审查节点 Taint 配置，确保 NoSchedule/NoExecute 设置合理",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodeTaints(ctx) }},
		{CheckID: "CIS-K8S-063", CheckName: "孤儿 Pod 检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在没有控制器管理的孤儿 Pod",
			Remediation: "使用 Deployment/StatefulSet/DaemonSet 管理 Pod",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkOrphanPods(ctx) }},
		{CheckID: "CIS-K8S-064", CheckName: "节点 Pod 数量检查",
			Category: "Node", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查节点上运行的 Pod 数量是否接近上限",
			Remediation: "增加节点或迁移 Pod 以降低密度",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNodePodCount(ctx) }},

		// ===== 集群配置 (065-073) =====
		{CheckID: "CIS-K8S-065", CheckName: "Kubernetes 版本检查",
			Category: "Cluster Config", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 Kubernetes 版本是否在支持范围内",
			Remediation: "升级 Kubernetes 版本至受支持的版本",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkK8sVersion(ctx) }},
		{CheckID: "CIS-K8S-066", CheckName: "Namespace LimitRange 检查",
			Category: "Cluster Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查非系统 Namespace 是否配置了 LimitRange",
			Remediation: "为每个 Namespace 创建 LimitRange 限制资源使用",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkLimitRange(ctx) }},
		{CheckID: "CIS-K8S-067", CheckName: "Namespace ResourceQuota 检查",
			Category: "Cluster Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查非系统 Namespace 是否配置了 ResourceQuota",
			Remediation: "为每个 Namespace 创建 ResourceQuota 限制资源总量",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkResourceQuota(ctx) }},
		{CheckID: "CIS-K8S-068", CheckName: "Pod Security Standards 标签检查",
			Category: "Cluster Config", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 Namespace 是否配置了 Pod Security Standards (PSS) 标签",
			Remediation: "为 Namespace 设置 pod-security.kubernetes.io/enforce 标签",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPSSLabels(ctx) }},
		{CheckID: "CIS-K8S-069", CheckName: "Admission Webhook 检查",
			Category: "Cluster Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查是否配置了 ValidatingWebhookConfiguration",
			Remediation: "配置准入 Webhook 加强安全策略执行",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkAdmissionWebhooks(ctx) }},
		{CheckID: "CIS-K8S-070", CheckName: "MutatingWebhook 超时检查",
			Category: "Cluster Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 MutatingWebhook 超时设置是否合理",
			Remediation: "设置 MutatingWebhook 超时为 10s 以下，避免阻塞 API",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkMutatingWebhookTimeout(ctx) }},
		{CheckID: "CIS-K8S-071", CheckName: "Namespace 数量审计",
			Category: "Cluster Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "审计集群中非系统 Namespace 的数量",
			Remediation: "清理不再使用的 Namespace",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkNamespaceCount(ctx) }},
		{CheckID: "CIS-K8S-072", CheckName: "PersistentVolume 回收策略检查",
			Category: "Cluster Config", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 PersistentVolume 是否使用 Delete 回收策略",
			Remediation: "对重要数据使用 Retain 策略，防止数据丢失",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPVReclaimPolicy(ctx) }},
		{CheckID: "CIS-K8S-073", CheckName: "StorageClass 扩展配置检查",
			Category: "Cluster Config", Severity: "low", Benchmark: cisBenchmark,
			Description: "检查 StorageClass 是否启用了卷扩展",
			Remediation: "设置 StorageClass allowVolumeExpansion: true",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkStorageClassExpansion(ctx) }},

		// ===== 供应链与运行时 (074-080) =====
		{CheckID: "CIS-K8S-074", CheckName: "镜像无 Digest 检查",
			Category: "Supply Chain", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查容器镜像是否使用了 digest 引用",
			Remediation: "使用 image@sha256:digest 格式引用镜像，确保不可变性",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkImageDigest(ctx) }},
		{CheckID: "CIS-K8S-075", CheckName: "Init 容器安全检查",
			Category: "Supply Chain", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查 Init 容器是否遵守安全最佳实践（非特权、非 root）",
			Remediation: "Init 容器也应遵循最小权限原则",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkInitContainerSecurity(ctx) }},
		{CheckID: "CIS-K8S-076", CheckName: "imagePullSecrets 检查",
			Category: "Supply Chain", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查 Pod 是否配置了 imagePullSecrets（私有仓库）",
			Remediation: "为使用私有镜像仓库的 Pod 配置 imagePullSecrets",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkImagePullSecrets(ctx) }},
		{CheckID: "CIS-K8S-077", CheckName: "Pending 状态 Pod 检查",
			Category: "Runtime", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否存在长时间处于 Pending 状态的 Pod",
			Remediation: "排查 Pending Pod 的调度问题（资源不足、亲和性等）",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPendingPods(ctx) }},
		{CheckID: "CIS-K8S-078", CheckName: "高重启次数 Pod 检查",
			Category: "Runtime", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否存在重启次数超过 10 次的 Pod",
			Remediation: "排查 Pod 频繁重启的原因（OOM、健康检查失败等）",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkHighRestartPods(ctx) }},
		{CheckID: "CIS-K8S-079", CheckName: "CrashLoopBackOff Pod 检查",
			Category: "Runtime", Severity: "high", Benchmark: cisBenchmark,
			Description: "检查是否存在 CrashLoopBackOff 状态的 Pod",
			Remediation: "查看 Pod 日志排查崩溃原因",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkCrashLoopPods(ctx) }},
		{CheckID: "CIS-K8S-080", CheckName: "无属主 Pod 检查",
			Category: "Runtime", Severity: "medium", Benchmark: cisBenchmark,
			Description: "检查是否存在没有 OwnerReference 的 Pod（不受控制器管理）",
			Remediation: "使用 Deployment/StatefulSet 等控制器管理 Pod",
			Run: func(ctx context.Context, ch *KubeBaselineChecker) (string, model.AffectedResources) { return ch.checkPodsWithoutOwner(ctx) }},
	}
}

// RunChecks 对指定集群执行所有基线检查
func (c *KubeBaselineChecker) RunChecks(clusterID uint) ([]model.KubeBaseline, error) {
	var cluster model.KubeCluster
	if err := c.db.First(&cluster, clusterID).Error; err != nil {
		return nil, fmt.Errorf("集群不存在: %w", err)
	}

	if _, err := c.kubeClient.GetClient(clusterID); err != nil {
		return nil, fmt.Errorf("连接集群失败: %w", err)
	}

	c.currentCluster = clusterID

	// 先删除该集群旧的检查结果
	c.db.Where("cluster_id = ?", clusterID).Delete(&model.KubeBaseline{})

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

// getLastClient 获取当前检查集群的客户端
func (c *KubeBaselineChecker) getLastClient() *kubernetes.Clientset {
	clientset, err := c.kubeClient.GetClient(c.currentCluster)
	if err != nil {
		c.logger.Error("获取集群客户端失败", zap.Uint("cluster_id", c.currentCluster), zap.Error(err))
		return nil
	}
	return clientset
}
