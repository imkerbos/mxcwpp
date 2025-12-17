<template>
  <a-card title="性能监控" :bordered="false" :loading="loading">
    <div v-if="metrics">
      <!-- 最新监控数据 -->
      <a-row :gutter="24" v-if="metrics.latest" style="margin-bottom: 24px">
        <a-col :span="8">
          <a-statistic
            title="CPU 使用率"
            :value="metrics.latest.cpu_usage"
            suffix="%"
            :precision="2"
            :value-style="{ color: getUsageColor(metrics.latest.cpu_usage) }"
          />
        </a-col>
        <a-col :span="8">
          <a-statistic
            title="内存使用率"
            :value="metrics.latest.mem_usage"
            suffix="%"
            :precision="2"
            :value-style="{ color: getUsageColor(metrics.latest.mem_usage) }"
          />
        </a-col>
        <a-col :span="8">
          <a-statistic
            title="磁盘使用率"
            :value="metrics.latest.disk_usage"
            suffix="%"
            :precision="2"
            :value-style="{ color: getUsageColor(metrics.latest.disk_usage) }"
          />
        </a-col>
      </a-row>

      <!-- 网络流量 -->
      <a-row :gutter="24" v-if="metrics.latest && (metrics.latest.net_bytes_sent || metrics.latest.net_bytes_recv)" style="margin-bottom: 24px">
        <a-col :span="12">
          <a-statistic
            title="网络发送"
            :value="formatBytes(metrics.latest.net_bytes_sent || 0)"
          />
        </a-col>
        <a-col :span="12">
          <a-statistic
            title="网络接收"
            :value="formatBytes(metrics.latest.net_bytes_recv || 0)"
          />
        </a-col>
      </a-row>

      <!-- 数据源信息 -->
      <a-alert
        :message="`数据源: ${metrics.source === 'mysql' ? 'MySQL' : 'Prometheus'}`"
        :description="metrics.latest?.collected_at ? `最后更新时间: ${formatTime(metrics.latest.collected_at)}` : ''"
        type="info"
        show-icon
        style="margin-bottom: 24px"
      />

      <!-- 时间序列图表（如果有数据） -->
      <div v-if="metrics.time_series && (metrics.time_series.cpu_usage?.length || metrics.time_series.mem_usage?.length)">
        <a-typography-title :level="5">趋势图</a-typography-title>
        <a-empty description="图表功能待实现，当前仅显示最新数据" />
      </div>

      <!-- 无数据提示 -->
      <a-empty v-else description="暂无监控数据" />
    </div>
    <a-empty v-else description="暂无监控数据" />
  </a-card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { hostsApi } from '@/api/hosts'
import type { HostMetrics } from '@/api/types'

const props = defineProps<{
  hostId: string
}>()

const loading = ref(false)
const metrics = ref<HostMetrics | null>(null)

const loadMetrics = async () => {
  if (!props.hostId) return

  loading.value = true
  try {
    // 查询最近1小时的监控数据
    const endTime = new Date()
    const startTime = new Date(endTime.getTime() - 60 * 60 * 1000) // 1小时前

    metrics.value = await hostsApi.getMetrics(props.hostId, {
      start_time: startTime.toISOString(),
      end_time: endTime.toISOString(),
    })
  } catch (error) {
    console.error('加载监控数据失败:', error)
  } finally {
    loading.value = false
  }
}

const getUsageColor = (usage?: number): string => {
  if (!usage) return '#1890ff'
  if (usage >= 90) return '#ff4d4f'
  if (usage >= 70) return '#faad14'
  return '#52c41a'
}

const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

const formatTime = (timeStr: string): string => {
  if (!timeStr) return '-'
  // 如果是 YYYY-MM-DD HH:mm:ss 格式，先转换为 ISO 格式
  let date = new Date(timeStr)
  if (isNaN(date.getTime()) && timeStr.includes(' ')) {
    date = new Date(timeStr.replace(' ', 'T'))
  }
  if (isNaN(date.getTime())) return timeStr
  return date.toLocaleString('zh-CN')
}

onMounted(() => {
  loadMetrics()
})
</script>
