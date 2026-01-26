<template>
  <a-layout class="layout">
    <!-- 顶部栏 -->
    <a-layout-header class="top-header">
      <div class="header-left">
        <div class="logo-container">
          <img
            v-if="siteConfigStore.siteLogo"
            :src="siteConfigStore.siteLogo"
            alt="Logo"
            class="logo-image"
          />
          <div v-else class="logo-icon">M</div>
          <div class="logo-text">
            <div class="logo-title">{{ siteConfigStore.siteName }}</div>
            <div class="logo-version">{{ appVersion }}</div>
          </div>
        </div>
      </div>
      <div class="header-right">
        <a-dropdown>
          <a class="user-dropdown" @click.prevent>
            <UserOutlined style="margin-right: 8px" />
            <span style="margin-right: 8px">{{ authStore.user?.username || 'admin' }}</span>
            <DownOutlined />
          </a>
          <template #overlay>
            <a-menu>
              <a-menu-item @click="showChangePasswordModal = true">
                <KeyOutlined />
                修改密码
              </a-menu-item>
              <a-menu-divider />
              <a-menu-item @click="handleLogout">
                <LogoutOutlined />
                退出登录
              </a-menu-item>
            </a-menu>
          </template>
        </a-dropdown>
      </div>
    </a-layout-header>

    <!-- 左侧导航栏 -->
    <a-layout>
      <a-layout-sider
        v-model:collapsed="collapsed"
        :width="240"
        :collapsed-width="80"
        class="sider"
        theme="light"
        :trigger="null"
      >
        <div class="sider-wrapper">
          <div class="sider-content">
            <a-menu
              v-model:selectedKeys="selectedKeys"
              v-model:openKeys="openKeys"
              theme="light"
              mode="inline"
              :inline-collapsed="collapsed"
              @click="handleMenuClick"
              @mousedown="handleMenuMouseDown"
            >
              <a-menu-item key="dashboard">
                <template #icon>
                  <DashboardOutlined />
                </template>
                <span>安全概览</span>
              </a-menu-item>
              <a-sub-menu key="assets-menu">
                <template #icon>
                  <DatabaseOutlined />
                </template>
                <template #title>资产中心</template>
                <a-menu-item key="hosts">主机列表</a-menu-item>
                <a-menu-item key="business-lines">业务线管理</a-menu-item>
              </a-sub-menu>
              <a-sub-menu key="baseline-menu">
                <template #icon>
                  <SafetyOutlined />
                </template>
                <template #title>基线安全</template>
                <a-menu-item key="policy-groups">策略组管理</a-menu-item>
                <a-menu-item key="policies">基线检查</a-menu-item>
                <a-menu-item key="tasks">任务执行</a-menu-item>
                <a-menu-item key="baseline-fix">基线修复</a-menu-item>
              </a-sub-menu>
              <a-menu-item key="alerts">
                <template #icon>
                  <BellOutlined />
                </template>
                <span>告警管理</span>
              </a-menu-item>
              <a-sub-menu key="system-menu">
                <template #icon>
                  <SettingOutlined />
                </template>
                <template #title>系统管理</template>
                <a-menu-item key="system-collection">平台授权</a-menu-item>
                <a-menu-item key="system-components">组件列表</a-menu-item>
                <a-menu-item key="system-install">安装配置</a-menu-item>
                <a-menu-item key="users">用户管理</a-menu-item>
                <a-menu-item key="system-settings">基本设置</a-menu-item>
                <a-menu-item key="system-notification">通知管理</a-menu-item>
                <a-menu-item key="system-reports">报告管理</a-menu-item>
                <a-menu-item key="system-task-report">任务报告</a-menu-item>
              </a-sub-menu>
            </a-menu>
          </div>
          <div class="sider-trigger" @click="collapsed = !collapsed">
            <MenuFoldOutlined v-if="!collapsed" />
            <MenuUnfoldOutlined v-else />
          </div>
        </div>
      </a-layout-sider>

      <!-- 内容区 -->
      <a-layout-content class="content" :style="{ marginLeft: collapsed ? '80px' : '240px' }">
        <div class="content-wrapper">
          <router-view />
        </div>
      </a-layout-content>
    </a-layout>

    <!-- 修改密码 Modal -->
    <a-modal
      v-model:open="showChangePasswordModal"
      title="修改密码"
      :confirm-loading="changePasswordLoading"
      @ok="handleChangePassword"
      @cancel="resetChangePasswordForm"
    >
      <a-form :model="changePasswordForm" :rules="changePasswordRules" ref="changePasswordFormRef">
        <a-form-item label="旧密码" name="oldPassword">
          <a-input-password v-model:value="changePasswordForm.oldPassword" placeholder="请输入旧密码" />
        </a-form-item>
        <a-form-item label="新密码" name="newPassword">
          <a-input-password v-model:value="changePasswordForm.newPassword" placeholder="请输入新密码（至少6位）" />
        </a-form-item>
        <a-form-item label="确认新密码" name="confirmPassword">
          <a-input-password v-model:value="changePasswordForm.confirmPassword" placeholder="请再次输入新密码" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-layout>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  DashboardOutlined,
  DatabaseOutlined,
  SafetyOutlined,
  SettingOutlined,
  UserOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  DownOutlined,
  LogoutOutlined,
  KeyOutlined,
  BellOutlined,
} from '@ant-design/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useSiteConfigStore } from '@/stores/site-config'
import { authApi } from '@/api/auth'
import apiClient from '@/api/client'
import { message } from 'ant-design-vue'
import type { FormInstance } from 'ant-design-vue'
import { onMounted } from 'vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const siteConfigStore = useSiteConfigStore()

