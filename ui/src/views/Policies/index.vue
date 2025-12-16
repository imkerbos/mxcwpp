<template>
  <div class="policies-page">
    <!-- 基线概述 -->
    <a-card :bordered="false" class="overview-card">
      <div class="overview-content">
        <div class="overview-left">
          <div class="overview-item">
            <span class="overview-label">最近检查时间：</span>
            <span class="overview-value">{{ lastCheckTime || '-' }}</span>
          </div>
          <a-button type="primary" @click="handleCheckNow" class="check-now-btn">
            立即检查
          </a-button>
        </div>
        <div class="overview-divider"></div>
        <div class="overview-stats">
          <div class="stat-card">
            <div class="stat-value">{{ overallPassRate }}%</div>
            <div class="stat-label">最近检查通过率</div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-card">
            <div class="stat-value">{{ totalHostCount }}</div>
            <div class="stat-label">检查主机数</div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-card">
            <div class="stat-value">{{ totalRuleCount }}</div>
            <div class="stat-label">检查项</div>
          </div>
        </div>
        <div class="overview-divider"></div>
        <div class="overview-right">
          <a-button type="link" @click="handleAutoCheckConfig" class="auto-config-btn">
            <template #icon>
              <SettingOutlined />
            </template>
            自动检查配置
          </a-button>
        </div>
      </div>
    </a-card>

    <!-- 基线内容 -->
    <a-card title="基线内容" :bordered="false" class="content-card">
      <!-- 搜索区域 -->
      <div class="filter-bar">
        <a-select
          v-model:value="filters.groupId"
          placeholder="全部策略组"
          style="width: 160px"
          allow-clear
          @change="handleGroupChange"
        >
          <a-select-option value="">全部策略组</a-select-option>
          <a-select-option v-for="group in policyGroups" :key="group.id" :value="group.id">
            {{ group.name }}
          </a-select-option>
        </a-select>
        <a-select
          v-model:value="filters.riskStatus"
          placeholder="全部"
          style="width: 120px"
          allow-clear
          @change="handleSearch"
        >
          <a-select-option value="all">全部</a-select-option>
          <a-select-option value="risk">有风险</a-select-option>
          <a-select-option value="no-risk">无风险</a-select-option>
        </a-select>
        <a-input
          v-model:value="filters.keyword"
          placeholder="请选择筛选条件并搜索"
          style="width: 300px"
          allow-clear
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
        <a-button @click="loadPolicies">
          <template #icon>
            <ReloadOutlined />
          </template>
        </a-button>
      </div>

      <a-table
        :columns="columns"
        :data-source="filteredPolicies"
        :loading="loading"
        row-key="id"
        :pagination="{ pageSize: 20, showSizeChanger: true, showTotal: (total: number) => `共 ${total} 条` }"
        :scroll="{ x: 800 }"
        class="policies-table"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'risk_count'">
            <a-tag v-if="getRiskCount(record) === 0" color="success">无风险</a-tag>
            <a-tag v-else color="error">{{ getRiskCount(record) }}个风险项</a-tag>
          </template>
          <template v-else-if="column.key === 'last_check_host_count'">
            {{ getLastCheckHostCount(record) }}
          </template>
          <template v-else-if="column.key === 'last_check_time'">
            {{ getLastCheckTime(record) || '-' }}
          </template>
          <template v-else-if="column.key === 'action'">
            <span class="action-cell">
              <a-button type="link" size="small" @click="handleViewDetail(record)">详情</a-button>
              <a-button type="link" size="small" @click="handleRecheck(record)">重新检查</a-button>
            </span>
          </template>
        </template>
      </a-table>
    </a-card>

    <!-- 创建/编辑策略对话框 -->
    <PolicyModal
      v-model:visible="modalVisible"
      :policy="currentPolicy"
      @success="handleModalSuccess"
    />

    <!-- 立即检查对话框 -->
    <a-modal
      v-model:open="checkNowVisible"
      title="立即检查"
      width="800px"
      @ok="handleConfirmCheckNow"
      @cancel="handleCancelCheckNow"
      :confirm-loading="checkNowLoading"
    >
      <a-form :model="checkNowForm" layout="vertical">
        <!-- 策略选择方式 -->
        <a-form-item label="选择方式">
          <a-radio-group v-model:value="checkNowForm.selection_mode" @change="handleSelectionModeChange">
            <a-radio value="group">按策略组选择</a-radio>
            <a-radio value="custom">自定义选择</a-radio>
          </a-radio-group>
        </a-form-item>

        <!-- 按策略组选择 -->
        <a-form-item
          v-if="checkNowForm.selection_mode === 'group'"
          label="选择策略组"
          :rules="[{ required: true, message: '请选择策略组' }]"
        >
          <a-checkbox-group v-model:value="checkNowForm.group_ids" style="width: 100%" @change="handleGroupSelectionChange">
            <a-row :gutter="[8, 8]">
              <a-col :span="12" v-for="group in enabledPolicyGroups" :key="group.id">
                <a-checkbox :value="group.id">
                  <span
                    class="group-icon-small"
                    :style="{ backgroundColor: group.color || '#1890ff' }"
                  >
                    {{ group.icon || group.name.charAt(0) }}
                  </span>
                  {{ group.name }}
                  <a-tag size="small" style="margin-left: 4px">{{ group.policy_count || 0 }}个策略</a-tag>
                </a-checkbox>
              </a-col>
            </a-row>
          </a-checkbox-group>
          <div style="margin-top: 8px">
            <a-button type="link" size="small" @click="handleSelectAllGroups">全选</a-button>
            <a-button type="link" size="small" @click="handleDeselectAllGroups">取消全选</a-button>
          </div>
        </a-form-item>

        <!-- 自定义选择策略 -->
        <a-form-item
          v-if="checkNowForm.selection_mode === 'custom'"
          label="选择检查基线"
          :rules="[{ required: true, message: '请选择至少一个检查基线' }]"
        >
          <a-checkbox-group v-model:value="checkNowForm.policy_ids" style="width: 100%">
            <div v-for="group in policyGroupsWithPolicies" :key="group.id" style="margin-bottom: 12px">
              <div style="font-weight: 500; margin-bottom: 8px; display: flex; align-items: center;">
                <span
                  class="group-icon-small"
                  :style="{ backgroundColor: group.color || '#1890ff' }"
                >
                  {{ group.icon || group.name.charAt(0) }}
                </span>
                <span style="margin-left: 8px">{{ group.name }}</span>
                <a-button type="link" size="small" @click="handleSelectGroupPolicies(group.id)">全选</a-button>
                <a-button type="link" size="small" @click="handleDeselectGroupPolicies(group.id)">取消</a-button>
              </div>
              <a-row :gutter="[8, 8]" style="padding-left: 28px">
                <a-col :span="12" v-for="policy in group.policies" :key="policy.id">
                  <a-checkbox :value="policy.id" :disabled="!policy.enabled">
                    {{ policy.name }}
                    <a-tag size="small" style="margin-left: 4px">{{ policy.rule_count || 0 }}项</a-tag>
                    <a-tag v-if="!policy.enabled" size="small" color="default">已禁用</a-tag>
                  </a-checkbox>
                </a-col>
              </a-row>
            </div>
            <div v-if="ungroupedPolicies.length > 0" style="margin-bottom: 12px">
              <div style="font-weight: 500; margin-bottom: 8px; color: #999;">
                未分组策略
                <a-button type="link" size="small" @click="handleSelectUngroupedPolicies">全选</a-button>
                <a-button type="link" size="small" @click="handleDeselectUngroupedPolicies">取消</a-button>
              </div>
              <a-row :gutter="[8, 8]" style="padding-left: 28px">
                <a-col :span="12" v-for="policy in ungroupedPolicies" :key="policy.id">
                  <a-checkbox :value="policy.id" :disabled="!policy.enabled">
                    {{ policy.name }}
                    <a-tag size="small" style="margin-left: 4px">{{ policy.rule_count || 0 }}项</a-tag>
                    <a-tag v-if="!policy.enabled" size="small" color="default">已禁用</a-tag>
                  </a-checkbox>
                </a-col>
              </a-row>
            </div>
          </a-checkbox-group>
          <div style="margin-top: 8px">
            <a-button type="link" size="small" @click="handleSelectAllPolicies">全选所有</a-button>
            <a-button type="link" size="small" @click="handleDeselectAllPolicies">取消全选</a-button>
          </div>
        </a-form-item>

        <a-divider />

        <!-- 主机范围 -->
        <a-form-item label="主机范围" :rules="[{ required: true, message: '请选择主机范围' }]">
          <a-radio-group v-model:value="checkNowForm.target_type" @change="handleTargetTypeChange">
            <a-radio value="all">全部主机</a-radio>
            <a-radio value="business_line">按业务线</a-radio>
            <a-radio value="tags">按标签</a-radio>
            <a-radio value="os_family">按操作系统</a-radio>
            <a-radio value="host_ids">指定主机</a-radio>
          </a-radio-group>
        </a-form-item>

        <!-- 业务线选择 -->
        <a-form-item
          v-if="checkNowForm.target_type === 'business_line'"
          label="选择业务线"
        >
          <a-select
            v-model:value="checkNowForm.business_lines"
            mode="multiple"
            placeholder="选择业务线"
            :loading="businessLinesLoading"
            style="width: 100%"
          >
            <a-select-option v-for="bl in businessLines" :key="bl.code" :value="bl.code">
              {{ bl.name }} ({{ bl.host_count || 0 }}台)
            </a-select-option>
          </a-select>
        </a-form-item>

        <!-- 标签选择 -->
        <a-form-item
          v-if="checkNowForm.target_type === 'tags'"
          label="选择标签"
        >
          <a-select
            v-model:value="checkNowForm.tags"
            mode="multiple"
            placeholder="选择或输入标签"
            style="width: 100%"
            :options="tagOptions"
          />
        </a-form-item>

        <!-- 操作系统选择 -->
        <a-form-item
          v-if="checkNowForm.target_type === 'os_family'"
          label="选择操作系统"
        >
          <a-select
            v-model:value="checkNowForm.os_family"
            mode="multiple"
            placeholder="选择操作系统"
            :options="osOptions"
          />
        </a-form-item>

        <!-- 指定主机 -->
        <a-form-item
          v-if="checkNowForm.target_type === 'host_ids'"
          label="选择主机"
        >
          <a-select
            v-model:value="checkNowForm.host_ids"
            mode="multiple"
            placeholder="选择主机"
            :options="hostOptions"
            :loading="hostsLoading"
            show-search
            :filter-option="filterHostOption"
          />
        </a-form-item>

        <a-alert
          type="info"
          show-icon
          style="margin-top: 16px"
        >
          <template #message>
            <span v-if="checkNowForm.selection_mode === 'group'">
              已选择 {{ checkNowForm.group_ids.length }} 个策略组（共 {{ getSelectedPoliciesCount() }} 个策略），
            </span>
            <span v-else>
              已选择 {{ checkNowForm.policy_ids.length }} 个策略，
            </span>
            {{ getTargetHostsDescription() }}
          </template>
        </a-alert>
      </a-form>
    </a-modal>

    <!-- 自动检查配置对话框 -->
    <a-modal
      v-model:visible="autoConfigVisible"
      title="自动检查配置"
      width="1200px"
      :footer="null"
      @cancel="handleCloseAutoConfig"
    >
      <div class="auto-config-content">
        <div class="auto-config-header">
          <a-button type="primary" @click="handleShowCreateTask">
            <template #icon>
              <PlusOutlined />
            </template>
            新建任务
          </a-button>
        </div>

        <!-- 任务列表 -->
        <a-table
          :columns="taskColumns"
          :data-source="scheduledTasks"
          :loading="tasksLoading"
          row-key="task_id"
          :pagination="{ pageSize: 10, showSizeChanger: true, showTotal: (total: number) => `共 ${total} 条` }"
          class="tasks-table"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'policy_names'">
              <a-tag v-for="name in getPolicyNames(record)" :key="name" style="margin-right: 4px">
                {{ name }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'host_scope'">
              {{ getHostScopeText(record) }}
            </template>
            <template v-else-if="column.key === 'check_time'">
              {{ getCheckTimeText(record) }}
            </template>
            <template v-else-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="handleEditTask(record)">编辑</a-button>
                <a-popconfirm
                  title="确定要删除这个任务吗？"
                  @confirm="handleDeleteTask(record)"
                >
                  <a-button type="link" size="small" danger>删除</a-button>
                </a-popconfirm>
              </a-space>
            </template>
          </template>
          <template #emptyText>
            <a-empty description="暂无数据" :image="false" />
          </template>
        </a-table>
      </div>
    </a-modal>

    <!-- 新建/编辑任务对话框 -->
    <a-modal
      v-model:visible="taskModalVisible"
      :title="editingTask ? '编辑任务' : '新建任务'"
      width="700px"
      @ok="handleSaveTask"
      @cancel="handleCancelTask"
    >
      <a-form :model="taskForm" layout="vertical" ref="taskFormRef">
        <a-form-item
          label="任务名称"
          name="name"
          :rules="[{ required: true, message: '请输入任务名称' }]"
        >
          <a-input v-model:value="taskForm.name" placeholder="请输入任务名称" />
        </a-form-item>

        <a-form-item
          label="定时周期"
          name="frequency"
          :rules="[{ required: true, message: '请选择定时周期' }]"
        >
          <a-radio-group v-model:value="taskForm.frequency">
            <a-radio value="daily">每日</a-radio>
            <a-radio value="weekly">每周</a-radio>
            <a-radio value="monthly">每月</a-radio>
            <a-radio value="custom">自定义</a-radio>
          </a-radio-group>
        </a-form-item>

        <template v-if="taskForm.frequency !== 'custom'">
          <a-form-item
            label="时间配置"
            name="time"
            :rules="[{ required: true, message: '请选择时间' }]"
          >
            <a-time-picker
              v-model:value="taskForm.time"
              format="HH:mm"
              placeholder="选择时间"
              style="width: 100%"
            />
          </a-form-item>
        </template>

        <template v-else>
          <a-form-item
            label="Cron表达式"
            name="cron"
            :rules="[{ required: true, message: '请输入Cron表达式' }]"
          >
            <a-input
              v-model:value="taskForm.cron"
              placeholder="例如: 0 0 2 * * ? (每天凌晨2点)"
            />
          </a-form-item>
        </template>

        <a-form-item
          label="时间基准"
          name="timezone"
          :rules="[{ required: true, message: '请选择时间基准' }]"
        >
          <a-select v-model:value="taskForm.timezone" placeholder="选择时间基准">
            <a-select-option value="UTC">UTC</a-select-option>
            <a-select-option value="Asia/Shanghai">本地时间（Asia/Shanghai）</a-select-option>
          </a-select>
        </a-form-item>

        <a-form-item
          label="扫描基线"
          name="policy_ids"
          :rules="[{ required: true, message: '请选择至少一个扫描基线' }]"
        >
          <a-select
            v-model:value="taskForm.policy_ids"
            mode="multiple"
            placeholder="请选择扫描基线（可多选）"
            :options="policyOptions"
          />
        </a-form-item>

        <a-form-item
          label="主机范围"
          name="target_type"
          :rules="[{ required: true, message: '请选择主机范围' }]"
        >
          <a-radio-group v-model:value="taskForm.target_type">
            <a-radio value="all">全部主机</a-radio>
            <a-radio value="os_family">按操作系统</a-radio>
          </a-radio-group>
        </a-form-item>

        <a-form-item
          v-if="taskForm.target_type === 'os_family'"
          label="操作系统"
          name="os_family"
          :rules="taskForm.target_type === 'os_family' ? [{ required: true, message: '请选择操作系统' }] : []"
        >
          <a-select
            v-model:value="taskForm.os_family"
            mode="multiple"
            placeholder="选择操作系统"
            :options="osOptions"
          />
        </a-form-item>

        <a-form-item label="备注" name="remark">
          <a-textarea
            v-model:value="taskForm.remark"
            placeholder="请输入备注信息（可选）"
            :rows="3"
          />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  SettingOutlined,
  SearchOutlined,
  ReloadOutlined,
  PlusOutlined,
} from '@ant-design/icons-vue'
import type { FormInstance } from 'ant-design-vue'
import dayjs, { type Dayjs } from 'dayjs'
import { policiesApi } from '@/api/policies'
import { policyGroupsApi } from '@/api/policy-groups'
import { resultsApi } from '@/api/results'
import { tasksApi } from '@/api/tasks'
import { hostsApi } from '@/api/hosts'
import { businessLinesApi, type BusinessLine } from '@/api/business-lines'
import type { Policy, ScanResult, PolicyGroup, Host } from '@/api/types'
import { message } from 'ant-design-vue'
import PolicyModal from './components/PolicyModal.vue'

