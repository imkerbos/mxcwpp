<template>
  <div class="image-scan-page">
    <div class="page-header">
      <h2>镜像扫描</h2>
      <span class="page-header-hint">扫描容器镜像中的安全漏洞</span>
    </div>

    <!-- 扫描输入 -->
    <div class="dashboard-card" style="margin-bottom: 16px;">
      <div class="card-body">
        <div class="scan-input-bar">
          <a-input
            v-model:value="imageInput"
            placeholder="请输入镜像名称，例如: nginx:latest 或 registry.example.com/app:v1.0"
            style="flex: 1"
            @pressEnter="handleScan"
          />
          <a-button
            type="primary"
            :loading="scanning"
            @click="handleScan"
          >
            扫描
          </a-button>
        </div>
      </div>
    </div>

    <!-- 扫描历史 -->
    <div class="dashboard-card">
      <div class="card-body">
        <div class="filter-bar">
          <span class="section-title">扫描历史</span>
          <div class="filter-actions">
            <a-button @click="loadScans">刷新</a-button>
          </div>
        </div>

        <a-table
          :columns="columns"
          :data-source="scans"
          :loading="loading"
          :pagination="pagination"
          size="middle"
          row-key="id"
          :custom-row="(record: ImageScan) => ({ onClick: () => showDetail(record) })"
          class="clickable-table"
          @change="handleTableChange"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'image'">
              <span class="image-name">{{ record.image }}</span>
            </template>
            <template v-else-if="column.key === 'status'">
              <a-tag :color="statusColor(record.status)" :bordered="false">
                {{ statusText(record.status) }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'criticalCnt'">
              <span :class="{ 'count-critical': record.criticalCnt > 0 }">
                {{ record.criticalCnt }}
              </span>
            </template>
            <template v-else-if="column.key === 'highCnt'">
              <span :class="{ 'count-high': record.highCnt > 0 }">
                {{ record.highCnt }}
              </span>
            </template>
            <template v-else-if="column.key === 'scannedAt'">
              {{ formatDate(record.scannedAt) }}
            </template>
          </template>
        </a-table>
      </div>
    </div>

    <!-- 漏洞详情抽屉 -->
    <a-drawer
      v-model:open="drawerVisible"
      :title="`镜像漏洞详情 - ${selectedScan?.image || ''}`"
      :width="900"
      placement="right"
    >
      <template v-if="selectedScan">
        <a-descriptions :column="2" bordered size="small" style="margin-bottom: 16px;">
          <a-descriptions-item label="镜像">{{ selectedScan.image }}</a-descriptions-item>
          <a-descriptions-item label="操作系统">{{ selectedScan.os || '-' }}</a-descriptions-item>
          <a-descriptions-item label="摘要">{{ selectedScan.digest || '-' }}</a-descriptions-item>
          <a-descriptions-item label="扫描时间">{{ formatDate(selectedScan.scannedAt) }}</a-descriptions-item>
          <a-descriptions-item label="漏洞总数">{{ selectedScan.totalVulns }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag :color="statusColor(selectedScan.status)">{{ statusText(selectedScan.status) }}</a-tag>
          </a-descriptions-item>
        </a-descriptions>

        <a-table
          :columns="vulnColumns"
          :data-source="vulns"
          :loading="loadingVulns"
          size="small"
          row-key="id"
          :pagination="{ pageSize: 20, showTotal: (total: number) => `共 ${total} 条` }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'severity'">
              <a-tag :color="severityColor(record.severity)" :bordered="false">
                {{ severityText(record.severity) }}
              </a-tag>
            </template>
          </template>
        </a-table>
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { imageScansApi } from '@/api/image-scans'
import type { ImageScan, ImageVulnerability } from '@/api/image-scans'

const loading = ref(false)
const scanning = ref(false)
const scans = ref<ImageScan[]>([])
const imageInput = ref('')
const drawerVisible = ref(false)
const selectedScan = ref<ImageScan | null>(null)
const vulns = ref<ImageVulnerability[]>([])
const loadingVulns = ref(false)

const pagination = ref({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const columns = [
  { title: 'ID', dataIndex: 'id', width: 60 },
  { title: '镜像', key: 'image', width: 280 },
  { title: '状态', key: 'status', width: 100 },
  { title: '漏洞总数', dataIndex: 'totalVulns', width: 90 },
  { title: '严重', key: 'criticalCnt', width: 70 },
  { title: '高危', key: 'highCnt', width: 70 },
  { title: '扫描时间', key: 'scannedAt', width: 170 },
]

const vulnColumns = [
  { title: 'CVE ID', dataIndex: 'cveId', width: 150 },
  { title: '等级', key: 'severity', width: 80 },
  { title: '软件包', dataIndex: 'package', width: 150 },
  { title: '当前版本', dataIndex: 'version', width: 120 },
  { title: '修复版本', dataIndex: 'fixedVersion', width: 120 },
  { title: '标题', dataIndex: 'title', ellipsis: true },
]

const statusColor = (status: string) => {
  const map: Record<string, string> = {
    completed: 'success',
    scanning: 'processing',
    failed: 'error',
    pending: 'default',
  }
  return map[status] || 'default'
}

const statusText = (status: string) => {
  const map: Record<string, string> = {
    completed: '已完成',
    scanning: '扫描中',
    failed: '失败',
    pending: '排队中',
  }
  return map[status] || status
}

const severityColor = (severity: string) => {
  const map: Record<string, string> = {
    critical: 'red',
    high: 'orange',
    medium: 'gold',
    low: 'blue',
    negligible: 'default',
  }
  return map[severity?.toLowerCase()] || 'default'
}

const severityText = (severity: string) => {
  const map: Record<string, string> = {
    critical: '严重',
    high: '高危',
    medium: '中危',
    low: '低危',
    negligible: '可忽略',
  }
  return map[severity?.toLowerCase()] || severity
}

const formatDate = (dateStr?: string): string => {
  if (!dateStr) return '-'
  return dateStr.replace('T', ' ').substring(0, 19)
}

const loadScans = async () => {
  loading.value = true
  try {
    const res = await imageScansApi.list({
      page: pagination.value.current,
      page_size: pagination.value.pageSize,
    })
    scans.value = res.items ?? []
    pagination.value.total = res.total ?? 0
  } catch {
    scans.value = []
  } finally {
    loading.value = false
  }
}

const handleTableChange = (pag: any) => {
  pagination.value.current = pag.current
  pagination.value.pageSize = pag.pageSize
  loadScans()
}

const handleScan = async () => {
  const image = imageInput.value.trim()
  if (!image) {
    message.warning('请输入镜像名称')
    return
  }
  scanning.value = true
  try {
    await imageScansApi.scan(image)
    message.success('扫描任务已提交')
    imageInput.value = ''
    loadScans()
  } catch {
    message.error('扫描提交失败')
  } finally {
    scanning.value = false
  }
}

const showDetail = async (record: ImageScan) => {
  selectedScan.value = record
  drawerVisible.value = true
  loadingVulns.value = true
  try {
    const data = await imageScansApi.getVulns(record.id)
    vulns.value = data ?? []
  } catch {
    vulns.value = []
    message.error('加载漏洞列表失败')
  } finally {
    loadingVulns.value = false
  }
}

onMounted(() => {
  loadScans()
})
</script>

<style scoped>
.image-scan-page { width: 100%; }

.page-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.page-header-hint {
  font-size: 13px;
  color: #86909C;
}

.dashboard-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; }
.card-body { padding: 20px; }

.scan-input-bar {
  display: flex;
  gap: 12px;
  align-items: center;
}

.filter-bar { display: flex; gap: 12px; margin-bottom: 16px; align-items: center; }
.filter-actions { margin-left: auto; }

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #262626;
}

.image-name {
  font-family: 'SF Mono', 'Monaco', monospace;
  font-size: 13px;
}

.clickable-table :deep(tbody tr) {
  cursor: pointer;
}

.clickable-table :deep(tbody tr:hover) {
  background: #F7F8FA;
}

.count-critical {
  color: #F53F3F;
  font-weight: 700;
}

.count-high {
  color: #FF7D00;
  font-weight: 700;
}
</style>
