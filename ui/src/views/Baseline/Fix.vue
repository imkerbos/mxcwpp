<template>
  <div class="baseline-fix-page">
    <div class="page-header">
      <h2>基线修复</h2>
      <a-space>
        <a-button
          v-if="selectedRowKeys.length > 0"
          type="primary"
          @click="handleBatchFix"
          :loading="fixing"
        >
          <template #icon>
            <ToolOutlined />
          </template>
          批量修复 ({{ selectedRowKeys.length }})
        </a-button>
        <a-button @click="handleRefresh" :loading="loading">
          <template #icon>
            <ReloadOutlined />
          </template>
          刷新
        </a-button>
      </a-space>
    </div>

    <!-- 筛选条件 -->
    <a-card :bordered="false" style="margin-bottom: 16px">
      <a-form layout="inline" :model="filters">
        <a-form-item label="主机选择">
          <a-select
            v-model:value="filters.host_ids"
            mode="multiple"
            placeholder="选择主机"
            style="width: 300px"
            allow-clear
            show-search
            :filter-option="filterHostOption"
            @change="handleFilterChange"
          >
            <a-select-option v-for="host in filteredHosts" :key="host.host_id" :value="host.host_id">
              {{ host.hostname }} ({{ host.ipv4[0] || host.host_id }})
            </a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="业务线">
          <a-select
            v-model:value="filters.business_line"
            placeholder="选择业务线"
            style="width: 200px"
            allow-clear
            @change="handleBusinessLineChange"
          >
            <a-select-option v-for="line in businessLines" :key="line" :value="line">
              {{ line }}
            </a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="风险等级">
          <a-checkbox-group v-model:value="filters.severities" @change="handleFilterChange">
            <a-checkbox value="critical">严重</a-checkbox>
            <a-checkbox value="high">高危</a-checkbox>
            <a-checkbox value="medium">中危</a-checkbox>
            <a-checkbox value="low">低危</a-checkbox>
          </a-checkbox-group>
        </a-form-item>
        <a-form-item>
          <a-button type="primary" @click="handleSearch">
            <template #icon>
              <SearchOutlined />
            </template>
            查询
          </a-button>
          <a-button style="margin-left: 8px" @click="handleReset">重置</a-button>
        </a-form-item>
      </a-form>
    </a-card>

    <!-- 统计信息 -->
    <a-card :bordered="false" style="margin-bottom: 16px" v-if="fixableItems.length > 0">
      <a-row :gutter="16">
        <a-col :span="6">
          <a-statistic title="可修复项总数" :value="fixableItems.length">
            <template #prefix>
              <UnorderedListOutlined />
            </template>
          </a-statistic>
        </a-col>
        <a-col :span="6">
          <a-statistic
            title="严重"
            :value="fixableItems.filter(i => i.severity === 'critical').length"
            :value-style="{ color: '#cf1322' }"
          >
            <template #prefix>
              <ExclamationCircleOutlined />
            </template>
          </a-statistic>
        </a-col>
        <a-col :span="6">
          <a-statistic
            title="高危"
            :value="fixableItems.filter(i => i.severity === 'high').length"
            :value-style="{ color: '#fa541c' }"
          >
            <template #prefix>
              <WarningOutlined />
            </template>
          </a-statistic>
        </a-col>
        <a-col :span="6">
          <a-statistic
            title="有自动修复方案"
            :value="fixableItems.filter(i => i.has_fix).length"
            :value-style="{ color: '#52c41a' }"
          >
            <template #prefix>
              <CheckCircleOutlined />
            </template>
          </a-statistic>
        </a-col>
      </a-row>
    </a-card>

    <!-- 可修复项列表 -->
    <a-table
      :columns="columns"
      :data-source="fixableItems"
      :loading="loading"
      :pagination="pagination"
      :row-selection="rowSelection"
      @change="handleTableChange"
      row-key="result_id"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'hostname'">
          <a @click="handleViewHost(record.host_id)">
            {{ record.hostname }}{{ record.ip ? ' (' + record.ip + ')' : '' }}
          </a>
        </template>
        <template v-else-if="column.key === 'business_line'">
          <a-tag v-if="record.business_line" color="blue">
            {{ record.business_line }}
          </a-tag>
          <span v-else style="color: #999">-</span>
        </template>
        <template v-else-if="column.key === 'severity'">
          <a-tag :color="getSeverityColor(record.severity)">
            {{ getSeverityText(record.severity) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'has_fix'">
          <a-tag v-if="record.has_fix" color="green">
            <CheckCircleOutlined /> 可自动修复
          </a-tag>
          <a-tag v-else color="default">
            <InfoCircleOutlined /> 需手动修复
          </a-tag>
        </template>
        <template v-else-if="column.key === 'action'">
          <a-space size="small">
            <a-button
              type="link"
              size="small"
              @click="handleViewDetail(record)"
            >
              查看详情
            </a-button>
            <a-popconfirm
              v-if="record.has_fix"
              title="确定要修复此项吗？"
              ok-text="确定"
              cancel-text="取消"
              @confirm="handleSingleFix(record)"
            >
              <a-button
                type="link"
                size="small"
                :loading="fixingItems[record.result_id]"
              >
                立即修复
              </a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </template>
    </a-table>

    <!-- 详情 Modal -->
    <a-modal
      v-model:open="detailModalVisible"
      title="修复项详情"
      width="800px"
      :footer="null"
    >
      <a-descriptions v-if="selectedItem" :column="2" bordered size="small">
        <a-descriptions-item label="主机名" :span="2">
          {{ selectedItem.hostname }}
        </a-descriptions-item>
        <a-descriptions-item label="规则ID">
          {{ selectedItem.rule_id }}
        </a-descriptions-item>
        <a-descriptions-item label="类别">
          {{ selectedItem.category }}
        </a-descriptions-item>
        <a-descriptions-item label="标题" :span="2">
          {{ selectedItem.title }}
        </a-descriptions-item>
        <a-descriptions-item label="严重级别">
          <a-tag :color="getSeverityColor(selectedItem.severity)">
            {{ getSeverityText(selectedItem.severity) }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="修复状态">
          <a-tag v-if="selectedItem.has_fix" color="green">
            可自动修复
          </a-tag>
          <a-tag v-else color="default">
            需手动修复
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="期望值" :span="2" v-if="selectedItem.expected">
          <code>{{ selectedItem.expected }}</code>
        </a-descriptions-item>
        <a-descriptions-item label="实际值" :span="2" v-if="selectedItem.actual">
          <code style="color: #ff4d4f;">{{ selectedItem.actual }}</code>
        </a-descriptions-item>
        <a-descriptions-item label="修复建议" :span="2" v-if="selectedItem.fix_suggestion">
          {{ selectedItem.fix_suggestion }}
        </a-descriptions-item>
        <a-descriptions-item label="修复命令" :span="2" v-if="selectedItem.fix_command">
          <div class="command-box">
            <code>{{ selectedItem.fix_command }}</code>
            <a-button
              type="link"
              size="small"
              @click="copyCommand(selectedItem.fix_command)"
            >
              <CopyOutlined /> 复制
            </a-button>
          </div>
        </a-descriptions-item>
      </a-descriptions>
      <div style="margin-top: 16px; text-align: right;" v-if="selectedItem?.has_fix">
        <a-popconfirm
          title="确定要执行修复吗？"
          ok-text="确定"
          cancel-text="取消"
          @confirm="handleSingleFix(selectedItem)"
        >
          <a-button type="primary" :loading="fixingItems[selectedItem.result_id]">
            <ToolOutlined /> 执行修复
          </a-button>
        </a-popconfirm>
      </div>
    </a-modal>

    <!-- 修复进度 Modal -->
    <a-modal
      v-model:open="progressModalVisible"
      title="修复进度"
      width="700px"
      :closable="!fixing"
      :maskClosable="false"
      :footer="null"
    >
      <div class="fix-progress">
        <a-progress
          :percent="fixProgress"
          :status="fixing ? 'active' : fixSuccess ? 'success' : 'exception'"
        />
        <div class="progress-info">
          <span>总计: {{ fixTotal }}</span>
          <span>成功: {{ fixSuccessCount }}</span>
          <span>失败: {{ fixFailedCount }}</span>
        </div>
        <a-divider />
        <div class="fix-results">
          <div
            v-for="(result, index) in fixResults"
            :key="index"
            :class="['fix-result-item', `status-${result.status}`]"
          >
            <div class="result-header">
              <span class="result-icon">
                <CheckCircleOutlined v-if="result.status === 'success'" />
                <CloseCircleOutlined v-else-if="result.status === 'failed'" />
                <SyncOutlined v-else spin />
              </span>
              <span class="result-title">{{ result.title }}</span>
              <span class="result-host">{{ result.hostname }}</span>
            </div>
            <div class="result-message" v-if="result.message">
              {{ result.message }}
            </div>
          </div>
        </div>
      </div>
      <div style="margin-top: 16px; text-align: right;" v-if="!fixing">
        <a-button type="primary" @click="handleCloseProgress">
          关闭
        </a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { useRouter } from 'vue-router'
import {
  ToolOutlined,
  ReloadOutlined,
  SearchOutlined,
  UnorderedListOutlined,
  ExclamationCircleOutlined,
  WarningOutlined,
  CheckCircleOutlined,
  InfoCircleOutlined,
  CopyOutlined,
  CloseCircleOutlined,
  SyncOutlined,
} from '@ant-design/icons-vue'
import { fixApi } from '@/api/fix'
import { hostsApi } from '@/api/hosts'
import type { FixableItem, Host, FixResult } from '@/api/types'

const router = useRouter()

const loading = ref(false)
const fixing = ref(false)
const hosts = ref<Host[]>([])
const businessLines = ref<string[]>([])
const fixableItems = ref<FixableItem[]>([])
const selectedRowKeys = ref<string[]>([])
const detailModalVisible = ref(false)
const progressModalVisible = ref(false)
const selectedItem = ref<FixableItem | null>(null)
const fixingItems = reactive<Record<string, boolean>>({})

// 修复进度相关
const fixProgress = ref(0)
const fixTotal = ref(0)
const fixSuccessCount = ref(0)
const fixFailedCount = ref(0)
const fixSuccess = ref(false)
const fixResults = ref<FixResult[]>([])

const filters = reactive({
  host_ids: [] as string[],
  business_line: '' as string,
  severities: ['critical', 'high'] as string[],
})

const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  {
    title: '主机',
    dataIndex: 'hostname',
    key: 'hostname',
    width: 250,
    customRender: ({ record }: { record: FixableItem }) => {
      return `${record.hostname}${record.ip ? ' (' + record.ip + ')' : ''}`
    },
  },
  {
    title: '业务线',
    dataIndex: 'business_line',
    key: 'business_line',
    width: 120,
  },
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
    width: 100,
  },
  {
    title: '修复状态',
    key: 'has_fix',
    width: 130,
  },
  {
    title: '操作',
    key: 'action',
    width: 180,
    fixed: 'right' as const,
  },
]

const rowSelection = computed(() => ({
  selectedRowKeys: selectedRowKeys.value,
  onChange: (keys: string[]) => {
    selectedRowKeys.value = keys
  },
  getCheckboxProps: (record: FixableItem) => ({
    disabled: !record.has_fix,
  }),
}))

// 根据业务线筛选主机列表
const filteredHosts = computed(() => {
  if (!filters.business_line) {
    return hosts.value
  }
  return hosts.value.filter(host => host.business_line === filters.business_line)
})

const loadHosts = async () => {
  try {
    const response = await hostsApi.list({ page_size: 1000 }) as any
    hosts.value = response.items || []

    // 提取业务线列表（去重）
    const lines = new Set<string>()
    hosts.value.forEach(host => {
      if (host.business_line) {
        lines.add(host.business_line)
      }
    })
    businessLines.value = Array.from(lines).sort()
  } catch (error) {
    console.error('加载主机列表失败:', error)
  }
}

const loadFixableItems = async () => {
  loading.value = true
  try {
    const response = await fixApi.getFixableItems({
      host_ids: filters.host_ids.length > 0 ? filters.host_ids : undefined,
      business_line: filters.business_line || undefined,
      severities: filters.severities.length > 0 ? filters.severities : undefined,
      page: pagination.current,
      page_size: pagination.pageSize,
    })
    fixableItems.value = response.items || []
    pagination.total = response.total || 0
  } catch (error) {
    console.error('加载可修复项失败:', error)
    message.error('加载可修复项失败')
  } finally {
    loading.value = false
  }
}

const handleFilterChange = () => {
  // 筛选条件变化时不自动查询，等待用户点击查询按钮
}

const handleBusinessLineChange = () => {
  // 业务线变化时，清空主机选择（因为筛选后的主机列表可能不包含之前选择的主机）
  filters.host_ids = []
  handleFilterChange()
}

const handleSearch = () => {
  pagination.current = 1
  loadFixableItems()
}

const handleReset = () => {
  filters.host_ids = []
  filters.business_line = ''
  filters.severities = ['critical', 'high']
  pagination.current = 1
  selectedRowKeys.value = []
  loadFixableItems()
}

const handleRefresh = () => {
  loadFixableItems()
  message.success('已刷新')
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  loadFixableItems()
}

const handleViewHost = (hostId: string) => {
  router.push(`/hosts/${hostId}`)
}

const handleViewDetail = (record: FixableItem) => {
  selectedItem.value = record
  detailModalVisible.value = true
}

const handleSingleFix = async (record: FixableItem) => {
  fixingItems[record.result_id] = true
  try {
    const response = await fixApi.createFixTask({
      host_ids: [record.host_id],
      rule_ids: [record.rule_id],
    })

    // 显示进度 Modal
    progressModalVisible.value = true
    fixing.value = true
    fixProgress.value = 0
    // 单个修复：1 项（后端会根据实际失败记录计算）
    fixTotal.value = 1
    fixSuccessCount.value = 0
    fixFailedCount.value = 0
    fixResults.value = []

    // 轮询任务状态
    await pollFixTask(response.task_id)

    message.success('修复完成')
    detailModalVisible.value = false
    loadFixableItems()
  } catch (error: any) {
    console.error('修复失败:', error)
    message.error('修复失败: ' + (error.response?.data?.message || error.message))
  } finally {
    fixingItems[record.result_id] = false
  }
}

const handleBatchFix = async () => {
  const selectedItems = fixableItems.value.filter(item =>
    selectedRowKeys.value.includes(item.result_id) && item.has_fix
  )

  if (selectedItems.length === 0) {
    message.warning('请选择可自动修复的项')
    return
  }

  fixing.value = true
  try {
    const response = await fixApi.createFixTask({
      host_ids: [...new Set(selectedItems.map(item => item.host_id))],
      rule_ids: [...new Set(selectedItems.map(item => item.rule_id))],
    })

    // 显示进度 Modal
    progressModalVisible.value = true
    fixProgress.value = 0
    // 使用选中的可修复项数量作为总数（后端会根据实际失败记录计算）
    fixTotal.value = selectedItems.length
    fixSuccessCount.value = 0
    fixFailedCount.value = 0
    fixResults.value = []

    // 轮询任务状态
    await pollFixTask(response.task_id)

    message.success('批量修复完成')
    selectedRowKeys.value = []
    loadFixableItems()
  } catch (error: any) {
    console.error('批量修复失败:', error)
    message.error('批量修复失败: ' + (error.response?.data?.message || error.message))
  } finally {
    fixing.value = false
  }
}

const pollFixTask = async (taskId: string) => {
  const maxAttempts = 60 // 最多轮询 60 次（5 分钟）
  let attempts = 0

  while (attempts < maxAttempts) {
    try {
      const task = await fixApi.getFixTask(taskId)
      fixProgress.value = task.progress
      fixSuccessCount.value = task.success_count
      fixFailedCount.value = task.failed_count

      // 获取修复结果
      const resultsResponse = await fixApi.getFixResults(taskId, { page_size: 1000 })
      fixResults.value = resultsResponse.items || []

      if (task.status === 'completed') {
        fixing.value = false
        fixSuccess.value = task.failed_count === 0
        break
      } else if (task.status === 'failed') {
        fixing.value = false
        fixSuccess.value = false
        break
      }

      // 等待 5 秒后继续轮询
      await new Promise(resolve => setTimeout(resolve, 5000))
      attempts++
    } catch (error) {
      console.error('轮询任务状态失败:', error)
      break
    }
  }

  if (attempts >= maxAttempts) {
    message.warning('任务执行超时，请稍后查看结果')
    fixing.value = false
  }
}

const handleCloseProgress = () => {
  progressModalVisible.value = false
  fixResults.value = []
}

const copyCommand = (command: string) => {
  navigator.clipboard.writeText(command)
  message.success('已复制到剪贴板')
}

const filterHostOption = (input: string, option: any) => {
  // 获取主机名和IP地址进行匹配
  const host = filteredHosts.value.find(h => h.host_id === option.value)
  if (!host) return false

  const searchText = input.toLowerCase()
  const hostname = host.hostname.toLowerCase()
  const ip = host.ipv4[0] || ''

  return hostname.includes(searchText) || ip.includes(searchText)
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
  loadHosts()
  loadFixableItems()
})
</script>

