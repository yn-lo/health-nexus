<script setup lang="ts">
/**
 * 系统配置入口页 — 角色感知导航（兜底页，主入口已迁至工作台）
 * SUPER_ADMIN：全部配置项
 * DEPT_ADMIN：仅科室级配置（人员管理 / 提示词模板 / 科室设置）
 */
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import {
 Cpu,
 ShieldBan,
 AlertTriangle,
 SlidersHorizontal,
 FileText,
 ScrollText,
 Building2,
 Users,
 MessageSquareWarning,
 ChevronRight,
 ShieldCheck,
 KeyRound,
} from '@lucide/vue'
import { AppHeader, StatRow, SectionHeading } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { SUPER_ADMIN_ROLE } from '@/shared/constants/roles'
import type { Component } from 'vue'

const router = useRouter()
const authStore = useAuthStore()

const isSuperAdmin = computed(() => authStore.user?.role === SUPER_ADMIN_ROLE)

interface ConfigEntry {
 key: string
 label: string
 desc: string
 icon: Component
 routeName: string
 variant: 'brand' | 'error'
}

interface ConfigSection {
 title: string
 entries: ConfigEntry[]
}

const sections = computed<ConfigSection[]>(() => {
 const all: ConfigSection[] = [
 {
  title: '人员与组织',
  entries: [
  {
  key: 'accounts',
  label: '账号管理',
  desc: '账户创建 / 锁定 / 解锁',
  icon: Users,
  routeName: 'staff-config-accounts',
  variant: 'brand',
  },
  {
  key: 'invite-codes',
  label: '邀请码管理',
  desc: '生成/查看患者注册邀请码',
  icon: KeyRound,
  routeName: 'staff-config-invite-codes',
  variant: 'brand',
  },
  ],
 },
 {
  title: 'AI 与检索',
  entries: [
  {
  key: 'rag',
  label: 'RAG 参数',
  desc: '切片 / 检索 / 相似度阈值',
  icon: SlidersHorizontal,
  routeName: 'staff-config-rag',
  variant: 'brand',
  },
  {
  key: 'prompts',
  label: '系统提示词',
  desc: 'AI 对话使用的 System Prompt',
  icon: FileText,
  routeName: 'staff-config-prompts',
  variant: 'brand',
  },
  ],
 },
 ]
 if (isSuperAdmin.value) {
  // 科室管理仅超管可见
  all[0].entries.push({
  key: 'departments',
  label: '科室管理',
  desc: '科室层级 / 公开 / 启用',
  icon: Building2,
  routeName: 'staff-config-departments',
  variant: 'brand',
  })
  all[1].entries.unshift(
 {
 key: 'ai-providers',
 label: 'AI 提供商',
 desc: 'LLM / Embedding / Rerank',
 icon: Cpu,
 routeName: 'staff-config-ai-providers',
 variant: 'brand',
 },
 )
 all.push({
 title: '安全与合规',
 entries: [
 {
 key: 'safety-policy',
 label: '安全策略总览',
 desc: '当前生效的敏感词、规则与话术一览',
 icon: ShieldCheck,
 routeName: 'staff-config-safety-policy',
 variant: 'brand',
 },
 {
 key: 'sensitive-words',
 label: '敏感词库',
 desc: '命中后触发紧急/危机/拒答话术',
 icon: ShieldBan,
 routeName: 'staff-config-sensitive-words',
 variant: 'error',
 },
 {
 key: 'safety-rules',
 label: '安全规则',
 desc: '正则匹配 AI 输出，决定拦截或替换（预配置阶段）',
 icon: AlertTriangle,
 routeName: 'staff-config-safety-rules',
 variant: 'error',
 },
 {
 key: 'safety-messages',
 label: '安全话术',
 desc: '命中条件时向患者推送的文案（已在线生效）',
 icon: MessageSquareWarning,
 routeName: 'staff-config-safety-messages',
 variant: 'error',
 },
 {
 key: 'audit-logs',
 label: '审计日志',
 desc: '配置变更追溯与合规审计',
 icon: ScrollText,
 routeName: 'staff-config-audit-logs',
 variant: 'error',
 },
 ],
 })
 }
 return all
})

function go(routeName: string) {
 router.push({ name: routeName })
}
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="系统配置" @back="router.back" />

 <div class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-24)]">
 <!-- Hero stat card -->
 <div class="ds-card p-[var(--spacer-20)] bg-[var(--ai-gradient-soft)]">
 <StatRow :stats="[{ value: String(sections.reduce((sum, s) => sum + s.entries.length, 0)), label: '项配置' }]" />
 </div>

 <!-- Grouped sections -->
 <section v-for="section in sections" :key="section.title">
 <SectionHeading :text="section.title" />
 <div class="ds-menu-list">
 <button
 v-for="entry in section.entries"
 :key="entry.key"
 type="button"
 class="ds-menu-row"
 @click="go(entry.routeName)"
 >
 <span
 class="flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--radius-card-soft)]"
 :class="entry.variant === 'error'
 ? 'bg-[var(--status-error-light)] text-[var(--status-error-default)]'
 : 'bg-[var(--bg-brand-light)] text-icon-brand'"
 >
 <component :is="entry.icon" class="h-[22px] w-[22px]" />
 </span>
 <span class="min-w-0 flex-1 text-left">
 <span class="block text-body-base font-medium text-text">{{ entry.label }}</span>
 <span class="block truncate text-body-xs text-text-tertiary">{{ entry.desc }}</span>
 </span>
 <ChevronRight class="h-5 w-5 shrink-0 text-text-tertiary" />
 </button>
 </div>
 </section>
 </div>
 </main>
</template>
