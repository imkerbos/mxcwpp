# 任务详情页改进方案

## 改进目标

1. 添加主机维度的统计和展示
2. 按主机分组查看检查结果
3. 添加导出功能

## 需要修改的文件

`ui/src/views/Tasks/index.vue`

## 改进内容

### 1. 添加主机执行统计（在第 330 行附近）

在"执行结果统计"部分添加主机维度的统计：

```vue
<!-- 主机执行统计 -->
<a-alert
  v-if="hostExecutionStats.total > 0"
  :message="`主机执行情况: 已完成 ${hostExecutionStats.completed}/${hostExecutionStats.total} 台`"
  :type="hostExecutionStats.completed === hostExecutionStats.total ? 'success' : 'warning'"
  show-icon
  style="margin-bottom: 16px;"
>
  <template #description>
    <div>
      <span style="margin-right: 16px;">✓ 已完成: {{ hostExecutionStats.completed }} 台</span>
      <span v-if="hostExecutionStats.failed > 0" style="margin-right: 16px; color: #cf1322;">✗ 未完成: {{ hostExecutionStats.failed }} 台</span>
    </div>
  </template>
</a-alert>
```

### 2. 添加主机筛选和分组展示（在第 380 行附近）

在"详细结果表格"之前添加主机筛选器和视图切换：

```vue
<div class="detailed-results" style="margin-top: 16px;">
  <!-- 视图切换 -->
  <div style="margin-bottom: 12px; display: flex; justify-content: space-between; align-items: center;">
    <div>
      <span style="margin-right: 8px;">视图:</span>
      <a-radio-group v-model:value="resultViewMode" size="small">
        <a-radio-button value="all">全部检查项</a-radio-button>
        <a-radio-button value="by-host">按主机分组</a-radio-button>
      </a-radio-group>
    </div>
    <div>
      <a-button type="primary" size="small" @click="handleExportResults" :loading="exporting">
        <template #icon>
          <DownloadOutlined />
        </template>
        导出结果
      </a-button>
    </div>
  </div>

  <!-- 主机筛选器（按主机分组时显示） -->
  <div v-if="resultViewMode === 'by-host'" style="margin-bottom: 12px;">
    <span style="margin-right: 8px;">选择主机:</span>
    <a-select
      v-model:value="selectedHostId"
      style="width: 300px;"
      placeholder="选择主机"
      allow-clear
    >
      <a-select-option value="">全部主机</a-select-option>
      <a-select-option
        v-for="host in hostResultsList"
        :key="host.host_id"
        :value="host.host_id"
      >
        {{ host.hostname }} ({{ host.completed_rules }}/{{ selectedTask?.total_rule_count || 0 }})
        <a-tag
          :color="host.completed_rules === selectedTask?.total_rule_count ? 'green' : 'orange'"
          size="small"
          style="margin-left: 8px;"
        >
          {{ host.completed_rules === selectedTask?.total_rule_count ? '完成' : '未完成' }}
        </a-tag>
      </a-select-option>
    </a-select>
  </div>

  <!-- 状态筛选 -->
  <div class="result-filter" style="margin-bottom: 12px;">
    <span style="margin-right: 8px;">筛选:</span>
    <a-radio-group v-model:value="resultFilter" size="small">
      <a-radio-button value="all">全部 ({{ taskResultStats.total }})</a-radio-button>
      <a-radio-button value="fail">失败 ({{ taskResultStats.fail }})</a-radio-button>
      <a-radio-button value="error">错误 ({{ taskResultStats.error }})</a-radio-button>
      <a-radio-button value="pass">通过 ({{ taskResultStats.pass }})</a-radio-button>
    </a-radio-group>
  </div>

  <!-- 按主机分组展示 -->
  <div v-if="resultViewMode === 'by-host' && !selectedHostId">
    <a-collapse v-model:activeKey="activeHostKeys" accordion>
      <a-collapse-panel
        v-for="host in hostResultsList"
        :key="host.host_id"
        :header="`${host.hostname} - ${host.completed_rules}/${selectedTask?.total_rule_count || 0} 项`"
      >
        <template #extra>
          <a-space>
            <a-tag color="green">通过: {{ host.passed }}</a-tag>
            <a-tag color="red">失败: {{ host.failed }}</a-tag>
            <a-tag v-if="host.error > 0" color="orange">错误: {{ host.error }}</a-tag>
          </a-space>
        </template>
        <a-table
          :columns="detailedResultColumns"
          :data-source="getHostResults(host.host_id)"
          :pagination="false"
          row-key="rule_id"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === 'pass' ? 'green' : record.status === 'fail' ? 'red' : 'orange'">
                {{ record.status === 'pass' ? '通过' : record.status === 'fail' ? '失败' : '错误' }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'failure_reason'">
              <template v-if="record.status === 'fail' || record.status === 'error'">
                <a-tooltip v-if="record.actual || record.expected" placement="topLeft">
                  <template #title>
                    <div>
                      <div v-if="record.expected"><strong>期望值:</strong> {{ record.expected }}</div>
                      <div v-if="record.actual"><strong>实际值:</strong> {{ record.actual }}</div>
                    </div>
                  </template>
                  <span class="failure-reason">
                    {{ record.actual ? `实际: ${record.actual.slice(0, 50)}${record.actual.length > 50 ? '...' : ''}` : '检查失败' }}
                  </span>
                </a-tooltip>
                <span v-else class="failure-reason">检查失败</span>
              </template>
              <span v-else style="color: #999;">-</span>
            </template>
          </template>
        </a-table>
      </a-collapse-panel>
    </a-collapse>
  </div>

  <!-- 原有的表格展示 -->
  <a-table
    v-else
    :columns="detailedResultColumns"
    :data-source="filteredDetailedResults"
    :loading="detailResultsLoading"
    :pagination="detailPagination"
    @change="handleDetailTableChange"
    row-key="rule_id"
    size="small"
  >
    <!-- 原有的 bodyCell 模板 -->
  </a-table>
</div>
```

