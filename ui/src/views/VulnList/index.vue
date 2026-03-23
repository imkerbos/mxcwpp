<template>
  <div class="vuln-list-page">
    <div class="page-header">
      <h2>漏洞列表</h2>
      <span class="page-header-hint">主机漏洞扫描结果与 CVE 详情</span>
    </div>

    <!-- 统计卡片 -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="6">
        <div class="vuln-stat-card">
          <div class="vuln-stat-value">{{ stats.total }}</div>
          <div class="vuln-stat-label">漏洞总数</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="vuln-stat-card">
          <div class="vuln-stat-value" style="color: #F53F3F">{{ stats.critical }}</div>
          <div class="vuln-stat-label">紧急漏洞</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="vuln-stat-card">
          <div class="vuln-stat-value" style="color: #FF7D00">{{ stats.high }}</div>
          <div class="vuln-stat-label">高危漏洞</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="vuln-stat-card">
          <div class="vuln-stat-value" style="color: #165DFF">{{ stats.affectedHosts }}</div>
          <div class="vuln-stat-label">受影响主机</div>
        </div>
      </a-col>
    </a-row>

    <!-- 列表 -->
    <div class="dashboard-card">
      <div class="card-body">
        <div class="filter-bar">
          <a-input-search
            v-model:value="searchText"
            placeholder="搜索 CVE 编号或描述"
            style="width: 280px"
            allow-clear
            @search="loadVulns"
          />
          <a-select v-model:value="filterSeverity" style="width: 140px" placeholder="严重级别" allow-clear @change="loadVulns">
            <a-select-option value="critical">紧急</a-select-option>
            <a-select-option value="high">高危</a-select-option>
            <a-select-option value="medium">中危</a-select-option>
            <a-select-option value="low">低危</a-select-option>
          </a-select>
          <a-select v-model:value="filterStatus" style="width: 140px" placeholder="修复状态" allow-clear @change="loadVulns">
            <a-select-option value="unpatched">未修复</a-select-option>
            <a-select-option value="patched">已修复</a-select-option>
            <a-select-option value="ignored">已忽略</a-select-option>
          </a-select>
          <div style="flex: 1"></div>
          <a-button @click="handleExport">导出报告</a-button>
          <a-button type="primary" @click="handleScan">立即扫描</a-button>
        </div>

        <a-table
          :columns="columns"
          :data-source="vulns"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'cve'">
              <a :href="`https://nvd.nist.gov/vuln/detail/${record.cveId}`" target="_blank" rel="noopener">
                {{ record.cveId }}
              </a>
            </template>
            <template v-if="column.key === 'severity'">
              <a-tag :color="severityColorMap[record.severity]" :bordered="false">
                {{ severityTextMap[record.severity] }}
              </a-tag>
            </template>
            <template v-if="column.key === 'cvss'">
              <span :style="{ color: record.cvssScore >= 9 ? '#F53F3F' : record.cvssScore >= 7 ? '#FF7D00' : '#1D2129', fontWeight: 600 }">
                {{ record.cvssScore }}
              </span>
            </template>
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === 'patched' ? 'green' : record.status === 'ignored' ? 'default' : 'red'" :bordered="false">
                {{ statusTextMap[record.status] }}
              </a-tag>
            </template>
            <template v-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="handleDetail(record)">详情</a-button>
                <a-button type="link" size="small" @click="handleIgnore(record)" v-if="record.status === 'unpatched'">忽略</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </div>
    </div>

    <!-- 漏洞详情 Drawer -->
    <a-drawer
      v-model:open="showDetail"
      :title="detailRecord?.cveId"
      width="640"
      placement="right"
    >
      <template v-if="detailRecord">
        <a-descriptions :column="1" bordered size="small">
          <a-descriptions-item label="CVE 编号">{{ detailRecord.cveId }}</a-descriptions-item>
          <a-descriptions-item label="CVSS 评分">{{ detailRecord.cvssScore }}</a-descriptions-item>
          <a-descriptions-item label="严重级别">
            <a-tag :color="severityColorMap[detailRecord.severity]" :bordered="false">{{ severityTextMap[detailRecord.severity] }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="影响组件">{{ detailRecord.component }}</a-descriptions-item>
          <a-descriptions-item label="当前版本">{{ detailRecord.currentVersion }}</a-descriptions-item>
          <a-descriptions-item label="修复版本">{{ detailRecord.fixedVersion || '暂无' }}</a-descriptions-item>
          <a-descriptions-item label="影响主机数">{{ detailRecord.affectedHosts }}</a-descriptions-item>
          <a-descriptions-item label="描述">{{ detailRecord.description }}</a-descriptions-item>
          <a-descriptions-item label="参考链接">
            <a :href="detailRecord.referenceUrl" target="_blank" rel="noopener">{{ detailRecord.referenceUrl }}</a>
          </a-descriptions-item>
        </a-descriptions>

        <a-divider>受影响主机</a-divider>
        <a-table
          :columns="hostColumns"
          :data-source="detailRecord.hosts ?? []"
          :pagination="false"
          size="small"
        />
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import apiClient from '@/api/client'

const searchText = ref('')
const filterSeverity = ref<string>()
const filterStatus = ref<string>()
const loading = ref(false)
const vulns = ref<any[]>([])
const stats = ref({ total: 0, critical: 0, high: 0, affectedHosts: 0 })

const showDetail = ref(false)
const detailRecord = ref<any>(null)

const pagination = ref({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
})

const severityColorMap: Record<string, string> = {
  critical: 'red', high: 'orange', medium: 'gold', low: 'blue',
}
const severityTextMap: Record<string, string> = {
  critical: '紧急', high: '高危', medium: '中危', low: '低危',
}
const statusTextMap: Record<string, string> = {
  unpatched: '未修复', patched: '已修复', ignored: '已忽略',
}

const columns = [
  { title: 'CVE 编号', key: 'cve', width: 160 },
  { title: '严重级别', key: 'severity', width: 100 },
  { title: 'CVSS', key: 'cvss', width: 80 },
  { title: '影响组件', dataIndex: 'component', key: 'component', width: 200 },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '影响主机', dataIndex: 'affectedHosts', key: 'affectedHosts', width: 100 },
  { title: '状态', key: 'status', width: 100 },
  { title: '发现时间', dataIndex: 'discoveredAt', key: 'discoveredAt', width: 180 },
  { title: '操作', key: 'action', width: 120 },
]

