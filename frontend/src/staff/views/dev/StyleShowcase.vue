<script setup lang="ts">
/**
 * 全局样式展示页 — 令牌 + 公共组件 + .ds组件 + 工具类
 * 修改 tokens.css / main.css 后通过 Vite HMR 自动热更新
 * 路由：/styles
 */
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import {
 User, MessageCircle, FileText,
 Globe, LogOut,
 ChevronRight, BookOpen, Users, HeartPulse, Stethoscope,
 Home,
 MessageSquare, BarChart3, Pencil, Trash2, CheckCircle2,
 ArrowRightLeft, Cpu, AlertTriangle, Search,
} from '@lucide/vue'
import {
 ProfileHeader, MenuList, MenuRow,
 AppHeader, BrandLogo, SectionHeading,
 StatRow, QuickActionGrid, QuickActionItem,
 EmptyState, DisclaimerFooter, PasswordStrength,
 DsTabBar, PageShell,
} from '@/shared/components'

const router = useRouter()

/* ── 交互状态 ── */
const searchText = ref('')
const switchVal = ref(true)
const pillSelected = ref('llm')
const inputVal = ref('')
const textareaVal = ref('')
const activeTab = ref(0)
const testPassword = ref('')
const tabbarVal = ref('home')

const isDark = ref(false)
function toggleDark() {
 isDark.value = !isDark.value
 document.documentElement.classList.toggle('dark', isDark.value)
}

/* ── 设计令牌展示数据 ── */
/** 品牌/中性色阶（合并两个结构相同的 grid 块，消除模板重复） */
const colorScales = [
 { var: 'brand', label: '品牌色阶 brand-50~950' },
 { var: 'grey', label: '中性灰阶 grey-50~950' },
]
const colorShades = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]

/* ── TabBar 示例数据 ── */
const tabbarItems = [
 { key: 'home', label: '首页', iconComponent: Home },
 { key: 'chat', label: '问答', iconComponent: MessageSquare },
 { key: 'stats', label: '数据', iconComponent: BarChart3 },
 { key: 'mine', label: '我的', iconComponent: User },
]

/* ── 锚点导航 ── */
const sections = [
 { id: 'tokens-colors', label: '颜色令牌' },
 { id: 'tokens-typography', label: '字体令牌' },
 { id: 'tokens-spacing', label: '圆角·间距·阴影' },
 { id: 'tokens-system', label: '系统令牌' },
 { id: 'tokens-ai', label: 'AI-Native 令牌' },
 { id: 'comp-brand', label: '品牌·布局组件' },
 { id: 'comp-data', label: '数据展示组件' },
 { id: 'comp-list', label: '列表样式' },
 { id: 'comp-menu', label: '菜单·操作组件' },
 { id: 'comp-form', label: '表单·交互组件' },
 { id: 'ds-components', label: '.ds 组件' },
 { id: 'utilities', label: 'CSS 工具类' },
] as const

