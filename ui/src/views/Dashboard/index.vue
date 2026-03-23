<template>
  <div class="dashboard-page">
    <!-- 页面标题 -->
    <div class="page-header">
      <h2>安全概览</h2>
      <span class="page-header-hint">实时监控平台安全态势</span>
    </div>

    <!-- 第一行: 资产统计卡片 (参考 Elkeid DescribeAgent / DescribeAsset) -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="4" v-for="item in assetStats" :key="item.key">
        <div class="stat-card-item" @click="item.route && $router.push(item.route)">
          <div class="stat-card-icon" :style="{ background: item.gradient }">
            <component :is="item.icon" />
          </div>
          <div class="stat-card-value">{{ item.value }}</div>
          <div class="stat-card-label">{{ item.label }}</div>
        </div>
      </a-col>
    </a-row>

    <!-- 第二行: 告警趋势 (左) + 基线风险 Top5 (右) -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="14">
        <div class="dashboard-card">
          <div class="card-header">
            <span class="card-title">入侵告警趋势</span>
            <a-radio-group v-model:value="alertTrendRange" size="small" @change="loadAlertTrend">
              <a-radio-button value="7d">近 7 天</a-radio-button>
              <a-radio-button value="30d">近 30 天</a-radio-button>
            </a-radio-group>
          </div>
          <div class="card-body chart-container">
            <v-chart v-if="alertTrendData.length > 0" :option="alertTrendOption" autoresize style="height: 280px" />
            <a-empty v-else description="暂无告警数据" />
          </div>
        </div>
      </a-col>
      <a-col :span="10">
        <div class="dashboard-card">
          <div class="card-header">
            <span class="card-title">基线风险 Top 5</span>
          </div>
          <div class="card-body chart-container">
            <v-chart v-if="baselineRisks.length > 0" :option="baselineRiskOption" autoresize style="height: 280px" />
            <a-empty v-else description="暂无基线风险" />
          </div>
        </div>
      </a-col>
    </a-row>

    <!-- 第三行: Agent 状态饼图 (左) + 基线安全统计 (中) + 服务健康 (右) -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="8">
        <div class="dashboard-card">
          <div class="card-header">
            <span class="card-title">Agent 状态分布</span>
          </div>
          <div class="card-body chart-container">
            <v-chart :option="agentPieOption" autoresize style="height: 240px" />
          </div>
        </div>
      </a-col>
      <a-col :span="8">
        <div class="dashboard-card">
          <div class="card-header">
            <span class="card-title">基线安全统计</span>
          </div>
          <div class="card-body">
            <div class="compliance-ring">
              <a-progress
                type="circle"
                :percent="stats.baselineHardeningPercent || 0"
                :size="120"
                :stroke-color="getPassRateColor(stats.baselineHardeningPercent)"
                :format="(p: number) => `${p}%`"
              />
              <div class="compliance-label">整体合规率</div>
            </div>
            <div class="compliance-detail">
              <div class="detail-row">
                <span class="detail-label">检查主机数</span>
                <span class="detail-value">{{ stats.hosts || 0 }}</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">高危主机占比</span>
                <span class="detail-value text-danger">{{ stats.baselineHostPercent || 0 }}%</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">待修复风险项</span>
                <span class="detail-value text-danger">{{ stats.baselineFailCount || 0 }}</span>
              </div>
            </div>
          </div>
        </div>
      </a-col>
      <a-col :span="8">
        <div class="dashboard-card">
          <div class="card-header">
            <span class="card-title">后端服务状态</span>
          </div>
          <div class="card-body">
            <div class="service-list">
              <div v-for="svc in serviceList" :key="svc.key" class="service-item">
                <div class="service-left">
                  <span class="status-dot" :class="`dot-${svc.status}`"></span>
                  <span class="service-name">{{ svc.name }}</span>
                </div>
                <a-tag :color="statusColorMap[svc.status]" :bordered="false">
                  {{ statusTextMap[svc.status] }}
                </a-tag>
              </div>
            </div>
            <a-divider style="margin: 16px 0 12px" />
            <div class="agent-summary">
              <div class="agent-summary-item">
                <span class="agent-summary-label">在线 Agent</span>
                <span class="agent-summary-value" style="color: #00B42A">{{ stats.onlineAgents || 0 }}</span>
              </div>
              <div class="agent-summary-item">
                <span class="agent-summary-label">离线 Agent</span>
                <span class="agent-summary-value" style="color: #F53F3F">{{ stats.offlineAgents || 0 }}</span>
              </div>
              <div class="agent-summary-item">
                <span class="agent-summary-label">平均 CPU</span>
                <span class="agent-summary-value">{{ stats.avgCpuUsage || 0 }}%</span>
              </div>
              <div class="agent-summary-item">
                <span class="agent-summary-label">平均内存</span>
                <span class="agent-summary-value">{{ formatMemory(stats.avgMemoryUsage ?? 0) }}</span>
              </div>
            </div>
          </div>
        </div>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import {
  DesktopOutlined,
  ClusterOutlined,
  CheckCircleOutlined,
  WarningOutlined,
} from '@ant-design/icons-vue'
import { dashboardApi } from '@/api/dashboard'
import type { DashboardStats, BaselineRisk } from '@/api/dashboard'

