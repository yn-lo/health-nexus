<script setup lang="ts">
/**
 * 邀请码管理 — 管理员生成患者注册邀请码 + 列表查看
 * API: authApi.createInviteCodes / authApi.listInviteCodes
 * 生成：6 位纯数字、有效期 30 天、一次性；PATIENT 注册强制必填。
 */
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { KeyRound, Plus, Copy, Check } from '@lucide/vue'
import { AppHeader } from '@/shared/components'
import { useDsToast } from '@/shared/composables'
import { authApi, errmsg, usePagedList } from '@/shared'
import type { InviteCode } from '@/shared'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()

// 分页列表（含已用/已过期）
const { items: codes, loading, load } = usePagedList<InviteCode>({
  pageSize: 50,
  fetcher: (params) => authApi.listInviteCodes(params),
})

// 生成表单
const count = ref(1)
const generating = ref(false)
// 本次生成的新码（蓝色高亮提示，含复制）
const newCodes = ref<InviteCode[]>([])

async function generate() {
  const n = Math.max(1, Math.min(100, Math.floor(count.value) || 1))
  generating.value = true
  try {
    const created = await authApi.createInviteCodes(n)
    newCodes.value = created
    showSuccessToast(`已生成 ${created.length} 个邀请码`)
    await load()
  } catch (e) {
    showFailToast(errmsg(e, '生成失败'))
  } finally {
    generating.value = false
  }
}

function statusOf(c: InviteCode) {
  if (c.used_at) return { text: '已使用', cls: 'ds-tag--neutral ds-tag--plain' }
  if (new Date(c.expires_at).getTime() <= Date.now()) return { text: '已过期', cls: 'ds-tag--error ds-tag--plain' }
  return { text: '未使用', cls: 'ds-tag--success ds-tag--plain' }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    showSuccessToast('已复制')
  } catch {
    showFailToast('复制失败，请手动选择复制')
  }
}

const stat = computed(() => {
  const total = codes.value.length
  const unused = codes.value.filter((c) => !c.used_at && new Date(c.expires_at).getTime() > Date.now()).length
  return [
    { value: String(total), label: '累计' },
    { value: String(unused), label: '可用' },
  ]
})

onMounted(() => load())
</script>

<template>
 <main class="mx-auto min-h-screen min-h-dvh max-w-[480px] bg-[var(--bg-base-default)] pb-24">
  <AppHeader title="邀请码管理" @back="router.back" />

  <div class="px-[var(--spacer-16)] pt-[var(--spacer-8)] pb-[var(--spacer-16)] flex flex-col gap-[var(--spacer-24)]">
   <!-- 生成区 -->
   <section class="ds-card p-[var(--spacer-16)]">
    <h2 class="mb-[var(--spacer-12)] flex items-center gap-[var(--spacer-8)] text-body-base font-medium">
     <KeyRound class="h-4 w-4 text-icon-brand" />
     生成邀请码
    </h2>
    <div class="flex items-end gap-[var(--spacer-8)]">
     <div class="flex min-w-0 flex-1 flex-col gap-[var(--spacer-4)]">
      <label class="text-body-sm text-text-secondary">数量</label>
      <input v-model.number="count" type="number" min="1" max="100" class="ds-input" aria-label="生成数量">
     </div>
     <button type="button" class="ds-btn ds-btn--primary shrink-0" :disabled="generating" @click="generate">
      <Plus class="h-4 w-4" />
      {{ generating ? '生成中…' : '生成' }}
     </button>
    </div>
    <p class="mt-[var(--spacer-8)] text-body-xs text-text-tertiary">每个码 6 位数字、有效期 30 天、一次性，仅限生成 1-100 个</p>
   </section>

   <!-- 本次生成的新码（点击复制） -->
   <section v-if="newCodes.length" class="flex flex-wrap gap-[var(--spacer-8)]">
    <button
     v-for="c in newCodes"
     :key="c.code"
     type="button"
     class="ds-tag ds-tag--brand inline-flex cursor-pointer items-center gap-[var(--spacer-4)] px-[var(--spacer-12)] py-[var(--spacer-6)]"
     @click="copy(c.code)"
    >
     {{ c.code }}<Copy class="h-3 w-3" />
    </button>
   </section>

   <!-- 全部邀请码列表 -->
   <section :aria-busy="loading" class="flex flex-col gap-[var(--spacer-12)]">
    <div class="flex items-center gap-[var(--spacer-16)]">
     <h2 class="text-body-base font-medium">全部邀请码</h2>
     <span class="flex gap-[var(--spacer-8)] text-body-xs text-text-tertiary">
      <span v-for="s in stat" :key="s.label">{{ s.label }} {{ s.value }}</span>
     </span>
    </div>

    <p v-if="loading" class="text-body-sm text-text-tertiary">加载中…</p>
    <p v-else-if="!codes.length" class="text-body-sm text-text-tertiary">暂无邀请码，请先生成</p>

    <article v-for="c in codes" :key="c.id" class="ds-list-item ds-list-item--divider">
     <span class="ds-list-item__icon ds-list-item__icon--brand">
      <KeyRound :size="18" />
     </span>
     <div class="ds-list-item__content">
      <span class="ds-list-item__title font-mono tracking-widest">{{ c.code }}</span>
      <span class="ds-list-item__meta">
       <span class="ds-tag ds-tag--plain" :class="statusOf(c).cls">{{ statusOf(c).text }}</span>
       <span class="text-body-xs text-text-tertiary">过期 {{ new Date(c.expires_at).toLocaleDateString() }}</span>
       <span v-if="c.used_at" class="text-body-xs text-text-tertiary">· {{ new Date(c.used_at).toLocaleString() }} 已用</span>
      </span>
     </div>
     <div class="ds-list-item__trailing">
      <button type="button" class="ds-btn ds-btn--ghost ds-btn--sm" aria-label="复制邀请码" @click="copy(c.code)">
       <Check class="h-4 w-4" />
      </button>
     </div>
    </article>
   </section>
  </div>
 </main>
</template>