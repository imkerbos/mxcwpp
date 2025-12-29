# 前端代码规范

本文档定义了项目中 TypeScript/Vue 代码的详细规范和最佳实践。

**最后更新**: 2025-12-29

---

## 1. 文件结构

```
src/
├── api/                    # API 客户端模块
│   ├── index.ts           # 导出所有 API
│   ├── hosts.ts           # 主机相关 API
│   ├── policies.ts        # 策略相关 API
│   └── ...
├── stores/                 # Pinia 状态管理
│   ├── index.ts
│   ├── auth.ts            # 认证状态
│   └── ui.ts              # UI 状态
├── views/                  # 页面组件
│   ├── Home.vue
│   ├── Hosts.vue
│   └── ...
├── components/             # 可重用组件
│   ├── HostTable.vue
│   ├── PolicyForm.vue
│   └── ...
└── utils/                  # 工具函数
    ├── request.ts         # HTTP 请求
    └── format.ts          # 数据格式化
```

---

## 2. 命名规范

```typescript
// 组件: PascalCase
export const HostList = defineComponent({})

// 函数: camelCase
const fetchHosts = async () => {}

// 常量: UPPER_CASE
const API_BASE_URL = 'http://localhost:8080'

// 接口: 以 I 开头 (可选)
interface IHost {
  id: string
  hostname: string
}
```

---

## 3. API 调用规范（必须遵循）

**文件位置**: `ui/src/api/*.ts`

所有 API 调用必须封装在 `src/api` 目录中，**禁止在组件中直接调用 axios**。

### API 封装示例

```typescript
// ui/src/api/hosts.ts
import { apiClient } from './client'

// 定义类型
export interface Host {
  id: string
  hostname: string
  ip: string
  os_family: string
  baseline_score: number
}

export interface ListHostsParams {
  page: number
  pageSize: number
  keyword?: string
  status?: string
}

// API 方法封装
export const hostsApi = {
  // 获取列表
  getList: (params: ListHostsParams) => {
    return apiClient.get<{ total: number; items: Host[] }>('/hosts', { params })
  },

  // 获取详情
  getById: (id: string) => {
    return apiClient.get<Host>(`/hosts/${id}`)
  },

  // 创建
  create: (data: Partial<Host>) => {
    return apiClient.post<Host>('/hosts', data)
  },

  // 更新
  update: (id: string, data: Partial<Host>) => {
    return apiClient.put<Host>(`/hosts/${id}`, data)
  },

  // 删除
  delete: (id: string) => {
    return apiClient.delete(`/hosts/${id}`)
  },
}
```

### 在组件中使用

```typescript
import { hostsApi } from '@/api/hosts'
import { message } from 'ant-design-vue'

const hosts = ref<Host[]>([])
const loading = ref(false)

const loadHosts = async () => {
  loading.value = true
  try {
    const { data } = await hostsApi.getList({ page: 1, pageSize: 10 })
    hosts.value = data.items
  } catch (error) {
    console.error('加载主机列表失败:', error)
    message.error('加载失败')
  } finally {
    loading.value = false
  }
}
```

### 错误做法

```typescript
// ❌ 直接在组件中调用 axios
const hosts = await axios.get('/api/v1/hosts')
```

---

## 4. 错误处理规范

### 正确用法

```typescript
const handleSubmit = async () => {
  try {
    await hostsApi.create(formData)
    message.success('创建成功')
    router.push('/hosts')
  } catch (error: any) {
    console.error('创建失败:', error)
    // 根据错误类型显示不同消息
    if (error.response?.status === 409) {
      message.error('资源已存在')
    } else if (error.response?.status === 400) {
      message.error(error.response?.data?.message || '参数错误')
    } else {
      message.error('操作失败，请重试')
    }
  }
}
```

### 错误做法

```typescript
// ❌ 忽略错误
const loadData = async () => {
  const data = await hostsApi.getList({ page: 1, pageSize: 10 })
  hosts.value = data.items
}
```

---

## 5. 类型定义

### 定义响应类型

```typescript
interface ApiResponse<T> {
  code: number
  data?: T
  message?: string
}

interface Host {
  id: string
  hostname: string
  os_family: string
  os_version: string
  baseline_score: number
}

// 使用类型
const response: ApiResponse<Host[]> = await getHosts()
```

---

## 6. Vue 组件规范

### 基本结构

```vue
<template>
  <div class="host-list">
    <a-table :columns="columns" :data-source="hosts" :loading="loading" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { hostsApi, type Host } from '@/api/hosts'

// 响应式数据
const hosts = ref<Host[]>([])
const loading = ref(false)

// 列表列定义
const columns = [
  { title: '主机名', dataIndex: 'hostname' },
  { title: '主机ID', dataIndex: 'id' },
]

// 加载数据
const loadHosts = async () => {
  loading.value = true
  try {
    const { data } = await hostsApi.getList({ page: 1, pageSize: 10 })
    hosts.value = data.items
  } catch (error) {
    console.error('加载失败:', error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadHosts()
})
</script>

<style scoped>
.host-list {
  padding: 20px;
}
</style>
```

