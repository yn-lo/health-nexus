<script setup lang="ts">
/**
 * DsActionSheet 自研底部动作面板
 * 动作列表 + 可选中途/取消，复用 ds-popup 底部弹层样式
 */
interface ActionItem {
  name: string
  color?: string
  disabled?: boolean
}

const props = withDefaults(defineProps<{
  show: boolean
  actions: ActionItem[]
  cancelText?: string
  /** 点击动作后自动关闭 */
  closeOnClickAction?: boolean
}>(), {
  cancelText: '取消',
  closeOnClickAction: false,
})

const emit = defineEmits<{
  'update:show': [value: boolean]
  select: [action: ActionItem]
}>()

function onSelect(action: ActionItem) {
  emit('select', action)
  if (action.disabled) return
  if (props.closeOnClickAction) emit('update:show', false)
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="ds-popup-backdrop" @click="emit('update:show', false)" />
    <div class="ds-popup ds-action-sheet" :class="show ? '' : 'ds-popup--hidden'">
      <ul class="ds-action-sheet__list">
        <li
          v-for="action in actions"
          :key="action.name"
          class="ds-action-sheet__item"
          :class="{
            'ds-action-sheet__item--danger': action.color === 'danger',
            'ds-action-sheet__item--disabled': action.disabled,
          }"
          :aria-disabled="action.disabled || undefined"
          @click="onSelect(action)"
        >
          {{ action.name }}
        </li>
      </ul>
      <button type="button" class="ds-action-sheet__cancel" @click="emit('update:show', false)">
        {{ cancelText }}
      </button>
    </div>
  </Teleport>
</template>