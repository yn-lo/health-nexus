<script setup lang="ts">
/**
 * PasswordStrength — 密码强度指示器
 * 基于 usePasswordStrength composable，渲染强度条 + 文字标签
 * 支持配置段数（Register 用 3 段，PasswordReset 用 4 段）
 */
import { computed } from 'vue'
import { usePasswordStrength } from '@/shared/composables/usePasswordStrength'

const props = withDefaults(defineProps<{
 password: string
 segments?: number
}>(), {
 segments: 3,
})

const { level, label } = usePasswordStrength(() => props.password)

const filledCount = computed(() => {
 if (level.value === 'weak') return 1
 if (level.value === 'medium') return 2
 return props.segments
})

const activeColor = computed(() => {
 if (level.value === 'weak') return 'var(--status-error-default)'
 if (level.value === 'medium') return 'var(--status-alert-default)'
 if (level.value === 'strong') return 'var(--status-success-default)'
 return 'var(--bg-overlay-l3)'
})

const barColors = computed(() =>
 Array.from({ length: props.segments }, (_, i) =>
 i < filledCount.value ? activeColor.value : 'var(--bg-overlay-l3)',
 ),
)
</script>

<template>
 <div v-if="password.length > 0" class="flex items-center gap-[var(--spacer-4)]">
 <div class="flex flex-1 gap-[var(--spacer-4)]">
 <span
 v-for="(color, idx) in barColors"
 :key="idx"
 class="h-1 flex-1 rounded-[var(--radius-full)]"
 :style="{ background: color }"
 />
 </div>
 <span
 class="text-body-sm "
 :style="{ color: activeColor }"
 >{{ label }}</span>
 </div>
</template>
