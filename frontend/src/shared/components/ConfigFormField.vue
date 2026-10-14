<script setup lang="ts">
/** 配置表单字段：标签（可带必填星号）+ 控件插槽 */
defineProps<{
  label: string
  required?: boolean
}>()
</script>

<template>
  <div class="flex flex-col gap-[var(--spacer-4)]">
    <span class="text-body-sm text-text-secondary">{{ label }}<span v-if="required" class="text-[var(--status-error-default)]">*</span></span>
    <slot />
  </div>
</template>
