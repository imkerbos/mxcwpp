<template>
  <div class="inspection-page">
    <!-- 统计卡片 -->
    <a-row :gutter="16" style="margin-bottom: 16px" class="stat-row">
      <a-col :span="8">
        <a-card :bordered="false" class="stat-card">
          <div class="stat-group">
            <div class="stat-item">
              <div class="stat-label">主机总数</div>
              <div class="stat-value">{{ summary.total_hosts }}</div>
            </div>
            <div class="stat-divider" />
            <div class="stat-item">
              <div class="stat-label">在线</div>
              <div class="stat-value" style="color: #52c41a">{{ summary.online_hosts }}</div>
            </div>
            <div class="stat-divider" />
            <div class="stat-item">
              <div class="stat-label">离线</div>
              <div class="stat-value" style="color: #ff4d4f">{{ summary.offline_hosts }}</div>
            </div>
          </div>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card :bordered="false" class="stat-card">
          <div class="stat-group">
            <div class="stat-item stat-item-wide">
              <div class="stat-label">Agent 待更新</div>
              <div class="stat-value" :style="summary.agent_outdated_count > 0 ? { color: '#faad14' } : {}">{{ summary.agent_outdated_count }}</div>
              <div class="stat-hint">最新版本: {{ latestAgentVersion || '-' }}</div>
            </div>
            <div class="stat-divider" />
            <div class="stat-item stat-item-wide">
              <div class="stat-label">插件异常</div>
              <div class="stat-value" :style="summary.plugin_error_count > 0 ? { color: '#ff4d4f' } : {}">{{ summary.plugin_error_count }}</div>
              <div class="stat-hint">停止或错误</div>
            </div>
          </div>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card :bordered="false" class="stat-card">
          <div class="stat-group">
            <div class="stat-item stat-item-full">
              <div class="stat-label">插件待更新</div>
              <div class="stat-value" :style="summary.plugin_outdated_count > 0 ? { color: '#faad14' } : {}">{{ summary.plugin_outdated_count }}</div>
              <div class="stat-hint">
                <span v-for="(ver, name) in latestPluginVersions" :key="name" class="plugin-ver-tag">{{ name }} {{ ver }}</span>
                <span v-if="Object.keys(latestPluginVersions).length === 0">-</span>
              </div>
            </div>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <!-- 主表 -->
    <a-card title="主机巡检详情" :bordered="false">
      <template #extra>
        <a-space>
          <a-select v-model:value="filterStatus" placeholder="状态筛选" style="width: 120px" allow-clear>
            <a-select-option value="online">在线</a-select-option>
            <a-select-option value="offline">离线</a-select-option>
          </a-select>
          <a-select v-model:value="filterIssue" placeholder="问题筛选" style="width: 150px" allow-clear>
            <a-select-option value="agent_outdated">Agent 待更新</a-select-option>
            <a-select-option value="plugin_error">插件异常</a-select-option>
            <a-select-option value="plugin_outdated">插件待更新</a-select-option>
          </a-select>
          <a-select v-model:value="filterAgentVersion" placeholder="Agent 版本" style="width: 130px" allow-clear>
            <a-select-option v-for="ver in agentVersionOptions" :key="ver" :value="ver">{{ ver }}</a-select-option>
          </a-select>
          <a-input-search v-model:value="searchText" placeholder="搜索主机名/IP" style="width: 200px" allow-clear />
          <a-button @click="loadData">
            <template #icon><ReloadOutlined /></template>
          </a-button>
        </a-space>
      </template>

      <a-table
        :columns="columns"
        :data-source="filteredHosts"
        :loading="loading"
        :row-selection="rowSelection"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal: (total: number) => `共 ${total} 条` }"
        row-key="host_id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'hostname'">
            <div>
              <router-link :to="`/hosts/${record.host_id}`" class="host-link">{{ record.hostname }}</router-link>
              <div class="host-id">{{ record.host_id.substring(0, 12) }}</div>
            </div>
          </template>
          <template v-else-if="column.key === 'ipv4'">
            <span>{{ record.ipv4?.[0] || '-' }}</span>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="record.status === 'online' ? 'green' : 'red'" style="margin: 0">
              {{ record.status === 'online' ? '在线' : '离线' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'agent_version'">
            <div style="display: flex; align-items: center; gap: 4px; white-space: nowrap;">
              <span>{{ record.agent_version || '-' }}</span>
              <a-tag v-if="isAgentOutdated(record)" color="warning" size="small" style="margin: 0">待更新</a-tag>
            </div>
          </template>
          <template v-else-if="column.key === 'plugins'">
            <div v-if="record.plugins && record.plugins.length > 0" class="plugin-list">
              <div v-for="p in record.plugins" :key="p.name" class="plugin-row">
                <span class="plugin-name">{{ p.name }}</span>
                <a-tag :color="pluginStatusColor(p.status)" size="small" style="margin: 0">{{ pluginStatusText(p.status) }}</a-tag>
                <span class="plugin-ver">{{ p.version || '-' }}</span>
                <a-tag v-if="p.need_update" color="warning" size="small" style="margin: 0">待更新</a-tag>
              </div>
            </div>
            <span v-else style="color: #bfbfbf">-</span>
          </template>
          <template v-else-if="column.key === 'last_heartbeat'">
            <span style="white-space: nowrap">{{ formatTime(record.last_heartbeat) }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-popconfirm title="确定重启此主机的 Agent？" @confirm="handleRestartAgent(record)">
              <a-button type="link" size="small" :disabled="record.status !== 'online'">重启</a-button>
            </a-popconfirm>
          </template>
        </template>
      </a-table>

      <!-- 批量操作 -->
      <div v-if="selectedRowKeys.length > 0" class="batch-bar">
        <span>已选择 {{ selectedRowKeys.length }} 台主机</span>
        <a-space>
          <a-popconfirm title="确定批量重启选中主机的 Agent？" @confirm="handleBatchRestart">
            <a-button type="primary" danger size="small">批量重启 Agent</a-button>
          </a-popconfirm>
        </a-space>
      </div>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { inspectionApi, type InspectionHostItem, type InspectionSummary } from '@/api/inspection'
import { hostsApi } from '@/api/hosts'
import { formatDateTime } from '@/utils/date'

const loading = ref(false)
const hosts = ref<InspectionHostItem[]>([])
const summary = ref<InspectionSummary>({
  total_hosts: 0,
  online_hosts: 0,
  offline_hosts: 0,
  agent_outdated_count: 0,
  plugin_error_count: 0,
  plugin_outdated_count: 0,
})
const selectedRowKeys = ref<string[]>([])
const searchText = ref('')
const filterStatus = ref<string | undefined>(undefined)
const filterIssue = ref<string | undefined>(undefined)
const filterAgentVersion = ref<string | undefined>(undefined)
const latestAgentVersion = ref('')
const latestPluginVersions = ref<Record<string, string>>({})

const rowSelection = computed(() => ({
  selectedRowKeys: selectedRowKeys.value,
  onChange: (keys: string[]) => {
    selectedRowKeys.value = keys
  },
}))

const columns = [
  { title: '主机名', key: 'hostname', width: 200 },
  { title: 'IP', key: 'ipv4', width: 150 },
  { title: '状态', key: 'status', width: 80, align: 'center' as const },
  { title: 'Agent 版本', key: 'agent_version', width: 140 },
  { title: '插件状态', key: 'plugins' },
  { title: '最近心跳', key: 'last_heartbeat', width: 170 },
  { title: '操作', key: 'action', width: 70, align: 'center' as const },
]

const agentVersionOptions = computed(() => {
  const versions = new Set<string>()
  for (const h of hosts.value) {
    if (h.agent_version) versions.add(h.agent_version)
  }
  return Array.from(versions).sort()
})

const filteredHosts = computed(() => {
  let result = hosts.value
  if (filterStatus.value) {
    result = result.filter(h => h.status === filterStatus.value)
  }
  if (filterAgentVersion.value) {
    result = result.filter(h => h.agent_version === filterAgentVersion.value)
  }
  if (filterIssue.value === 'agent_outdated') {
    result = result.filter(h => isAgentOutdated(h))
  } else if (filterIssue.value === 'plugin_error') {
    result = result.filter(h => h.plugins?.some(p => p.status === 'error' || p.status === 'stopped'))
  } else if (filterIssue.value === 'plugin_outdated') {
    result = result.filter(h => h.plugins?.some(p => p.need_update))
  }
  if (searchText.value) {
    const s = searchText.value.toLowerCase()
    result = result.filter(h =>
      h.hostname.toLowerCase().includes(s) ||
      h.ipv4?.some(ip => ip.includes(s)) ||
      h.host_id.toLowerCase().includes(s)
    )
  }
  return result
})

const isAgentOutdated = (record: InspectionHostItem) => {
  if (!latestAgentVersion.value || !record.agent_version) return false
  return record.agent_version !== latestAgentVersion.value
}

const pluginStatusColor = (status: string) => {
  switch (status) {
    case 'running': return 'green'
    case 'stopped': return 'orange'
    case 'error': return 'red'
    case 'updating': return 'blue'
    default: return 'default'
  }
}

const pluginStatusText = (status: string) => {
  switch (status) {
    case 'running': return '运行中'
    case 'stopped': return '已停止'
    case 'error': return '异常'
    case 'updating': return '更新中'
    default: return status
  }
}

const formatTime = (time: string | null) => {
  if (!time) return '-'
  return formatDateTime(time)
}

const loadData = async () => {
  loading.value = true
  try {
    const data = await inspectionApi.getOverview()
    summary.value = data.summary
    hosts.value = data.hosts
    latestAgentVersion.value = data.latest_agent_version || ''
    latestPluginVersions.value = data.latest_plugin_versions || {}
  } catch (error) {
    console.error('加载巡检数据失败:', error)
    message.error('加载巡检数据失败')
  } finally {
    loading.value = false
  }
}

const handleRestartAgent = async (record: InspectionHostItem) => {
  try {
    await hostsApi.restartAgent([record.host_id])
    message.success('重启命令已提交')
  } catch (error: any) {
    message.error(error?.message || '重启失败')
  }
}

const handleBatchRestart = async () => {
  try {
    await hostsApi.restartAgent(selectedRowKeys.value)
    message.success(`已提交 ${selectedRowKeys.value.length} 台主机的重启命令`)
    selectedRowKeys.value = []
  } catch (error: any) {
    message.error(error?.message || '批量重启失败')
  }
}

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.inspection-page {
  width: 100%;
}

.stat-row {
  display: flex;
  align-items: stretch;
}

.stat-row > .ant-col {
  display: flex;
}

.stat-card {
  width: 100%;
}

.stat-card :deep(.ant-card-body) {
  padding: 20px 24px;
}

.stat-group {
  display: flex;
  align-items: center;
  min-height: 80px;
}

.stat-item {
  flex: 1;
  text-align: center;
  padding: 0 12px;
}

.stat-item-wide {
  flex: 1;
}

.stat-item-full {
  flex: 1;
  text-align: center;
}

.stat-divider {
  width: 1px;
  height: 48px;
  background: #f0f0f0;
  flex-shrink: 0;
}

.stat-label {
  font-size: 13px;
  color: rgba(0, 0, 0, 0.45);
  margin-bottom: 8px;
}

.stat-value {
  font-size: 28px;
  font-weight: 600;
  color: rgba(0, 0, 0, 0.85);
  line-height: 1.2;
}

.stat-hint {
  margin-top: 6px;
  font-size: 12px;
  color: rgba(0, 0, 0, 0.35);
}

.plugin-ver-tag {
  display: inline-block;
  padding: 0 6px;
  margin: 0 2px;
  background: #f5f5f5;
  border-radius: 3px;
  font-size: 11px;
}

.host-link {
  color: #1890ff;
  text-decoration: none;
}

.host-link:hover {
  color: #40a9ff;
  text-decoration: underline;
}

.host-id {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.35);
  line-height: 1.4;
}

.plugin-list {
  line-height: 1.8;
}

.plugin-row {
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}

.plugin-name {
  color: rgba(0, 0, 0, 0.65);
  min-width: 58px;
}

.plugin-ver {
  color: #8c8c8c;
  font-size: 13px;
}

.batch-bar {
  margin-top: 12px;
  padding: 8px 16px;
  background: #e6f7ff;
  border: 1px solid #91d5ff;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>