// ========== 数据 ==========
const stats = ref<DashboardStats>({
  hosts: 0, clusters: 0, containers: 0,
  onlineAgents: 0, offlineAgents: 0,
  pendingAlerts: 0, pendingVulnerabilities: 0,
  vulnDbUpdateTime: '', baselineFailCount: 0,
  baselineHardeningPercent: 0,
})

const baselineRisks = ref<BaselineRisk[]>([])
const alertTrendRange = ref('7d')
const alertTrendData = ref<any[]>([])

const serviceStatus = ref({
  database: 'healthy' as string,
  agentcenter: 'healthy' as string,
  manager: 'healthy' as string,
})

const statusColorMap: Record<string, string> = {
  healthy: 'green', warning: 'orange', error: 'red',
}
const statusTextMap: Record<string, string> = {
  healthy: '正常', warning: '警告', error: '异常',
}

// ========== 资产统计卡片 ==========
const assetStats = computed(() => [
  {
    key: 'hosts', label: '主机总数', value: stats.value.hosts,
    icon: DesktopOutlined, gradient: 'linear-gradient(135deg, #165DFF, #0E42D2)',
    route: '/hosts',
  },
  {
    key: 'online', label: '在线 Agent', value: stats.value.onlineAgents,
    icon: CheckCircleOutlined, gradient: 'linear-gradient(135deg, #00B42A, #009A29)',
    route: '/hosts',
  },
  {
    key: 'offline', label: '离线 Agent', value: stats.value.offlineAgents,
    icon: WarningOutlined, gradient: 'linear-gradient(135deg, #F53F3F, #CB2634)',
    route: '/hosts',
  },
  {
    key: 'alerts', label: '待处理告警', value: stats.value.pendingAlerts,
    icon: WarningOutlined, gradient: 'linear-gradient(135deg, #FF7D00, #D25F00)',
    route: '/alerts',
  },
  {
    key: 'baseline', label: '待修复基线', value: stats.value.baselineFailCount,
    icon: DesktopOutlined, gradient: 'linear-gradient(135deg, #FADC19, #F7BA1E)',
    route: '/policies',
  },
  {
    key: 'clusters', label: '容器集群', value: stats.value.clusters,
    icon: ClusterOutlined, gradient: 'linear-gradient(135deg, #722ED1, #531DAB)',
  },
])

// ========== 告警趋势折线图 ==========
const alertTrendOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    backgroundColor: '#fff',
    borderColor: '#E5E8EF',
    textStyle: { color: '#1D2129', fontSize: 12 },
  },
  legend: {
    bottom: 0,
    itemWidth: 12, itemHeight: 3,
    textStyle: { color: '#86909C', fontSize: 12 },
  },
  grid: { top: 16, right: 16, bottom: 36, left: 48 },
  xAxis: {
    type: 'category',
    data: alertTrendData.value.map((d: any) => d.date),
    axisLine: { lineStyle: { color: '#E5E6EB' } },
    axisLabel: { color: '#86909C', fontSize: 11 },
    axisTick: { show: false },
  },
  yAxis: {
    type: 'value', minInterval: 1,
    axisLine: { show: false },
    axisLabel: { color: '#86909C', fontSize: 11 },
    splitLine: { lineStyle: { color: '#F2F3F5' } },
  },
  series: [
    { name: '紧急', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2 }, itemStyle: { color: '#F53F3F' }, data: alertTrendData.value.map((d: any) => d.critical ?? 0) },
    { name: '高危', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2 }, itemStyle: { color: '#FF7D00' }, data: alertTrendData.value.map((d: any) => d.high ?? 0) },
    { name: '中危', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2 }, itemStyle: { color: '#FADC19' }, data: alertTrendData.value.map((d: any) => d.medium ?? 0) },
    { name: '低危', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2 }, itemStyle: { color: '#165DFF' }, data: alertTrendData.value.map((d: any) => d.low ?? 0) },
  ],
}))

