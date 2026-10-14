<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{
  lines: { icon: Component; text: string }[]
}>()
</script>

<template>
  <div
    v-if="lines.length"
    class="mx-[var(--spacer-16)] mb-[var(--spacer-16)] rounded-[var(--radius-12)] bg-[var(--bg-base-default)] px-[var(--spacer-16)] py-[var(--spacer-12)] border border-[var(--border-neutral-l1)]"
  >
    <div class="flex flex-col gap-[var(--spacer-4)]">
      <div
        v-for="line in lines"
        :key="line.text"
        class="flex items-center gap-[var(--spacer-8)] text-body-sm text-text-secondary"
      >
        <component :is="line.icon" class="h-4 w-4 shrink-0 text-icon-tertiary" />
        <span>{{ line.text }}</span>
      </div>
    </div>
  </div>
</template>