const router = useRouter()
const route = useRoute()

const loading = ref(false)
const policies = ref<Policy[]>([])
const policyGroups = ref<PolicyGroup[]>([])
const policyStats = ref<Map<string, PolicyStats>>(new Map())
const filters = reactive({
  groupId: '' as string,
  riskStatus: 'all' as 'all' | 'risk' | 'no-risk',
  keyword: '',
})

const modalVisible = ref(false)
const autoConfigVisible = ref(false)
const taskModalVisible = ref(false)
const tasksLoading = ref(false)
const currentPolicy = ref<Policy | null>(null)
const editingTask = ref<ScanTask | null>(null)
const scheduledTasks = ref<ScanTask[]>([])
const taskFormRef = ref<FormInstance>()

// 立即检查对话框
const checkNowVisible = ref(false)
const checkNowLoading = ref(false)
const hostsLoading = ref(false)
const businessLinesLoading = ref(false)
const hosts = ref<Host[]>([])
const businessLines = ref<BusinessLine[]>([])
const allPolicies = ref<Policy[]>([]) // 所有策略（用于自定义选择）
const checkNowForm = reactive({
  selection_mode: 'group' as 'group' | 'custom',
  group_ids: [] as string[],
  policy_ids: [] as string[],
  target_type: 'all' as 'all' | 'business_line' | 'tags' | 'os_family' | 'host_ids',
  business_lines: [] as string[],
  tags: [] as string[],
  os_family: [] as string[],
  host_ids: [] as string[],
})

