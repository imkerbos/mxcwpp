<template>
  <div class="system-components-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <h2>组件管理</h2>
      <a-button type="primary" @click="showCreateModal = true">
        <template #icon><PlusOutlined /></template>
        新建组件
      </a-button>
    </div>

    <!-- 插件同步状态 -->
    <div class="plugin-status-section">
      <div class="section-title">
        <span>插件配置状态</span>
        <a-tooltip title="显示 Agent 端插件的配置同步状态。系统会将插件配置推送给 Agent，Agent 根据配置下载并运行插件。">
          <QuestionCircleOutlined style="margin-left: 8px; color: #999" />
        </a-tooltip>
      </div>
      <div v-if="pluginStatuses.length === 0" class="empty-status">
        <a-empty description="暂无插件配置" />
      </div>
      <div v-else class="plugin-status-cards">
        <div
          v-for="status in pluginStatuses"
          :key="status.name"
          class="plugin-status-card"
          :class="getPluginStatusClass(status.status)"
        >
          <div class="plugin-name">
            <a-tag :color="getNameColor(status.name)">{{ status.name }}</a-tag>
            <a-switch
              :checked="status.config_enabled"
              size="small"
              style="margin-left: 8px"
              disabled
            />
          </div>
          <div class="plugin-info">
            <div class="info-row">
              <span class="label">配置版本:</span>
              <span class="value">{{ status.config_version }}</span>
            </div>
            <div class="info-row" v-if="status.has_package">
              <span class="label">组件包:</span>
              <span class="value">v{{ status.package_version }} ({{ status.package_arch }})</span>
            </div>
            <div class="info-row">
              <span class="label">状态:</span>
              <a-tag :color="getPluginStatusColor(status.status)">
                {{ getPluginStatusLabel(status.status) }}
              </a-tag>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 组件列表表格 -->
    <a-table
      :columns="columns"
      :data-source="components"
      :loading="loading"
      row-key="id"
      :pagination="false"
    >
      <template #bodyCell="{ column, record }">
        <!-- 组件名称 -->
        <template v-if="column.key === 'name'">
          <a-tag :color="getCategoryColor(record.category)">
            {{ record.category === 'agent' ? 'Agent' : 'Plugin' }}
          </a-tag>
          <span style="margin-left: 8px; font-weight: 500">{{ record.name }}</span>
        </template>

        <!-- 最新版本 -->
        <template v-else-if="column.key === 'latest_version'">
          <span v-if="record.latest_version">{{ record.latest_version }}</span>
          <span v-else style="color: #999">-</span>
        </template>

        <!-- 版本数量 -->
        <template v-else-if="column.key === 'version_count'">
          <span>{{ record.version_count || 0 }}</span>
        </template>

        <!-- 创建者 -->
        <template v-else-if="column.key === 'created_by'">
          <span>{{ record.created_by }}</span>
        </template>

        <!-- 创建时间 -->
        <template v-else-if="column.key === 'created_at'">
          <span>{{ formatDate(record.created_at) }}</span>
        </template>

        <!-- 操作 -->
        <template v-else-if="column.key === 'action'">
          <a-space>
            <a-button type="link" size="small" @click="openReleaseModal(record)">
              发布版本
            </a-button>
            <a-button type="link" size="small" @click="openVersionsModal(record)">
              详情
            </a-button>
            <a-popconfirm
              title="确定要删除这个组件吗？"
              @confirm="deleteComponent(record)"
            >
              <a-button type="link" size="small" danger>删除</a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </template>
    </a-table>

    <!-- 新建组件弹窗 -->
    <a-modal
      v-model:open="showCreateModal"
      title="新建组件"
      :footer="null"
      :width="500"
      @cancel="resetCreateForm"
    >
      <a-form
        ref="createFormRef"
        :model="createForm"
        :rules="createRules"
        layout="vertical"
        @finish="handleCreate"
      >
        <a-form-item label="组件名称" name="name">
          <a-select v-model:value="createForm.name" placeholder="请选择组件" @change="onComponentNameChange">
            <a-select-option value="agent">Agent (主程序)</a-select-option>
            <a-select-option value="baseline">Baseline (基线检查插件)</a-select-option>
            <a-select-option value="collector">Collector (资产采集插件)</a-select-option>
          </a-select>
        </a-form-item>

        <a-form-item label="组件分类" name="category">
          <a-select v-model:value="createForm.category" placeholder="请选择组件分类" disabled>
            <a-select-option value="agent">Agent (主程序)</a-select-option>
            <a-select-option value="plugin">Plugin (插件)</a-select-option>
          </a-select>
        </a-form-item>

        <a-form-item label="描述" name="description">
          <a-textarea
            v-model:value="createForm.description"
            placeholder="可选：添加组件描述"
            :rows="2"
          />
        </a-form-item>

        <a-form-item>
          <a-space>
            <a-button type="primary" html-type="submit" :loading="creating">
              创建
            </a-button>
            <a-button @click="showCreateModal = false">取消</a-button>
          </a-space>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 发布版本弹窗 -->
    <a-modal
      v-model:open="showReleaseModal"
      :title="`发布版本 - ${selectedComponent?.name || ''}`"
      :footer="null"
      :width="700"
      @cancel="resetReleaseForm"
    >
      <a-form
        ref="releaseFormRef"
        :model="releaseForm"
        :rules="releaseRules"
        layout="vertical"
        @finish="handleRelease"
      >
        <a-form-item label="版本号" name="version">
          <a-input v-model:value="releaseForm.version" placeholder="例如: 1.0.0 或 1.8.5.31" />
        </a-form-item>

        <a-form-item label="上传文件">
          <div class="upload-grid">
            <!-- Agent 类型：显示 RPM 和 DEB -->
            <template v-if="selectedComponent?.category === 'agent'">
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux RHEL x86-64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.rpm_amd64"
                    :before-upload="() => false"
                    :max-count="1"
                    accept=".rpm"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.rpm_amd64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.rpm_amd64[0]?.name }}
                </div>
              </div>
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux RHEL AArch64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.rpm_arm64"
                    :before-upload="() => false"
                    :max-count="1"
                    accept=".rpm"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.rpm_arm64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.rpm_arm64[0]?.name }}
                </div>
              </div>
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux Debian x86-64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.deb_amd64"
                    :before-upload="() => false"
                    :max-count="1"
                    accept=".deb"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.deb_amd64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.deb_amd64[0]?.name }}
                </div>
              </div>
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux Debian AArch64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.deb_arm64"
                    :before-upload="() => false"
                    :max-count="1"
                    accept=".deb"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.deb_arm64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.deb_arm64[0]?.name }}
                </div>
              </div>
            </template>

            <!-- Plugin 类型：只显示二进制文件 -->
            <template v-else>
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux x86-64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.binary_amd64"
                    :before-upload="() => false"
                    :max-count="1"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.binary_amd64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.binary_amd64[0]?.name }}
                </div>
              </div>
              <div class="upload-item">
                <div class="upload-header">
                  <span class="upload-label">* Linux AArch64</span>
                  <a-upload
                    v-model:fileList="releaseForm.files.binary_arm64"
                    :before-upload="() => false"
                    :max-count="1"
                    :showUploadList="false"
                  >
                    <a-button type="primary" size="small">
                      <template #icon><UploadOutlined /></template>
                      选择文件
                    </a-button>
                  </a-upload>
                </div>
                <div class="upload-file" v-if="releaseForm.files.binary_arm64.length">
                  <PaperClipOutlined /> {{ releaseForm.files.binary_arm64[0]?.name }}
                </div>
              </div>
            </template>
          </div>
          <div class="upload-hint">
            提示：可以只上传部分平台的包，未上传的平台将不可用
          </div>
        </a-form-item>

        <a-form-item label="更新日志" name="changelog">
          <a-textarea
            v-model:value="releaseForm.changelog"
            placeholder="可选：添加版本更新说明"
            :rows="3"
          />
        </a-form-item>

        <a-form-item>
          <a-checkbox v-model:checked="releaseForm.set_latest">
            设置为最新版本
          </a-checkbox>
        </a-form-item>

        <a-form-item>
          <a-space>
            <a-button type="primary" html-type="submit" :loading="releasing">
              发布
            </a-button>
            <a-button @click="showReleaseModal = false">取消</a-button>
          </a-space>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 版本详情弹窗 -->
    <a-modal
      v-model:open="showVersionsModal"
      :title="`${selectedComponent?.name || ''} - 版本历史`"
      :footer="null"
      :width="900"
    >
      <a-spin :spinning="loadingVersions">
        <a-table
          :columns="versionColumns"
          :data-source="versions"
          row-key="id"
          :pagination="false"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <!-- 版本号 -->
            <template v-if="column.key === 'version'">
              <span>{{ record.version }}</span>
              <a-tag v-if="record.is_latest" color="green" style="margin-left: 8px">最新</a-tag>
            </template>

            <!-- 包文件 -->
            <template v-else-if="column.key === 'packages'">
              <div class="packages-list">
                <template v-if="record.packages && record.packages.length > 0">
                  <a-tag
                    v-for="pkg in record.packages"
                    :key="pkg.id"
                    :color="pkg.pkg_type === 'binary' ? 'purple' : pkg.pkg_type === 'rpm' ? 'blue' : 'orange'"
                  >
                    {{ pkg.arch }}/{{ pkg.pkg_type }}
                  </a-tag>
                </template>
                <span v-else style="color: #999">无</span>
              </div>
            </template>

            <!-- 上传者 -->
            <template v-else-if="column.key === 'created_by'">
              <span>{{ record.created_by }}</span>
            </template>

            <!-- 时间 -->
            <template v-else-if="column.key === 'created_at'">
              <span>{{ formatDate(record.created_at) }}</span>
            </template>

            <!-- 操作 -->
            <template v-else-if="column.key === 'action'">
              <a-space>
                <a-button
                  v-if="!record.is_latest"
                  type="link"
                  size="small"
                  @click="setLatestVersion(record)"
                >
                  设为最新
                </a-button>
                <a-popconfirm
                  title="确定要删除这个版本吗？"
                  @confirm="deleteVersion(record)"
                >
                  <a-button type="link" size="small" danger>删除</a-button>
                </a-popconfirm>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-spin>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { message } from 'ant-design-vue'
