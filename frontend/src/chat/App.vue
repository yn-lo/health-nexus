<template>
  <router-view />
  <DsFeedbackLayer />
</template>

<script setup lang="ts">
// 患者端根组件
// 主题令牌统一由 tokens.css 定义，无需第三方主题注入
import { DsFeedbackLayer } from '@/shared/components'
</script>