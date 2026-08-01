<script setup lang="ts">
/**
 * 安全话术配置 — 6 字段单例 GET/PUT
 * API: configApi.getSafetyMessages/updateSafetyMessages
 * 当前阶段实际消费：通过适配器注入聊天流，Redis 缓存
 */
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useDsToast } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { configApi } from '@/shared'
import { errmsg } from '@/shared/api/client'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()

const loading = ref(false)
const saving = ref(false)
const updated_at = ref('')

const form = reactive({
 rejection_message: '',
 emergency_message: '',
 safety_warning_message: '',
 crisis_response: '',
 no_knowledge_message: '',
 system_error_message: '',
})

// 输入侧：患者消息命中敏感词时触发推送
const inputSideFields: { key: keyof typeof form; label: string; description: string; placeholder: string; rows: number; required: boolean }[] = [
 { key: 'emergency_message', label: '紧急响应话术', description: '患者描述的症状命中紧急关键词（胸痛、呼吸困难、大出血等）时，立即推送就医提醒。', placeholder: '您描述的症状需要紧急就医，请立即前往最近的医院急诊科或拨打 120。', rows: 3, required: true },
 { key: 'crisis_response', label: '危机干预话术', description: '患者消息命中自杀/自残相关关键词时，推送心理危机干预文本（建议包含心理援助热线）。', placeholder: '如果您正在经历心理困扰或有自伤想法，请立即拨打心理援助热线 400-161-9995，或前往最近的医院急诊科。', rows: 3, required: false },
 { key: 'rejection_message', label: '输入拒答话术', description: '注入攻击命中或 LLM 审查判定输入不当时，以此话术拒答，不调用 AI 生成回答。', placeholder: '抱歉，我无法回答这个问题，建议您咨询您的主治医生。', rows: 3, required: true },
]

// 输出侧：AI 回答检测 / 知识库检索 / 系统异常时触发推送
const outputSideFields: { key: keyof typeof form; label: string; description: string; placeholder: string; rows: number; required: boolean }[] = [
 { key: 'no_knowledge_message', label: '无知识话术', description: '知识库检索无相关结果或检索服务故障时，以此话术告知患者。', placeholder: '抱歉，知识库中暂无与您问题相关的内容，建议您咨询主治医生或换个问法试试。', rows: 3, required: true },
 { key: 'system_error_message', label: '系统异常话术', description: 'LLM 服务故障（空输出 / 流中断）时，以此话术告知患者。', placeholder: '抱歉，系统暂时繁忙未能生成回答，请稍后重试。', rows: 3, required: true },
 { key: 'safety_warning_message', label: '安全警告话术', description: 'AI 回答涉及用药或治疗建议时，在回答末尾追加安全警告（含用药免责声明）。', placeholder: '请注意：以上信息仅供参考，不能替代专业医疗诊断和治疗。用药请严格遵照医嘱，如有疑问请咨询您的主治医生或药师。', rows: 2, required: false },
]

/** 输入/输出侧话术分组渲染（合并结构相同的两块，消除模板重复） */
const messageSideSections = [
 { title: '输入侧：患者消息触发时推送', borderClass: 'border-[var(--border-error)]', fields: inputSideFields },
 { title: '输出侧：AI 回答检测 / 系统异常时推送', borderClass: 'border-[var(--border-brand)]', fields: outputSideFields },
]

async function load() {
 loading.value = true
 try {
 const m = await configApi.getSafetyMessages()
 form.rejection_message = m.rejection_message
 form.emergency_message = m.emergency_message
 form.safety_warning_message = m.safety_warning_message
 form.crisis_response = m.crisis_response
 form.no_knowledge_message = m.no_knowledge_message
 form.system_error_message = m.system_error_message
 updated_at.value = m.updated_at
 } catch (e) {
 showFailToast(errmsg(e, '加载失败'))
 } finally {
 loading.value = false
 }
}

async function save() {
 if (!form.rejection_message.trim() || !form.emergency_message.trim() || !form.no_knowledge_message.trim() || !form.system_error_message.trim()) {
 showFailToast('拒答 / 无知识 / 系统异常 / 紧急 不可为空')
 return
 }
 saving.value = true
 try {
 const m = await configApi.updateSafetyMessages({ ...form })
 updated_at.value = m.updated_at
 showSuccessToast('已保存')
 } catch (e) {
 showFailToast(errmsg(e, '保存失败'))
 } finally {
 saving.value = false
 }
}

onMounted(load)
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
 <AppHeader title="安全话术" @back="router.back" />

 <section class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <!-- 顶部说明：触发场景总览 -->
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-soft)] bg-[var(--ai-gradient-soft)] p-[var(--spacer-16)]">
 <h2 class="text-[var(--body-lg-strong-font-size)] font-semibold text-text">安全话术配置</h2>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">
 患者在聊天中触发特定安全条件时，系统自动推送对应话术（SSE 事件）。读取失败时后端降级使用内置默认值。
 </p>
 <div class="mt-[var(--spacer-12)] flex flex-col gap-[var(--spacer-4)] text-body-sm text-text-secondary">
 <p><span class="font-medium text-text">输入侧触发</span>（患者消息命中敏感词）→ 推送 <span class="text-text-brand">紧急响应</span> / <span class="text-text-brand">危机干预</span> / <span class="text-text-brand">输入拒答</span></p>
 <p><span class="font-medium text-text">输出侧触发</span>（AI 回答检测/系统异常）→ 推送 <span class="text-text-brand">无知识</span> / <span class="text-text-brand">系统异常</span> / <span class="text-text-brand">安全警告</span></p>
 </div>
 </div>

 <div class="flex flex-col gap-[var(--spacer-16)]">
 <div v-for="section in messageSideSections" :key="section.title">
 <div class="text-body-sm font-medium text-text-secondary mb-[var(--spacer-8)] pl-[var(--spacer-12)] border-l-2" :class="section.borderClass">{{ section.title }}</div>
 <div class="flex flex-col gap-[var(--spacer-12)]">
 <div
 v-for="f in section.fields"
 :key="f.key"
 class="rounded-[var(--radius-card-soft)] border border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]"
 >
 <span class="mb-[var(--spacer-2)] block text-body-sm font-medium text-text">{{ f.label }}<span v-if="f.required" class="text-[var(--status-error-default)]">*</span></span>
 <p class="mb-[var(--spacer-8)] text-body-sm text-text-secondary">{{ f.description }}</p>
 <textarea v-model="form[f.key]" :rows="f.rows" :placeholder="f.placeholder" class="ds-textarea ds-textarea--secondary"></textarea>
 </div>
 </div>
 </div>
 </div>

 <div class="mt-[var(--spacer-24)] flex items-center justify-between">
 <span v-if="updated_at" class="text-body-xs text-text-tertiary">
 上次更新：{{ updated_at.slice(0, 19).replace('T', ' ') }}
 </span>
 <button type="button" class="ds-btn ds-btn--primary ml-auto" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
 </div>
 </section>
 </main>
</template>