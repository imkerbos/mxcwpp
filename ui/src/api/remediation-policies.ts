import apiClient from './client'

export interface RemediationPolicy {
  id: number
  name: string
  description: string
  targetType: string
  targetValue: string
  severityMin: string
  priorityMin: number
  autoConfirm: boolean
  maxParallel: number
  rolloutType: string
  canaryRatio: number
  enabled: boolean
  lastRunAt?: string
  createdBy: string
  createdAt: string
  updatedAt: string
}

export interface PolicyPreview {
  hostCount: number
  vulnCount: number
  taskCount: number
}

export const remediationPoliciesApi = {
  list: () => {
    return apiClient.get<RemediationPolicy[]>('/remediation-policies')
  },

  create: (data: Partial<RemediationPolicy>) => {
    return apiClient.post<RemediationPolicy>('/remediation-policies', data)
  },

  get: (id: number) => {
    return apiClient.get<RemediationPolicy>(`/remediation-policies/${id}`)
  },

  update: (id: number, data: Partial<RemediationPolicy>) => {
    return apiClient.put(`/remediation-policies/${id}`, data)
  },

  delete: (id: number) => {
    return apiClient.delete(`/remediation-policies/${id}`)
  },

  execute: (id: number) => {
    return apiClient.post(`/remediation-policies/${id}/execute`)
  },

  preview: (id: number) => {
    return apiClient.post<PolicyPreview>(`/remediation-policies/${id}/preview`)
  },
}