const taskForm = reactive({
  name: '',
  frequency: 'daily' as 'daily' | 'weekly' | 'monthly' | 'custom',
  time: null as Dayjs | null,
  cron: '',
  timezone: 'Asia/Shanghai' as 'UTC' | 'Asia/Shanghai',
  policy_ids: [] as string[],
  target_type: 'all' as 'all' | 'os_family',
  os_family: [] as string[],
  remark: '',
})

const policyOptions = computed(() => {
  return policies.value.map((p) => ({
    label: p.name,
    value: p.id,
  }))
})

const enabledPolicies = computed(() => {
  return policies.value.filter((p) => p.enabled)
})

// 启用的策略组
const enabledPolicyGroups = computed(() => {
  return policyGroups.value.filter((g) => g.enabled)
})

// 策略组（含策略列表）
const policyGroupsWithPolicies = computed(() => {
  return policyGroups.value.map(group => ({
    ...group,
    policies: allPolicies.value.filter(p => p.group_id === group.id)
  })).filter(g => g.policies.length > 0)
})

// 未分组的策略
const ungroupedPolicies = computed(() => {
  return allPolicies.value.filter(p => !p.group_id && p.enabled)
})

// 标签选项（从主机中提取）
const tagOptions = computed(() => {
  const tags = new Set<string>()
  hosts.value.forEach(h => {
    if (h.tags) {
      h.tags.forEach(t => tags.add(t))
    }
  })
  return Array.from(tags).map(t => ({ label: t, value: t }))
})

