<script setup lang="ts">
import { computed, watch } from 'vue'
import { MessageSquare, ChevronRight, SquarePen, Trash2 } from '@lucide/vue'
import { useRouter, useRoute } from 'vue-router'
import { useChatStore, removeAnonSession } from '@/stores/chat'
import { getAccessToken, getUserStored, timeAgo } from '@/shared'
import { STAFF_ROLES, ROLE_LABEL, DEFAULT_STAFF_LABEL, type UserRole } from '@/shared/constants/roles'
import { EmptyState, DsPopup, DsSwipeCell } from '@/shared/components'
import { useDsDialog } from '@/shared/composables'

const props = withDefaults(defineProps<{
  visible: boolean
}>(), {
  visible: false,
})

const emit = defineEmits<{
  'update:visible': [value: boolean]
  select: [conversationId: string]
  newChat: []
}>()

const chatStore = useChatStore()
const router = useRouter()
const route = useRoute()
const { showConfirmDialog } = useDsDialog()

/** 匿名（无 token）：会话索引来自 localStorage；已登录：来自服务端 */
const isAnon = computed(() => !getAccessToken())
/** 匿名历史列表（本地索引）与登录会话列表合并展示 */
const displayList = computed(() => (isAnon.value ? chatStore.anonSessions : chatStore.conversations))
/** 当前高亮会话 id：匿名用路由参数，登录用 store 中的 currentConversation */
const activeConvId = computed(() => {
  if (isAnon.value) return (route.params.id as string) || ''
  return chatStore.currentConversation?.id
})

const user = computed(() => getUserStored())
const userInitial = computed(() => (user.value?.username ?? '?').charAt(0).toUpperCase())
const userRoleLabel = computed(() => {
  const role = user.value?.role as UserRole | undefined
  return (role && ROLE_LABEL[role]) || DEFAULT_STAFF_LABEL
})

// 仅在抽屉打开时加载会话列表：登录拉取服务端，匿名读本地索引
watch(() => props.visible, (visible) => {
  if (!visible) return
  if (isAnon.value) chatStore.loadAnonSessionsList()
  else chatStore.fetchConversations()
})

function onClose() {
  emit('update:visible', false)
}

function onSelect(id: string) {
  emit('select', id)
  onClose()
}

function goProfile() {
  onClose()
  const role = getAccessToken() ? (getUserStored()?.role as UserRole) : null
  if (role && STAFF_ROLES.includes(role)) {
    window.location.href = '/staff/profile' // ponytail:allow-location 跨 MPA 跳转
  } else {
    router.push({ name: 'personal-center' })
  }
}

async function onDelete(id: string) {
  try {
    await showConfirmDialog({ title: '删除对话', message: '确定删除该对话记录？删除后不可恢复。', danger: true })
    if (isAnon.value) {
      // 匿名：删除本地索引 + 本地消息缓存（服务端 Redis 上下文无公开删除端点，48h 自动过期）
      removeAnonSession(id)
      chatStore.loadAnonSessionsList()
      return
    }
    await chatStore.deleteConversation(id)
  } catch {
    // 用户取消
  }
}
</script>

<template>
  <DsPopup
    :show="visible"
    position="right"
    width="80vw"
    height="100%"
    @update:show="onClose"
  >
    <div class="history-drawer">
      <!-- 顶部栏：左侧折叠关闭 + 用户信息区 -->
      <div class="history-drawer__topbar">
        <!-- 折叠按钮：点击关闭整个抽屉 -->
        <button type="button" class="history-drawer__fold" aria-label="关闭抽屉" @click="onClose">
          <svg viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="currentColor" aria-hidden="true">
            <path d="M165.312 144a21.312 21.312 0 0 0-21.312 21.312v693.376c0 11.776 9.6 21.312 21.312 21.312h693.376c11.776 0 21.312-9.6 21.312-21.312V165.312a21.312 21.312 0 0 0-21.312-21.312H165.312zM48 165.312C48 100.544 100.48 48 165.312 48h693.376c64.768 0 117.312 52.48 117.312 117.312v693.376c0 64.768-52.48 117.312-117.312 117.312H165.312A117.312 117.312 0 0 1 48 858.688V165.312z"/>
            <path d="M336 896V128h96v768h-96zM716.608 392.704a48 48 0 0 1 0 67.904L665.216 512l51.392 51.392a48 48 0 1 1-67.904 67.84L563.392 545.92a48 48 0 0 1 0-67.84l85.312-85.376a48 48 0 0 1 67.84 0z"/>
          </svg>
        </button>
        <!-- 用户信息区：头像 + 用户名 + 个人中心（整卡可点击） -->
        <button type="button" class="history-drawer__user" aria-label="个人中心" @click="goProfile">
          <span class="ds-avatar ds-avatar--brand text-body-sm-strong font-semibold">{{ userInitial }}</span>
          <span class="history-drawer__user-info">
            <span class="history-drawer__user-name">{{ user?.username }}</span>
            <span class="history-drawer__user-sub">{{ userRoleLabel }}</span>
          </span>
          <ChevronRight :size="18" class="history-drawer__profile" />
        </button>
      </div>

      <!-- 新对话入口（Qwen 风格）：整行按钮，点击发起全新对话 -->
      <button type="button" class="history-drawer__new" aria-label="新对话" @click="emit('newChat')">
        <SquarePen :size="16" />
        <span>新对话</span>
      </button>

      <!-- 分隔线：用户区与对话列表之间 -->
      <div class="history-drawer__divider" />

      <div v-if="chatStore.loading" class="ds-loading py-4">
        <span class="ds-loading__spinner" />
      </div>

      <EmptyState
        v-else-if="!displayList.length"
        text="暂无对话记录"
      />

      <ul v-else class="ds-list history-drawer__list">
        <DsSwipeCell
          v-for="conv in displayList"
          :key="conv.id"
        >
          <li
            class="ds-list-item"
            :class="{ 'ds-list-item--active': conv.id === activeConvId }"
            @click="onSelect(conv.id)"
          >
            <div class="ds-list-item__icon">
              <MessageSquare :size="18" />
            </div>
            <div class="ds-list-item__content">
              <span class="ds-list-item__title">{{ conv.title || '未命名对话' }}</span>
              <span class="ds-list-item__meta">{{ timeAgo(conv.last_message_at ?? conv.created_at) }}</span>
            </div>
          </li>
          <template #right>
            <button class="history-item__delete" @click="onDelete(conv.id)">
              <Trash2 :size="16" />
            </button>
          </template>
        </DsSwipeCell>
      </ul>
    </div>
  </DsPopup>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
