<script setup lang="ts">
/**
 * 医护端文章编辑/创建表单 — 像素级还原 design/pages/article-form.html
 * 标题 + 科室选择 + 标签 + 摘要 + TipTap 富文本编辑器 + 底部操作栏
 * API: wikiApi.createArticle() + wikiApi.updateArticle() + wikiApi.submitArticle()
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
 ChevronDown,
 Bold,
 Italic,
 Heading,
 List,
 Trash2,
 RefreshCw,
 Layers,
} from '@lucide/vue'
import { useEditor, EditorContent } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import type { Editor } from '@tiptap/vue-3'
import type { Component } from 'vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { wikiApi, useDepartmentOptions, stripHtml } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES } from '@/shared/constants/roles'
import type { ArticleStatus, ArticleChunk } from '@/shared'

const router = useRouter()
const route = useRoute()
const { showSuccessToast, showFailToast } = useDsToast()
const { showConfirmDialog } = useDsDialog()
const authStore = useAuthStore()

/** 当前用户是否为管理员（科室管理员/系统管理员可跳过审核直接发布） */
const isAdmin = computed(() => {
 const role = authStore.user?.role
 return !!role && (ADMIN_ROLES as readonly string[]).includes(role)
})

const title = ref('')
const departmentId = ref<number | null>(null)
const summary = ref('')
const content = ref('')
const saving = ref(false)
const { options: departmentOptions, load: loadDepartments } = useDepartmentOptions()
/** 编辑模式下的文章状态（决定是否显示删除按钮） */
const articleStatus = ref<ArticleStatus | null>(null)
/** 编辑模式下加载到的文章版本号（更新时回传启用乐观锁，防并发编辑丢失更新） */
const articleVersion = ref<number | null>(null)
/** 文章切片列表（仅 published 状态加载） */
const chunks = ref<ArticleChunk[]>([])
const chunksLoading = ref(false)
const revectorizing = ref(false)
/** 切片面板展开状态 */
const chunksExpanded = ref(false)
/** 当前展开查看完整内容的切片 ID */
const expandedChunkId = ref<number | null>(null)

/** 是否编辑模式 */
const isEditMode = computed(() => !!route.params.id)

/** 是否可删除：仅编辑模式 + 草稿状态（与列表页一致，避免误删已发布/归档/待审核） */
const canDelete = computed(() => isEditMode.value && articleStatus.value === 'draft')

/** 是否显示切片状态区块：仅已发布文章（切片由 Worker 在发布时生成） */
const showChunks = computed(() => isEditMode.value && articleStatus.value === 'published')

/** 切片最后生成时间（取首片 created_at 作为代理） */
const chunksCreatedAt = computed(() => chunks.value[0]?.created_at ?? '')

/** 页面标题 */
const pageTitle = computed(() => (isEditMode.value ? '编辑文章' : '创建文章'))

/** TipTap 编辑器实例 */
const editor = useEditor({
 extensions: [StarterKit],
 content: '',
 onUpdate: ({ editor: e }: { editor: Editor }) => {
 content.value = e.getHTML()
 },
})

/** 工具栏按钮配置 */
interface ToolbarButton {
 icon: Component
 label: string
 active: boolean
 action: () => void
}

/** 工具栏按钮 */
const toolbarButtons = computed<ToolbarButton[]>(() => [
 {
 icon: Bold,
 label: '加粗',
 active: editor.value?.isActive('bold') ?? false,
 action: () => editor.value?.chain().focus().toggleBold().run(),
 },
 {
 icon: Italic,
 label: '斜体',
 active: editor.value?.isActive('italic') ?? false,
 action: () => editor.value?.chain().focus().toggleItalic().run(),
 },
 {
 icon: Heading,
 label: '标题',
 active: editor.value?.isActive('heading', { level: 2 }) ?? false,
 action: () => editor.value?.chain().focus().toggleHeading({ level: 2 }).run(),
 },
 {
 icon: List,
 label: '列表',
 active: editor.value?.isActive('bulletList') ?? false,
 action: () => editor.value?.chain().focus().toggleBulletList().run(),
 },
])

/** 构建创建请求体（含 department_id，对齐后端 createArticleRequest） */
function buildCreatePayload() {
 if (departmentId.value === null) {
 throw new Error('请选择科室')
 }
 return {
 title: title.value,
 content: content.value,
 summary: summary.value || undefined,
 department_id: departmentId.value,
 }
}

/** 构建更新请求体（不含 department_id，对齐后端 updateArticleRequest） */
function buildUpdatePayload() {
 return {
 title: title.value,
 content: content.value,
 summary: summary.value || undefined,
 version: articleVersion.value ?? undefined,
 }
}

