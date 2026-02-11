<template>
  <div class="login-container">
    <!-- 左侧安全主题区域 -->
    <div class="login-left">
      <!-- 网格动画背景 -->
      <div class="grid-background">
        <div class="grid-line" v-for="i in 20" :key="'h'+i" :style="{ top: (i * 5) + '%' }"></div>
        <div class="grid-line vertical" v-for="i in 20" :key="'v'+i" :style="{ left: (i * 5) + '%' }"></div>
      </div>
      <!-- 浮动节点 -->
      <div class="floating-nodes">
        <div class="node" v-for="i in 8" :key="'n'+i" :class="'node-' + i"></div>
      </div>
      <!-- 文案区域 -->
      <div class="left-content">
        <div class="brand-icon">
          <svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" stroke-width="1.5">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
        </div>
        <h2 class="brand-title">Matrix Cloud Security</h2>
        <p class="brand-desc">矩阵云安全平台</p>
        <div class="features">
          <div class="feature-item">
            <div class="feature-dot"></div>
            <span>主机基线合规检查</span>
          </div>
          <div class="feature-item">
            <div class="feature-dot"></div>
            <span>多维度安全评估</span>
          </div>
          <div class="feature-item">
            <div class="feature-dot"></div>
            <span>实时威胁监控告警</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 右侧登录表单区域 -->
    <div class="login-right">
      <div class="login-content">
        <div class="login-header">
          <img
            v-if="siteConfigStore.siteLogo"
            :src="siteConfigStore.siteLogo"
            alt="Logo"
            class="login-logo"
          />
          <h1>{{ siteConfigStore.siteName }}</h1>
          <p class="login-subtitle">安全管理控制台</p>
        </div>

        <a-form
          :model="form"
          :rules="rules"
          @finish="handleLogin"
          class="login-form"
          layout="vertical"
        >
          <a-form-item name="username">
            <a-input
              v-model:value="form.username"
              size="large"
              placeholder="用户名"
              :prefix="h(UserOutlined)"
              class="login-input"
            />
          </a-form-item>
          <a-form-item name="password">
            <a-input-password
              v-model:value="form.password"
              size="large"
              placeholder="密码"
              :prefix="h(LockOutlined)"
              @pressEnter="handleLogin"
              class="login-input"
            />
          </a-form-item>
          <a-form-item>
            <a-button
              type="primary"
              html-type="submit"
              size="large"
              block
              :loading="loading"
              class="login-button"
            >
              登录
            </a-button>
          </a-form-item>
        </a-form>

        <div v-if="error" class="error-message">
          <a-alert :message="error" type="error" show-icon />
        </div>
      </div>

      <!-- 页脚 -->
      <div class="login-footer">
        &copy; {{ new Date().getFullYear() }} {{ siteConfigStore.siteName }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, h, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { UserOutlined, LockOutlined } from '@ant-design/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useSiteConfigStore } from '@/stores/site-config'
import type { Rule } from 'ant-design-vue/es/form'

const router = useRouter()
const authStore = useAuthStore()
const siteConfigStore = useSiteConfigStore()

// 初始化站点配置
onMounted(() => {
  siteConfigStore.init()
})

const loading = ref(false)
const error = ref('')

const form = reactive({
  username: '',
  password: '',
})

const rules: Record<string, Rule[]> = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

const handleLogin = async () => {
  error.value = ''
  loading.value = true
  try {
    await authStore.login({
      username: form.username,
      password: form.password,
    })
    router.push('/')
  } catch (err: any) {
    error.value = err.message || '登录失败，请检查用户名和密码'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  min-height: 100vh;
  width: 100%;
}

/* 左侧安全主题区域 */
.login-left {
  flex: 0 0 40%;
  position: relative;
  background: linear-gradient(135deg, #001529 0%, #002140 40%, #003a70 100%);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 网格背景 */
.grid-background {
  position: absolute;
  inset: 0;
  opacity: 0.08;
}

.grid-line {
  position: absolute;
  left: 0;
  right: 0;
  height: 1px;
  background: linear-gradient(90deg, transparent 0%, #1890ff 50%, transparent 100%);
}

.grid-line.vertical {
  top: 0;
  bottom: 0;
  width: 1px;
  height: auto;
  background: linear-gradient(180deg, transparent 0%, #1890ff 50%, transparent 100%);
}

/* 浮动节点 */
.floating-nodes {
  position: absolute;
  inset: 0;
}

.node {
  position: absolute;
  width: 6px;
  height: 6px;
  background: #1890ff;
  border-radius: 50%;
  opacity: 0.4;
  animation: pulse 3s ease-in-out infinite;
}

.node-1 { top: 15%; left: 20%; animation-delay: 0s; }
.node-2 { top: 30%; left: 60%; animation-delay: 0.5s; }
.node-3 { top: 50%; left: 35%; animation-delay: 1s; }
.node-4 { top: 70%; left: 70%; animation-delay: 1.5s; }
.node-5 { top: 25%; left: 80%; animation-delay: 2s; }
.node-6 { top: 60%; left: 15%; animation-delay: 0.8s; }
.node-7 { top: 80%; left: 45%; animation-delay: 1.2s; }
.node-8 { top: 40%; left: 85%; animation-delay: 1.8s; }

@keyframes pulse {
  0%, 100% {
    transform: scale(1);
    opacity: 0.4;
    box-shadow: 0 0 0 0 rgba(24, 144, 255, 0.4);
  }
  50% {
    transform: scale(1.8);
    opacity: 0.8;
    box-shadow: 0 0 12px 4px rgba(24, 144, 255, 0.2);
  }
}

/* 左侧文案 */
.left-content {
  position: relative;
  z-index: 1;
  text-align: center;
  color: #ffffff;
  padding: 40px;
}

.brand-icon {
  margin-bottom: 24px;
  color: #1890ff;
  filter: drop-shadow(0 0 20px rgba(24, 144, 255, 0.3));
}

.brand-title {
  font-size: 28px;
  font-weight: 600;
  color: #ffffff;
  margin: 0 0 8px 0;
  letter-spacing: 1px;
}

.brand-desc {
  font-size: 16px;
  color: rgba(255, 255, 255, 0.65);
  margin: 0 0 40px 0;
  letter-spacing: 2px;
}

.features {
  display: flex;
  flex-direction: column;
  gap: 16px;
  align-items: flex-start;
  max-width: 240px;
  margin: 0 auto;
}

.feature-item {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
  color: rgba(255, 255, 255, 0.75);
}

.feature-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #1890ff;
  flex-shrink: 0;
  box-shadow: 0 0 8px rgba(24, 144, 255, 0.5);
}

/* 右侧登录表单区域 */
.login-right {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  background: #ffffff;
  padding: 40px;
  position: relative;
}

.login-content {
  width: 100%;
  max-width: 400px;
}

.login-header {
  text-align: center;
  margin-bottom: 40px;
}

.login-header h1 {
  margin: 0 0 4px 0;
  font-size: 26px;
  font-weight: 600;
  color: #001529;
  letter-spacing: 0.5px;
}

.login-subtitle {
  font-size: 14px;
  color: rgba(0, 0, 0, 0.45);
  margin: 0;
}

.login-logo {
  width: 56px;
  height: 56px;
  object-fit: contain;
  margin-bottom: 16px;
}

.login-form {
  margin-bottom: 24px;
}

.login-input {
  height: 48px;
  border-radius: 8px;
}

.login-input :deep(.ant-input) {
  font-size: 15px;
}

.login-input :deep(.anticon) {
  color: #8c8c8c;
  font-size: 16px;
}

.login-button {
  height: 48px;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 500;
  margin-top: 8px;
  background: linear-gradient(135deg, #1890ff 0%, #096dd9 100%);
  border: none;
  box-shadow: 0 4px 12px rgba(24, 144, 255, 0.35);
  transition: all 0.3s ease;
}

.login-button:hover {
  box-shadow: 0 6px 16px rgba(24, 144, 255, 0.45);
  transform: translateY(-1px);
}

.error-message {
  margin-top: 16px;
}

/* 页脚 */
.login-footer {
  position: absolute;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  font-size: 13px;
  color: rgba(0, 0, 0, 0.35);
  text-align: center;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .login-container {
    flex-direction: column;
  }

  .login-left {
    flex: 0 0 200px;
    min-height: 200px;
  }

  .brand-title {
    font-size: 20px;
  }

  .features {
    display: none;
  }

  .login-right {
    flex: 1;
    padding: 24px;
  }

  .login-content {
    max-width: 100%;
  }
}
</style>
