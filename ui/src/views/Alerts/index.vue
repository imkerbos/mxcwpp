<template>
  <div class="alerts-page">
    <div class="page-header">
      <h2 class="page-title">告警管理</h2>
      <a-button type="primary" @click="showConfigModal = true">
        <template #icon><SettingOutlined /></template>
        告警配置
      </a-button>
    </div>
    
    <!-- 统计 -->
    <div class="stats">
      <div class="stat-card">
        <div class="stat-value">{{ statistics.active || 0 }}</div>
        <div class="stat-label">活跃告警</div>
      </div>
      <div class="stat-card">
        <div class="stat-value critical">{{ statistics.critical || 0 }}</div>
        <div class="stat-label">严重</div>
      </div>
      <div class="stat-card">
        <div class="stat-value high">{{ statistics.high || 0 }}</div>
        <div class="stat-label">高危</div>
      </div>
      <div class="stat-card">
        <div class="stat-value medium">{{ statistics.medium || 0 }}</div>
        <div class="stat-label">中危</div>
      </div>
      <div class="stat-card">
        <div class="stat-value low">{{ statistics.low || 0 }}</div>
        <div class="stat-label">低危</div>
      </div>
    </div>

    <!-- 标签切换 -->
    <div class="tabs-header">
      <div 
        :class="['tab-item', { active: activeTab === 'active' }]"
        @click="activeTab = 'active'; handleTabChange()"
      >
        活跃告警
      </div>
      <div 
        :class="['tab-item', { active: activeTab === 'history' }]"
        @click="activeTab = 'history'; handleTabChange()"
      >
        历史告警
      </div>
    </div>

    <!-- 表格 -->
    <AlertTable
      v-if="activeTab === 'active'"
      :alerts="alerts"
      :loading="loading"
      :pagination="pagination"
      :filters="filters"
      status="active"
      @change="handleTableChange"
      @resolve="handleResolve"
      @ignore="handleIgnore"
      @refresh="loadAlerts"
    />
    <AlertTable
      v-else
      :alerts="alerts"
      :loading="loading"
      :pagination="pagination"
      :filters="filters"
      status="history"
      @change="handleTableChange"
      @refresh="loadAlerts"
    />

    <!-- 告警配置弹窗 -->
    <a-modal
      v-model:open="showConfigModal"
      title="告警配置"
      @ok="handleSaveConfig"
      :confirm-loading="savingConfig"
    >
      <a-form layout="vertical">
        <a-form-item label="重复告警通知间隔">
          <a-select v-model:value="alertConfig.repeat_alert_interval" style="width: 100%">
            <a-select-option :value="15">15 分钟</a-select-option>
            <a-select-option :value="30">30 分钟</a-select-option>
            <a-select-option :value="60">1 小时</a-select-option>
            <a-select-option :value="120">2 小时</a-select-option>
            <a-select-option :value="360">6 小时</a-select-option>
            <a-select-option :value="720">12 小时</a-select-option>
            <a-select-option :value="1440">24 小时</a-select-option>
          </a-select>
          <div class="config-tip">
            同一主机同一问题在此间隔内只会通知一次，避免重复告警轰炸
          </div>
        </a-form-item>
        <a-form-item label="定期汇总">
          <a-switch v-model:checked="alertConfig.enable_periodic_summary" />
          <span style="margin-left: 8px; color: #8c8c8c;">
            {{ alertConfig.enable_periodic_summary ? '已启用：已存在的告警按间隔定期发送' : '已关闭：只在首次发现时发送通知' }}
          </span>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import { SettingOutlined } from '@ant-design/icons-vue'
import { alertsApi, type Alert, type AlertStatistics } from '@/api/alerts'
import { systemConfigApi, type AlertConfig } from '@/api/system-config'
import AlertTable from './components/AlertTable.vue'

const activeTab = ref<'active' | 'history'>('active')
const loading = ref(false)
const alerts = ref<Alert[]>([])
const statistics = ref<AlertStatistics>({
  total: 0,
  active: 0,
  resolved: 0,
  ignored: 0,
  critical: 0,
  high: 0,
  medium: 0,
  low: 0,
})

// 告警配置
const showConfigModal = ref(false)
const savingConfig = ref(false)
const alertConfig = reactive<AlertConfig>({
  repeat_alert_interval: 30,
  enable_periodic_summary: true,
})

