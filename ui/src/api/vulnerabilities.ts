import apiClient from './client'
import type { VulnerabilityListResult } from './types'
import type { SecurityDBSyncRecord } from './antivirus'

export const vulnerabilitiesApi = {
  list: (params?: {
    page?: number
    page_size?: number
    host_id?: string
    search?: string
    severity?: string
    status?: string
    component?: string
  }) => {
    return apiClient.get<VulnerabilityListResult>('/vulnerabilities', { params })
  },

  ignore: (id: number) => {
    return apiClient.post(`/vulnerabilities/${id}/ignore`)
  },

  triggerScan: () => {
    return apiClient.post('/vulnerabilities/scan')
  },

  getScanStatus: () => {
    return apiClient.get<SecurityDBSyncRecord | { status: string; message: string }>('/vulnerabilities/scan-status')
  },

  getScanHistory: (params?: { page?: number; page_size?: number }) => {
    return apiClient.get<{ total: number; items: SecurityDBSyncRecord[] }>('/vulnerabilities/scan-history', { params })
  },
}
