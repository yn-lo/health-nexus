<script setup lang="ts">
/**
 * 科室管理 — 树形 CRUD
 * API: baseApi.listDepartmentTree/createDepartment/updateDepartment/deleteDepartment
 * 后端返回扁平数组，前端按 parent_id 组装为缩进树展示。
 * 范围：SUPER_ADMIN 看全树；DEPT_ADMIN 仅看主科室子树（由后端裁剪）。
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Pencil, Trash2, Building2, ChevronDown } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, DsPopup, StatRow, DsSearchBox } from '@/shared/components'
import { baseApi, useCrudEditor } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { DepartmentTreeNode, DepartmentCreateRequest, DepartmentUpdateRequest } from '@/shared'
import { buildChildMap } from '@/shared/utils/departmentTree'

const { showFailToast } = useDsToast()
const router = useRouter()

const tree = ref<DepartmentTreeNode[]>([])
const loading = ref(false)
const search = ref('')

const totalCount = computed(() => tree.value.length)
const activeCount = computed(() => tree.value.filter(n => n.is_active).length)
const publicCount = computed(() => tree.value.filter(n => n.is_public).length)

// ===== 树形组装 =====
// 后端扁平数组 → 按 parent_id 组装为带 depth 的展示列表（深度优先）。
interface DisplayRow {
 node: DepartmentTreeNode
 depth: number
}

const displayRows = computed<DisplayRow[]>(() => {
  const byParent = buildChildMap(tree.value)

  const rows: DisplayRow[] = []
  const walk = (parentId: number | null, depth: number) => {
    const children = byParent.get(parentId) ?? []
    for (const c of children) {
      rows.push({ node: c, depth })
      walk(c.id, depth + 1)
    }
  }
  walk(null, 0)
  return rows
})

const filteredRows = computed<DisplayRow[]>(() => {
 if (!search.value.trim()) return displayRows.value
 const q = search.value.trim().toLowerCase()
 // 搜索时：匹配节点 + 保留其祖先链（让用户看到上下文）
 const matchedIds = new Set<number>()
 const ancestorChain = new Set<number>()
 // 先扁平建立 id → node + parent 映射
 const byId = new Map<number, DepartmentTreeNode>()
 for (const n of tree.value) byId.set(n.id, n)
 for (const n of tree.value) {
 if (n.name.toLowerCase().includes(q) || (n.description ?? '').toLowerCase().includes(q)) {
 matchedIds.add(n.id)
 // 向上追溯祖先
 let p = n.parent_id
 while (p !== null && byId.has(p) && !ancestorChain.has(p)) {
 ancestorChain.add(p)
 p = byId.get(p)!.parent_id
 }
 }
 }
 return displayRows.value.filter((r) => matchedIds.has(r.node.id) || ancestorChain.has(r.node.id))
})

async function load() {
 loading.value = true
 try {
 tree.value = await baseApi.listDepartmentTree()
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

// ===== 编辑器（创建 + 编辑共用 popup） =====
const { showEditor, editing, form, openCreate, openEdit, submit, remove } = useCrudEditor<DepartmentTreeNode, DepartmentCreateRequest>({
 listRef: tree,
 defaultForm: () => ({ name: '', parent_id: null, is_public: false, is_active: true, description: '' }),
 toForm: (n) => ({ name: n.name, parent_id: n.parent_id, is_public: n.is_public, is_active: n.is_active, description: n.description ?? '' }),
 validate: (f) => {
  if (!f.name.trim()) return '请输入科室名称'
  if (f.name.length > 100) return '名称长度需为 1-100 字符'
  return null
 },
 create: (f) => baseApi.createDepartment(f),
 update: (n, f) => {
  // PATCH：仅发送变更字段；parent_id 特殊处理（0 = 变根科室）
  const patch: DepartmentUpdateRequest = {
   name: f.name,
   description: f.description ?? '',
   is_public: f.is_public,
   is_active: f.is_active,
  }
  // 父科室变更才发送 parent_id（避免无意义的 0 与原 null 误判）
  const original = n.parent_id
  const next = f.parent_id ?? null
  if ((original ?? null) !== (next ?? null)) {
   patch.parent_id = next ?? 0
  }
  return baseApi.updateDepartment(n.id, patch)
 },
 remove: {
  message: (n) => `删除科室「${n.name}」？\n若有子科室或关联用户将无法删除。`,
  run: (id) => baseApi.deleteDepartment(id),
 },
 onSaved: load,
})

// 父科室下拉候选：编辑时排除自身及其后代（避免成环）
const parentCandidates = computed<{ id: number; label: string; depth: number }[]>(() => {
  const byParent = buildChildMap(tree.value)

 // 计算编辑目标的后代集合（不能选自己后代作父）
 const forbidden = new Set<number>()
 if (editing.value) {
 forbidden.add(editing.value.id)
 const stack = [editing.value.id]
 while (stack.length > 0) {
 const cur = stack.pop()!
 for (const child of byParent.get(cur) ?? []) {
 if (!forbidden.has(child.id)) {
 forbidden.add(child.id)
 stack.push(child.id)
 }
 }
 }
 }

 const out: { id: number; label: string; depth: number }[] = []
 const walk = (parentId: number | null, depth: number) => {
 for (const c of byParent.get(parentId) ?? []) {
 if (forbidden.has(c.id)) continue
 const prefix = depth === 0 ? '' : '　'.repeat(depth) + '└ '
 out.push({ id: c.id, label: prefix + c.name, depth })
 walk(c.id, depth + 1)
 }
 }
 walk(null, 0)
 return out
})

async function toggleActive(node: DepartmentTreeNode, isActive: boolean) {
 try {
 await baseApi.updateDepartment(node.id, { is_active: isActive })
 node.is_active = isActive
 } catch (e) {
 showFailToast(errmsg(e, '切换失败'))
 }
}

async function togglePublic(node: DepartmentTreeNode) {
 try {
 await baseApi.updateDepartment(node.id, { is_public: !node.is_public })
 node.is_public = !node.is_public
 } catch (e) {
 showFailToast(errmsg(e, '切换失败'))
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="科室管理" showCreate @create="openCreate" @back="router.back" />

 <!-- 搜索 -->
 <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
 <div class="rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-12)]">
 <StatRow :stats="[
 { value: totalCount, label: '总数' },
 { value: activeCount, label: '启用' },
 { value: publicCount, label: '公开' },
 ]" />
 </div>

 <DsSearchBox v-model="search" placeholder="搜索科室名称或描述" class="mt-[var(--spacer-12)]" />
 </section>

 <!-- 树形列表 -->
 <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <div v-if="filteredRows.length > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
 <article
 v-for="row in filteredRows"
 :key="row.node.id"
 class="ds-list-item ds-list-item--divider"
 :style="{ paddingLeft: `calc(var(--spacer-16) + ${row.depth * 20}px)` }"
 >
 <span
 class="ds-list-item__icon"
 :class="row.node.is_active ? 'ds-list-item__icon--brand' : 'ds-list-item__icon--medical'"
 >
 <Building2 :size="20" />
 </span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">{{ row.node.name }}</span>
 <span class="ds-list-item__meta">
 <span v-if="row.node.description" class="truncate">{{ row.node.description }}</span>
 </span>
 </div>
 <div class="ds-list-item__trailing">
 <button
 type="button"
 class="ds-list-item__action-btn"
 :aria-label="row.node.is_public ? '设为私有' : '设为公开'"
 @click="togglePublic(row.node)"
 >
 <span class="ds-tag ds-tag--plain ds-tag--md" :class="row.node.is_public ? 'ds-tag--primary' : 'ds-tag--default'">{{ row.node.is_public ? '公开' : '私有' }}</span>
 </button>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" :checked="row.node.is_active" @change="toggleActive(row.node, ($event.target as HTMLInputElement).checked)">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="编辑"
 @click="openEdit(row.node)"
 >
 <Pencil :size="16" />
 </button>
 <button
 type="button"
 class="ds-list-item__action-btn"
 aria-label="删除"
 @click="remove(row.node)"
 >
 <Trash2 :size="16" />
 </button>
 </div>
 </article>
 </div>
 <div v-else-if="!loading" class="flex flex-col items-center py-20">
 <span class="flex h-14 w-14 items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
 <Building2 :size="28" class="text-icon-brand" />
 </span>
 <p class="mt-[var(--spacer-12)] text-heading-sm font-semibold text-text">还没有科室</p>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">创建第一个科室</p>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]" @click="openCreate">新建</button>
 </div>
 </section>

 <!-- 编辑/新建 popup -->
 <DsPopup v-model:show="showEditor">
 <div class="p-[var(--spacer-16)] pb-[var(--spacer-24)]">
 <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">
 {{ editing ? '编辑科室' : '新建科室' }}
 </h3>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">名称<span class="text-[var(--status-error-default)]">*</span></span>
 <input v-model="form.name" class="ds-input" placeholder="如 心内科" maxlength="100">
 </div>

 <!-- 父科室下拉：使用原生 select 保持与 ArticleForm 一致风格 -->
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">父科室</span>
 <div class="relative">
 <select
 v-model="form.parent_id"
 class="ds-select"
 >
 <option :value="null">— 根科室（顶级） —</option>
 <option v-for="c in parentCandidates" :key="c.id" :value="c.id">
 {{ c.label }}
 </option>
 </select>
 <ChevronDown class="pointer-events-none absolute right-[var(--spacer-12)] top-1/2 h-4 w-4 -translate-y-1/2 text-icon-tertiary" />
 </div>
 <span v-if="editing && form.parent_id === null" class="text-body-xs text-text-tertiary">
 注意：保存后此科室将变为根科室
 </span>
 </div>

 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">描述</span>
 <textarea v-model="form.description" class="ds-textarea" rows="2" placeholder="可选，如 心血管内科"></textarea>
 </div>

 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="text-body-base text-text">对患者公开</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_public">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 <div class="flex items-center justify-between px-[var(--spacer-16)] py-[var(--spacer-8)]">
 <span class="text-body-base text-text">启用</span>
 <label class="ds-switch">
 <input type="checkbox" class="ds-switch__input" v-model="form.is_active">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 </div>
 </div>
 <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="showEditor = false">取消</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="submit">保存</button>
 </div>
 </div>
 </DsPopup>
 </main>
</template>
