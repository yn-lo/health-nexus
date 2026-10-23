<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useBrandStore } from '@/stores/brand'

withDefaults(defineProps<{
  size?: 'sm' | 'md' | 'lg'
  orientation?: 'vertical' | 'horizontal'
  /** 隐藏品牌文字，仅显示图标 */
  hideName?: boolean
}>(), {
  size: 'md',
  orientation: 'vertical',
  hideName: false,
})

// 品牌标识来自全局 store；未配置 Logo 时 store 内回退到内置图
const { brand, logoUrl } = storeToRefs(useBrandStore())
</script>

<template>
  <div
    class="brand-logo"
    :class="[
      orientation === 'vertical' ? 'flex-col' : 'flex-row items-center',
      `brand-logo--${size}`,
    ]"
  >
    <div class="brand-logo__icon-wrapper">
      <img
        :src="logoUrl"
        :alt="`${brand.name} Logo`"
        class="brand-logo__img"
      >
    </div>
    <span v-if="!hideName" class="brand-logo__name">{{ brand.name }}</span>
  </div>
</template>
