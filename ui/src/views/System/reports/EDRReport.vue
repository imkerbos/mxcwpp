<template>
  <div class="category-report">
    <a-spin :spinning="loading">
      <!-- 元数据 -->
      <a-row :gutter="[16, 16]" class="stats-overview">
        <a-col :xs="12" :sm="8" :lg="{ span: 4, offset: 2 }">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="在线主机" :value="report.meta.onlineHosts" :value-style="{ color: '#22C55E' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="启用规则" :value="report.meta.enabledRules" :value-style="{ color: '#3B82F6' }">
              <template #suffix>/ {{ report.meta.totalRules }}</template>
            </a-statistic>
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="告警总数" :value="report.summary.totalAlerts" :value-style="{ color: '#F59E0B' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="活跃告警" :value="report.summary.activeAlerts" :value-style="{ color: '#EF4444' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="已忽略" :value="report.summary.ignoredAlerts" :value-style="{ color: '#86909C' }" />
          </a-card>
        </a-col>
      </a-row>

      <a-row :gutter="[16, 16]" class="stats-overview">
        <a-col :xs="12" :sm="8" :lg="{ span: 4, offset: 2 }">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="受影响主机" :value="report.summary.affectedHosts" :value-style="{ color: '#722ed1' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="攻击故事线" :value="report.summary.totalStories" :value-style="{ color: '#3B82F6' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic title="高危故事线" :value="report.summary.highRiskStories" :value-style="{ color: '#EF4444' }" />
          </a-card>
        </a-col>
        <a-col :xs="12" :sm="8" :lg="4">
          <a-card :bordered="false" class="stat-card">
            <a-statistic
              title="环比"
              :value="Math.abs(report.trend.growthPercent)"
              :precision="1"
              suffix="%"
              :value-style="{ color: trendColor }"
            >
              <template #prefix>{{ trendArrow }}</template>
            </a-statistic>
          </a-card>
        </a-col>
      </a-row>

      <!-- 严重程度 + Category 分布 -->
      <a-row :gutter="[16, 16]" style="margin-top: 16px">
        <a-col :xs="24" :lg="12">
          <a-card title="严重程度分布" :bordered="false">
            <VChart :option="severityOption" style="height: 320px" autoresize />
          </a-card>
        </a-col>
        <a-col :xs="24" :lg="12">
          <a-card title="MITRE ATT&CK 战术分布" :bordered="false">
            <VChart :option="tacticOption" style="height: 320px" autoresize />
          </a-card>
        </a-col>
      </a-row>

      <!-- Top 触发规则 + Top 主机 -->
      <a-row :gutter="[16, 16]" style="margin-top: 16px">
        <a-col :xs="24" :lg="12">
          <a-card title="Top 10 触发规则" :bordered="false">
            <a-table
              :columns="ruleColumns"
              :data-source="report.topRules"
              :pagination="false"
              size="small"
              row-key="title"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.key === 'severity'">
                  <a-tag :color="severityColors[record.severity] || 'default'">
                    {{ severityLabelMap[record.severity] || record.severity }}
                  </a-tag>
                </template>
              </template>
            </a-table>
          </a-card>
        </a-col>
        <a-col :xs="24" :lg="12">
          <a-card title="Top 10 受影响主机" :bordered="false">
            <a-table
              :columns="hostColumns"
              :data-source="report.topHosts"
              :pagination="false"
              size="small"
              row-key="host_id"
            />
          </a-card>
        </a-col>
      </a-row>

      <!-- Top 高危故事线 + 误报抑制 -->
      <a-row :gutter="[16, 16]" style="margin-top: 16px">
        <a-col :xs="24" :lg="14">
          <a-card title="Top 5 高风险攻击故事线" :bordered="false">
            <a-table
              :columns="storyColumns"
              :data-source="report.topStories"
              :pagination="false"
              size="small"
              row-key="story_id"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.key === 'severity'">
                  <a-tag :color="severityColors[record.severity] || 'default'">
                    {{ severityLabelMap[record.severity] || record.severity }}
                  </a-tag>
                </template>
                <template v-else-if="column.key === 'risk_score'">
                  <a-progress
                    :percent="record.risk_score"
                    :stroke-color="record.risk_score >= 70 ? '#EF4444' : record.risk_score >= 40 ? '#F59E0B' : '#3B82F6'"
                    size="small"
                  />
                </template>
              </template>
            </a-table>
          </a-card>
        </a-col>
        <a-col :xs="24" :lg="10">
          <a-card title="误报抑制统计" :bordered="false">
            <a-table
              :columns="suppressColumns"
              :data-source="report.suppressionStats"
              :pagination="false"
              size="small"
              row-key="reason"
            />
            <a-empty v-if="!report.suppressionStats.length" description="无抑制记录" />
          </a-card>
        </a-col>
      </a-row>
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { PieChart, BarChart } from 'echarts/charts'
import {
  TitleComponent, TooltipComponent, LegendComponent, GridComponent,
} from 'echarts/components'
import type { EChartsOption } from 'echarts'
import type { Dayjs } from 'dayjs'
import { reportsApi, type EDRReport } from '@/api/reports'

use([CanvasRenderer, PieChart, BarChart, TitleComponent, TooltipComponent, LegendComponent, GridComponent])

const props = defineProps<{ dateRange: [Dayjs, Dayjs] }>()

const loading = ref(false)
const report = ref<EDRReport>({
  meta: { reportID: '', period: '', generatedAt: '', onlineHosts: 0, totalRules: 0, enabledRules: 0 },
  summary: { totalAlerts: 0, activeAlerts: 0, resolvedAlerts: 0, ignoredAlerts: 0, affectedHosts: 0, totalStories: 0, highRiskStories: 0 },
  severityDistribution: {},
  categoryDistribution: [],
  tacticDistribution: {},
  topRules: [],
  topHosts: [],
  topStories: [],
  suppressionStats: [],
  trend: { prevPeriodAlerts: 0, growthPercent: 0, direction: 'stable' },
})

const severityColors: Record<string, string> = {
  critical: '#dc2626', high: '#ea580c', medium: '#ca8a04', low: '#0891b2',
}
const severityLabelMap: Record<string, string> = {
  critical: '严重', high: '高危', medium: '中危', low: '低危',
}

const tacticLabelMap: Record<string, string> = {
  initial_access: '初始访问', execution: '执行', persistence: '持久化',
  privilege_escalation: '权限提升', defense_evasion: '防御规避',
  credential_access: '凭据访问', discovery: '发现', lateral_movement: '横向移动',
  collection: '收集', exfiltration: '数据渗出', command_and_control: 'C2 通信',
  impact: '影响', other: '其他',
}

const trendColor = computed(() =>
  report.value.trend.direction === 'up' ? '#EF4444'
    : report.value.trend.direction === 'down' ? '#22C55E' : '#86909C'
)
const trendArrow = computed(() =>
  report.value.trend.direction === 'up' ? '↑'
    : report.value.trend.direction === 'down' ? '↓' : '→'
)

const severityOption = computed<EChartsOption>(() => ({
  tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
  legend: { orient: 'vertical', left: 'left' },
  series: [{
    name: '严重级别', type: 'pie', radius: ['40%', '70%'],
    itemStyle: { borderRadius: 8, borderColor: '#fff', borderWidth: 2 },
    data: (['critical', 'high', 'medium', 'low'] as const)
      .map(sev => ({
        value: report.value.severityDistribution[sev] || 0,
        name: severityLabelMap[sev],
        itemStyle: { color: severityColors[sev] },
      }))
      .filter(item => item.value > 0),
  }],
}))

const tacticOption = computed<EChartsOption>(() => {
  const entries = Object.entries(report.value.tacticDistribution)
    .filter(([, v]) => v > 0)
    .sort((a, b) => b[1] - a[1])
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: {
      type: 'category',
      data: entries.map(([k]) => tacticLabelMap[k] || k),
      axisLabel: { rotate: 30, interval: 0 },
    },
    yAxis: { type: 'value' },
    series: [{
      name: '告警数', type: 'bar',
      data: entries.map(([, v]) => v),
      itemStyle: { color: '#722ed1' },
    }],
  }
})

const ruleColumns = [
  { title: '规则', key: 'title', dataIndex: 'title', ellipsis: true },
  { title: '级别', key: 'severity', width: 80 },
  { title: '命中', key: 'count', dataIndex: 'count', width: 80 },
]

const hostColumns = [
  { title: '主机', key: 'hostname', dataIndex: 'hostname', ellipsis: true },
  { title: '告警数', key: 'count', dataIndex: 'count', width: 90 },
]

const storyColumns = [
  { title: '主机', key: 'hostname', dataIndex: 'hostname', ellipsis: true, width: 200 },
  { title: '阶段', key: 'phase', dataIndex: 'phase', width: 130 },
  { title: '级别', key: 'severity', width: 70 },
  { title: '事件', key: 'event_count', dataIndex: 'event_count', width: 70 },
  { title: '告警', key: 'alert_count', dataIndex: 'alert_count', width: 70 },
  { title: '风险', key: 'risk_score', width: 130 },
]

const suppressColumns = [
  { title: '抑制原因', key: 'reason', dataIndex: 'reason', ellipsis: true },
  { title: '数量', key: 'count', dataIndex: 'count', width: 80 },
]

const loadData = async () => {
  loading.value = true
  try {
    const data = await reportsApi.getEDRReport({
      start_time: props.dateRange[0].format('YYYY-MM-DD'),
      end_time: props.dateRange[1].format('YYYY-MM-DD'),
    })
    report.value = data
  } catch (e) {
    console.error('加载 EDR 报告失败:', e)
    message.error('加载 EDR 报告失败')
  } finally {
    loading.value = false
  }
}

defineExpose({ loadData })

watch(() => props.dateRange, loadData, { deep: true })
onMounted(loadData)
</script>

<style scoped>
.category-report {
  padding: 0;
}
.stats-overview {
  margin-bottom: 0;
}
.stat-card {
  text-align: center;
}
</style>
