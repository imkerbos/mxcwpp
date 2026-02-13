import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import Layout from '@/layouts/BasicLayout.vue'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '登录', public: true },
  },
  {
    path: '/',
    component: Layout,
    redirect: '/dashboard',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/Dashboard/index.vue'),
        meta: { title: '安全概览' },
      },
      {
        path: 'hosts',
        name: 'Hosts',
        component: () => import('@/views/Hosts/index.vue'),
        meta: { title: '主机列表' },
      },
      {
        path: 'hosts/:hostId',
        name: 'HostDetail',
        component: () => import('@/views/Hosts/Detail.vue'),
        meta: { title: '主机详情' },
      },
      {
        path: 'business-lines',
        name: 'BusinessLines',
        component: () => import('@/views/BusinessLines/index.vue'),
        meta: { title: '业务线管理' },
      },
      {
        path: 'policies',
        name: 'Policies',
        component: () => import('@/views/Policies/index.vue'),
        meta: { title: '基线检查' },
      },
      {
        path: 'policies/:policyId',
        name: 'PolicyDetail',
        component: () => import('@/views/Policies/Detail.vue'),
        meta: { title: '基线检查详情' },
      },
      {
        path: 'policy-groups',
        name: 'PolicyGroups',
        component: () => import('@/views/PolicyGroups/index.vue'),
        meta: { title: '策略组管理' },
      },
      {
        path: 'policy-groups/policies/:policyId/rules',
        name: 'PolicyRules',
        component: () => import('@/views/PolicyGroups/PolicyRules.vue'),
        meta: { title: '规则管理' },
      },
      {
        path: 'tasks',
        name: 'Tasks',
        component: () => import('@/views/Tasks/index.vue'),
        meta: { title: '任务执行' },
      },
      {
        path: 'baseline/fix',
        name: 'BaselineFix',
        component: () => import('@/views/Baseline/Fix.vue'),
        meta: { title: '基线修复' },
      },
      {
        path: 'baseline/fix-history',
        name: 'BaselineFixHistory',
        component: () => import('@/views/Baseline/FixHistory.vue'),
        meta: { title: '修复历史' },
      },
      {
        path: 'system/collection',
        name: 'SystemCollection',
        component: () => import('@/views/System/Collection.vue'),
        meta: { title: '平台授权' },
      },
      {
        path: 'system/components',
        name: 'SystemComponents',
        component: () => import('@/views/System/Components.vue'),
        meta: { title: '组件列表' },
      },
      {
        path: 'system/install',
        name: 'SystemInstall',
        component: () => import('@/views/System/Install.vue'),
        meta: { title: '安装配置' },
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('@/views/Users/index.vue'),
        meta: { title: '用户管理' },
      },
      {
        path: 'system/settings',
        name: 'SystemSettings',
        component: () => import('@/views/System/Settings.vue'),
        meta: { title: '基本设置' },
      },
      {
        path: 'system/notification',
        name: 'SystemNotification',
        component: () => import('@/views/System/Notification.vue'),
        meta: { title: '通知管理' },
      },
      {
        path: 'system/reports',
        name: 'SystemReports',
        component: () => import('@/views/System/Reports.vue'),
        meta: { title: '统计报表' },
      },
      {
        path: 'system/task-report',
        name: 'SystemTaskReport',
        component: () => import('@/views/System/TaskReport.vue'),
        meta: { title: '任务报告' },
      },
      {
        path: 'alerts',
        name: 'Alerts',
        component: () => import('@/views/Alerts/index.vue'),
        meta: { title: '告警管理' },
      },
      {
        path: 'alerts/:alertId',
        name: 'AlertDetail',
        component: () => import('@/views/Alerts/Detail.vue'),
        meta: { title: '告警详情' },
      },
      {
        path: 'system/inspection',
        name: 'Inspection',
        component: () => import('@/views/Inspection/index.vue'),
        meta: { title: '运维巡检' },
      },
    ],
  },
  // 404 错误页面
  {
    path: '/404',
    name: 'NotFound',
    component: () => import('@/views/Error/404.vue'),
    meta: { title: '页面不存在', public: true },
  },
  // 500 错误页面
  {
    path: '/500',
    name: 'ServerError',
    component: () => import('@/views/Error/500.vue'),
    meta: { title: '服务器错误', public: true },
  },
  // 捕获所有未匹配的路由
  {
    path: '/:pathMatch(.*)*',
    redirect: '/404',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()
  const { useSiteConfigStore } = await import('@/stores/site-config')
  const siteConfigStore = useSiteConfigStore()

  // 初始化站点配置（如果还未初始化）
  if (!siteConfigStore.config.site_name || siteConfigStore.config.site_name === '矩阵云安全平台') {
    await siteConfigStore.init()
  }

  // 更新页面标题
  if (to.meta.title) {
    document.title = `${to.meta.title} - ${siteConfigStore.siteName}`
  } else {
    document.title = siteConfigStore.siteName
  }

  // 公开路由（如登录页）直接放行
  if (to.meta.public) {
    // 如果已登录，重定向到首页
    if (authStore.isAuthenticated()) {
      next('/')
    } else {
      next()
    }
    return
  }

  // 需要认证的路由
  if (to.meta.requiresAuth) {
    if (!authStore.isAuthenticated()) {
      next('/login')
      return
    }
    // 初始化认证信息
    try {
      await authStore.initAuth()
    } catch (error) {
      console.error('认证初始化失败:', error)
      // 认证失败，跳转到登录页
      next('/login')
      return
    }
  }

  next()
})

// 路由错误处理
router.onError((error) => {
  console.error('路由错误:', error)
  // 如果是组件加载失败，跳转到 500 页面
  if (error.message && error.message.includes('Failed to fetch dynamically imported module')) {
    router.push('/500')
  }
})

export default router
