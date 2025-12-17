import apiClient from './client'
import type { Host, HostDetail, PaginatedResponse, BaselineScore, BaselineSummary, HostMetrics } from './types'

export interface HostStatusDistribution {
  running: number
  abnormal: number
  offline: number
  not_installed: number
  uninstalled: number
}

export interface HostRiskDistribution {
  critical: number   // 存在严重风险基线的主机数
  high: number       // 存在高危风险基线的主机数
  medium: number     // 存在中危风险基线的主机数
  low: number        // 存在低危风险基线的主机数
}

export interface HostRiskStatistics {
  alerts: {
    total: number
    critical: number
    high: number
    medium: number
    low: number
  }
  vulnerabilities: {
    total: number
    critical: number
    high: number
    medium: number
    low: number
  }
  baseline: {
    total: number
    critical: number
    high: number
    medium: number
    low: number
  }
}

export const hostsApi = {
  // 获取主机列表
  list: (params?: {
    page?: number
    page_size?: number
    os_family?: string
    status?: string
    business_line?: string
    search?: string // 搜索关键词（主机名、host_id等）
    is_container?: boolean // 容器/主机类型筛选
  }) => {
    return apiClient.get<PaginatedResponse<Host>>('/hosts', { params })
  },

  // 获取主机详情
  get: (hostId: string) => {
    return apiClient.get<HostDetail>(`/hosts/${hostId}`)
  },

  // 获取主机基线得分
  getScore: (hostId: string) => {
    return apiClient.get<BaselineScore>(`/results/host/${hostId}/score`)
  },

  // 获取主机基线摘要
  getSummary: (hostId: string) => {
    return apiClient.get<BaselineSummary>(`/results/host/${hostId}/summary`)
  },

  // 获取主机状态分布
  getStatusDistribution: () => {
    return apiClient.get<HostStatusDistribution>('/hosts/status-distribution')
  },

  // 获取主机风险分布
  getRiskDistribution: () => {
    return apiClient.get<HostRiskDistribution>('/hosts/risk-distribution')
  },

  // 获取主机监控数据
  getMetrics: (hostId: string, params?: {
    start_time?: string
    end_time?: string
  }) => {
    return apiClient.get<HostMetrics>(`/hosts/${hostId}/metrics`, { params })
  },

  // 更新主机标签
  updateTags: (hostId: string, tags: string[]) => {
    return apiClient.put(`/hosts/${hostId}/tags`, { tags })
  },

  // 获取主机风险统计
  getRiskStatistics: (hostId: string) => {
    return apiClient.get<HostRiskStatistics>(`/hosts/${hostId}/risk-statistics`)
  },

  // 更新主机业务线
  updateBusinessLine: (hostId: string, businessLine: string) => {
    return apiClient.put(`/hosts/${hostId}/business-line`, { business_line: businessLine })
  },
}
