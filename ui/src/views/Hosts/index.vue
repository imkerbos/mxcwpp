<template>
  <div class="hosts-page">
    <!-- 主机状态分布和风险分布 -->
    <a-row :gutter="16" style="margin-bottom: 16px" class="distribution-row">
      <!-- 主机状态分布 -->
      <a-col :span="12" class="distribution-col">
        <a-card title="主机状态分布" :bordered="false" class="distribution-card">
          <div class="status-distribution-container">
            <div class="chart-container" @click="handleStatusChartClick">
              <v-chart
                class="status-chart"
                :option="statusChartOption"
                autoresize
              />
              <div class="chart-hint">点击图表查看详情</div>
            </div>
            <div class="legend-container">
              <div class="status-legend">
                <div class="legend-item">
                  <span class="legend-color" style="background: #52c41a"></span>
                  <span>运行中</span>
                  <span class="legend-value">{{ statusDistribution.running }}</span>
                </div>
                <div class="legend-item">
                  <span class="legend-color" style="background: #faad14"></span>
                  <span>运行异常</span>
                  <span class="legend-value">{{ statusDistribution.abnormal }}</span>
                </div>
                <div class="legend-item">
                  <span class="legend-color" style="background: #ff4d4f"></span>
                  <span>离线</span>
                  <span class="legend-value">{{ statusDistribution.offline }}</span>
                </div>
                <div class="legend-item">
                  <span class="legend-color" style="background: #1890ff"></span>
                  <span>未安装</span>
                  <span class="legend-value">{{ statusDistribution.not_installed }}</span>
                </div>
                <div class="legend-item">
                  <span class="legend-color" style="background: #8c8c8c"></span>
                  <span>已卸载</span>
                  <span class="legend-value">{{ statusDistribution.uninstalled }}</span>
                </div>
              </div>
            </div>
          </div>
        </a-card>
      </a-col>

      <!-- 主机基线风险分布 -->
      <a-col :span="12" class="distribution-col">
        <a-card title="主机基线风险分布" :bordered="false" class="distribution-card">
          <div class="risk-distribution-container">
            <div class="risk-card">
              <div class="risk-icon" style="border-color: #cf1322">
                <ExclamationCircleOutlined style="color: #cf1322" />
              </div>
              <div class="risk-content">
                <div class="risk-label">严重</div>
                <div class="risk-value" style="color: #cf1322">{{ riskDistribution.critical }}</div>
              </div>
            </div>
            <div class="risk-card">
              <div class="risk-icon" style="border-color: #ff4d4f">
                <ExclamationCircleOutlined style="color: #ff4d4f" />
              </div>
              <div class="risk-content">
                <div class="risk-label">高危</div>
                <div class="risk-value" style="color: #ff4d4f">{{ riskDistribution.high }}</div>
              </div>
            </div>
            <div class="risk-card">
              <div class="risk-icon" style="border-color: #faad14">
                <ExclamationCircleOutlined style="color: #faad14" />
              </div>
              <div class="risk-content">
                <div class="risk-label">中危</div>
                <div class="risk-value" style="color: #faad14">{{ riskDistribution.medium }}</div>
              </div>
            </div>
            <div class="risk-card">
              <div class="risk-icon" style="border-color: #1890ff">
                <ExclamationCircleOutlined style="color: #1890ff" />
              </div>
              <div class="risk-content">
                <div class="risk-label">低危</div>
                <div class="risk-value" style="color: #1890ff">{{ riskDistribution.low }}</div>
              </div>
            </div>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <!-- 主机内容 -->
    <a-card title="主机内容" :bordered="false">
      <!-- 操作按钮和筛选 -->
      <div class="action-bar">
        <div class="action-left">
          <a-space>
            <a-button>Agent离线通知</a-button>
            <a-button>批量导出主机</a-button>
            <a-button>批量添加标签</a-button>
            <a-button>批量下发任务</a-button>
            <a-dropdown>
              <a-button>
                更多
                <DownOutlined />
              </a-button>
              <template #overlay>
                <a-menu>
                  <a-menu-item @click="handleBatchBindBusinessLine">批量绑定业务线</a-menu-item>
                  <a-menu-item>批量导入标签</a-menu-item>
                  <a-menu-item>清理离线数据</a-menu-item>
                  <a-menu-item>删除未安装记录</a-menu-item>
                </a-menu>
              </template>
            </a-dropdown>
          </a-space>
        </div>
        <div class="action-right">
          <a-space>
            <span style="color: rgba(0, 0, 0, 0.65)">主机范围:</span>
            <a-select
              v-model:value="filters.hostRange"
              placeholder="全部"
              style="width: 120px"
            >
              <a-select-option value="all">全部</a-select-option>
              <a-select-option value="online">在线</a-select-option>
              <a-select-option value="offline">离线</a-select-option>
            </a-select>
            <a-button @click="loadHosts">
              <template #icon>
                <ReloadOutlined />
              </template>
            </a-button>
          </a-space>
        </div>
      </div>

      <!-- 搜索区域 -->
      <div class="filter-bar">
        <a-select
          v-model:value="filters.business_line"
          placeholder="业务线"
          style="width: 150px"
          allow-clear
          show-search
          :filter-option="filterBusinessLineOption"
        >
          <a-select-option value="__unbound__">
            <span style="color: #8c8c8c;">无业务线</span>
          </a-select-option>
          <a-select-option v-for="bl in businessLines" :key="bl.name" :value="bl.name">
            {{ bl.name }}
          </a-select-option>
        </a-select>
        <a-select
          v-model:value="filters.os_family"
          placeholder="操作系统"
          style="width: 150px"
          allow-clear
        >
          <a-select-option
            v-for="os in osOptions"
            :key="os.value"
            :value="os.value"
          >
            {{ os.label }}
          </a-select-option>
        </a-select>
        <a-select
          v-model:value="filters.status"
          placeholder="状态"
          style="width: 120px"
          allow-clear
        >
          <a-select-option value="online">在线</a-select-option>
          <a-select-option value="offline">离线</a-select-option>
        </a-select>
        <a-select
          v-model:value="filters.runtime_type"
          placeholder="运行环境"
          style="width: 120px"
          allow-clear
        >
          <a-select-option value="vm">虚拟机/物理机</a-select-option>
          <a-select-option value="docker">Docker 容器</a-select-option>
          <a-select-option value="k8s">K8s Pod</a-select-option>
        </a-select>
        <a-input
          v-model:value="filters.search"
          placeholder="请输入主机名或ID搜索"
          style="width: 300px"
          allow-clear
          @press-enter="handleSearch"
        >
          <template #prefix>
            <SearchOutlined />
          </template>
        </a-input>
        <a-button type="primary" @click="handleSearch">
          <template #icon>
            <SearchOutlined />
          </template>
          搜索
        </a-button>
      </div>

    <!-- 主机列表表格 -->
    <a-table
      :columns="columns"
      :data-source="hosts"
      :loading="loading"
      :pagination="pagination"
      :row-selection="rowSelection"
      @change="handleTableChange"
      row-key="host_id"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'hostname'">
          <div style="display: flex; align-items: center; gap: 8px;">
            <a-button type="link" @click="(e: MouseEvent) => handleViewDetail(record, e)" @mousedown="(e: MouseEvent) => handleLinkMouseDown(record.host_id, e)">
              {{ record.hostname }}
            </a-button>
            <a-tag v-if="record.runtime_type === 'docker'" color="blue" style="margin: 0;">
              Docker
            </a-tag>
            <a-tag v-else-if="record.runtime_type === 'k8s'" color="purple" style="margin: 0;">
              K8s
            </a-tag>
          </div>
        </template>
        <template v-else-if="column.key === 'tags'">
          <a-space>
            <a-tag v-for="tag in record.tags" :key="tag">{{ tag }}</a-tag>
            <span v-if="!record.tags || record.tags.length === 0" style="color: #8c8c8c">-</span>
          </a-space>
        </template>
        <template v-else-if="column.key === 'risk'">
          <ScoreDisplay :host-id="record.host_id" />
        </template>
        <template v-else-if="column.key === 'status'">
          <a-tag :color="record.status === 'online' ? 'green' : 'red'">
            {{ record.status === 'online' ? '在线' : '离线' }}
          </a-tag>
        </template>
        <template v-else-if="column.key === 'resource_usage'">
          <div>
            <div>CPU: {{ record.cpu_usage || 0 }}%</div>
            <div>内存: {{ record.memory_usage || 0 }}%</div>
          </div>
        </template>
        <template v-else-if="column.key === 'action'">
          <a-space>
            <a-button type="link" @click="(e: MouseEvent) => handleViewDetail(record, e)" @mousedown="(e: MouseEvent) => handleLinkMouseDown(record.host_id, e)">查看详情</a-button>
            <a-popconfirm
              title="确定要删除这台主机吗？"
              description="删除后将同时删除该主机的所有扫描结果、告警和相关数据，此操作不可恢复。"
              ok-text="确定"
              cancel-text="取消"
              @confirm="handleDeleteHost(record)"
            >
              <a-button type="link" danger>删除</a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </template>
      <template #emptyText>
        <a-empty description="暂无数据" />
      </template>
    </a-table>
    </a-card>

    <!-- 批量绑定业务线对话框 -->
    <a-modal
      v-model:open="batchBindBusinessLineModalVisible"
      title="批量绑定业务线"
      :width="500"
      @ok="handleConfirmBatchBindBusinessLine"
      @cancel="handleCancelBatchBindBusinessLine"
    >
      <div style="margin-bottom: 16px;">
        <div style="margin-bottom: 8px; color: rgba(0, 0, 0, 0.65);">
          已选择 <strong>{{ selectedRowKeys.length }}</strong> 台主机
        </div>
      </div>
      <div style="margin-bottom: 16px;">
        <div style="margin-bottom: 8px; font-weight: 500;">选择业务线</div>
        <a-select
          v-model:value="batchBindBusinessLine"
          placeholder="请选择业务线"
          style="width: 100%"
          show-search
          allow-clear
          :filter-option="filterBusinessLineOption"
        >
          <a-select-option v-for="bl in businessLines" :key="bl.name" :value="bl.name">
            {{ bl.name }}
          </a-select-option>
        </a-select>
        <div style="margin-top: 8px; color: rgba(0, 0, 0, 0.45); font-size: 12px;">
          提示：选择业务线后，所选主机将绑定到该业务线。留空表示取消业务线绑定。
        </div>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  ReloadOutlined,
  SearchOutlined,
  DownOutlined,
  ExclamationCircleOutlined,
} from '@ant-design/icons-vue'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { PieChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  LegendComponent,
} from 'echarts/components'
import VChart from 'vue-echarts'
import { hostsApi, type HostStatusDistribution, type HostRiskDistribution } from '@/api/hosts'
import { businessLinesApi, type BusinessLine } from '@/api/business-lines'
import type { Host } from '@/api/types'
import ScoreDisplay from './components/ScoreDisplay.vue'
import { message } from 'ant-design-vue'
import { formatDateTime } from '@/utils/date'
import { OS_OPTIONS } from '@/constants/os'

