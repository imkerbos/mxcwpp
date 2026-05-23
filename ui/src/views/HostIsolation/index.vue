<template>
  <div class="isolation-page">
    <div class="page-header">
      <h2>主机隔离管理</h2>
      <a-button @click="handleRefresh">
        <ReloadOutlined /> 刷新
      </a-button>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <a-select
        v-model:value="filters.status"
        placeholder="状态"
        style="width: 120px"
        allow-clear
        @change="handleSearch"
      >
        <a-select-option value="pending">等待中</a-select-option>
        <a-select-option value="active">隔离中</a-select-option>
        <a-select-option value="released">已解除</a-select-option>
        <a-select-option value="failed">失败</a-select-option>
      </a-select>
    </div>

    <!-- 隔离记录表格 -->
    <a-table
      :columns="columns"
      :data-source="isolations"
      :loading="loading"
      :pagination="pagination"
      row-key="id"
      size="small"
      @change="handleTableChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'level'">
          <a-tag :color="getLevelColor(record.level)">{{ getLevelLabel(record.level) }}</a-tag>
        </template>
        <template v-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">{{ getStatusLabel(record.status) }}</a-tag>
        </template>
        <template v-if="column.key === 'source'">
          <a-tag>{{ getSourceLabel(record.source) }}</a-tag>
        </template>
        <template v-if="column.key === 'timeout'">
          {{ formatTimeout(record.timeout) }}
        </template>
        <template v-if="column.key === 'action'">
          <a v-if="record.status === 'active' || record.status === 'pending'" @click="handleRelease(record)">
            解除隔离
          </a>
          <span v-else style="color: #999">{{ record.released_by || '-' }}</span>
        </template>
      </template>
    </a-table>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { hostIsolationApi } from '@/api/host-isolation'
import type { HostIsolation } from '@/api/host-isolation'

const loading = ref(false)
const isolations = ref<HostIsolation[]>([])

const filters = reactive({
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
  { title: '主机 ID', dataIndex: 'host_id', width: 200, ellipsis: true },
  { title: '隔离级别', key: 'level', width: 100, align: 'center' as const },
  { title: '状态', key: 'status', width: 80, align: 'center' as const },
  { title: '来源', key: 'source', width: 80 },
  { title: '超时', key: 'timeout', width: 80 },
  { title: '原因', dataIndex: 'reason', ellipsis: true },
  { title: '操作者', dataIndex: 'created_by', width: 100 },
  { title: '操作', key: 'action', width: 100 },
]

const getLevelColor = (level: string) => {
  const colors: Record<string, string> = { none: 'default', selective: 'blue', standard: 'orange', complete: 'red' }
  return colors[level] || 'default'
}

const getLevelLabel = (level: string) => {
  const labels: Record<string, string> = { none: '无', selective: '选择性', standard: '标准', complete: '完全' }
  return labels[level] || level
}

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = { pending: 'default', active: 'red', released: 'green', failed: 'volcano' }
  return colors[status] || 'default'
}

const getStatusLabel = (status: string) => {
  const labels: Record<string, string> = { pending: '等待中', active: '隔离中', released: '已解除', failed: '失败' }
  return labels[status] || status
}

const getSourceLabel = (source: string) => {
  const labels: Record<string, string> = { manual: '手动', auto_response: '自动响应', threat_intel: '威胁情报' }
  return labels[source] || source
}

const formatTimeout = (seconds: number) => {
  if (seconds >= 3600) return `${Math.round(seconds / 3600)}h`
  if (seconds >= 60) return `${Math.round(seconds / 60)}m`
  return `${seconds}s`
}

const fetchIsolations = async () => {
  loading.value = true
  try {
    const res = await hostIsolationApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      status: filters.status,
    })
    isolations.value = res.items || []
    pagination.total = res.total
  } catch {
    // handled
  } finally {
    loading.value = false
  }
}

const handleRelease = async (record: HostIsolation) => {
  try {
    await hostIsolationApi.release({ host_id: record.host_id })
    message.success('隔离解除指令已下发')
    fetchIsolations()
  } catch {
    // handled
  }
}

const handleSearch = () => {
  pagination.current = 1
  fetchIsolations()
}

const handleRefresh = () => {
  fetchIsolations()
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchIsolations()
}

onMounted(() => {
  fetchIsolations()
})
</script>

<style scoped>
.isolation-page { padding: 0; }
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0; font-size: 20px; }
.filter-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}
</style>
