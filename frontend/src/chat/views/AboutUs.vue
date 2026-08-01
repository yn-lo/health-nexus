<script setup lang="ts">
/**
 * AboutUs 关于我们 — AI-Native 风格
 * Hero 使用与 ChatHome 一致的 orb + pulse 光晕
 * 特性卡片使用 context-card 风格，配色与项目统一
 */
import { useRouter } from 'vue-router'
import { Bot, BookOpen, Users, Shield, Settings, BarChart3 } from '@lucide/vue'
import { AppHeader, BrandLogo } from '@/shared/components'

const router = useRouter()

function goBack() {
 router.back()
}

const appTagline = '健康宣教系统'
const version = 'v1.0.0'

const features = [
 { icon: Bot, title: 'AI 智能问答', desc: '基于 RAG 技术，AI 回答 100% 可追溯来源' },
 { icon: Users, title: '双端分离架构', desc: '医护管理端、患者端独立门户' },
 { icon: BookOpen, title: '知识库管理', desc: '文章全生命周期管理，支持知识切片与向量化' },
 { icon: BarChart3, title: '运营数据统计', desc: '知识覆盖率、需求热力图、用户活跃度分析' },
 { icon: Shield, title: '角色权限控制', desc: '基于 RBAC 的细粒度权限管理' },
 { icon: Settings, title: '系统配置管理', desc: '三级缓存架构，支持配置热更新' },
]
</script>

<template>
 <div class="about-page">
 <AppHeader title="关于我们" @back="goBack" />

 <div class="about-page-body px-[var(--spacer-20)]">
 <!-- Hero — 与 ChatHome 一致的 orb + pulse 光晕 -->
 <section class="flex flex-col items-center text-center pt-[var(--spacer-32)] pb-[var(--spacer-24)]">

 <BrandLogo size="md" orientation="vertical" class="mt-0" />

 <p class="text-body-base text-text-secondary mt-[10px] mb-[10px]">
 {{ appTagline }}
 </p>
 <span
 class="inline-flex items-center rounded-[var(--radius-full)] bg-[var(--bg-brand-light)] text-text-brand px-[var(--spacer-12)] py-[var(--spacer-4)] text-body-xs-strong font-medium mt-[var(--spacer-8)]"
 >
 {{ version }}
 </span>
 </section>

 <!-- Description -->
 <section class="pb-[var(--spacer-16)]">
 <p class="text-body-base text-text-secondary text-center">
 AI 驱动的智能健康宣教平台，基于 RAG 技术为患者提供 7×24 小时个性化健康指导，为医护减轻重复性宣教工作量。
 </p>
 </section>

 <!-- Core Features — context-card 风格 -->
 <section aria-labelledby="sec-features" class="pb-[var(--spacer-16)]">
 <p
 id="sec-features"
 class="text-body-sm font-medium text-text-tertiary mb-[var(--spacer-12)] uppercase tracking-wider"
 >
 核心特性
 </p>
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div
 v-for="feat in features"
 :key="feat.title"
 class="ai-context-card group flex items-center w-full text-left bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[14px_16px] gap-[var(--spacer-12)]"
 >
 <span class="ai-context-icon shrink-0 flex items-center justify-center w-10 h-10 rounded-[var(--radius-10)] bg-[var(--bg-brand-light)] text-icon-brand">
 <component :is="feat.icon" :size="20" />
 </span>
 <div class="flex-1 min-w-0">
 <p class="text-body-base text-text font-medium">
 {{ feat.title }}
 </p>
 <p class="text-body-sm text-text-tertiary">
 {{ feat.desc }}
 </p>
 </div>
 </div>
 </div>
 </section>

 <!-- System Positioning — 品牌色边框卡片 -->
 <section class="pb-[var(--spacer-16)]">
 <div class="ai-input-card bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[var(--spacer-16)]">
 <p class="text-body-sm text-text-secondary">
 本系统定位为健康宣教工具，不提供在线诊疗服务。系统无诊断权、无处方权，仅基于已审核的宣教知识库为患者提供住院期间及出院后康复相关的健康指导。
 </p>
 </div>
 </section>

 <!-- Tech Stack — 品牌色卡片网格 -->
 <section aria-labelledby="sec-tech" class="pb-[var(--spacer-16)]">
 <p
 id="sec-tech"
 class="text-body-sm font-medium text-text-tertiary mb-[var(--spacer-12)] uppercase tracking-wider"
 >
 技术栈
 </p>
 <div class="grid grid-cols-2 gap-[var(--spacer-8)]">
 <div class="ai-context-card bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-xs text-text-tertiary">后端框架</span>
 <span class="text-body-base text-text font-medium">Go 1.25 + Chi v5</span>
 </div>
 <div class="ai-context-card bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-xs text-text-tertiary">数据库</span>
 <span class="text-body-base text-text font-medium">PostgreSQL 16</span>
 </div>
 <div class="ai-context-card bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-xs text-text-tertiary">前端框架</span>
 <span class="text-body-base text-text font-medium">Vue 3 + TS</span>
 </div>
 <div class="ai-context-card bg-[var(--bg-base-default)] rounded-[var(--radius-12)] p-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-xs text-text-tertiary">AI 模型</span>
<span class="text-body-base text-text font-medium">自配接入</span>
 </div>
 </div>
 </section>

 <!-- License -->
 <section class="text-center pb-[var(--spacer-32)]">
 <p class="text-body-xs text-text-tertiary">
 AGPL-3.0
 </p>
 <p class="text-body-xs text-text-tertiary mt-[var(--spacer-4)]">
 让健康知识触手可及
 </p>
 </section>
 </div>
 </div>
</template>

<style scoped ponytail:allow-scoped-css 组件级样式覆盖，折中>
.about-page {
 background: var(--bg-base-default);
 min-height: 100dvh;
 display: flex;
 flex-direction: column;
}

.about-page-body {
 flex: 1;
 overflow-y: auto;
}

/* ── Context cards + Micro hover（与 ChatHome 一致） ──────── */
.ai-context-card {
 box-shadow: var(--shadow-xs);
 transition: transform var(--micro-duration) var(--micro-ease),
 box-shadow var(--micro-duration) var(--micro-ease);
}

.ai-context-card:hover {
 transform: translateY(-1px);
 box-shadow: var(--shadow-glow-card);
}

.ai-context-card:active {
 transform: translateY(0) scale(0.99);
}

.ai-context-icon {
 transition: background-color var(--micro-duration) var(--micro-ease),
 color var(--micro-duration) var(--micro-ease);
}

.ai-context-card:hover .ai-context-icon {
 background: var(--ai-accent);
 color: var(--text-onbrand);
}

/* ── 品牌色光晕边框卡片（与 ChatHome 输入卡片一致） ──────── */
.ai-input-card {
 border: 1px solid var(--brand-glow-border);
 box-shadow: var(--shadow-glow-input);
 backdrop-filter: blur(8px);
 transition: box-shadow var(--micro-duration) var(--micro-ease),
 border-color var(--micro-duration) var(--micro-ease);
}
.ai-input-card:focus-within {
 border-color: var(--brand-glow-border-strong);
 box-shadow: var(--shadow-glow-input-focus);
}

/* ── A11y：减弱动效偏好 ───────────────────────────────────── */
@media (prefers-reduced-motion: reduce) {
 .ai-context-card,
 .ai-context-icon {
 transition: none;
 }
 .ai-context-card:hover {
 transform: none;
 }
}
</style>
