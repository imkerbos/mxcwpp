<template>
  <div class="anomaly-page">
    <div class="page-header">
      <h2>ML 异常检测</h2>
      <a-button @click="handleRefresh">
        <ReloadOutlined /> 刷新
      </a-button>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="16" class="stat-cards">
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="总告警" :value="stats.total" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="待处理" :value="stats.open" :value-style="{ color: '#FF7D00' }" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="严重告警" :value="stats.critical" :value-style="{ color: '#F53F3F' }" />
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card size="small" title="按类型分布">
          <div v-if="stats.by_type.length > 0" class="type-list">
            <div v-for="item in stats.by_type" :key="item.alert_type" class="type-item">
              <span>{{ getAlertTypeLabel(item.alert_type) }}</span>
              <a-tag>{{ item.count }}</a-tag>
            </div>
          </div>
          <span v-else class="empty-text">暂无数据</span>
        </a-card>
      </a-col>
      <a-col :span="6">
        <a-card size="small" title="关联模式分布">
          <div v-if="stats.by_pattern.length > 0" class="type-list">
            <div v-for="item in stats.by_pattern" :key="item.alert_type" class="type-item">
              <span>{{ getPatternLabel(item.alert_type) }}</span>
              <a-tag>{{ item.count }}</a-tag>
            </div>
          </div>
          <span v-else class="empty-text">暂无数据</span>
        </a-card>
      </a-col>
    </a-row>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <a-input
        v-model:value="filters.host_id"
        placeholder="主机 ID"
        style="width: 200px"
        allow-clear
        @pressEnter="handleSearch"
      />
      <a-select
        v-model:value="filters.alert_type"
        placeholder="告警类型"
        style="width: 140px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="isolation_forest">Isolation Forest</a-select-option>
        <a-select-option value="correlation">多维关联</a-select-option>
      </a-select>
      <a-select
        v-model:value="filters.severity"
        placeholder="严重度"
        style="width: 120px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="critical">严重</a-select-option>
        <a-select-option value="high">高危</a-select-option>
        <a-select-option value="medium">中危</a-select-option>
        <a-select-option value="low">低危</a-select-option>
      </a-select>
      <a-select
        v-model:value="filters.status"
        placeholder="状态"
        style="width: 120px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="open">待处理</a-select-option>
        <a-select-option value="confirmed">已确认</a-select-option>
        <a-select-option value="false_positive">误报</a-select-option>
      </a-select>
    </div>

    <!-- 告警表格 -->
    <a-table
      :columns="columns"
      :data-source="alerts"
      :loading="loading"
      :pagination="pagination"
      row-key="id"
      size="small"
      @change="handleTableChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'severity'">
          <a-tag :color="getSeverityConfig(record.severity).tagColor">
            {{ getSeverityConfig(record.severity).label }}
          </a-tag>
        </template>
        <template v-if="column.key === 'alert_type'">
          <a-tag :color="record.alert_type === 'isolation_forest' ? 'purple' : 'geekblue'">
            {{ getAlertTypeLabel(record.alert_type) }}
          </a-tag>
        </template>
        <template v-if="column.key === 'anomaly_score'">
          <a-progress
            :percent="Math.round(record.anomaly_score * 100)"
            :stroke-color="record.anomaly_score >= 0.8 ? '#F53F3F' : record.anomaly_score >= 0.7 ? '#FF7D00' : '#F7BA1E'"
            :size="[100, 6]"
            :show-info="true"
          />
        </template>
        <template v-if="column.key === 'detail'">
          <template v-if="record.alert_type === 'isolation_forest'">
            <span class="mono-text">{{ record.top_metric }}</span>
            <span v-if="record.top_value" class="detail-value"> = {{ record.top_value.toFixed(1) }}</span>
          </template>
          <template v-else>
            <span>{{ getPatternLabel(record.pattern_name) }}</span>
          </template>
        </template>
        <template v-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">{{ getStatusLabel(record.status) }}</a-tag>
        </template>
        <template v-if="column.key === 'action'">
          <a-space v-if="record.status === 'open'">
            <a @click="handleResolve(record, 'confirmed')">确认</a>
            <a @click="handleResolve(record, 'false_positive')" style="color: #86909C">误报</a>
          </a-space>
          <span v-else style="color: #999">{{ record.resolved_by }}</span>
        </template>
      </template>
    </a-table>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { anomalyApi } from '@/api/anomaly'
