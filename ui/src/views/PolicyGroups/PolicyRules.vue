<template>
  <div class="policy-rules-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-left">
        <a-button type="text" @click="handleBack">
          <LeftOutlined /> 返回
        </a-button>
        <a-divider type="vertical" />
        <h2 class="page-title">{{ policy?.name || '规则管理' }}</h2>
        <a-tag v-if="!policy?.enabled" color="default" style="margin-left: 8px;">已禁用</a-tag>
      </div>
      <div class="header-right">
        <a-button type="primary" @click="handleCreateRule">
          <template #icon>
            <PlusOutlined />
          </template>
          新建规则
        </a-button>
      </div>
    </div>

    <!-- 策略信息 -->
    <div class="policy-info-card" v-if="policy">
      <a-descriptions :column="4" size="small">
        <a-descriptions-item label="策略ID">{{ policy.id }}</a-descriptions-item>
        <a-descriptions-item label="版本">{{ policy.version || '-' }}</a-descriptions-item>
        <a-descriptions-item label="适用系统">
          <a-tag v-for="os in policy.os_family" :key="os" style="margin-right: 4px;">{{ os }}</a-tag>
          <span v-if="!policy.os_family || policy.os_family.length === 0">-</span>
        </a-descriptions-item>
        <a-descriptions-item label="检查项数量">{{ rules.length }}</a-descriptions-item>
      </a-descriptions>
      <p v-if="policy.description" class="policy-description">{{ policy.description }}</p>
    </div>

    <!-- 规则列表 -->
    <a-card title="检查规则列表" :bordered="false">
      <template #extra>
        <a-space>
          <a-input-search
            v-model:value="searchKeyword"
            placeholder="搜索规则名称"
            style="width: 200px"
            allow-clear
          />
          <a-button @click="loadPolicyDetail">
            <template #icon>
              <ReloadOutlined />
            </template>
          </a-button>
        </a-space>
      </template>

      <a-table
        :columns="ruleColumns"
        :data-source="filteredRules"
        :loading="loading"
        row-key="rule_id"
        :pagination="{ pageSize: 15, showSizeChanger: true, showTotal: (total: number) => `共 ${total} 条` }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'title'">
            <div>
              <span style="font-weight: 500;">{{ record.title }}</span>
            </div>
            <div style="font-size: 12px; color: #999;">{{ record.rule_id }}</div>
          </template>
          <template v-else-if="column.key === 'category'">
            <a-tag>{{ getCategoryText(record.category) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'severity'">
            <a-tag :color="getSeverityColor(record.severity)">
              {{ getSeverityText(record.severity) }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'description'">
            <a-tooltip :title="record.description" placement="topLeft">
              <span class="ellipsis-text">{{ record.description || '-' }}</span>
            </a-tooltip>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button type="link" size="small" @click="handleEditRule(record)">
                编辑
              </a-button>
              <a-popconfirm
                title="确定要删除此规则吗？"
                ok-text="删除"
                cancel-text="取消"
                @confirm="handleDeleteRule(record)"
              >
                <a-button type="link" size="small" danger>删除</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>

        <template #emptyText>
          <a-empty description="暂无检查规则">
            <a-button type="primary" @click="handleCreateRule">创建规则</a-button>
          </a-empty>
        </template>
      </a-table>
    </a-card>

    <!-- 规则编辑对话框 -->
    <RuleModal
      v-model:visible="ruleModalVisible"
      :rule="editingRule"
      :policy-id="policyId"
      @success="handleRuleModalSuccess"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { LeftOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { policiesApi } from '@/api/policies'
import { rulesApi } from '@/api/rules'
import type { Policy, Rule } from '@/api/types'
import { message } from 'ant-design-vue'
import RuleModal from '@/views/Policies/components/RuleModal.vue'

const router = useRouter()
const route = useRoute()

const policyId = computed(() => route.params.policyId as string)
const loading = ref(false)
const policy = ref<Policy | null>(null)
const rules = ref<Rule[]>([])
const searchKeyword = ref('')
const ruleModalVisible = ref(false)
const editingRule = ref<Rule | null>(null)

const ruleColumns = [
  {
    title: '规则名称',
    key: 'title',
    ellipsis: true,
  },
  {
    title: '分类',
    key: 'category',
    width: 120,
  },
  {
    title: '级别',
    key: 'severity',
    width: 90,
  },
  {
    title: '描述',
    key: 'description',
    ellipsis: true,
    width: 300,
  },
  {
    title: '操作',
    key: 'action',
    width: 150,
    fixed: 'right' as const,
  },
]

const filteredRules = computed(() => {
  if (!searchKeyword.value) return rules.value
  return rules.value.filter((rule) =>
    rule.title.toLowerCase().includes(searchKeyword.value.toLowerCase()) ||
    rule.rule_id.toLowerCase().includes(searchKeyword.value.toLowerCase())
  )
})

const loadPolicyDetail = async () => {
  if (!policyId.value) return

  loading.value = true
  try {
    const data = await policiesApi.get(policyId.value) as unknown as Policy
    policy.value = data
    rules.value = data.rules || []
  } catch (error) {
    console.error('加载策略详情失败:', error)
    message.error('加载策略详情失败')
  } finally {
    loading.value = false
  }
}

const handleBack = () => {
  router.push('/policy-groups')
}

const handleCreateRule = () => {
  editingRule.value = null
  ruleModalVisible.value = true
}

const handleEditRule = (rule: Rule) => {
  editingRule.value = rule
  ruleModalVisible.value = true
}

const handleDeleteRule = async (rule: Rule) => {
  try {
    await rulesApi.delete(rule.rule_id)
    message.success('规则已删除')
    loadPolicyDetail()
  } catch (error) {
    console.error('删除规则失败:', error)
    message.error('删除规则失败')
  }
}

const handleRuleModalSuccess = () => {
  ruleModalVisible.value = false
  loadPolicyDetail()
}

const getSeverityColor = (severity: string) => {
  const colors: Record<string, string> = {
    critical: 'red',
    high: 'orange',
    medium: 'gold',
    low: 'blue',
  }
  return colors[severity] || 'default'
}

const getSeverityText = (severity: string) => {
  const texts: Record<string, string> = {
    critical: '严重',
    high: '高危',
    medium: '中危',
    low: '低危',
  }
  return texts[severity] || severity
}

const getCategoryText = (category: string) => {
  const texts: Record<string, string> = {
    ssh: 'SSH 配置',
    password: '密码策略',
    account: '账户安全',
    file: '文件权限',
    kernel: '内核参数',
    service: '服务状态',
    audit: '审计日志',
    network: '网络安全',
    other: '其他',
  }
  return texts[category] || category || '-'
}

onMounted(() => {
  loadPolicyDetail()
})
</script>

<style scoped>
.policy-rules-page {
  width: 100%;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
  padding-bottom: 16px;
  border-bottom: 1px solid #f0f0f0;
}

.header-left {
  display: flex;
  align-items: center;
}

.page-title {
  font-size: 20px;
  font-weight: 600;
  margin: 0 0 0 8px;
  color: #262626;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.policy-info-card {
  background: #fff;
  border-radius: 8px;
  padding: 16px 20px;
  margin-bottom: 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
}

.policy-description {
  margin: 12px 0 0 0;
  color: #666;
  font-size: 14px;
}

.ellipsis-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 280px;
}
</style>
