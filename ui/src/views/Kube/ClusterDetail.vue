<template>
  <div class="kube-detail-page">
    <div class="page-header">
      <h2>
        <a-button type="text" @click="$router.push('/kube/clusters')" style="margin-right: 8px; padding: 0">
          <LeftOutlined />
        </a-button>
        {{ cluster.name || '集群详情' }}
      </h2>
      <div style="display: flex; align-items: center; gap: 12px">
        <span class="status-dot" :class="`dot-${cluster.status}`"></span>
        <a-tag :color="statusColorMap[cluster.status]" :bordered="false">{{ statusTextMap[cluster.status] }}</a-tag>
        <span class="page-header-hint">K8s {{ cluster.version }}</span>
      </div>
    </div>

    <!-- 概览卡片 -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="4" v-for="item in summaryStats" :key="item.key">
        <div class="summary-card">
          <div class="summary-value" :style="{ color: item.color }">{{ item.value }}</div>
          <div class="summary-label">{{ item.label }}</div>
        </div>
      </a-col>
    </a-row>

    <!-- Tab 内容区 -->
    <div class="dashboard-card">
      <a-tabs v-model:activeKey="activeTab">
        <!-- 概览 -->
        <a-tab-pane key="overview" tab="集群概览">
          <a-descriptions :column="3" bordered size="small">
            <a-descriptions-item label="集群名称">{{ cluster.name }}</a-descriptions-item>
            <a-descriptions-item label="API Server">{{ cluster.apiServer }}</a-descriptions-item>
            <a-descriptions-item label="K8s 版本">{{ cluster.version }}</a-descriptions-item>
            <a-descriptions-item label="运行时间">{{ cluster.uptime }}</a-descriptions-item>
            <a-descriptions-item label="接入时间">{{ cluster.createdAt }}</a-descriptions-item>
            <a-descriptions-item label="最后心跳">{{ cluster.lastHeartbeat }}</a-descriptions-item>
            <a-descriptions-item label="备注" :span="3">{{ cluster.remark || '--' }}</a-descriptions-item>
          </a-descriptions>
        </a-tab-pane>

        <!-- Node 列表 -->
        <a-tab-pane key="nodes" tab="Node 节点">
          <a-table :columns="nodeColumns" :data-source="nodes" :loading="loadingNodes" :pagination="false" size="middle" row-key="name">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'status'">
                <a-tag :color="record.status === 'Ready' ? 'green' : 'red'" :bordered="false">{{ record.status }}</a-tag>
              </template>
              <template v-if="column.key === 'cpu'">
                <a-progress :percent="record.cpuPercent" :size="6" :stroke-color="record.cpuPercent > 80 ? '#F53F3F' : '#165DFF'" />
              </template>
              <template v-if="column.key === 'memory'">
                <a-progress :percent="record.memoryPercent" :size="6" :stroke-color="record.memoryPercent > 80 ? '#F53F3F' : '#00B42A'" />
              </template>
            </template>
          </a-table>
        </a-tab-pane>

        <!-- Pod 列表 -->
        <a-tab-pane key="pods" tab="Pod">
          <div class="filter-bar" style="margin-bottom: 16px">
            <a-input-search v-model:value="podSearch" placeholder="搜索 Pod 名称" style="width: 240px" allow-clear @search="loadPods" />
            <a-select v-model:value="podNamespace" style="width: 180px" placeholder="Namespace" allow-clear show-search @change="loadPods">
              <a-select-option v-for="ns in namespaces" :key="ns" :value="ns">{{ ns }}</a-select-option>
            </a-select>
            <a-select v-model:value="podStatus" style="width: 140px" placeholder="状态" allow-clear @change="loadPods">
              <a-select-option value="Running">Running</a-select-option>
              <a-select-option value="Pending">Pending</a-select-option>
              <a-select-option value="Failed">Failed</a-select-option>
              <a-select-option value="Succeeded">Succeeded</a-select-option>
            </a-select>
          </div>
          <a-table :columns="podColumns" :data-source="pods" :loading="loadingPods" :pagination="podPagination" @change="handlePodTableChange" size="middle" row-key="name">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'status'">
                <a-tag :color="podStatusColor[record.status]" :bordered="false">{{ record.status }}</a-tag>
              </template>
              <template v-if="column.key === 'containers'">
                <span>{{ record.readyContainers }}/{{ record.totalContainers }}</span>
              </template>
              <template v-if="column.key === 'restarts'">
                <span :style="{ color: record.restarts > 5 ? '#F53F3F' : '#1D2129' }">{{ record.restarts }}</span>
              </template>
            </template>
          </a-table>
        </a-tab-pane>

        <!-- Workload -->
        <a-tab-pane key="workloads" tab="Workload">
          <a-table :columns="workloadColumns" :data-source="workloads" :loading="loadingWorkloads" :pagination="false" size="middle" row-key="name">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'type'">
                <a-tag :bordered="false">{{ record.type }}</a-tag>
              </template>
              <template v-if="column.key === 'replicas'">
                <span :style="{ color: record.readyReplicas < record.desiredReplicas ? '#FF7D00' : '#00B42A' }">
                  {{ record.readyReplicas }}/{{ record.desiredReplicas }}
                </span>
              </template>
            </template>
          </a-table>
        </a-tab-pane>

        <!-- 安全风险 -->
        <a-tab-pane key="risks" tab="安全风险">
          <a-row :gutter="[16, 16]" style="margin-bottom: 16px">
            <a-col :span="8">
              <div class="risk-card">
                <div class="risk-value" style="color: #F53F3F">{{ riskStats.alarms }}</div>
                <div class="risk-label">安全告警</div>
              </div>
            </a-col>
            <a-col :span="8">
              <div class="risk-card">
                <div class="risk-value" style="color: #FF7D00">{{ riskStats.events }}</div>
                <div class="risk-label">安全事件</div>
              </div>
            </a-col>
            <a-col :span="8">
              <div class="risk-card">
                <div class="risk-value" style="color: #165DFF">{{ riskStats.baseline }}</div>
                <div class="risk-label">基线问题</div>
              </div>
            </a-col>
          </a-row>
          <a-table :columns="riskColumns" :data-source="risks" :loading="loadingRisks" size="middle" row-key="id">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'severity'">
                <a-tag :color="severityColorMap[record.severity]" :bordered="false">{{ severityTextMap[record.severity] }}</a-tag>
              </template>
              <template v-if="column.key === 'type'">
                <a-tag :bordered="false">{{ record.type }}</a-tag>
              </template>
            </template>
          </a-table>
        </a-tab-pane>
      </a-tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { LeftOutlined } from '@ant-design/icons-vue'
