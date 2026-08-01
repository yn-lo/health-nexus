<script setup lang="ts">

withDefaults(defineProps<{
  text?: string
}>(), {
  text: '暂无数据',
})
</script>

<template>
  <div class="py-[var(--spacer-16)] text-center text-body-md text-text-tertiary">
    {{ text }}
  </div>
</template>
