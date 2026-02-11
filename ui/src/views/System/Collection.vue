<template>
  <div class="system-collection-page">
    <div class="page-header">
      <h2>平台授权</h2>
    </div>

    <div class="license-card">
      <!-- 授权状态 -->
      <div class="license-status">
        <div class="status-icon">
          <CheckCircleFilled />
        </div>
        <h3 class="status-title">开源版本</h3>
        <p class="status-desc">当前版本为开源版本，无需授权</p>
      </div>

      <!-- 信息区域 -->
      <div class="license-details">
        <div class="detail-section">
          <div class="section-title">
            <InfoCircleOutlined />
            <span>版本信息</span>
          </div>
          <p class="section-content">
            Matrix Cloud Security Platform 是一个开源的安全基线检查平台，当前版本为社区版（Community Edition）。
          </p>
        </div>

        <a-divider />

        <div class="detail-section">
          <div class="section-title">
            <SafetyCertificateOutlined />
            <span>授权说明</span>
          </div>
          <div class="feature-list">
            <div class="feature-item">
              <CheckOutlined class="feature-check" />
              <span>开源版本完全免费使用，无需任何授权</span>
            </div>
            <div class="feature-item">
              <CheckOutlined class="feature-check" />
              <span>支持所有核心功能，包括基线检查、资产采集、策略管理等</span>
            </div>
            <div class="feature-item">
              <CheckOutlined class="feature-check" />
              <span>可自由部署和使用，无功能限制</span>
            </div>
          </div>
        </div>

        <a-divider />

        <div class="detail-section">
          <div class="section-title">
            <LinkOutlined />
            <span>更多信息</span>
          </div>
          <div class="link-list">
            <div class="link-item">
              <GithubOutlined />
              <a href="https://github.com/imkerbos/mxsec-platform" target="_blank">
                github.com/imkerbos/mxsec-platform
              </a>
            </div>
            <div class="link-item">
              <UserOutlined />
              <span>Powered by Kerbos</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  CheckCircleFilled,
  CheckOutlined,
  InfoCircleOutlined,
  SafetyCertificateOutlined,
  LinkOutlined,
  GithubOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
</script>

<style scoped>
.system-collection-page {
  width: 100%;
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.license-card {
  max-width: 680px;
  margin: 0 auto;
}

/* 授权状态 */
.license-status {
  text-align: center;
  padding: 48px 24px 40px;
  background: linear-gradient(135deg, #f6ffed 0%, #e6f7ff 100%);
  border-radius: 12px;
  margin-bottom: 32px;
}

.status-icon {
  font-size: 56px;
  color: #52c41a;
  margin-bottom: 16px;
  line-height: 1;
}

.status-title {
  font-size: 24px;
  font-weight: 600;
  color: #262626;
  margin: 0 0 8px 0;
}

.status-desc {
  font-size: 15px;
  color: rgba(0, 0, 0, 0.45);
  margin: 0;
}

/* 详情区域 */
.license-details {
  background: #fff;
  border-radius: 12px;
  padding: 32px;
  border: 1px solid #f0f0f0;
}

.detail-section {
  padding: 4px 0;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: #262626;
  margin-bottom: 16px;
}

.section-title :deep(.anticon) {
  color: #1890ff;
  font-size: 18px;
}

.section-content {
  color: #595959;
  line-height: 1.8;
  margin: 0;
  padding-left: 26px;
}

/* 功能列表 */
.feature-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-left: 26px;
}

.feature-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  color: #595959;
  line-height: 1.6;
}

.feature-check {
  color: #52c41a;
  font-size: 14px;
  margin-top: 4px;
  flex-shrink: 0;
}

/* 链接列表 */
.link-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-left: 26px;
}

.link-item {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #595959;
  font-size: 14px;
}

.link-item :deep(.anticon) {
  font-size: 16px;
  color: #8c8c8c;
}

.link-item a {
  color: #1890ff;
  text-decoration: none;
}

.link-item a:hover {
  text-decoration: underline;
}
</style>
