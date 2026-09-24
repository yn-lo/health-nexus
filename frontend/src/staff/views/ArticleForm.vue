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
 Underline,
 Strikethrough,
 Heading,
 Quote,
 Minus,
 List,
 ListOrdered,
 AlignLeft,
 AlignCenter,
 AlignRight,
 ImagePlus,
 Trash2,
 RefreshCw,
 Layers,
} from '@lucide/vue'
import { useEditor, EditorContent } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import Image from '@tiptap/extension-image'
import TextAlign from '@tiptap/extension-text-align'
import type { Editor } from '@tiptap/vue-3'
import type { Component } from 'vue'
import { useDsToast, useDsDialog } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { wikiApi, useDepartmentOptions, stripHtml } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { useAuthStore } from '@/stores/auth'
import { ADMIN_ROLES, SUPER_ADMIN_ROLE } from '@/shared/constants/roles'
import { CONTENT_RISK_DEFAULT, CONTENT_RISK_OPTIONS } from '@/shared/constants/wiki'
import type { ArticleStatus, ArticleChunk, ContentRisk } from '@/shared'

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

/** 是否超管：超管可写任意科室文章，其余医护/科室管理员仅能写本科室 */
const isSuperAdmin = computed(() => authStore.user?.role === SUPER_ADMIN_ROLE)
/** 当前用户主科室 ID（医护/科室管理员仅能在此科室创作） */
const ownDeptId = computed(() => authStore.user?.dept_id ?? null)
/** 科室选择器可选范围：超管可见全部科室，其余只可见本科室 */
const allowedDepartments = computed(() =>
 isSuperAdmin.value
 ? departmentOptions.value
 : departmentOptions.value.filter((o) => o.id === ownDeptId.value),
)

const title = ref('')
const departmentId = ref<number | null>(null)
const summary = ref('')
const content = ref('')
/** 知识来源（可空，用于复审追溯与检索可见性） */
const source = ref('')
/** 适用人群（可空，如"高血压患者""孕产妇"） */
const applicablePopulation = ref('')
/** 有效期至（日期选择值 YYYY-MM-DD；空字符串表示未设置/清空） */
const validUntil = ref('')
/** 内容风险等级（high 逾期后退出检索） */
const contentRisk = ref<ContentRisk>(CONTENT_RISK_DEFAULT)
const saving = ref(false)
const { options: departmentOptions, load: loadDepartments } = useDepartmentOptions()
/** 编辑模式下的文章状态（决定是否显示删除按钮） */
const articleStatus = ref<ArticleStatus | null>(null)
/** 编辑模式下加载到的文章版本号（更新时回传启用乐观锁，防并发编辑丢失更新） */
const articleVersion = ref<number | null>(null)
/** 加载时的正文快照 — 用于判断已发布文章正文是否被改动 */
const originalContent = ref('')
/** 文章切片列表（仅 published 状态加载） */
const chunks = ref<ArticleChunk[]>([])
const chunksLoading = ref(false)
const revectorizing = ref(false)
/** 切片面板展开状态 */
const chunksExpanded = ref(false)
/** 当前展开查看完整内容的切片 ID */
const expandedChunkId = ref<number | null>(null)

/** 隐藏的文件选择框（工具栏"插入图片"触发） */
const fileInput = ref<HTMLInputElement | null>(null)
/** 图片上传中（防重复触发） */
const uploading = ref(false)

/** 是否编辑模式 */
const isEditMode = computed(() => !!route.params.id)

/** 是否可删除：仅编辑模式 + 草稿状态（与列表页一致，避免误删已发布/归档/待审核） */
const canDelete = computed(() => isEditMode.value && articleStatus.value === 'draft')

/** 是否显示切片状态区块：仅已发布文章（切片由 Worker 在发布时生成） */
const showChunks = computed(() => isEditMode.value && articleStatus.value === 'published')

/** 已发布文章正文被改动：保存后状态回到 pending，审核通过前线上仍展示上一版 */
const showRepublishNotice = computed(
  () => articleStatus.value === 'published' && content.value !== originalContent.value,
)

/** 切片最后生成时间（取首片 created_at 作为代理） */
const chunksCreatedAt = computed(() => chunks.value[0]?.created_at ?? '')

