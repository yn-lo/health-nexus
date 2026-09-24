<script setup lang="ts">
/**
 * FeedbackStats 反馈统计 — 医护端
 * 数据源: GET /api/staff/chat/feedback/stats（三态汇总 + 最近反馈）
 * 反馈三态由患者端在 AI 回答上提交（解决了/部分解决/未解决）
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { CheckCircle2, CircleDot, CircleX, BarChart3 } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, PageShell, EmptyState } from '@/shared/components'
import { staffChatApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { feedbackLabel } from '@/shared/constants/feedback'
import type { FeedbackStatsItem, MessageFeedbackValue } from '@/shared'

const router = useRouter()
const { showFailToast } = useDsToast()

const loading = ref(false)
const stats = ref<{ total: number; solved: number; partial: number; unsolved: number }>({
  total: 0,
  solved: 0,
  partial: 0,
  unsolved: 0,
})
const recent = ref<FeedbackStatsItem[]>([])

/** 解决率（已解决 / 全部反馈，无反馈时显示 --） */
const resolveRate = computed(() =>
  stats.value.total > 0 ? Math.round((stats.value.solved / stats.value.total) * 100) : null,
)

/** 反馈值 → ds-tag 变体 */
function tagType(value: MessageFeedbackValue): 'success' | 'warning' | 'danger' {
  if (value === 'solved') return 'success'
  if (value === 'partial') return 'warning'
  return 'danger'
}

/** 格式化日期时间 */
function fmtDateTime(dateStr: string): string {
  return dateStr.slice(0, 16).replace('T', ' ')
}

async function loadStats() {
  loading.value = true
  try {
    const res = await staffChatApi.getFeedbackStats()
    stats.value = { total: res.total, solved: res.solved, partial: res.partial, unsolved: res.unsolved }
    recent.value = res.recent
  } catch (e) {
    showFailToast(errmsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

onMounted(loadStats)
</script>

<template>
  <PageShell :bottom-nav="false">
    <AppHeader title="反馈统计" @back="router.back" />

    <!-- 汇总概览 — 解决率主卡 + 三态计数 -->
    <div class="px-[var(--spacer-16)] pt-[var(--spacer-16)]">
      <div class="grid grid-cols-2 gap-[var(--spacer-12)]">
        <div class="flex items-center gap-[var(--spacer-12)] rounded-[var(--radius-card-medium)] bg-[var(--bg-brand)] p-[var(--spacer-16)] shadow-[var(--shadow-brand)]">
          <span class="inline-flex items-center justify-center shrink-0 w-9 h-9 rounded-full bg-[var(--text-onbrand)]">
            <BarChart3 class="w-5 h-5 text-icon-brand" />
          </span>
          <div class="min-w-0">
            <div class="font-metric text-heading-xl leading-none font-semibold text-onbrand tabular-nums">
              {{ resolveRate === null ? '--' : resolveRate + '%' }}
            </div>
            <div class="mt-[var(--spacer-4)] text-body-sm text-onbrand opacity-75">解决率</div>
          </div>
        </div>
        <div class="flex items-center gap-[var(--spacer-12)] rounded-[var(--radius-card-medium)] bg-[var(--bg-brand-light)] p-[var(--spacer-16)]">
          <span class="inline-flex items-center justify-center shrink-0 w-9 h-9 rounded-full bg-[var(--bg-brand)]">
            <CheckCircle2 class="w-5 h-5 text-icon-onbrand" />
          </span>
          <div class="min-w-0">
            <div class="font-metric text-heading-xl leading-none font-semibold text-text-brand tabular-nums">{{ stats.total }}</div>
            <div class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">总反馈数</div>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-3 gap-[var(--spacer-12)] mt-[var(--spacer-12)]">
        <div class="rounded-[var(--radius-card-medium)] bg-[var(--bg-base-default)] border border-[var(--border-neutral-l1)] p-[var(--spacer-12)] text-center">
          <div class="font-metric text-heading-lg font-semibold text-text tabular-nums">{{ stats.solved }}</div>
          <div class="mt-[var(--spacer-4)] text-body-xs text-text-secondary">解决了</div>
        </div>
        <div class="rounded-[var(--radius-card-medium)] bg-[var(--bg-base-default)] border border-[var(--border-neutral-l1)] p-[var(--spacer-12)] text-center">
          <div class="font-metric text-heading-lg font-semibold text-text tabular-nums">{{ stats.partial }}</div>
          <div class="mt-[var(--spacer-4)] text-body-xs text-text-secondary">部分解决</div>
        </div>
        <div class="rounded-[var(--radius-card-medium)] bg-[var(--bg-base-default)] border border-[var(--border-neutral-l1)] p-[var(--spacer-12)] text-center">
          <div class="font-metric text-heading-lg font-semibold text-text tabular-nums">{{ stats.unsolved }}</div>
          <div class="mt-[var(--spacer-4)] text-body-xs text-text-secondary">未解决</div>
        </div>
      </div>
    </div>

    <!-- 最近反馈列表 -->
    <section class="px-[var(--spacer-16)] pt-[var(--spacer-16)] pb-[var(--spacer-16)]">
      <div v-if="loading" class="py-[var(--spacer-32)] text-center text-body-md text-text-tertiary">
        加载中...
      </div>
      <EmptyState v-else-if="recent.length === 0" text="暂无患者反馈" />
      <div v-else class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
        <article
          v-for="item in recent"
          :key="item.message_id"
          class="ds-list-item ds-list-item--divider"
        >
          <span
            class="ds-list-item__icon"
            :class="item.feedback === 'solved'
              ? 'ds-list-item__icon--success'
              : item.feedback === 'partial'
                ? 'ds-list-item__icon--alert'
                : 'ds-list-item__icon--error'"
          >
            <CircleDot v-if="item.feedback === 'partial'" :size="20" />
            <CircleX v-else-if="item.feedback === 'unsolved'" :size="20" />
            <CheckCircle2 v-else :size="20" />
          </span>
          <div class="ds-list-item__content">
            <span class="ds-list-item__title">{{ item.content }}</span>
            <span class="ds-list-item__meta flex-wrap">
              <span class="ds-tag ds-tag--plain" :class="'ds-tag--' + tagType(item.feedback)">
                {{ feedbackLabel(item.feedback) }}
              </span>
              <span>· {{ fmtDateTime(item.created_at) }}</span>
            </span>
          </div>
        </article>
      </div>
    </section>
  </PageShell>
</template>
