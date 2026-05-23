import apiClient from './client'
import type { PaginatedResponse } from './types'

export interface AnomalyAlert {
  id: number
  host_id: string
  hostname: string
  alert_type: 'isolation_forest' | 'correlation'
  pattern_name: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  anomaly_score: number
  top_metric: string
  top_value: number
  description: string
  status: 'open' | 'confirmed' | 'false_positive'
  resolved_by: string
  created_at: string
  updated_at: string
}

export interface AnomalyStats {
  total: number
  open: number
  critical: number
  by_type: { alert_type: string; count: number }[]
  by_pattern: { alert_type: string; count: number }[]
}

export interface ListAnomalyParams {
  page?: number
  page_size?: number
  host_id?: string
  alert_type?: string
  severity?: string
  status?: string
}

export const anomalyApi = {
  list: (params?: ListAnomalyParams) => {
    return apiClient.get<PaginatedResponse<AnomalyAlert>>('/anomalies', { params })
  },

  stats: () => {
    return apiClient.get<AnomalyStats>('/anomalies/stats')
  },

  resolve: (id: number, status: 'confirmed' | 'false_positive') => {
    return apiClient.put(`/anomalies/${id}/resolve`, { status })
  },
}