// 应用版本号
const appVersion = ref('--')

// 获取应用版本
const fetchAppVersion = async () => {
  try {
    const response = await apiClient.get<{ version: string; status: string }>('/health')
    appVersion.value = response.version || '--'
  } catch {
    appVersion.value = '--'
  }
}

// 初始化站点配置
onMounted(() => {
  siteConfigStore.init()
  fetchAppVersion()
})

const collapsed = ref(false)
const selectedKeys = ref<string[]>([])
const openKeys = ref<string[]>(['baseline-menu'])
const showChangePasswordModal = ref(false)
const changePasswordLoading = ref(false)
const changePasswordFormRef = ref<FormInstance>()
const changePasswordForm = ref({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

// 修改密码表单验证规则
const validateConfirmPassword = async (_rule: any, value: string) => {
  if (!value) {
    return Promise.reject('请确认新密码')
  }
  if (value !== changePasswordForm.value.newPassword) {
    return Promise.reject('两次输入的密码不一致')
  }
  return Promise.resolve()
}

const changePasswordRules = {
  oldPassword: [{ required: true, message: '请输入旧密码', trigger: 'blur' }],
  newPassword: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 6, message: '密码长度至少6位', trigger: 'blur' },
  ],
  confirmPassword: [{ required: true, validator: validateConfirmPassword, trigger: 'change' }],
}

// 根据路由更新选中菜单
watch(
  () => route.name,
  (name) => {
    if (name === 'Dashboard') {
      selectedKeys.value = ['dashboard']
      openKeys.value = []
    } else if (name === 'Hosts' || name === 'HostDetail') {
      selectedKeys.value = ['hosts']
      openKeys.value = ['assets-menu']
    } else if (name === 'BusinessLines') {
      selectedKeys.value = ['business-lines']
      openKeys.value = ['assets-menu']
    } else if (name === 'PolicyGroups') {
      selectedKeys.value = ['policy-groups']
      openKeys.value = ['baseline-menu']
    } else if (name === 'Policies' || name === 'PolicyDetail') {
      selectedKeys.value = ['policies']
      openKeys.value = ['baseline-menu']
    } else if (name === 'Tasks') {
      selectedKeys.value = ['tasks']
      openKeys.value = ['baseline-menu']
    } else if (name === 'BaselineFix') {
      selectedKeys.value = ['baseline-fix']
      openKeys.value = ['baseline-menu']
    } else if (name === 'Users') {
      selectedKeys.value = ['users']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemCollection') {
      selectedKeys.value = ['system-collection']
      openKeys.value = ['system-menu']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemSettings') {
      selectedKeys.value = ['system-settings']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemNotification') {
      selectedKeys.value = ['system-notification']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemComponents') {
      selectedKeys.value = ['system-components']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemInstall') {
      selectedKeys.value = ['system-install']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemReports') {
      selectedKeys.value = ['system-reports']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemTaskReport') {
      selectedKeys.value = ['system-task-report']
      openKeys.value = ['system-menu']
    } else if (name === 'Alerts') {
      selectedKeys.value = ['alerts']
      openKeys.value = []
    }
  },
  { immediate: true }
)

