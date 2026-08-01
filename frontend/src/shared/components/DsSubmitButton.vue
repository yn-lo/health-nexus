<script setup lang="ts">
/**
 * DsSubmitButton — 表单底部提交按钮（block 主按钮 + loading 态）
 * 复用点：Login / Register / ChangePassword 的提交按钮（jscpd clone 提取）
 */
withDefaults(defineProps<{
  loading?: boolean
  text: string
}>(), {
  loading: false,
})
</script>

<template>
 <button
  type="submit"
  class="ds-btn ds-btn--primary ds-btn--block"
  :class="{ 'ds-btn--loading': loading }"
  :disabled="loading"
 >
  <span v-if="loading" class="ds-btn__spinner" />
  {{ text }}
 </button>
</template>
