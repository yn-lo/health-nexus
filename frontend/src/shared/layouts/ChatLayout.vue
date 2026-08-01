<script setup lang="ts">
/** 患者端布局 — 无底部 tabbar，导航由 header 胶囊切换 + 头像按钮承担 */
</script>

<template>
  <div class="chat-layout">
    <router-view />
  </div>
</template>

<style scoped ponytail:allow-scoped-css 布局容器样式，与 BottomNavLayout 对齐>
.chat-layout {
  min-height: 100vh;
  min-height: 100dvh;
  background-color: var(--bg-base-default);
  max-width: var(--layout-max-width);
  margin: 0 auto;
  position: relative;
}
</style>
