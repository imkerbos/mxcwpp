<template>
  <div class="tasks-page">
    <div class="page-header">
      <h2>任务执行</h2>
      <a-space>
        <a-button
          v-if="selectedRowKeys.length > 0"
          danger
          @click="handleBatchCancel"
          :loading="batchCanceling"
        >
          <template #icon>
            <StopOutlined />
          </template>
          批量取消 ({{ selectedRowKeys.length }})
        </a-button>
        <a-button
          v-if="selectedRowKeys.length > 0"
          @click="handleBatchDelete"
          :loading="batchDeleting"
        >
          <template #icon>
            <DeleteOutlined />
          </template>
          批量删除 ({{ selectedRowKeys.length }})
        </a-button>
        <a-button type="primary" @click="handleCreate">
          <template #icon>
            <PlusOutlined />
          </template>
          新建任务
        </a-button>
      </a-space>
    </div>

    <!-- 筛选条件 -->
    <a-card :bordered="false" style="margin-bottom: 16px">
      <a-form layout="inline" :model="filters">
        <a-form-item label="任务类型">
          <a-select
            v-model:value="filters.type"
            placeholder="选择类型"
            style="width: 150px"
            allow-clear
          >
            <a-select-option value="baseline">基线检查</a-select-option>
            <a-select-option value="manual">手动任务</a-select-option>
            <a-select-option value="scheduled">定时任务</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select
            v-model:value="filters.status"
            placeholder="选择状态"
            style="width: 150px"
            allow-clear
          >
            <a-select-option value="pending">待执行</a-select-option>
            <a-select-option value="running">执行中</a-select-option>
            <a-select-option value="completed">已完成</a-select-option>
            <a-select-option value="failed">失败</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="任务名称">
          <a-input
            v-model:value="filters.name"
            placeholder="输入任务名称"
            style="width: 200px"
            allow-clear
          />
        </a-form-item>
        <a-form-item>
          <a-button type="primary" @click="handleSearch">
            <template #icon>
              <SearchOutlined />
            </template>
            查询
          </a-button>
          <a-button style="margin-left: 8px" @click="handleReset">重置</a-button>
          <a-button style="margin-left: 8px" @click="handleRefresh" :loading="loading">
            <template #icon>
              <ReloadOutlined />
            </template>
            刷新
          </a-button>
        </a-form-item>
      </a-form>
    </a-card>

    <!-- 任务列表表格 -->
    <a-table
      :columns="columns"
      :data-source="tasks"
      :loading="loading"
      :pagination="pagination"
      :row-selection="rowSelection"
      @change="handleTableChange"
      row-key="task_id"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">
            <template #icon v-if="record.status === 'running'">
              <SyncOutlined spin />
            </template>
            {{ getStatusText(record.status) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'type'">
          <a-tag :color="getTypeColor(record.type)">
            {{ getTypeText(record.type) }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'target_type'">
          <a-tag>{{ getTargetTypeText(record.target_type) }}</a-tag>
        </template>
        <template v-else-if="column.key === 'action'">
          <a-space>
            <!-- 只有待执行状态才能执行 -->
            <a-popconfirm
              v-if="record.status === 'pending'"
              title="确定要执行此任务吗？"
              ok-text="确定"
              cancel-text="取消"
              @confirm="handleRun(record)"
            >
              <a-button
                type="link"
                :loading="runningTasks[record.task_id]"
              >
                <template #icon>
                  <PlayCircleOutlined />
                </template>
                执行
              </a-button>
            </a-popconfirm>
            <!-- 执行中显示取消按钮 -->
            <a-popconfirm
              v-if="record.status === 'running'"
              title="确定要取消此任务吗？"
              ok-text="确定"
              cancel-text="取消"
              @confirm="handleCancel(record)"
            >
              <a-button
                type="link"
                danger
                :loading="cancelingTasks[record.task_id]"
              >
                <template #icon>
                  <StopOutlined />
                </template>
                取消
              </a-button>
            </a-popconfirm>
            <a-button type="link" @click="handleViewDetail(record)">
              <template #icon>
                <EyeOutlined />
              </template>
              查看
            </a-button>
          </a-space>
        </template>
      </template>
    </a-table>

    <!-- 创建任务对话框 -->
    <TaskModal
      v-model:visible="modalVisible"
      @success="handleModalSuccess"
    />

    <!-- 任务详情对话框 -->
    <a-modal
      v-model:open="detailModalVisible"
      title="任务详情"
      width="900px"
      :footer="null"
    >
      <template #title>
        <div class="modal-title-with-refresh">
          <span>任务详情</span>
          <a-button
            type="text"
            size="small"
            @click="handleRefreshDetail"
            :loading="detailRefreshing"
            class="refresh-detail-btn"
          >
            <template #icon>
              <ReloadOutlined />
            </template>
            刷新
          </a-button>
        </div>
      </template>
      <a-descriptions v-if="selectedTask" :column="2" bordered size="small">
        <a-descriptions-item label="任务ID" :span="2">
          <span style="font-family: monospace;">{{ selectedTask.task_id }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="任务名称">
          {{ selectedTask.name }}
        </a-descriptions-item>
        <a-descriptions-item label="任务类型">
          <a-tag :color="getTypeColor(selectedTask.type)">
            {{ getTypeText(selectedTask.type) }}
          </a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="状态">
          <a-tag :color="getStatusColor(selectedTask.status)">
            <template #icon v-if="selectedTask.status === 'running'">
              <SyncOutlined spin />
            </template>
            {{ getStatusText(selectedTask.status) }}
          </a-tag>
          <!-- 执行中显示取消按钮 -->
          <a-popconfirm
            v-if="selectedTask.status === 'running'"
            title="确定要取消此任务吗？"
            ok-text="确定"
            cancel-text="取消"
            @confirm="handleCancelFromDetail"
          >
            <a-button
              type="link"
              danger
              size="small"
              style="margin-left: 8px;"
              :loading="cancelingTasks[selectedTask.task_id]"
            >
              取消任务
            </a-button>
          </a-popconfirm>
        </a-descriptions-item>
        <a-descriptions-item label="目标类型">
          <a-tag>{{ getTargetTypeText(selectedTask.target_type) }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="关联策略" :span="2">
          {{ selectedTask.policy_id || '-' }}
        </a-descriptions-item>
        <a-descriptions-item label="目标主机" :span="2">
          <template v-if="selectedTask.target_hosts && selectedTask.target_hosts.length > 0">
            <a-tag v-for="host in selectedTask.target_hosts.slice(0, 5)" :key="host">
              {{ host }}
            </a-tag>
            <span v-if="selectedTask.target_hosts.length > 5">
              等 {{ selectedTask.target_hosts.length }} 台主机
            </span>
          </template>
          <span v-else>全部主机</span>
        </a-descriptions-item>
        <a-descriptions-item label="创建时间">
          {{ formatTime(selectedTask.created_at) }}
        </a-descriptions-item>
        <a-descriptions-item label="执行时间">
          {{ formatTime(selectedTask.executed_at) || '-' }}
        </a-descriptions-item>
        <a-descriptions-item label="完成时间" :span="2">
          {{ formatTime(selectedTask.completed_at) || '-' }}
        </a-descriptions-item>
      </a-descriptions>

      <!-- 执行进度（如果正在执行） -->
      <div v-if="selectedTask?.status === 'running'" class="task-progress">
        <a-divider>执行进度</a-divider>
        <a-progress
          :percent="taskProgress"
          :status="'active'"
          :format="(percent: number) => `${percent}%`"
          :stroke-color="{
            '0%': '#108ee9',
            '100%': '#87d068',
          }"
        />
        <div class="progress-info">
          <span>已完成: {{ taskResultStats.total }} 项检查</span>
          <span>通过: {{ taskResultStats.pass }}</span>
          <span>失败: {{ taskResultStats.fail }}</span>
        </div>
        <p class="progress-tip">
          <SyncOutlined spin /> 任务正在执行中，请稍候...
        </p>
      </div>

      <!-- 执行结果统计（如果已完成） -->
      <div v-if="selectedTask?.status === 'completed' || selectedTask?.status === 'failed'" class="task-result">
        <a-divider>执行结果</a-divider>
        <a-row :gutter="16">
          <a-col :span="6">
            <a-statistic
              title="总计"
              :value="taskResultStats.total"
            >
              <template #prefix>
                <UnorderedListOutlined />
              </template>
            </a-statistic>
          </a-col>
          <a-col :span="6">
            <a-statistic
              title="通过"
              :value="taskResultStats.pass"
              :value-style="{ color: '#3f8600' }"
            >
              <template #prefix>
                <CheckCircleOutlined />
              </template>
            </a-statistic>
          </a-col>
          <a-col :span="6">
            <a-statistic
              title="失败"
              :value="taskResultStats.fail"
              :value-style="{ color: '#cf1322' }"
            >
              <template #prefix>
                <CloseCircleOutlined />
              </template>
            </a-statistic>
          </a-col>
          <a-col :span="6">
            <a-statistic
              title="错误"
              :value="taskResultStats.error"
              :value-style="{ color: '#faad14' }"
            >
              <template #prefix>
                <ExclamationCircleOutlined />
              </template>
            </a-statistic>
          </a-col>
        </a-row>
      </div>

      <!-- 执行日志 -->
      <div class="task-logs">
        <a-divider>执行日志</a-divider>
        <a-spin :spinning="logsLoading">
          <div class="logs-container" ref="logsContainer">
            <div v-if="taskLogs.length === 0" class="no-logs">
              暂无执行日志
            </div>
            <div v-else class="log-list">
              <div
                v-for="(log, index) in taskLogs"
                :key="index"
                :class="['log-item', `log-${log.level}`]"
              >
                <span class="log-time">{{ log.time }}</span>
                <span class="log-level">{{ log.level.toUpperCase() }}</span>
                <span class="log-message">{{ log.message }}</span>
              </div>
            </div>
          </div>
        </a-spin>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { message, Modal } from 'ant-design-vue'
import {
  PlusOutlined,
  SearchOutlined,
  PlayCircleOutlined,
  EyeOutlined,
  SyncOutlined,
  StopOutlined,
  DeleteOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined,
  UnorderedListOutlined,
  ReloadOutlined,
} from '@ant-design/icons-vue'
import { tasksApi } from '@/api/tasks'
import { resultsApi } from '@/api/results'
import type { ScanTask } from '@/api/types'
import TaskModal from './components/TaskModal.vue'

interface TaskLog {
  time: string
  level: 'info' | 'success' | 'warning' | 'error'
  message: string
}

const loading = ref(false)
const tasks = ref<ScanTask[]>([])
const filters = reactive({
  status: undefined as string | undefined,
  type: undefined as string | undefined,
  name: undefined as string | undefined,
})
const modalVisible = ref(false)
const detailModalVisible = ref(false)
const selectedTask = ref<ScanTask | null>(null)
const runningTasks = reactive<Record<string, boolean>>({})
const cancelingTasks = reactive<Record<string, boolean>>({})
const taskProgress = ref(0)
const taskResultStats = reactive({
  total: 0,
  pass: 0,
  fail: 0,
  error: 0,
})
const taskLogs = ref<TaskLog[]>([])
const logsLoading = ref(false)
const logsContainer = ref<HTMLElement | null>(null)
const detailRefreshing = ref(false)

// 批量操作相关
const selectedRowKeys = ref<string[]>([])
const batchCanceling = ref(false)
const batchDeleting = ref(false)

// 行选择配置
const rowSelection = computed(() => ({
  selectedRowKeys: selectedRowKeys.value,
  onChange: (keys: string[]) => {
    selectedRowKeys.value = keys
  },
  getCheckboxProps: (record: ScanTask) => ({
    disabled: false,
  }),
}))

// 自动刷新定时器
let refreshTimer: ReturnType<typeof setInterval> | null = null
let detailRefreshTimer: ReturnType<typeof setInterval> | null = null

const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  {
    title: '任务名称',
    dataIndex: 'name',
    key: 'name',
    width: 200,
  },
  {
    title: '任务类型',
    dataIndex: 'type',
    key: 'type',
    width: 120,
  },
  {
    title: '目标类型',
    key: 'target_type',
    width: 120,
  },
  {
    title: '关联策略',
    dataIndex: 'policy_id',
    key: 'policy_id',
    width: 200,
    ellipsis: true,
  },
  {
    title: '状态',
    key: 'status',
    width: 120,
  },
  {
    title: '创建时间',
    dataIndex: 'created_at',
    key: 'created_at',
    width: 180,
    customRender: ({ text }: { text: string }) => formatTime(text),
  },
  {
    title: '执行时间',
    dataIndex: 'executed_at',
    key: 'executed_at',
    width: 180,
    customRender: ({ text }: { text: string }) => formatTime(text) || '-',
  },
  {
    title: '操作',
    key: 'action',
    width: 150,
    fixed: 'right' as const,
  },
]

const loadTasks = async () => {
  loading.value = true
  try {
    const response = await tasksApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      status: filters.status,
    }) as any
    tasks.value = response.items || []
    pagination.total = response.total || 0
  } catch (error) {
    console.error('加载任务列表失败:', error)
    message.error('加载任务列表失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.current = 1
  loadTasks()
}

const handleReset = () => {
  filters.status = undefined
  filters.type = undefined
  filters.name = undefined
  pagination.current = 1
  selectedRowKeys.value = []
  loadTasks()
}

const handleRefresh = () => {
  loadTasks()
  message.success('已刷新')
}

// 批量取消任务
const handleBatchCancel = () => {
  const cancelableTasks = tasks.value.filter(
    t => selectedRowKeys.value.includes(t.task_id) && (t.status === 'pending' || t.status === 'running')
  )

  if (cancelableTasks.length === 0) {
    message.warning('没有可取消的任务（只有待执行或执行中的任务可以取消）')
    return
  }

  Modal.confirm({
    title: '批量取消任务',
    content: `确定要取消 ${cancelableTasks.length} 个任务吗？`,
    okText: '确认取消',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      batchCanceling.value = true
      let successCount = 0
      let failCount = 0

      for (const task of cancelableTasks) {
        try {
          await tasksApi.cancel(task.task_id)
          successCount++
        } catch (error) {
          console.error(`取消任务 ${task.task_id} 失败:`, error)
          failCount++
        }
      }

      batchCanceling.value = false
      selectedRowKeys.value = []

      if (failCount === 0) {
        message.success(`成功取消 ${successCount} 个任务`)
      } else {
        message.warning(`成功取消 ${successCount} 个任务，${failCount} 个任务取消失败`)
      }

      loadTasks()
    },
  })
}

// 批量删除任务
const handleBatchDelete = () => {
  const deletableTasks = tasks.value.filter(
    t => selectedRowKeys.value.includes(t.task_id) && (t.status === 'pending' || t.status === 'completed' || t.status === 'failed')
  )

  if (deletableTasks.length === 0) {
    message.warning('没有可删除的任务（执行中的任务不能删除）')
    return
  }

  Modal.confirm({
    title: '批量删除任务',
    content: `确定要删除 ${deletableTasks.length} 个任务吗？此操作不可恢复。`,
    okText: '确认删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      batchDeleting.value = true
      let successCount = 0
      let failCount = 0

      for (const task of deletableTasks) {
        try {
          await tasksApi.delete(task.task_id)
          successCount++
        } catch (error) {
          console.error(`删除任务 ${task.task_id} 失败:`, error)
          failCount++
        }
      }

      batchDeleting.value = false
      selectedRowKeys.value = []

      if (failCount === 0) {
        message.success(`成功删除 ${successCount} 个任务`)
      } else {
        message.warning(`成功删除 ${successCount} 个任务，${failCount} 个任务删除失败`)
      }

      loadTasks()
    },
  })
}

const handleCreate = () => {
  modalVisible.value = true
}

const handleRun = async (record: ScanTask) => {
  runningTasks[record.task_id] = true
  try {
    await tasksApi.run(record.task_id)
    message.success('任务已开始执行')
    loadTasks()
    // 开始自动刷新
    startAutoRefresh()
  } catch (error: any) {
    console.error('执行任务失败:', error)
    if (error.response?.status === 409) {
      message.warning('任务正在执行中，请勿重复执行')
    } else {
      message.error('执行任务失败: ' + (error.response?.data?.message || error.message))
    }
  } finally {
    runningTasks[record.task_id] = false
  }
}

const handleCancel = async (record: ScanTask) => {
  cancelingTasks[record.task_id] = true
  try {
    // 调用取消任务 API
    await tasksApi.cancel(record.task_id)
    message.success('任务已取消')
    loadTasks()
  } catch (error: any) {
    console.error('取消任务失败:', error)
    message.error('取消任务失败: ' + (error.response?.data?.message || error.message))
  } finally {
    cancelingTasks[record.task_id] = false
  }
}

const handleCancelFromDetail = async () => {
  if (!selectedTask.value) return
  await handleCancel(selectedTask.value)
  // 重新加载详情
  if (selectedTask.value) {
    await refreshTaskDetail(selectedTask.value.task_id)
  }
}

const handleViewDetail = async (record: ScanTask) => {
  selectedTask.value = record
  detailModalVisible.value = true
  taskProgress.value = 0
  taskResultStats.total = 0
  taskResultStats.pass = 0
  taskResultStats.fail = 0
  taskResultStats.error = 0
  taskLogs.value = []

  // 加载任务结果统计
  await loadTaskResultStats(record.task_id)

  // 生成执行日志
  generateTaskLogs(record)

  // 如果任务正在执行，启动定时刷新
  if (record.status === 'running') {
    startDetailRefresh(record.task_id)
  }
}

const refreshTaskDetail = async (taskId: string) => {
  try {
    const task = await tasksApi.get(taskId) as any
    selectedTask.value = task
    await loadTaskResultStats(taskId)
    generateTaskLogs(task)

    // 如果任务已完成，停止刷新
    if (task.status !== 'running') {
      stopDetailRefresh()
    }
  } catch (error) {
    console.error('刷新任务详情失败:', error)
  }
}

const loadTaskResultStats = async (taskId: string) => {
  try {
    const response = await resultsApi.list({
      task_id: taskId,
      page_size: 1000,
    }) as any
    const results = response.items || []
    taskResultStats.total = results.length
    taskResultStats.pass = results.filter((r: any) => r.status === 'pass').length
    taskResultStats.fail = results.filter((r: any) => r.status === 'fail').length
    taskResultStats.error = results.filter((r: any) => r.status === 'error').length

    // 计算进度百分比（基于预期检查项数量，如果没有则使用已完成数量）
    if (selectedTask.value?.status === 'running') {
      // 假设每个任务大约有 20 个检查项，根据实际完成数量计算进度
      const expectedTotal = 20 // 可以从任务配置中获取
      const completedPercent = Math.min(Math.round((taskResultStats.total / expectedTotal) * 100), 99)
      taskProgress.value = completedPercent
    } else {
      taskProgress.value = 100
    }
  } catch (error) {
    console.error('加载任务结果统计失败:', error)
  }
}

const generateTaskLogs = (task: ScanTask) => {
  const logs: TaskLog[] = []

  // 创建时间日志
  if (task.created_at) {
    logs.push({
      time: formatLogTime(task.created_at),
      level: 'info',
      message: `任务创建成功，任务ID: ${task.task_id}`,
    })
  }

  // 执行时间日志
  if (task.executed_at) {
    logs.push({
      time: formatLogTime(task.executed_at),
      level: 'info',
      message: '任务开始执行',
    })
    logs.push({
      time: formatLogTime(task.executed_at),
      level: 'info',
      message: `目标主机: ${task.target_type === 'all' ? '全部主机' : (task.target_hosts?.length || 0) + ' 台主机'}`,
    })
    logs.push({
      time: formatLogTime(task.executed_at),
      level: 'info',
      message: `关联策略: ${task.policy_id || '无'}`,
    })
  }

  // 根据当前状态添加日志
  if (task.status === 'running') {
    if (taskResultStats.total > 0) {
      logs.push({
        time: formatLogTime(new Date().toISOString()),
        level: 'info',
        message: `正在执行检查... 已完成 ${taskResultStats.total} 项`,
      })
      if (taskResultStats.pass > 0) {
        logs.push({
          time: formatLogTime(new Date().toISOString()),
          level: 'success',
          message: `${taskResultStats.pass} 项检查通过`,
        })
      }
      if (taskResultStats.fail > 0) {
        logs.push({
          time: formatLogTime(new Date().toISOString()),
          level: 'warning',
          message: `${taskResultStats.fail} 项检查失败`,
        })
      }
    }
  }

  // 完成时间日志
  if (task.completed_at) {
    if (task.status === 'completed') {
      logs.push({
        time: formatLogTime(task.completed_at),
        level: 'success',
        message: `任务执行完成，共检查 ${taskResultStats.total} 项`,
      })
      logs.push({
        time: formatLogTime(task.completed_at),
        level: taskResultStats.fail > 0 ? 'warning' : 'success',
        message: `结果统计: 通过 ${taskResultStats.pass} 项, 失败 ${taskResultStats.fail} 项, 错误 ${taskResultStats.error} 项`,
      })
    } else if (task.status === 'failed') {
      logs.push({
        time: formatLogTime(task.completed_at),
        level: 'error',
        message: '任务执行失败',
      })
    }
  }

  taskLogs.value = logs

  // 滚动到底部
  nextTick(() => {
    if (logsContainer.value) {
      logsContainer.value.scrollTop = logsContainer.value.scrollHeight
    }
  })
}

const formatLogTime = (time: string) => {
  const date = new Date(time)
  if (isNaN(date.getTime())) return time
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

const handleRefreshDetail = async () => {
  if (!selectedTask.value) return
  detailRefreshing.value = true
  try {
    await refreshTaskDetail(selectedTask.value.task_id)
    message.success('已刷新')
  } finally {
    detailRefreshing.value = false
  }
}

const handleModalSuccess = () => {
  modalVisible.value = false
  loadTasks()
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  loadTasks()
}

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = {
    pending: 'default',
    running: 'processing',
    completed: 'success',
    failed: 'error',
  }
  return colors[status] || 'default'
}

const getStatusText = (status: string) => {
  const texts: Record<string, string> = {
    pending: '待执行',
    running: '执行中',
    completed: '已完成',
    failed: '失败',
  }
  return texts[status] || status
}

const getTypeColor = (type: string) => {
  const colors: Record<string, string> = {
    baseline: 'blue',
    manual: 'green',
    scheduled: 'orange',
  }
  return colors[type] || 'default'
}

const getTypeText = (type: string) => {
  const texts: Record<string, string> = {
    baseline: '基线检查',
    manual: '手动任务',
    scheduled: '定时任务',
  }
  return texts[type] || type
}

const getTargetTypeText = (type: string) => {
  const texts: Record<string, string> = {
    all: '全部主机',
    host_ids: '指定主机',
    os_family: '按OS筛选',
  }
  return texts[type] || type
}

const formatTime = (time: string | undefined) => {
  if (!time) return ''
  const date = new Date(time)
  if (isNaN(date.getTime())) return time
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

// 自动刷新列表（当有任务执行中时）
const startAutoRefresh = () => {
  if (refreshTimer) return
  refreshTimer = setInterval(() => {
    const hasRunning = tasks.value.some(t => t.status === 'running')
    if (hasRunning) {
      loadTasks()
    } else {
      stopAutoRefresh()
    }
  }, 5000)
}

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// 详情页定时刷新
const startDetailRefresh = (taskId: string) => {
  if (detailRefreshTimer) return
  detailRefreshTimer = setInterval(() => {
    if (detailModalVisible.value && selectedTask.value?.status === 'running') {
      refreshTaskDetail(taskId)
    } else {
      stopDetailRefresh()
    }
  }, 3000)
}

const stopDetailRefresh = () => {
  if (detailRefreshTimer) {
    clearInterval(detailRefreshTimer)
    detailRefreshTimer = null
  }
}

onMounted(() => {
  loadTasks()
})

onUnmounted(() => {
  stopAutoRefresh()
  stopDetailRefresh()
})
</script>

<style scoped>
.tasks-page {
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

.task-progress {
  margin-top: 16px;
}

.progress-info {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
  color: #666;
  font-size: 13px;
}

.progress-tip {
  text-align: center;
  color: #1890ff;
  margin-top: 8px;
}

.task-result {
  margin-top: 16px;
}

.task-logs {
  margin-top: 16px;
}

.logs-container {
  background: #1e1e1e;
  border-radius: 4px;
  padding: 12px;
  max-height: 300px;
  overflow-y: auto;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
}

.no-logs {
  color: #666;
  text-align: center;
  padding: 20px;
}

.log-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.log-item {
  display: flex;
  gap: 8px;
  line-height: 1.5;
}

.log-time {
  color: #888;
  min-width: 120px;
}

.log-level {
  min-width: 60px;
  font-weight: bold;
}

.log-message {
  flex: 1;
  word-break: break-all;
}

.log-info .log-level {
  color: #1890ff;
}

.log-info .log-message {
  color: #d4d4d4;
}

.log-success .log-level {
  color: #52c41a;
}

.log-success .log-message {
  color: #52c41a;
}

.log-warning .log-level {
  color: #faad14;
}

.log-warning .log-message {
  color: #faad14;
}

.log-error .log-level {
  color: #ff4d4f;
}

.log-error .log-message {
  color: #ff4d4f;
}
</style>