/** 创建或更新文章，返回文章 ID（编辑态走更新，新建态先创建）。saveDraft/submitReview/publishDirectly 共用 */
async function ensureArticleSaved(): Promise<number> {
  const articleId = isEditMode.value
    ? Number(route.params.id)
    : (await wikiApi.createArticle(buildCreatePayload())).id
  if (isEditMode.value) {
    await wikiApi.updateArticle(articleId, buildUpdatePayload())
  }
  return articleId
}

/** 存为草稿 */
async function saveDraft() {
  saving.value = true
  try {
    await ensureArticleSaved()
    router.push({ name: 'staff-articles' })
  } catch (e) {
    showFailToast(errmsg(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

/** 提交审核（创建或更新后提交） */
async function submitReview() {
  saving.value = true
  try {
    const articleId = await ensureArticleSaved()
    await wikiApi.submitArticle(articleId)
    router.push({ name: 'staff-articles' })
  } catch (e) {
    showFailToast(errmsg(e, '提交失败'))
  } finally {
    saving.value = false
  }
}

/** 直接发布（管理员跳过审核：draft → pending → published） */
async function publishDirectly() {
  saving.value = true
  try {
    const articleId = await ensureArticleSaved()
    // 已是 pending 状态则无需再 submit
    if (articleStatus.value !== 'pending') {
      await wikiApi.submitArticle(articleId)
    }
    await wikiApi.approveArticle(articleId)
    showSuccessToast('发布成功')
    router.push({ name: 'staff-articles' })
  } catch (e) {
    showFailToast(errmsg(e, '发布失败'))
  } finally {
    saving.value = false
  }
}

/** 删除文章（带确认弹窗，仅草稿可删） */
async function handleDelete() {
 if (!isEditMode.value) return
 try {
 await showConfirmDialog({
 title: '确认删除',
 message: '删除后无法恢复，确定要删除这篇文章吗？',
 confirmButtonText: '删除',
 danger: true,
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 saving.value = true
 try {
 await wikiApi.deleteArticle(Number(route.params.id))
 showSuccessToast('已删除')
 router.replace({ name: 'staff-articles' })
 } catch (e) {
 showFailToast(errmsg(e, '删除失败'))
 } finally {
 saving.value = false
 }
}

/** ISO 日期 → YYYY-MM-DD HH:mm 完整格式 */
function fmtDateTime(iso: string): string {
 if (!iso) return ''
 const d = new Date(iso)
 if (isNaN(d.getTime())) return ''
 const yyyy = d.getFullYear()
 const mm = String(d.getMonth() + 1).padStart(2, '0')
 const dd = String(d.getDate()).padStart(2, '0')
 const hh = String(d.getHours()).padStart(2, '0')
 const mi = String(d.getMinutes()).padStart(2, '0')
 return `${yyyy}-${mm}-${dd} ${hh}:${mi}`
}

/** 加载切片列表（仅 published 状态调用） */
async function loadChunks() {
 if (!isEditMode.value) return
 chunksLoading.value = true
 try {
 const res = await wikiApi.listArticleChunks(Number(route.params.id))
 chunks.value = res.items
 } catch {
 // 切片加载失败不阻塞编辑，仅清空列表
 chunks.value = []
 } finally {
 chunksLoading.value = false
 }
}

/** 重新切片向量化（带确认弹窗） */
async function handleRevectorize() {
 if (!isEditMode.value) return
 try {
 await showConfirmDialog({
 title: '重新切片',
 message: '将失效当前切片并重新生成向量化切片，确认继续吗？',
 confirmButtonText: '重新切片',
 cancelButtonText: '取消',
 })
 } catch {
 return
 }
 revectorizing.value = true
 try {
 await wikiApi.revectorizeArticle(Number(route.params.id))
 showSuccessToast('已入队重新切片任务')
 // 重新加载切片（Worker 异步处理，立即刷新可能仍是旧数据，但保持 UI 一致）
 await loadChunks()
 } catch (e) {
 showFailToast(errmsg(e, '入队失败'))
 } finally {
 revectorizing.value = false
 }
}

/** 编辑模式下加载文章数据；并预加载科室列表用于下拉选择 */
onMounted(async () => {
 // 科室列表用于下拉选择（GET /api/base/departments 需 JWT + RequireStaff）
 loadDepartments()

 if (!isEditMode.value) return
 try {
 const article = await wikiApi.getMyArticle(Number(route.params.id))
 title.value = article.title
 summary.value = stripHtml(article.summary)
 departmentId.value = article.department_id
 content.value = article.content
 articleStatus.value = article.status
 articleVersion.value = article.version
 editor.value?.commands.setContent(article.content)
 // 已发布文章加载切片状态（诊断 RAG）
 if (article.status === 'published') {
 loadChunks()
 }
 } catch (e) {
 showFailToast(errmsg(e, '加载文章失败'))
 router.back()
 }
})

</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader :title="pageTitle" @back="router.back">
 <template #right>
 <button
 type="button"
 class="shrink-0 border-none bg-transparent font-heading text-body-base font-medium text-text-brand disabled:opacity-50"
 :disabled="saving"
 @click="saveDraft"
 >
 保存
 </button>
 </template>
 </AppHeader>

 <!-- 表单字段 -->
 <section class="flex flex-col gap-[var(--spacer-20)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <!-- 文章标题 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 文章标题<span class="text-[var(--status-error-default)]">*</span>
 </label>
 <div class="ds-field-wrap">
 <input
 v-model="title"
 type="text"
 placeholder="请输入文章标题"
 >
 </div>
 </div>

 <!-- 科室选择 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 科室选择<span class="text-[var(--status-error-default)]">*</span>
 </label>
 <div class="relative">
 <select
 v-model="departmentId"
 class="ds-select"
 >
 <option :value="null" disabled>请选择科室</option>
 <option v-for="opt in departmentOptions" :key="opt.id" :value="opt.id">
 {{ opt.label }}
 </option>
 </select>
 <ChevronDown class="pointer-events-none absolute right-[var(--spacer-12)] top-1/2 h-4 w-4 -translate-y-1/2 text-icon-tertiary" />
 </div>
 </div>

 <!-- 摘要 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 摘要
 </label>
 <textarea
 v-model="summary"
 rows="3"
 placeholder="请输入文章摘要..."
 class="ds-textarea"
 />
 </div>

 <!-- 正文内容（TipTap 富文本编辑器） -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 正文内容
 </label>
 <div
 class="overflow-hidden rounded-[var(--ds-control-radius-md)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]"
 >
 <!-- 工具栏 -->
 <div
 class="flex items-center gap-[var(--spacer-4)] border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-8)] py-[var(--spacer-8)]"
 >
 <button
 v-for="btn in toolbarButtons"
 :key="btn.label"
 type="button"
 class="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-4)] border-none transition-colors"
 :class="
 btn.active
 ? 'bg-[var(--bg-overlay-l2)] text-text'
 : 'bg-transparent text-icon hover:bg-[var(--bg-overlay-l1)]'
 "
 :aria-label="btn.label"
 @click="btn.action"
 >
 <component :is="btn.icon" class="h-4 w-4" />
 </button>
 </div>
 <!-- 编辑区 -->
 <EditorContent
 class="prose-article min-h-[180px] px-[var(--spacer-12)] py-[var(--spacer-12)] font-heading text-body-base text-text"
 :editor="editor"
 />
 </div>
 </div>

 <!-- 切片状态（仅已发布文章显示，契约 §4.12/4.13） -->
 <div v-if="showChunks" class="flex flex-col gap-[var(--spacer-8)]">
 <div class="flex items-center justify-between">
 <label class="font-heading text-body-base font-medium text-text">
 切片状态
 </label>
 <button
 type="button"
 class="inline-flex items-center gap-[var(--spacer-4)] border-none bg-transparent font-heading text-body-sm text-text-brand disabled:opacity-50"
 :disabled="revectorizing"
 @click="handleRevectorize"
 >
 <RefreshCw class="h-3.5 w-3.5" :class="revectorizing ? 'animate-spin' : ''" />
 重新切片
 </button>
 </div>
 <div
 class="rounded-[var(--ds-control-radius-md)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-12)] py-[var(--spacer-12)]"
 >
 <!-- 概要 -->
 <div class="flex items-center gap-[var(--spacer-8)]">
 <span class="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-4)] bg-[var(--bg-overlay-l1)] text-icon">
 <Layers class="h-4 w-4" />
 </span>
 <div class="flex-1 flex flex-col gap-[var(--spacer-2)]">
 <span v-if="chunksLoading" class="font-heading text-body-sm text-text-tertiary">加载中…</span>
 <span v-else-if="chunks.length === 0" class="font-heading text-body-sm text-[var(--status-error-default)]">
 暂无切片（向量化可能失败，可尝试重新切片）
 </span>
 <span v-else class="font-heading text-body-sm text-text">
 共 {{ chunks.length }} 片 · 版本 v{{ chunks[0].version }}
 </span>
 <span v-if="chunksCreatedAt" class="font-heading text-body-xs text-text-tertiary">
 生成于 {{ fmtDateTime(chunksCreatedAt) }}
 </span>
 </div>
 <button
 v-if="chunks.length > 0"
 type="button"
 class="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-4)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)] text-icon transition-colors hover:bg-[var(--bg-overlay-l1)]"
 :aria-label="chunksExpanded ? '收起' : '展开'"
 @click="chunksExpanded = !chunksExpanded"
 >
 <ChevronDown
 class="h-4 w-4 transition-transform"
 :class="chunksExpanded ? 'rotate-180' : ''"
 />
 </button>
 </div>
 <!-- 切片详情列表 -->
 <div
 v-if="chunksExpanded && chunks.length > 0"
 class="mt-[var(--spacer-12)] flex flex-col gap-[var(--spacer-8)] border-t border-[var(--border-neutral-l1)] pt-[var(--spacer-12)]"
 >
 <div
 v-for="chunk in chunks"
 :key="chunk.id"
 class="flex cursor-pointer flex-col gap-[var(--spacer-2)] rounded-[var(--radius-4)] px-[var(--spacer-4)] py-[var(--spacer-4)] transition-colors hover:bg-[var(--bg-overlay-l1)]"
 @click="expandedChunkId = expandedChunkId === chunk.id ? null : chunk.id"
 >
 <div class="flex items-center gap-[var(--spacer-4)]">
 <span class="inline-flex h-5 min-w-5 items-center justify-center rounded-[var(--radius-2)] bg-[var(--bg-brand)] px-[var(--spacer-4)] font-heading text-body-xs font-medium text-onbrand">
 {{ chunk.chunk_index + 1 }}
 </span>
 <span class="font-heading text-body-xs text-text-tertiary">
 {{ chunk.content_hash.slice(0, 8) }}
 </span>
 </div>
 <p
 class="font-heading text-body-sm text-text-secondary"
 :class="expandedChunkId === chunk.id ? '' : 'line-clamp-2'"
 >
 {{ stripHtml(chunk.content) }}
 </p>
 </div>
 </div>
 </div>
 </div>
 </section>

 <!-- 底部固定操作栏 -->
 <div
 class="fixed inset-x-0 bottom-0 z-40 border-t border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)]"
 >
 <div class="mx-auto flex max-w-[480px] gap-[var(--spacer-12)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <button
 v-if="canDelete"
 type="button"
 class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--danger shrink-0"
 :disabled="saving"
 aria-label="删除"
 @click="handleDelete"
 >
 <Trash2 class="icon h-4 w-4" />
 </button>
 <button
 type="button"
 class="h-8 flex-1 rounded-[var(--ds-control-radius-md)] border border-[var(--border-neutral-l1)] bg-[var(--bg-overlay-l1)] font-heading text-body-base font-medium text-text transition-colors hover:bg-[var(--bg-overlay-l2)] disabled:opacity-50"
 :disabled="saving"
 @click="saveDraft"
 >
 存为草稿
 </button>
 <button
 v-if="!isAdmin"
 type="button"
 class="h-8 flex-1 rounded-[var(--ds-control-radius-md)] border border-transparent bg-[var(--bg-brand)] font-heading text-body-base font-medium text-white transition-colors hover:bg-[var(--bg-brand-hover)] disabled:opacity-50"
 :disabled="saving"
 @click="submitReview"
 >
 提交审核
 </button>
 <button
 v-if="isAdmin"
 type="button"
 class="h-8 flex-1 rounded-[var(--ds-control-radius-md)] border border-[var(--border-neutral-l1)] bg-[var(--bg-overlay-l1)] font-heading text-body-base font-medium text-text transition-colors hover:bg-[var(--bg-overlay-l2)] disabled:opacity-50"
 :disabled="saving"
 @click="submitReview"
 >
 提交审核
 </button>
 <button
 v-if="isAdmin"
 type="button"
 class="h-8 flex-1 rounded-[var(--ds-control-radius-md)] border border-transparent bg-[var(--bg-brand)] font-heading text-body-base font-medium text-white transition-colors hover:bg-[var(--bg-brand-hover)] disabled:opacity-50"
 :disabled="saving"
 @click="publishDirectly"
 >
 直接发布
 </button>
 </div>
 </div>
 </main>
</template>

<style scoped>
/* TipTap 编辑器内容排版（ProseMirror） */
.prose-article :deep(.ProseMirror) {
 min-height: 180px;
 outline: none;
}

.prose-article :deep(.ProseMirror p) {
 margin: 0 0 var(--spacer-8) 0;
}

.prose-article :deep(.ProseMirror p:last-child) {
 margin-bottom: 0;
}

.prose-article :deep(.ProseMirror strong) {
 font-weight: 600;
}

.prose-article :deep(.ProseMirror h2) {
 font-size: var(--body-lg-font-size);
 font-weight: 600;
 margin: var(--spacer-12) 0 var(--spacer-8) 0;
}

.prose-article :deep(.ProseMirror ul) {
 list-style: disc;
 padding-left: var(--spacer-20);
 margin: var(--spacer-8) 0;
}

.prose-article :deep(.ProseMirror li) {
 margin: var(--spacer-4) 0;
}

.prose-article :deep(.ProseMirror p.is-editor-empty:first-child::before) {
 content: attr(data-placeholder);
 float: left;
 color: var(--text-tertiary);
 pointer-events: none;
 height: 0;
}
</style>
