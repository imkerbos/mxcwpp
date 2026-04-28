# GKE 集群接入指南

矩阵云安全平台使用标准 Kubernetes kubeconfig 连接集群。GKE 默认依赖 `gke-gcloud-auth-plugin` exec 插件认证，该方式要求运行环境安装 gcloud SDK 并持有有效凭据，不适合服务端长期运行的场景。

本文档介绍如何为 GKE 集群创建专用 ServiceAccount，配置最小权限，并生成基于静态 Token 的 kubeconfig 接入矩阵云安全平台。

## 前置条件

- 本地已安装 `gcloud` CLI 和 `kubectl`
- 本地已安装 `gke-gcloud-auth-plugin`（用于初始连接）
- 拥有目标 GKE 集群的管理员权限（需创建 ClusterRole 和 ClusterRoleBinding）
- 矩阵云安全平台服务端网络可达 GKE 集群 API Server

## 操作步骤

### 1. 连接到目标 GKE 集群

```bash
gcloud container clusters get-credentials <CLUSTER_NAME> \
  --zone <ZONE> \
  --project <PROJECT_ID>
```

### 2. 创建专用 ServiceAccount

```bash
# 创建命名空间
kubectl create namespace mxsec || true

# 创建 ServiceAccount
kubectl create serviceaccount mxsec-sa -n mxsec
```

### 3. 创建自定义 ClusterRole

> **重要**: 不能使用内置的 `view` ClusterRole。`view` 仅包含命名空间级资源的只读权限，不包含 nodes、RBAC 等集群级资源。矩阵云安全平台的容器安全基线检查需要读取 17 种 K8s 资源，必须使用自定义 ClusterRole。

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mxsec-readonly
  labels:
    app: mxsec-platform
rules:
  # 核心资源 - 节点、Pod、命名空间、服务等
  - apiGroups: [""]
    resources:
      - nodes
      - pods
      - namespaces
      - services
      - configmaps
      - secrets
      - serviceaccounts
    verbs: ["get", "list"]

  # 应用工作负载 - Deployment、StatefulSet、DaemonSet
  - apiGroups: ["apps"]
    resources:
      - deployments
      - statefulsets
      - daemonsets
    verbs: ["get", "list"]

  # RBAC - 集群角色与绑定（安全基线检查必需）
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources:
      - clusterroles
      - clusterrolebindings
    verbs: ["get", "list"]

  # 网络 - NetworkPolicy、Ingress
  - apiGroups: ["networking.k8s.io"]
    resources:
      - networkpolicies
      - ingresses
    verbs: ["get", "list"]

  # 策略 - PodDisruptionBudget
  - apiGroups: ["policy"]
    resources:
      - poddisruptionbudgets
    verbs: ["get", "list"]

  # 批处理 - CronJob、Job
  - apiGroups: ["batch"]
    resources:
      - cronjobs
      - jobs
    verbs: ["get", "list"]
EOF
```

<details>
<summary>各资源权限用途说明</summary>

| API 组 | 资源 | 用途 |
|--------|------|------|
| core | nodes | 节点列表、CPU/内存使用率、就绪状态检查 |
| core | pods | Pod 列表、安全基线检查（特权容器、hostNetwork 等） |
| core | namespaces | 命名空间列表、网络策略覆盖率检查 |
| core | services | 服务列表、NodePort/LoadBalancer 安全检查 |
| core | configmaps | 配置检查、环境变量安全审计 |
| core | secrets | 密钥管理安全检查 |
| core | serviceaccounts | SA Token 自动挂载检查、默认 SA 使用检查 |
| apps | deployments | 工作负载列表、副本数/更新策略检查 |
| apps | statefulsets | 工作负载列表 |
| apps | daemonsets | 工作负载列表 |
| rbac | clusterroles | 通配符权限、exec/attach 权限、Secrets 访问检查 |
| rbac | clusterrolebindings | 匿名用户绑定、cluster-admin 绑定检查 |
| networking | networkpolicies | 网络策略覆盖率、默认拒绝规则检查 |
| networking | ingresses | Ingress TLS 配置检查 |
| policy | poddisruptionbudgets | PDB 覆盖率检查 |
| batch | cronjobs | CronJob 截止时间检查 |
| batch | jobs | Job 配置检查 |

</details>

### 4. 绑定权限

```bash
kubectl create clusterrolebinding mxsec-binding \
  --clusterrole=mxsec-readonly \
  --serviceaccount=mxsec:mxsec-sa
```

### 5. 创建永久 Token

Kubernetes 1.24+ 不再自动为 ServiceAccount 创建永久 Token，需手动创建：

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: mxsec-sa-token
  namespace: mxsec
  annotations:
    kubernetes.io/service-account.name: mxsec-sa
type: kubernetes.io/service-account-token
EOF
```

等待几秒后获取 Token 和 CA 证书：

```bash
# 获取 Token
TOKEN=$(kubectl get secret mxsec-sa-token -n mxsec \
  -o jsonpath='{.data.token}' | base64 -d)

# 获取 CA 证书
CA_CERT=$(kubectl get secret mxsec-sa-token -n mxsec \
  -o jsonpath='{.data.ca\.crt}')
```

### 6. 获取集群 API Server 地址

```bash
ENDPOINT=$(gcloud container clusters describe <CLUSTER_NAME> \
  --zone <ZONE> \
  --project <PROJECT_ID> \
  --format="value(endpoint)")

echo "API Server: https://${ENDPOINT}"
```

### 7. 验证权限

在生成 kubeconfig 之前，先验证 ServiceAccount 是否拥有所需权限：

