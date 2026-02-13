<template>
  <a-layout class="layout">
    <!-- 左侧导航栏（含 Logo） -->
    <a-layout-sider
      v-model:collapsed="collapsed"
      :width="260"
      :collapsed-width="80"
      class="sider"
      theme="dark"
      :trigger="null"
    >
      <div class="sider-wrapper">
        <!-- Logo 区域 -->
        <div class="logo-area">
          <div class="logo-container">
            <img
              v-if="siteConfigStore.siteLogo"
              :src="siteConfigStore.siteLogo"
              alt="Logo"
              class="logo-image"
            />
            <div v-else class="logo-icon">M</div>
            <transition name="fade">
              <div v-if="!collapsed" class="logo-text">
                <div class="logo-title">{{ siteConfigStore.siteName }}</div>
                <div class="logo-version">{{ appVersion }}</div>
              </div>
            </transition>
          </div>
        </div>

        <!-- 菜单 -->
        <div class="sider-content">
          <a-menu
            v-model:selectedKeys="selectedKeys"
            v-model:openKeys="openKeys"
            theme="dark"
            mode="inline"
            :inline-collapsed="collapsed"
            @click="handleMenuClick"
          >
            <a-menu-item key="dashboard" @click.native="(e: MouseEvent) => handleNavClick(e, 'dashboard')">
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
              <a-menu-item key="hosts" @click.native="(e: MouseEvent) => handleNavClick(e, 'hosts')">主机列表</a-menu-item>
              <a-menu-item key="business-lines" @click.native="(e: MouseEvent) => handleNavClick(e, 'business-lines')">业务线管理</a-menu-item>
            </a-sub-menu>
            <a-sub-menu key="baseline-menu">
              <template #icon>
                <SafetyOutlined />
              </template>
              <template #title>基线安全</template>
              <a-menu-item key="policy-groups" @click.native="(e: MouseEvent) => handleNavClick(e, 'policy-groups')">策略组管理</a-menu-item>
              <a-menu-item key="policies" @click.native="(e: MouseEvent) => handleNavClick(e, 'policies')">基线检查</a-menu-item>
              <a-menu-item key="tasks" @click.native="(e: MouseEvent) => handleNavClick(e, 'tasks')">任务执行</a-menu-item>
              <a-menu-item key="baseline-fix" @click.native="(e: MouseEvent) => handleNavClick(e, 'baseline-fix')">基线修复</a-menu-item>
              <a-menu-item key="baseline-fix-history" @click.native="(e: MouseEvent) => handleNavClick(e, 'baseline-fix-history')">修复历史</a-menu-item>
            </a-sub-menu>
            <a-menu-item key="alerts" @click.native="(e: MouseEvent) => handleNavClick(e, 'alerts')">
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
              <a-menu-item key="system-collection" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-collection')">平台授权</a-menu-item>
              <a-menu-item key="system-components" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-components')">组件列表</a-menu-item>
              <a-menu-item key="system-install" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-install')">安装配置</a-menu-item>
              <a-menu-item key="users" @click.native="(e: MouseEvent) => handleNavClick(e, 'users')">用户管理</a-menu-item>
              <a-menu-item key="system-settings" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-settings')">基本设置</a-menu-item>
              <a-menu-item key="system-notification" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-notification')">通知管理</a-menu-item>
              <a-menu-item key="system-reports" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-reports')">报告管理</a-menu-item>
              <a-menu-item key="system-task-report" @click.native="(e: MouseEvent) => handleNavClick(e, 'system-task-report')">任务报告</a-menu-item>
            </a-sub-menu>
          </a-menu>
        </div>

        <!-- 折叠按钮 -->
        <div class="sider-trigger" @click="collapsed = !collapsed">
          <MenuFoldOutlined v-if="!collapsed" />
          <MenuUnfoldOutlined v-else />
        </div>
      </div>
    </a-layout-sider>

    <!-- 右侧区域 -->
    <a-layout :style="{ marginLeft: collapsed ? '80px' : '260px' }" class="right-layout">
      <!-- 顶部栏 -->
      <a-layout-header class="top-header">
        <div class="header-left">
          <!-- 可放面包屑或留空 -->
        </div>
        <div class="header-right">
          <a-dropdown>
            <a class="user-dropdown" @click.prevent>
              <div class="user-avatar">
                <UserOutlined />
              </div>
              <span class="user-name">{{ authStore.user?.username || 'admin' }}</span>
              <DownOutlined class="user-arrow" />
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

      <!-- 内容区 -->
      <a-layout-content class="content">
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
    } else if (name === 'BaselineFixHistory') {
      selectedKeys.value = ['baseline-fix-history']
      openKeys.value = ['baseline-menu']
    } else if (name === 'Users') {
      selectedKeys.value = ['users']
      openKeys.value = ['system-menu']
    } else if (name === 'SystemCollection') {
      selectedKeys.value = ['system-collection']
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
  'baseline-fix-history': '/baseline/fix-history',
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

// 记录是否是 Ctrl/Cmd+Click，用于阻止 handleMenuClick 的路由跳转
let isNewTabClick = false

// Ctrl+Click / Cmd+Click 新开标签页，当前页面保持不动
const handleNavClick = (e: MouseEvent, key: string) => {
  const path = routeMap[key]
  if (!path) return

  if (e.ctrlKey || e.metaKey) {
    isNewTabClick = true
    e.preventDefault()
    e.stopPropagation()
    window.open(router.resolve(path).href, '_blank')
    // 恢复当前选中状态
    const currentKey = Object.entries(routeMap).find(([, v]) => v === route.path)?.[0]
    if (currentKey) {
      selectedKeys.value = [currentKey]
    }
  }
}

const handleMenuClick = ({ key }: { key: string }) => {
  if (isNewTabClick) {
    isNewTabClick = false
    return
  }
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
      // 表单验证错误
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

/* ========== 左侧导航栏 ========== */
.sider {
  position: fixed !important;
  left: 0 !important;
  top: 0 !important;
  height: 100vh !important;
  max-height: 100vh !important;
  overflow: hidden !important;
  z-index: 1001;
  background: #001529 !important;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.15);
}

.sider :deep(.ant-layout-sider-children) {
  display: flex !important;
  flex-direction: column !important;
  height: 100% !important;
  overflow: hidden !important;
}

.sider-wrapper {
  display: flex !important;
  flex-direction: column !important;
  height: 100% !important;
  overflow: hidden !important;
}

/* Logo 区域 */
.logo-area {
  height: 64px;
  min-height: 64px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  flex-shrink: 0;
}

.logo-container {
  display: flex;
  align-items: center;
  overflow: hidden;
}

.logo-icon {
  width: 40px;
  height: 40px;
  background: linear-gradient(135deg, #1890ff 0%, #096dd9 100%);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: bold;
  font-size: 20px;
  flex-shrink: 0;
}

.logo-image {
  width: 40px;
  height: 40px;
  object-fit: contain;
  flex-shrink: 0;
}

.logo-text {
  margin-left: 12px;
  overflow: hidden;
  white-space: nowrap;
}

.logo-title {
  font-size: 16px;
  font-weight: 600;
  color: #ffffff;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
}

.logo-version {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.35);
  line-height: 1.4;
}

/* Logo 文字淡入淡出 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* 菜单滚动区域 */
.sider-content {
  flex: 1 1 auto;
  overflow-y: auto !important;
  overflow-x: hidden !important;
  height: 0;
  min-height: 0;
  padding-top: 4px;
}

.sider-content::-webkit-scrollbar {
  width: 4px;
}

.sider-content::-webkit-scrollbar-track {
  background: transparent;
}

.sider-content::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.15);
  border-radius: 2px;
}

