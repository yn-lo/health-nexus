<script setup lang="ts">
/** 患者端布局 — 无底部 tabbar，导航由 header 胶囊切换 + 头像按钮承担 */
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ConsentNotice } from '@/shared/components'
import { hasAcceptedConsent } from '@/shared/utils/consent'

const route = useRoute()

/** 首次进入聊天门户的知情告知：勾选确认后方可继续（版本号仅记入本地） */
const showConsent = ref(!hasAcceptedConsent())
/** 仅在聊天门户强制告知；协议页/知识库/关于页不拦截 */
const inChatPortal = computed(() => route.path === '/chat' || route.path.startsWith('/chat/'))
</script>

<template>
  <div class="ds-app-shell">
    <router-view />
    <ConsentNotice v-if="inChatPortal" v-model:show="showConsent" gate />
  </div>
</template>

