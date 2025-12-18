<template>
  <a-card title="基线检查结果" :bordered="false">
    <!-- 统计概览 -->
    <div class="stats-row">
      <div class="stat-item">
        <span class="stat-label">总计</span>
        <span class="stat-value">{{ totalCount }}</span>
      </div>
      <div class="stat-item pass">
        <span class="stat-label">通过</span>
        <span class="stat-value">{{ passCount }}</span>
      </div>
      <div class="stat-item fail">
        <span class="stat-label">失败</span>
        <span class="stat-value">{{ failCount }}</span>
      </div>
      <div class="stat-item error">
        <span class="stat-label">错误</span>
        <span class="stat-value">{{ errorCount }}</span>
      </div>
    </div>

    <!-- 筛选器 -->
    <div class="filter-bar">
      <a-radio-group v-model:value="statusFilter" button-style="solid" size="small">
        <a-radio-button value="all">全部 ({{ totalCount }})</a-radio-button>
        <a-radio-button value="fail">失败 ({{ failCount + errorCount }})</a-radio-button>
        <a-radio-button value="pass">通过 ({{ passCount }})</a-radio-button>
      </a-radio-group>
      <a-input-search
        v-model:value="searchKeyword"
        placeholder="搜索规则ID或标题"
        style="width: 250px"
        allow-clear
      />
    </div>

    <a-table
      :columns="columns"
      :data-source="filteredResults"
      :loading="loading"
      :pagination="{ pageSize: 20, showSizeChanger: true, pageSizeOptions: ['10', '20', '50', '100', '200'], showTotal: (total: number) => `共 ${total} 条` }"
      row-key="result_id"
      size="small"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">
            {{ getStatusText(record.status) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'severity'">
          <a-tag :color="getSeverityColor(record.severity)">
            {{ getSeverityText(record.severity) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'failure_reason'">
          <template v-if="record.status === 'fail' || record.status === 'error'">
            <a-tooltip v-if="record.actual || record.expected" placement="topLeft">
              <template #title>
                <div>
                  <div v-if="record.expected"><strong>期望值:</strong> {{ record.expected }}</div>
                  <div v-if="record.actual"><strong>实际值:</strong> {{ record.actual }}</div>
                </div>
              </template>
              <span class="failure-reason">
                {{ record.actual ? `实际: ${record.actual.slice(0, 30)}${record.actual.length > 30 ? '...' : ''}` : '检查失败' }}
              </span>
            </a-tooltip>
            <span v-else class="failure-reason">检查失败</span>
          </template>
          <span v-else style="color: #52c41a;">-</span>
        </template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { hostsApi } from '@/api/hosts'
import type { ScanResult } from '@/api/types'

const props = defineProps<{
  hostId: string
}>()

const loading = ref(false)
const results = ref<ScanResult[]>([])
const statusFilter = ref<'all' | 'fail' | 'pass'>('all')
const searchKeyword = ref('')

// 统计数据
const totalCount = computed(() => results.value.length)
const passCount = computed(() => results.value.filter(r => r.status === 'pass').length)
const failCount = computed(() => results.value.filter(r => r.status === 'fail').length)
const errorCount = computed(() => results.value.filter(r => r.status === 'error').length)

// 过滤后的结果
const filteredResults = computed(() => {
  let filtered = results.value

  // 状态筛选
  if (statusFilter.value === 'fail') {
    filtered = filtered.filter(r => r.status === 'fail' || r.status === 'error')
  } else if (statusFilter.value === 'pass') {
    filtered = filtered.filter(r => r.status === 'pass')
  }

  // 关键词搜索
  if (searchKeyword.value) {
    const keyword = searchKeyword.value.toLowerCase()
    filtered = filtered.filter(r =>
      r.rule_id?.toLowerCase().includes(keyword) ||
      r.title?.toLowerCase().includes(keyword)
    )
  }

  return filtered
})

const columns = [
  {
    title: '规则ID',
    dataIndex: 'rule_id',
    key: 'rule_id',
    width: 180,
    ellipsis: true,
  },
  {
    title: '类别',
    dataIndex: 'category',
    key: 'category',
    width: 100,
  },
  {
    title: '标题',
    dataIndex: 'title',
    key: 'title',
    ellipsis: true,
  },
  {
    title: '严重级别',
    key: 'severity',
    width: 90,
  },
  {
    title: '状态',
    key: 'status',
    width: 80,
  },
  {
    title: '失败原因',
    key: 'failure_reason',
    width: 200,
    ellipsis: true,
  },
  {
    title: '检查时间',
    dataIndex: 'checked_at',
    key: 'checked_at',
    width: 160,
  },
]

const loadBaselineResults = async () => {
  loading.value = true
  try {
    const hostDetail = await hostsApi.get(props.hostId)
    // 显示所有结果
    results.value = hostDetail.baseline_results || []
  } catch (error) {
    console.error('加载基线结果失败:', error)
  } finally {
    loading.value = false
  }
}

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = {
    pass: 'green',
    fail: 'red',
    error: 'orange',
    na: 'default',
  }
  return colors[status] || 'default'
}

const getStatusText = (status: string) => {
  const texts: Record<string, string> = {
    pass: '通过',
    fail: '失败',
    error: '错误',
    na: '不适用',
  }
  return texts[status] || status
}

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
    high: '高',
    medium: '中',
    low: '低',
  }
  return texts[severity] || severity
}

onMounted(() => {
  loadBaselineResults()
})
</script>

<style scoped>
.stats-row {
  display: flex;
  gap: 32px;
  margin-bottom: 16px;
  padding: 16px;
  background: #fafafa;
  border-radius: 8px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.stat-label {
  font-size: 13px;
  color: #8c8c8c;
  margin-bottom: 4px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: #262626;
}

.stat-item.pass .stat-value {
  color: #52c41a;
}

.stat-item.fail .stat-value {
  color: #ff4d4f;
}

.stat-item.error .stat-value {
  color: #faad14;
}

.filter-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.failure-reason {
  color: #ff4d4f;
  font-size: 12px;
  cursor: help;
}
</style>