const pagination = ref({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const filters = ref({
  severity: undefined as 'critical' | 'high' | 'medium' | 'low' | undefined,
  host_id: undefined as string | undefined,
  category: undefined as string | undefined,
  alert_type: undefined as 'baseline' | 'agent_offline' | undefined,
  keyword: undefined as string | undefined,
})


const loadStatistics = async () => {
  try {
    const data = await alertsApi.statistics()
    statistics.value = data
  } catch (error: any) {
    console.error('加载告警统计失败:', error)
  }
}

const loadAlerts = async () => {
  loading.value = true
  try {
    const params: any = {
      page: pagination.value.current,
      page_size: pagination.value.pageSize,
    }

    // 根据标签页设置状态
    if (activeTab.value === 'active') {
      params.status = 'active'
    } else {
      // 历史告警：已解决或已忽略
      params.status = 'resolved,ignored'
    }

    // 添加其他过滤条件
    if (filters.value.severity) {
      params.severity = filters.value.severity
    }
    if (filters.value.host_id) {
      params.host_id = filters.value.host_id
    }
    if (filters.value.category) {
      params.category = filters.value.category
    }
    if (filters.value.alert_type) {
      params.alert_type = filters.value.alert_type
    }
    if (filters.value.keyword) {
      params.keyword = filters.value.keyword
    }

    const response = await alertsApi.list(params)
    alerts.value = response.items || []
    pagination.value.total = response.total || 0
  } catch (error: any) {
    message.error(error?.message || '加载告警列表失败')
  } finally {
    loading.value = false
  }
}

const handleTabChange = () => {
  pagination.value.current = 1
  loadAlerts()
}

const handleTableChange = (newFilters: any) => {
  // 如果传入了 page 参数，使用传入的值（翻页操作）
  if (newFilters.page) {
    pagination.value.current = newFilters.page
  } else {
    // 否则重置为第 1 页（筛选条件变化）
    pagination.value.current = 1
  }
  // 如果传入了 pageSize 参数，更新每页条数
  if (newFilters.pageSize) {
    pagination.value.pageSize = newFilters.pageSize
  }
  // 更新其他过滤条件（排除 page 和 pageSize）
  const { page, pageSize, ...otherFilters } = newFilters
  filters.value = { ...filters.value, ...otherFilters }
  loadAlerts()
}

const handleResolve = async (alert: Alert, reason?: string) => {
  try {
    await alertsApi.resolve(alert.id, reason)
    message.success('告警已解决')
    loadAlerts()
    loadStatistics()
  } catch (error: any) {
    message.error(error?.message || '解决告警失败')
  }
}

const handleIgnore = async (alert: Alert) => {
  try {
    await alertsApi.ignore(alert.id)
    message.success('告警已忽略')
    loadAlerts()
    loadStatistics()
  } catch (error: any) {
    message.error(error?.message || '忽略告警失败')
  }
}

// 加载告警配置
const loadAlertConfig = async () => {
  try {
    const config = await systemConfigApi.getAlertConfig()
    alertConfig.repeat_alert_interval = config.repeat_alert_interval
    alertConfig.enable_periodic_summary = config.enable_periodic_summary
  } catch (error: any) {
    console.error('加载告警配置失败:', error)
  }
}

// 保存告警配置
const handleSaveConfig = async () => {
  savingConfig.value = true
  try {
    await systemConfigApi.updateAlertConfig(alertConfig)
    message.success('告警配置保存成功')
    showConfigModal.value = false
  } catch (error: any) {
    message.error(error?.message || '保存告警配置失败')
  } finally {
    savingConfig.value = false
  }
}

onMounted(() => {
  loadStatistics()
  loadAlerts()
  loadAlertConfig()
})
</script>

<style scoped lang="less">
.alerts-page {
  padding: 24px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.page-title {
  font-size: 20px;
  font-weight: 500;
  margin: 0;
  color: #262626;
}

.config-tip {
  margin-top: 8px;
  font-size: 13px;
  color: #8c8c8c;
}

.stats {
  display: flex;
  gap: 16px;
  margin-bottom: 24px;
}

.stat-card {
  flex: 1;
  background: #fff;
  border: 1px solid #e8e8e8;
  border-radius: 4px;
  padding: 20px;
  text-align: center;
  transition: all 0.2s;

  &:hover {
    border-color: #d9d9d9;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  }
}

.stat-value {
  font-size: 32px;
  font-weight: 600;
  color: #262626;
  margin-bottom: 8px;
  line-height: 1;

  &.critical {
    color: #ff4d4f;
  }

  &.high {
    color: #ff7a45;
  }

  &.medium {
    color: #faad14;
  }

  &.low {
    color: #1890ff;
  }
}

.stat-label {
  font-size: 14px;
  color: #8c8c8c;
  font-weight: 400;
}

.tabs-header {
  display: flex;
  gap: 0;
  margin-bottom: 24px;
  background: #fff;
  border: 1px solid #e8e8e8;
  border-radius: 4px;
  padding: 4px;
}

.tab-item {
  flex: 1;
  padding: 10px 20px;
  text-align: center;
  cursor: pointer;
  border-radius: 4px;
  font-size: 14px;
  color: #595959;
  transition: all 0.2s;
  font-weight: 400;

  &:hover {
    color: #1890ff;
    background: #f5f5f5;
  }

  &.active {
    background: #1890ff;
    color: #fff;
    font-weight: 500;

    &:hover {
      background: #40a9ff;
      color: #fff;
    }
  }
}
</style>