// 注册 ECharts 组件
use([CanvasRenderer, PieChart, TitleComponent, TooltipComponent, LegendComponent])

const router = useRouter()
const osOptions = OS_OPTIONS

const loading = ref(false)
const hosts = ref<Host[]>([])
const selectedRowKeys = ref<string[]>([])
const businessLines = ref<BusinessLine[]>([])

// 批量绑定业务线
const batchBindBusinessLineModalVisible = ref(false)
const batchBindBusinessLine = ref<string>('')
const statusDistribution = ref<HostStatusDistribution>({
  running: 0,
  abnormal: 0,
  offline: 0,
  not_installed: 0,
  uninstalled: 0,
})
const riskDistribution = ref<HostRiskDistribution>({
  critical: 0,
  high: 0,
  medium: 0,
  low: 0,
})

const filters = reactive({
  hostRange: 'all' as string,
  search: '' as string,
  business_line: undefined as string | undefined,
  os_family: undefined as string | undefined,
  status: undefined as string | undefined,
  runtime_type: undefined as string | undefined, // 运行环境类型筛选：vm/docker/k8s
})


// 主机状态分布饼图配置
const statusChartOption = computed(() => {
  const data = [
    {
      value: statusDistribution.value.running,
      name: '运行中',
      itemStyle: { color: '#52c41a' },
    },
    {
      value: statusDistribution.value.abnormal,
      name: '运行异常',
      itemStyle: { color: '#faad14' },
    },
    {
      value: statusDistribution.value.offline,
      name: '离线',
      itemStyle: { color: '#ff4d4f' },
    },
    {
      value: statusDistribution.value.not_installed,
      name: '未安装',
      itemStyle: { color: '#1890ff' },
    },
    {
      value: statusDistribution.value.uninstalled,
      name: '已卸载',
      itemStyle: { color: '#8c8c8c' },
    },
  ]

  // 如果所有值都是0，显示一个占位饼图（显示所有分类，但值为0）
  const hasData = data.some((item) => item.value > 0)

  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: any) => {
        if (!hasData) {
          return `${params.name}: 0`
        }
        return `${params.name}: ${params.value} (${params.percent}%)`
      },
    },
    series: [
      {
        name: '主机状态',
        type: 'pie',
        radius: ['40%', '70%'],
        center: ['50%', '50%'],
        avoidLabelOverlap: false,
        itemStyle: {
          borderRadius: 4,
          borderColor: '#fff',
          borderWidth: 2,
        },
        label: {
          show: hasData,
          formatter: '{b}: {c}',
          fontSize: 12,
        },
        emphasis: {
          label: {
            show: true,
            fontSize: 14,
            fontWeight: 'bold',
          },
        },
        labelLine: {
          show: hasData,
        },
        // 即使没有数据也显示所有分类的饼图
        data: hasData
          ? data.filter((item) => item.value > 0)
          : data.map((item) => ({
              ...item,
              value: 1, // 每个分类占相等比例（20%）
              itemStyle: { ...item.itemStyle, opacity: 0.5 }, // 降低透明度表示无数据
            })),
        animation: true,
        animationType: 'scale',
        animationEasing: 'elasticOut',
      },
    ],
  }
})