const hostOptions = computed(() => {
  return hosts.value.map((h) => ({
    label: `${h.hostname} (${h.ipv4?.[0] || h.host_id})`,
    value: h.host_id,
  }))
})

const osOptions = [
  { label: 'Rocky Linux', value: 'rocky' },
  { label: 'CentOS', value: 'centos' },
  { label: 'Oracle Linux', value: 'oracle' },
  { label: 'Debian', value: 'debian' },
  { label: 'Ubuntu', value: 'ubuntu' },
  { label: 'openEuler', value: 'openeuler' },
  { label: 'Alibaba Cloud Linux', value: 'alibaba' },
]

const taskColumns = [
  {
    title: '任务名称',
    dataIndex: 'name',
    key: 'name',
    ellipsis: true,
  },
  {
    title: '检查基线',
    key: 'policy_names',
    width: 200,
  },
  {
    title: '主机范围',
    key: 'host_scope',
    width: 150,
  },
  {
    title: '检查时间',
    key: 'check_time',
    width: 200,
  },
  {
    title: '备注',
    dataIndex: 'remark',
    key: 'remark',
    ellipsis: true,
  },
  {
    title: '最近操作人/时间',
    dataIndex: 'updated_at',
    key: 'updated_at',
    width: 180,
  },
  {
    title: '操作',
    key: 'action',
    width: 150,
    fixed: 'right' as const,
  },
]

interface PolicyStats {
  passRate: number
  hostCount: number
  riskCount: number
  lastCheckTime: string
  lastCheckHostCount: number
}

const columns = [
  {
    title: '基线名称',
    dataIndex: 'name',
    key: 'name',
    ellipsis: true,
  },
  {
    title: '检查项',
    dataIndex: 'rule_count',
    key: 'rule_count',
    width: 120,
  },
  {
    title: '风险项',
    key: 'risk_count',
    width: 120,
    sorter: (a: Policy, b: Policy) => {
      return (getRiskCount(a) || 0) - (getRiskCount(b) || 0)
    },
  },
  {
    title: '最近检查主机数',
    key: 'last_check_host_count',
    width: 150,
    sorter: (a: Policy, b: Policy) => {
      return (
        (getLastCheckHostCount(a) || 0) - (getLastCheckHostCount(b) || 0)
      )
    },
  },
  {
    title: '最近检查时间',
    key: 'last_check_time',
    width: 180,
    sorter: (a: Policy, b: Policy) => {
      const timeA = getLastCheckTime(a)
      const timeB = getLastCheckTime(b)
      if (!timeA && !timeB) return 0
      if (!timeA) return 1
      if (!timeB) return -1
      return new Date(timeA).getTime() - new Date(timeB).getTime()
    },
  },
  {
    title: '操作',
    key: 'action',
    width: 150,
    fixed: 'right' as const,
  },
]

