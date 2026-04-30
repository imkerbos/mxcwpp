# 自建 K8s 集群审计日志接入指南

本文档介绍如何为自建 Kubernetes 集群（kubeadm、RKE、k3s 等）配置 Audit Webhook，将 apiserver 审计日志实时推送到矩阵云安全平台，实现容器安全事件检测和告警。

> **适用范围**: 可以自行配置 apiserver 启动参数的集群。GKE 等托管集群请参考 [GKE 审计日志接入指南](gke-audit-log-setup.md)。

## 架构说明

```
K8s Apiserver
    ↓ (audit-webhook-config-file)
矩阵云安全平台 Manager (HTTP Webhook Endpoint)
    ↓
KubeEvent (安全事件) + KubeAlarm (安全告警)
```

平台通过 Webhook 接收 apiserver 的审计日志，经过规则引擎检测后生成安全事件和告警。当前支持的检测规则：

| 规则 ID | 名称 | 说明 |
|---------|------|------|
| K8S-001 | kubectl exec 进入容器 | 检测对容器执行 exec/attach 操作 |
| K8S-002 | 创建 hostNetwork/hostPID Pod | 检测使用宿主机网络或 PID 命名空间的 Pod |
| K8S-003 | ClusterRole 绑定高权限 | 检测 cluster-admin 等高权限绑定 |
| K8S-004 | 访问 Secret 资源 | 检测对 Secret 的读取操作 |
| K8S-005 | 创建特权容器 | 检测 privileged 容器 |
| K8S-006 | ServiceAccount Token 异常使用 | 检测 SA Token 的异常访问模式 |
| K8S-007 | 容器内反弹 Shell 迹象 | 检测可能的反弹 Shell 行为 |
| K8S-008 | 挂载宿主机路径 | 检测容器逃逸相关的宿主机目录挂载 |

## 前置条件

- 集群已接入矩阵云安全平台（通过「容器安全 → 集群管理 → 接入集群」完成）
- 拥有 apiserver 所在节点的 root 或 sudo 权限
- apiserver 节点网络可达矩阵云安全平台 Manager 服务

## 操作步骤

### 1. 获取 Webhook 配置信息

在矩阵云安全平台中获取 Webhook URL 和 Audit Token：

**方式 A：集群创建时获取**

创建集群成功后，弹窗会展示 Webhook URL 和 Audit Token，请妥善保存。

**方式 B：集群详情页获取**

进入「容器安全 → 集群管理」→ 点击目标集群 → 概览 tab → 底部「Audit Webhook 配置」区域。

该区域展示：
- **Webhook URL**：`https://<平台地址>/api/v1/kube/audit-webhook/<token>`
- **Audit Token**：点击眼睛图标查看完整 Token，点击复制按钮复制
- **Webhook 配置文件**：可直接复制使用的 kubeconfig 格式配置

> **Token 丢失或泄露**: 点击「重新生成」按钮可生成新的 Audit Token，旧 Token 立即失效，需同步更新 apiserver 的 webhook 配置文件。

### 2. 创建审计策略文件

在 apiserver 节点上创建审计策略文件：

```bash
sudo cat > /etc/kubernetes/audit-policy.yaml << 'EOF'
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  # 记录敏感资源的写操作（完整请求体 + 响应体）
  - level: RequestResponse
    resources:
      - group: ""
        resources: ["pods", "services", "secrets", "configmaps"]
      - group: "apps"
        resources: ["deployments", "daemonsets", "statefulsets"]
      - group: "rbac.authorization.k8s.io"
        resources: ["clusterroles", "clusterrolebindings", "roles", "rolebindings"]
    verbs: ["create", "update", "patch", "delete"]

  # 记录容器交互操作（exec / attach / portforward）
  - level: Request
    resources:
      - group: ""
        resources: ["pods/exec", "pods/attach", "pods/portforward"]

  # 排除高频低价值事件
  - level: None
    users: ["system:kube-proxy"]
  - level: None
    resources:
      - group: ""
        resources: ["endpoints", "events"]
  - level: None
    nonResourceURLs: ["/healthz*", "/readyz*", "/livez*"]

  # 其余操作记录 Metadata
  - level: Metadata
EOF
```