// 路由映射表
const routeMap: Record<string, string> = {
  'dashboard': '/dashboard',
  'hosts': '/hosts',
  'business-lines': '/business-lines',
  'policy-groups': '/policy-groups',
  'policies': '/policies',
  'tasks': '/tasks',
  'baseline-fix': '/baseline/fix',
  'users': '/users',
  'system-collection': '/system/collection',
  'system-settings': '/system/settings',
  'system-notification': '/system/notification',
  'system-components': '/system/components',
  'system-install': '/system/install',
  'system-reports': '/system/reports',
  'system-task-report': '/system/task-report',
  'alerts': '/alerts',
}

// 处理菜单鼠标按下事件（支持 Ctrl/Cmd+Click 新标签打开）
const handleMenuMouseDown = (e: MouseEvent) => {
  // 检测是否按下 Ctrl（Windows/Linux）或 Cmd（Mac）键
  if (e.ctrlKey || e.metaKey) {
    // 找到点击的菜单项
    const target = e.target as HTMLElement
    const menuItem = target.closest('.ant-menu-item')
    if (menuItem) {
      const key = menuItem.getAttribute('data-menu-id')
      if (key && routeMap[key]) {
        e.preventDefault()
        // 在新标签页打开
        const url = window.location.origin + routeMap[key]
        window.open(url, '_blank')
      }
    }
  }
}

const handleMenuClick = ({ key }: { key: string }) => {
  const path = routeMap[key]
  if (path) {
    router.push(path)
  }
}

const handleLogout = async () => {
  await authStore.logout()
  message.success('已退出登录')
  router.push('/login')
}

const handleChangePassword = async () => {
  try {
    await changePasswordFormRef.value?.validate()
    changePasswordLoading.value = true
    await authApi.changePassword({
      old_password: changePasswordForm.value.oldPassword,
      new_password: changePasswordForm.value.newPassword,
    })
    message.success('密码修改成功')
    showChangePasswordModal.value = false
    resetChangePasswordForm()
  } catch (error: any) {
    if (error?.response?.data?.message) {
      message.error(error.response.data.message)
    } else if (error?.errorFields) {
      // 表单验证错误，不显示消息
    } else {
      message.error('密码修改失败')
    }
  } finally {
    changePasswordLoading.value = false
  }
}

const resetChangePasswordForm = () => {
  changePasswordForm.value = {
    oldPassword: '',
    newPassword: '',
    confirmPassword: '',
  }
  changePasswordFormRef.value?.resetFields()
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
}

/* 顶部栏 */
.top-header {
  background: #fff;
  padding: 0 24px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 1000;
}

.header-left {
  display: flex;
  align-items: center;
}