import {
  PlusOutlined,
  UploadOutlined,
  QuestionCircleOutlined,
  PaperClipOutlined,
} from '@ant-design/icons-vue'
import {
  componentsApi,
  type Component,
  type ComponentVersion,
  type PluginSyncStatus,
} from '@/api/components'

// 表格列定义
const columns = [
  { title: '组件', key: 'name', dataIndex: 'name', width: 200 },
  { title: '最新版本', key: 'latest_version', dataIndex: 'latest_version', width: 120 },
  { title: '版本数', key: 'version_count', dataIndex: 'version_count', width: 80 },
  { title: '创建者', key: 'created_by', dataIndex: 'created_by', width: 100 },
  { title: '创建时间', key: 'created_at', dataIndex: 'created_at', width: 180 },
  { title: '操作', key: 'action', width: 200 },
]

const versionColumns = [
  { title: '版本', key: 'version', dataIndex: 'version', width: 150 },
  { title: '包文件', key: 'packages', width: 300 },
  { title: '上传者', key: 'created_by', dataIndex: 'created_by', width: 100 },
  { title: '发布时间', key: 'created_at', dataIndex: 'created_at', width: 180 },
  { title: '操作', key: 'action', width: 150 },
]

// 数据
const loading = ref(false)
const components = ref<Component[]>([])
const pluginStatuses = ref<PluginSyncStatus[]>([])