const filteredPolicies = computed(() => {
  let result = policies.value

  // 关键词搜索
  if (filters.keyword) {
    result = result.filter((policy) =>
      policy.name.toLowerCase().includes(filters.keyword.toLowerCase())
    )
  }

  // 风险状态筛选
  if (filters.riskStatus === 'risk') {
    result = result.filter((policy) => getRiskCount(policy) > 0)
  } else if (filters.riskStatus === 'no-risk') {
    result = result.filter((policy) => getRiskCount(policy) === 0)
  }

  return result
})

// 统计数据
const lastCheckTime = computed(() => {
  const times = Array.from(policyStats.value.values())
    .map((s) => s.lastCheckTime)
    .filter((t) => t)
    .sort()
    .reverse()
  return times[0] || ''
})

const overallPassRate = computed(() => {
  const stats = Array.from(policyStats.value.values())
  if (stats.length === 0) return 0
  const totalPassRate = stats.reduce((sum, s) => sum + s.passRate, 0)
  return Math.round(totalPassRate / stats.length)
})

const totalHostCount = computed(() => {
  const hostIds = new Set<string>()
  Array.from(policyStats.value.values()).forEach((s) => {
    // TODO: 从实际结果中获取主机ID
  })
  return hostIds.size
})

const totalRuleCount = computed(() => {
  return policies.value.reduce((sum, p) => sum + (p.rule_count || 0), 0)
})

const getRiskCount = (policy: Policy): number => {
  return policyStats.value.get(policy.id)?.riskCount || 0
}

const getLastCheckHostCount = (policy: Policy): number => {
  return policyStats.value.get(policy.id)?.lastCheckHostCount || 0
}

const getLastCheckTime = (policy: Policy): string => {
  return policyStats.value.get(policy.id)?.lastCheckTime || ''
}

const loadPolicies = async () => {
  loading.value = true
  try {
    // 构建查询参数
    const params: { group_id?: string } = {}
    if (filters.groupId) {
      params.group_id = filters.groupId
    }

    const response = (await policiesApi.list(params)) as unknown as {
      items: Policy[]
    }
    policies.value = response.items

    // 加载每个策略的统计数据
    await loadPolicyStats()
  } catch (error) {
    console.error('加载策略列表失败:', error)
    message.error('加载策略列表失败')
  } finally {
    loading.value = false
  }
}

// 加载策略组列表
const loadPolicyGroups = async () => {
  try {
    const response = await policyGroupsApi.list() as any
    policyGroups.value = response.data?.items || response.items || []
  } catch (error) {
    console.error('加载策略组列表失败:', error)
  }
}

// 加载主机列表
const loadHosts = async () => {
  hostsLoading.value = true
  try {
    const response = await hostsApi.list({ page_size: 1000 }) as any
    hosts.value = response.items || []
  } catch (error) {
    console.error('加载主机列表失败:', error)
  } finally {
    hostsLoading.value = false
  }
}

// 处理策略组筛选变化
const handleGroupChange = () => {
  // 更新 URL 参数
  if (filters.groupId) {
    router.replace({ query: { ...route.query, group_id: filters.groupId } })
  } else {
    const { group_id, ...rest } = route.query
    router.replace({ query: rest })
  }
  loadPolicies()
}

// 主机选项过滤
const filterHostOption = (input: string, option: any) => {
  return option.label.toLowerCase().includes(input.toLowerCase())
}

const loadPolicyStats = async () => {
  for (const policy of policies.value) {
    try {
      const resultsResponse = (await resultsApi.list({
        policy_id: policy.id,
        page_size: 1000,
      })) as unknown as { total: number; items: ScanResult[] }

      const results = resultsResponse.items
      if (results.length === 0) {
        policyStats.value.set(policy.id, {
          passRate: 0,
          hostCount: 0,
          riskCount: 0,
          lastCheckTime: '',
          lastCheckHostCount: 0,
        })
        continue
      }

      // 计算通过率
      const totalResults = results.length
      const passResults = results.filter((r) => r.status === 'pass').length
      const passRate =
        totalResults > 0 ? Math.round((passResults / totalResults) * 100) : 0

      // 计算风险项数量
      const failedRules = new Set(
        results.filter((r) => r.status === 'fail').map((r) => r.rule_id)
      )
      const riskCount = failedRules.size

      // 计算主机数
      const hostIds = new Set(results.map((r) => r.host_id))
      const hostCount = hostIds.size

      // 获取最近检查时间
      const checkTimes = results
        .map((r) => r.checked_at)
        .filter((t) => t)
        .sort()
        .reverse()
      const lastCheckTime = checkTimes[0] || ''

      policyStats.value.set(policy.id, {
        passRate,
        hostCount,
        riskCount,
        lastCheckTime,
        lastCheckHostCount: hostCount,
      })
    } catch (error) {
      console.error(`加载策略 ${policy.id} 统计失败:`, error)
    }
  }
}

const handleSearch = () => {
  // 搜索已通过filteredPolicies处理
}

const handleCheckNow = async () => {
  // 打开立即检查对话框
  checkNowForm.selection_mode = 'group'
  checkNowForm.group_ids = []
  checkNowForm.policy_ids = []
  checkNowForm.target_type = 'all'
  checkNowForm.business_lines = []
  checkNowForm.tags = []
  checkNowForm.os_family = []
  checkNowForm.host_ids = []
  checkNowVisible.value = true

  // 加载所有策略（用于自定义选择）
  if (allPolicies.value.length === 0) {
    try {
      const response = await policiesApi.list() as any
      allPolicies.value = response.items || []
    } catch (error) {
      console.error('加载所有策略失败:', error)
    }
  }

  // 加载主机列表
  if (hosts.value.length === 0) {
    loadHosts()
  }

  // 加载业务线列表
  if (businessLines.value.length === 0) {
    loadBusinessLines()
  }
}

