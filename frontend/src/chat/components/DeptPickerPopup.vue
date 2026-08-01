<script setup lang="ts">
import { computed, ref } from 'vue'
import { Popup as VanPopup } from 'vant'
import { Search } from '@lucide/vue'
import type { Department } from '@/shared'

const props = defineProps<{
  show: boolean
  departments: Department[]
  selectedId: number
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  select: [id: number]
}>()

const deptSearch = ref('')

const filteredDepartments = computed(() => {
  const q = deptSearch.value.trim().toLowerCase()
  if (!q) return props.departments
  return props.departments.filter((d) => d.name.toLowerCase().includes(q))
})

function onOpen() {
  deptSearch.value = ''
}

function onSelect(id: number) {
  emit('select', id)
  emit('update:show', false)
}
</script>

<template>
  <VanPopup
    :show="show"
    position="bottom"
    round
    :style="{ height: '60vh' }"
    @update:show="emit('update:show', $event)"
    @open="onOpen"
  >
    <div class="flex flex-col h-full px-[var(--spacer-16)] pb-[calc(var(--spacer-16)+env(safe-area-inset-bottom,0px))]">
      <header class="pt-[var(--spacer-16)] pb-[var(--spacer-12)]">
        <h2 class="m-0 font-heading text-heading-md font-semibold text-text">选择科室</h2>
      </header>
      <div class="flex items-center gap-[var(--spacer-12)] px-[var(--spacer-12)] py-[var(--spacer-12)] mb-[var(--spacer-12)] rounded-[var(--radius-8)] bg-[var(--bg-base-secondary)]">
        <Search :size="16" class="shrink-0 text-icon-tertiary" />
        <input
          v-model="deptSearch"
          class="flex-1 border-none outline-none bg-transparent text-body-base text-text"
          placeholder="搜索科室..."
        >
      </div>
      <ul class="flex-1 overflow-y-auto list-none m-0 p-0 no-scrollbar">
        <li
          v-for="dept in filteredDepartments"
          :key="dept.id"
          class="flex items-center justify-between py-[var(--spacer-12)] px-[var(--spacer-8)] border-b border-[var(--border-neutral-l1)] text-body-base"
          :class="dept.id === selectedId ? 'text-text-brand font-medium' : 'text-text'"
          @click="onSelect(dept.id)"
        >
          <span>{{ dept.name }}</span>
          <span v-if="dept.id === selectedId" class="text-text-brand font-semibold">✓</span>
        </li>
      </ul>
    </div>
  </VanPopup>
</template>
