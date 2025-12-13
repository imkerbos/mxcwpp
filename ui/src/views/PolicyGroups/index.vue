<template>
  <div class="policy-groups-page">
    <div class="page-header">
      <h2>策略组管理</h2>
      <a-button type="primary" @click="handleCreate">
        <template #icon>
          <PlusOutlined />
        </template>
        新建策略组
      </a-button>
    </div>

    <!-- 策略组列表 -->
    <a-spin :spinning="loading">
      <div class="groups-grid">
        <a-card
          v-for="group in policyGroups"
          :key="group.id"
          class="group-card"
          :class="{ disabled: !group.enabled }"
          hoverable
        >
          <template #title>
            <div class="card-title">
              <span
                class="group-icon"
                :style="{ backgroundColor: group.color || '#1890ff' }"
              >
                {{ group.icon || group.name.charAt(0) }}
              </span>
              <span class="group-name">{{ group.name }}</span>
              <a-tag v-if="!group.enabled" color="default">已禁用</a-tag>
            </div>
          </template>
          <template #extra>
            <a-dropdown>
              <a-button type="text" size="small">
                <MoreOutlined />
              </a-button>
              <template #overlay>
                <a-menu @click="({ key }) => handleMenuClick(key, group)">
                  <a-menu-item key="edit">
                    <EditOutlined /> 编辑
                  </a-menu-item>
                  <a-menu-item key="toggle">
                    <template v-if="group.enabled">
                      <StopOutlined /> 禁用
                    </template>
                    <template v-else>
                      <CheckOutlined /> 启用
                    </template>
                  </a-menu-item>
                  <a-menu-divider />
                  <a-menu-item key="delete" danger>
                    <DeleteOutlined /> 删除
                  </a-menu-item>
                </a-menu>
              </template>
            </a-dropdown>
          </template>

          <p class="group-description">{{ group.description || '暂无描述' }}</p>

          <div class="group-stats">
            <a-row :gutter="16">
              <a-col :span="8">
                <a-statistic title="策略数" :value="group.policy_count || 0" />
              </a-col>
              <a-col :span="8">
                <a-statistic title="检查项" :value="group.rule_count || 0" />
              </a-col>
              <a-col :span="8">
                <a-statistic
                  title="通过率"
                  :value="group.pass_rate || 0"
                  :precision="1"
                  suffix="%"
                  :value-style="{ color: getPassRateColor(group.pass_rate || 0) }"
                />
              </a-col>
            </a-row>
          </div>

          <div class="group-footer">
            <span class="host-count">
              <DesktopOutlined /> 检查主机: {{ group.host_count || 0 }}
            </span>
            <a-button type="link" size="small" @click="handleViewPolicies(group)">
              查看策略 <RightOutlined />
            </a-button>
          </div>
        </a-card>

        <!-- 空状态 -->
        <a-empty
          v-if="policyGroups.length === 0 && !loading"
          description="暂无策略组"
          class="empty-state"
        >
          <a-button type="primary" @click="handleCreate">创建策略组</a-button>
        </a-empty>
      </div>
    </a-spin>

    <!-- 创建/编辑策略组对话框 -->
    <a-modal
      v-model:open="modalVisible"
      :title="editingGroup ? '编辑策略组' : '新建策略组'"
      @ok="handleModalOk"
      @cancel="handleModalCancel"
      :confirm-loading="modalLoading"
    >
      <a-form
        ref="formRef"
        :model="formState"
        :rules="formRules"
        :label-col="{ span: 6 }"
        :wrapper-col="{ span: 18 }"
      >
        <a-form-item label="策略组ID" name="id" v-if="!editingGroup">
          <a-input
            v-model:value="formState.id"
            placeholder="留空自动生成"
          />
        </a-form-item>
        <a-form-item label="策略组名称" name="name">
          <a-input
            v-model:value="formState.name"
            placeholder="例如：系统基线组、应用基线组"
          />
        </a-form-item>
        <a-form-item label="描述" name="description">
          <a-textarea
            v-model:value="formState.description"
            placeholder="策略组描述"
            :rows="3"
          />
        </a-form-item>
        <a-form-item label="图标" name="icon">
          <a-input
            v-model:value="formState.icon"
            placeholder="单个字符或 emoji"
            :maxlength="2"
            style="width: 100px"
          />
        </a-form-item>
        <a-form-item label="颜色" name="color">
          <a-input
            v-model:value="formState.color"
            type="color"
            style="width: 100px; padding: 0"
          />
        </a-form-item>
        <a-form-item label="排序" name="sort_order">
          <a-input-number
            v-model:value="formState.sort_order"
            :min="0"
            :max="999"
          />
        </a-form-item>
        <a-form-item label="启用状态" name="enabled">
          <a-switch v-model:checked="formState.enabled" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { message, Modal } from 'ant-design-vue'
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  MoreOutlined,
  RightOutlined,
  DesktopOutlined,
  StopOutlined,
  CheckOutlined,
} from '@ant-design/icons-vue'
import { policyGroupsApi } from '@/api/policy-groups'
import type { PolicyGroup } from '@/api/types'
import type { FormInstance } from 'ant-design-vue'

const router = useRouter()

