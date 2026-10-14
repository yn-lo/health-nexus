<script setup lang="ts">
import { CircleAlert } from '@lucide/vue'

defineProps<{ message: string }>()
</script>

<template>
  <div class="ds-alert ds-alert--error" role="alert">
    <CircleAlert class="icon" />
    <span>{{ message }}</span>
  </div>
</template>
