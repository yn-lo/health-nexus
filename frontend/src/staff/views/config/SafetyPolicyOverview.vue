<script setup lang="ts">
/**
 * 安全策略总览 — 展示后端实际生效的敏感词、输出规则、话术及来源
 * API: configApi.getSafetyPolicy
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ShieldCheck, Database, Code2, AlertTriangle, MessageSquareWarning } from '@lucide/vue'
import { useDsToast } from '@/shared/composables'
import { AppHeader, StatRow, SectionHeading } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import type { SafetyPolicyResponse, SafetyPolicyWords, SafetyPolicyOutputRule } from '@/shared'

const router = useRouter()
const { showFailToast } = useDsToast()

const loading = ref(false)
const policy = ref<SafetyPolicyResponse | null>(null)

const categoryLabel: Record<string, string> = {
 suicide: '自杀/自残',
 emergency: '急诊/紧急',
 injection: '注入攻击',
}

const ruleCategoryLabel: Record<string, string> = {
 stop_medication: '停药建议',
 prescription: '处方建议',
 diagnosis: '诊断建议',
 delay_medical: '延误就医',
 other: '其他',
}

const ruleActionLabel: Record<string, string> = {
 block: '拦截',
 replace: '替换',
}

const sourceLabel: Record<string, string> = {
 default: '默认值',
 database: '数据库',
 hardcoded: '硬编码',
}

const sourceIcon: Record<string, typeof Database> = {
 default: Code2,
 database: Database,
 hardcoded: Code2,
}

const heroStats = computed(() => {
 if (!policy.value) return []
 const p = policy.value
 const wordCount = p.input_sensitive_words.suicide.words.length
 + p.input_sensitive_words.emergency.words.length
 + p.input_sensitive_words.injection.words.length
 return [
 { value: wordCount, label: '敏感词' },
 { value: p.output_rules.length, label: '输出规则' },
 { value: 6, label: '话术项' },
 ]
})

const wordSections = computed<{ category: string; label: string; data: SafetyPolicyWords }[]>(() => {
 if (!policy.value) return []
 const p = policy.value
 return [
 { category: 'suicide', label: categoryLabel.suicide, data: p.input_sensitive_words.suicide },
 { category: 'emergency', label: categoryLabel.emergency, data: p.input_sensitive_words.emergency },
 { category: 'injection', label: categoryLabel.injection, data: p.input_sensitive_words.injection },
 ]
})

const groupedRules = computed<{ category: string; label: string; rules: SafetyPolicyOutputRule[] }[]>(() => {
 if (!policy.value) return []
 const groups: Record<string, SafetyPolicyOutputRule[]> = {}
 for (const r of policy.value.output_rules) {
 if (!groups[r.category]) groups[r.category] = []
 groups[r.category].push(r)
 }
 const order = ['stop_medication', 'prescription', 'diagnosis', 'delay_medical', 'other']
 return order
 .filter(c => groups[c])
 .map(c => ({ category: c, label: ruleCategoryLabel[c] || c, rules: groups[c] }))
})

const messageFields = computed<{ key: string; label: string; value: string }[]>(() => {
 if (!policy.value) return []
 const m = policy.value.messages
 return [
 { key: 'rejection_message', label: '拒答话术', value: m.rejection_message },
 { key: 'emergency_message', label: '紧急响应话术', value: m.emergency_message },
 { key: 'crisis_response', label: '危机干预话术', value: m.crisis_response },
 { key: 'safety_warning_message', label: '安全警告话术', value: m.safety_warning_message },
 { key: 'crisis_hotline', label: '危机热线', value: m.crisis_hotline },
 { key: 'medication_disclaimer', label: '用药免责声明', value: m.medication_disclaimer },
 ]
})

async function load() {
 loading.value = true
 try {
 policy.value = await configApi.getSafetyPolicy()
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

function goEdit(routeName: string) {
 router.push({ name: routeName })
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="安全策略总览" @back="router.back" />

 <div v-if="loading" class="flex items-center justify-center py-[var(--spacer-48)]">
 <span class="text-body-sm text-text-tertiary">加载中…</span>
 </div>

 <div v-else-if="policy" class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-24)]">
 <!-- Hero -->
 <div class="ds-card p-[var(--spacer-20)] bg-[var(--ai-gradient-soft)]">
 <StatRow :stats="heroStats" />
 </div>

 <!-- 输入敏感词 -->
 <section>
 <div class="flex items-center justify-between mb-[var(--spacer-8)]">
 <SectionHeading text="输入敏感词" />
 <button type="button" class="text-body-xs text-text-brand" @click="goEdit('staff-config-sensitive-words')">管理 →</button>
 </div>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div
 v-for="sec in wordSections"
 :key="sec.category"
 class="rounded-[var(--radius-card-soft)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]"
 >
 <div class="flex items-center gap-[var(--spacer-8)] mb-[var(--spacer-8)]">
 <AlertTriangle class="h-4 w-4 text-[var(--status-error-default)]" />
 <span class="text-body-sm font-medium text-text">{{ sec.label }}</span>
 <span class="ml-auto inline-flex items-center gap-[var(--spacer-4)] rounded-[var(--radius-full)] px-[var(--spacer-8)] py-[2px] text-[10px] font-medium leading-none"
 :class="sec.data.source === 'database' ? 'bg-[var(--bg-brand-light)] text-text-brand' : 'bg-[var(--bg-overlay-l1)] text-text-tertiary'"
 >
 <component :is="sourceIcon[sec.data.source]" class="h-3 w-3" />
 {{ sourceLabel[sec.data.source] }}
 </span>
 </div>
 <div class="flex flex-wrap gap-[var(--spacer-4)]">
 <span
 v-for="word in sec.data.words"
 :key="word"
 class="inline-block rounded-[var(--radius-full)] bg-[var(--status-error-light)] px-[var(--spacer-8)] py-[2px] text-body-xs text-[var(--status-error-default)]"
 >{{ word }}</span>
 <span v-if="sec.data.words.length === 0" class="text-body-xs text-text-tertiary">（空）</span>
 </div>
 </div>
 </div>
 </section>

 <!-- 输出安全规则 -->
 <section>
 <div class="flex items-center justify-between mb-[var(--spacer-8)]">
 <SectionHeading text="输出安全规则" />
 <button type="button" class="text-body-xs text-text-brand" @click="goEdit('staff-config-safety-rules')">管理 →</button>
 </div>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div
 v-for="group in groupedRules"
 :key="group.category"
 class="rounded-[var(--radius-card-soft)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]"
 >
 <div class="flex items-center gap-[var(--spacer-8)] mb-[var(--spacer-8)]">
 <ShieldCheck class="h-4 w-4 text-icon-brand" />
 <span class="text-body-sm font-medium text-text">{{ group.label }}</span>
 <span class="ml-auto inline-flex items-center gap-[var(--spacer-4)] rounded-[var(--radius-full)] px-[var(--spacer-8)] py-[2px] text-[10px] font-medium leading-none"
 :class="group.rules[0]?.source === 'database' ? 'bg-[var(--bg-brand-light)] text-text-brand' : 'bg-[var(--bg-overlay-l1)] text-text-tertiary'"
 >
 <component :is="sourceIcon[group.rules[0]?.source || 'hardcoded']" class="h-3 w-3" />
 {{ sourceLabel[group.rules[0]?.source || 'hardcoded'] }}
 </span>
 </div>
 <div class="flex flex-col gap-[var(--spacer-6)]">
 <div
 v-for="(rule, idx) in group.rules"
 :key="idx"
 class="rounded-[var(--radius-card-soft)] bg-[var(--bg-base-default)] px-[var(--spacer-10)] py-[var(--spacer-8)]"
 >
 <div class="flex items-center gap-[var(--spacer-6)] mb-[var(--spacer-4)]">
 <span class="inline-block rounded-[var(--radius-full)] px-[var(--spacer-6)] py-[1px] text-[10px] font-medium leading-none"
 :class="rule.action === 'block' ? 'bg-[var(--status-error-light)] text-[var(--status-error-default)]' : 'bg-[var(--bg-brand-light)] text-text-brand'"
 >{{ ruleActionLabel[rule.action] || rule.action }}</span>
 <code class="text-body-xs text-text-tertiary truncate">{{ rule.pattern }}</code>
 </div>
 <p v-if="rule.replacement" class="text-body-xs text-text-secondary line-clamp-2">{{ rule.replacement }}</p>
 </div>
 </div>
 </div>
 </div>
 </section>

 <!-- 安全话术 -->
 <section>
 <div class="flex items-center justify-between mb-[var(--spacer-8)]">
 <SectionHeading text="安全话术" />
 <button type="button" class="text-body-xs text-text-brand" @click="goEdit('staff-config-safety-messages')">管理 →</button>
 </div>
 <div class="flex flex-col gap-[var(--spacer-8)]">
 <div
 v-for="f in messageFields"
 :key="f.key"
 class="rounded-[var(--radius-card-soft)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-12)] py-[var(--spacer-10)]"
 >
 <div class="flex items-center gap-[var(--spacer-6)] mb-[var(--spacer-4)]">
 <MessageSquareWarning class="h-3.5 w-3.5 text-icon-brand" />
 <span class="text-body-sm font-medium text-text">{{ f.label }}</span>
 </div>
 <p class="text-body-xs text-text-secondary">{{ f.value || '（未设置）' }}</p>
 </div>
 </div>
 </section>
 </div>
 </main>
</template>
