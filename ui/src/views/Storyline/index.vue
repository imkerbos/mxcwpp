<template>
  <div class="storyline-page">
    <div class="page-header">
      <h2>攻击故事线</h2>
      <a-button @click="handleRefresh">
        <ReloadOutlined /> 刷新
      </a-button>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="16" class="stat-cards">
      <a-col :span="8">
        <a-card size="small">
          <a-statistic title="总故事线" :value="stats.total" />
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card size="small">
          <a-statistic title="活跃" :value="stats.active" :value-style="{ color: '#FF7D00' }" />
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card size="small">
          <a-statistic title="严重 (活跃)" :value="stats.critical_active" :value-style="{ color: '#F53F3F' }" />
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
        <a-select-option value="active">活跃</a-select-option>
        <a-select-option value="investigating">调查中</a-select-option>
        <a-select-option value="resolved">已处理</a-select-option>
      </a-select>
    </div>

    <!-- 故事线表格 -->
    <a-table
      :columns="columns"
      :data-source="storylines"
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
        <template v-if="column.key === 'risk_score'">
          <a-progress
            :percent="record.risk_score"
            :stroke-color="record.risk_score >= 80 ? '#F53F3F' : record.risk_score >= 60 ? '#FF7D00' : '#F7BA1E'"
            :size="[80, 6]"
            :show-info="true"
          />
        </template>
        <template v-if="column.key === 'phase'">
          <a-tag color="volcano">{{ getPhaseLabel(record.phase) }}</a-tag>
        </template>
        <template v-if="column.key === 'status'">
          <a-tag :color="getStatusColor(record.status)">{{ getStatusLabel(record.status) }}</a-tag>
        </template>
        <template v-if="column.key === 'action'">
          <a-space>
            <a @click="showDetail(record)">详情</a>
            <a v-if="record.status === 'active'" @click="handleResolve(record)">处理</a>
          </a-space>
        </template>
      </template>
    </a-table>

    <!-- 故事线详情弹窗 -->
    <a-modal
      v-model:open="detailVisible"
      title="攻击故事线详情"
      :width="900"
      :footer="null"
    >
      <a-spin :spinning="detailLoading">
        <template v-if="detail">
          <a-descriptions :column="2" bordered size="small" style="margin-bottom: 16px">
            <a-descriptions-item label="故事线 ID">{{ detail.storyline.story_id }}</a-descriptions-item>
            <a-descriptions-item label="主机">{{ detail.storyline.hostname }}</a-descriptions-item>
            <a-descriptions-item label="严重度">
              <a-tag :color="getSeverityConfig(detail.storyline.severity).tagColor">
                {{ getSeverityConfig(detail.storyline.severity).label }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="风险分">{{ detail.storyline.risk_score }}</a-descriptions-item>
            <a-descriptions-item label="ATT&CK 阶段">{{ getPhaseLabel(detail.storyline.phase) }}</a-descriptions-item>
            <a-descriptions-item label="事件数">{{ detail.storyline.event_count }}</a-descriptions-item>
            <a-descriptions-item label="匹配规则" :span="2">{{ detail.storyline.rule_names || '-' }}</a-descriptions-item>
            <a-descriptions-item label="摘要" :span="2">{{ detail.storyline.summary || '-' }}</a-descriptions-item>
            <a-descriptions-item label="首次发现">{{ detail.storyline.first_seen_at }}</a-descriptions-item>
            <a-descriptions-item label="最后活动">{{ detail.storyline.last_seen_at }}</a-descriptions-item>
          </a-descriptions>

          <h4>事件时间线 ({{ detail.events.length }})</h4>
          <a-timeline mode="left">
            <a-timeline-item
              v-for="event in detail.events"
              :key="event.id"
              :color="getTimelineColor(event)"
            >
              <div class="timeline-event">
                <div class="timeline-header">
                  <span class="timeline-time">{{ event.timestamp }}</span>
                  <a-tag size="small" :color="getEventTypeColor(event.event_type)">
                    {{ event.event_type }}
                  </a-tag>
                  <a-tag v-if="event.rule_name" size="small" color="red">{{ event.rule_name }}</a-tag>
                </div>
                <div class="timeline-body">
                  <code>{{ event.exe }}</code>
                  <span v-if="event.pid" class="timeline-pid"> (PID: {{ event.pid }})</span>
                </div>
              </div>
            </a-timeline-item>
          </a-timeline>
        </template>
      </a-spin>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { storylineApi } from '@/api/storyline'
import type { Storyline, StorylineDetail, StorylineStats, StorylineEvent } from '@/api/storyline'
import { getSeverityConfig } from '@/constants/severity'

const loading = ref(false)
const detailLoading = ref(false)
const detailVisible = ref(false)
const storylines = ref<Storyline[]>([])
const detail = ref<StorylineDetail | null>(null)
const stats = reactive<StorylineStats>({ total: 0, active: 0, critical_active: 0 })

const filters = reactive({
  host_id: '',
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
  { title: '最后活动', dataIndex: 'last_seen_at', width: 170 },
  { title: '主机', dataIndex: 'hostname', width: 120, ellipsis: true },
  { title: '严重度', key: 'severity', width: 80, align: 'center' as const },
  { title: '风险分', key: 'risk_score', width: 120 },
  { title: '阶段', key: 'phase', width: 110 },
  { title: '事件数', dataIndex: 'event_count', width: 80, align: 'center' as const },
  { title: '告警数', dataIndex: 'alert_count', width: 80, align: 'center' as const },
  { title: '摘要', dataIndex: 'summary', ellipsis: true },
  { title: '状态', key: 'status', width: 80, align: 'center' as const },
  { title: '操作', key: 'action', width: 100 },
]

const getPhaseLabel = (phase: string) => {
  const labels: Record<string, string> = {
    initial_access: '初始访问',
    execution: '执行',
    persistence: '持久化',
    privilege_escalation: '提权',
    defense_evasion: '防御规避',
    credential_access: '凭据访问',
    discovery: '发现',
    lateral_movement: '横向移动',
    collection: '数据收集',
    exfiltration: '数据窃取',
    command_and_control: 'C2 通信',
    impact: '影响',
  }
  return labels[phase] || phase || '-'
}

const getStatusColor = (status: string) => {
  const colors: Record<string, string> = { active: 'orange', investigating: 'blue', resolved: 'green' }
  return colors[status] || 'default'
}

const getStatusLabel = (status: string) => {
  const labels: Record<string, string> = { active: '活跃', investigating: '调查中', resolved: '已处理' }
  return labels[status] || status
}

const getEventTypeColor = (type: string) => {
  const colors: Record<string, string> = {
    process_exec: 'blue', file_open: 'orange', file_write: 'orange',
    tcp_connect: 'cyan', udp_send: 'green', dns_query: 'purple',
  }
  return colors[type] || 'default'
}

const getTimelineColor = (event: StorylineEvent) => {
  return event.rule_name ? 'red' : 'blue'
}

const fetchStorylines = async () => {
  loading.value = true
  try {
    const res = await storylineApi.list({
      page: pagination.current,
      page_size: pagination.pageSize,
      host_id: filters.host_id || undefined,
      severity: filters.severity,
      status: filters.status,
    })
    storylines.value = res.items || []
    pagination.total = res.total
  } catch {
    // handled
  } finally {
    loading.value = false
  }
}

const fetchStats = async () => {
  try {
    const res = await storylineApi.stats()
    Object.assign(stats, res)
  } catch {
    // silent
  }
}

const showDetail = async (record: Storyline) => {
  detailVisible.value = true
  detailLoading.value = true
  try {
    detail.value = await storylineApi.get(record.story_id)
  } catch {
    // handled
  } finally {
    detailLoading.value = false
  }
}

const handleResolve = async (record: Storyline) => {
  try {
    await storylineApi.resolve(record.story_id)
    message.success('故事线已标记为已处理')
    fetchStorylines()
    fetchStats()
  } catch {
    // handled
  }
}

const handleSearch = () => {
  pagination.current = 1
  fetchStorylines()
}

const handleRefresh = () => {
  fetchStorylines()
  fetchStats()
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  fetchStorylines()
}

onMounted(() => {
  fetchStorylines()
  fetchStats()
})
</script>

<style scoped>
.storyline-page { padding: 0; }
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
.timeline-event { margin-bottom: 4px; }
.timeline-header { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
.timeline-time { color: #999; font-size: 12px; }
.timeline-body code { font-size: 12px; color: #333; }
.timeline-pid { color: #999; font-size: 12px; }
</style>
