<script setup lang="ts">
import type { Component } from 'vue'

interface TabBarItem {
  key: string
  label: string
  iconComponent: Component
}

defineProps<{
  modelValue: string
  items: TabBarItem[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

function onSelect(key: string) {
  emit('update:modelValue', key)
}
</script>

<template>
  <nav class="ds-tabbar">
    <button
      v-for="item in items"
      :key="item.key"
      class="ds-tabbar__item"
      :class="{ 'ds-tabbar__item--active': modelValue === item.key }"
      @click="onSelect(item.key)"
    >
      <span class="ds-tabbar__icon">
        <component
          :is="item.iconComponent"
          :size="20"
        />
      </span>
      <span>{{ item.label }}</span>
    </button>
  </nav>
</template>
