<template>
  <router-view />
  <DsFeedbackLayer />
  <BrandFavicon />
</template>

<script setup lang="ts">
import { DsFeedbackLayer, BrandFavicon } from '@/shared/components'
</script>
