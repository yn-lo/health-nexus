<script setup lang="ts">
defineProps<{
 text: string
}>()
</script>

<template>
 <h2
 class="font-heading font-semibold text-text mb-[var(--spacer-12)] text-heading-sm "
 >
 {{ text }}
 </h2>
</template>