```bash
# 验证核心权限 - 如果这两个通过，说明自定义 ClusterRole 生效
kubectl auth can-i list nodes --as=system:serviceaccount:mxsec:mxsec-sa
kubectl auth can-i list pods --as=system:serviceaccount:mxsec:mxsec-sa -A

# 验证 RBAC 权限（基线检查必需）
kubectl auth can-i list clusterroles --as=system:serviceaccount:mxsec:mxsec-sa
kubectl auth can-i list clusterrolebindings --as=system:serviceaccount:mxsec:mxsec-sa

# 验证网络资源权限
kubectl auth can-i list networkpolicies --as=system:serviceaccount:mxsec:mxsec-sa -A
kubectl auth can-i list ingresses --as=system:serviceaccount:mxsec:mxsec-sa -A
```

所有命令应返回 `yes`。如果任何一项返回 `no`，请检查 ClusterRole 和 ClusterRoleBinding 是否正确创建。

### 8. 生成 kubeconfig

使用脚本一键生成：

```bash
cat <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA_CERT}
    server: https://${ENDPOINT}
  name: gke-cluster
contexts:
- context:
    cluster: gke-cluster
    user: mxsec-sa
  name: gke-context
current-context: gke-context
users:
- name: mxsec-sa
  user:
    token: ${TOKEN}
EOF
```

也可以直接写入文件：

```bash
cat <<EOF > mxsec-kubeconfig.yaml
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA_CERT}
    server: https://${ENDPOINT}
  name: gke-cluster
contexts:
- context:
    cluster: gke-cluster
    user: mxsec-sa
  name: gke-context
current-context: gke-context
users:
- name: mxsec-sa
  user:
    token: ${TOKEN}
EOF

echo "kubeconfig 已保存到 mxsec-kubeconfig.yaml"
```

### 9. 接入矩阵云安全平台

1. 登录矩阵云安全平台管理界面
2. 进入「容器安全」→「集群管理」页面
3. 点击「接入集群」
4. 输入集群名称，将生成的 kubeconfig YAML 粘贴到输入框
5. 保存后系统会自动验证连接并开始同步集群信息

接入成功后，系统将：
- 自动获取集群版本、节点数、Pod 数等基本信息
- 每 5 分钟同步一次集群状态
- 可手动触发容器安全基线检查（CIS Kubernetes Benchmark）

## Token 说明

通过 `kubernetes.io/service-account-token` 类型 Secret 生成的 Token **永不过期**，只要 Secret 和 ServiceAccount 存在就一直有效。

| Token 类型 | 有效期 | 适用场景 |
|------------|--------|----------|
| Secret 绑定 Token（本文方案） | 永久 | 服务端长期连接 |
| TokenRequest API（`kubectl create token`） | 默认 1 小时 | 临时调试 |
| Pod 挂载 Projected Token | 默认 1 小时，自动刷新 | Pod 内部使用 |

Token 失效条件：
- 删除对应的 Secret
- 删除对应的 ServiceAccount
- 删除 ClusterRoleBinding（Token 仍有效但无权限）

## GKE 网络配置

矩阵云安全平台服务端必须能访问 GKE 集群的 API Server。根据集群类型：

- **公共集群**: 确保服务端的出口 IP 在 GKE [Authorized Networks](https://cloud.google.com/kubernetes-engine/docs/how-to/authorized-networks) 白名单中
- **私有集群**: 需通过 VPN、Cloud Interconnect 或在同一 VPC 内部署服务端

## 安全建议

1. **最小权限原则**: 本文档的自定义 ClusterRole 已限制为只读权限（get/list），无法修改或删除任何集群资源
2. **定期轮换 Token**: 建议每 90 天轮换一次：
   ```bash
   # 删除旧 Secret（Token 立即失效）
   kubectl delete secret mxsec-sa-token -n mxsec
   # 重新创建（参考步骤 5）
   # 在矩阵云安全平台中更新集群的 kubeconfig
   ```
3. **审计日志**: 在 GKE 中启用 Admin Activity 审计日志，监控 ServiceAccount 的 API 调用
4. **独立 ServiceAccount**: 每个集群使用独立的 ServiceAccount，便于追踪和撤销

## 故障排查

| 问题 | 可能原因 | 解决方法 |
|------|----------|----------|
| 连接超时 | 网络不通 | 检查防火墙和 Authorized Networks 配置 |
| 401 Unauthorized | Token 无效或已失效 | 检查 Secret 和 ServiceAccount 是否存在 |
| 403 Forbidden: cannot list resource "nodes" | 使用了 `view` 而非自定义 ClusterRole | 按步骤 3 创建 `mxsec-readonly` ClusterRole 并重新绑定 |
| 403 Forbidden: cannot list resource "clusterroles" | RBAC 权限缺失 | 检查 ClusterRole 是否包含 `rbac.authorization.k8s.io` 资源 |
| 500 查询节点列表失败 | ServiceAccount 无 nodes list 权限 | 运行步骤 7 的权限验证命令排查 |
| x509 证书错误 | CA 证书不匹配 | 重新获取 CA 证书并更新 kubeconfig |
| 基线检查部分项目报错 | 缺少特定资源权限 | 对比步骤 3 的 ClusterRole 定义，确保所有资源已包含 |

## 清理资源

如需移除接入，按以下顺序清理：

```bash
# 1. 先在矩阵云安全平台中删除集群
# 2. 删除 K8s 资源
kubectl delete clusterrolebinding mxsec-binding
kubectl delete clusterrole mxsec-readonly
kubectl delete namespace mxsec
```
