import type { Component } from 'vue'
import {
  DashboardOutlined,
  DatabaseOutlined,
  SafetyOutlined,
  SettingOutlined,
  FileSearchOutlined,
  DesktopOutlined,
  MonitorOutlined,
  BugOutlined,
  AuditOutlined,
  CloudServerOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons-vue'

export interface MenuItem {
  key: string
  title: string
  icon?: Component
  route?: string
  children?: MenuItem[]
  adminOnly?: boolean
}

/**
 * 侧边栏菜单配置 — 参考 Elkeid 导航结构
 * 手风琴模式: 同一时间只展开一个子菜单
 */
export const menuConfig: MenuItem[] = [
  {
    key: 'dashboard',
    title: '安全概览',
    icon: DashboardOutlined,
    route: '/dashboard',
  },
  {
    key: 'assets',
    title: '资产中心',
    icon: DatabaseOutlined,
    children: [
      { key: 'hosts', title: '主机列表', route: '/hosts' },
      { key: 'asset-fingerprint', title: '资产指纹', route: '/asset-fingerprint' },
      { key: 'business-lines', title: '业务线管理', route: '/business-lines' },
    ],
  },
  {
    key: 'host-protection',
    title: '主机防护',
    icon: DesktopOutlined,
    children: [
      { key: 'alerts', title: '告警列表', route: '/alerts' },
      { key: 'whitelist', title: '白名单', route: '/whitelist' },
      { key: 'vuln-list', title: '漏洞列表', route: '/vuln-list' },
      { key: 'policies', title: '基线检查', route: '/policies' },
    ],
  },
  {
    key: 'baseline',
    title: '基线安全',
    icon: SafetyOutlined,
    children: [
      { key: 'policy-groups', title: '策略组管理', route: '/policy-groups' },
      { key: 'tasks', title: '任务执行', route: '/tasks' },
      { key: 'baseline-fix', title: '基线修复', route: '/baseline/fix' },
      { key: 'baseline-fix-history', title: '修复历史', route: '/baseline/fix-history' },
    ],
  },
  {
    key: 'fim',
    title: '文件完整性',
    icon: FileSearchOutlined,
    children: [
      { key: 'fim-dashboard', title: 'FIM 概览', route: '/fim/dashboard' },
      { key: 'fim-policies', title: 'FIM 策略', route: '/fim/policies' },
      { key: 'fim-events', title: 'FIM 事件', route: '/fim/events' },
      { key: 'fim-tasks', title: 'FIM 任务', route: '/fim/tasks' },
    ],
  },
  {
    key: 'virus',
    title: '病毒查杀',
    icon: BugOutlined,
    children: [
      { key: 'virus-scan', title: '病毒扫描', route: '/virus/scan' },
      { key: 'virus-quarantine', title: '文件隔离箱', route: '/virus/quarantine' },
    ],
  },
  {
    key: 'kube',
    title: '容器集群',
    icon: CloudServerOutlined,
    children: [
      { key: 'kube-clusters', title: '集群管理', route: '/kube/clusters' },
      { key: 'kube-alarms', title: '安全告警', route: '/kube/alarms' },
      { key: 'kube-events', title: '安全事件', route: '/kube/events' },
      { key: 'kube-baseline', title: '基线检查', route: '/kube/baseline' },
      { key: 'kube-whitelist', title: '告警白名单', route: '/kube/whitelist' },
    ],
  },
  {
    key: 'runtime-detection',
    title: '运行时检测',
    icon: ThunderboltOutlined,
    children: [
      { key: 'runtime-alerts', title: '告警事件', route: '/detection/events' },
      { key: 'detection-rules', title: '检测规则', route: '/detection/rules' },
      { key: 'threat-intel', title: '威胁情报', route: '/threat-intel' },
    ],
  },
  {
    key: 'system',
    title: '系统管理',
    icon: SettingOutlined,
    children: [
      { key: 'system-components', title: '组件列表', route: '/system/components' },
      { key: 'system-install', title: '安装配置', route: '/system/install' },
      { key: 'users', title: '用户管理', route: '/users' },
      { key: 'system-notification', title: '通知管理', route: '/system/notification' },
      { key: 'system-settings', title: '基本设置', route: '/system/settings' },
      { key: 'system-reports', title: '报告管理', route: '/system/reports' },
      { key: 'system-task-report', title: '任务报告', route: '/system/task-report' },
      { key: 'inspection', title: '运维巡检', route: '/system/inspection' },
      { key: 'system-backup', title: '配置备份', route: '/system/backup' },
      { key: 'system-migration', title: '迁移助手', route: '/system/migration', adminOnly: true },
      { key: 'system-collection', title: '平台授权', route: '/system/collection' },
    ],
  },
  {
    key: 'monitoring',
    title: '系统监控',
    icon: MonitorOutlined,
    children: [
      { key: 'host-monitor', title: '后端监控', route: '/system/host-monitor' },
      { key: 'service-monitor', title: '后端服务', route: '/system/service-monitor' },
      { key: 'service-alert', title: '服务告警', route: '/system/service-alert' },
    ],
  },
  {
    key: 'audit',
    title: '审计日志',
    icon: AuditOutlined,
    route: '/audit-log',
  },
]

/**
 * 扁平化菜单, 生成 key -> route 映射表
 */
export function buildRouteMap(items: MenuItem[]): Record<string, string> {
  const map: Record<string, string> = {}
  for (const item of items) {
    if (item.route) {
      map[item.key] = item.route
    }
    if (item.children) {
      Object.assign(map, buildRouteMap(item.children))
    }
  }
  return map
}

/**
 * 根据当前路径, 反查选中的 menu key 和展开的 submenu key
 */
export function resolveMenuKeys(path: string): { selectedKey: string; openKey: string } {
  for (const item of menuConfig) {
    if (item.route === path) {
      return { selectedKey: item.key, openKey: '' }
    }
    if (item.children) {
      for (const child of item.children) {
        if (child.route && path.startsWith(child.route)) {
          return { selectedKey: child.key, openKey: item.key }
        }
      }
    }
  }
  return { selectedKey: '', openKey: '' }
}

/**
 * 路由映射表 (由 menuConfig 自动生成)
 */
export const routeMap = buildRouteMap(menuConfig)
