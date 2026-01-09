<template>
  <div class="dashboard-page">
    <div class="page-header">
      <h2>安全概览</h2>
    </div>

    <!-- 第一行：资产概览 -->
    <a-row :gutter="[16, 16]" class="dashboard-row">
      <a-col :xs="24" :sm="24" :md="24" :lg="24" :xl="24">
        <a-card title="资产概览" :bordered="false" class="dashboard-card asset-overview-card">
          <a-row :gutter="[24, 16]">
            <a-col :xs="12" :sm="8" :md="6" :lg="6" :xl="6">
              <a-statistic title="主机" :value="stats.hosts" :value-style="{ fontSize: '24px' }" />
            </a-col>
            <a-col :xs="12" :sm="8" :md="6" :lg="6" :xl="6">
              <a-statistic title="集群" :value="stats.clusters" :value-style="{ fontSize: '24px' }" />
            </a-col>
            <a-col :xs="12" :sm="8" :md="6" :lg="6" :xl="6">
              <a-statistic title="容器" :value="stats.containers" :value-style="{ fontSize: '24px' }" />
            </a-col>
            <a-col :xs="12" :sm="8" :md="6" :lg="6" :xl="6">
              <a-statistic title="在线Agent" :value="stats.onlineAgents" :value-style="{ fontSize: '24px' }" />
            </a-col>
          </a-row>
        </a-card>
      </a-col>
    </a-row>

    <!-- 第二行：基线风险、基线统计 -->
    <a-row :gutter="[16, 16]" class="dashboard-row">
      <a-col :xs="24" :sm="24" :md="12" :lg="12" :xl="12">
        <a-card title="基线风险 Top 3" :bordered="false" class="dashboard-card">
          <div class="baseline-risk-content">
            <div v-if="baselineRisks.length === 0" class="empty-state">
              <a-empty description="暂无基线风险" :image="false" />
            </div>
            <div v-else>
              <div v-for="(risk, index) in baselineRisks" :key="index" class="baseline-risk-item">
                <div class="risk-rank">{{ index + 1 }}.</div>
                <div class="risk-info">
                  <div class="risk-name">{{ risk.name }}</div>
                  <div class="risk-stats">
                    <a-tag color="red">高危 {{ risk.critical }}</a-tag>
                    <a-tag color="orange">中危 {{ risk.medium }}</a-tag>
                    <a-tag color="blue">低危 {{ risk.low }}</a-tag>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="24" :md="12" :lg="12" :xl="12">
        <a-card title="基线安全统计" :bordered="false" class="dashboard-card">
          <div class="baseline-stats-content">
            <div class="baseline-main-stat">
              <div class="baseline-number" :style="{ color: getPassRateColor(stats.baselineHardeningPercent) }">
                {{ stats.baselineHardeningPercent || 0 }}%
              </div>
              <div class="baseline-label">整体基线合规率</div>
            </div>
            <a-divider style="margin: 16px 0" />
            <a-space direction="vertical" style="width: 100%" size="middle">
              <div class="stat-row">
                <span>检查主机数</span>
                <span>{{ stats.hosts || 0 }}</span>
              </div>
              <div class="stat-row">
                <span>存在高危基线主机</span>
                <span class="danger-text">{{ stats.baselineHostPercent || 0 }}%</span>
              </div>
              <div class="stat-row">
                <span>待处理基线风险</span>
                <span class="danger-text">{{ stats.baselineFailCount || 0 }}</span>
              </div>
            </a-space>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <!-- 第四行：Agent 概述、后端服务状态 -->
    <a-row :gutter="[16, 16]" class="dashboard-row">
      <a-col :xs="24" :sm="24" :md="12" :lg="12" :xl="12">
        <a-card title="Agent 概览" :bordered="false" class="dashboard-card">
          <a-space direction="vertical" style="width: 100%" size="middle">
            <div class="agent-stat-item">
              <a-statistic
                title="在线 Agent"
                :value="stats.onlineAgents"
                :value-style="{ color: '#52c41a', fontSize: '20px' }"
              >
                <template #suffix>
                  <span class="stat-suffix">较昨日 {{ stats.onlineAgentsChange || 0 }}</span>
                </template>
              </a-statistic>
            </div>
            <div class="agent-stat-item">
              <a-statistic
                title="离线 Agent"
                :value="stats.offlineAgents"
                :value-style="{ color: '#ff4d4f', fontSize: '20px' }"
              >
                <template #suffix>
                  <span class="stat-suffix">较昨日 {{ stats.offlineAgentsChange || 0 }}</span>
                </template>
              </a-statistic>
            </div>
            <a-divider style="margin: 8px 0" />
            <div class="agent-stat-item">
              <div class="stat-label">CPU 平均使用率</div>
              <div class="stat-value">
                <span class="stat-number">{{ stats.avgCpuUsage || 0 }}%</span>
                <span class="stat-suffix">较昨日 {{ stats.avgCpuUsageChange || 0 }}%</span>
              </div>
            </div>
            <div class="agent-stat-item">
              <div class="stat-label">内存平均使用量</div>
              <div class="stat-value">
                <span class="stat-number">{{ formatMemory(stats.avgMemoryUsage ?? 0) }}</span>
                <span class="stat-suffix">较昨日 {{ formatMemoryChange(stats.avgMemoryUsageChange ?? 0) }}</span>
              </div>
            </div>
          </a-space>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="24" :md="12" :lg="12" :xl="12">
        <a-card title="后端服务状态" :bordered="false" class="dashboard-card">
          <a-space direction="vertical" style="width: 100%" size="middle">
            <div class="service-status-item">
              <div class="service-name">
                <span class="status-dot" :class="getServiceStatusClass('database')"></span>
                <span>数据库连接</span>
              </div>
              <a-tag :color="getServiceStatusColor(serviceStatus.database)">
                {{ getServiceStatusText(serviceStatus.database) }}
              </a-tag>
            </div>
            <div class="service-status-item">
              <div class="service-name">
                <span class="status-dot" :class="getServiceStatusClass('agentcenter')"></span>
                <span>AgentCenter 服务</span>
              </div>
              <a-tag :color="getServiceStatusColor(serviceStatus.agentcenter)">
                {{ getServiceStatusText(serviceStatus.agentcenter) }}
              </a-tag>
            </div>
            <div class="service-status-item">
              <div class="service-name">
                <span class="status-dot" :class="getServiceStatusClass('manager')"></span>
                <span>Manager 服务</span>
              </div>
              <a-tag :color="getServiceStatusColor(serviceStatus.manager)">
                {{ getServiceStatusText(serviceStatus.manager) }}
              </a-tag>
            </div>
            <!-- 基线检查插件在 Agent 端运行，Server 端无法直接检查其状态 -->
            <!-- 如需了解基线检查活动情况，可通过"在线 Agent 数量"或"最近基线检查结果"来判断 -->
          </a-space>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { dashboardApi } from '@/api/dashboard'
