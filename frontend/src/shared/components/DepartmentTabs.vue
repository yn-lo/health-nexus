<script setup lang="ts">
/**
 * DepartmentTabs 科室分类选择器（下拉式）
 * 触发按钮显示当前选中科室，点击弹出底部选择面板
 * 触摸目标 ≥44pt（WCAG AAA）
 */
import { computed, ref } from 'vue'
import { Popup as VanPopup } from 'vant'
import { ChevronDown } from '@lucide/vue'

interface TabOption {
  id: number | string
  label: string
}

const props = withDefaults(defineProps<{
  /** 可选项（首项通常为"全部"） */
  options: TabOption[]
  /** 当前选中项 id */
  modelValue: number | string
}>(), {})

const emit = defineEmits<{
  'update:modelValue': [value: number | string]
}>()

const show = ref(false)

const currentLabel = computed(
  () => props.options.find((o) => o.id === props.modelValue)?.label ?? '全部',
)

function onSelect(id: number | string) {
  emit('update:modelValue', id)
  show.value = false
}
</script>

<template>
  <div class="relative shrink-0">
    <button
      type="button"
      class="flex items-center gap-[var(--spacer-4)] min-h-[var(--search-height)] px-[var(--spacer-12)] rounded-[var(--search-radius)] border border-[var(--brand-glow-border)] bg-[var(--bg-base-default)] text-body-base font-medium text-text whitespace-nowrap"
      @click="show = true"
    >
      {{ currentLabel }}
      <ChevronDown :size="16" class="shrink-0 text-icon-tertiary" />
    </button>

    <VanPopup
      :show="show"
      position="bottom"
      round
      :style="{ height: '60vh' }"
      @update:show="show = $event"
    >
      <div class="flex flex-col h-full px-[var(--spacer-16)] pb-[calc(var(--spacer-16)+env(safe-area-inset-bottom,0px))]">
        <header class="pt-[var(--spacer-16)] pb-[var(--spacer-12)]">
          <h2 class="m-0 font-heading text-heading-md font-semibold text-text">选择科室</h2>
        </header>
        <ul class="flex-1 overflow-y-auto list-none m-0 p-0 no-scrollbar">
          <li
            v-for="opt in options"
            :key="opt.id"
            class="flex items-center justify-between py-[var(--spacer-12)] px-[var(--spacer-8)] border-b border-[var(--border-neutral-l1)] text-body-base"
            :class="modelValue === opt.id ? 'text-text-brand font-medium' : 'text-text'"
            @click="onSelect(opt.id)"
          >
            <span>{{ opt.label }}</span>
            <span v-if="modelValue === opt.id" class="text-text-brand font-semibold">✓</span>
          </li>
        </ul>
      </div>
    </VanPopup>
  </div>
</template>
