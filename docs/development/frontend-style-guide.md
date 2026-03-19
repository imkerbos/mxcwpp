# 前端代码规范

Vue 3 + TypeScript + Ant Design Vue

## 目录结构

```
ui/src/
├── api/          # API 调用封装
├── stores/       # Pinia 状态管理
├── views/        # 页面组件
├── components/   # 通用组件
├── utils/        # 工具函数
├── router/       # 路由定义
└── assets/       # 静态资源
```

## 命名规范

| 类型 | 规范 | 示例 |
|------|------|------|
| 组件文件 | PascalCase | `HostDetail.vue` |
| API 文件 | camelCase | `hosts.ts` |
| 函数 | camelCase | `fetchHostList` |
| 常量 | UPPER_CASE | `MAX_PAGE_SIZE` |
| 类型/接口 | PascalCase | `HostInfo`, `PolicyListResponse` |

## API 调用（必须遵循）

**所有 API 调用必须封装在 `src/api/` 目录，禁止直接调用 axios。**

```typescript
// src/api/hosts.ts
import apiClient from './client'
import type { Host, HostListResponse } from './types'

export const hostsApi = {
  getList: (params: { page: number; page_size: number }) =>
    apiClient.get<HostListResponse>('/hosts', { params }),

  getById: (hostId: string) =>
    apiClient.get<Host>(`/hosts/${hostId}`),

  delete: (hostId: string) =>
    apiClient.delete(`/hosts/${hostId}`),
}
```

```typescript
// 使用 - 正确
import { hostsApi } from '@/api/hosts'
const { data } = await hostsApi.getList({ page: 1, page_size: 20 })

// 错误 - 禁止
import axios from 'axios'
const { data } = await axios.get('/api/v1/hosts')
```

## 错误处理（必须遵循）

**所有 API 调用必须有 try-catch。**

```typescript
const loadHosts = async () => {
  loading.value = true
  try {
    const { data } = await hostsApi.getList({ page: 1, page_size: 20 })
    hosts.value = data.items
  } catch (error) {
    console.error('加载主机列表失败:', error)
    message.error('加载失败')
  } finally {
    loading.value = false
  }
}
```

## TypeScript 类型

为所有 API 响应定义接口:

```typescript
// src/api/types.ts
export interface Host {
  host_id: string
  hostname: string
  os_family: string
  os_version: string
  status: 'online' | 'offline'
  last_heartbeat: string
}

export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export type HostListResponse = PaginatedResponse<Host>
```

## Vue 组件

使用 Composition API + `<script setup>`:

```vue
<template>
  <a-table :dataSource="hosts" :loading="loading" :columns="columns" />
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { hostsApi } from '@/api/hosts'
import type { Host } from '@/api/types'

const hosts = ref<Host[]>([])
const loading = ref(false)

const loadData = async () => {
  loading.value = true
  try {
    const { data } = await hostsApi.getList({ page: 1, page_size: 20 })
    hosts.value = data.items
  } catch (error) {
    console.error('加载失败:', error)
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>
```

## Pinia Store

```typescript
// src/stores/auth.ts
import { defineStore } from 'pinia'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('token') || '',
    user: null as UserInfo | null,
  }),
  actions: {
    async login(username: string, password: string) {
      const { data } = await authApi.login({ username, password })
      this.token = data.token
      localStorage.setItem('token', data.token)
    },
    logout() {
      this.token = ''
      this.user = null
      localStorage.removeItem('token')
    },
  },
})
```
