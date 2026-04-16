<template>
  <div class="kube-alarms-page">
    <div class="page-header">
      <h2>容器集群安全告警</h2>
      <span class="page-header-hint">Kubernetes 集群入侵检测告警</span>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="6">
        <div class="alarm-stat critical" @click="filterSeverity = 'critical'; loadAlarms()">
          <div class="alarm-stat-value">{{ stats.critical }}</div>
          <div class="alarm-stat-label">紧急</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="alarm-stat high" @click="filterSeverity = 'high'; loadAlarms()">
          <div class="alarm-stat-value">{{ stats.high }}</div>
          <div class="alarm-stat-label">高危</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="alarm-stat medium" @click="filterSeverity = 'medium'; loadAlarms()">
          <div class="alarm-stat-value">{{ stats.medium }}</div>
          <div class="alarm-stat-label">中危</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="alarm-stat low" @click="filterSeverity = 'low'; loadAlarms()">
          <div class="alarm-stat-value">{{ stats.low }}</div>
          <div class="alarm-stat-label">低危</div>
        </div>
      </a-col>
    </a-row>

    <!-- 表格 -->
    <div class="dashboard-card">
      <div class="card-body">
        <div class="filter-bar">
          <a-input-search v-model:value="searchText" placeholder="搜索告警内容" style="width: 240px" allow-clear @search="loadAlarms" />
          <a-select v-model:value="filterCluster" style="width: 180px" placeholder="集群" allow-clear show-search @change="loadAlarms">
            <a-select-option v-for="c in clusterOptions" :key="c.value" :value="c.value">{{ c.label }}</a-select-option>
          </a-select>
          <a-select v-model:value="filterSeverity" style="width: 120px" placeholder="级别" allow-clear @change="loadAlarms">
            <a-select-option value="critical">紧急</a-select-option>
            <a-select-option value="high">高危</a-select-option>
            <a-select-option value="medium">中危</a-select-option>
            <a-select-option value="low">低危</a-select-option>
          </a-select>
          <a-select v-model:value="filterStatus" style="width: 120px" placeholder="状态" allow-clear @change="loadAlarms">
            <a-select-option value="pending">待处理</a-select-option>
            <a-select-option value="processed">已处理</a-select-option>
            <a-select-option value="ignored">已忽略</a-select-option>
          </a-select>
          <div style="flex: 1"></div>
          <a-button @click="handleBatchIgnore" :disabled="!selectedRowKeys.length">批量忽略</a-button>
          <a-button type="primary" @click="handleBatchProcess" :disabled="!selectedRowKeys.length">批量处理</a-button>
        </div>

        <a-table
          :columns="columns"
          :data-source="alarms"
          :loading="loading"
          :pagination="pagination"
          :row-selection="{ selectedRowKeys, onChange: onSelectChange }"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'severity'">
              <a-tag :color="severityColorMap[record.severity]" :bordered="false">{{ severityTextMap[record.severity] }}</a-tag>
            </template>
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === 'pending' ? 'orange' : record.status === 'processed' ? 'green' : 'default'" :bordered="false">
                {{ statusTextMap[record.status] }}
              </a-tag>
            </template>
            <template v-if="column.key === 'alarmType'">
              <a-tag :color="alarmTypeColorMap[record.alarmType] || 'default'" :bordered="false">{{ alarmTypeTextMap[record.alarmType] || record.alarmType }}</a-tag>
            </template>
            <template v-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="showAlarmDetail(record)">详情</a-button>
                <a-button type="link" size="small" @click="handleProcess(record)" v-if="record.status === 'pending'">处理</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </div>
    </div>

    <!-- 告警详情 Drawer -->
    <a-drawer v-model:open="showDetail" title="告警详情" width="680">
      <template v-if="detailRecord">
        <!-- 告警标题 -->
        <div class="alarm-detail-header">
          <a-tag :color="severityColorMap[detailRecord.severity]" :bordered="false" class="severity-tag">{{ severityTextMap[detailRecord.severity] }}</a-tag>
          <span class="alarm-detail-title">{{ detailRecord.title }}</span>
        </div>

        <!-- 告警摘要 -->
        <div class="alarm-detail-message">{{ detailRecord.message }}</div>

        <!-- 规则说明 -->
        <div class="alarm-detail-section" v-if="detailRecord.description">
          <div class="section-label">规则说明</div>
          <div class="section-content">{{ detailRecord.description }}</div>
        </div>

        <!-- 处置建议 -->
        <div class="alarm-detail-section remediation" v-if="detailRecord.remediation">
          <div class="section-label">处置建议</div>
          <div class="section-content remediation-content">{{ detailRecord.remediation }}</div>
        </div>

        <a-divider style="margin: 16px 0" />

        <!-- 基本信息 -->
        <a-descriptions :column="2" bordered size="small">
          <a-descriptions-item label="告警 ID">{{ detailRecord.id }}</a-descriptions-item>
          <a-descriptions-item label="告警类型">
            <a-tag :color="alarmTypeColorMap[detailRecord.alarmType] || 'default'" :bordered="false">{{ alarmTypeTextMap[detailRecord.alarmType] || detailRecord.alarmType }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="集群">{{ detailRecord.clusterName }}</a-descriptions-item>
          <a-descriptions-item label="Namespace">{{ detailRecord.namespace || '-' }}</a-descriptions-item>
          <a-descriptions-item label="影响对象" :span="2">{{ detailRecord.target || '-' }}</a-descriptions-item>
          <a-descriptions-item label="发现时间">{{ detailRecord.createdAt }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag :color="detailRecord.status === 'pending' ? 'orange' : detailRecord.status === 'processed' ? 'green' : 'default'" :bordered="false">
              {{ statusTextMap[detailRecord.status] }}
            </a-tag>
          </a-descriptions-item>
        </a-descriptions>

        <a-divider v-if="detailRecord.rawData" style="margin: 16px 0">原始审计事件</a-divider>
        <pre v-if="detailRecord.rawData" class="raw-json">{{ JSON.stringify(detailRecord.rawData, null, 2) }}</pre>
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import apiClient from '@/api/client'

const searchText = ref('')
const filterCluster = ref<string>()
const filterSeverity = ref<string>()
const filterStatus = ref<string>()
const loading = ref(false)
const alarms = ref<any[]>([])
const clusterOptions = ref<any[]>([])
const selectedRowKeys = ref<string[]>([])
const showDetail = ref(false)
const detailRecord = ref<any>(null)
const stats = ref({ critical: 0, high: 0, medium: 0, low: 0 })

const pagination = ref({ current: 1, pageSize: 20, total: 0, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` })

const severityColorMap: Record<string, string> = { critical: 'red', high: 'orange', medium: 'gold', low: 'blue' }
const severityTextMap: Record<string, string> = { critical: '紧急', high: '高危', medium: '中危', low: '低危' }
const statusTextMap: Record<string, string> = { pending: '待处理', processed: '已处理', ignored: '已忽略' }
const alarmTypeTextMap: Record<string, string> = {
  container_escape: '容器逃逸',
  abnormal_process: '异常进程',
  abnormal_network: '异常网络',
  file_tamper: '文件篡改',
  privilege_escalation: '权限提升',
  reverse_shell: '反弹 Shell',
  crypto_mining: '挖矿行为',
}
const alarmTypeColorMap: Record<string, string> = {
  container_escape: 'red',
  abnormal_process: 'orange',
  abnormal_network: 'purple',
  file_tamper: 'gold',
  privilege_escalation: 'red',
  reverse_shell: 'red',
  crypto_mining: 'volcano',
}

const columns = [
  { title: '告警时间', dataIndex: 'createdAt', key: 'createdAt', width: 180 },
  { title: '级别', key: 'severity', width: 80 },
  { title: '集群', dataIndex: 'clusterName', key: 'clusterName', width: 140 },
  { title: '告警类型', key: 'alarmType', width: 120 },
  { title: '告警标题', dataIndex: 'title', key: 'title', width: 260, ellipsis: true },
  { title: '告警摘要', dataIndex: 'message', key: 'message', ellipsis: true },
  { title: '状态', key: 'status', width: 100 },
  { title: '操作', key: 'action', width: 130 },
]

const onSelectChange = (keys: string[]) => { selectedRowKeys.value = keys }

const loadAlarms = async () => {
  loading.value = true
  try {
    const res = await apiClient.get<any>('/kube/alarms', {
      params: { page: pagination.value.current, page_size: pagination.value.pageSize, search: searchText.value || undefined, cluster_id: filterCluster.value || undefined, severity: filterSeverity.value || undefined, status: filterStatus.value || undefined },
    })
    alarms.value = res.items ?? []
    pagination.value.total = res.total ?? 0
    if (res.stats) stats.value = res.stats
  } catch { alarms.value = [] }
  finally { loading.value = false }
}

const handleTableChange = (pag: any) => { pagination.value.current = pag.current; pagination.value.pageSize = pag.pageSize; loadAlarms() }
const showAlarmDetail = (record: any) => { detailRecord.value = record; showDetail.value = true }
const handleProcess = async (record: any) => { try { await apiClient.post(`/kube/alarms/${record.id}/process`); message.success('已处理'); loadAlarms() } catch { message.error('操作失败') } }
const handleBatchProcess = async () => { try { await apiClient.post('/kube/alarms/batch-process', { ids: selectedRowKeys.value }); message.success('批量处理成功'); selectedRowKeys.value = []; loadAlarms() } catch { message.error('操作失败') } }
const handleBatchIgnore = async () => { try { await apiClient.post('/kube/alarms/batch-ignore', { ids: selectedRowKeys.value }); message.success('批量忽略成功'); selectedRowKeys.value = []; loadAlarms() } catch { message.error('操作失败') } }

const loadClusters = async () => {
  try {
    const res = await apiClient.get<any>('/kube/clusters', { params: { page_size: 100 } })
    clusterOptions.value = (res.items ?? []).map((c: any) => ({ value: String(c.id), label: c.name }))
  } catch { /* ignore */ }
}

onMounted(() => { loadClusters(); loadAlarms() })
</script>

<style scoped>
.kube-alarms-page { width: 100%; }
.section-row { margin-bottom: 16px; }

.alarm-stat { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; padding: 20px; text-align: center; cursor: pointer; transition: all 0.2s; }
.alarm-stat:hover { transform: translateY(-2px); box-shadow: 0 2px 8px rgba(0,0,0,0.08); }
.alarm-stat.critical .alarm-stat-value { color: #F53F3F; }
.alarm-stat.high .alarm-stat-value { color: #FF7D00; }
.alarm-stat.medium .alarm-stat-value { color: #F7BA1E; }
.alarm-stat.low .alarm-stat-value { color: #165DFF; }
.alarm-stat-value { font-size: 28px; font-weight: 700; line-height: 1.2; }
.alarm-stat-label { font-size: 13px; color: #86909C; margin-top: 4px; }

.dashboard-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; }
.card-body { padding: 20px; }
.filter-bar { display: flex; gap: 8px; align-items: center; margin-bottom: 16px; padding: 12px 16px; background: #F7F8FA; border-radius: 4px; border: 1px solid #E5E8EF; flex-wrap: wrap; }

.raw-json { background: #F7F8FA; padding: 16px; border-radius: 4px; font-size: 12px; font-family: 'SF Mono', 'Consolas', monospace; overflow-x: auto; max-height: 300px; color: #1D2129; }

.alarm-detail-header { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.alarm-detail-title { font-size: 16px; font-weight: 600; color: #1D2129; }
.severity-tag { font-size: 13px; }
.alarm-detail-message { font-size: 14px; color: #4E5969; line-height: 1.6; margin-bottom: 16px; padding: 12px 16px; background: #F7F8FA; border-radius: 6px; border-left: 3px solid #165DFF; }

.alarm-detail-section { margin-bottom: 12px; }
.section-label { font-size: 13px; font-weight: 600; color: #1D2129; margin-bottom: 6px; }
.section-content { font-size: 13px; color: #4E5969; line-height: 1.8; padding: 10px 14px; background: #F7F8FA; border-radius: 6px; }
.alarm-detail-section.remediation .section-content { background: #FFF7E6; border-left: 3px solid #FF7D00; white-space: pre-line; }
</style>
