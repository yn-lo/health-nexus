<script setup lang="ts">
/**
 * DsSwitchRow — 表单「启用/开关」行（label + ds-switch）
 * 复用点：SafetyRule / SensitiveWord / PromptTemplate / Department 等配置表单
 */
withDefaults(defineProps<{
  label?: string
  modelValue: boolean
}>(), {
  label: '启用',
})

defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<template>
  <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
    <span class="text-body-base text-text">{{ label }}</span>
    <label class="ds-switch">
      <input
        type="checkbox"
        class="ds-switch__input"
        :checked="modelValue"
        @change="$emit('update:modelValue', ($event.target as HTMLInputElement).checked)"
      >
      <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
    </label>
  </div>
</template>
