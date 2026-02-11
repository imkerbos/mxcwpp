<template>
  <a-config-provider :theme="themeConfig">
    <router-view />
  </a-config-provider>
</template>

<script setup lang="ts">
import { theme } from 'ant-design-vue'

const themeConfig = {
  algorithm: theme.defaultAlgorithm,
  token: {
    colorPrimary: '#1890ff',
    borderRadius: 6,
    colorBgContainer: '#ffffff',
    colorBgLayout: '#f0f2f5',
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif",
  },
  components: {
    Button: {
      borderRadius: 6,
    },
    Card: {
      borderRadiusLG: 8,
    },
    Input: {
      borderRadius: 6,
    },
    Select: {
      borderRadius: 6,
    },
    Table: {
      borderRadiusLG: 8,
    },
    Modal: {
      borderRadiusLG: 8,
    },
    Tag: {
      borderRadiusSM: 4,
    },
  },
}
</script>

<style>
#app {
  width: 100%;
  height: 100vh;
}
</style>
