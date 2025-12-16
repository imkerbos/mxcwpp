import apiClient from './client'
import type { Policy, Rule, PolicyStatistics } from './types'

export const policiesApi = {
  // 获取策略列表
  list: (params?: {
    os_family?: string
    enabled?: boolean
    group_id?: string
  }) => {
    return apiClient.get<{ items: Policy[] }>('/policies', { params })
  },

  // 获取策略详情
  get: (policyId: string) => {
    return apiClient.get<Policy>(`/policies/${policyId}`)
  },

  // 创建策略
  create: (data: {
    id: string
    name: string
    version?: string
    description?: string
    os_family?: string[]
    os_version?: string
    enabled?: boolean
    group_id?: string
    rules?: Array<{
      rule_id: string
      category?: string
      title: string
      description?: string
      severity?: string
      check_config: any
      fix_config?: any
    }>
  }) => {
    return apiClient.post<Policy>('/policies', data)
  },

  // 更新策略
  update: (policyId: string, data: {
    name?: string
    version?: string
    description?: string
    os_family?: string[]
    os_version?: string
    enabled?: boolean
    group_id?: string
    rules?: Array<{
      rule_id: string
      category?: string
      title: string
      description?: string
      severity?: string
      check_config: any
      fix_config?: any
    }>
  }) => {
    return apiClient.put<Policy>(`/policies/${policyId}`, data)
  },

  // 删除策略
  delete: (policyId: string) => {
    return apiClient.delete(`/policies/${policyId}`)
  },

  // 获取策略统计信息
  getStatistics: (policyId: string) => {
    return apiClient.get<PolicyStatistics>(`/policies/${policyId}/statistics`)
  },
}