// 加载业务线列表
const loadBusinessLines = async () => {
  businessLinesLoading.value = true
  try {
    const response = await businessLinesApi.list({ page_size: 1000 }) as any
    businessLines.value = response.items || []
  } catch (error) {
    console.error('加载业务线列表失败:', error)
  } finally {
    businessLinesLoading.value = false
  }
}

// 选择方式变更
const handleSelectionModeChange = () => {
  // 清空已选择的内容
  checkNowForm.group_ids = []
  checkNowForm.policy_ids = []
}

// 策略组选择变更
const handleGroupSelectionChange = () => {
  // 更新策略组选择后的计数
}

// 目标类型变更
const handleTargetTypeChange = () => {
  // 清空相关选择
  checkNowForm.business_lines = []
  checkNowForm.tags = []
  checkNowForm.os_family = []
  checkNowForm.host_ids = []
}

// 全选策略组
const handleSelectAllGroups = () => {
  checkNowForm.group_ids = enabledPolicyGroups.value.map(g => g.id)
}

// 取消全选策略组
const handleDeselectAllGroups = () => {
  checkNowForm.group_ids = []
}

// 选择某个策略组下的所有策略
const handleSelectGroupPolicies = (groupId: string) => {
  const groupPolicies = allPolicies.value
    .filter(p => p.group_id === groupId && p.enabled)
    .map(p => p.id)
  const newIds = new Set([...checkNowForm.policy_ids, ...groupPolicies])
  checkNowForm.policy_ids = Array.from(newIds)
}

// 取消选择某个策略组下的所有策略
const handleDeselectGroupPolicies = (groupId: string) => {
  const groupPolicyIds = allPolicies.value
    .filter(p => p.group_id === groupId)
    .map(p => p.id)
  checkNowForm.policy_ids = checkNowForm.policy_ids.filter(id => !groupPolicyIds.includes(id))
}

// 选择未分组策略
const handleSelectUngroupedPolicies = () => {
  const ungroupedIds = ungroupedPolicies.value.map(p => p.id)
  const newIds = new Set([...checkNowForm.policy_ids, ...ungroupedIds])
  checkNowForm.policy_ids = Array.from(newIds)
}

// 取消选择未分组策略
const handleDeselectUngroupedPolicies = () => {
  const ungroupedIds = ungroupedPolicies.value.map(p => p.id)
  checkNowForm.policy_ids = checkNowForm.policy_ids.filter(id => !ungroupedIds.includes(id))
}

// 获取选中策略组包含的策略数量
const getSelectedPoliciesCount = () => {
  let count = 0
  for (const groupId of checkNowForm.group_ids) {
    const group = policyGroups.value.find(g => g.id === groupId)
    if (group) {
      count += group.policy_count || 0
    }
  }
  return count
}

// 确认立即检查
const handleConfirmCheckNow = async () => {
  // 根据选择模式获取策略ID列表
  let policyIds: string[] = []
  if (checkNowForm.selection_mode === 'group') {
    if (checkNowForm.group_ids.length === 0) {
      message.warning('请选择至少一个策略组')
      return
    }
    // 获取所选策略组下的所有启用策略
    for (const groupId of checkNowForm.group_ids) {
      const groupPolicies = allPolicies.value
        .filter(p => p.group_id === groupId && p.enabled)
        .map(p => p.id)
      policyIds.push(...groupPolicies)
    }
    if (policyIds.length === 0) {
      message.warning('所选策略组下没有启用的策略')
      return
    }
  } else {
    if (checkNowForm.policy_ids.length === 0) {
      message.warning('请选择至少一个检查基线')
      return
    }
    policyIds = [...checkNowForm.policy_ids]
  }

  // 验证目标选择
  if (checkNowForm.target_type === 'business_line' && checkNowForm.business_lines.length === 0) {
    message.warning('请选择业务线')
    return
  }

  if (checkNowForm.target_type === 'tags' && checkNowForm.tags.length === 0) {
    message.warning('请选择标签')
    return
  }

  if (checkNowForm.target_type === 'os_family' && checkNowForm.os_family.length === 0) {
    message.warning('请选择操作系统')
    return
  }

  if (checkNowForm.target_type === 'host_ids' && checkNowForm.host_ids.length === 0) {
    message.warning('请选择主机')
    return
  }

  checkNowLoading.value = true
  try {
    // 根据目标类型构建目标配置
    let targetType: 'all' | 'host_ids' | 'os_family' = 'all'
    let targetHostIds: string[] | undefined
    let targetOsFamily: string[] | undefined

    if (checkNowForm.target_type === 'host_ids') {
      targetType = 'host_ids'
      targetHostIds = checkNowForm.host_ids
    } else if (checkNowForm.target_type === 'os_family') {
      targetType = 'os_family'
      targetOsFamily = checkNowForm.os_family
    } else if (checkNowForm.target_type === 'business_line') {
      // 按业务线筛选主机
      targetType = 'host_ids'
      targetHostIds = hosts.value
        .filter(h => h.business_line && checkNowForm.business_lines.includes(h.business_line))
        .map(h => h.host_id)
      if (targetHostIds.length === 0) {
        message.warning('所选业务线下没有主机')
        checkNowLoading.value = false
        return
      }
    } else if (checkNowForm.target_type === 'tags') {
      // 按标签筛选主机
      targetType = 'host_ids'
      targetHostIds = hosts.value
        .filter(h => h.tags && h.tags.some(t => checkNowForm.tags.includes(t)))
        .map(h => h.host_id)
      if (targetHostIds.length === 0) {
        message.warning('所选标签下没有主机')
        checkNowLoading.value = false
        return
      }
    }

    // 为每个选中的策略创建检查任务
    for (const policyId of policyIds) {
      const policy = allPolicies.value.find(p => p.id === policyId) || policies.value.find(p => p.id === policyId)
      await tasksApi.create({
        name: `立即检查-${policy?.name || policyId}`,
        type: 'manual',
        targets: {
          type: targetType,
          os_family: targetOsFamily,
          host_ids: targetHostIds,
        },
        policy_id: policyId,
      })
    }

    message.success(`已创建 ${policyIds.length} 个检查任务`)
    checkNowVisible.value = false
    // 刷新统计数据
    await loadPolicyStats()
  } catch (error) {
    console.error('创建检查任务失败:', error)
    message.error('创建检查任务失败')
  } finally {
    checkNowLoading.value = false
  }
}