/** 页面标题 */
const pageTitle = computed(() => (isEditMode.value ? '编辑文章' : '创建文章'))

/** TipTap 编辑器实例 */
// Image.extend：块级图片默认不吃 text-align（不在段落内），补一个 textAlign 属性，
// 渲染为 display:block + margin auto 实现居中/右对齐；患者端消毒器已放行这些声明。
const AlignedImage = Image.extend({
 addAttributes() {
 return {
 ...this.parent?.(),
 textAlign: {
 default: null as string | null,
 parseHTML: (el: HTMLElement) => {
 const s = el.style
 if (s.display === 'block' && s.marginLeft === 'auto' && s.marginRight === 'auto') return 'center'
 if (s.display === 'block' && s.marginLeft === 'auto' && s.marginRight === '0px') return 'right'
 return null
 },
 renderHTML: (attrs: { textAlign?: string | null }) => {
 if (attrs.textAlign === 'center') {
 return { style: 'display:block;margin-left:auto;margin-right:auto' }
 }
 if (attrs.textAlign === 'right') {
 return { style: 'display:block;margin-left:auto;margin-right:0' }
 }
 return {}
 },
 },
 }
 },
 // 官方 ResizableNodeView 的 update() 只保留 DOM、不同步节点属性（拖拽时是内部直接改样式），
 // 外部 updateAttributes 改对齐后编辑器画面不会更新。包一层：属性变更时同步容器
 // justify-content（官方容器为 flex 布局，对齐落在这里才生效）。
 addNodeView() {
 // this.parent?.() 先执行父方法，得到视图工厂 (props) => NodeView
 const parentFactory = this.parent?.() as ((props: unknown) => unknown) | undefined
 return (props) => {
 type ResizableLike = {
 container?: HTMLElement | null
 update?: (node: unknown, decorations: unknown, innerDecorations: unknown) => boolean
 }
 const nv = parentFactory?.(props) as ResizableLike | undefined
 if (!nv || typeof nv.update !== 'function') return nv
 const syncAlign = (node: unknown) => {
 if (!nv.container) return
 const align = (node as { attrs?: { textAlign?: string | null } }).attrs?.textAlign
 nv.container.style.justifyContent =
 align === 'center' ? 'center' : align === 'right' ? 'flex-end' : 'flex-start'
 }
 syncAlign(props.node)
 const originalUpdate = nv.update.bind(nv)
 nv.update = (node, decorations, innerDecorations) => {
 const ok = originalUpdate(node, decorations, innerDecorations)
 if (ok) syncAlign(node)
 return ok
 }
 return nv
 }
 },
})

const editor = useEditor({
 // StarterKit v3 已内置 Underline/Strike/Blockquote/HorizontalRule/OrderedList
 extensions: [
  StarterKit,
  AlignedImage.configure({
   // 官方 resize：选中图片出现拖拽手柄，宽度以 width/height 属性写进正文
   resize: { enabled: true, alwaysPreserveAspectRatio: true },
  }),
  TextAlign.configure({ types: ['heading', 'paragraph'] }),
 ],
 content: '',
 onUpdate: ({ editor: e }: { editor: Editor }) => {
 content.value = e.getHTML()
 },
})

/** 允许的图片类型与大小上限（与后端 upload.max_size_mb 对齐，前端先拦一道减少无效请求） */
const MAX_IMAGE_MB = 5
const ALLOWED_IMAGE_TYPES = ['image/jpeg', 'image/png', 'image/webp', 'image/gif']

/** 触发文件选择 */
function pickImage() {
 fileInput.value?.click()
}

/** 选择文件后上传并插入正文（正文只存 URL，图片本身不参与向量化） */
async function onImageSelected(e: Event) {
 const input = e.target as HTMLInputElement
 const file = input.files?.[0]
 input.value = '' // 清空以便重复选择同一文件
 if (!file || uploading.value) return
 if (!ALLOWED_IMAGE_TYPES.includes(file.type)) {
 showFailToast('仅支持 JPG/PNG/WebP/GIF 格式图片')
 return
 }
 if (file.size > MAX_IMAGE_MB * 1024 * 1024) {
 showFailToast(`图片不能超过 ${MAX_IMAGE_MB}MB`)
 return
 }
 uploading.value = true
 try {
 const { url } = await wikiApi.uploadArticleImage(file)
 editor.value?.chain().focus().setImage({ src: url, alt: file.name }).run()
 } catch (err) {
 showFailToast(errmsg(err, '图片上传失败'))
 } finally {
 uploading.value = false
 }
}