import type { DashboardStats } from '@/api/dashboard'

interface BaselineRisk {
  name: string
  critical: number
  medium: number
  low: number
}

const stats = ref<DashboardStats>({
  hosts: 0,
  clusters: 0,
  containers: 0,
  onlineAgents: 0,
  offlineAgents: 0,
  onlineAgentsChange: 0,
  offlineAgentsChange: 0,
  pendingAlerts: 0,
  pendingVulnerabilities: 0,
  vulnDbUpdateTime: '',
  hotPatchCount: 0,
  baselineFailCount: 0,
  baselineHardeningPercent: 0,
  avgCpuUsage: 0,
  avgCpuUsageChange: 0,
  avgMemoryUsage: 0,
  avgMemoryUsageChange: 0,
  hostAlertPercent: 0,
  vulnHostPercent: 0,
  baselineHostPercent: 0,
  runtimeAlertPercent: 0,
  virusHostPercent: 0,
})

const baselineRisks = ref<BaselineRisk[]>([])

const serviceStatus = ref({
  database: 'healthy',
  agentcenter: 'healthy',
  manager: 'healthy',
})

const getPassRateColor = (rate: number): string => {
  if (rate >= 90) return '#52c41a'
  if (rate >= 70) return '#faad14'
  if (rate >= 50) return '#fa8c16'
  return '#ff4d4f'
}

const formatMemory = (bytes: number): string => {
  if (!bytes || bytes === 0) return '0B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + sizes[i]
}

const formatMemoryChange = (bytes: number): string => {
  if (!bytes || bytes === 0) return '0B'
  const sign = bytes > 0 ? '+' : ''
  return sign + formatMemory(bytes)
}

const getServiceStatusClass = (service: string) => {
  const status = serviceStatus.value[service as keyof typeof serviceStatus.value]
  return {
    'status-dot-healthy': status === 'healthy',
    'status-dot-warning': status === 'warning',
    'status-dot-error': status === 'error',
  }
}

const getServiceStatusColor = (status: string) => {
  const colorMap: Record<string, string> = {
    healthy: 'green',
    warning: 'orange',
    error: 'red',
  }
  return colorMap[status] || 'default'
}

const getServiceStatusText = (status: string) => {
  const textMap: Record<string, string> = {
    healthy: '正常',
    warning: '警告',
    error: '异常',
  }
  return textMap[status] || '未知'
}