---

## 7. 组件开发最佳实践

### 使用 Composition API

```typescript
import { ref, computed, watch, onMounted } from 'vue'

// 响应式数据
const count = ref(0)

// 计算属性
const doubleCount = computed(() => count.value * 2)

// 监听器
watch(count, (newVal, oldVal) => {
  console.log(`count changed from ${oldVal} to ${newVal}`)
})

// 生命周期
onMounted(() => {
  console.log('Component mounted')
})
```

### Props 和 Emits

```vue
<script setup lang="ts">
interface Props {
  title: string
  count?: number
}

interface Emits {
  (e: 'update', value: number): void
  (e: 'delete'): void
}

const props = withDefaults(defineProps<Props>(), {
  count: 0
})

const emit = defineEmits<Emits>()

const handleUpdate = () => {
  emit('update', props.count + 1)
}
</script>
```

---

## 8. 状态管理 (Pinia)

### Store 定义

```typescript
// stores/auth.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(null)
  const user = ref<User | null>(null)

  const login = async (username: string, password: string) => {
    const { data } = await authApi.login({ username, password })
    token.value = data.token
    user.value = data.user
  }

  const logout = () => {
    token.value = null
    user.value = null
  }

  const isAuthenticated = computed(() => !!token.value)

  return {
    token,
    user,
    login,
    logout,
    isAuthenticated
  }
})
```

### 在组件中使用

```vue
<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const handleLogin = async () => {
  await authStore.login(username.value, password.value)
  router.push('/')
}
</script>
```

---

## 9. 工具函数

### API 客户端

```typescript
// utils/request.ts
import axios from 'axios'
import { message } from 'ant-design-vue'

const apiClient = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

// 请求拦截器
apiClient.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

// 响应拦截器
apiClient.interceptors.response.use(
  response => {
    return response.data
  },
  error => {
    if (error.response?.status === 401) {
      message.error('未授权，请重新登录')
      // 重定向到登录页
    }
    return Promise.reject(error)
  }
)

export { apiClient }
```

### 消息提示

```typescript
import { message } from 'ant-design-vue'

message.success('操作成功')
message.error('操作失败')
message.warning('警告信息')
message.info('提示信息')
message.loading('加载中...')
```

---

## 页面组件模板

### 列表页面

```vue
<template>
  <div class="page-container">
    <a-table
      :columns="columns"
      :data-source="items"
      :loading="loading"
      :pagination="pagination"
      @change="handleTableChange"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, reactive } from 'vue'
import { xxxApi, type Xxx } from '@/api/xxx'
import { message } from 'ant-design-vue'

const items = ref<Xxx[]>([])
const loading = ref(false)
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
})

const columns = [
  { title: 'ID', dataIndex: 'id' },
  { title: '名称', dataIndex: 'name' },
]

const loadData = async () => {
  loading.value = true
  try {
    const { data } = await xxxApi.getList({
      page: pagination.current,
      pageSize: pagination.pageSize,
    })
    items.value = data.items
    pagination.total = data.total
  } catch (error) {
    console.error('加载数据失败:', error)
    message.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleTableChange = (pag: any) => {
  pagination.current = pag.current
  pagination.pageSize = pag.pageSize
  loadData()
}

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.page-container {
  padding: 20px;
}
</style>
```

### 表单页面

```vue
<template>
  <div class="form-container">
    <a-form
      :model="formData"
      :rules="rules"
      @finish="handleSubmit"
    >
      <a-form-item label="名称" name="name">
        <a-input v-model:value="formData.name" />
      </a-form-item>

      <a-form-item>
        <a-button type="primary" html-type="submit" :loading="loading">
          提交
        </a-button>
      </a-form-item>
    </a-form>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { message } from 'ant-design-vue'
import { useRouter } from 'vue-router'
import { xxxApi } from '@/api/xxx'

const router = useRouter()
const loading = ref(false)

const formData = reactive({
  name: '',
})

const rules = {
  name: [
    { required: true, message: '请输入名称' },
    { min: 3, max: 50, message: '名称长度在 3-50 之间' }
  ]
}

const handleSubmit = async () => {
  loading.value = true
  try {
    await xxxApi.create(formData)
    message.success('创建成功')
    router.push('/list')
  } catch (error: any) {
    console.error('创建失败:', error)
    message.error(error.response?.data?.message || '创建失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.form-container {
  max-width: 600px;
  margin: 20px auto;
}
</style>
```

---

**文档维护者**: Claude Code
**最后更新**: 2025-12-29