// 取消立即检查
const handleCancelCheckNow = () => {
  checkNowVisible.value = false
}

// 全选策略
const handleSelectAllPolicies = () => {
  checkNowForm.policy_ids = enabledPolicies.value.map(p => p.id)
}

// 取消全选策略
const handleDeselectAllPolicies = () => {
  checkNowForm.policy_ids = []
}

// 获取目标主机描述
const getTargetHostsDescription = () => {
  if (checkNowForm.target_type === 'all') {
    return '将检查全部主机'
  } else if (checkNowForm.target_type === 'business_line') {
    if (checkNowForm.business_lines.length === 0) {
      return '请选择业务线'
    }
    const selectedBLNames = checkNowForm.business_lines.map(code => {
      const bl = businessLines.value.find(b => b.code === code)
      return bl?.name || code
    })
    return `将检查业务线: ${selectedBLNames.join(', ')}`
  } else if (checkNowForm.target_type === 'tags') {
    if (checkNowForm.tags.length === 0) {
      return '请选择标签'
    }
    return `将检查标签: ${checkNowForm.tags.join(', ')}`
  } else if (checkNowForm.target_type === 'os_family') {
    if (checkNowForm.os_family.length === 0) {
      return '请选择操作系统'
    }
    return `将检查 ${checkNowForm.os_family.join(', ')} 系统的主机`
  } else if (checkNowForm.target_type === 'host_ids') {
    if (checkNowForm.host_ids.length === 0) {
      return '请选择主机'
    }
    return `将检查 ${checkNowForm.host_ids.length} 台指定主机`
  }
  return ''
}

const handleAutoCheckConfig = () => {
  autoConfigVisible.value = true
  loadScheduledTasks()
}

const handleCloseAutoConfig = () => {
  autoConfigVisible.value = false
  editingTask.value = null
}

const loadScheduledTasks = async () => {
  tasksLoading.value = true
  try {
    const response = (await tasksApi.list({
      page_size: 1000,
    })) as unknown as { total: number; items: ScanTask[] }

    // 只显示定时任务
    scheduledTasks.value = response.items.filter((task) => task.type === 'scheduled')
  } catch (error) {
    console.error('加载定时任务列表失败:', error)
    message.error('加载定时任务列表失败')
  } finally {
    tasksLoading.value = false
  }
}

const handleShowCreateTask = () => {
  editingTask.value = null
  resetTaskForm()
  taskModalVisible.value = true
}

const handleEditTask = (task: ScanTask) => {
  editingTask.value = task
  // TODO: 从任务中解析并填充表单
  taskForm.name = task.name
  taskForm.target_type = task.target_type
  taskForm.os_family = task.target_config.os_family || []
  taskForm.policy_ids = [task.policy_id] // 单个策略ID
  taskModalVisible.value = true
}

const handleDeleteTask = async (task: ScanTask) => {
  try {
    // TODO: 实现删除任务API
    message.success('删除任务成功')
    loadScheduledTasks()
  } catch (error) {
    console.error('删除任务失败:', error)
    message.error('删除任务失败')
  }
}

const resetTaskForm = () => {
  taskForm.name = ''
  taskForm.frequency = 'daily'
  taskForm.time = null
  taskForm.cron = ''
  taskForm.timezone = 'Asia/Shanghai'
  taskForm.policy_ids = []
  taskForm.target_type = 'all'
  taskForm.os_family = []
  taskForm.remark = ''
  taskFormRef.value?.resetFields()
}

const handleSaveTask = async () => {
  try {
    await taskFormRef.value?.validate()

    if (taskForm.policy_ids.length === 0) {
      message.warning('请选择至少一个扫描基线')
      return
    }

    // 构建Cron表达式
    let cron = ''
    if (taskForm.frequency === 'custom') {
      cron = taskForm.cron
    } else {
      const time = taskForm.time || dayjs('02:00', 'HH:mm')
      const hour = time.hour()
      const minute = time.minute()

      if (taskForm.frequency === 'daily') {
        cron = `${minute} ${hour} * * ?` // 每天
      } else if (taskForm.frequency === 'weekly') {
        cron = `${minute} ${hour} ? * MON` // 每周一
      } else if (taskForm.frequency === 'monthly') {
        cron = `${minute} ${hour} 1 * ?` // 每月1号
      }
    }

    if (!cron) {
      message.warning('请配置检查时间')
      return
    }

    // 为每个选中的策略创建任务
    for (const policyId of taskForm.policy_ids) {
      await tasksApi.create({
        name: taskForm.name + (taskForm.policy_ids.length > 1 ? `-${policies.value.find((p) => p.id === policyId)?.name || ''}` : ''),
        type: 'scheduled',
        targets: {
          type: taskForm.target_type,
          os_family:
            taskForm.target_type === 'os_family' ? taskForm.os_family : undefined,
        },
        policy_id: policyId,
        schedule: {
          cron,
          timezone: taskForm.timezone,
          remark: taskForm.remark,
        },
      })
    }

    message.success(editingTask.value ? '任务已更新' : '任务已创建')
    taskModalVisible.value = false
    resetTaskForm()
    loadScheduledTasks()
  } catch (error: any) {
    if (error?.errorFields) {
      // 表单验证错误
      return
    }
    console.error('保存任务失败:', error)
    message.error('保存任务失败')
  }
}

