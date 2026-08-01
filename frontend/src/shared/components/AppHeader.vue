<script setup lang="ts">
import { ChevronLeft } from '@lucide/vue'

withDefaults(defineProps<{
 title?: string
 showBack?: boolean
 variant?: 'solid' | 'frosted' | 'transparent'
}>(), {
 title: '',
 showBack: true,
 variant: 'solid',
})

defineEmits<{
 back: []
}>()
</script>

<template>
 <header
 class="sticky top-0 z-30 flex h-[calc(var(--layout-header-height)+env(safe-area-inset-top,0px))] items-center px-[var(--spacer-12)] pt-[env(safe-area-inset-top,0px)]"
 :class="{
 'bg-[var(--bg-base-default)]/95 backdrop-blur-md border-b border-[var(--border-neutral-l1)]': variant === 'frosted',
 'bg-[var(--bg-base-default)] border-b border-[var(--border-neutral-l1)]': variant === 'solid',
 'bg-transparent': variant === 'transparent',
 }"
 >
 <!-- 左侧：默认返回按钮 -->
 <div class="flex items-center min-w-[40px] flex-1">
 <button
 v-if="showBack"
 type="button"
 class="flex h-[var(--touch-target-min)] w-[var(--touch-target-min)] items-center justify-center rounded-[var(--radius-8)] text-text hover:bg-[var(--bg-overlay-l1)] active:bg-[var(--bg-overlay-l2)] focus-visible:shadow-[var(--focus-ring)] transition-[background-color_var(--micro-duration)_var(--micro-ease)]"
 aria-label="返回"
 @click="$emit('back')"
 >
 <ChevronLeft :size="22" />
 </button>
 <slot v-else name="left" />
 </div>

 <!-- 中间：标题或自定义内容 -->
 <div class="shrink-0 flex justify-center px-[var(--spacer-8)] h-[40px] items-center">
 <slot name="center">
 <h1
 v-if="title"
 class="truncate text-center font-heading text-heading-sm font-semibold text-text"
 >
 {{ title }}
 </h1>
 </slot>
 </div>

 <!-- 右侧：操作区 -->
 <div class="flex items-center justify-end min-w-[40px] flex-1 gap-[var(--spacer-4)]">
 <slot name="right" />
 </div>
 </header>
</template>