> **策略调整说明**: 上述策略已针对平台检测规则优化。如果不需要 K8S-004（Secret 访问检测），可将 `secrets` 从 RequestResponse 规则中移除以减少日志量。

### 3. 创建 Webhook 配置文件

在 apiserver 节点上创建 Webhook 配置文件（kubeconfig 格式）：

```bash
# 替换为步骤 1 中获取的 Webhook URL
WEBHOOK_URL="https://mxsec.example.com/api/v1/kube/audit-webhook/YOUR_TOKEN"

sudo cat > /etc/kubernetes/audit-webhook.yaml << EOF
apiVersion: v1
kind: Config
clusters:
- name: mxsec-audit
  cluster:
    server: "${WEBHOOK_URL}"
    insecure-skip-tls-verify: true
contexts:
- name: mxsec-audit
  context:
    cluster: mxsec-audit
current-context: mxsec-audit
EOF
```

> **TLS 说明**: 如果平台使用了受信任的证书，可将 `insecure-skip-tls-verify: true` 替换为 `certificate-authority: /path/to/ca.crt`。

### 4. 修改 apiserver 启动参数

#### kubeadm 集群

编辑 apiserver 静态 Pod manifest：

```bash
sudo vim /etc/kubernetes/manifests/kube-apiserver.yaml
```

在 `spec.containers[0].command` 中添加以下参数：

```yaml
    - --audit-policy-file=/etc/kubernetes/audit-policy.yaml
    - --audit-webhook-config-file=/etc/kubernetes/audit-webhook.yaml
    - --audit-webhook-batch-max-wait=5s
```

确保审计文件已挂载到 apiserver Pod，在 `spec.containers[0].volumeMounts` 中添加：

```yaml
    - mountPath: /etc/kubernetes/audit-policy.yaml
      name: audit-policy
      readOnly: true
    - mountPath: /etc/kubernetes/audit-webhook.yaml
      name: audit-webhook
      readOnly: true
```

在 `spec.volumes` 中添加：

```yaml
  - hostPath:
      path: /etc/kubernetes/audit-policy.yaml
      type: File
    name: audit-policy
  - hostPath:
      path: /etc/kubernetes/audit-webhook.yaml
      type: File
    name: audit-webhook
```

保存后 kubelet 会自动重启 apiserver（通常 1-2 分钟）。

#### k3s 集群

```bash
# 创建配置目录（如不存在）
sudo mkdir -p /etc/rancher/k3s

# 修改 k3s 服务配置
sudo vim /etc/systemd/system/k3s.service

# 在 ExecStart 中添加参数：
# --kube-apiserver-arg=audit-policy-file=/etc/kubernetes/audit-policy.yaml
# --kube-apiserver-arg=audit-webhook-config-file=/etc/kubernetes/audit-webhook.yaml
# --kube-apiserver-arg=audit-webhook-batch-max-wait=5s

# 重启 k3s
sudo systemctl daemon-reload
sudo systemctl restart k3s
```

#### RKE / RKE2 集群

在 `cluster.yml`（RKE）或 `/etc/rancher/rke2/config.yaml`（RKE2）中添加：

**RKE**:
```yaml
services:
  kube-api:
    extra_args:
      audit-policy-file: /etc/kubernetes/audit-policy.yaml
      audit-webhook-config-file: /etc/kubernetes/audit-webhook.yaml
      audit-webhook-batch-max-wait: 5s
    extra_binds:
      - "/etc/kubernetes/audit-policy.yaml:/etc/kubernetes/audit-policy.yaml:ro"
      - "/etc/kubernetes/audit-webhook.yaml:/etc/kubernetes/audit-webhook.yaml:ro"
```

**RKE2**:
```yaml
kube-apiserver-arg:
  - audit-policy-file=/etc/kubernetes/audit-policy.yaml
  - audit-webhook-config-file=/etc/kubernetes/audit-webhook.yaml
  - audit-webhook-batch-max-wait=5s
```

### 5. 验证

#### 检查 apiserver 是否正常重启