.logo-container {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-icon {
  width: 32px;
  height: 32px;
  background: linear-gradient(135deg, #1890ff 0%, #096dd9 100%);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: bold;
  font-size: 18px;
  margin-right: 12px;
  flex-shrink: 0;
}

.logo-image {
  width: 32px;
  height: 32px;
  object-fit: contain;
  margin-right: 12px;
  flex-shrink: 0;
}

.logo-text {
  display: flex;
  flex-direction: column;
}

.logo-title {
  font-size: 16px;
  font-weight: 600;
  color: rgba(0, 0, 0, 0.85);
  line-height: 1.2;
}

.logo-version {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.45);
  line-height: 1.2;
}

.header-right {
  display: flex;
  align-items: center;
}

.user-dropdown {
  color: rgba(0, 0, 0, 0.85);
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 4px 8px;
  border-radius: 4px;
  transition: background-color 0.3s;
}

.user-dropdown:hover {
  background-color: rgba(0, 0, 0, 0.04);
}

/* 左侧导航栏 */
.sider {
  display: flex !important;
  flex-direction: column !important;
  position: fixed !important;
  left: 0 !important;
  top: 64px !important;
  height: calc(100vh - 64px) !important;
  max-height: calc(100vh - 64px) !important;
  border-right: 1px solid #f0f0f0;
  overflow: hidden !important;
  z-index: 999;
}

/* 覆盖 Ant Design 的默认样式 */
.sider :deep(.ant-layout-sider) {
  height: 100% !important;
  max-height: 100% !important;
  overflow: hidden !important;
  display: flex !important;
  flex-direction: column !important;
}

/* 确保 Ant Design sider 内部容器也使用 flex */
.sider :deep(.ant-layout-sider-children) {
  display: flex !important;
  flex-direction: column !important;
  height: 100% !important;
  overflow: hidden !important;
}

/* 包装容器 */
.sider-wrapper {
  display: flex !important;
  flex-direction: column !important;
  height: 100% !important;
  max-height: 100% !important;
  overflow: hidden !important;
}

.sider-content {
  flex: 1 1 auto;
  overflow-y: auto !important;
  overflow-x: hidden !important;
  height: 0; /* 配合 flex: 1 使用，确保可以正确计算高度 */
  min-height: 0; /* 确保可以收缩 */
  /* 确保滚动条始终可见且可用 */
  -webkit-overflow-scrolling: touch;
  scrollbar-width: thin;
  position: relative;
}

/* 自定义滚动条样式（Webkit浏览器） */
.sider-content::-webkit-scrollbar {
  width: 6px;
}

.sider-content::-webkit-scrollbar-track {
  background: #f1f1f1;
}

.sider-content::-webkit-scrollbar-thumb {
  background: #c1c1c1;
  border-radius: 3px;
}

.sider-content::-webkit-scrollbar-thumb:hover {
  background: #a8a8a8;
}

.sider-trigger {
  height: 48px;
  min-height: 48px;
  max-height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  border-top: 1px solid #f0f0f0;
  transition: background-color 0.3s;
  flex-shrink: 0;
  flex-grow: 0;
  position: relative;
  z-index: 10;
  background-color: #fff;
  width: 100%;
}

.sider-trigger:hover {
  background-color: rgba(0, 0, 0, 0.02);
}

.content {
  margin-top: 64px;
  padding: 24px;
  background: #f0f2f5;
  min-height: calc(100vh - 64px);
  transition: margin-left 0.2s;
}

.content-wrapper {
  background: #fff;
  padding: 24px;
  min-height: calc(100vh - 112px);
  border-radius: 4px;
}

/* 菜单样式优化 */
.sider :deep(.ant-menu) {
  border-right: none;
  height: auto !important;
  max-height: none !important;
  overflow: visible !important;
}

/* 确保菜单容器不会限制滚动 */
.sider :deep(.ant-menu-root) {
  height: auto !important;
  overflow: visible !important;
}

.sider :deep(.ant-menu-item),
.sider :deep(.ant-menu-submenu-title) {
  margin: 4px 8px;
  border-radius: 4px;
}

.sider :deep(.ant-menu-item-selected) {
  background-color: #e6f7ff;
  color: #1890ff;
}

.sider :deep(.ant-menu-item-selected::after) {
  display: none;
}

.sider :deep(.ant-menu-item:hover),
.sider :deep(.ant-menu-submenu-title:hover) {
  background-color: rgba(0, 0, 0, 0.02);
}
</style>
