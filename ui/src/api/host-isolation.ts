import apiClient from './client'
import type { PaginatedResponse } from './types'

export interface HostIsolation {
  id: number
  host_id: string
  hostname: string
  level: 'none' | 'selective' | 'standard' | 'complete'
  reason: string
  timeout: number
  status: 'pending' | 'active' | 'released' | 'failed'
  source: 'manual' | 'auto_response' | 'threat_intel'
  created_by: string
  isolated_at?: string
  released_at?: string
  released_by?: string
  created_at: string
  updated_at: string
}

export interface IsolationStatus {
  isolated: boolean
  isolation?: HostIsolation
}

export const hostIsolationApi = {
  isolate: (data: { host_id: string; level: string; reason: string; timeout?: number }) => {
    return apiClient.post('/hosts/isolate', data)
  },

  release: (data: { host_id: string }) => {
    return apiClient.post('/hosts/release', data)
  },

  getStatus: (hostId: string) => {
    return apiClient.get<IsolationStatus>(`/hosts/${hostId}/isolation-status`)
  },

  list: (params?: { page?: number; page_size?: number; status?: string }) => {
    return apiClient.get<PaginatedResponse<HostIsolation>>('/hosts/isolations', { params })
  },
}