```bash
# kubeadm
kubectl get pods -n kube-system -l component=kube-apiserver

# 检查 apiserver 日志是否有 audit 相关错误
kubectl logs -n kube-system kube-apiserver-<node-name> | grep -i audit | tail -5
```

#### 触发测试告警

```bash
# 执行 exec 进入容器，触发 K8S-001 规则
kubectl exec -it <pod-name> -- /bin/sh
```

等待 10-30 秒后检查：

1. 矩阵云安全平台「安全事件」页面出现新事件
2. 矩阵云安全平台「安全告警」页面出现 K8S-001 告警

#### 使用 curl 直接测试 Webhook 端点

如果需要验证 Webhook 端点可达性：

```bash
curl -X POST https://mxsec.example.com/api/v1/kube/audit-webhook/YOUR_TOKEN \
  -H 'Content-Type: application/json' \
  -d '{
    "apiVersion": "audit.k8s.io/v1",
    "kind": "EventList",
    "items": [{
      "verb": "create",
      "objectRef": {
        "resource": "pods",
        "subresource": "exec",
        "name": "test-pod",
        "namespace": "default"
      },
      "user": {"username": "admin"},
      "stage": "ResponseComplete",
      "sourceIPs": ["1.2.3.4"]
    }]
  }'
```

预期返回：`{"received": 1}`

## 配置说明

### server.yaml 中的 external_url

平台展示的 Webhook URL 依赖 `server.yaml` 中的 `external_url` 配置：

```yaml
server:
  external_url: "https://mxsec.example.com"  # 平台公网访问地址
  http:
    host: "0.0.0.0"
    port: 8080
```

- 已配置 `external_url`：Webhook URL 为 `https://mxsec.example.com/api/v1/kube/audit-webhook/<token>`
- 未配置 `external_url`：Webhook URL 回退为 `http://<host>:<port>/api/v1/kube/audit-webhook/<token>`

生产环境建议配置 `external_url`，确保 Webhook URL 中的地址是集群可达的。

### Webhook 批处理参数

| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `--audit-webhook-batch-max-wait` | 批量发送最大等待时间 | `5s` |
| `--audit-webhook-batch-max-size` | 单批最大事件数 | `100`（默认） |
| `--audit-webhook-batch-buffer-size` | 内存缓冲区大小 | `10000`（默认） |
| `--audit-webhook-truncate-max-batch-size` | 截断后的批最大字节 | `10485760`（默认 10MB） |

大规模集群可适当调大 `batch-buffer-size` 防止日志丢失。

## 故障排查

| 问题 | 可能原因 | 解决方法 |
|------|----------|----------|
| apiserver 无法启动 | 审计策略文件语法错误 | 检查 audit-policy.yaml 是否为合法 YAML，apiVersion 是否正确 |
| apiserver 无法启动 | 文件未挂载到 Pod | 检查 volumeMounts 和 volumes 配置 |
| 平台无数据 | Webhook URL 不可达 | 在 apiserver 节点执行 curl 测试端点连通性 |
| 平台无数据 | Token 错误 | 从集群详情页复制最新 Token，更新 webhook 配置文件 |
| 平台有事件但无告警 | 事件不匹配检测规则 | 检查事件的 verb/resource 是否在规则覆盖范围内 |
| 告警延迟较高 | batch-max-wait 过大 | 减小 `--audit-webhook-batch-max-wait` 值 |
| apiserver 日志报 webhook 超时 | 网络延迟或平台负载高 | 检查网络连通性，确认 Manager 服务正常运行 |

## 多 Master 节点

如果集群有多个 Master 节点，**每个节点都需要配置**审计策略和 Webhook 文件。所有 apiserver 实例使用相同的 Webhook URL 和 Token，平台会自动去重。

## 关闭审计日志

如需关闭审计日志接入：

1. 移除 apiserver 的 `--audit-policy-file`、`--audit-webhook-config-file`、`--audit-webhook-batch-max-wait` 参数
2. 移除对应的 volumeMounts 和 volumes
3. 等待 apiserver 自动重启
4. （可选）在平台集群详情页点击「重新生成」Token 使旧 Token 失效
