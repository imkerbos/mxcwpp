import apiClient from './client'
import type { ScanTask, PaginatedResponse } from './types'

export const tasksApi = {
  // 获取任务列表
  list: (params?: {
    page?: number
    page_size?: number
    status?: string
    policy_id?: string
  }) => {
    return apiClient.get<PaginatedResponse<ScanTask>>('/tasks', { params })
  },

  // 获取任务详情
  get: (taskId: string) => {
    return apiClient.get<ScanTask>(`/tasks/${taskId}`)
  },

  // 创建任务
  create: (data: {
    name: string
    type: 'manual' | 'scheduled'
    targets: {
      type: 'all' | 'host_ids' | 'os_family'
      host_ids?: string[]
      os_family?: string[]
    }
    policy_id: string
    rule_ids?: string[]
    schedule?: any
  }) => {
    return apiClient.post<ScanTask>('/tasks', data)
  },

  // 执行任务
  run: (taskId: string) => {
    return apiClient.post<ScanTask>(`/tasks/${taskId}/run`)
  },

  // 取消任务
  cancel: (taskId: string) => {
    return apiClient.post<ScanTask>(`/tasks/${taskId}/cancel`)
  },
}
