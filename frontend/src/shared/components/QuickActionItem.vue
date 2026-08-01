<script setup lang="ts">
/**
 * QuickActionItem — 快捷操作项
 * 对照设计稿 staff-dashboard.html 462-489 行
 * 卡片 + 48x48 圆角彩色背景图标 + 标签 + 可选 badge
 */
import type { Component } from 'vue'

const props = withDefaults(defineProps<{
  icon: Component
  label: string
  iconColor?: string
  iconBg?: string
  badge?: number | string
}>(), {
  iconColor: '',
  iconBg: '',
})

defineEmits<{
  click: []
}>()
</script>

<template>
  <button
    type="button"
    class="ds-action-card"
    @click="$emit('click')"
  >
    <span
      v-if="badge !== undefined"
      class="ds-badge"
    >{{ badge }}</span>
    <div
      class="ds-action-card__icon"
      :style="props.iconBg ? { background: props.iconBg } : undefined"
    >
      <component :is="icon" class="w-6 h-6" :style="props.iconColor ? { color: props.iconColor } : undefined" />
    </div>
    <span class="ds-action-card__label">{{ label }}</span>
  </button>
</template>