const hostColumns = [
  { title: '主机名', dataIndex: 'hostname', key: 'hostname' },
  { title: 'IP', dataIndex: 'ip', key: 'ip' },
  { title: '当前版本', dataIndex: 'currentVersion', key: 'currentVersion' },
  { title: '状态', dataIndex: 'status', key: 'status' },
]

const loadVulns = async () => {
  loading.value = true
  try {
    const res = await apiClient.get<any>('/vulnerabilities', {
      params: {
        page: pagination.value.current,
        page_size: pagination.value.pageSize,
        search: searchText.value || undefined,
        severity: filterSeverity.value || undefined,
        status: filterStatus.value || undefined,
      },
    })
    vulns.value = res.items ?? []
    pagination.value.total = res.total ?? 0
    if (res.stats) stats.value = res.stats
  } catch {
    vulns.value = []
  } finally {
    loading.value = false
  }
}

const handleTableChange = (pag: any) => {
  pagination.value.current = pag.current
  pagination.value.pageSize = pag.pageSize
  loadVulns()
}

const handleDetail = (record: any) => {
  detailRecord.value = record
  showDetail.value = true
}

const handleIgnore = async (record: any) => {
  try {
    await apiClient.post(`/vulnerabilities/${record.id}/ignore`)
    message.success('已忽略该漏洞')
    loadVulns()
  } catch {
    message.error('操作失败')
  }
}

const handleScan = async () => {
  try {
    await apiClient.post('/vulnerabilities/scan')
    message.success('扫描任务已创建')
  } catch {
    message.error('创建扫描任务失败')
  }
}

const handleExport = () => {
  // TODO: 实现导出
}

onMounted(() => { loadVulns() })
</script>

<style scoped>
.vuln-list-page { width: 100%; }
.section-row { margin-bottom: 16px; }

.vuln-stat-card {
  background: #FFFFFF;
  border: 1px solid #E5E8EF;
  border-radius: 8px;
  padding: 20px;
  text-align: center;
}
.vuln-stat-value { font-size: 28px; font-weight: 700; color: #1D2129; line-height: 1.2; }
.vuln-stat-label { font-size: 13px; color: #86909C; margin-top: 4px; }

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