// 新建组件
const showCreateModal = ref(false)
const creating = ref(false)
const createFormRef = ref()
const createForm = reactive({
  name: '',
  category: undefined as string | undefined,
  description: '',
})

const createRules = {
  name: [{ required: true, message: '请选择组件' }],
  category: [{ required: true, message: '请选择组件分类' }],
}

// 组件名称变化时自动设置分类
const onComponentNameChange = (name: string) => {
  if (name === 'agent') {
    createForm.category = 'agent'
  } else {
    createForm.category = 'plugin'
  }
}

// 发布版本
const showReleaseModal = ref(false)
const releasing = ref(false)
const releaseFormRef = ref()
const selectedComponent = ref<Component | null>(null)
const releaseForm = reactive({
  version: '',
  changelog: '',
  set_latest: true,
  files: {
    rpm_amd64: [] as any[],
    rpm_arm64: [] as any[],
    deb_amd64: [] as any[],
    deb_arm64: [] as any[],
    binary_amd64: [] as any[],
    binary_arm64: [] as any[],
  },
})

const releaseRules = {
  version: [
    { required: true, message: '请输入版本号' },
    { pattern: /^\d+\.\d+(\.\d+)*(-\w+)?$/, message: '版本号格式不正确，例如: 1.0.0' },
  ],
}

