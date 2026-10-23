<script setup lang="ts">
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import type { Component } from 'vue'
import DsTabBar from '@/shared/components/DsTabBar.vue'

/**
 * 底部导航布局 - 患者端/医护端共用
 * 接收 tab items 和 route 映射，统一布局结构
 */

interface NavItem {
  key: string
  label: string
  iconComponent: Component
  /** 路由名前缀匹配，用于判断激活态 */
  routeNames: string[]
  /** 点击跳转的目标路由名 */
  routeName: string
  /** 跨 MPA 外部链接（如 /chat），设置后 routeName 无效 */
  externalUrl?: string
}

const props = defineProps<{
  items: NavItem[]
  /** 默认激活的 tab key */
  defaultActive?: string
}>()

const router = useRouter()
const route = useRoute()

const activeTab = computed(() => {
  const name = route.name as string
  const matched = props.items.find((item) => item.routeNames.includes(name))
  return matched?.key ?? props.defaultActive ?? props.items[0]?.key ?? ''
})

/** 是否显示底部导航（详情页通过路由 meta.hideTabBar 声明） */
const showTabBar = computed(() => {
  if (!route.name) return false
  return !route.meta.hideTabBar
})

function onTabChange(key: string) {
  const item = props.items.find((i) => i.key === key)
  if (!item) return
  if (item.externalUrl) {
    // ponytail:allow-location — 跨 MPA 跳转
    window.location.href = item.externalUrl
    return
  }
  router.push({ name: item.routeName })
}
</script>

<template>
  <div class="ds-app-shell" :class="{ 'bottom-nav-layout--with-tabbar': showTabBar }">
    <router-view />
    <DsTabBar
      v-if="showTabBar"
      :model-value="activeTab"
      :items="items"
      @update:model-value="onTabChange"
    />
  </div>
</template>

