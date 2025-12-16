<template>
  <div>
    <!-- 搜索和过滤 -->
    <div class="toolbar">
      <a-input
        v-model:value="localFilters.keyword"
        placeholder="搜索告警标题或描述"
        allow-clear
        @press-enter="handleSearch"
        style="width: 300px"
      >
        <template #prefix>
          <SearchOutlined />
        </template>
      </a-input>
      <a-select
        v-model:value="localFilters.severity"
        placeholder="严重级别"
        allow-clear
        style="width: 120px"
        @change="handleSearch"
      >
        <a-select-option value="critical">严重</a-select-option>
        <a-select-option value="high">高危</a-select-option>
        <a-select-option value="medium">中危</a-select-option>
        <a-select-option value="low">低危</a-select-option>
      </a-select>
      <a-select
        v-model:value="localFilters.category"
        placeholder="类别"
        allow-clear
        style="width: 150px"
        @change="handleSearch"
      >
        <a-select-option value="ssh">SSH</a-select-option>
        <a-select-option value="password">密码策略</a-select-option>
        <a-select-option value="file_permission">文件权限</a-select-option>
        <a-select-option value="sysctl">内核参数</a-select-option>
        <a-select-option value="service">服务状态</a-select-option>
      </a-select>
      <a-button @click="handleSearch">搜索</a-button>
      <a-button @click="handleRefresh">
        <template #icon>
          <ReloadOutlined />
        </template>
        刷新
      </a-button>
    </div>

    <!-- 告警表格 -->
    <a-table
      :columns="columns"
      :data-source="alerts"
      :loading="loading"
      :pagination="pagination"
      row-key="id"
      @change="handleTableChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'severity'">
          <a-tag :color="getSeverityColor(record.severity)">
            {{ getSeverityText(record.severity) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">
            {{ getStatusText(record.status) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'host'">
          <a @click="handleViewHost(record.host_id)">
            {{ record.host?.hostname || record.host_id }}
          </a>
        </template>
        <template v-else-if="column.key === 'first_seen_at'">
          {{ formatDateTime(record.first_seen_at) }}
        </template>
        <template v-else-if="column.key === 'last_seen_at'">
          {{ formatDateTime(record.last_seen_at) }}
        </template>
        <template v-else-if="column.key === 'actions'">
          <a-space>
            <a-button type="link" size="small" @click="handleViewDetail(record)">
              查看详情
            </a-button>
            <template v-if="status === 'active'">
              <a-button type="link" size="small" @click="handleResolveClick(record)">
                解决
              </a-button>
              <a-button type="link" size="small" danger @click="handleIgnoreClick(record)">
                忽略
              </a-button>
            </template>
          </a-space>
        </template>
      </template>
    </a-table>

    <!-- 解决告警对话框 -->
    <a-modal
      v-model:open="resolveModalVisible"
      title="解决告警"
      @ok="handleResolveConfirm"
      @cancel="resolveModalVisible = false"
    >
      <a-form-item label="解决原因">
        <a-textarea
          v-model:value="resolveReason"
          placeholder="请输入解决原因（可选）"
          :rows="4"
        />
      </a-form-item>
    </a-modal>

    <!-- 忽略确认对话框 -->
    <a-modal
      v-model:open="ignoreModalVisible"
      title="确认忽略"
      @ok="handleIgnoreConfirm"
      @cancel="ignoreModalVisible = false"
      ok-text="确认忽略"
      :ok-button-props="{ danger: true }"
    >
      <p>确定要忽略告警「{{ ignoreAlert?.title }}」吗？</p>
      <p style="color: #8c8c8c; font-size: 13px;">忽略后告警将移至历史记录。</p>
      <a-alert
        type="warning"
        message="忽略告警不会解决实际的安全问题，建议优先解决告警。"
        show-icon
        style="margin-top: 12px"
      />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { formatDateTime } from '@/utils/date'
import type { Alert } from '@/api/alerts'
import type { TableProps } from 'ant-design-vue'

const props = defineProps<{
  alerts: Alert[]
  loading: boolean
  pagination: any
  filters: any
  status: 'active' | 'history'
}>()

const emit = defineEmits<{
  change: [filters: any]
  resolve: [alert: Alert, reason?: string]
  ignore: [alert: Alert]
  refresh: []
}>()

const router = useRouter()
const localFilters = ref({ ...props.filters })
const resolveModalVisible = ref(false)
const ignoreModalVisible = ref(false)
const currentAlert = ref<Alert | null>(null)
const ignoreAlert = ref<Alert | null>(null)
const resolveReason = ref('')

watch(
  () => props.filters,
  (newFilters) => {
    localFilters.value = { ...newFilters }
  },
  { deep: true }
)

const columns = [
  {
    title: '告警标题',
    dataIndex: 'title',
    key: 'title',
    width: 250,
    ellipsis: true,
  },
  {
    title: '严重级别',
    key: 'severity',
    width: 100,
  },
  {
    title: '类别',
    dataIndex: 'category',
    key: 'category',
    width: 120,
  },
  {
    title: '主机',
    key: 'host',
    width: 150,
  },
  {
    title: '首次发现',
    key: 'first_seen_at',
    width: 180,
  },
  {
    title: '最后发现',
    key: 'last_seen_at',
    width: 180,
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
  },
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

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = {
    active: 'red',
    resolved: 'green',
    ignored: 'default',
  }
  return colors[status] || 'default'
}

const getStatusText = (status: string) => {
  const texts: Record<string, string> = {
    active: '活跃',
    resolved: '已解决',
    ignored: '已忽略',
  }
  return texts[status] || status
}

const handleSearch = () => {
  emit('change', localFilters.value)
}

const handleRefresh = () => {
  emit('refresh')
}

const handleTableChange: TableProps['onChange'] = (pag, filters, sorter) => {
  if (pag) {
    emit('change', { ...localFilters.value, page: pag.current, pageSize: pag.pageSize })
  }
}

const handleViewHost = (hostId: string) => {
  router.push(`/hosts/${hostId}`)
}

const handleViewDetail = (alert: Alert) => {
  router.push(`/alerts/${alert.id}`)
}

const handleResolveClick = (alert: Alert) => {
  currentAlert.value = alert
  resolveReason.value = ''
  resolveModalVisible.value = true
}

const handleResolveConfirm = () => {
  if (currentAlert.value) {
    emit('resolve', currentAlert.value, resolveReason.value || undefined)
    resolveModalVisible.value = false
    currentAlert.value = null
    resolveReason.value = ''
  }
}

const handleIgnoreClick = (alert: Alert) => {
  ignoreAlert.value = alert
  ignoreModalVisible.value = true
}

const handleIgnoreConfirm = () => {
  if (ignoreAlert.value) {
    emit('ignore', ignoreAlert.value)
    ignoreModalVisible.value = false
    ignoreAlert.value = null
  }
}
</script>

<style scoped lang="less">
.toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  align-items: center;
}
</style>