/** 工具栏按钮配置 */
interface ToolbarButton {
 icon: Component
 label: string
 active: boolean
 action: () => void
}

/** 对齐动作：选中图片时改图片自身对齐（块级图片不吃段落 text-align），否则改段落/标题 */
type AlignValue = 'left' | 'center' | 'right'

function applyAlign(align: AlignValue) {
 const ed = editor.value
 if (!ed) return
 if (ed.isActive('image')) {
 ed.chain().focus().updateAttributes('image', { textAlign: align }).run()
 } else {
 ed.chain().focus().setTextAlign(align).run()
 }
}

/** 对齐激活态：图片节点读自身 textAlign，文本读段落 text-align */
function isAlignActive(align: AlignValue): boolean {
 const ed = editor.value
 if (!ed) return false
 if (ed.isActive('image')) return ed.getAttributes('image').textAlign === align
 return ed.isActive({ textAlign: align })
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
 icon: Underline,
 label: '下划线',
 active: editor.value?.isActive('underline') ?? false,
 action: () => editor.value?.chain().focus().toggleUnderline().run(),
 },
 {
 icon: Strikethrough,
 label: '删除线',
 active: editor.value?.isActive('strike') ?? false,
 action: () => editor.value?.chain().focus().toggleStrike().run(),
 },
 {
 icon: Heading,
 label: '标题',
 active: editor.value?.isActive('heading', { level: 2 }) ?? false,
 action: () => editor.value?.chain().focus().toggleHeading({ level: 2 }).run(),
 },
 {
 icon: Quote,
 label: '引用',
 active: editor.value?.isActive('blockquote') ?? false,
 action: () => editor.value?.chain().focus().toggleBlockquote().run(),
 },
 {
 icon: List,
 label: '无序列表',
 active: editor.value?.isActive('bulletList') ?? false,
 action: () => editor.value?.chain().focus().toggleBulletList().run(),
 },
 {
 icon: ListOrdered,
 label: '有序列表',
 active: editor.value?.isActive('orderedList') ?? false,
 action: () => editor.value?.chain().focus().toggleOrderedList().run(),
 },
 {
 icon: Minus,
 label: '分割线',
 active: false,
 action: () => editor.value?.chain().focus().setHorizontalRule().run(),
 },
 {
 icon: AlignLeft,
 label: '左对齐',
 active: isAlignActive('left'),
 action: () => applyAlign('left'),
 },
 {
 icon: AlignCenter,
 label: '居中',
 active: isAlignActive('center'),
 action: () => applyAlign('center'),
 },
 {
 icon: AlignRight,
 label: '右对齐',
 active: isAlignActive('right'),
 action: () => applyAlign('right'),
 },
 {
 icon: ImagePlus,
 label: '插入图片',
 active: uploading.value,
 action: pickImage,
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
 source: source.value || undefined,
 applicable_population: applicablePopulation.value || undefined,
 valid_until: toRFC3339(validUntil.value) || undefined,
 content_risk: contentRisk.value,
 }
}

/** 日期选择值（YYYY-MM-DD）→ RFC3339；空值返回空字符串（后端语义：清空有效期） */
function toRFC3339(date: string): string {
  if (!date) return ''
  const d = new Date(`${date}T00:00:00Z`)
  return isNaN(d.getTime()) ? '' : d.toISOString()
}

/** 构建更新请求体（不含 department_id，对齐后端 updateArticleRequest） */
function buildUpdatePayload() {
  return {
    title: title.value,
    content: content.value,
    summary: summary.value || undefined,
    source: source.value,
    applicable_population: applicablePopulation.value,
    valid_until: toRFC3339(validUntil.value),
    content_risk: contentRisk.value,
    version: articleVersion.value ?? undefined,
  }
}

