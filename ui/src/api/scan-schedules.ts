import apiClient from './client'

export interface ScanSchedule {
  id: number
  name: string
  scanType: string
  cronExpr: string
  enabled: boolean
  lastRunAt?: string
  nextRunAt?: string
  createdBy: string
  createdAt: string
  updatedAt: string
}

export const scanSchedulesApi = {
  list: () => {
    return apiClient.get<ScanSchedule[]>('/vulnerabilities/schedules')
  },

  create: (data: { name: string; scanType: string; cronExpr: string }) => {
    return apiClient.post<ScanSchedule>('/vulnerabilities/schedules', data)
  },

  update: (id: number, data: Partial<ScanSchedule>) => {
    return apiClient.put(`/vulnerabilities/schedules/${id}`, data)
  },

  delete: (id: number) => {
    return apiClient.delete(`/vulnerabilities/schedules/${id}`)
  },

  toggle: (id: number) => {
    return apiClient.post(`/vulnerabilities/schedules/${id}/toggle`)
  },
}