// ========== 基线风险 Top5 横向柱状图 ==========
const baselineRiskOption = computed(() => {
  const names = baselineRisks.value.map(r => r.name)
  const criticalData = baselineRisks.value.map(r => r.critical)
  const mediumData = baselineRisks.value.map(r => r.medium)
  const lowData = baselineRisks.value.map(r => r.low)

  return {
    tooltip: {
      trigger: 'axis', axisPointer: { type: 'shadow' },
      backgroundColor: '#fff', borderColor: '#E5E8EF',
      textStyle: { color: '#1D2129', fontSize: 12 },
    },
    legend: {
      bottom: 0, itemWidth: 12, itemHeight: 8,
      textStyle: { color: '#86909C', fontSize: 12 },
    },
    grid: { top: 8, right: 16, bottom: 36, left: 120 },
    xAxis: {
      type: 'value', minInterval: 1,
      axisLine: { show: false },
      axisLabel: { color: '#86909C', fontSize: 11 },
      splitLine: { lineStyle: { color: '#F2F3F5' } },
    },
    yAxis: {
      type: 'category', data: names.reverse(),
      axisLine: { show: false }, axisTick: { show: false },
      axisLabel: { color: '#4E5969', fontSize: 12, width: 110, overflow: 'truncate' },
    },
    series: [
      { name: '高危', type: 'bar', stack: 'total', barWidth: 16, itemStyle: { color: '#F53F3F', borderRadius: [0, 0, 0, 0] }, data: criticalData.reverse() },
      { name: '中危', type: 'bar', stack: 'total', barWidth: 16, itemStyle: { color: '#FF7D00' }, data: mediumData.reverse() },
      { name: '低危', type: 'bar', stack: 'total', barWidth: 16, itemStyle: { color: '#165DFF', borderRadius: [0, 2, 2, 0] }, data: lowData.reverse() },
    ],
  }
})

// ========== Agent 状态饼图 ==========
const agentPieOption = computed(() => ({
  tooltip: {
    trigger: 'item',
    backgroundColor: '#fff', borderColor: '#E5E8EF',
    textStyle: { color: '#1D2129', fontSize: 12 },
  },
  legend: {
    orient: 'vertical', right: 24, top: 'center',
    itemWidth: 10, itemHeight: 10, itemGap: 16,
    textStyle: { color: '#4E5969', fontSize: 13 },
  },
  series: [{
    type: 'pie', radius: ['55%', '80%'], center: ['35%', '50%'],
    avoidLabelOverlap: false,
    label: {
      show: true, position: 'center',
      formatter: () => `{total|${(stats.value.onlineAgents || 0) + (stats.value.offlineAgents || 0)}}\n{label|总计}`,
      rich: {
        total: { fontSize: 24, fontWeight: 700, color: '#1D2129', lineHeight: 32 },
        label: { fontSize: 12, color: '#86909C', lineHeight: 20 },
      },
    },
    itemStyle: { borderColor: '#fff', borderWidth: 2 },
    data: [
      { value: stats.value.onlineAgents || 0, name: '在线', itemStyle: { color: '#00B42A' } },
      { value: stats.value.offlineAgents || 0, name: '离线', itemStyle: { color: '#F53F3F' } },
    ],
  }],
}))

// ========== 服务列表 ==========
const serviceList = computed(() => [
  { key: 'database', name: '数据库', status: serviceStatus.value.database },
  { key: 'agentcenter', name: 'AgentCenter', status: serviceStatus.value.agentcenter },
  { key: 'manager', name: 'Manager', status: serviceStatus.value.manager },
])