const loading = ref(false)
const policyGroups = ref<PolicyGroup[]>([])
const modalVisible = ref(false)
const modalLoading = ref(false)
const editingGroup = ref<PolicyGroup | null>(null)
const formRef = ref<FormInstance>()

const formState = reactive({
  id: '',
  name: '',
  description: '',
  icon: '',
  color: '#1890ff',
  sort_order: 0,
  enabled: true,
})

const formRules = {
  name: [{ required: true, message: '请输入策略组名称', trigger: 'blur' }],
}

// 加载策略组列表
const loadPolicyGroups = async () => {
  loading.value = true
  try {
    const response = await policyGroupsApi.list() as any
    policyGroups.value = response.data?.items || response.items || []
  } catch (error) {
    console.error('加载策略组失败:', error)
    message.error('加载策略组失败')
  } finally {
    loading.value = false
  }
}

// 获取通过率颜色
const getPassRateColor = (rate: number) => {
  if (rate >= 80) return '#52c41a'
  if (rate >= 60) return '#faad14'
  return '#f5222d'
}

// 创建策略组
const handleCreate = () => {
  editingGroup.value = null
  resetForm()
  modalVisible.value = true
}

// 编辑策略组
const handleEdit = (group: PolicyGroup) => {
  editingGroup.value = group
  formState.id = group.id
  formState.name = group.name
  formState.description = group.description || ''
  formState.icon = group.icon || ''
  formState.color = group.color || '#1890ff'
  formState.sort_order = group.sort_order || 0
  formState.enabled = group.enabled
  modalVisible.value = true
}

// 菜单点击
const handleMenuClick = async (key: string, group: PolicyGroup) => {
  if (key === 'edit') {
    handleEdit(group)
  } else if (key === 'toggle') {
    await handleToggle(group)
  } else if (key === 'delete') {
    handleDelete(group)
  }
}

// 切换启用状态
const handleToggle = async (group: PolicyGroup) => {
  try {
    await policyGroupsApi.update(group.id, { enabled: !group.enabled })
    message.success(group.enabled ? '已禁用策略组' : '已启用策略组')
    loadPolicyGroups()
  } catch (error) {
    console.error('更新策略组失败:', error)
    message.error('更新策略组失败')
  }
}

// 删除策略组
const handleDelete = (group: PolicyGroup) => {
  Modal.confirm({
    title: '确认删除',
    content: `确定要删除策略组「${group.name}」吗？删除后无法恢复。`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      try {
        await policyGroupsApi.delete(group.id)
        message.success('删除成功')
        loadPolicyGroups()
      } catch (error: any) {
        console.error('删除策略组失败:', error)
        if (error.response?.status === 409) {
          message.error('策略组下存在策略，无法删除')
        } else {
          message.error('删除策略组失败')
        }
      }
    },
  })
}

// 查看策略组下的策略
const handleViewPolicies = (group: PolicyGroup) => {
  router.push({ path: '/policies', query: { group_id: group.id } })
}

// 提交表单
const handleModalOk = async () => {
  try {
    await formRef.value?.validate()
    modalLoading.value = true

    if (editingGroup.value) {
      await policyGroupsApi.update(editingGroup.value.id, {
        name: formState.name,
        description: formState.description,
        icon: formState.icon,
        color: formState.color,
        sort_order: formState.sort_order,
        enabled: formState.enabled,
      })
      message.success('更新成功')
    } else {
      await policyGroupsApi.create({
        id: formState.id || undefined,
        name: formState.name,
        description: formState.description,
        icon: formState.icon,
        color: formState.color,
        sort_order: formState.sort_order,
        enabled: formState.enabled,
      })
      message.success('创建成功')
    }

    modalVisible.value = false
    loadPolicyGroups()
  } catch (error: any) {
    if (error?.errorFields) {
      return
    }
    console.error('保存策略组失败:', error)
    if (error.response?.status === 409) {
      message.error('策略组 ID 已存在')
    } else {
      message.error('保存策略组失败')
    }
  } finally {
    modalLoading.value = false
  }
}

// 取消表单
const handleModalCancel = () => {
  modalVisible.value = false
  resetForm()
}

// 重置表单
const resetForm = () => {
  formState.id = ''
  formState.name = ''
  formState.description = ''
  formState.icon = ''
  formState.color = '#1890ff'
  formState.sort_order = 0
  formState.enabled = true
  formRef.value?.resetFields()
}

onMounted(() => {
  loadPolicyGroups()
})
</script>

<style scoped>
.policy-groups-page {
  width: 100%;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
}

.groups-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 16px;
}

.group-card {
  transition: all 0.3s;
}

.group-card.disabled {
  opacity: 0.6;
}

.group-card:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.group-icon {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
}

.group-name {
  font-weight: 500;
  font-size: 16px;
}

.group-description {
  color: rgba(0, 0, 0, 0.45);
  margin-bottom: 16px;
  min-height: 44px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.group-stats {
  margin-bottom: 16px;
  padding: 16px;
  background: #fafafa;
  border-radius: 8px;
}

.group-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 12px;
  border-top: 1px solid #f0f0f0;
}

.host-count {
  color: rgba(0, 0, 0, 0.45);
  font-size: 13px;
}

.empty-state {
  grid-column: 1 / -1;
  padding: 60px 0;
}
</style>
