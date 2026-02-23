<template>
  <div class="fim-events">
    <div class="page-header">
      <h2>FIM 变更事件</h2>
      <a-button @click="fetchEvents">
        <ReloadOutlined /> 刷新
      </a-button>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="16" class="stat-cards">
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="总事件" :value="stats.total" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="严重" :value="stats.critical" :value-style="{ color: '#cf1322' }" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="高危" :value="stats.high" :value-style="{ color: '#fa541c' }" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="中危" :value="stats.medium" :value-style="{ color: '#faad14' }" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="低危" :value="stats.low" :value-style="{ color: '#1890ff' }" />
        </a-card>
      </a-col>
      <a-col :span="4">
        <a-card size="small">
          <a-statistic title="变更类型" :value="`${stats.added}/${stats.changed}/${stats.removed}`" :value-style="{ fontSize: '18px' }" />
          <div style="color: #999; font-size: 12px">新增/变更/删除</div>
        </a-card>
      </a-col>
    </a-row>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <a-input
        v-model:value="filters.hostname"
        placeholder="主机名"
        style="width: 160px"
        allow-clear
        @change="handleSearch"
      >
        <template #prefix><SearchOutlined /></template>
      </a-input>
      <a-input
        v-model:value="filters.file_path"
        placeholder="文件路径"
        style="width: 200px; margin-left: 8px"
        allow-clear
        @change="handleSearch"
      />
      <a-select
        v-model:value="filters.change_type"
        placeholder="变更类型"
        style="width: 120px; margin-left: 8px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="added">新增</a-select-option>
        <a-select-option value="changed">变更</a-select-option>
        <a-select-option value="removed">删除</a-select-option>
      </a-select>
      <a-select
        v-model:value="filters.severity"
        placeholder="严重等级"
        style="width: 120px; margin-left: 8px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="critical">严重</a-select-option>
        <a-select-option value="high">高危</a-select-option>
        <a-select-option value="medium">中危</a-select-option>
        <a-select-option value="low">低危</a-select-option>
      </a-select>
      <a-select
        v-model:value="filters.category"
        placeholder="分类"
        style="width: 120px; margin-left: 8px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="binary">二进制</a-select-option>
        <a-select-option value="config">配置文件</a-select-option>
        <a-select-option value="auth">认证文件</a-select-option>
        <a-select-option value="log">日志</a-select-option>
        <a-select-option value="other">其他</a-select-option>
      </a-select>
      <a-range-picker
        v-model:value="dateRange"
        style="margin-left: 8px"
        @change="handleDateChange"
      />
    </div>

    <!-- 事件表格 -->
    <a-table
      :columns="columns"
      :data-source="events"
      :loading="loading"
      :pagination="pagination"
      row-key="event_id"
      @change="handleTableChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'hostname'">
          <a-tooltip :title="record.host_id">
            {{ record.hostname || record.host_id?.substring(0, 8) }}
          </a-tooltip>
        </template>
        <template v-if="column.key === 'file_path'">
          <a-tooltip :title="record.file_path">
            <span class="file-path">{{ record.file_path }}</span>
          </a-tooltip>
        </template>
        <template v-if="column.key === 'change_type'">
          <a-tag :color="getChangeTypeColor(record.change_type)">
            {{ getChangeTypeText(record.change_type) }}
          </a-tag>
        </template>
        <template v-if="column.key === 'severity'">
          <a-tag :color="getSeverityColor(record.severity)">
            {{ getSeverityText(record.severity) }}
          </a-tag>
        </template>
        <template v-if="column.key === 'category'">
          <a-tag>{{ getCategoryText(record.category) }}</a-tag>
        </template>
        <template v-if="column.key === 'action'">
          <a @click="showDetail(record)">详情</a>
        </template>
      </template>
    </a-table>

    <!-- 事件详情弹窗 -->
    <a-modal
      v-model:open="detailVisible"
      title="变更事件详情"
      :width="640"
      :footer="null"
    >
      <template v-if="selectedEvent">
        <a-descriptions :column="2" bordered size="small">
          <a-descriptions-item label="事件 ID" :span="2">
            <span style="font-family: monospace; font-size: 12px">{{ selectedEvent.event_id }}</span>
          </a-descriptions-item>
          <a-descriptions-item label="主机名">{{ selectedEvent.hostname }}</a-descriptions-item>
          <a-descriptions-item label="主机 ID">
            <span style="font-family: monospace; font-size: 12px">{{ selectedEvent.host_id }}</span>
          </a-descriptions-item>
          <a-descriptions-item label="文件路径" :span="2">
            <code>{{ selectedEvent.file_path }}</code>
          </a-descriptions-item>
          <a-descriptions-item label="变更类型">
            <a-tag :color="getChangeTypeColor(selectedEvent.change_type)">
              {{ getChangeTypeText(selectedEvent.change_type) }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="严重等级">
            <a-tag :color="getSeverityColor(selectedEvent.severity)">
              {{ getSeverityText(selectedEvent.severity) }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="分类">{{ getCategoryText(selectedEvent.category) }}</a-descriptions-item>
          <a-descriptions-item label="检测时间">{{ selectedEvent.detected_at }}</a-descriptions-item>
        </a-descriptions>

        <a-divider>变更详情</a-divider>
        <a-descriptions :column="2" bordered size="small" v-if="selectedEvent.change_detail">
          <a-descriptions-item label="文件大小（前）">
            {{ selectedEvent.change_detail.size_before || '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="文件大小（后）">
            {{ selectedEvent.change_detail.size_after || '-' }}
          </a-descriptions-item>
          <a-descriptions-item label="哈希变更">
            <a-tag :color="selectedEvent.change_detail.hash_changed ? 'red' : 'green'">
              {{ selectedEvent.change_detail.hash_changed ? '是' : '否' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="权限变更">
            <a-tag :color="selectedEvent.change_detail.permission_changed ? 'red' : 'green'">
              {{ selectedEvent.change_detail.permission_changed ? '是' : '否' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="属主变更">
            <a-tag :color="selectedEvent.change_detail.owner_changed ? 'red' : 'green'">
              {{ selectedEvent.change_detail.owner_changed ? '是' : '否' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="属性标记">
            <code>{{ selectedEvent.change_detail.attributes || '-' }}</code>
          </a-descriptions-item>
        </a-descriptions>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { fimApi } from '@/api/fim'
import type { FIMEvent, FIMEventStats } from '@/api/types'
import type { Dayjs } from 'dayjs'

const loading = ref(false)
const events = ref<FIMEvent[]>([])
const detailVisible = ref(false)
const selectedEvent = ref<FIMEvent | null>(null)
const dateRange = ref<[Dayjs, Dayjs] | null>(null)

const stats = reactive<FIMEventStats>({
  total: 0,
  critical: 0,
  high: 0,
  medium: 0,
  low: 0,
  added: 0,
  removed: 0,
  changed: 0,
  by_category: {},
  top_hosts: [],
  trend: [],
})

const filters = reactive({
  hostname: '',
  file_path: '',
  change_type: undefined as string | undefined,
  severity: undefined as string | undefined,
  category: undefined as string | undefined,
  date_from: undefined as string | undefined,
  date_to: undefined as string | undefined,
})

const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  { title: '主机名', key: 'hostname', width: 130 },
  { title: '文件路径', key: 'file_path', ellipsis: true },
  { title: '变更类型', key: 'change_type', width: 90, align: 'center' as const },
  { title: '严重等级', key: 'severity', width: 90, align: 'center' as const },
  { title: '分类', key: 'category', width: 90, align: 'center' as const },
  { title: '检测时间', dataIndex: 'detected_at', width: 170 },
  { title: '操作', key: 'action', width: 70 },
]

const getSeverityColor = (severity: string) => {
  const colors: Record<string, string> = {
    critical: 'red',
    high: 'orange',
    medium: 'gold',
    low: 'blue',
  }
  return colors[severity] || 'default'
}

const getSeverityText = (severity: string) => {
  const texts: Record<string, string> = {
    critical: '严重',
    high: '高危',
    medium: '中危',
    low: '低危',
  }
  return texts[severity] || severity
}

const getChangeTypeColor = (type: string) => {
  const colors: Record<string, string> = {
    added: 'green',
    removed: 'red',
    changed: 'orange',
  }
  return colors[type] || 'default'
}

const getChangeTypeText = (type: string) => {
  const texts: Record<string, string> = {
    added: '新增',
    removed: '删除',
    changed: '变更',
  }
  return texts[type] || type
}

const getCategoryText = (category: string) => {
  const texts: Record<string, string> = {
    binary: '二进制',
    config: '配置文件',
    auth: '认证文件',
    ssh: 'SSH',
    log: '日志',
    other: '其他',
  }
  return texts[category] || category || '-'
}

const fetchEvents = async () => {
  loading.value = true
  try {
    const res = await fimApi.listEvents({
      page: pagination.current,
      page_size: pagination.pageSize,
      hostname: filters.hostname || undefined,
      file_path: filters.file_path || undefined,
      change_type: filters.change_type,
      severity: filters.severity,
      category: filters.category,
      date_from: filters.date_from,
      date_to: filters.date_to,
    })
    events.value = res.items || []
    pagination.total = res.total
  } catch {
    // API 客户端已处理错误提示
  } finally {
    loading.value = false
  }
}

const fetchStats = async () => {
  try {
    const res = await fimApi.getEventStats(30)
    Object.assign(stats, res)
  } catch {
    // 静默处理
  }
}

const handleSearch = () => {
  pagination.current = 1
  fetchEvents()
}

const handleDateChange = (dates: [Dayjs, Dayjs] | null) => {
  if (dates) {
    filters.date_from = dates[0].format('YYYY-MM-DD')
    filters.date_to = dates[1].format('YYYY-MM-DD')
  } else {
    filters.date_from = undefined
    filters.date_to = undefined
  }
  handleSearch()
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchEvents()
}

const showDetail = (event: FIMEvent) => {
  selectedEvent.value = event
  detailVisible.value = true
}

onMounted(() => {
  fetchEvents()
  fetchStats()
})
</script>

<style scoped>
.fim-events {
  padding: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
}

.stat-cards {
  margin-bottom: 16px;
}

.filter-bar {
  display: flex;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 4px;
}

.file-path {
  font-family: monospace;
  font-size: 13px;
}
</style>
