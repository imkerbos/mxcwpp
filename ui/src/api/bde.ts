import apiClient from './client'
import type { PaginatedResponse } from './types'

export interface HostBaselineState {
  id: number
  host_id: string
  phase: 'learning' | 'active'
  samples: number
  first_seen: string
  created_at: string
  updated_at: string
}

export interface BaselineStats {
  total_hosts: number
  learning_hosts: number
  active_hosts: number
  open_alerts: number
}

export interface BehaviorAlert {
  id: number
  host_id: string
  hostname: string
  risk_score: number
  metric: string
  value: number
  mean: number
  stddev: number
  z_score: number
  status: 'open' | 'resolved' | 'ignored'
  created_at: string
  updated_at: string
}

export const bdeApi = {
  listBaselineStates: (params?: { page?: number; page_size?: number; phase?: string; host_id?: string }) => {
    return apiClient.get<PaginatedResponse<HostBaselineState>>('/bde/baseline/states', { params })
  },

  baselineStats: () => {
    return apiClient.get<BaselineStats>('/bde/baseline/stats')
  },

  listAlerts: (params?: { page?: number; page_size?: number; host_id?: string; status?: string; metric?: string }) => {
    return apiClient.get<PaginatedResponse<BehaviorAlert>>('/bde/alerts', { params })
  },
}
