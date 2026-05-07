import apiClient from './client'

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: {
    username: string
    role: string
  }
}

export interface ChangePasswordRequest {
  old_password: string
  new_password: string
}

export const authApi = {
  login: async (data: LoginRequest): Promise<LoginResponse> => {
    return apiClient.post('/auth/login', data)
  },

  logout: async (): Promise<void> => {
    return apiClient.post('/auth/logout')
  },

  getCurrentUser: async (): Promise<{ username: string; role: string }> => {
    return apiClient.get('/auth/me')
  },

  changePassword: async (data: ChangePasswordRequest): Promise<void> => {
    return apiClient.post('/auth/change-password', data)
  },
}
