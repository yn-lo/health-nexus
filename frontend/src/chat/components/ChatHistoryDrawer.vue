<script setup lang="ts">
import { watch } from 'vue'
import { Popup as VanPopup, SwipeCell as VanSwipeCell, Dialog, Loading as VanLoading } from 'vant'
import { MessageSquare, Plus, Settings, Trash2 } from '@lucide/vue'
import { useRouter } from 'vue-router'
import { useChatStore } from '@/stores/chat'
import { getAccessToken, getUserStored, timeAgo } from '@/shared'
import { STAFF_ROLES, type UserRole } from '@/shared/constants/roles'
import { EmptyState } from '@/shared/components'

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

// 仅在抽屉打开且用户已登录时拉取会话列表
watch(() => props.visible, (visible) => {
  if (visible && getAccessToken()) {
    chatStore.fetchConversations()
  }
})

function onClose() {
  emit('update:visible', false)
}

function onSelect(id: string) {
  emit('select', id)
  onClose()
}

function onNewChat() {
  emit('newChat')
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
    await Dialog.confirm({ title: '删除对话', message: '确定删除该对话记录？删除后不可恢复。' })
    await chatStore.deleteConversation(id)
  } catch {
    // 用户取消
  }
}
</script>

<template>
  <VanPopup
    :show="visible"
    position="right"
    :style="{ width: '80vw', height: '100%' }"
    @update:show="onClose"
  >
    <div class="history-drawer">
      <header class="history-drawer__header">
        <h2 class="history-drawer__title">对话历史</h2>
        <div class="history-drawer__actions">
          <button class="history-drawer__new" @click="onNewChat">
            <Plus :size="16" />
            <span>新对话</span>
          </button>
          <button class="history-drawer__profile" aria-label="个人中心" @click="goProfile">
            <Settings :size="18" />
          </button>
        </div>
      </header>

      <VanLoading v-if="chatStore.loading" type="spinner" class="py-4" />

      <EmptyState
        v-else-if="!chatStore.conversations.length"
        text="暂无对话记录"
      />

      <ul v-else class="ds-list history-drawer__list">
        <VanSwipeCell
          v-for="conv in chatStore.conversations"
          :key="conv.id"
        >
          <li
            class="ds-list-item"
            :class="{ 'ds-list-item--active': conv.id === chatStore.currentConversation?.id }"
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
        </VanSwipeCell>
      </ul>
    </div>
  </VanPopup>
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

.history-drawer__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--spacer-16) 0 var(--spacer-12);
  border-bottom: 1px solid var(--border-neutral-l1);
}

.history-drawer__title {
  margin: 0;
  font-size: var(--heading-sm-font-size);
  font-weight: var(--font-weight-strong);
  color: var(--text-default);
}

.history-drawer__new {
  display: inline-flex;
  align-items: center;
  gap: var(--spacer-4);
  padding: var(--spacer-4) var(--spacer-12);
  border: none;
  border-radius: var(--radius-full);
  background: var(--bg-brand-light);
  color: var(--text-brand);
  font-size: var(--body-xs-font-size);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  transition: all var(--transition-fast);
}

.history-drawer__new:active {
  opacity: 0.8;
}

.history-drawer__actions {
  display: inline-flex;
  align-items: center;
  gap: var(--spacer-8);
}

.history-drawer__profile {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--ds-control-height-sm);
  height: var(--ds-control-height-sm);
  border: none;
  border-radius: var(--radius-full);
  background: var(--bg-base-default);
  color: var(--icon-default);
  transition: all var(--transition-fast);
}

.history-drawer__profile:active {
  opacity: 0.8;
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
  padding: var(--spacer-12) var(--spacer-8);
  border-radius: var(--radius-8);
  border-bottom: 1px solid var(--border-neutral-l1);
}

.history-drawer__list :deep(.van-swipe-cell:last-child) .ds-list-item {
  border-bottom: none;
}

/* 滑动单元格：禁止 flex 压缩，否则 cell 高度 < li 高度，overflow:hidden 裁切导致相邻项重叠 */
.history-drawer__list :deep(.van-swipe-cell) {
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