<style scoped>
.baseline-fix-page {
  width: 100%;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0;
}

.command-box {
  display: flex;
  align-items: center;
  gap: 8px;
  background: #f5f5f5;
  padding: 8px 12px;
  border-radius: 4px;
}

.command-box code {
  flex: 1;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
}

.fix-progress {
  padding: 16px 0;
}

.progress-info {
  display: flex;
  justify-content: space-between;
  margin-top: 12px;
  color: #666;
  font-size: 14px;
}

.fix-results {
  max-height: 400px;
  overflow-y: auto;
}

.fix-result-item {
  padding: 12px;
  margin-bottom: 8px;
  border-radius: 4px;
  border: 1px solid #d9d9d9;
}

.fix-result-item.status-success {
  background: #f6ffed;
  border-color: #b7eb8f;
}

.fix-result-item.status-failed {
  background: #fff2f0;
  border-color: #ffccc7;
}

.fix-result-item.status-running {
  background: #e6f7ff;
  border-color: #91d5ff;
}

.result-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.result-icon {
  font-size: 16px;
}

.status-success .result-icon {
  color: #52c41a;
}

.status-failed .result-icon {
  color: #ff4d4f;
}

.status-running .result-icon {
  color: #1890ff;
}

.result-title {
  flex: 1;
  font-weight: 500;
}

.result-host {
  color: #8c8c8c;
  font-size: 12px;
}

.result-message {
  margin-top: 4px;
  padding-left: 24px;
  color: #666;
  font-size: 12px;
}
</style>
