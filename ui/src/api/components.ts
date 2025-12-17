/**
 * 组件管理 API
 */
import apiClient from './client'

// 组件分类
export type ComponentCategory = 'agent' | 'plugin'

// 包类型
export type PackageType = 'rpm' | 'deb' | 'binary'

// 架构类型
export type ArchType = 'amd64' | 'arm64'

// 组件信息
export interface Component {
  id: number
  name: string
  category: ComponentCategory
  description: string
  created_by: string
  created_at: string
  latest_version?: string
  version_count?: number
  package_count?: number
}

// 组件版本信息
export interface ComponentVersion {
  id: number
  component_id: number
  version: string
  changelog: string
  is_latest: boolean
  created_by: string
  created_at: string
  packages?: ComponentPackage[]
  packages_summary?: string
}

// 组件包信息
export interface ComponentPackage {
  id: number
  version_id: number
  os: string
  arch: ArchType
  pkg_type: PackageType
  file_path: string
  file_name: string
  file_size: number
  sha256: string
  enabled: boolean
  uploaded_by: string
  uploaded_at: string
}

// 创建组件请求
export interface CreateComponentRequest {
  name: string
  category: ComponentCategory
  description?: string
}

// 发布版本请求
export interface ReleaseVersionRequest {
  version: string
  changelog?: string
  set_latest?: boolean
}

// 版本列表响应
export interface VersionsResponse {
  component: Component
  versions: ComponentVersion[]
}

// 插件同步状态
export interface PluginSyncStatus {
  name: string
  type: string
  config_version: string
  config_sha256: string
  config_enabled: boolean
  download_urls: string[]
  description: string
  has_package: boolean
  package_version?: string
  package_arch?: string
  status: 'ready' | 'missing_package' | 'outdated' | 'default_config'
}

// 组件 API
export const componentsApi = {
  /**
   * 获取组件列表
   */
  list: async (): Promise<Component[]> => {
    return await apiClient.get('/components')
  },

  /**
   * 创建组件
   */
  create: async (data: CreateComponentRequest): Promise<Component> => {
    return await apiClient.post('/components', data)
  },

  /**
   * 获取组件详情
   */
  get: async (id: number): Promise<Component> => {
    return await apiClient.get(`/components/${id}`)
  },

  /**
   * 删除组件
   */
  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/components/${id}`)
  },

  /**
   * 获取组件的版本列表
   */
  listVersions: async (componentId: number): Promise<VersionsResponse> => {
    return await apiClient.get(`/components/${componentId}/versions`)
  },

  /**
   * 发布新版本
   */
  releaseVersion: async (componentId: number, data: ReleaseVersionRequest): Promise<ComponentVersion> => {
    return await apiClient.post(`/components/${componentId}/versions`, data)
  },

  /**
   * 获取版本详情
   */
  getVersion: async (componentId: number, versionId: number): Promise<ComponentVersion> => {
    return await apiClient.get(`/components/${componentId}/versions/${versionId}`)
  },

  /**
   * 设置为最新版本
   */
  setLatestVersion: async (componentId: number, versionId: number): Promise<void> => {
    await apiClient.put(`/components/${componentId}/versions/${versionId}/set-latest`)
  },

  /**
   * 删除版本
   */
  deleteVersion: async (componentId: number, versionId: number): Promise<void> => {
    await apiClient.delete(`/components/${componentId}/versions/${versionId}`)
  },

  /**
   * 上传包文件
   */
  uploadPackage: async (componentId: number, versionId: number, formData: FormData): Promise<ComponentPackage> => {
    return await apiClient.post(`/components/${componentId}/versions/${versionId}/packages`, formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    })
  },

  /**
   * 删除包
   */
  deletePackage: async (packageId: number): Promise<void> => {
    await apiClient.delete(`/packages/${packageId}`)
  },

  /**
   * 获取插件同步状态
   */
  getPluginSyncStatus: async (): Promise<PluginSyncStatus[]> => {
    return await apiClient.get('/components/plugin-status')
  },
}

export default componentsApi
