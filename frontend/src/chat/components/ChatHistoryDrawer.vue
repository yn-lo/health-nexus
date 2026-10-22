<script setup lang="ts">
import { computed, watch } from 'vue'
import { MessageSquare, Settings, SquarePen, PanelRightClose, Trash2 } from '@lucide/vue'
import { useRouter, useRoute } from 'vue-router'
import { useChatStore, removeAnonSession } from '@/stores/chat'
import { deleteAnonConversation } from '@/shared/api/chat'
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

/** 是否医护角色：仅医护可从对话抽屉直达工作台 */
const isStaff = computed(() => {
  const role = getAccessToken() ? (getUserStored()?.role as UserRole) : null
  return !!role && STAFF_ROLES.includes(role)
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
  if (isStaff.value) {
    window.location.href = '/staff/profile' // ponytail:allow-location 跨 MPA 跳转
  } else {
    router.push({ name: 'personal-center' })
  }
}

/** 工作台入口：医护跨 MPA 回到医护端首页 */
function goWorkbench() {
  onClose()
  window.location.href = '/staff' // ponytail:allow-location 跨 MPA 跳转
}

async function onDelete(id: string) {
  try {
    await showConfirmDialog({ title: '删除对话', message: '确定删除该对话记录？删除后不可恢复。', danger: true })
    if (isAnon.value) {
      // 匿名：同时清除服务端瞬态上下文（Redis 环）+ 本地索引与消息缓存。
      // 否则服务端上下文仍会被后续请求复用；清除失败不阻断本地清理（12h 后自动过期）。
      try {
        await deleteAnonConversation(id)
      } catch {
        /* 服务端上下文清理失败：本地照常删除，服务端 12h TTL 兜底 */
      }
      removeAnonSession(id)
      chatStore.loadAnonSessionsList()
      return
    }
    await chatStore.deleteConversation(id)
  } catch {
    // 用户取消
  }
}

/** 登录会话列表的下一页入口：总数已加载完则不展示 */
const hasMoreConversations = computed(
  () => !isAnon.value && chatStore.conversations.length < chatStore.conversationsTotal,
)

async function loadMore() {
  try {
    await chatStore.loadMoreConversations()
  } catch {
    // 分页失败保留已加载内容
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
        <button type="button" class="history-drawer__fold" aria-label="收起抽屉" @click="onClose">
          <PanelRightClose :size="18" />
        </button>
        <!-- 用户信息区：头像 + 用户名 + 个人中心（整卡可点击） -->
        <button type="button" class="history-drawer__user" aria-label="个人中心" @click="goProfile">
          <span class="ds-avatar ds-avatar--brand text-body-sm-strong font-semibold">{{ userInitial }}</span>
          <span class="history-drawer__user-info">
            <span class="history-drawer__user-name">{{ user?.username }}</span>
            <span class="history-drawer__user-sub">{{ userRoleLabel }}</span>
          </span>
        </button>
        <!-- 工作台入口：仅医护可见，点击直接回到医护端工作台 -->
        <button
          v-if="isStaff"
          type="button"
          class="history-drawer__workbench"
          aria-label="前往工作台"
          @click="goWorkbench"
        >
          <Settings :size="18" />
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
            class="ds-list-item ds-list-item--compact"
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
        <!-- 会话列表分页：还有更早会话时提供加载入口 -->
        <li v-if="hasMoreConversations" class="history-drawer__more">
          <button type="button" class="history-drawer__more-btn ds-ghost-pill" :disabled="chatStore.loading" @click="loadMore">
            {{ chatStore.loading ? '加载中…' : '加载更多' }}
          </button>
        </li>
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
}
/* 折叠关闭 / 新对话按钮共用的指针与按压过渡 */
.history-drawer__fold,
.history-drawer__new {
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

/* 工作台入口：仅医护可见的 40px 点击区，与折叠按钮同尺寸 */
.history-drawer__workbench {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  padding: 0;
  color: var(--icon-tertiary);
  background: transparent;
  border: none;
  border-radius: var(--radius-full);
  cursor: pointer;
}
.history-drawer__workbench:active {
  background: var(--bg-overlay-l1);
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
  background: transparent;
  color: var(--icon-tertiary);
  font-size: var(--body-xs-font-size);
}

/* ── 会话列表分页入口 ───────────────────────────────────── */
.history-drawer__more {
  display: flex;
  justify-content: center;
  padding: var(--spacer-12) 0;
  border-bottom: none;
}
.history-drawer__more-btn:disabled {
  opacity: 0.6;
}
</style>