// 版本详情
const showVersionsModal = ref(false)
const loadingVersions = ref(false)
const versions = ref<ComponentVersion[]>([])

// 加载组件列表
const loadComponents = async () => {
  loading.value = true
  try {
    const data = await componentsApi.list()
    components.value = data || []
  } catch (error: any) {
    message.error(error.message || '加载组件列表失败')
  } finally {
    loading.value = false
  }
}

// 加载插件状态
const loadPluginStatus = async () => {
  try {
    const data = await componentsApi.getPluginSyncStatus()
    pluginStatuses.value = data || []
  } catch (error) {
    console.error('加载插件状态失败:', error)
  }
}

// 创建组件
const handleCreate = async () => {
  creating.value = true
  try {
    await componentsApi.create({
      name: createForm.name,
      category: createForm.category as 'agent' | 'plugin',
      description: createForm.description,
    })
    message.success('创建成功')
    showCreateModal.value = false
    resetCreateForm()
    loadComponents()
  } catch (error: any) {
    message.error(error.message || '创建失败')
  } finally {
    creating.value = false
  }
}

// 重置创建表单
const resetCreateForm = () => {
  createForm.name = ''
  createForm.category = undefined
  createForm.description = ''
  createFormRef.value?.resetFields()
}

// 打开发布版本弹窗
const openReleaseModal = (component: Component) => {
  selectedComponent.value = component
  showReleaseModal.value = true
}

// 发布版本
const handleRelease = async () => {
  if (!selectedComponent.value) return

  // 检查是否至少上传了一个文件
  const hasFiles = Object.values(releaseForm.files).some(files => files.length > 0)
  if (!hasFiles) {
    message.error('请至少上传一个包文件')
    return
  }

  releasing.value = true
  try {
    // 1. 先创建版本
    const version = await componentsApi.releaseVersion(selectedComponent.value.id, {
      version: releaseForm.version,
      changelog: releaseForm.changelog,
      set_latest: releaseForm.set_latest,
    })

    // 2. 上传包文件
    const uploadTasks: Promise<any>[] = []
    const fileMapping: { files: any[]; pkgType: string; arch: string }[] = [
      { files: releaseForm.files.rpm_amd64, pkgType: 'rpm', arch: 'amd64' },
      { files: releaseForm.files.rpm_arm64, pkgType: 'rpm', arch: 'arm64' },
      { files: releaseForm.files.deb_amd64, pkgType: 'deb', arch: 'amd64' },
      { files: releaseForm.files.deb_arm64, pkgType: 'deb', arch: 'arm64' },
      { files: releaseForm.files.binary_amd64, pkgType: 'binary', arch: 'amd64' },
      { files: releaseForm.files.binary_arm64, pkgType: 'binary', arch: 'arm64' },
    ]

    for (const { files, pkgType, arch } of fileMapping) {
      if (files.length > 0) {
        const formData = new FormData()
        formData.append('file', files[0].originFileObj)
        formData.append('pkg_type', pkgType)
        formData.append('arch', arch)
        uploadTasks.push(
          componentsApi.uploadPackage(selectedComponent.value.id, version.id, formData)
        )
      }
    }

    if (uploadTasks.length > 0) {
      await Promise.all(uploadTasks)
    }

    message.success('发布成功')
    showReleaseModal.value = false
    resetReleaseForm()
    loadComponents()
    loadPluginStatus()
  } catch (error: any) {
    message.error(error.message || '发布失败')
  } finally {
    releasing.value = false
  }
}

// 重置发布表单
const resetReleaseForm = () => {
  releaseForm.version = ''
  releaseForm.changelog = ''
  releaseForm.set_latest = true
  releaseForm.files = {
    rpm_amd64: [],
    rpm_arm64: [],
    deb_amd64: [],
    deb_arm64: [],
    binary_amd64: [],
    binary_arm64: [],
  }
  releaseFormRef.value?.resetFields()
}

// 打开版本详情弹窗
const openVersionsModal = async (component: Component) => {
  selectedComponent.value = component
  showVersionsModal.value = true
  loadingVersions.value = true
  try {
    const data = await componentsApi.listVersions(component.id)
    versions.value = data.versions || []
  } catch (error: any) {
    message.error(error.message || '加载版本列表失败')
  } finally {
    loadingVersions.value = false
  }
}

