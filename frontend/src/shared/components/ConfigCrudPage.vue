<script setup lang="ts">
/**
 * ConfigCrudPage — 配置 CRUD 列表页公共脚手架
 * 消除 SensitiveWord / SafetyRule / Department / Account 配置页的平行重复（jscpd clone）：
 * main 外壳 / AppHeader / 统计卡 / 搜索框 / 列表区(含空态) / 编辑弹窗壳(含底部按钮)。
 * 页面只通过 props + 插槽提供差异：筛选(#toolbar)、头部备注(#header-note)、列表项(默认槽)、表单(#form)。
 */
import type { Component } from 'vue'
import { AppHeader, DsPopup, StatRow, DsSearchBox } from '.'

type Stat = { value: number | string; label: string }

withDefaults(defineProps<{
  title: string
  stats?: Stat[]
  search: string
  searchPlaceholder?: string
  listCount: number
  loading?: boolean
  emptyTitle?: string
  emptyDesc?: string
  /** 空态图标（lucide 组件） */
  emptyIcon?: Component
  createLabel?: string
  editorShow?: boolean
  editorTitle?: string
  saveLabel?: string
}>(), {
  stats: () => [],
  searchPlaceholder: '搜索',
  loading: false,
  emptyTitle: '暂无数据',
  emptyDesc: '',
  emptyIcon: undefined,
  createLabel: '新建',
  editorShow: false,
  editorTitle: '',
  saveLabel: '保存',
})

const emit = defineEmits<{
  (e: 'update:search', v: string): void
  (e: 'create'): void
  (e: 'back'): void
  (e: 'save'): void
  (e: 'cancel'): void
  (e: 'update:editorShow', v: boolean): void
}>()

defineSlots<{
  /** 列表项（默认槽，渲染于 ds-list 内），空态时隐藏 */
  default?(): unknown
  /** 搜索框上方的备注/说明（可选） */
  'header-note'?(): unknown
  /** 搜索框下方的筛选控件（可选，如下拉框/筛选区） */
  toolbar?(): unknown
  /** 编辑弹窗表单主体 */
  form?(): unknown
}>()
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
  <AppHeader :title="title" show-create @create="emit('create')" @back="emit('back')" />

  <section v-if="stats && stats.length" class="mx-[var(--spacer-16)] mt-[var(--spacer-12)] rounded-[var(--radius-card-large)] bg-[var(--ai-gradient-soft)] px-[var(--spacer-16)] py-[var(--spacer-16)]">
   <StatRow :stats="stats" />
  </section>

  <section class="px-[var(--spacer-16)] pt-[var(--spacer-12)] pb-[var(--spacer-8)]">
   <slot name="header-note" />
   <DsSearchBox :model-value="search" :placeholder="searchPlaceholder" @update:model-value="emit('update:search', $event)" />
   <slot name="toolbar" />
  </section>

  <section class="px-[var(--spacer-16)] py-[var(--spacer-8)]">
   <div v-if="listCount > 0" class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] overflow-hidden">
    <slot />
   </div>
   <div v-else-if="!loading" class="flex flex-col items-center py-[var(--spacer-48)]">
    <div class="flex h-[56px] w-[56px] items-center justify-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)]">
     <component :is="emptyIcon" v-if="emptyIcon" class="h-6 w-6 text-icon-brand" />
    </div>
    <p class="mt-[var(--spacer-16)] text-heading-sm font-semibold text-text">{{ emptyTitle }}</p>
    <p v-if="emptyDesc" class="mt-[var(--spacer-4)] text-body-sm text-text-tertiary">{{ emptyDesc }}</p>
    <button type="button" class="ds-btn ds-btn--primary ds-btn--sm mt-[var(--spacer-16)]" @click="emit('create')">{{ createLabel }}</button>
   </div>
  </section>

  <DsPopup :show="editorShow" @update:show="v => emit('update:editorShow', v)">
   <div class="max-h-[85vh] overflow-y-auto p-[var(--spacer-16)] pb-[var(--spacer-24)]">
    <h3 class="mb-[var(--spacer-16)] text-heading-sm font-semibold text-text">{{ editorTitle }}</h3>
    <slot name="form" />
    <div class="mt-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
     <button type="button" class="ds-btn ds-btn--secondary ds-btn--block" @click="emit('cancel')">取消</button>
     <button type="button" class="ds-btn ds-btn--primary ds-btn--block" @click="emit('save')">{{ saveLabel }}</button>
    </div>
   </div>
  </DsPopup>
 </main>
</template>