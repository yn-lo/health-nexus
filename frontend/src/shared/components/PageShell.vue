<script setup lang="ts">
withDefaults(defineProps<{
  background?: string
  padded?: boolean
  bottomNav?: boolean
}>(), {
  background: 'var(--bg-base-default)',
  padded: false,
  bottomNav: true,
})
</script>

<template>
  <main
    class="mx-auto min-h-screen min-h-dvh max-w-[480px]"
    :class="{
      'px-[var(--spacer-16)]': padded,
      'pb-20': bottomNav,
    }"
    :style="{
      'background': background,
    }"
  >
    <slot />
  </main>
</template>
