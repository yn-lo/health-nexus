<script setup lang="ts">
/**
 * ConsentNotice 知情告知层 — 首次进入时强制确认（gate），日常可随时回看（非 gate）。
 * 内容要点：AI 非医生、不做个体化诊疗/用药调整、急症走急诊、知识库可能不完整、
 * 匿名不转交医护也不留记录、数据用途。版本号仅记入本地（匿名不落库）。
 */
import { ref, watch } from 'vue'
import DsPopup from './DsPopup.vue'
import { acceptConsent } from '@/shared/utils/consent'

const props = withDefaults(defineProps<{
  show: boolean
  /** 强制确认模式：勾选后方可继续，且不可通过点击遮罩关闭 */
  gate?: boolean
}>(), {
  gate: false,
})

const emit = defineEmits<{ 'update:show': [value: boolean] }>()

/** 告知内容 — 与后端固定话术（rag 边界说明）保持一致口径 */
const NOTICE_ITEMS = [
  '本服务内容由 AI 生成，不是医生，不能替代面诊、诊断与治疗。',
  '不提供个体化的诊疗判断或用药调整建议（加量、减量、停药、换药），此类问题请咨询您的主治医生或药师。',
  '如出现胸痛、呼吸困难、意识不清等急症，请立即前往急诊或拨打 120，不要依赖本服务。',
  '回答基于本院已审核的知识库，可能不完整，也不一定完全适用于您的情况。',
  '未登录（匿名）时，您的提问不会转交医护人员，也不会留下记录、不会有人跟进；如需医护介入请登录后咨询。',
  '您输入的健康问题会用于生成回答；脱敏后的对话数据可能用于服务优化，详见《隐私政策》。',
]

const agreed = ref(false)

// 每次打开重置勾选状态（强制确认模式要求每次重新勾选）
watch(() => props.show, (visible) => {
  if (visible) agreed.value = false
})

/** 遮罩点击：强制确认模式下忽略，避免绕过告知 */
function onBackdropClose() {
  if (props.gate) return
  emit('update:show', false)
}

/** 确认：强制确认模式记录本地版本号后关闭 */
function onAccept() {
  if (props.gate) acceptConsent()
  emit('update:show', false)
}
</script>

<template>
  <DsPopup :show="show" position="center" @update:show="onBackdropClose">
    <div class="px-[var(--spacer-24)] pt-[var(--spacer-24)] pb-[calc(var(--spacer-24)+env(safe-area-inset-bottom,0px))]">
      <h2 class="m-0 font-heading text-heading-sm font-semibold text-text">
        使用前请了解
      </h2>

      <ul class="mt-[var(--spacer-16)] mb-0 list-none p-0 flex flex-col gap-[var(--spacer-12)]">
        <li
          v-for="item in NOTICE_ITEMS"
          :key="item"
          class="flex gap-[var(--spacer-8)] text-body-sm leading-relaxed text-text-secondary"
        >
          <span class="shrink-0 text-icon-tertiary">•</span>
          <span>{{ item }}</span>
        </li>
      </ul>

      <p class="mt-[var(--spacer-12)] mb-0 text-body-sm text-text-tertiary">
        完整条款见
        <RouterLink class="text-text-brand hover:underline underline-offset-2" to="/terms">《用户协议》</RouterLink>
        与
        <RouterLink class="text-text-brand hover:underline underline-offset-2" to="/privacy">《隐私政策》</RouterLink>
      </p>

      <label v-if="gate" class="ds-checkbox mt-[var(--spacer-20)]">
        <input v-model="agreed" type="checkbox" class="ds-checkbox__input">
        <span class="ds-checkbox__box" />
        <span class="text-body-sm text-text">我已阅读并理解上述内容</span>
      </label>

      <button
        type="button"
        class="ds-btn ds-btn--primary w-full mt-[var(--spacer-20)]"
        :disabled="gate && !agreed"
        @click="onAccept"
      >
        {{ gate ? '同意并继续' : '我知道了' }}
      </button>
    </div>
  </DsPopup>
</template>
