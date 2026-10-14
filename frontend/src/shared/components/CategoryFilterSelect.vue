<script setup lang="ts">
/** 配置页分类筛选下拉（含「全部」选项 + 各分类计数） */
defineProps<{
  modelValue: string
  options: { value: string; label: string }[]
  counts: Record<string, number>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'change'): void
}>()
</script>

<template>
  <div class="mt-[var(--spacer-12)]">
    <select
      class="ds-input"
      :value="modelValue"
      @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value); emit('change')"
    >
      <option value="all">全部分类</option>
      <option v-for="opt in options.slice(1)" :key="opt.value" :value="opt.value">{{ opt.label }}（{{ counts[opt.value] }}）</option>
    </select>
  </div>
</template>
