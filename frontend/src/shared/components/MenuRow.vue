<script setup lang="ts">
import { ChevronRight } from '@lucide/vue'
import type { Component } from 'vue'

defineProps<{
 icon: Component
 label: string
 danger?: boolean
}>()

defineEmits<{
 click: []
}>()
</script>

<template>
 <button
 type="button"
 class="ds-menu-row"
 @click="$emit('click')"
 >
 <component
 :is="icon"
 class="w-[22px] h-[22px] shrink-0"
 :class="danger ? 'text-[var(--status-error-default)]' : 'text-icon-secondary'"
 />
 <span
 class="flex-1 text-body-base truncate text-left"
 :class="danger ? 'text-[var(--status-error-default)]' : 'text-text'"
 >{{ label }}</span>
 <slot name="value" />
 <ChevronRight v-if="!$slots.value" class="w-5 h-5 text-text-tertiary shrink-0" />
 </button>
</template>
