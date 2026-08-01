<template>
  <van-config-provider :theme-vars="vantThemeVars" theme-vars-scope="global">
    <router-view />
    <DsFeedbackLayer />
  </van-config-provider>
</template>

<script setup lang="ts">
// 患者端根组件
// 主题变量统一从 shared/constants/vant-theme 引入，避免与 staff/App.vue 重复（75 行 → 1 行）
import { vantThemeVars } from '@/shared/constants/vant-theme'
import { DsFeedbackLayer } from '@/shared/components'
</script>