import type { AnomalyAlert, AnomalyStats } from '@/api/anomaly'
import { getSeverityConfig } from '@/constants/severity'

const loading = ref(false)
const alerts = ref<AnomalyAlert[]>([])
const stats = reactive<AnomalyStats>({
  total: 0,
  open: 0,
  critical: 0,
  by_type: [],
  by_pattern: [],
})

const filters = reactive({
  host_id: '',
  alert_type: undefined as string | undefined,
  severity: undefined as string | undefined,
  status: undefined as string | undefined,
})

const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  { title: '时间', dataIndex: 'created_at', width: 170 },
  { title: '主机', dataIndex: 'hostname', width: 120, ellipsis: true },
  { title: '类型', key: 'alert_type', width: 120 },
  { title: '严重度', key: 'severity', width: 80, align: 'center' as const },
  { title: '异常分数', key: 'anomaly_score', width: 150 },
  { title: '详情', key: 'detail', ellipsis: true },
  { title: '状态', key: 'status', width: 80, align: 'center' as const },
  { title: '操作', key: 'action', width: 120 },
]

const getAlertTypeLabel = (type: string) => {
  return type === 'isolation_forest' ? 'Isolation Forest' : '多维关联'
}

const getPatternLabel = (name: string) => {
  const labels: Record<string, string> = {
    c2_beacon: 'C2 信标',
    data_exfiltration: '数据外泄',
    privilege_escalation: '权限提升',
    reconnaissance: '侦察扫描',
  }
  return labels[name] || name
}

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = { open: 'orange', confirmed: 'red', false_positive: 'default' }
  return colors[status] || 'default'
}

const getStatusLabel = (status: string) => {
  const labels: Record<string, string> = { open: '待处理', confirmed: '已确认', false_positive: '误报' }
  return labels[status] || status
}

const fetchAlerts = async () => {
  loading.value = true
  try {
    const res = await anomalyApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      host_id: filters.host_id || undefined,
      alert_type: filters.alert_type,
      severity: filters.severity,
      status: filters.status,
    })
    alerts.value = res.items || []
    pagination.total = res.total
  } catch {
    // handled by client
  } finally {
    loading.value = false
  }
}

const fetchStats = async () => {
  try {
    const res = await anomalyApi.stats()
    Object.assign(stats, res)
  } catch {
    // silent
  }
}

const handleSearch = () => {
  pagination.current = 1
  fetchAlerts()
}

const handleRefresh = () => {
  fetchAlerts()
  fetchStats()
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchAlerts()
}

const handleResolve = async (record: AnomalyAlert, status: 'confirmed' | 'false_positive') => {
  try {
    await anomalyApi.resolve(record.id, status)
    message.success(status === 'confirmed' ? '已确认威胁' : '已标记为误报')
    fetchAlerts()
    fetchStats()
  } catch {
    // handled by client
  }
}

onMounted(() => {
  fetchAlerts()
  fetchStats()
})
</script>

<style scoped>
.anomaly-page { padding: 0; }
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0; font-size: 20px; }
.stat-cards { margin-bottom: 16px; }
.filter-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.type-list { display: flex; flex-direction: column; gap: 4px; }
.type-item { display: flex; justify-content: space-between; align-items: center; }
.empty-text { color: #999; font-size: 12px; }
.mono-text { font-family: monospace; font-size: 13px; }
.detail-value { color: #F53F3F; font-weight: 500; }
</style>
