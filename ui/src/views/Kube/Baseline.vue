<template>
  <div class="kube-baseline-page">
    <div class="page-header">
      <h2>容器集群基线检查</h2>
      <span class="page-header-hint">Kubernetes 集群安全基线合规检查 (CIS Benchmark)</span>
    </div>

    <!-- 合规概览 -->
    <a-row :gutter="[16, 16]" class="section-row">
      <a-col :span="6">
        <div class="baseline-stat-card">
          <a-progress type="circle" :percent="stats.passRate" :size="80" :stroke-color="stats.passRate >= 80 ? '#00B42A' : stats.passRate >= 60 ? '#FF7D00' : '#F53F3F'" />
          <div class="baseline-stat-label" style="margin-top: 8px">整体合规率</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="baseline-stat-card">
          <div class="baseline-stat-value">{{ stats.totalChecks }}</div>
          <div class="baseline-stat-label">检查项总数</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="baseline-stat-card">
          <div class="baseline-stat-value" style="color: #00B42A">{{ stats.passed }}</div>
          <div class="baseline-stat-label">通过</div>
        </div>
      </a-col>
      <a-col :span="6">
        <div class="baseline-stat-card">
          <div class="baseline-stat-value" style="color: #F53F3F">{{ stats.failed }}</div>
          <div class="baseline-stat-label">未通过</div>
        </div>
      </a-col>
    </a-row>

    <!-- 基线检查列表 -->
    <div class="dashboard-card">
      <div class="card-header">
        <span class="card-title">基线检查项</span>
        <a-space>
          <a-select v-model:value="filterCluster" style="width: 180px" placeholder="选择集群" allow-clear @change="loadBaseline">
            <a-select-option v-for="c in clusterOptions" :key="c.value" :value="c.value">{{ c.label }}</a-select-option>
          </a-select>
          <a-button type="primary" @click="handleRunCheck" :loading="checkLoading">立即检查</a-button>
        </a-space>
      </div>
      <div class="card-body">
        <div class="filter-bar">
          <a-input-search v-model:value="searchText" placeholder="搜索检查项" style="width: 240px" allow-clear @search="loadBaseline" />
          <a-select v-model:value="filterCategory" style="width: 180px" placeholder="检查分类" allow-clear @change="loadBaseline">
            <a-select-option value="control_plane">控制平面</a-select-option>
            <a-select-option value="etcd">etcd</a-select-option>
            <a-select-option value="worker_node">Worker 节点</a-select-option>
            <a-select-option value="policies">安全策略</a-select-option>
            <a-select-option value="network">网络策略</a-select-option>
            <a-select-option value="rbac">RBAC</a-select-option>
            <a-select-option value="pod_security">Pod 安全</a-select-option>
          </a-select>
          <a-select v-model:value="filterResult" style="width: 120px" placeholder="结果" allow-clear @change="loadBaseline">
            <a-select-option value="pass">通过</a-select-option>
            <a-select-option value="fail">未通过</a-select-option>
            <a-select-option value="warn">警告</a-select-option>
          </a-select>
        </div>

        <a-table
          :columns="columns"
          :data-source="checks"
          :loading="loading"
          :pagination="pagination"
          @change="handleTableChange"
          size="middle"
          row-key="id"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'result'">
              <a-tag :color="record.result === 'pass' ? 'green' : record.result === 'fail' ? 'red' : 'orange'" :bordered="false">
                {{ resultTextMap[record.result] }}
              </a-tag>
            </template>
            <template v-if="column.key === 'severity'">
              <a-tag :color="severityColorMap[record.severity]" :bordered="false">{{ severityTextMap[record.severity] }}</a-tag>
            </template>
            <template v-if="column.key === 'action'">
              <a-button type="link" size="small" @click="showCheckDetail(record)">详情</a-button>
            </template>
          </template>
        </a-table>
      </div>
    </div>

    <!-- 检查项详情 Drawer -->
    <a-drawer v-model:open="showDetail" title="基线检查详情" width="640">
      <template v-if="detailRecord">
        <a-descriptions :column="1" bordered size="small">
          <a-descriptions-item label="检查编号">{{ detailRecord.checkId }}</a-descriptions-item>
          <a-descriptions-item label="检查分类">{{ detailRecord.category }}</a-descriptions-item>
          <a-descriptions-item label="检查项">{{ detailRecord.title }}</a-descriptions-item>
          <a-descriptions-item label="结果">
            <a-tag :color="detailRecord.result === 'pass' ? 'green' : 'red'" :bordered="false">{{ resultTextMap[detailRecord.result] }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="严重级别">
            <a-tag :color="severityColorMap[detailRecord.severity]" :bordered="false">{{ severityTextMap[detailRecord.severity] }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="描述">{{ detailRecord.description }}</a-descriptions-item>
          <a-descriptions-item label="修复建议">{{ detailRecord.remediation }}</a-descriptions-item>
          <a-descriptions-item label="参考标准">{{ detailRecord.benchmark }}</a-descriptions-item>
        </a-descriptions>
        <a-divider v-if="detailRecord.affectedResources?.length">受影响资源</a-divider>
        <a-table v-if="detailRecord.affectedResources?.length" :columns="resourceColumns" :data-source="detailRecord.affectedResources" :pagination="false" size="small" />
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import apiClient from '@/api/client'

const searchText = ref('')
const filterCluster = ref<string>()
const filterCategory = ref<string>()
const filterResult = ref<string>()
const loading = ref(false)
const checkLoading = ref(false)
const checks = ref<any[]>([])
const clusterOptions = ref<any[]>([])
const showDetail = ref(false)
const detailRecord = ref<any>(null)
const stats = ref({ passRate: 0, totalChecks: 0, passed: 0, failed: 0 })

const pagination = ref({ current: 1, pageSize: 20, total: 0, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` })

const severityColorMap: Record<string, string> = { critical: 'red', high: 'orange', medium: 'gold', low: 'blue' }
const severityTextMap: Record<string, string> = { critical: '紧急', high: '高危', medium: '中危', low: '低危' }
const resultTextMap: Record<string, string> = { pass: '通过', fail: '未通过', warn: '警告' }

const columns = [
  { title: '编号', dataIndex: 'checkId', key: 'checkId', width: 100 },
  { title: '分类', dataIndex: 'category', key: 'category', width: 120 },
  { title: '检查项', dataIndex: 'title', key: 'title', ellipsis: true },
  { title: '级别', key: 'severity', width: 80 },
  { title: '集群', dataIndex: 'clusterName', key: 'clusterName', width: 140 },
  { title: '结果', key: 'result', width: 100 },
  { title: '检查时间', dataIndex: 'checkedAt', key: 'checkedAt', width: 180 },
  { title: '操作', key: 'action', width: 80 },
]

const resourceColumns = [
  { title: '类型', dataIndex: 'kind', key: 'kind', width: 120 },
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: 'Namespace', dataIndex: 'namespace', key: 'namespace', width: 140 },
]

const loadBaseline = async () => {
  loading.value = true
  try {
    const res = await apiClient.get<any>('/kube/baseline', {
      params: { page: pagination.value.current, page_size: pagination.value.pageSize, search: searchText.value || undefined, cluster_id: filterCluster.value || undefined, category: filterCategory.value || undefined, result: filterResult.value || undefined },
    })
    checks.value = res.items ?? []
    pagination.value.total = res.total ?? 0
    if (res.stats) stats.value = res.stats
  } catch { checks.value = [] }
  finally { loading.value = false }
}

const handleTableChange = (pag: any) => { pagination.value.current = pag.current; pagination.value.pageSize = pag.pageSize; loadBaseline() }
const showCheckDetail = (record: any) => { detailRecord.value = record; showDetail.value = true }

const handleRunCheck = async () => {
  checkLoading.value = true
  try { await apiClient.post('/kube/baseline/detect', { cluster_id: filterCluster.value }); message.success('基线检查任务已创建'); loadBaseline() }
  catch { message.error('创建检查任务失败') }
  finally { checkLoading.value = false }
}

onMounted(() => { loadBaseline() })
</script>

<style scoped>
.kube-baseline-page { width: 100%; }
.section-row { margin-bottom: 16px; }

.baseline-stat-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; padding: 20px; text-align: center; }
.baseline-stat-value { font-size: 28px; font-weight: 700; color: #1D2129; line-height: 1.2; }
.baseline-stat-label { font-size: 13px; color: #86909C; margin-top: 4px; }

.dashboard-card { background: #FFFFFF; border: 1px solid #E5E8EF; border-radius: 8px; }
.card-header { display: flex; align-items: center; justify-content: space-between; padding: 14px 20px; border-bottom: 1px solid #F2F3F5; }
.card-title { font-size: 14px; font-weight: 600; color: #1D2129; }
.card-body { padding: 20px; }
.filter-bar { display: flex; gap: 8px; align-items: center; margin-bottom: 16px; padding: 12px 16px; background: #F7F8FA; border-radius: 4px; border: 1px solid #E5E8EF; }
</style>