.sider-content::-webkit-scrollbar-thumb:hover {
  background: rgba(255, 255, 255, 0.25);
}

/* 折叠按钮 */
.sider-trigger {
  height: 48px;
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  transition: all 0.2s;
  flex-shrink: 0;
  background-color: #001529;
  color: rgba(255, 255, 255, 0.5);
  font-size: 16px;
}

.sider-trigger:hover {
  background-color: rgba(255, 255, 255, 0.04);
  color: rgba(255, 255, 255, 0.85);
}

/* 菜单样式 */
.sider :deep(.ant-menu) {
  border-right: none;
  background: #001529;
  padding: 4px 0;
}

.sider :deep(.ant-menu-item),
.sider :deep(.ant-menu-submenu-title) {
  margin: 4px 12px !important;
  border-radius: 8px;
  height: 44px !important;
  line-height: 44px !important;
}

.sider :deep(.ant-menu-submenu .ant-menu-item) {
  height: 40px !important;
  line-height: 40px !important;
  margin: 2px 12px !important;
}

.sider :deep(.ant-menu-item-selected) {
  background: #1890ff !important;
  color: #ffffff !important;
}

.sider :deep(.ant-menu-item-selected::after) {
  display: none;
}

.sider :deep(.ant-menu-item:not(.ant-menu-item-selected):hover),
.sider :deep(.ant-menu-submenu-title:hover) {
  background-color: rgba(255, 255, 255, 0.06);
}

.sider :deep(.ant-menu-sub.ant-menu-inline) {
  background: rgba(0, 0, 0, 0.12) !important;
}

/* ========== 右侧区域 ========== */
.right-layout {
  transition: margin-left 0.2s;
}

/* 顶部栏 */
.top-header {
  background: #ffffff;
  padding: 0 24px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.06);
  position: sticky;
  top: 0;
  z-index: 999;
}

.header-left {
  flex: 1;
}

.header-right {
  display: flex;
  align-items: center;
}

.user-dropdown {
  color: rgba(0, 0, 0, 0.65);
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 6px 12px;
  border-radius: 6px;
  transition: all 0.3s;
}

.user-dropdown:hover {
  background-color: #f5f5f5;
  color: rgba(0, 0, 0, 0.85);
}

.user-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, #1890ff, #096dd9);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-right: 8px;
  font-size: 14px;
  color: #fff;
}

.user-name {
  margin-right: 6px;
  font-size: 14px;
}

.user-arrow {
  font-size: 10px;
  opacity: 0.45;
}

/* ========== 内容区 ========== */
.content {
  padding: 24px;
  background: #f0f2f5;
  min-height: calc(100vh - 64px);
}

.content-wrapper {
  background: #fff;
  padding: 24px;
  min-height: calc(100vh - 112px);
  border-radius: 8px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03), 0 2px 4px rgba(0, 0, 0, 0.04);
}
</style>
