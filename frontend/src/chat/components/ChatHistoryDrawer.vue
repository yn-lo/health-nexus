<script setup lang="ts">
import { computed, watch } from 'vue'
import { MessageSquare, Settings, SquarePen, PanelRightClose, Trash2 } from '@lucide/vue'
import { useRouter, useRoute } from 'vue-router'
import { useChatStore, removeAnonSession } from '@/stores/chat'
import { deleteAnonConversation } from '@/shared/api/chat'
import { timeAgo } from '@/shared'
import { getUserStored, isAuthenticated } from '@/shared/api/client'
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
const isAnon = computed(() => !isAuthenticated.value)
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
  const role = isAuthenticated.value ? (getUserStored()?.role as UserRole) : null
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
