<template>
  <div class="host-detail-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <a-button type="text" @click="handleBack" style="padding: 0; margin-right: 8px">
        <ArrowLeftOutlined />
      </a-button>
      <div class="header-content">
        <h2 style="margin: 0">{{ host?.hostname || '主机详情' }}</h2>
      </div>
    </div>

    <!-- 标签页 -->
    <a-tabs v-model:activeKey="activeTab" @change="handleTabChange">
      <a-tab-pane key="overview" tab="主机概览">
        <HostOverview :host="host" :loading="loading" @update:host="host = $event" @view-detail="handleViewDetail" />
      </a-tab-pane>
      <a-tab-pane key="alerts" :tab="`安全告警(${alertCount})`">
        <SecurityAlerts :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="vulnerabilities" :tab="`漏洞风险(${vulnerabilityCount})`">
        <VulnerabilityRisk :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="baseline" :tab="`基线风险(${baselineCount})`">
        <BaselineRisk :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="runtime" :tab="`运行时安全告警(0)`">
        <RuntimeAlerts :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="antivirus" :tab="`病毒查杀(0)`">
        <AntivirusScan :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="performance" tab="性能监控">
        <PerformanceMonitor :host-id="hostId" />
      </a-tab-pane>
      <a-tab-pane key="fingerprint" tab="资产指纹">
        <AssetFingerprint :host-id="hostId" />
      </a-tab-pane>
    </a-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ArrowLeftOutlined } from '@ant-design/icons-vue'
import { hostsApi } from '@/api/hosts'
import type { HostDetail } from '@/api/types'
import HostOverview from './components/HostOverview.vue'
import SecurityAlerts from './components/SecurityAlerts.vue'
import VulnerabilityRisk from './components/VulnerabilityRisk.vue'
import BaselineRisk from './components/BaselineRisk.vue'
import RuntimeAlerts from './components/RuntimeAlerts.vue'
import AntivirusScan from './components/AntivirusScan.vue'
import PerformanceMonitor from './components/PerformanceMonitor.vue'
import AssetFingerprint from './components/AssetFingerprint.vue'

const router = useRouter()
const route = useRoute()

const loading = ref(false)
const host = ref<HostDetail | null>(null)
const activeTab = ref('overview')
const hostId = ref('')

const alertCount = ref(0)
const vulnerabilityCount = ref(0)
const baselineCount = ref(0)

const loadHostDetail = async () => {
  const id = route.params.hostId as string
  if (!id) return

  hostId.value = id
  loading.value = true
  try {
    const [hostData, scoreData] = await Promise.all([
      hostsApi.get(id),
      hostsApi.getScore(id).catch(() => null),
    ])
    host.value = hostData

    // 计算基线风险数量
    if (scoreData) {
      baselineCount.value = scoreData.fail_count
    }

    // TODO: 加载告警和漏洞数量
  } catch (error) {
    console.error('加载主机详情失败:', error)
  } finally {
    loading.value = false
  }
}

const handleBack = () => {
  router.push('/hosts')
}

const handleTabChange = (_key: string) => {
  // 可以在这里加载对应标签页的数据
}

const handleViewDetail = (tab: string) => {
  activeTab.value = tab
}


onMounted(() => {
  loadHostDetail()
})
</script>

<style scoped>
.host-detail-page {
  width: 100%;
}

.page-header {
  display: flex;
  align-items: center;
  margin-bottom: 16px;
}

.header-content {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 16px;
}

.page-header h2 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
}
</style>
