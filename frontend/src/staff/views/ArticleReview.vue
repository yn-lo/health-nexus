<script setup lang="ts">
/**
 * 医护端文章审核 — 像素级还原 design/pages/article-review.html
 * 待审核数量徽标 + 审核卡片列表（标题/作者/标签/摘要 + 驳回/通过按钮）
 * API: wikiApi.listMyArticles({status:'pending'}) + wikiApi.approveArticle() + wikiApi.rejectArticle()
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { FileText, X, Check } from '@lucide/vue'
import { showDialog } from 'vant'
import { useDsToast } from '@/shared/composables'
import { AppHeader, EmptyState } from '@/shared/components'
import { wikiApi, stripHtml } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { ArticleStaff } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const articles = ref<ArticleStaff[]>([])
const loading = ref(false)

/** 待审核数量 */
const pendingCount = computed(() => articles.value.length)

/** 通过文章 */
async function handleApprove(id: number) {
 try {
 await wikiApi.approveArticle(id)
 articles.value = articles.value.filter((a) => a.id !== id)
 showSuccessToast('已通过')
 } catch (e) {
 showFailToast(errmsg(e, '操作失败'))
 }
}

/** 驳回文章 */
async function handleReject(id: number) {
  let reason = ''
  try {
    const res = await showDialog({
      title: '驳回文章',
      message: '请输入驳回原因，驳回后将退回作者草稿箱。',
      showInput: true,
      inputPlaceholder: '请输入驳回原因',
      inputValidator: (val: string) => (val && val.trim() ? true : '驳回原因不能为空'),
      confirmButtonText: '驳回',
      cancelButtonText: '取消',
    }) as { value: string }
    reason = res.value.trim()
  } catch {
    return
  }
  try {
    await wikiApi.rejectArticle(id, reason)
    articles.value = articles.value.filter((a) => a.id !== id)
    showSuccessToast('已驳回')
  } catch (e) {
    showFailToast(errmsg(e, '操作失败'))
  }
}

onMounted(async () => {
 loading.value = true
 try {
 const res = await wikiApi.listMyArticles({ status: 'pending' })
 articles.value = res.items
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
})
</script>

<template>
 <main class="min-h-screen min-h-dvh bg-[var(--bg-base-default)] pb-[var(--spacer-20)]">
 <AppHeader title="文章审核" @back="router.back" />

 <!-- 待审核数量徽标 -->
 <div class="flex items-center gap-[var(--spacer-8)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <span class="font-heading text-body-base text-text-secondary">
 待审核
 </span>
 <span
 class="inline-flex min-w-[20px] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand)] px-[var(--spacer-8)] py-[2px] font-metric text-body-sm font-medium text-onbrand"
 >
 {{ pendingCount }}
 </span>
 <span class="font-heading text-body-base text-text-secondary">
 篇
 </span>
 </div>

 <!-- 审核卡片列表 -->
 <div class="px-[var(--spacer-16)]">
 <div v-if="articles.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <section
 v-for="article in articles"
 :key="article.id"
 class="ds-list-item ds-list-item--divider"
 >
 <span class="ds-list-item__icon ds-list-item__icon--brand">
 <FileText :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ article.title }}</span>
 <span class="ds-list-item__meta">
 <span>{{ article.author_name }}</span>
 <span>· {{ article.department_name }}</span>
 <span v-if="article.summary">· {{ stripHtml(article.summary).slice(0, 30) }}{{ stripHtml(article.summary).length > 30 ? '…' : '' }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="驳回"
 @click="handleReject(article.id)"
 >
 <X :size="16" />
 </button>
 <button
 type="button"
 class="ds-list-item__action-btn text-[var(--status-success-default)]"
 aria-label="通过"
 @click="handleApprove(article.id)"
 >
 <Check :size="16" />
 </button>
 </div>
 </section>
 </div>

 <!-- 空状态 -->
 <EmptyState v-if="!loading && articles.length === 0" text="暂无待审核文章" />
 </div>
 </main>
</template>