const handleCancelTask = () => {
  taskModalVisible.value = false
  resetTaskForm()
  editingTask.value = null
}

const getPolicyNames = (task: ScanTask): string[] => {
  const policy = policies.value.find((p) => p.id === task.policy_id)
  return policy ? [policy.name] : [task.policy_id]
}

const getHostScopeText = (task: ScanTask): string => {
  if (task.target_type === 'all') {
    return '全部主机'
  } else if (task.target_type === 'os_family') {
    const osList = task.target_config.os_family || []
    return osList.length > 0 ? osList.join(', ') : '按操作系统'
  }
  return '-'
}

const getCheckTimeText = (task: ScanTask): string => {
  // TODO: 从schedule配置中解析并显示时间
  return '定时执行'
}

const handleViewDetail = (record: Policy) => {
  router.push(`/policies/${record.id}`)
}

const handleRecheck = async (record: Policy) => {
  try {
    await tasksApi.create({
      name: `重新检查-${record.name}`,
      type: 'manual',
      targets: {
        type: 'all',
      },
      policy_id: record.id,
    })
    message.success('重新检查任务已创建')
    await loadPolicyStats()
  } catch (error) {
    console.error('创建重新检查任务失败:', error)
    message.error('创建重新检查任务失败')
  }
}

const handleModalSuccess = () => {
  modalVisible.value = false
  loadPolicies()
}

onMounted(() => {
  // 从 URL 读取 group_id 参数
  const groupIdFromUrl = route.query.group_id as string
  if (groupIdFromUrl) {
    filters.groupId = groupIdFromUrl
  }

  // 加载策略组列表
  loadPolicyGroups()
  // 加载策略列表
  loadPolicies()
})

// 监听路由参数变化
watch(
  () => route.query.group_id,
  (newGroupId) => {
    const groupId = newGroupId as string || ''
    if (filters.groupId !== groupId) {
      filters.groupId = groupId
      loadPolicies()
    }
  }
)
</script>

<style scoped>
.policies-page {
  width: 100%;
}

/* 基线概述卡片 */
.overview-card {
  margin-bottom: 16px;
}

.overview-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.overview-divider {
  width: 1px;
  height: 40px;
  background: #f0f0f0;
  margin: 0 24px;
  flex-shrink: 0;
}

.overview-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.overview-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.overview-label {
  color: #8c8c8c;
  font-size: 14px;
}

.overview-value {
  color: #262626;
  font-size: 14px;
  font-weight: 500;
}

.check-now-btn {
  font-weight: 500;
}

.overview-stats {
  display: flex;
  align-items: center;
  flex: 1;
  justify-content: center;
}

.stat-card {
  text-align: center;
}

.stat-divider {
  width: 1px;
  height: 40px;
  background: #f0f0f0;
  margin: 0 24px;
}

.stat-value {
  font-size: 32px;
  font-weight: 600;
  color: #1890ff;
  line-height: 1;
  margin-bottom: 8px;
}

.stat-label {
  font-size: 14px;
  color: #8c8c8c;
}

.overview-right {
  display: flex;
  align-items: center;
}

.auto-config-btn {
  color: #595959;
  font-size: 14px;
  padding: 0;
}

.auto-config-btn:hover {
  color: #1890ff;
}

/* 基线内容卡片 */
.filter-bar {
  margin-bottom: 16px;
  display: flex;
  gap: 8px;
  align-items: center;
}

.policies-table :deep(.ant-table) {
  background: #fff;
}

.policies-table :deep(.ant-table-thead > tr > th) {
  background: #fafafa;
  font-weight: 600;
  color: #262626;
  border-bottom: 2px solid #f0f0f0;
}

.policies-table :deep(.ant-table-tbody > tr) {
  transition: all 0.2s;
}

.policies-table :deep(.ant-table-tbody > tr:hover) {
  background: #f5f5f5;
}

/* 响应式 */
@media (max-width: 1200px) {
  .overview-content {
    flex-wrap: wrap;
  }

  .overview-stats {
    width: 100%;
    justify-content: space-around;
  }
}

@media (max-width: 768px) {
  .overview-content {
    flex-direction: column;
    align-items: flex-start;
  }

  .overview-stats {
    width: 100%;
    justify-content: space-between;
  }

  .content-header {
    flex-direction: column;
    align-items: stretch;
  }
}

/* 自动检查配置 */
.auto-config-content {
  padding: 0;
}

.auto-config-header {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 16px;
}

.tasks-table :deep(.ant-table) {
  background: #fff;
}

.tasks-table :deep(.ant-table-thead > tr > th) {
  background: #fafafa;
  font-weight: 600;
  color: #262626;
  border-bottom: 2px solid #f0f0f0;
}

.tasks-table :deep(.ant-table-tbody > tr) {
  transition: all 0.2s;
}

.tasks-table :deep(.ant-table-tbody > tr:hover) {
  background: #f5f5f5;
}

/* 操作列样式 */
.action-cell {
  display: inline-flex;
  align-items: center;
  white-space: nowrap;
}

.action-cell :deep(.ant-btn) {
  padding: 0 4px;
}

.group-icon-small {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  color: #fff;
  font-size: 12px;
  font-weight: bold;
  margin-right: 4px;
}
</style>
