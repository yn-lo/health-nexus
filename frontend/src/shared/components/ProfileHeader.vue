<script setup lang="ts">
withDefaults(defineProps<{
 name: string
 avatarText?: string
 meta?: string[]
 badge?: string
}>(), {
 meta: () => [],
 badge: '',
})
</script>

<template>
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-32)] pb-[var(--spacer-12)] flex flex-col items-center text-center">
 <div class="relative mb-[var(--spacer-16)]">
 <div class="profile-avatar w-24 h-24 rounded-full flex items-center justify-center overflow-hidden">
 <span class="text-onbrand text-3xl font-semibold font-heading">{{ avatarText }}</span>
 </div>
 <div v-if="$slots.action" class="absolute -right-1 -bottom-1">
 <slot name="action" />
 </div>
 </div>
 <div class="flex items-center gap-[var(--spacer-8)] mb-[var(--spacer-4)]">
 <h1 class="font-heading text-heading-sm font-semibold text-text ">{{ name }}</h1>
 <span
 v-if="badge"
 class="inline-flex items-center px-[var(--spacer-8)] py-[var(--spacer-2)] rounded-[var(--radius-full)] bg-[var(--bg-overlay-l1)] text-text-tertiary text-body-sm leading-none"
 >{{ badge }}</span>
 </div>
 <p
 v-for="(line, i) in meta"
 :key="i"
 class="text-body-sm text-text-tertiary "
 :class="{ 'mb-[var(--spacer-4)]': i < meta.length - 1 }"
 >{{ line }}</p>
 </section>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
.profile-avatar {
 background: linear-gradient(135deg, var(--bg-brand) 0%, var(--brand-400) 100%);
}
</style>