const rowSelection = computed(() => ({
  selectedRowKeys: selectedRowKeys.value,
  onChange: (keys: string[]) => {
    selectedRowKeys.value = keys
  },
}))

const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  {
    title: '主机名称',
    key: 'hostname',
    width: 200,
  },
  {
    title: '标签',
    key: 'tags',
    width: 150,
  },
  {
    title: '业务线',
    dataIndex: 'business_line',
    key: 'business_line',
    width: 120,
    customRender: ({ record }: { record: Host }) => {
      return record.business_line || '-'
    },
  },
  {
    title: '操作系统',
    key: 'os',
    width: 180,
    customRender: ({ record }: { record: Host }) => {
      return `${record.os_family} ${record.os_version}`
    },
  },
  {
    title: '风险',
    key: 'risk',
    width: 150,
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
  },
  {
    title: '客户端资源使用',
    key: 'resource_usage',
    width: 150,
  },
  {
    title: '更新时间',
    dataIndex: 'last_heartbeat',
    key: 'last_heartbeat',
    width: 180,
    customRender: ({ record }: { record: Host }) => {
      return formatDateTime(record.last_heartbeat)
    },
  },
  {
    title: '操作',
    key: 'action',
    width: 100,
    fixed: 'right' as const,
  },
]

const loadHosts = async () => {
  loading.value = true
  try {
    const params: any = {
      page: pagination.current,
      page_size: pagination.pageSize,
    }
    if (filters.os_family) {
      params.os_family = filters.os_family
    }
    if (filters.status) {
      params.status = filters.status
    }
    if (filters.business_line) {
      params.business_line = filters.business_line
    }
    if (filters.search && filters.search.trim()) {
      params.search = filters.search.trim()
    }
    if (filters.runtime_type) {
      params.runtime_type = filters.runtime_type
    }
    const response = await hostsApi.list(params)
    hosts.value = response.items
    pagination.total = response.total
  } catch (error) {
    console.error('加载主机列表失败:', error)
    message.error('加载主机列表失败')
  } finally {
    loading.value = false
  }
}