.history-drawer {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 0 var(--spacer-16);
  padding-top: env(safe-area-inset-top, 0px);
  padding-bottom: calc(var(--spacer-16) + env(safe-area-inset-bottom, 0px));
}

/* ── 顶部栏：左侧折叠 + 用户区 ─────────────────────────── */
.history-drawer__topbar {
  display: flex;
  align-items: center;
  gap: var(--spacer-12);
}

/* 折叠关闭按钮（Qwen 风格折叠把手） */
.history-drawer__fold {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  color: var(--icon-tertiary);
  background: var(--bg-overlay-l1);
  border: none;
  border-radius: var(--radius-full);
  cursor: pointer;
  transition: transform var(--micro-duration) var(--micro-ease),
              background-color var(--micro-duration) var(--micro-ease);
}
.history-drawer__fold:active {
  transform: scale(var(--press-scale));
  background: var(--bg-overlay-l2, var(--bg-overlay-l1));
}

/* ── 用户信息区：头像 + 用户名 + 个人中心（整卡可点击） ─── */
.history-drawer__user {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--spacer-12);
  flex: 1;
  min-width: 0;
  width: 100%;
  padding: var(--spacer-20) 0 var(--spacer-16);
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.history-drawer__user:active {
  opacity: 0.8;
}

.history-drawer__user-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 0 1 auto;
}

.history-drawer__user-name {
  font-size: var(--heading-sm-font-size);
  font-weight: var(--font-weight-strong);
  color: var(--text-default);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-drawer__user-sub {
  font-size: var(--body-xs-font-size);
  color: var(--text-tertiary);
}

.history-drawer__profile {
  flex-shrink: 0;
  color: var(--icon-tertiary);
}

/* ── 新对话入口：整行按钮（Qwen 风格） ─────────────────── */
.history-drawer__new {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacer-8);
  width: 100%;
  padding: var(--spacer-12) 0;
  margin-bottom: var(--spacer-12);
  font-size: var(--body-base-font-size);
  font-weight: var(--font-weight-strong);
  color: var(--text-brand);
  background: var(--bg-brand-light);
  border: none;
  border-radius: var(--radius-12);
  cursor: pointer;
  transition: transform var(--micro-duration) var(--micro-ease),
              background-color var(--micro-duration) var(--micro-ease);
}
.history-drawer__new:active {
  transform: scale(var(--press-scale));
}

.history-drawer__divider {
  border-top: 1px solid var(--border-neutral-l2);
  margin-bottom: var(--spacer-12);
}

.history-drawer__list {
  flex: 1;
  overflow-y: auto;
  list-style: none;
  margin: 0;
  padding: var(--spacer-8) 0;
  -webkit-overflow-scrolling: touch;
}

/* ponytail: ds-list-item 全局类覆盖默认 padding，适配抽屉紧凑布局，折中 */
.history-drawer__list .ds-list-item {
  padding: var(--spacer-14) var(--spacer-8);
  border-radius: var(--radius-8);
  border-bottom: 1px solid var(--border-neutral-l1);
}

.history-drawer__list :deep(.ds-swipe-cell:last-child) .ds-list-item {
  border-bottom: none;
}

/* 滑动单元格：禁止 flex 压缩，否则 cell 高度 < li 高度，overflow:hidden 裁切导致相邻项重叠 */
.history-drawer__list :deep(.ds-swipe-cell) {
  flex-shrink: 0;
}

.history-drawer__list .ds-list-item--active {
  background: var(--bg-brand-light);
}

.history-drawer__list .ds-list-item--active .ds-list-item__icon {
  background: var(--bg-brand);
  color: var(--text-onbrand);
}

.history-item__delete {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 100%;
  border: none;
  border-radius: 0;
  background: var(--status-error-default);
  color: var(--text-onaccent);
  font-size: var(--body-xs-font-size);
}
</style>
