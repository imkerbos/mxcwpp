<template>
  <div class="asset-fingerprint-page">
    <div class="page-header">
      <h2>资产指纹</h2>
      <span class="page-header-hint">全局资产指纹采集汇总, 跨主机维度查看</span>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="4" v-for="item in tabStats" :key="item.key">
        <div
          class="fp-stat-card"
          :class="{ active: activeTab === item.key }"
          @click="activeTab = item.key"
        >
          <div class="fp-stat-icon">
            <component :is="item.icon" />
          </div>
          <div class="fp-stat-value">{{ item.count }}</div>
          <div class="fp-stat-label">{{ item.label }}</div>
        </div>
      </a-col>
    </a-row>

    <!-- Tab 内容 -->
    <div class="dashboard-card">
      <div class="card-body">
        <!-- 筛选栏 -->
        <div class="filter-bar">
          <a-input-search
            v-model:value="searchText"
            :placeholder="searchPlaceholder"
            style="width: 280px"
            allow-clear
            @search="loadData"
          />
          <a-select v-model:value="filterHost" style="width: 200px" placeholder="按主机筛选" allow-clear show-search @change="loadData">
            <a-select-option v-for="h in hostOptions" :key="h.value" :value="h.value">{{ h.label }}</a-select-option>
          </a-select>
          <a-button @click="handleExport">导出</a-button>
        </div>

        <!-- 端口列表 -->
        <a-table
          v-if="activeTab === 'ports'"
          :columns="portColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />

        <!-- 进程列表 -->
        <a-table
          v-if="activeTab === 'processes'"
          :columns="processColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />

        <!-- 用户列表 -->
        <a-table
          v-if="activeTab === 'users'"
          :columns="userColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />

        <!-- 软件包 -->
        <a-table
          v-if="activeTab === 'packages'"
          :columns="packageColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />

        <!-- 定时任务 -->
        <a-table
          v-if="activeTab === 'crontabs'"
          :columns="crontabColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />

        <!-- 服务列表 -->
        <a-table
          v-if="activeTab === 'services'"
          :columns="serviceColumns"
          :data-source="tableData"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import {
  ApiOutlined,
  CodeOutlined,
  UserOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
} from '@ant-design/icons-vue'
import apiClient from '@/api/client'

const activeTab = ref('ports')
const searchText = ref('')
const filterHost = ref<string>()
const loading = ref(false)
const tableData = ref<any[]>([])
const hostOptions = ref<any[]>([])

const pagination = ref({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const tabStats = ref([
  { key: 'ports', label: '开放端口', count: 0, icon: ApiOutlined },
  { key: 'processes', label: '运行进程', count: 0, icon: CodeOutlined },
  { key: 'users', label: '系统用户', count: 0, icon: UserOutlined },
  { key: 'packages', label: '软件包', count: 0, icon: AppstoreOutlined },
  { key: 'crontabs', label: '定时任务', count: 0, icon: ClockCircleOutlined },
  { key: 'services', label: '系统服务', count: 0, icon: CloudServerOutlined },
])

const searchPlaceholder = computed(() => {
  const map: Record<string, string> = {
    ports: '搜索端口号或服务名',
    processes: '搜索进程名或PID',
    users: '搜索用户名',
    packages: '搜索软件包名',
    crontabs: '搜索任务命令',
    services: '搜索服务名',
  }
  return map[activeTab.value] || '搜索'
})

const portColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '端口', dataIndex: 'port', key: 'port', width: 100 },
  { title: '协议', dataIndex: 'protocol', key: 'protocol', width: 80 },
  { title: '进程名', dataIndex: 'processName', key: 'processName', width: 140 },
  { title: 'PID', dataIndex: 'pid', key: 'pid', width: 80 },
  { title: '监听地址', dataIndex: 'listenAddr', key: 'listenAddr' },
  { title: '更新时间', dataIndex: 'updatedAt', key: 'updatedAt', width: 180 },
]

const processColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '进程名', dataIndex: 'name', key: 'name', width: 160 },
  { title: 'PID', dataIndex: 'pid', key: 'pid', width: 80 },
  { title: '用户', dataIndex: 'user', key: 'user', width: 120 },
  { title: '命令行', dataIndex: 'cmdline', key: 'cmdline', ellipsis: true },
  { title: 'CPU%', dataIndex: 'cpu', key: 'cpu', width: 80 },
  { title: '内存%', dataIndex: 'memory', key: 'memory', width: 80 },
  { title: '更新时间', dataIndex: 'updatedAt', key: 'updatedAt', width: 180 },
]

const userColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '用户名', dataIndex: 'username', key: 'username', width: 140 },
  { title: 'UID', dataIndex: 'uid', key: 'uid', width: 80 },
  { title: 'GID', dataIndex: 'gid', key: 'gid', width: 80 },
  { title: 'Home', dataIndex: 'homeDir', key: 'homeDir' },
  { title: 'Shell', dataIndex: 'shell', key: 'shell', width: 160 },
  { title: '最后登录', dataIndex: 'lastLogin', key: 'lastLogin', width: 180 },
]

const packageColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '包名', dataIndex: 'name', key: 'name', width: 200 },
  { title: '版本', dataIndex: 'version', key: 'version', width: 200 },
  { title: '类型', dataIndex: 'type', key: 'type', width: 100 },
  { title: '架构', dataIndex: 'arch', key: 'arch', width: 100 },
  { title: '安装时间', dataIndex: 'installedAt', key: 'installedAt', width: 180 },
]

const crontabColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '用户', dataIndex: 'user', key: 'user', width: 120 },
  { title: '调度', dataIndex: 'schedule', key: 'schedule', width: 160 },
  { title: '命令', dataIndex: 'command', key: 'command', ellipsis: true },
  { title: '更新时间', dataIndex: 'updatedAt', key: 'updatedAt', width: 180 },
]

const serviceColumns = [
  { title: '主机', dataIndex: 'hostname', key: 'hostname', width: 160 },
  { title: '服务名', dataIndex: 'name', key: 'name', width: 200 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 100 },
  { title: '启动方式', dataIndex: 'startType', key: 'startType', width: 100 },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '更新时间', dataIndex: 'updatedAt', key: 'updatedAt', width: 180 },
]

const loadData = async () => {
  loading.value = true
  try {
    const res = await apiClient.get<any>(`/asset-fingerprint/${activeTab.value}`, {
      params: {
        page: pagination.value.current,
        page_size: pagination.value.pageSize,
        search: searchText.value || undefined,
        host_id: filterHost.value || undefined,
      },
    })
    tableData.value = res.items ?? []
    pagination.value.total = res.total ?? 0
  } catch {
    tableData.value = []
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await apiClient.get<any>('/asset-fingerprint/stats')
    if (res) {
      tabStats.value.forEach(tab => {
        tab.count = res[tab.key] ?? 0
      })
    }
  } catch {
    // API 未就绪
  }
}

const loadHosts = async () => {
  try {
    const res = await apiClient.get<any>('/hosts', { params: { page_size: 1000 } })
    hostOptions.value = (res.items ?? []).map((h: any) => ({
      value: h.id,
      label: h.hostname || h.intranet_ip,
    }))
  } catch {
    // ignore
  }
}

const handleTableChange = (pag: any) => {
  pagination.value.current = pag.current
  pagination.value.pageSize = pag.pageSize
  loadData()
}

const handleExport = () => {
  // TODO: 实现导出功能
}

watch(activeTab, () => {
  pagination.value.current = 1
  searchText.value = ''
  loadData()
})

onMounted(() => {
  loadStats()
  loadHosts()
  loadData()
})
</script>

<style scoped>
.asset-fingerprint-page { width: 100%; }
.section-row { margin-bottom: 16px; }

.fp-stat-card {
  background: #FFFFFF;
  border: 1px solid #E5E8EF;
  border-radius: 8px;
  padding: 16px;
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
}
.fp-stat-card:hover { border-color: #165DFF; }
.fp-stat-card.active {
  border-color: #165DFF;
  background: #E8F3FF;
}
.fp-stat-icon { font-size: 20px; color: #165DFF; margin-bottom: 8px; }
.fp-stat-value { font-size: 22px; font-weight: 700; color: #1D2129; line-height: 1.2; }
.fp-stat-label { font-size: 12px; color: #86909C; margin-top: 4px; }

.dashboard-card {
  background: #FFFFFF;
  border: 1px solid #E5E8EF;
  border-radius: 8px;
}
.card-body { padding: 20px; }

.filter-bar {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 16px;
  padding: 12px 16px;
  background: #F7F8FA;
  border-radius: 4px;
  border: 1px solid #E5E8EF;
}
</style>
