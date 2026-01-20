<template>
  <div class="system-collection-page">
    <div class="page-header">
      <h2>平台授权</h2>
    </div>

    <div class="license-info">
      <a-result
        status="success"
        title="开源版本"
        sub-title="当前版本为开源版本，无需授权"
      >
        <template #extra>
          <a-typography>
            <a-typography-paragraph>
              <strong>版本信息：</strong>
            </a-typography-paragraph>
            <a-typography-paragraph>
              Matrix Cloud Security Platform 是一个开源的安全基线检查平台，当前版本为社区版（Community Edition）。
            </a-typography-paragraph>
            <a-typography-paragraph>
              <strong>授权说明：</strong>
            </a-typography-paragraph>
            <a-typography-paragraph>
              • 开源版本完全免费使用，无需任何授权<br>
              • 支持所有核心功能，包括基线检查、资产采集、策略管理等<br>
              • 可自由部署和使用，无功能限制
            </a-typography-paragraph>
            <a-typography-paragraph>
              <strong>更多信息：</strong>
            </a-typography-paragraph>
            <a-typography-paragraph>
              • GitHub 仓库: <a href="https://github.com/imkerbos/mxsec-platform" target="_blank">https://github.com/imkerbos/mxsec-platform</a><br>
              • Powered by Kerbos
            </a-typography-paragraph>
          </a-typography>
        </template>
      </a-result>
    </div>
  </div>
</template>

<script setup lang="ts">
// 平台授权页面 - 当前版本为开源版本，无需授权
</script>

<style scoped>
.system-collection-page {
  width: 100%;
  padding: 24px;
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.license-info {
  padding: 40px 0;
}

.license-info :deep(.ant-typography) {
  text-align: left;
  max-width: 600px;
  margin: 0 auto;
}

.license-info :deep(.ant-typography-paragraph) {
  margin-bottom: 16px;
  color: #595959;
  line-height: 1.8;
}

.license-info :deep(.ant-typography-paragraph strong) {
  color: #262626;
  font-weight: 600;
}
</style>