// 加载业务线列表
const loadBusinessLines = async () => {
  try {
    const response = await businessLinesApi.list({ enabled: 'true', page_size: 1000 })
    businessLines.value = response.items
  } catch (error) {
    console.error('加载业务线列表失败:', error)
  }
}

// 业务线筛选选项过滤
const filterBusinessLineOption = (input: string, option: any) => {
  return option.children[0].children.toLowerCase().indexOf(input.toLowerCase()) >= 0
}

const loadStatusDistribution = async () => {
  try {
    const data = await hostsApi.getStatusDistribution()
    statusDistribution.value = data
  } catch (error) {
    console.error('加载主机状态分布失败:', error)
  }
}

const loadRiskDistribution = async () => {
  try {
    const data = await hostsApi.getRiskDistribution()
    riskDistribution.value = data
  } catch (error) {
    console.error('加载主机风险分布失败:', error)
  }
}

const handleStatusChartClick = () => {
  // TODO: 实现点击图表查看详情的功能
  message.info('点击图表查看详情功能开发中')
}

const handleSearch = () => {
  pagination.current = 1
  loadHosts()
}


const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  loadHosts()
}

const handleViewDetail = (record: Host, e?: MouseEvent) => {
  // 如果按下 Ctrl/Cmd 键，不执行默认导航（由 mousedown 处理）
  if (e && (e.ctrlKey || e.metaKey)) {
    return
  }
  router.push(`/hosts/${record.host_id}`)
}

// 处理链接鼠标按下事件（支持 Ctrl/Cmd+Click 新标签打开）
const handleLinkMouseDown = (hostId: string, e: MouseEvent) => {
  if (e.ctrlKey || e.metaKey) {
    e.preventDefault()
    const url = `${window.location.origin}/hosts/${hostId}`
    window.open(url, '_blank')
  }
}