const showToc = ref(false)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-secondary)] pb-24">
 <!-- ── 暗色模式切换（浮动） ── -->
 <button
 type="button"
 class="fixed bottom-6 right-6 z-[var(--z-toast)] flex h-10 w-10 items-center justify-center rounded-full bg-[var(--bg-brand)] text-onbrand shadow-[var(--shadow-lg)] transition-transform active:scale-95"
 @click="toggleDark"
 >
 <span class="text-sm">{{ isDark ? '☀️' : '🌙' }}</span>
 </button>

 <!-- ── 目录导航弹出 ── -->
 <button
 type="button"
 class="fixed bottom-6 right-20 z-[var(--z-toast)] flex h-10 w-10 items-center justify-center rounded-full bg-[var(--bg-white)] text-text-secondary shadow-[var(--shadow-lg)] border border-[var(--border-neutral-l1)] transition-transform active:scale-95"
 @click="showToc = !showToc"
 >
 <span class="text-xs font-semibold">TOC</span>
 </button>
 <Transition name="fade">
 <nav
 v-if="showToc"
 class="fixed bottom-20 right-6 z-[var(--z-toast)] w-48 rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-12)] shadow-[var(--shadow-xl)] border border-[var(--border-neutral-l1)]"
 >
 <p class="mb-[var(--spacer-8)] text-body-xs font-medium text-text-tertiary">目录导航</p>
 <a
 v-for="s in sections"
 :key="s.id"
 :href="`#${s.id}`"
 class="block py-[var(--spacer-4)] text-body-sm text-text-secondary hover:text-text-brand"
 @click="showToc = false"
 >{{ s.label }}</a>
 </nav>
 </Transition>

 <AppHeader title="全局样式展示" @back="router.back()" />

 <!-- ══════════════════════════════════════════════════════
 一、设计令牌 Design Tokens
 ══════════════════════════════════════════════════════ -->

 <!-- ── 1.1 颜色令牌 ── -->
 <section :id="'tokens-colors'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">颜色令牌</h2>

 <!-- 品牌色阶 + 中性灰阶 -->
 <div v-for="scale in colorScales" :key="scale.var">
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">{{ scale.label }}</p>
 <div class="mb-[var(--spacer-16)] grid grid-cols-5 gap-[var(--spacer-8)]">
 <div v-for="shade in colorShades" :key="shade" class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-full rounded-[var(--radius-md)]" :style="{ backgroundColor: `var(--${scale.var}-${shade})` }" />
 <span class="text-body-xs text-text-tertiary">{{ shade }}</span>
 </div>
 </div>
 </div>

 <!-- 背景层级 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">背景层级</p>
 <div class="mb-[var(--spacer-16)] flex flex-wrap gap-[var(--spacer-8)]">
 <div v-for="bg in ['base','surface','surface-variant','overlay','overlay-2','overlay-3','overlay-4','menu','tooltip','invert']" :key="bg" class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-16 rounded-[var(--radius-md)] border border-[var(--border-neutral-l1)]" :style="{ backgroundColor: `var(--color-${bg})` }" />
 <span class="text-body-xs text-text-tertiary text-center">{{ bg }}</span>
 </div>
 </div>

 <!-- 文本色 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">文本色</p>
 <div class="mb-[var(--spacer-16)] flex flex-wrap gap-[var(--spacer-8)]">
 <div v-for="t in ['text','text-secondary','text-tertiary','text-disabled','text-brand']" :key="t" class="flex items-center gap-[var(--spacer-4)]">
 <div class="h-6 w-6 rounded-[var(--radius-sm)]" :style="{ backgroundColor: `var(--color-${t})` }" />
 <span class="text-body-xs text-text-tertiary">{{ t }}</span>
 </div>
 </div>

 <!-- 状态色 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">状态色</p>
 <div class="mb-[var(--spacer-16)] grid grid-cols-2 gap-[var(--spacer-8)]">
 <div v-for="s in ['success','warning','error','info']" :key="s" class="flex items-center gap-[var(--spacer-8)]">
 <div class="h-8 w-8 rounded-[var(--radius-md)]" :style="{ backgroundColor: `var(--color-${s})` }" />
 <div>
 <p class="text-body-sm text-text-secondary">{{ s }}</p>
 <div class="mt-1 flex gap-1">
 <div class="h-4 w-4 rounded-[var(--radius-xs)]" :style="{ backgroundColor: `var(--color-${s}-hover)` }" />
 <div class="h-4 w-4 rounded-[var(--radius-xs)]" :style="{ backgroundColor: `var(--color-${s}-surface)` }" />
 </div>
 </div>
 </div>
 </div>

 <!-- 辅助色（图表/标签） -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">辅助色 accent（图表/标签）</p>
 <div class="mb-[var(--spacer-16)] flex flex-wrap gap-[var(--spacer-8)]">
 <div v-for="a in ['teal','coral','amber','lime','cyan','blue','magenta','violet','slate']" :key="a" class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-10 rounded-[var(--radius-md)]" :style="{ backgroundColor: `var(--accent-${a})` }" />
 <span class="text-body-xs text-text-tertiary">{{ a }}</span>
 </div>
 </div>

 </section>

 <!-- ── 1.2 字体令牌 ── -->
 <section :id="'tokens-typography'" class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-default)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">字体令牌</h2>

 <!-- 字体族 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">字体族</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-8)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <p class="text-body-base" style="font-family: var(--font-family-default)">font-family-default — SF Pro Text / PingFang SC</p>
 <p class="text-body-base" style="font-family: var(--font-family-heading)">font-family-heading — SF Pro / PingFang SC</p>
 <p class="text-body-base" style="font-family: var(--font-family-metric)">font-family-metric — Inter / SF Pro Text <span class="tabular-nums">0123456789</span></p>
 </div>

 <!-- Heading 层级 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">Heading 层级</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-8)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <p class="text-heading-display font-heading font-semibold text-text leading-tight">Display — 52px</p>
 <p class="text-heading-display-sm font-heading font-semibold text-text leading-tight">Display SM — 40px</p>
 <p class="text-heading-3xl font-heading font-semibold text-text">3XL — 32px</p>
 <p class="text-heading-2xl font-heading font-semibold text-text">2XL — 28px</p>
 <p class="text-heading-xl font-heading font-semibold text-text">XL — 24px</p>
 <p class="text-heading-lg font-heading font-semibold text-text">LG — 22px</p>
 <p class="text-heading-md font-heading font-semibold text-text">MD — 20px</p>
 <p class="text-heading-sm font-heading font-semibold text-text">SM — 16px</p>
 <p class="text-heading-xs font-heading font-semibold text-text">XS — 13px</p>
 <p class="text-heading-2xs font-heading font-semibold text-text">2XS — 12px</p>
 <p class="text-heading-3xs font-heading font-semibold text-text">3XS — 11px</p>
 </div>

 <!-- Body 层级 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">Body 层级</p>
 <div class="flex flex-col gap-[var(--spacer-8)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <p class="text-body-lg text-text">Body LG — 18px 正文大字</p>
 <p class="text-body-base text-text">Body Base — 14px 正文默认 text-default</p>
 <p class="text-body-base text-text-secondary">Body Base — 14px 正文次要 text-secondary</p>
 <p class="text-body-base text-text-tertiary">Body Base — 14px 正文辅助 text-tertiary</p>
 <p class="text-body-md text-text-tertiary">Body MD — 12px</p>
 <p class="text-body-sm text-text-tertiary">Body SM — 11px 小字说明</p>
 <p class="text-body-xs text-text-tertiary">Body XS — 10px 极小标注</p>
 <p class="text-body-base text-text-disabled">Text Disabled — 禁用文字</p>
 <div class="h-px bg-[var(--border-neutral-l1)]" />
 <p class="text-body-base-strong font-medium text-text">Body Base Strong — 14px 500weight</p>
 <p class="text-body-md-strong font-medium text-text">Body MD Strong — 12px 500weight</p>
 <p class="text-body-sm-strong font-medium text-text">Body SM Strong — 11px 500weight</p>
 <p class="text-body-xs-strong font-medium text-text">Body XS Strong — 10px 500weight</p>
 </div>
 </section>

 <!-- ── 1.3 圆角·间距·阴影 ── -->
 <section :id="'tokens-spacing'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">圆角 · 间距 · 阴影</h2>

 <!-- 圆角 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">圆角令牌</p>
 <div class="mb-[var(--spacer-16)] flex flex-wrap gap-[var(--spacer-12)]">
 <div v-for="r in ['xs','sm','md','lg','xl','card-medium','card-soft','card-large']" :key="r" class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-12 w-12 border-2 border-[var(--border-brand)] bg-[var(--bg-white)]" :style="{ borderRadius: `var(--radius-${r})` }" />
 <span class="text-body-xs text-text-tertiary">{{ r }}</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-12 w-12 rounded-full border-2 border-[var(--border-brand)] bg-[var(--bg-white)]" />
 <span class="text-body-xs text-text-tertiary">full</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-12 w-20 border-2 border-[var(--border-brand)] bg-[var(--bg-white)]" style="border-radius: var(--radius-pill)" />
 <span class="text-body-xs text-text-tertiary">pill</span>
 </div>
 </div>

 <!-- 间距 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">间距令牌 spacer</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-8)]">
 <div v-for="s in [2,3,4,6,8,10,12,16,20,24,32,40,48,64]" :key="s" class="flex items-center gap-[var(--spacer-8)]">
 <span class="w-12 text-right text-body-xs text-text-tertiary tabular-nums">{{ s }}px</span>
 <div class="h-3 rounded-[var(--radius-sm)] bg-[var(--bg-brand)]" :style="{ width: `${s * 2}px` }" />
 </div>
 </div>

 <!-- 阴影 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">阴影令牌</p>
 <div class="flex flex-wrap gap-[var(--spacer-16)]">
 <div v-for="s in ['xs','sm','md','lg','xl','brand']" :key="s" class="flex flex-col items-center gap-[var(--spacer-8)]">
 <div class="h-14 w-14 rounded-[var(--radius-md)] bg-[var(--bg-white)]" :style="{ boxShadow: `var(--shadow-${s})` }" />
 <span class="text-body-xs text-text-tertiary">{{ s }}</span>
 </div>
 </div>
 </section>

 <!-- ── 1.4 系统令牌 ── -->
 <section :id="'tokens-system'" class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-default)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">系统令牌</h2>

 <!-- 图标尺寸 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">图标尺寸 icon-size</p>
 <div class="mb-[var(--spacer-16)] flex flex-wrap items-end gap-[var(--spacer-12)]">
 <div v-for="sz in [12,14,16,20,24,32,48]" :key="sz" class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="rounded-[var(--radius-sm)] bg-[var(--accent-teal)]" :style="{ width: `${sz}px`, height: `${sz}px` }" />
 <span class="text-body-xs text-text-tertiary tabular-nums">{{ sz }}</span>
 </div>
 </div>

 <!-- z-index 层级 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">z-index 层级</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-4)]">
 <div v-for="z in ['base','dropdown','sticky','fixed','modal-backdrop','modal','popover','toast','tooltip','notification']" :key="z" class="flex items-center gap-[var(--spacer-8)]">
 <span class="w-28 text-right text-body-xs text-text-tertiary">{{ z }}</span>
 <div class="h-4 rounded-[var(--radius-sm)] bg-[var(--bg-brand-light)]" :style="{ width: `calc(var(--z-${z}) / 1800 * 100%)` }" />
 <span class="text-body-xs text-text-tertiary tabular-nums">{{ z === 'base' ? '0' : '' }}</span>
 </div>
 </div>

 <!-- 焦点环 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">焦点环（无障碍）</p>
 <div class="mb-[var(--spacer-16)] flex gap-[var(--spacer-12)]">
 <button type="button" class="rounded-[var(--radius-md)] bg-[var(--bg-white)] px-[var(--spacer-16)] py-[var(--spacer-8)] text-body-sm text-text-secondary border border-[var(--border-neutral-l1)]">Tab 到这里看焦点环</button>
 <button type="button" class="rounded-[var(--radius-md)] bg-[var(--bg-brand)] px-[var(--spacer-16)] py-[var(--spacer-8)] text-body-sm text-onbrand">Tab 到这里</button>
 </div>

 <!-- 透明度 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">透明度</p>
 <div class="flex gap-[var(--spacer-12)]">
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-16 rounded-[var(--radius-md)] bg-[var(--bg-brand)]" style="opacity: var(--opacity-disabled)" />
 <span class="text-body-xs text-text-tertiary">disabled</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-16 rounded-[var(--radius-md)] bg-[var(--bg-brand)]" style="opacity: var(--opacity-overlay)" />
 <span class="text-body-xs text-text-tertiary">overlay</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <div class="h-10 w-16 rounded-[var(--radius-md)] bg-[var(--bg-brand)]" style="opacity: var(--opacity-readonly)" />
 <span class="text-body-xs text-text-tertiary">readonly</span>
 </div>
 </div>

 <!-- 控件令牌 -->
 <p class="mt-[var(--spacer-16)] mb-[var(--spacer-8)] text-body-sm text-text-tertiary">控件令牌 ds-control</p>
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div class="flex items-center gap-[var(--spacer-8)]">
 <span class="w-24 text-right text-body-xs text-text-tertiary">height-sm</span>
 <div class="w-24 rounded-[var(--ds-control-radius-sm)] border border-[var(--border-brand)] flex items-center justify-center text-body-xs text-text-tertiary" style="height: var(--ds-control-height-sm)">32px</div>
 </div>
 <div class="flex items-center gap-[var(--spacer-8)]">
 <span class="w-24 text-right text-body-xs text-text-tertiary">height-md</span>
 <div class="w-24 rounded-[var(--ds-control-radius-md)] border border-[var(--border-brand)] flex items-center justify-center text-body-xs text-text-tertiary" style="height: var(--ds-control-height-md)">44px</div>
 </div>
 <div class="flex items-center gap-[var(--spacer-8)]">
 <span class="w-24 text-right text-body-xs text-text-tertiary">height-lg</span>
 <div class="w-24 rounded-[var(--ds-control-radius-lg)] border border-[var(--border-brand)] flex items-center justify-center text-body-xs text-text-tertiary" style="height: var(--ds-control-height-lg)">48px</div>
 </div>
 </div>

 <!-- 动效 -->
 <p class="mt-[var(--spacer-16)] mb-[var(--spacer-8)] text-body-sm text-text-tertiary">动效令牌</p>
 <div class="flex flex-col gap-[var(--spacer-4)] text-body-xs text-text-tertiary">
 <p>duration-fast: 150ms / duration-normal: 250ms / duration-slow: 350ms</p>
 <p>ease-out: cubic-bezier(0.16, 1, 0.3, 1) / ease-in-out: cubic-bezier(0.4, 0, 0.2, 1)</p>
 <p>micro-duration: 100ms / hover-scale: 1.02 / press-scale: 0.97</p>
 </div>
 </section>

 <!-- ── 1.5 AI-Native 令牌 ── -->
 <section :id="'tokens-ai'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">AI-Native 令牌</h2>

 <!-- 气泡配色 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">对话气泡</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-12)]">
 <div class="ml-auto max-w-[75%] rounded-[var(--radius-card-large)] rounded-br-[var(--radius-xs)] p-[var(--spacer-12)] text-body-base" style="background: var(--user-bubble-bg); color: var(--user-bubble-text)">
 用户气泡 — user-bubble-bg / user-bubble-text
 </div>
 <div class="max-w-[75%] rounded-[var(--radius-card-large)] rounded-bl-[var(--radius-xs)] border p-[var(--spacer-12)] text-body-base" style="background: var(--ai-bubble-bg); color: var(--ai-bubble-text); border-color: var(--ai-bubble-border)">
 AI 气泡 — ai-bubble-bg / ai-bubble-text / ai-bubble-border
 </div>
 </div>

 <!-- 输入框 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">AI 输入框</p>
 <div class="mb-[var(--spacer-16)] flex items-center gap-[var(--spacer-8)] rounded-full border border-[var(--border-neutral-l1)] bg-[var(--bg-white)] px-[var(--spacer-16)]" style="height: var(--ai-input-height)">
 <span class="text-body-sm text-text-tertiary">ai-input-height: 48px / ai-input-radius: full</span>
 </div>

 <!-- 打字指示器 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">打字指示器 dots</p>
 <div class="flex items-center gap-[var(--spacer-4)]">
 <div class="rounded-full bg-[var(--bg-brand)]" style="width: var(--typing-dot-size); height: var(--typing-dot-size)" />
 <div class="rounded-full bg-[var(--bg-brand)]" style="width: var(--typing-dot-size); height: var(--typing-dot-size); opacity: 0.6" />
 <div class="rounded-full bg-[var(--bg-brand)]" style="width: var(--typing-dot-size); height: var(--typing-dot-size); opacity: 0.3" />
 <span class="ml-[var(--spacer-4)] text-body-xs text-text-tertiary">typing-dot-size: 8px</span>
 </div>
 </section>

 <!-- ══════════════════════════════════════════════════════
 二、公共组件 Shared Components
 ══════════════════════════════════════════════════════ -->

 <!-- ── 2.1 品牌·布局组件 ── -->
 <section :id="'comp-brand'" class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-default)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">品牌 · 布局组件</h2>

 <!-- BrandLogo -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">BrandLogo</p>
 <div class="mb-[var(--spacer-16)] flex items-end gap-[var(--spacer-24)]">
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <BrandLogo size="sm" />
 <span class="text-body-xs text-text-tertiary">sm</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <BrandLogo size="md" />
 <span class="text-body-xs text-text-tertiary">md</span>
 </div>
 <div class="flex flex-col items-center gap-[var(--spacer-4)]">
 <BrandLogo size="lg" />
 <span class="text-body-xs text-text-tertiary">lg</span>
 </div>
 </div>
 <div class="mb-[var(--spacer-16)] flex flex-col items-center gap-[var(--spacer-4)]">
 <BrandLogo size="sm" orientation="horizontal" />
 <span class="text-body-xs text-text-tertiary">horizontal</span>
 </div>

 <!-- AppHeader 变体 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">AppHeader（3 种 variant）</p>
 <div class="mb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-8)]">
 <div class="overflow-hidden rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)]">
 <AppHeader title="solid（默认）" :show-back="true" variant="solid" />
 <div class="h-8" />
 </div>
 <div class="overflow-hidden rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)]">
 <AppHeader title="frosted 磨玻璃" :show-back="true" variant="frosted" />
 <div class="h-8" />
 </div>
 <div class="overflow-hidden rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)]">
 <AppHeader title="transparent" :show-back="true" variant="transparent" />
 <div class="h-8" />
 </div>
 </div>

 <!-- PageShell -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">PageShell（页面骨架容器）</p>
 <div class="mb-[var(--spacer-16)] overflow-hidden rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)]" style="height: 120px">
 <PageShell :padded="true">
 <p class="text-body-sm text-text-tertiary">max-w-[480px] 居中 + bg + pb（底部导航留白）</p>
 </PageShell>
 </div>

 <!-- SectionHeading -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">SectionHeading</p>
 <SectionHeading text="区块标题示例" />
 </section>

 <!-- ── 2.2 数据展示组件 ── -->
 <section :id="'comp-data'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">数据展示组件</h2>

 <!-- StatRow -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">StatRow（水平统计行）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] shadow-[var(--shadow-xs)]">
 <StatRow
 :stats="[
 { value: 128, label: '收藏' },
 { value: '2.4k', label: '浏览' },
 { value: 36, label: '提问' },
 ]"
 />
 </div>

 <!-- ProfileHeader -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ProfileHeader</p>
 <ProfileHeader
 name="czwziy"
 avatar-text="C"
 badge="医护"
 :meta="['主治医师', '工号: HN00000001']"
 >
 <template #action>
 <button
 type="button"
 class="ds-icon-btn ds-icon-btn--sm ds-icon-btn--brand"
 aria-label="编辑"
 >
 <Pencil :size="16" class="icon" />
 </button>
 </template>
 </ProfileHeader>

 <!-- EmptyState -->
 <p class="mt-[var(--spacer-16)] mb-[var(--spacer-8)] text-body-sm text-text-tertiary">EmptyState</p>
 <div class="rounded-[var(--radius-card-large)] bg-[var(--bg-white)] border border-[var(--border-neutral-l1)]">
 <EmptyState text="暂无聊天记录" />
 </div>

 <!-- DisclaimerFooter -->
 <p class="mt-[var(--spacer-16)] mb-[var(--spacer-8)] text-body-sm text-text-tertiary">DisclaimerFooter</p>
 <DisclaimerFooter />
 </section>

 <!-- ── 2.3 列表样式 ── -->
 <section :id="'comp-list'" class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-default)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">列表样式</h2>

 <!-- 基础极简单行列表 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-list + ds-list-item（基础极简单行 · 分割线）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] border border-[var(--border-neutral-l1)] overflow-hidden">
 <ul class="ds-list">
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon"><MessageSquare :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">高血压用药咨询</span>
 <span class="ds-list-item__meta">2 小时前 · 患者 #1024</span>
 </div>
 <span class="ds-list-item__time">14:32</span>
 </li>
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon"><FileText :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">糖尿病饮食指南</span>
 <span class="ds-list-item__meta">昨天 · 张医生</span>
 </div>
 <span class="ds-list-item__time">09:15</span>
 </li>
 <li class="ds-list-item">
 <span class="ds-list-item__icon"><HeartPulse :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">心血管健康问答</span>
 <span class="ds-list-item__meta">3 天前 · 李医生</span>
 </div>
 <span class="ds-list-item__time">周一</span>
 </li>
 </ul>
 </div>

 <!-- 状态色图标变体 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-list-item__icon--brand / success / alert / error（状态色图标）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] border border-[var(--border-neutral-l1)] overflow-hidden">
 <ul class="ds-list">
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon ds-list-item__icon--brand"><Cpu :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">GPT-4o Provider</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--primary ds-tag--plain">LLM</span> · gpt-4o · 启用</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--success ds-tag--plain">启用</span>
 <button type="button" class="ds-list-item__action-btn" aria-label="编辑"><Pencil :size="16" /></button>
 </div>
 </li>
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon ds-list-item__icon--success"><CheckCircle2 :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">系统 Prompt v3</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--primary ds-tag--plain">System</span> · 生效中</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--success ds-tag--plain">生效</span>
 </div>
 </li>
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon ds-list-item__icon--alert"><AlertTriangle :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">患者提及胸闷气短症状</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--warning ds-tag--plain">紧急</span> · 待处理 · 14:32</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--danger">待处理</span>
 <button type="button" class="ds-list-item__action-btn" aria-label="处理"><CheckCircle2 :size="16" /></button>
 </div>
 </li>
 <li class="ds-list-item">
 <span class="ds-list-item__icon ds-list-item__icon--error"><AlertTriangle :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">检测到自杀相关敏感词</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--danger ds-tag--plain">危机</span> · 待处理 · 13:08</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--danger">待处理</span>
 <button type="button" class="ds-list-item__action-btn" aria-label="处理"><CheckCircle2 :size="16" /></button>
 </div>
 </li>
 </ul>
 </div>

 <!-- 列表项 - 引用管理（带操作按钮） -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">带多个操作按钮的列表项（通过/驳回/撤销）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] border border-[var(--border-neutral-l1)] overflow-hidden">
 <ul class="ds-list">
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon ds-list-item__icon--alert"><ArrowRightLeft :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">糖尿病饮食指南</span>
 <span class="ds-list-item__meta">心内科 → 内分泌科 · 李医生 · 2026-07-18</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--warning ds-tag--plain">待审</span>
 <button type="button" class="ds-list-item__action-btn" style="color: var(--status-success-default)" aria-label="通过"><CheckCircle2 :size="16" /></button>
 <button type="button" class="ds-list-item__action-btn" style="color: var(--status-error-default)" aria-label="驳回"><Trash2 :size="16" /></button>
 </div>
 </li>
 <li class="ds-list-item">
 <span class="ds-list-item__icon ds-list-item__icon--success"><ArrowRightLeft :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">高血压日常管理</span>
 <span class="ds-list-item__meta">心内科 → 全科 · 王医生 · 2026-07-15</span>
 </div>
 <div class="ds-list-item__trailing">
 <span class="ds-tag ds-tag--success ds-tag--plain">已通过</span>
 </div>
 </li>
 </ul>
 </div>

 <!-- 列表项 - 带 switch 开关 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">带 switch 开关的列表项（敏感词库）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] border border-[var(--border-neutral-l1)] overflow-hidden">
 <ul class="ds-list">
 <li class="ds-list-item ds-list-item--divider">
 <span class="ds-list-item__icon ds-list-item__icon--error"><AlertTriangle :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">想死</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--danger ds-tag--plain">自杀</span> · 启用</span>
 </div>
 <div class="ds-list-item__trailing">
 <label class="ds-switch"><input type="checkbox" class="ds-switch__input" :checked="true"><span class="ds-switch__track"><span class="ds-switch__thumb" /></span></label>
 <button type="button" class="ds-list-item__action-btn" aria-label="编辑"><Pencil :size="16" /></button>
 <button type="button" class="ds-list-item__action-btn" aria-label="删除"><Trash2 :size="16" /></button>
 </div>
 </li>
 <li class="ds-list-item">
 <span class="ds-list-item__icon ds-list-item__icon--alert"><AlertTriangle :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">胸痛</span>
 <span class="ds-list-item__meta"><span class="ds-tag ds-tag--warning ds-tag--plain">急诊</span> · 停用</span>
 </div>
 <div class="ds-list-item__trailing">
 <label class="ds-switch"><input type="checkbox" class="ds-switch__input" :checked="false"><span class="ds-switch__track"><span class="ds-switch__thumb" /></span></label>
 <button type="button" class="ds-list-item__action-btn" aria-label="编辑"><Pencil :size="16" /></button>
 <button type="button" class="ds-list-item__action-btn" aria-label="删除"><Trash2 :size="16" /></button>
 </div>
 </li>
 </ul>
 </div>

 <!-- Page Header + FAB 组合 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-page-header + ds-icon-btn + ds-fab 组合</p>
 <div class="mb-[var(--spacer-16)] relative rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)] overflow-hidden">
 <header class="ds-page-header">
 <div class="ds-page-header__title-wrap">
 <h1 class="ds-page-header__title">工作台</h1>
 <ChevronRight class="ds-page-header__chevron" :size="20" />
 </div>
 <div class="ds-page-header__actions">
 <button type="button" class="ds-icon-btn" aria-label="搜索"><Search :size="20" /></button>
 <button type="button" class="ds-avatar-btn" aria-label="头像"><User :size="20" /></button>
 </div>
 </header>
 <div class="px-[var(--spacer-16)] pb-[var(--spacer-16)]">
 <p class="text-body-sm text-text-tertiary">页面内容区域。右下角的 ds-fab 悬浮按钮在真实页面中固定到视口右下角。</p>
 </div>
 <button type="button" class="ds-fab" aria-label="新建" style="position: absolute; right: 16px; bottom: 16px;">
 <span class="text-2xl leading-none">+</span>
 </button>
 </div>

 <!-- FAB Segment 分段切换器 -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-fab-segment FAB 风格分段切换器</p>
 <div class="mb-[var(--spacer-16)]">
 <div class="ds-fab-segment ds-fab-segment--full">
 <button type="button" class="ds-fab-segment__btn ds-fab-segment__btn--active">医护</button>
 <button type="button" class="ds-fab-segment__btn">患者</button>
 </div>
 </div>
 </section>

 <!-- ── 2.3 菜单·操作组件 ── -->
 <section :id="'comp-menu'" class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-default)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">菜单 · 操作组件</h2>

 <!-- MenuList + MenuRow -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">MenuList + MenuRow</p>
 <div class="mb-[var(--spacer-16)]">
 <MenuList>
 <MenuRow :icon="User" label="账户" @click="() => {}" />
 <MenuRow :icon="MessageCircle" label="消息" @click="() => {}" />
 <MenuRow :icon="Globe" label="语言" @click="() => {}">
 <template #value>
 <span class="text-body-base text-text-tertiary">中文</span>
 <ChevronRight class="ml-[var(--spacer-4)] w-5 h-5 text-text-tertiary shrink-0" />
 </template>
 </MenuRow>
 </MenuList>
 </div>
 <div class="mb-[var(--spacer-16)]">
 <MenuList>
 <MenuRow :icon="LogOut" label="退出登录" danger @click="() => {}" />
 </MenuList>
 </div>

 <!-- QuickActionGrid + QuickActionItem -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">QuickActionGrid + QuickActionItem</p>
 <QuickActionGrid>
 <QuickActionItem
 :icon="BookOpen"
 label="知识管理"
 icon-color="var(--text-brand)"
 icon-bg="var(--bg-brand-light)"
 :badge="3"
 @click="() => {}"
 />
 <QuickActionItem
 :icon="Users"
 label="患者管理"
 icon-color="var(--accent-teal)"
 icon-bg="var(--accent-teal-surface)"
 @click="() => {}"
 />
 <QuickActionItem
 :icon="Stethoscope"
 label="在线问诊"
 icon-color="var(--text-brand)"
 icon-bg="var(--bg-brand-light)"
 @click="() => {}"
 />
 <QuickActionItem
 :icon="BarChart3"
 label="数据统计"
 icon-color="var(--accent-violet)"
 icon-bg="var(--accent-violet-surface)"
 @click="() => {}"
 />
 </QuickActionGrid>
 </section>

 <!-- ── 2.4 表单·交互组件 ── -->
 <section :id="'comp-form'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">表单 · 交互组件</h2>

 <!-- PasswordStrength -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">PasswordStrength（密码强度）</p>
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">密码</span>
 <input v-model="testPassword" type="password" class="ds-input" placeholder="输入密码查看强度">
 </div>
 <div class="px-[var(--spacer-12)] pb-[var(--spacer-8)]">
 <PasswordStrength :password="testPassword" />
 </div>
 </div>

 <!-- DsTabBar -->
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">DsTabBar（底部导航）</p>
 <div class="relative overflow-hidden rounded-[var(--radius-card-large)] border border-[var(--border-neutral-l1)]" style="height: 120px">
 <div class="flex h-full items-center justify-center">
 <p class="text-body-sm text-text-tertiary">底部导航预览区</p>
 </div>
 <div class="absolute bottom-0 left-0 right-0">
 <DsTabBar v-model="tabbarVal" :items="tabbarItems" />
 </div>
 </div>
 </section>

 <!-- ══════════════════════════════════════════════════════
 三、.ds 设计系统组件
 ══════════════════════════════════════════════════════ -->

 <section :id="'ds-components'" class="bg-[var(--bg-base-default)]">
 <!-- ds-pill -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-pill 胶囊选择器</h2>

 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">标准 ds-pill-group</p>
 <div class="ds-pill-group mb-[var(--spacer-16)]">
 <button
 v-for="opt in ['llm','embedding','rerank','rewrite']"
 :key="opt"
 type="button"
 class="ds-pill"
 :class="pillSelected === opt ? 'ds-pill--selected' : 'ds-pill--unselected'"
 @click="pillSelected = opt"
 >{{ opt.toUpperCase() }}</button>
 </div>

 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-pill--sm 小号</p>
 <div class="ds-pill-group mb-[var(--spacer-16)]">
 <span class="ds-pill ds-pill--sm ds-pill--selected">已启用</span>
 <span class="ds-pill ds-pill--sm ds-pill--unselected">已禁用</span>
 </div>

 <p class="mb-[var(--spacer-8)] text-body-sm text-text-tertiary">ds-pill--lg 大号</p>
 <div class="ds-pill-group">
 <button type="button" class="ds-pill ds-pill--lg ds-pill--selected">保存</button>
 <button type="button" class="ds-pill ds-pill--lg ds-pill--unselected">取消</button>
 </div>
 </div>

 <!-- ds-btn -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-btn 按钮</h2>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block">Primary Block</button>
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--block">Secondary Block</button>
 <button type="button" class="ds-btn ds-btn--brand-outline ds-btn--block">Brand Outline</button>
 <button type="button" class="ds-btn ds-btn--ghost ds-btn--block">Ghost Block</button>
 <div class="flex gap-[var(--spacer-8)]">
 <button type="button" class="ds-btn ds-btn--primary ds-btn--sm">Small</button>
 <button type="button" class="ds-btn ds-btn--secondary ds-btn--sm">Default</button>
 <button type="button" class="ds-btn ds-btn--brand-outline ds-btn--sm">Outline</button>
 </div>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--pill ds-btn--block">Pill Primary</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" disabled>Disabled</button>
 <button type="button" class="ds-btn ds-btn--primary ds-btn--block" disabled>
 <span class="ds-loading__spinner ds-loading--sm !w-4 !h-4 !border-[1.5px]" />
 加载中…
 </button>
 </div>
 </div>

 <!-- ds-tag -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-secondary)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-tag 标签</h2>
 <div class="flex flex-wrap items-center gap-[var(--spacer-8)]">
 <span class="ds-tag ds-tag--primary">Primary</span>
 <span class="ds-tag ds-tag--success">Success</span>
 <span class="ds-tag ds-tag--warning">Warning</span>
 <span class="ds-tag ds-tag--danger">Danger</span>
 <span class="ds-tag ds-tag--default">Default</span>
 <span class="ds-tag ds-tag--primary ds-tag--round">Round</span>
 <span class="ds-tag ds-tag--success ds-tag--plain">Plain</span>
 </div>
 </div>

 <!-- ds-switch -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-switch 开关</h2>
 <div class="flex items-center gap-[var(--spacer-16)]">
 <div class="flex items-center gap-[var(--spacer-8)]">
 <label class="ds-switch">
 <input v-model="switchVal" type="checkbox" class="ds-switch__input">
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 <span class="text-body-sm text-text-secondary">{{ switchVal ? 'ON' : 'OFF' }}</span>
 </div>
 <label class="ds-switch pointer-events-none opacity-50">
 <input type="checkbox" class="ds-switch__input" disabled>
 <span class="ds-switch__track"><span class="ds-switch__thumb" /></span>
 </label>
 <span class="text-body-sm text-text-disabled">Disabled</span>
 </div>
 </div>

 <!-- ds-search-box -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-secondary)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-search-box 搜索框</h2>
 <div class="ds-search-box">
 <Search class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input v-model="searchText" type="text" placeholder="搜索…" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>
 </div>

 <!-- ds-input -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-input 输入框</h2>
 <div class="flex flex-col gap-[var(--spacer-12)] rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">单行输入</span>
 <input v-model="inputVal" class="ds-input" placeholder="请输入内容">
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">密码</span>
 <input v-model="inputVal" type="password" class="ds-input" placeholder="请输入密码">
 </div>
 <div class="flex flex-col gap-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">带图标</span>
 <div class="ds-search-box">
 <Search class="h-4 w-4 shrink-0 text-icon-tertiary" />
 <input type="text" placeholder="搜索" class="min-w-0 flex-1 border-none bg-transparent font-heading text-body-base text-text outline-none placeholder:text-text-tertiary">
 </div>
 </div>
 </div>
 </div>

 <!-- ds-input textarea -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-secondary)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-input textarea（多行文本）</h2>
 <div class="rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-12)] border border-[var(--border-neutral-l1)]">
 <span class="mb-[var(--spacer-4)] block text-body-sm text-text-secondary">话术模板</span>
 <textarea v-model="textareaVal" class="ds-input" rows="3" placeholder="输入多行文本，测试自适应高度…" />
 </div>
 </div>

 <!-- ds-list 单元格组 -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-list 单元格组</h2>
 <div class="ds-list rounded-[var(--radius-card-large)] bg-[var(--bg-white)] overflow-hidden border border-[var(--border-neutral-l1)]">
 <div class="ds-list-item ds-list-item--divider">
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">标题</span>
 <span class="ds-list-item__meta">内容</span>
 </div>
 </div>
 <div class="ds-list-item ds-list-item--divider">
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">标题</span>
 <span class="ds-list-item__meta">描述信息 · 链接</span>
 </div>
 <span class="ds-list-item__trailing"><ChevronRight :size="16" class="text-icon-tertiary" /></span>
 </div>
 <div class="ds-list-item">
 <span class="ds-list-item__icon"><Globe :size="20" /></span>
 <div class="ds-list-item__content">
 <span class="ds-list-item__title">带图标</span>
 <span class="ds-list-item__meta">详情</span>
 </div>
 <span class="ds-list-item__trailing"><ChevronRight :size="16" class="text-icon-tertiary" /></span>
 </div>
 </div>
 </div>

 <!-- ds-tab 标签页 -->
 <div class="px-[var(--spacer-16)] py-[var(--spacer-16)] bg-[var(--bg-base-secondary)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">ds-tab 标签页</h2>
 <div class="flex gap-[var(--spacer-24)] border-b border-[var(--border-neutral-l1)] no-scrollbar overflow-x-auto">
 <button
 v-for="(tab, idx) in ['概览','详情','设置']"
 :key="tab"
 type="button"
 :class="activeTab === idx
 ? 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors font-medium text-text-brand'
 : 'relative whitespace-nowrap border-none bg-transparent py-[var(--spacer-12)] font-heading text-body-base transition-colors text-text-tertiary hover:text-text-brand'"
 @click="activeTab = idx"
 >{{ tab }}<span v-if="activeTab === idx" class="ds-tab-underline" /></button>
 </div>
 <div class="py-[var(--spacer-16)]">
 <p class="text-body-base text-text">{{ ['概览内容区域','详情内容区域','设置内容区域'][activeTab] }}</p>
 </div>
 </div>
 </section>

 <!-- ══════════════════════════════════════════════════════
 四、CSS 工具类
 ══════════════════════════════════════════════════════ -->

 <section :id="'utilities'" class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <h2 class="mb-[var(--spacer-12)] text-heading-sm font-semibold text-text">CSS 工具类</h2>

 <div class="flex flex-col gap-[var(--spacer-16)]">
 <!-- no-scrollbar -->
 <div>
 <p class="mb-[var(--spacer-4)] text-body-sm text-text-tertiary">.no-scrollbar — 隐藏滚动条</p>
 <div class="no-scrollbar flex gap-[var(--spacer-8)] overflow-x-auto rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-12)] border border-[var(--border-neutral-l1)]">
 <div v-for="i in 12" :key="i" class="h-16 w-16 flex-shrink-0 rounded-[var(--radius-md)] bg-[var(--bg-brand-light)] flex items-center justify-center text-body-sm text-text-brand">{{ i }}</div>
 </div>
 </div>

 <!-- tabular-nums -->
 <div>
 <p class="mb-[var(--spacer-4)] text-body-sm text-text-tertiary">.tabular-nums — 等宽数字（metric 字体）</p>
 <div class="rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] border border-[var(--border-neutral-l1)]">
 <p class="tabular-nums text-heading-lg" style="font-family: var(--font-family-metric)">123,456.78</p>
 <p class="tabular-nums text-heading-lg" style="font-family: var(--font-family-metric)">000,000.00</p>
 </div>
 </div>

 <!-- hero-orb -->
 <div>
 <p class="mb-[var(--spacer-4)] text-body-sm text-text-tertiary">.hero-orb — 品牌渐变球</p>
 <div class="flex items-center gap-[var(--spacer-16)]">
 <div class="hero-orb h-16 w-16 rounded-full" />
 <div class="hero-orb h-10 w-10 rounded-full" />
 <div class="hero-orb h-6 w-6 rounded-full" />
 </div>
 </div>

 <!-- tab-icon states -->
 <div>
 <p class="mb-[var(--spacer-4)] text-body-sm text-text-tertiary">.tab-icon-active / .tab-icon-inactive</p>
 <div class="flex gap-[var(--spacer-16)]">
 <div class="flex items-center gap-[var(--spacer-4)]">
 <Home class="tab-icon-active h-5 w-5" />
 <span class="text-body-sm text-text-brand">active</span>
 </div>
 <div class="flex items-center gap-[var(--spacer-4)]">
 <Home class="tab-icon-inactive h-5 w-5" />
 <span class="text-body-sm text-text-tertiary">inactive</span>
 </div>
 </div>
 </div>

 <!-- 卡片样式组合 -->
 <div>
 <p class="mb-[var(--spacer-4)] text-body-sm text-text-tertiary">卡片样式组合</p>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div class="rounded-[var(--radius-card-large)] bg-[var(--bg-white)] p-[var(--spacer-16)] shadow-[var(--shadow-sm)]">
 <p class="text-body-base text-text">radius-card-large + shadow-sm（菜单卡片）</p>
 </div>
 <div class="rounded-[var(--radius-card-soft)] bg-[var(--bg-white)] p-[var(--spacer-16)] shadow-[var(--shadow-xs)]">
 <p class="text-body-base text-text">radius-card-soft + shadow-xs（内容卡片）</p>
 </div>
 <div class="rounded-[var(--radius-card-medium)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-16)]">
 <p class="text-body-base text-text">radius-card-medium + border（表单分组）</p>
 </div>
 </div>
 </div>
 </div>
 </section>

 <div class="h-[var(--spacer-48)]" />
 </main>
</template>

<style scoped ponytail:allow-scoped-css Vue transition 过渡类，组件级>
.fade-enter-active, .fade-leave-active {
 transition: opacity var(--transition-fast);
}
.fade-enter-from, .fade-leave-to {
 opacity: 0;
}
</style>
