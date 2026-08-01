<script setup lang="ts">
interface Stat {
 value: number | string
 label: string
}

defineProps<{
 stats: Stat[]
}>()
</script>

<template>
 <div class="flex justify-between">
 <template v-for="(stat, i) in stats" :key="i">
 <div class="flex-1 flex flex-col items-center">
 <span class="font-metric text-[22px] font-semibold text-text-brand tabular-nums ">{{ stat.value }}</span>
 <span class="text-[11px] text-text-tertiary mt-0.5 ">{{ stat.label }}</span>
 </div>
 <div v-if="i < stats.length - 1" class="w-px self-stretch bg-[var(--border-neutral-l1)]" />
 </template>
 </div>
</template>