// 删除主机
const handleDeleteHost = async (record: Host) => {
  try {
    await hostsApi.delete(record.host_id)
    message.success(`主机 ${record.hostname} 删除成功`)
    
    // 刷新主机列表和统计
    loadHosts()
    loadStatusDistribution()
    loadRiskDistribution()
  } catch (error: any) {
    console.error('删除主机失败:', error)
    message.error(error?.message || '删除主机失败，请重试')
  }
}

// 批量绑定业务线
const handleBatchBindBusinessLine = () => {
  if (selectedRowKeys.value.length === 0) {
    message.warning('请先选择要绑定的主机')
    return
  }
  batchBindBusinessLine.value = ''
  batchBindBusinessLineModalVisible.value = true
}

// 确认批量绑定业务线
const handleConfirmBatchBindBusinessLine = async () => {
  if (selectedRowKeys.value.length === 0) {
    message.warning('请先选择要绑定的主机')
    return
  }

  try {
    const businessLine = batchBindBusinessLine.value || ''
    
    // 批量更新业务线
    const promises = selectedRowKeys.value.map((hostId) =>
      hostsApi.updateBusinessLine(hostId, businessLine)
    )
    
    await Promise.all(promises)
    
    message.success(`成功绑定 ${selectedRowKeys.value.length} 台主机到业务线`)
    
    // 清空选择并关闭对话框
    selectedRowKeys.value = []
    batchBindBusinessLineModalVisible.value = false
    batchBindBusinessLine.value = ''
    
    // 刷新主机列表
    loadHosts()
  } catch (error: any) {
    console.error('批量绑定业务线失败:', error)
    message.error(error?.message || '批量绑定业务线失败，请重试')
  }
}

// 取消批量绑定业务线
const handleCancelBatchBindBusinessLine = () => {
  batchBindBusinessLineModalVisible.value = false
  batchBindBusinessLine.value = ''
}

onMounted(() => {
  // 从 URL 查询参数读取业务线筛选
  const route = router.currentRoute.value
  if (route.query.business_line) {
    filters.business_line = route.query.business_line as string
  }
  
  loadBusinessLines()
  loadHosts()
  loadStatusDistribution()
  loadRiskDistribution()
})
</script>

<style scoped>
.hosts-page {
  width: 100%;
}

.distribution-row {
  display: flex;
  align-items: stretch;
}

.distribution-col {
  display: flex;
}

.distribution-card {
  width: 100%;
  display: flex;
  flex-direction: column;
}

.distribution-card :deep(.ant-card-body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.status-distribution-container {
  display: flex;
  align-items: flex-start;
  gap: 24px;
  flex: 1;
  min-height: 280px;
}

.chart-container {
  flex: 1;
  position: relative;
  cursor: pointer;
}

.status-chart {
  width: 100%;
  height: 200px;
}

.chart-hint {
  text-align: center;
  margin-top: 8px;
  color: #8c8c8c;
  font-size: 12px;
}

.legend-container {
  flex: 1;
  min-width: 200px;
}

.status-legend {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 6px;
  transition: background 0.2s;
}

.legend-item:hover {
  background: #f5f7fa;
}

.legend-color {
  width: 12px;
  height: 12px;
  border-radius: 3px;
  display: inline-block;
}

.legend-value {
  margin-left: auto;
  font-weight: 600;
  color: rgba(0, 0, 0, 0.85);
}

.risk-distribution-container {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
  flex: 1;
  align-content: start;
}

.risk-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  border: none;
  border-radius: 8px;
  background: #fafbfc;
  min-height: 70px;
  transition: all 0.3s ease;
}

.risk-card:hover {
  background: #f0f5ff;
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06);
}

.risk-icon {
  width: 44px;
  height: 44px;
  border: 2px solid;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  flex-shrink: 0;
}

.risk-content {
  flex: 1;
  min-width: 0;
}

.risk-label {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.65);
  margin-bottom: 4px;
  line-height: 1.4;
  word-break: break-word;
}

.risk-value {
  font-size: 22px;
  font-weight: 700;
  color: rgba(0, 0, 0, 0.85);
}

.action-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #f0f0f0;
}

.action-left {
  flex: 1;
}

.action-right {
  display: flex;
  align-items: center;
}

.filter-bar {
  margin-bottom: 16px;
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 12px 16px;
  background: #fafbfc;
  border-radius: 6px;
  border: 1px solid #f0f0f0;
}
</style>