// ========== 工具函数 ==========
const getPassRateColor = (rate: number): string => {
  if (rate >= 90) return '#00B42A'
  if (rate >= 70) return '#FF7D00'
  return '#F53F3F'
}

const formatMemory = (bytes: number): string => {
  if (!bytes || bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
}

// ========== 数据加载 ==========
const loadAlertTrend = async () => {
  try {
    // TODO: 接入真实 API, 当前使用空数据
    alertTrendData.value = []
  } catch {
    alertTrendData.value = []
  }
}

const loadDashboardData = async () => {
  try {
    const data = await dashboardApi.getStats()
    stats.value = {
      ...data,
      baselineHardeningPercent: Math.round(data.baselineHardeningPercent || 0),
      baselineHostPercent: Math.round(data.baselineHostPercent ?? 0),
    }
    if (data.baselineRisks) {
      baselineRisks.value = data.baselineRisks.slice(0, 5)
    }
    if (data.serviceStatus) {
      serviceStatus.value = {
        database: data.serviceStatus.database || 'healthy',
        agentcenter: data.serviceStatus.agentcenter || 'healthy',
        manager: data.serviceStatus.manager || 'healthy',
      }
    }
  } catch (error) {
    console.error('加载 Dashboard 数据失败:', error)
  }
}

let refreshTimer: number | null = null

onMounted(() => {
  loadDashboardData()
  loadAlertTrend()
  refreshTimer = window.setInterval(loadDashboardData, 30000)
})

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
.dashboard-page {
  width: 100%;
}

.section-row {
  margin-bottom: 16px;
}

/* ========== 资产统计卡片 ========== */
.stat-card-item {
  background: #FFFFFF;
  border: 1px solid #E5E8EF;
  border-radius: 8px;
  padding: 20px;
  text-align: center;
  cursor: pointer;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.stat-card-item:hover {
  border-color: #165DFF;
  box-shadow: 0 2px 8px rgba(22, 93, 255, 0.1);
}

.stat-card-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #FFFFFF;
  font-size: 18px;
  margin: 0 auto 12px;
}

.stat-card-value {
  font-size: 28px;
  font-weight: 700;
  color: #1D2129;
  line-height: 1.2;
}

.stat-card-label {
  font-size: 13px;
  color: #86909C;
  margin-top: 4px;
}

/* ========== Dashboard Card (Elkeid 风格) ========== */
.dashboard-card {
  background: #FFFFFF;
  border: 1px solid #E5E8EF;
  border-radius: 8px;
  height: 100%;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 20px;
  border-bottom: 1px solid #F2F3F5;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1D2129;
}

.card-body {
  padding: 16px 20px;
}

.chart-container {
  display: flex;
  align-items: center;
  justify-content: center;
}

/* ========== 基线安全统计 ========== */
.compliance-ring {
  text-align: center;
  padding: 12px 0;
}

.compliance-label {
  font-size: 13px;
  color: #86909C;
  margin-top: 8px;
}

.compliance-detail {
  margin-top: 12px;
}

.detail-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid #F7F8FA;
}

.detail-row:last-child {
  border-bottom: none;
}

.detail-label {
  font-size: 13px;
  color: #4E5969;
}

.detail-value {
  font-size: 14px;
  font-weight: 500;
  color: #1D2129;
}

/* ========== 后端服务状态 ========== */
.service-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.service-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  border-radius: 4px;
  transition: background 0.2s;
}

.service-item:hover {
  background: #F7F8FA;
}

.service-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.service-name {
  font-size: 14px;
  color: #1D2129;
}

/* 状态指示点 */
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
}

.dot-healthy {
  background: #00B42A;
  box-shadow: 0 0 0 3px rgba(0, 180, 42, 0.15);
}

.dot-warning {
  background: #FF7D00;
  box-shadow: 0 0 0 3px rgba(255, 125, 0, 0.15);
}

.dot-error {
  background: #F53F3F;
  box-shadow: 0 0 0 3px rgba(245, 63, 63, 0.15);
}

/* Agent 概要指标 */
.agent-summary {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.agent-summary-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.agent-summary-label {
  font-size: 12px;
  color: #86909C;
}

.agent-summary-value {
  font-size: 18px;
  font-weight: 600;
  color: #1D2129;
}
</style>
