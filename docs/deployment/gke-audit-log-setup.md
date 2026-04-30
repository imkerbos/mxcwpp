# GKE 审计日志接入指南

GKE 集群的 apiserver 由 Google 托管，无法直接配置 `--audit-webhook-config-file`。审计日志通过 Cloud Logging 自动采集，需要通过 **Cloud Logging → Pub/Sub → 矩阵云安全平台** 的链路将审计事件接入平台。

## 架构说明

```
GKE Apiserver
    ↓ (自动)
Cloud Logging (Admin Activity / Data Access 审计日志)
    ↓ (Log Router Sink)
Pub/Sub Topic
    ↓ (Subscription)
矩阵云安全平台 Manager (Pub/Sub Consumer)
    ↓
KubeEvent (安全事件) + KubeAlarm (安全告警)
```

## 选择你的部署方式

根据矩阵云安全平台的部署位置，选择对应的方式，**从头到尾跟着走即可**：

| 部署方式 | 选择 | 是否需要密钥文件 |
|----------|------|------------------|
| 平台部署在 **GCE 虚拟机**上（与 GKE 同项目/网络打通） | [方式 A](#方式-a平台部署在-gce-上推荐) | 否 |
| 平台部署在 **GKE 内部**（作为 Pod 运行） | [方式 B](#方式-b平台部署在-gke-内部) | 否 |
| 平台部署在 **GCP 外部**（自建 IDC / 其他云） | [方式 C](#方式-c平台部署在-gcp-外部) | 是 |

---

## 方式 A：平台部署在 GCE 上（推荐）

适用场景：平台运行在 GCE 虚拟机上，与 GKE 网络互通。直接使用 GCE 实例自带的 Service Account，无需创建额外 SA 或管理密钥文件。

### 前置条件

- 拥有 GCP 项目的 Owner 或 Editor 权限
- 已安装 `gcloud` CLI 并完成认证
- GKE 集群已接入矩阵云安全平台（集群名称需与 GKE 一致）

### 步骤 1. 设置环境变量

```bash
export PROJECT_ID="your-gcp-project-id"
export TOPIC_NAME="mxsec-k8s-audit"
export SUB_NAME="mxsec-k8s-audit-sub"
export SINK_NAME="mxsec-k8s-audit-sink"
```

### 步骤 2. 创建 Pub/Sub Topic

```bash
gcloud pubsub topics create $TOPIC_NAME --project=$PROJECT_ID
```

### 步骤 3. 创建 Pub/Sub Subscription

```bash
gcloud pubsub subscriptions create $SUB_NAME \
    --topic=$TOPIC_NAME \
    --project=$PROJECT_ID \
    --ack-deadline=60 \
    --message-retention-duration=7d
```

### 步骤 4. 创建 Cloud Logging Log Router Sink

将 GKE 审计日志转发到 Pub/Sub Topic：

```bash
gcloud logging sinks create $SINK_NAME \
    "pubsub.googleapis.com/projects/$PROJECT_ID/topics/$TOPIC_NAME" \
    --project=$PROJECT_ID \
    --log-filter='resource.type="k8s_cluster" AND (logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Factivity" OR logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Fdata_access")'
```

> **说明**: 默认只有 Admin Activity 审计日志开启。如需接收读操作（如 `kubectl get secrets`），需额外开启 Data Access 日志（见步骤 6）。

### 步骤 5. 授权 Sink Service Account

Log Router Sink 需要向 Pub/Sub Topic 发布消息的权限：

```bash
# 获取 Sink 的 writer identity
SINK_SA=$(gcloud logging sinks describe $SINK_NAME \
    --project=$PROJECT_ID \
    --format='value(writerIdentity)')

echo "Sink Service Account: $SINK_SA"

# 授权 pubsub.publisher 角色
gcloud pubsub topics add-iam-policy-binding $TOPIC_NAME \
    --project=$PROJECT_ID \
    --member="$SINK_SA" \
    --role="roles/pubsub.publisher"
```

### 步骤 6. 启用 Data Access 审计日志（可选）

默认情况下，GKE 只记录 Admin Activity（写操作）审计日志。如需检测读取 Secret 等操作，需手动启用：

1. 打开 GCP Console → IAM & Admin → Audit Logs
2. 找到 **Kubernetes Engine API**
3. 勾选 **Data Read** 和 **Data Write**
4. 保存

> **注意**: Data Access 日志量较大，可能产生额外费用。建议仅在需要时启用。

### 步骤 7. 授权 GCE 实例 Service Account 读取 Pub/Sub

```bash
# 查看平台所在 GCE 实例的 Service Account
GCE_SA=$(gcloud compute instances describe INSTANCE_NAME \
    --zone=ZONE \
    --project=$PROJECT_ID \
    --format='value(serviceAccounts[0].email)')

echo "GCE Service Account: $GCE_SA"

# 授权该 SA 读取 Pub/Sub Subscription
gcloud pubsub subscriptions add-iam-policy-binding $SUB_NAME \
    --project=$PROJECT_ID \
    --member="serviceAccount:$GCE_SA" \
    --role="roles/pubsub.subscriber"
```

> **注意**: 将 `INSTANCE_NAME` 和 `ZONE` 替换为平台所在 GCE 实例的名称和可用区。如果实例使用的是默认 Compute Engine SA（`PROJECT_NUMBER-compute@developer.gserviceaccount.com`），同样适用。确保实例创建时未限制 access scope（或至少包含 `https://www.googleapis.com/auth/pubsub`）。

### 步骤 8. 配置矩阵云安全平台

在 `server.yaml` 中添加 GCP 配置段：

```yaml
gcp:
  enabled: true
  project_id: "your-gcp-project-id"
  credentials_file: ""    # 留空，GCE 上自动使用实例 SA 的 ADC 认证
  pubsub:
    subscription: "mxsec-k8s-audit-sub"
    max_outstanding_messages: 100
```

重启 Manager 服务后，日志中应出现：

```
GCP Pub/Sub 消费者启动中  {"project_id": "your-project", "subscription": "mxsec-k8s-audit-sub"}
GCP Pub/Sub 消费者已启动，开始接收消息
```

### 步骤 9. 验证

在 GKE 集群中执行一个会触发告警的操作：

```bash
# 这会触发 K8S-001 规则（kubectl exec 进入容器）
kubectl exec -it <pod-name> -- /bin/sh
```

等待 1-2 分钟后检查：
1. Cloud Logging 中有对应的审计日志
2. Pub/Sub Subscription 的消息数增加
3. 矩阵云安全平台「安全告警」页面出现新告警

---

## 方式 B：平台部署在 GKE 内部

适用场景：矩阵云安全平台本身作为 Pod 运行在 GKE 集群上。使用 Workload Identity 免密钥认证。

### 前置条件

- 拥有 GCP 项目的 Owner 或 Editor 权限
- 已安装 `gcloud` CLI 和 `kubectl` 并完成认证
- GKE 集群已启用 Workload Identity（`--workload-pool=$PROJECT_ID.svc.id.goog`）
- GKE 集群已接入矩阵云安全平台（集群名称需与 GKE 一致）

### 步骤 1. 设置环境变量

```bash
export PROJECT_ID="your-gcp-project-id"
export TOPIC_NAME="mxsec-k8s-audit"
export SUB_NAME="mxsec-k8s-audit-sub"
export SINK_NAME="mxsec-k8s-audit-sink"
```

### 步骤 2. 创建 Pub/Sub Topic

```bash
gcloud pubsub topics create $TOPIC_NAME --project=$PROJECT_ID
```

### 步骤 3. 创建 Pub/Sub Subscription

```bash
gcloud pubsub subscriptions create $SUB_NAME \
    --topic=$TOPIC_NAME \
    --project=$PROJECT_ID \
    --ack-deadline=60 \
    --message-retention-duration=7d
```

### 步骤 4. 创建 Cloud Logging Log Router Sink

将 GKE 审计日志转发到 Pub/Sub Topic：

```bash
gcloud logging sinks create $SINK_NAME \
    "pubsub.googleapis.com/projects/$PROJECT_ID/topics/$TOPIC_NAME" \
    --project=$PROJECT_ID \
    --log-filter='resource.type="k8s_cluster" AND (logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Factivity" OR logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Fdata_access")'
```

> **说明**: 默认只有 Admin Activity 审计日志开启。如需接收读操作（如 `kubectl get secrets`），需额外开启 Data Access 日志（见步骤 6）。

### 步骤 5. 授权 Sink Service Account

Log Router Sink 需要向 Pub/Sub Topic 发布消息的权限：

```bash
# 获取 Sink 的 writer identity
SINK_SA=$(gcloud logging sinks describe $SINK_NAME \
    --project=$PROJECT_ID \
    --format='value(writerIdentity)')

echo "Sink Service Account: $SINK_SA"

# 授权 pubsub.publisher 角色
gcloud pubsub topics add-iam-policy-binding $TOPIC_NAME \
    --project=$PROJECT_ID \
    --member="$SINK_SA" \
    --role="roles/pubsub.publisher"
```

### 步骤 6. 启用 Data Access 审计日志（可选）

默认情况下，GKE 只记录 Admin Activity（写操作）审计日志。如需检测读取 Secret 等操作，需手动启用：

1. 打开 GCP Console → IAM & Admin → Audit Logs
2. 找到 **Kubernetes Engine API**
3. 勾选 **Data Read** 和 **Data Write**
4. 保存

> **注意**: Data Access 日志量较大，可能产生额外费用。建议仅在需要时启用。

### 步骤 7. 创建 GCP Service Account 并绑定 Workload Identity

```bash
# 1. 创建 GCP Service Account
gcloud iam service-accounts create mxsec-pubsub-reader \
    --project=$PROJECT_ID \
    --display-name="MxSec Pub/Sub Reader"

# 2. 授权 Subscription 读取权限
gcloud pubsub subscriptions add-iam-policy-binding $SUB_NAME \
    --project=$PROJECT_ID \
    --member="serviceAccount:mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/pubsub.subscriber"

# 3. 绑定 Workload Identity
# 将 NAMESPACE 和 KSA_NAME 替换为平台在 K8s 中的命名空间和 ServiceAccount 名称
gcloud iam service-accounts add-iam-policy-binding \
    "mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com" \
    --member="serviceAccount:$PROJECT_ID.svc.id.goog[NAMESPACE/KSA_NAME]" \
    --role="roles/iam.workloadIdentityUser"

# 4. 在 K8s ServiceAccount 上添加注解
kubectl annotate serviceaccount KSA_NAME \
    --namespace NAMESPACE \
    iam.gke.io/gcp-service-account=mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com
```

> **注意**: 将 `NAMESPACE` 替换为平台 Pod 所在的命名空间，`KSA_NAME` 替换为平台 Pod 使用的 Kubernetes ServiceAccount 名称。

### 步骤 8. 配置矩阵云安全平台

在 `server.yaml` 中添加 GCP 配置段：

```yaml
gcp:
  enabled: true
  project_id: "your-gcp-project-id"
  credentials_file: ""    # 留空，Workload Identity 自动认证
  pubsub:
    subscription: "mxsec-k8s-audit-sub"
    max_outstanding_messages: 100
```

重启 Manager 服务（重新部署 Pod）后，日志中应出现：

```
GCP Pub/Sub 消费者启动中  {"project_id": "your-project", "subscription": "mxsec-k8s-audit-sub"}
GCP Pub/Sub 消费者已启动，开始接收消息
```

### 步骤 9. 验证

在 GKE 集群中执行一个会触发告警的操作：

```bash
# 这会触发 K8S-001 规则（kubectl exec 进入容器）
kubectl exec -it <pod-name> -- /bin/sh
```

等待 1-2 分钟后检查：
1. Cloud Logging 中有对应的审计日志
2. Pub/Sub Subscription 的消息数增加
3. 矩阵云安全平台「安全告警」页面出现新告警

---

## 方式 C：平台部署在 GCP 外部

适用场景：平台部署在自建 IDC、其他云厂商（AWS/Azure/阿里云等）等非 GCP 环境。需要创建专用 Service Account 并导出 JSON Key。

### 前置条件

- 拥有 GCP 项目的 Owner 或 Editor 权限
- 已安装 `gcloud` CLI 并完成认证
- 平台服务器网络可达 Google API 端点（`pubsub.googleapis.com`）
- GKE 集群已接入矩阵云安全平台（集群名称需与 GKE 一致）

### 步骤 1. 设置环境变量

```bash
export PROJECT_ID="your-gcp-project-id"
export TOPIC_NAME="mxsec-k8s-audit"
export SUB_NAME="mxsec-k8s-audit-sub"
export SINK_NAME="mxsec-k8s-audit-sink"
```

### 步骤 2. 创建 Pub/Sub Topic

```bash
gcloud pubsub topics create $TOPIC_NAME --project=$PROJECT_ID
```

### 步骤 3. 创建 Pub/Sub Subscription

```bash
gcloud pubsub subscriptions create $SUB_NAME \
    --topic=$TOPIC_NAME \
    --project=$PROJECT_ID \
    --ack-deadline=60 \
    --message-retention-duration=7d
```

### 步骤 4. 创建 Cloud Logging Log Router Sink

将 GKE 审计日志转发到 Pub/Sub Topic：

```bash
gcloud logging sinks create $SINK_NAME \
    "pubsub.googleapis.com/projects/$PROJECT_ID/topics/$TOPIC_NAME" \
    --project=$PROJECT_ID \
    --log-filter='resource.type="k8s_cluster" AND (logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Factivity" OR logName="projects/'"$PROJECT_ID"'/logs/cloudaudit.googleapis.com%2Fdata_access")'
```

> **说明**: 默认只有 Admin Activity 审计日志开启。如需接收读操作（如 `kubectl get secrets`），需额外开启 Data Access 日志（见步骤 6）。

### 步骤 5. 授权 Sink Service Account

Log Router Sink 需要向 Pub/Sub Topic 发布消息的权限：

```bash
# 获取 Sink 的 writer identity
SINK_SA=$(gcloud logging sinks describe $SINK_NAME \
    --project=$PROJECT_ID \
    --format='value(writerIdentity)')

echo "Sink Service Account: $SINK_SA"

# 授权 pubsub.publisher 角色
gcloud pubsub topics add-iam-policy-binding $TOPIC_NAME \
    --project=$PROJECT_ID \
    --member="$SINK_SA" \
    --role="roles/pubsub.publisher"
```

### 步骤 6. 启用 Data Access 审计日志（可选）

默认情况下，GKE 只记录 Admin Activity（写操作）审计日志。如需检测读取 Secret 等操作，需手动启用：

1. 打开 GCP Console → IAM & Admin → Audit Logs
2. 找到 **Kubernetes Engine API**
3. 勾选 **Data Read** 和 **Data Write**
4. 保存

> **注意**: Data Access 日志量较大，可能产生额外费用。建议仅在需要时启用。

### 步骤 7. 创建 Service Account 并生成 JSON Key

```bash
# 1. 创建 Service Account
gcloud iam service-accounts create mxsec-pubsub-reader \
    --project=$PROJECT_ID \
    --display-name="MxSec Pub/Sub Reader"

# 2. 授权 Subscription 读取权限
gcloud pubsub subscriptions add-iam-policy-binding $SUB_NAME \
    --project=$PROJECT_ID \
    --member="serviceAccount:mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/pubsub.subscriber"

# 3. 生成 JSON Key
gcloud iam service-accounts keys create mxsec-pubsub-key.json \
    --iam-account="mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com"

echo "密钥已保存到 mxsec-pubsub-key.json"
```

将 `mxsec-pubsub-key.json` 文件传输到平台服务器上的安全位置（如 `/etc/mxsec/mxsec-pubsub-key.json`）。

> **安全建议**: JSON Key 具有永久有效性，请妥善保管。建议定期轮换密钥（`gcloud iam service-accounts keys create` 创建新 Key 后删除旧 Key）。

### 步骤 8. 配置矩阵云安全平台

在 `server.yaml` 中添加 GCP 配置段：

```yaml
gcp:
  enabled: true
  project_id: "your-gcp-project-id"
  credentials_file: "/etc/mxsec/mxsec-pubsub-key.json"    # 指定 JSON Key 文件路径
  pubsub:
    subscription: "mxsec-k8s-audit-sub"
    max_outstanding_messages: 100
```

重启 Manager 服务后，日志中应出现：

```
GCP Pub/Sub 消费者启动中  {"project_id": "your-project", "subscription": "mxsec-k8s-audit-sub"}
GCP Pub/Sub 消费者已启动，开始接收消息
```

### 步骤 9. 验证

在 GKE 集群中执行一个会触发告警的操作：

```bash
# 这会触发 K8S-001 规则（kubectl exec 进入容器）
kubectl exec -it <pod-name> -- /bin/sh
```

等待 1-2 分钟后检查：
1. Cloud Logging 中有对应的审计日志
2. Pub/Sub Subscription 的消息数增加
3. 矩阵云安全平台「安全告警」页面出现新告警

---

## 重要说明

### 集群名称匹配

平台通过 GKE 审计日志中的 `resource.labels.cluster_name` 查找对应的集群记录。**请确保平台中接入的集群名称与 GKE 集群名称一致**。

### 多集群支持

一个 Pub/Sub Subscription 可以接收同一 GCP 项目下所有 GKE 集群的审计日志。平台会自动按 `cluster_name` 分发到对应的集群记录。

### 费用说明

| 组件 | 免费额度 | 超出后费用 |
|------|----------|------------|
| Cloud Logging | 前 50 GiB/月 | $0.50/GiB |
| Pub/Sub | 前 10 GiB/月 | $0.04/GiB |
| Data Access 日志 | 不在免费额度内 | 按 Cloud Logging 计费 |

Admin Activity 审计日志**免费且不可关闭**。Data Access 审计日志按量计费。

## 故障排查

| 问题 | 可能原因 | 解决方法 |
|------|----------|----------|
| 平台无告警 | Sink 未正确创建 | `gcloud logging sinks list` 检查 Sink 状态 |
| Pub/Sub 无消息 | Sink SA 无发布权限 | 检查步骤 5 的 IAM 绑定 |
| 消费者连接失败 | 凭据无效 | 方式 A: 检查 GCE access scope；方式 B: 检查 Workload Identity 绑定和注解；方式 C: 检查 JSON Key 路径和权限 |
| 只有写操作告警 | 未启用 Data Access 日志 | 参考步骤 6 启用 |
| 集群不匹配 | 平台集群名与 GKE 不一致 | 确保平台中的集群名称与 GKE 完全一致 |

## 清理资源

如需移除审计日志接入：

```bash
# 删除 Log Router Sink
gcloud logging sinks delete $SINK_NAME --project=$PROJECT_ID

# 删除 Pub/Sub Subscription 和 Topic
gcloud pubsub subscriptions delete $SUB_NAME --project=$PROJECT_ID
gcloud pubsub topics delete $TOPIC_NAME --project=$PROJECT_ID

# 删除 Service Account（方式 B 和 C 创建了专用 SA）
gcloud iam service-accounts delete \
    "mxsec-pubsub-reader@$PROJECT_ID.iam.gserviceaccount.com" --project=$PROJECT_ID
```

同时在 `server.yaml` 中设置 `gcp.enabled: false` 并重启 Manager。
