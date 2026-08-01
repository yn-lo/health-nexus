<script setup lang="ts">
defineProps<{ show: boolean }>()
defineEmits<{ 'update:show': [value: boolean] }>()
</script>

<template>
  <Teleport to="body">
    <div
      v-if="show"
      class="ds-popup-backdrop"
      @click="$emit('update:show', false)"
    />
    <div :class="show ? 'ds-popup' : 'ds-popup ds-popup--hidden'">
      <slot />
    </div>
  </Teleport>
</template>