/** 创建或更新文章，返回文章 ID（编辑态走更新，新建态先创建）。saveDraft/submitReview/publishDirectly 共用 */
async function ensureArticleSaved(): Promise<number> {
  if (!isEditMode.value) {
    return (await wikiApi.createArticle(buildCreatePayload())).id
  }
  const articleId = Number(route.params.id)
  // 后端行为：已发布文章修改正文后状态回到 pending（审核通过前线上仍用上一版）。
  // 必须用更新返回值刷新本地状态与版本号，否则 publishDirectly 会误判为仍是 published 而跳过审核。
  const updated = await wikiApi.updateArticle(articleId, buildUpdatePayload())
  articleStatus.value = updated.status
  articleVersion.value = updated.version
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

/** 提交审核（创建或更新后提交）。草稿/新建才真正提交；已是 pending/published 则无需重复提交 */
async function submitReview() {
  saving.value = true
  try {
    const articleId = await ensureArticleSaved()
    const st = articleStatus.value
    if (st !== 'pending' && st !== 'published') {
      await wikiApi.submitArticle(articleId)
    }
    showSuccessToast(st === 'published' ? '已保存' : (st === 'pending' ? '已在审核中' : '已提交审核'))
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
  const st = articleStatus.value
  // 直接发布 = 草稿/新建：先提交再审核通过；pending（含已发布改正文后回到 pending）：仅审核通过；
  // 已发布且正文未变：更新后状态仍为 published，无需再走状态机
  if (st !== 'pending' && st !== 'published') {
    await wikiApi.submitArticle(articleId)
  }
  if (st !== 'published') {
    await wikiApi.approveArticle(articleId)
  }
  showSuccessToast(st === 'published' ? '已保存' : '发布成功')
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

 if (!isEditMode.value) {
   // 医护/科室管理员仅能在本科室创作，自动锁定主科室为默认选择
   if (!isSuperAdmin.value && ownDeptId.value !== null) {
     departmentId.value = ownDeptId.value
   }
   return
 }
 try {
 const article = await wikiApi.getMyArticle(Number(route.params.id))
 title.value = article.title
 summary.value = stripHtml(article.summary)
 departmentId.value = article.department_id
 content.value = article.content
 source.value = article.source
 applicablePopulation.value = article.applicable_population
 // RFC3339 → 日期选择器可用的 YYYY-MM-DD（null 表示未设置）
 validUntil.value = article.valid_until ? article.valid_until.slice(0, 10) : ''
 contentRisk.value = article.content_risk
 articleStatus.value = article.status
 articleVersion.value = article.version
 originalContent.value = article.content
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
 <!-- 动作统一收敛到底部操作行（存为草稿/提交审核/直接发布），顶栏不再放保存按钮 -->
 <AppHeader :title="pageTitle" @back="router.back" />

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
 :disabled="!isSuperAdmin"
 >
 <option :value="null" disabled>请选择科室</option>
 <option v-for="opt in allowedDepartments" :key="opt.id" :value="opt.id">
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
 <!-- 工具栏：移动端横向滑动（13 按钮在窄屏放不下，禁止换行/挤压，配 shrink-0） -->
 <div
 class="flex flex-nowrap items-center gap-[var(--spacer-4)] overflow-x-auto border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-8)] py-[var(--spacer-8)]"
 >
 <button
 v-for="btn in toolbarButtons"
 :key="btn.label"
 type="button"
 class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-4)] border-none transition-colors"
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
 <!-- 隐藏的图片选择框（仅接受图片，上传结果插入为 <img>，不参与向量化） -->
 <input
 ref="fileInput"
 type="file"
 accept="image/jpeg,image/png,image/webp,image/gif"
 class="hidden"
 aria-hidden="true"
 @change="onImageSelected"
 >
 </div>
 <!-- 编辑区 -->
 <EditorContent
 class="prose-article min-h-[180px] px-[var(--spacer-12)] py-[var(--spacer-12)] font-heading text-body-base text-text"
 :editor="editor"
 />
 </div>
 </div>

 <!-- 知识元数据（创建/编辑均可设置：决定复审周期与检索可见性） -->
 <!-- 知识来源 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
  <label class="font-heading text-body-base font-medium text-text">
  知识来源
  </label>
 <div class="ds-field-wrap">
 <input
 v-model="source"
 type="text"
 placeholder="如：《中国高血压防治指南（2024）》，可留空"
 >
 </div>
 <span class="font-heading text-body-xs text-text-tertiary">
 用于复审追溯与检索可见性
 </span>
 </div>

 <!-- 适用人群 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 适用人群
 </label>
 <div class="ds-field-wrap">
 <input
 v-model="applicablePopulation"
 type="text"
 placeholder="如：高血压患者、孕产妇，可留空"
 >
 </div>
 <span class="font-heading text-body-xs text-text-tertiary">
 用于检索可见性
 </span>
 </div>

 <!-- 有效期至（清空后提交空字符串，表示清空有效期） -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 有效期至
 </label>
 <div class="flex items-center gap-[var(--spacer-8)]">
 <div class="ds-field-wrap flex-1">
 <input
 v-model="validUntil"
 type="date"
 aria-label="有效期至"
 >
 </div>
 <button
 v-if="validUntil"
 type="button"
 class="border-none bg-transparent font-heading text-body-sm text-text-brand"
 @click="validUntil = ''"
 >
 清空
 </button>
 </div>
 <span class="font-heading text-body-xs text-text-tertiary">
 用于复审提醒与检索可见性；到期后进入复审
 </span>
 </div>

 <!-- 内容风险等级 -->
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <label class="font-heading text-body-base font-medium text-text">
 内容风险等级
 </label>
 <div class="relative">
 <select
 v-model="contentRisk"
 class="ds-select"
 aria-label="内容风险等级"
 >
 <option v-for="opt in CONTENT_RISK_OPTIONS" :key="opt.value" :value="opt.value">
 {{ opt.label }}
 </option>
 </select>
 <ChevronDown class="pointer-events-none absolute right-[var(--spacer-12)] top-1/2 h-4 w-4 -translate-y-1/2 text-icon-tertiary" />
 </div>
 <span class="font-heading text-body-xs text-text-tertiary">
 高风险内容（用药、检查准备、高风险护理）逾期后会退出检索
 </span>
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
 class="fixed inset-x-0 bottom-0 z-40 border-t border-[var(--border-neutral-l1)] bg-[var(--bg-base-default)] pb-[env(safe-area-inset-bottom,0px)]"
 >
 <!-- 已发布文章正文改动提示：保存后回到待审核，审核通过前线上仍展示上一版 -->
 <p
 v-if="showRepublishNotice"
 class="mx-auto max-w-[480px] px-[var(--spacer-16)] pt-[var(--spacer-8)] font-heading text-body-xs text-[var(--status-warning-default)]"
 >
 已发布文章修改正文后需重新审核，审核通过前线上仍展示上一版本
 </p>
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

/* 插图：与患者端 .markdown-body img 保持一致的排版观感 */
.prose-article :deep(.ProseMirror img) {
 max-width: 100%;
 height: auto;
 border-radius: var(--radius-8);
 margin: var(--spacer-8) 0;
}

/* 选中图片：selectednode 类由官方 NodeView 加在容器 div 上 */
.prose-article :deep([data-resize-container].ProseMirror-selectednode img) {
 outline: 2px solid var(--border-brand);
}

/* 官方 resize 手柄不提供默认样式（宿主职责）：选中图片时显示四角拖拽点 */
.prose-article :deep([data-resize-handle]) {
 position: absolute;
 width: 12px;
 height: 12px;
 background: var(--bg-base-default);
 border: 2px solid var(--border-brand);
 border-radius: 9999px;
 box-shadow: 0 1px 4px rgb(0 0 0 / 25%);
 opacity: 0;
 pointer-events: none;
 transition: opacity 0.12s;
 z-index: 10;
}

.prose-article :deep([data-resize-container].ProseMirror-selectednode [data-resize-handle]) {
 opacity: 1;
 pointer-events: auto;
}

.prose-article :deep([data-resize-handle='bottom-right']),
.prose-article :deep([data-resize-handle='top-left']) {
 cursor: nwse-resize;
}

.prose-article :deep([data-resize-handle='bottom-left']),
.prose-article :deep([data-resize-handle='top-right']) {
 cursor: nesw-resize;
}

.prose-article :deep(.ProseMirror p.is-editor-empty:first-child::before) {
 content: attr(data-placeholder);
 float: left;
 color: var(--text-tertiary);
 pointer-events: none;
 height: 0;
}
</style>
