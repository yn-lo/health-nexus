<script setup lang="ts">
/**
 * BrandFavicon — 将配置的 favicon 注入 <head>。
 * 用 Vue Teleport 而非 document API（架构约束 AC-ARCH-FE-03 禁止直接 DOM 操作）。
 * 未配置 favicon_url 时不渲染，保留浏览器默认图标。
 */
import { storeToRefs } from 'pinia'
import { useBrandStore } from '@/stores/brand'

const { brand } = storeToRefs(useBrandStore())
</script>

<template>
  <Teleport to="head">
    <link v-if="brand.favicon_url" rel="icon" :href="brand.favicon_url">
  </Teleport>
</template>