### 3. 添加数据和方法（在 script 部分）

```typescript
// 导入图标
import { DownloadOutlined } from '@ant-design/icons-vue'

// 添加响应式数据
const resultViewMode = ref<'all' | 'by-host'>('all') // 视图模式
const selectedHostId = ref<string>('') // 选中的主机ID
const activeHostKeys = ref<string[]>([]) // 展开的主机面板
const exporting = ref(false) // 导出中

// 主机执行统计
const hostExecutionStats = computed(() => {
  if (!selectedTask.value || !hostResultsList.value.length) {
    return { total: 0, completed: 0, failed: 0 }
  }

  const total = selectedTask.value.dispatched_host_count || 0
  const completed = hostResultsList.value.length
  const failed = total - completed

  return { total, completed, failed }
})

// 主机结果列表
const hostResultsList = ref<Array<{
  host_id: string
  hostname: string
  completed_rules: number
  passed: number
  failed: number
  error: number
}>>([])

// 获取主机结果列表
const fetchHostResults = async () => {
  if (!selectedTask.value) return

  try {
    const response = await resultsApi.getResults({
      task_id: selectedTask.value.task_id,
      page: 1,
      page_size: 10000, // 获取所有结果
    })

    // 按主机分组统计
    const hostMap = new Map()
    response.data.items.forEach((result: any) => {
      if (!hostMap.has(result.host_id)) {
        hostMap.set(result.host_id, {
          host_id: result.host_id,
          hostname: result.hostname || result.host_id,
          completed_rules: 0,
          passed: 0,
          failed: 0,
          error: 0,
        })
      }

      const host = hostMap.get(result.host_id)
      host.completed_rules++
      if (result.status === 'pass') host.passed++
      else if (result.status === 'fail') host.failed++
      else host.error++
    })

    hostResultsList.value = Array.from(hostMap.values())
  } catch (error) {
    console.error('获取主机结果失败:', error)
  }
}

// 获取指定主机的结果
const getHostResults = (hostId: string) => {
  return detailedResults.value.filter((r: any) => r.host_id === hostId)
}

// 导出结果
const handleExportResults = async () => {
  if (!selectedTask.value) return

  exporting.value = true
  try {
    // 获取所有结果
    const response = await resultsApi.getResults({
      task_id: selectedTask.value.task_id,
      page: 1,
      page_size: 10000,
    })

    // 转换为 CSV 格式
    const headers = ['主机ID', '主机名', '规则ID', '规则标题', '状态', '期望值', '实际值', '检查时间']
    const rows = response.data.items.map((item: any) => [
      item.host_id,
      item.hostname || '',
      item.rule_id,
      item.title || '',
      item.status === 'pass' ? '通过' : item.status === 'fail' ? '失败' : '错误',
      item.expected || '',
      item.actual || '',
      item.checked_at || '',
    ])

    const csvContent = [
      headers.join(','),
      ...rows.map((row: any[]) => row.map(cell => `"${cell}"`).join(',')),
    ].join('\n')

    // 下载文件
    const blob = new Blob(['\ufeff' + csvContent], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `task_${selectedTask.value.task_id}_results.csv`
    link.click()

    message.success('导出成功')
  } catch (error) {
    console.error('导出失败:', error)
    message.error('导出失败')
  } finally {
    exporting.value = false
  }
}

// 在 loadTaskDetail 函数中调用
const loadTaskDetail = async (task: ScanTask) => {
  // ... 原有代码 ...

  // 加载主机结果列表
  await fetchHostResults()
}
```

## 实施步骤

1. 备份原文件
2. 在第 330 行附近添加主机执行统计
3. 在第 380 行附近替换详细结果表格部分
4. 在 script 部分添加新的数据和方法
5. 在 imports 中添加 `DownloadOutlined` 图标
6. 测试功能

## 预期效果

1. **主机执行统计**：清楚显示有多少台主机完成了检查
2. **按主机分组**：可以折叠展开查看每台主机的检查结果
3. **主机筛选**：可以选择查看特定主机的结果
4. **导出功能**：可以导出 CSV 格式的完整结果
