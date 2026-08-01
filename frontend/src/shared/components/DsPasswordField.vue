<script setup lang="ts">
/**
 * DsPasswordField — 密码输入框（含显隐切换按钮）
 * 复用点：Login / Register / ChangePassword 的密码输入块（jscpd clone 提取）
 * 约定：显隐切换为「动作式」图标（隐藏时显示 Eye=点击可查看，可见时显示 EyeOff）
 */
import { ref } from 'vue'
import { Lock, Eye, EyeOff } from '@lucide/vue'

withDefaults(defineProps<{
  modelValue: string
  label?: string
  placeholder?: string
  autocomplete?: string
  ariaLabel?: string
  error?: boolean
  /** 图标/按钮配色：brand 用于品牌主色表单（登录页），默认 tertiary */
  tone?: 'tertiary' | 'brand'
}>(), {
  label: '',
  placeholder: '',
  autocomplete: 'current-password',
  ariaLabel: '',
  error: false,
  tone: 'tertiary',
})

defineEmits<{ 'update:modelValue': [string] }>()

const show = ref(false)
</script>

<template>
 <div class="flex flex-col gap-[var(--spacer-8)]">
  <label v-if="label" class="text-body-sm font-medium text-text-secondary">
   {{ label }}
  </label>
  <div class="ds-field-wrap ds-field-wrap--secondary" :class="{ 'ds-field-wrap--error': error }">
   <Lock
    class="h-4 w-4 shrink-0"
    :class="tone === 'brand' ? 'text-icon-brand' : 'text-icon-tertiary'"
   />
   <input
    :value="modelValue"
    :type="show ? 'text' : 'password'"
    :placeholder="placeholder"
    :autocomplete="autocomplete"
    :aria-label="ariaLabel"
    @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
   >
   <button
    type="button"
    class="inline-flex h-6 w-6 shrink-0 items-center justify-center p-0"
    :class="tone === 'brand' ? 'text-icon-brand hover:text-[var(--icon-brand-hover)]' : 'text-icon-tertiary hover:text-icon'"
    :aria-label="show ? '隐藏密码' : '显示密码'"
    @click="show = !show"
   >
    <Eye v-if="!show" class="h-4 w-4" />
    <EyeOff v-else class="h-4 w-4" />
   </button>
  </div>
  <slot />
 </div>
</template>
