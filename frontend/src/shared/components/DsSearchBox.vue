<script setup lang="ts">
/**
 * DsSearchBox — 列表页搜索框
 * SafetyRule / SensitiveWord / Account / Department 配置页的搜索框（jscpd clone 提取）
 */
import { Search } from '@lucide/vue'

withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
}>(), {
  placeholder: '搜索',
})

defineEmits<{ 'update:modelValue': [string] }>()
</script>

<template>
 <div class="ds-search-box">
  <Search class="h-4 w-4 shrink-0 text-icon-brand" />
  <input
   :value="modelValue"
   type="text"
   :placeholder="placeholder"
   class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary"
   @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
  >
 </div>
</template>
