<script setup lang="ts">
/**
 * DsFilterTabs — 列表页分类筛选 Tab 栏（激活下划线 + 可选计数徽标）
 * 复用点：SafetyRule / SensitiveWord / Account 配置页的筛选 Tab（jscpd clone 提取）
 */
withDefaults(defineProps<{
  options: { value: string; label: string }[]
  modelValue: string
  /** 每项计数；缺省时不渲染徽标 */
  counts?: Record<string, number>
}>(), {
  counts: undefined,
})

defineEmits<{ 'update:modelValue': [string] }>()
</script>

<template>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] mt-[var(--spacer-12)] no-scrollbar overflow-x-auto">
  <button
   v-for="opt in options"
   :key="opt.value"
   type="button"
   :class="modelValue === opt.value
    ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
    : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
   @click="$emit('update:modelValue', opt.value)"
  >
   {{ opt.label }}<span
   v-if="counts && counts[opt.value] !== undefined && counts[opt.value] > 0"
   class="ml-[var(--spacer-4)] inline-flex items-center justify-center min-w-[16px] h-[16px] px-[var(--spacer-4)] rounded-[var(--radius-full)] text-[10px] font-medium leading-none transition-colors"
   :class="modelValue === opt.value ? 'bg-[var(--bg-brand-light)] text-text-brand' : 'bg-[var(--bg-overlay-l1)] text-text-tertiary'"
   >{{ counts[opt.value] }}</span><span v-if="modelValue === opt.value" class="ds-tab-underline" />
  </button>
 </div>
</template>