// 设置最新版本
const setLatestVersion = async (version: ComponentVersion) => {
  if (!selectedComponent.value) return
  try {
    await componentsApi.setLatestVersion(selectedComponent.value.id, version.id)
    message.success('设置成功')
    // 刷新版本列表
    openVersionsModal(selectedComponent.value)
    loadComponents()
    loadPluginStatus()
  } catch (error: any) {
    message.error(error.message || '设置失败')
  }
}

// 删除版本
const deleteVersion = async (version: ComponentVersion) => {
  if (!selectedComponent.value) return
  try {
    await componentsApi.deleteVersion(selectedComponent.value.id, version.id)
    message.success('删除成功')
    openVersionsModal(selectedComponent.value)
    loadComponents()
  } catch (error: any) {
    message.error(error.message || '删除失败')
  }
}

// 删除组件
const deleteComponent = async (component: Component) => {
  try {
    await componentsApi.delete(component.id)
    message.success('删除成功')
    loadComponents()
  } catch (error: any) {
    message.error(error.message || '删除失败')
  }
}

// 格式化日期
const formatDate = (dateStr: string): string => {
  if (!dateStr) return '-'
  return dateStr.replace('T', ' ').substring(0, 19)
}

// 获取分类颜色
const getCategoryColor = (category: string): string => {
  return category === 'agent' ? 'blue' : 'green'
}

// 获取名称颜色
const getNameColor = (name: string): string => {
  const colors: Record<string, string> = {
    agent: 'blue',
    baseline: 'green',
    collector: 'purple',
  }
  return colors[name] || 'default'
}

// 获取插件状态颜色
const getPluginStatusColor = (status: string): string => {
  const colors: Record<string, string> = {
    ready: 'green',
    missing_package: 'red',
    outdated: 'orange',
    default_config: 'blue',
  }
  return colors[status] || 'default'
}

// 获取插件状态标签
const getPluginStatusLabel = (status: string): string => {
  const labels: Record<string, string> = {
    ready: '就绪',
    missing_package: '缺少组件包',
    outdated: '版本不一致',
    default_config: '默认配置',
  }
  return labels[status] || status
}

// 获取插件状态样式类
const getPluginStatusClass = (status: string): string => {
  return `status-${status.replace('_', '-')}`
}

onMounted(() => {
  loadComponents()
  loadPluginStatus()
})
</script>

<style scoped>
.system-components-page {
  width: 100%;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

/* 插件状态区域样式 */
.plugin-status-section {
  margin-bottom: 20px;
  padding: 16px;
  background: #fafafa;
  border-radius: 8px;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #262626;
  margin-bottom: 12px;
  display: flex;
  align-items: center;
}

.plugin-status-cards {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}

.plugin-status-card {
  flex: 1;
  min-width: 200px;
  max-width: 300px;
  padding: 12px 16px;
  background: #fff;
  border-radius: 6px;
  border: 1px solid #e8e8e8;
  transition: all 0.2s;
}

.plugin-status-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.plugin-status-card.status-ready {
  border-left: 3px solid #52c41a;
}

.plugin-status-card.status-missing-package {
  border-left: 3px solid #ff4d4f;
}

.plugin-status-card.status-outdated {
  border-left: 3px solid #faad14;
}

.plugin-status-card.status-default-config {
  border-left: 3px solid #1890ff;
}

.plugin-name {
  margin-bottom: 8px;
  display: flex;
  align-items: center;
}

.plugin-info {
  font-size: 12px;
  color: #595959;
}

.info-row {
  display: flex;
  justify-content: space-between;
  margin-bottom: 4px;
}

.info-row:last-child {
  margin-bottom: 0;
}

.info-row .label {
  color: #8c8c8c;
}

.info-row .value {
  color: #262626;
  font-weight: 500;
}

.empty-status {
  padding: 20px;
  text-align: center;
}

/* 上传表单样式 */
.upload-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.upload-item {
  padding: 12px;
  background: #fafafa;
  border-radius: 6px;
  border: 1px dashed #d9d9d9;
}

.upload-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.upload-label {
  font-size: 13px;
  color: #595959;
  font-weight: 500;
}

.upload-file {
  font-size: 12px;
  color: #1890ff;
  word-break: break-all;
  line-height: 1.4;
}

.upload-hint {
  margin-top: 12px;
  font-size: 12px;
  color: #1890ff;
}

/* 包列表样式 */
.packages-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
</style>