const loadDashboardData = async () => {
  try {
    // 加载 Dashboard 统计数据
    const dashboardStats = await dashboardApi.getStats()

    stats.value = {
      ...dashboardStats,
      baselineHardeningPercent: Math.round(dashboardStats.baselineHardeningPercent || 0),
      baselineHostPercent: Math.round(dashboardStats.baselineHostPercent || 0),
      onlineAgentsChange: dashboardStats.onlineAgentsChange || 0,
      offlineAgentsChange: dashboardStats.offlineAgentsChange || 0,
      hotPatchCount: dashboardStats.hotPatchCount || 0,
      avgCpuUsage: dashboardStats.avgCpuUsage || 0,
      avgCpuUsageChange: dashboardStats.avgCpuUsageChange || 0,
      avgMemoryUsage: dashboardStats.avgMemoryUsage || 0,
      avgMemoryUsageChange: dashboardStats.avgMemoryUsageChange || 0,
      hostAlertPercent: dashboardStats.hostAlertPercent || 0,
      vulnHostPercent: dashboardStats.vulnHostPercent || 0,
      runtimeAlertPercent: dashboardStats.runtimeAlertPercent || 0,
      virusHostPercent: dashboardStats.virusHostPercent || 0,
    }

    // 加载基线风险 Top 3
    if (dashboardStats.baselineRisks) {
      baselineRisks.value = dashboardStats.baselineRisks.slice(0, 3)
    }

    // 加载服务状态
    if (dashboardStats.serviceStatus) {
      serviceStatus.value = {
        database: dashboardStats.serviceStatus.database || 'healthy',
        agentcenter: dashboardStats.serviceStatus.agentcenter || 'healthy',
        manager: dashboardStats.serviceStatus.manager || 'healthy',
      }
    }
  } catch (error) {
    console.error('加载Dashboard数据失败:', error)
  }
}

let refreshInterval: number | null = null

onMounted(() => {
  loadDashboardData()
  // 每30秒刷新一次数据
  refreshInterval = window.setInterval(() => {
    loadDashboardData()
  }, 30000)
})

onUnmounted(() => {
  if (refreshInterval !== null) {
    clearInterval(refreshInterval)
  }
})
</script>

<style scoped>
.dashboard-page {
  width: 100%;
  padding: 0;
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.dashboard-row {
  margin-bottom: 16px;
}

.dashboard-card {
  height: 100%;
  min-height: 280px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  border-radius: 4px;
}

/* 资产概览卡片 - 降低高度 */
.asset-overview-card {
  min-height: auto;
  height: auto;
}

/* 资产概览 */
.dashboard-card :deep(.ant-card-body) {
  padding: 20px;
}

.asset-overview-card :deep(.ant-card-body) {
  padding: 16px 20px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

.stat-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 0;
  font-size: 14px;
}

/* 基线风险 */
.baseline-risk-content {
  min-height: 200px;
}

.baseline-risk-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid #f0f0f0;
}

.baseline-risk-item:last-child {
  border-bottom: none;
}

.risk-rank {
  font-size: 16px;
  font-weight: bold;
  color: #1890ff;
  min-width: 24px;
}

.risk-info {
  flex: 1;
}

.risk-name {
  font-size: 14px;
  font-weight: 500;
  color: #333;
  margin-bottom: 8px;
}

.risk-stats {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* 基线安全统计 */
.baseline-stats-content {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.baseline-main-stat {
  text-align: center;
  padding: 16px 0;
}

.baseline-number {
  font-size: 36px;
  font-weight: bold;
  line-height: 1;
}

.baseline-label {
  color: #8c8c8c;
  margin-top: 8px;
  font-size: 14px;
}

.danger-text {
  color: #ff4d4f;
  font-weight: 500;
}

/* Agent 概述 */
.agent-stat-item {
  padding: 8px 0;
}

.stat-label {
  font-size: 14px;
  color: #8c8c8c;
  margin-bottom: 4px;
}

.stat-value {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.stat-number {
  font-size: 20px;
  font-weight: 500;
  color: #333;
}

.stat-suffix {
  font-size: 12px;
  color: #8c8c8c;
}

/* 后端服务状态 */
.service-status-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
}

.service-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: #333;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
}

.status-dot-healthy {
  background-color: #52c41a;
}

.status-dot-warning {
  background-color: #ff9800;
}

.status-dot-error {
  background-color: #ff4d4f;
}

/* 响应式调整 */
@media (max-width: 768px) {
  .dashboard-card {
    min-height: auto;
  }

  .baseline-number {
    font-size: 28px;
  }
}
</style>
