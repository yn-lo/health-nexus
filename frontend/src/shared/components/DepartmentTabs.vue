<script setup lang="ts">
/**
 * DepartmentTabs 科室分类选择器
 * 横向滚动 tabs，下划线指示器，触摸目标 ≥44pt（WCAG AAA）
 * 用于按科室筛选内容（文章列表等）
 */
interface TabOption {
  id: number | string
  label: string
}

withDefaults(defineProps<{
  /** 可选项（首项通常为"全部"） */
  options: TabOption[]
  /** 当前选中项 id */
  modelValue: number | string
  /** sticky 定位的 top 偏移（CSS 长度），不传则不 sticky */
  stickyTop?: string
}>(), {
  stickyTop: '',
})

defineEmits<{
  'update:modelValue': [value: number | string]
}>()
</script>

<template>
  <nav
    class="z-20 bg-[var(--bg-base-default)] border-b border-[var(--border-neutral-l1)]"
    :class="stickyTop ? 'sticky' : ''"
    :style="stickyTop ? { top: stickyTop } : undefined"
  >
    <div class="flex flex-nowrap overflow-x-auto gap-[var(--spacer-4)] no-scrollbar px-[var(--spacer-16)] py-[var(--spacer-8)]">
      <button
        v-for="opt in options"
        :key="opt.id"
        type="button"
        class="shrink-0 relative min-h-[var(--touch-target-min)] px-[var(--spacer-12)] flex items-center text-body-base font-medium transition-colors whitespace-nowrap"
        :class="modelValue === opt.id
          ? 'text-text-brand'
          : 'text-text-secondary hover:text-text'"
        @click="$emit('update:modelValue', opt.id)"
      >
        {{ opt.label }}
        <span
          class="absolute bottom-[var(--spacer-4)] left-[var(--spacer-12)] right-[var(--spacer-12)] h-[2px] rounded-[var(--radius-full)] transition-all duration-200"
          :class="modelValue === opt.id ? 'bg-[var(--bg-brand)] opacity-100' : 'opacity-0'"
        />
      </button>
    </div>
  </nav>
</template>