import apiClient from '@/api/client'

const route = useRoute()
const clusterId = route.params.id as string
const activeTab = ref('overview')

const cluster = ref<any>({ name: '', status: 'running', version: '', apiServer: '' })
const nodes = ref<any[]>([])
const pods = ref<any[]>([])
const workloads = ref<any[]>([])
const risks = ref<any[]>([])
const namespaces = ref<string[]>([])
const loadingNodes = ref(false)
const loadingPods = ref(false)
const loadingWorkloads = ref(false)
const loadingRisks = ref(false)

const podSearch = ref('')
const podNamespace = ref<string>()
const podStatus = ref<string>()
const podPagination = ref({ current: 1, pageSize: 20, total: 0, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` })

const summaryStats = ref([
  { key: 'nodes', label: 'Node', value: 0, color: '#165DFF' },
  { key: 'pods', label: 'Pod', value: 0, color: '#00B42A' },
  { key: 'namespaces', label: 'Namespace', value: 0, color: '#722ED1' },
  { key: 'deployments', label: 'Deployment', value: 0, color: '#FF7D00' },
  { key: 'services', label: 'Service', value: 0, color: '#165DFF' },
  { key: 'alarms', label: '安全告警', value: 0, color: '#F53F3F' },
])

const riskStats = ref({ alarms: 0, events: 0, baseline: 0 })

const statusColorMap: Record<string, string> = { running: 'green', warning: 'orange', offline: 'red' }
const statusTextMap: Record<string, string> = { running: '运行中', warning: '异常', offline: '离线' }
const podStatusColor: Record<string, string> = { Running: 'green', Pending: 'orange', Failed: 'red', Succeeded: 'blue' }
const severityColorMap: Record<string, string> = { critical: 'red', high: 'orange', medium: 'gold', low: 'blue' }
const severityTextMap: Record<string, string> = { critical: '紧急', high: '高危', medium: '中危', low: '低危' }

const nodeColumns = [
  { title: '节点名称', dataIndex: 'name', key: 'name', width: 200 },
  { title: '状态', key: 'status', width: 100 },
  { title: '角色', dataIndex: 'roles', key: 'roles', width: 120 },
  { title: 'IP', dataIndex: 'ip', key: 'ip', width: 140 },
  { title: 'OS', dataIndex: 'os', key: 'os', width: 160 },
  { title: 'CPU 使用', key: 'cpu', width: 160 },
  { title: '内存使用', key: 'memory', width: 160 },
  { title: 'Pod 数', dataIndex: 'podCount', key: 'podCount', width: 80 },
  { title: '版本', dataIndex: 'kubeletVersion', key: 'kubeletVersion', width: 120 },
]

const podColumns = [
  { title: 'Pod 名称', dataIndex: 'name', key: 'name', ellipsis: true },
  { title: 'Namespace', dataIndex: 'namespace', key: 'namespace', width: 140 },
  { title: '状态', key: 'status', width: 100 },
  { title: '容器', key: 'containers', width: 80 },
  { title: '重启次数', key: 'restarts', width: 100 },
  { title: '节点', dataIndex: 'nodeName', key: 'nodeName', width: 160 },
  { title: 'IP', dataIndex: 'podIp', key: 'podIp', width: 130 },
  { title: '运行时间', dataIndex: 'age', key: 'age', width: 120 },
]

const workloadColumns = [
  { title: '名称', dataIndex: 'name', key: 'name', width: 200 },
  { title: '类型', key: 'type', width: 120 },
  { title: 'Namespace', dataIndex: 'namespace', key: 'namespace', width: 140 },
  { title: '副本', key: 'replicas', width: 100 },
  { title: '镜像', dataIndex: 'images', key: 'images', ellipsis: true },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 180 },
]

const riskColumns = [
  { title: '风险类型', key: 'type', width: 120 },
  { title: '严重级别', key: 'severity', width: 100 },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '影响对象', dataIndex: 'target', key: 'target', width: 200 },
  { title: '发现时间', dataIndex: 'discoveredAt', key: 'discoveredAt', width: 180 },
]

const loadCluster = async () => {
  try {
    const res = await apiClient.get<any>(`/kube/clusters/${clusterId}`)
    cluster.value = res
    if (res.summary) {
      summaryStats.value = [
        { key: 'nodes', label: 'Node', value: res.summary.nodes ?? 0, color: '#165DFF' },
        { key: 'pods', label: 'Pod', value: res.summary.pods ?? 0, color: '#00B42A' },
        { key: 'namespaces', label: 'Namespace', value: res.summary.namespaces ?? 0, color: '#722ED1' },
        { key: 'deployments', label: 'Deployment', value: res.summary.deployments ?? 0, color: '#FF7D00' },
        { key: 'services', label: 'Service', value: res.summary.services ?? 0, color: '#165DFF' },
        { key: 'alarms', label: '安全告警', value: res.summary.alarms ?? 0, color: '#F53F3F' },
      ]
    }
    if (res.namespaces) namespaces.value = res.namespaces
    if (res.risks) riskStats.value = res.risks
  } catch { /* API 未就绪 */ }
}

const loadNodes = async () => {
  loadingNodes.value = true
  try { const res = await apiClient.get<any>(`/kube/clusters/${clusterId}/nodes`); nodes.value = res.items ?? [] }
  catch { nodes.value = [] }
  finally { loadingNodes.value = false }
}

const loadPods = async () => {
  loadingPods.value = true
  try {
    const res = await apiClient.get<any>(`/kube/clusters/${clusterId}/pods`, {
      params: { page: podPagination.value.current, page_size: podPagination.value.pageSize, search: podSearch.value || undefined, namespace: podNamespace.value || undefined, status: podStatus.value || undefined },
    })
    pods.value = res.items ?? []
    podPagination.value.total = res.total ?? 0
  } catch { pods.value = [] }
  finally { loadingPods.value = false }
}

const loadWorkloads = async () => {
  loadingWorkloads.value = true
  try { const res = await apiClient.get<any>(`/kube/clusters/${clusterId}/workloads`); workloads.value = res.items ?? [] }
  catch { workloads.value = [] }
  finally { loadingWorkloads.value = false }
}

const handlePodTableChange = (pag: any) => { podPagination.value.current = pag.current; podPagination.value.pageSize = pag.pageSize; loadPods() }

onMounted(() => { loadCluster(); loadNodes(); loadPods(); loadWorkloads() })
</script>

<style scoped>
.kube-detail-page { width: 100%; }
.section-row { margin-bottom: 16px; }

.summary-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; padding: 16px; text-align: center; }
.summary-value { font-size: 24px; font-weight: 700; line-height: 1.2; }
.summary-label { font-size: 12px; color: #86909C; margin-top: 4px; }

.risk-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; padding: 20px; text-align: center; }
.risk-value { font-size: 28px; font-weight: 700; line-height: 1.2; }
.risk-label { font-size: 13px; color: #86909C; margin-top: 4px; }

.dashboard-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; padding: 0 20px 20px; }

.filter-bar { display: flex; gap: 8px; align-items: center; padding: 12px 16px; background: #F7F8FA; border-radius: 4px; border: 1px solid #E5E8EF; }

.status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; }
.dot-running { background: #00B42A; box-shadow: 0 0 0 3px rgba(0,180,42,0.15); }
.dot-warning { background: #FF7D00; box-shadow: 0 0 0 3px rgba(255,125,0,0.15); }
.dot-offline { background: #F53F3F; box-shadow: 0 0 0 3px rgba(245,63,63,0.15); }
</style>
