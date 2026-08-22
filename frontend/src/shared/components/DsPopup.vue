<script setup lang="ts">
withDefaults(defineProps<{
  show: boolean
  /** 弹出方向：bottom 底部面板 / center 居中弹窗 / right 右侧抽屉 */
  position?: 'bottom' | 'center' | 'right'
  /** right 抽屉宽度 / center 弹窗宽度 */
  width?: string
  height?: string
}>(), {
  position: 'bottom',
})

defineEmits<{ 'update:show': [value: boolean] }>()
</script>

<template>
  <Teleport to="body">
    <div
      v-if="show"
      class="ds-popup-backdrop"
      @click="$emit('update:show', false)"
    />
    <div
      class="ds-popup"
      :class="[
        position === 'center' ? 'ds-popup--center' : '',
        position === 'right' ? 'ds-popup--right' : '',
        show ? '' : 'ds-popup--hidden',
      ]"
      :style="{ width: width ?? undefined, height: height ?? undefined }"
    >
      <slot />
    </div>
  </Teleport>
</template>