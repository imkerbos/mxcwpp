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
              class="login-input"
            />
          </a-form-item>
          <a-form-item name="captcha_code">
            <div class="captcha-row">
              <a-input
                v-model:value="form.captcha_code"
                size="large"
                placeholder="验证码"
                :prefix="h(SafetyCertificateOutlined)"
                @pressEnter="handleLogin"
                class="captcha-input"
              />
              <img
                v-if="captchaImage"
                :src="captchaImage"
                alt="验证码"
                class="captcha-image"
                @click="refreshCaptcha"
                title="点击刷新验证码"
              />
              <div v-else class="captcha-placeholder" @click="refreshCaptcha">
                加载中...
              </div>
            </div>
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

        <!-- 强制修改密码弹窗 -->
        <a-modal
          v-model:open="showChangePassword"
          title="首次登录 — 请修改默认密码"
          :closable="false"
          :maskClosable="false"
          :footer="null"
        >
          <p style="color: #86909C; margin-bottom: 16px;">为确保账户安全，请设置新密码（至少 8 位）</p>
          <a-form layout="vertical" @finish="handleChangePassword">
            <a-form-item label="新密码" required>
              <a-input-password
                v-model:value="changePasswordForm.new_password"
                placeholder="请输入新密码（至少 8 位）"
              />
            </a-form-item>
            <a-form-item label="确认新密码" required>
              <a-input-password
                v-model:value="changePasswordForm.confirm_password"
                placeholder="请再次输入新密码"
              />
            </a-form-item>
            <a-form-item>
              <a-button type="primary" html-type="submit" block :loading="changePwdLoading">
                确认修改
              </a-button>
            </a-form-item>
          </a-form>
        </a-modal>
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
import { UserOutlined, LockOutlined, SafetyCertificateOutlined } from '@ant-design/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useSiteConfigStore } from '@/stores/site-config'
import { authApi } from '@/api/auth'
import type { Rule } from 'ant-design-vue/es/form'

const router = useRouter()
const authStore = useAuthStore()
const siteConfigStore = useSiteConfigStore()

const captchaId = ref('')
const captchaImage = ref('')

const refreshCaptcha = async () => {
  try {
    const res = await authApi.getCaptcha()
    captchaId.value = res.captcha_id
    captchaImage.value = res.captcha_image
  } catch (e) {
    console.error('获取验证码失败:', e)
  }
}

// 初始化站点配置和验证码
onMounted(() => {
  siteConfigStore.init()
  refreshCaptcha()
})

const loading = ref(false)
const error = ref('')

const form = reactive({
  username: '',
  password: '',
  captcha_code: '',
})

const rules: Record<string, Rule[]> = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  captcha_code: [{ required: true, message: '请输入验证码', trigger: 'blur' }],
}

const showChangePassword = ref(false)
const changePasswordForm = reactive({
  old_password: '',
  new_password: '',
  confirm_password: '',
})
const changePwdLoading = ref(false)

const handleLogin = async () => {
  error.value = ''
  loading.value = true
  try {
    const response = await authStore.login({
      username: form.username,
      password: form.password,
      captcha_id: captchaId.value,
      captcha_code: form.captcha_code,
    })
    if (response.need_change_password) {
      showChangePassword.value = true
      changePasswordForm.old_password = form.password
    } else {
      router.push('/')
    }
  } catch (err: any) {
    error.value = err.message || '登录失败，请检查用户名和密码'
    // 登录失败后刷新验证码（旧验证码已被消费）
    form.captcha_code = ''
    refreshCaptcha()
  } finally {
    loading.value = false
  }
}

const handleChangePassword = async () => {
  if (changePasswordForm.new_password !== changePasswordForm.confirm_password) {
    error.value = '两次输入的密码不一致'
    return
  }
  if (changePasswordForm.new_password.length < 8) {
    error.value = '新密码长度至少 8 位'
    return
  }
  changePwdLoading.value = true
  error.value = ''
  try {
    await authApi.changePassword({
      old_password: changePasswordForm.old_password,
      new_password: changePasswordForm.new_password,
    })
    showChangePassword.value = false
    router.push('/')
  } catch (err: any) {
    error.value = err.message || '修改密码失败'
  } finally {
    changePwdLoading.value = false
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
  background: linear-gradient(135deg, #165DFF 0%, #1148C2 40%, #0E42D2 100%);
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
  background: linear-gradient(90deg, transparent 0%, #165DFF 50%, transparent 100%);
}

.grid-line.vertical {
  top: 0;
  bottom: 0;
  width: 1px;
  height: auto;
  background: linear-gradient(180deg, transparent 0%, #165DFF 50%, transparent 100%);
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
  background: #165DFF;
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
    box-shadow: 0 0 0 0 rgba(22, 93, 255, 0.4);
  }
  50% {
    transform: scale(1.8);
    opacity: 0.8;
    box-shadow: 0 0 12px 4px rgba(22, 93, 255, 0.2);
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
  color: #165DFF;
  filter: drop-shadow(0 0 20px rgba(22, 93, 255, 0.3));
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
  background: #165DFF;
  flex-shrink: 0;
  box-shadow: 0 0 8px rgba(22, 93, 255, 0.5);
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
  color: #1D2129;
  letter-spacing: 0.5px;
}

.login-subtitle {
  font-size: 14px;
  color: #86909C;
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
  color: #86909C;
  font-size: 16px;
}

.login-button {
  height: 48px;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 500;
  margin-top: 8px;
  background: linear-gradient(135deg, #165DFF 0%, #0E42D2 100%);
  border: none;
  box-shadow: 0 4px 12px rgba(22, 93, 255, 0.35);
  transition: all 0.3s ease;
}

.login-button:hover {
  box-shadow: 0 6px 16px rgba(22, 93, 255, 0.45);
  transform: translateY(-1px);
}

.captcha-row {
  display: flex;
  gap: 12px;
  align-items: center;
}

.captcha-input {
  flex: 1;
  height: 48px;
  border-radius: 8px;
}

.captcha-input :deep(.ant-input) {
  font-size: 15px;
}

.captcha-input :deep(.anticon) {
  color: #86909C;
  font-size: 16px;
}

.captcha-image {
  height: 48px;
  border-radius: 8px;
  cursor: pointer;
  border: 1px solid #e5e6eb;
  flex-shrink: 0;
  transition: opacity 0.2s;
}

.captcha-image:hover {
  opacity: 0.75;
}

.captcha-placeholder {
  height: 48px;
  width: 150px;
  border-radius: 8px;
  border: 1px solid #e5e6eb;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #86909C;
  font-size: 13px;
  cursor: pointer;
  flex-shrink: 0;
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
