<script setup lang="ts">
/**
 * AboutUs 关于我们 — 「精致编辑风」设计语言
 * Hero 区保留（用户满意）：品牌行 + 大标题 + 副标题
 * 其余部分推倒重来：大字号分区标题 + 编号特性 + 精致留白节奏
 */
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { AppHeader, BrandLogo } from '@/shared/components'
import { splitSlogan } from '@/shared'
import { useBrandStore } from '@/stores/brand'

const router = useRouter()
const { brand } = storeToRefs(useBrandStore())

function goBack() {
 router.back()
}

const appTagline = '健康宣教系统'
const version = 'v1.0.0'

/** 系统标语分行：品牌配置的 slogan（支持 `\n` 换行），未配置回退默认定位语 */
const sloganLines = computed(() => splitSlogan(brand.value.slogan, [appTagline]))
/** 版权署名：配置了单位名称则派生「© 年份 单位名称 版权所有」，否则回退原作者署名 */
const copyrightText = computed(() => {
 const year = new Date().getFullYear()
 return brand.value.org_name
  ? `© ${year} ${brand.value.org_name} 版权所有`
  : `${year} © YNLO`
})
/** 官网链接文案：去掉协议前缀，与原展示风格一致（www.xxx） */
const siteLabel = computed(() => brand.value.site_url.replace(/^https?:\/\//, ''))

const features = [
 { title: 'AI 智能问答', desc: '基于 RAG 技术，AI 回答 100% 可追溯来源' },
 { title: '双端分离架构', desc: '医护管理端、患者端独立门户' },
 { title: '知识库管理', desc: '文章全生命周期管理，支持知识切片与向量化' },
 { title: '运营数据统计', desc: '知识覆盖率、需求热力图、用户活跃度分析' },
 { title: '角色权限控制', desc: '基于 RBAC 的细粒度权限管理' },
 { title: '系统配置管理', desc: '三级缓存架构，支持配置热更新' },
]

const techStack = [
 { label: '后端框架', value: 'Go 1.25 + Chi v5' },
 { label: '数据库', value: 'PostgreSQL 16' },
 { label: '前端框架', value: 'Vue 3 + TS' },
 { label: 'AI 模型', value: '自配接入' },
]
</script>

<template>
 <div class="about-page">
 <AppHeader title="关于我们" @back="goBack" />

 <div class="about-page-body px-[var(--spacer-20)]">
 <!-- Hero — 保留（用户满意） -->
 <section class="flex flex-col pt-[var(--spacer-32)] pb-[var(--spacer-32)]">
 <div class="about-brand-row flex items-center gap-[var(--spacer-10)]">
 <BrandLogo size="sm" hide-name />
 <span class="about-brand-name font-heading text-body-sm-strong tracking-[0.14em] text-text-tertiary">
  {{ brand.wordmark }}
 </span>
 </div>

 <div class="mt-[var(--spacer-32)] flex flex-col gap-[var(--spacer-12)]">
 <h1 class="about-title font-heading font-semibold leading-[1.15] tracking-[-0.02em] text-text">
  关于我们
 </h1>
 <p class="about-subtitle text-body-base leading-relaxed text-text-secondary">
  <span v-for="(line, i) in sloganLines" :key="i">{{ line }}<br v-if="i < sloganLines.length - 1"></span>
  <span> · {{ version }}</span>
 </p>
 </div>
 </section>

 <!-- Description — 精致引言 -->
 <section class="py-[var(--spacer-32)]">
 <p class="about-lead font-heading text-heading-md font-medium leading-relaxed text-text">
  AI 驱动的智能健康宣教平台，基于 RAG 技术为患者提供 7×24 小时个性化健康指导，为医护减轻重复性宣教工作量。
 </p>
 </section>

 <!-- Core Features — 编号特性列表 -->
 <section aria-labelledby="sec-features" class="pb-[var(--spacer-32)]">
 <div class="about-section-head">
 <span class="about-section-index">Ⅰ</span>
 <h2 id="sec-features" class="about-section-title font-heading text-heading-lg font-semibold tracking-[-0.02em] text-text">
  核心特性
 </h2>
 </div>

 <div class="about-features mt-[var(--spacer-20)]">
 <div
  v-for="(feat, i) in features"
  :key="feat.title"
  class="about-feature"
 >
 <span class="about-feature-index font-heading text-heading-2xl font-semibold text-text-tertiary">
  {{ String(i + 1).padStart(2, '0') }}
 </span>
 <div class="flex-1 min-w-0">
  <p class="text-body-base text-text font-medium">
   {{ feat.title }}
  </p>
  <p class="text-body-sm text-text-tertiary mt-[var(--spacer-4)] leading-relaxed">
   {{ feat.desc }}
  </p>
 </div>
 </div>
 </div>
 </section>

 <!-- System Positioning — 精致引言块 -->
 <section class="pb-[var(--spacer-32)]">
 <div class="about-section-head">
 <span class="about-section-index">Ⅱ</span>
 <h2 class="about-section-title font-heading text-heading-lg font-semibold tracking-[-0.02em] text-text">
  系统定位
 </h2>
 </div>

 <div class="about-quote mt-[var(--spacer-20)]">
 <p class="text-body-base leading-relaxed text-text-secondary">
  本系统定位为健康宣教工具，不提供在线诊疗服务。系统无诊断权、无处方权，仅基于已审核的宣教知识库为患者提供住院期间及出院后康复相关的健康指导。
 </p>
 </div>
 </section>

 <!-- Tech Stack — 大数字技术栈 -->
 <section aria-labelledby="sec-tech" class="pb-[var(--spacer-32)]">
 <div class="about-section-head">
 <span class="about-section-index">Ⅲ</span>
 <h2 id="sec-tech" class="about-section-title font-heading text-heading-lg font-semibold tracking-[-0.02em] text-text">
  技术栈
 </h2>
 </div>

 <div class="about-tech mt-[var(--spacer-20)]">
 <div
  v-for="item in techStack"
  :key="item.label"
  class="about-tech-item"
 >
 <span class="about-tech-label text-body-sm text-text-tertiary">{{ item.label }}</span>
 <span class="about-tech-value font-heading text-heading-md font-semibold text-text">{{ item.value }}</span>
 </div>
 </div>
 </section>

 <!-- License & Author -->
<section class="text-center pb-[var(--spacer-32)]">
 <div class="about-author mt-[var(--spacer-24)]">
 <p class="about-author-line text-body-sm text-text-tertiary">
  {{ copyrightText }}
 </p>
 <a
  v-if="brand.site_url"
  class="about-author-link text-body-sm text-text-brand hover:text-text-brand-hover"
  :href="brand.site_url"
  target="_blank"
  rel="noopener noreferrer"
 >
  {{ siteLabel }}
 </a>
 </div>
 </section>
 </div>
 </div>
</template>
