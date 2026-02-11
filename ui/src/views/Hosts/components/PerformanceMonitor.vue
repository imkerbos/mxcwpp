<template>
  <div class="performance-monitor">
    <a-spin :spinning="loading">
      <div v-if="metrics">
        <!-- 核心指标卡片 -->
        <div class="metrics-row" v-if="metrics.latest">
          <div class="metric-card">
            <div class="metric-icon-bg cpu-bg">
              <DashboardOutlined />
            </div>
            <div class="metric-info">
              <div class="metric-label">CPU 使用率</div>
              <div class="metric-value" :style="{ color: getUsageColor(metrics.latest.cpu_usage) }">
                {{ metrics.latest.cpu_usage?.toFixed(2) ?? '-' }}<span class="metric-unit">%</span>
              </div>
              <a-progress
                :percent="metrics.latest.cpu_usage || 0"
                :show-info="false"
                :stroke-color="getUsageColor(metrics.latest.cpu_usage)"
                :stroke-width="4"
                size="small"
              />
            </div>
          </div>
          <div class="metric-card">
            <div class="metric-icon-bg mem-bg">
              <DatabaseOutlined />
            </div>
            <div class="metric-info">
              <div class="metric-label">内存使用率</div>
              <div class="metric-value" :style="{ color: getUsageColor(metrics.latest.mem_usage) }">
                {{ metrics.latest.mem_usage?.toFixed(2) ?? '-' }}<span class="metric-unit">%</span>
              </div>
              <a-progress
                :percent="metrics.latest.mem_usage || 0"
                :show-info="false"
                :stroke-color="getUsageColor(metrics.latest.mem_usage)"
                :stroke-width="4"
                size="small"
              />
            </div>
          </div>
          <div class="metric-card">
            <div class="metric-icon-bg disk-bg">
              <HddOutlined />
            </div>
            <div class="metric-info">
              <div class="metric-label">磁盘使用率</div>
              <div class="metric-value" :style="{ color: getUsageColor(metrics.latest.disk_usage) }">
                {{ metrics.latest.disk_usage?.toFixed(2) ?? '-' }}<span class="metric-unit">%</span>
              </div>
              <a-progress
                :percent="metrics.latest.disk_usage || 0"
                :show-info="false"
                :stroke-color="getUsageColor(metrics.latest.disk_usage)"
                :stroke-width="4"
                size="small"
              />
            </div>
          </div>
        </div>

        <!-- 网络流量 -->
        <div class="metrics-row" v-if="metrics.latest && (metrics.latest.net_bytes_sent || metrics.latest.net_bytes_recv)">
          <div class="metric-card">
            <div class="metric-icon-bg net-send-bg">
              <ArrowUpOutlined />
            </div>
            <div class="metric-info">
              <div class="metric-label">网络发送</div>
              <div class="metric-value net">{{ formatBytes(metrics.latest.net_bytes_sent || 0) }}</div>
            </div>
          </div>
          <div class="metric-card">
            <div class="metric-icon-bg net-recv-bg">
              <ArrowDownOutlined />
            </div>
            <div class="metric-info">
              <div class="metric-label">网络接收</div>
              <div class="metric-value net">{{ formatBytes(metrics.latest.net_bytes_recv || 0) }}</div>
            </div>
          </div>
        </div>

        <!-- 数据源信息 -->
        <div class="source-bar">
          <div class="source-left">
            <CloudServerOutlined style="margin-right: 6px;" />
            数据源: {{ metrics.source === 'mysql' ? 'MySQL' : 'Prometheus' }}
          </div>
          <div class="source-right" v-if="metrics.latest?.collected_at">
            <ClockCircleOutlined style="margin-right: 4px;" />
            最后更新: {{ formatTime(metrics.latest.collected_at) }}
          </div>
        </div>

        <!-- 时间序列图表 -->
        <div v-if="metrics.time_series && (metrics.time_series.cpu_usage?.length || metrics.time_series.mem_usage?.length)" class="chart-section">
          <div class="chart-header">趋势图</div>
          <a-empty description="图表功能待实现，当前仅显示最新数据" />
        </div>

        <!-- 无趋势数据 -->
        <div v-else class="chart-section">
          <a-empty description="暂无监控数据" />
        </div>
      </div>
      <a-empty v-else description="暂无监控数据" />
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  DashboardOutlined,
  DatabaseOutlined,
  HddOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  CloudServerOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons-vue'
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

<style scoped lang="less">
.performance-monitor {
  width: 100%;
}

.metrics-row {
  display: flex;
  gap: 16px;
  margin-bottom: 16px;
}

.metric-card {
  flex: 1;
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 20px;
  background: #fff;
  border-radius: 8px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03),
    0 2px 4px rgba(0, 0, 0, 0.04),
    0 4px 8px rgba(0, 0, 0, 0.04);
  transition: all 0.3s ease;

  &:hover {
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08),
      0 8px 24px rgba(0, 0, 0, 0.06);
  }
}

.metric-icon-bg {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  flex-shrink: 0;
  color: #fff;
}

.cpu-bg {
  background: linear-gradient(135deg, #1890ff, #096dd9);
}

.mem-bg {
  background: linear-gradient(135deg, #722ed1, #531dab);
}

.disk-bg {
  background: linear-gradient(135deg, #fa8c16, #d46b08);
}

.net-send-bg {
  background: linear-gradient(135deg, #52c41a, #389e0d);
}

.net-recv-bg {
  background: linear-gradient(135deg, #13c2c2, #08979c);
}

.metric-info {
  flex: 1;
  min-width: 0;
}

.metric-label {
  font-size: 13px;
  color: #8c8c8c;
  margin-bottom: 6px;
}

.metric-value {
  font-size: 28px;
  font-weight: 700;
  line-height: 1;
  margin-bottom: 10px;

  &.net {
    font-size: 22px;
    color: #262626;
    margin-bottom: 0;
  }
}

.metric-unit {
  font-size: 14px;
  font-weight: 400;
  margin-left: 2px;
}

/* 数据源信息 */
.source-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: #fafbfc;
  border-radius: 8px;
  border: 1px solid #f0f0f0;
  margin-bottom: 16px;
  font-size: 13px;
  color: #595959;
}

.source-left {
  display: flex;
  align-items: center;
}

.source-right {
  display: flex;
  align-items: center;
  color: #8c8c8c;
}

/* 图表区域 */
.chart-section {
  background: #fff;
  border-radius: 8px;
  padding: 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03),
    0 2px 4px rgba(0, 0, 0, 0.04),
    0 4px 8px rgba(0, 0, 0, 0.04);
}

.chart-header {
  font-size: 16px;
  font-weight: 600;
  color: #262626;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid #f0f0f0;
}
</style>
