import apiClient from './client'
import type { PaginatedResponse } from './types'

export interface HuntQuery {
  id: number
  name: string
  description: string
  mql: string
  category: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  owner: string
  is_builtin: boolean
  last_run_at?: string
  last_hits: number
  created_at: string
  updated_at: string
}

export interface QueryResult {
  columns: string[]
  rows: Record<string, unknown>[]
  total_rows: number
  elapsed_ms: number
  sql: string
}

export interface CreateHuntQueryParams {
  name: string
  description?: string
  mql: string
  category?: string
  severity?: string
}

export const huntingApi = {
  executeQuery: (mql: string, timeout_seconds?: number) => {
    return apiClient.post<QueryResult>('/hunting/query', { mql, timeout_seconds })
  },

  listQueries: (params?: { page?: number; page_size?: number; category?: string }) => {
    return apiClient.get<PaginatedResponse<HuntQuery>>('/hunting/queries', { params })
  },

  createQuery: (data: CreateHuntQueryParams) => {
    return apiClient.post<HuntQuery>('/hunting/queries', data)
  },

  deleteQuery: (id: number) => {
    return apiClient.delete(`/hunting/queries/${id}`)
  },
}
