<template>
  <div class="vuln-sources-page">
    <a-page-header
      title="漏洞源管理"
      sub-title="管理国内/国外漏洞数据库同步开关 + 查看同步状态"
    >
      <template #extra>
        <a-button @click="loadSources" :loading="loading">
          <ReloadOutlined /> 刷新
        </a-button>
      </template>
    </a-page-header>

    <a-card v-for="group in groupedSources" :key="group.key" class="source-group" :title="group.title">
      <template #extra>
        <a-tag :color="group.color">{{ group.subtitle }}</a-tag>
      </template>
      <a-table
        :columns="columns"
        :data-source="group.items"
        :pagination="false"
        row-key="id"
        size="middle"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'enabled'">
            <a-switch
              :checked="record.enabled"
              :loading="updatingId === record.id"
              checked-children="启用"
              un-checked-children="禁用"
              @change="(v: boolean) => handleToggle(record, v)"
            />
          </template>
          <template v-else-if="column.key === 'lastStatus'">
            <a-tag :color="statusColor(record.lastStatus)">{{ statusLabel(record.lastStatus) }}</a-tag>
            <span v-if="record.lastCount > 0" class="status-meta">
              {{ record.lastCount.toLocaleString() }} 条
            </span>
            <span v-if="record.lastDurationMs > 0" class="status-meta">
              耗时 {{ (record.lastDurationMs / 1000).toFixed(1) }}s
            </span>
          </template>
          <template v-else-if="column.key === 'lastSyncAt'">
            <span v-if="record.lastSyncAt">{{ formatTime(record.lastSyncAt) }}</span>
            <span v-else class="text-muted">从未</span>
          </template>
          <template v-else-if="column.key === 'lastError'">
            <a-tooltip v-if="record.lastError" :title="record.lastError">
              <span class="text-danger">{{ truncate(record.lastError, 60) }}</span>
            </a-tooltip>
            <span v-else>—</span>
          </template>
          <template v-else-if="column.key === 'baseUrl'">
            <a-typography-text :copyable="{ text: record.baseUrl }" code class="base-url">
              {{ truncate(record.baseUrl, 50) }}
            </a-typography-text>
          </template>
          <template v-else-if="column.key === 'actions'">
            <a-space>
              <a-button size="small" @click="handleTest(record)" :loading="testingId === record.id">
                测试连通性
              </a-button>
              <a-button
                size="small"
                type="primary"
                @click="handleSync(record)"
                :disabled="!record.enabled"
                :loading="syncingId === record.id"
              >
                同步
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { vulnDataSourcesApi, type VulnDataSource } from '@/api/vuln-data-sources'

const sources = ref<VulnDataSource[]>([])
const loading = ref(false)
const updatingId = ref<number | null>(null)
const testingId = ref<number | null>(null)
const syncingId = ref<number | null>(null)

const columns = [
  { title: '名称', dataIndex: 'displayName', key: 'displayName', width: 280 },
  { title: '启用', key: 'enabled', width: 110 },
  { title: 'Base URL', key: 'baseUrl', width: 280 },
  { title: '上次状态', key: 'lastStatus', width: 200 },
  { title: '上次同步', key: 'lastSyncAt', width: 170 },
  { title: '错误信息', key: 'lastError', width: 200 },
  { title: '操作', key: 'actions', width: 200 },
]

const groupedSources = computed(() => {
  const cnOfficial = sources.value.filter(s => s.category === 'cn_official')
  const osAdvisory = sources.value.filter(s => s.category === 'os_advisory')
  const cveMetadata = sources.value.filter(s => s.category === 'cve_metadata')
  const exploit = sources.value.filter(s => s.category === 'exploit')
  return [
    { key: 'os', title: 'OS 厂商漏洞库（CentOS / RHEL / Rocky / Ubuntu / Debian / Alpine）',
      subtitle: '国外 OS Advisory', color: 'blue', items: osAdvisory },
    { key: 'cve', title: 'CVE 标准元数据（MITRE / NVD / OSV）',
      subtitle: 'CVE Metadata', color: 'green', items: cveMetadata },
    { key: 'exp', title: '0day / 已剥削漏洞（CISA KEV / Exploit-DB）',
      subtitle: '0day & Exploit', color: 'volcano', items: exploit },
    { key: 'cn', title: '国内官方漏洞库（CNNVD / CNVD）',
      subtitle: '国内官方', color: 'red', items: cnOfficial },
  ]
})

const loadSources = async () => {
  loading.value = true
  try {
    sources.value = await vulnDataSourcesApi.list()
  } catch (err: any) {
    message.error('加载失败: ' + (err?.message || err))
  } finally {
    loading.value = false
  }
}

const handleToggle = async (record: VulnDataSource, enabled: boolean) => {
  updatingId.value = record.id
  try {
    const updated = await vulnDataSourcesApi.update(record.id, { enabled })
    const idx = sources.value.findIndex(s => s.id === record.id)
    if (idx >= 0) sources.value[idx] = updated
    message.success(`${record.displayName} 已${enabled ? '启用' : '禁用'}`)
  } catch (err: any) {
    message.error('切换失败: ' + (err?.message || err))
  } finally {
    updatingId.value = null
  }
}

const handleTest = async (record: VulnDataSource) => {
  testingId.value = record.id
  try {
    const r = await vulnDataSourcesApi.testConnection(record.id)
    if (r.reachable) {
      message.success(`${record.displayName} 连通 (HTTP ${r.http_status})`)
    } else {
      message.error(`${record.displayName} 不可达: ${r.error || 'HTTP ' + r.http_status}`)
    }
  } catch (err: any) {
    message.error('测试失败: ' + (err?.message || err))
  } finally {
    testingId.value = null
  }
}

const handleSync = async (record: VulnDataSource) => {
  syncingId.value = record.id
  try {
    const r = await vulnDataSourcesApi.triggerSync(record.id)
    message.success(r.message || '同步已触发')
    // 3 秒后刷新拿状态
    setTimeout(loadSources, 3000)
  } catch (err: any) {
    message.error('触发失败: ' + (err?.message || err))
  } finally {
    syncingId.value = null
  }
}

const statusColor = (s: string) => {
  switch (s) {
    case 'success': return 'green'
    case 'running': return 'processing'
    case 'failed':  return 'red'
    default:        return 'default'
  }
}

const statusLabel = (s: string) => {
  switch (s) {
    case 'success': return '成功'
    case 'running': return '同步中'
    case 'failed':  return '失败'
    default:        return '从未'
  }
}

const formatTime = (iso: string) => {
  if (!iso) return ''
  return iso.replace('T', ' ').slice(0, 19)
}

const truncate = (s: string, n: number) => (s.length <= n ? s : s.slice(0, n) + '…')

onMounted(loadSources)
</script>

<style scoped>
.vuln-sources-page {
  padding: 16px;
}
.source-group {
  margin-bottom: 16px;
}
.status-meta {
  margin-left: 8px;
  color: #888;
  font-size: 12px;
}
.base-url {
  font-size: 12px;
}
.text-muted {
  color: #999;
}
.text-danger {
  color: #cf1322;
}
</style>
