<script setup lang="ts">
/**
 * 品牌配置 — 单例 GET/PUT（仅超级管理员）
 * API: configApi.getBrandConfig / updateBrandConfig
 * Logo / favicon 复用知识库图片上传（wikiApi.uploadArticleImage → /uploads/{name}），避免重复上传链路
 */
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useDsToast } from '@/shared/composables'
import { AppHeader } from '@/shared/components'
import { configApi, wikiApi, BRAND_LIMITS } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { useBrandStore } from '@/stores/brand'
import type { BrandConfig as BrandConfigData, BrandConfigUpdateRequest } from '@/shared'
import logoFallback from '@/assets/images/hn_logo.webp'

const router = useRouter()
const { showSuccessToast, showFailToast } = useDsToast()
const brandStore = useBrandStore()

const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const updated_at = ref('')

const form = reactive({
  name: '',
  wordmark: '',
  org_name: '',
  slogan: '',
  site_url: '',
  site_title: '',
  logo_url: '',
  favicon_url: '',
})

const logoPreview = computed(() => form.logo_url || logoFallback)

/** URL 校验：空串允许（回退默认），否则须为站内路径或 http(s) 地址（与后端一致，拒绝伪协议）。 */
function isInvalidUrl(v: string): boolean {
  if (!v) return false
  return !(v.startsWith('/') || v.startsWith('http://') || v.startsWith('https://'))
}

const errors = computed<Record<string, string>>(() => {
  const e: Record<string, string> = {}
  if (form.name.length > BRAND_LIMITS.name) e.name = `不能超过 ${BRAND_LIMITS.name} 字符`
  if (form.wordmark.length > BRAND_LIMITS.wordmark) e.wordmark = `不能超过 ${BRAND_LIMITS.wordmark} 字符`
  if (form.org_name.length > BRAND_LIMITS.org_name) e.org_name = `不能超过 ${BRAND_LIMITS.org_name} 字符`
  if (form.slogan.length > BRAND_LIMITS.slogan) e.slogan = `不能超过 ${BRAND_LIMITS.slogan} 字符`
  if (form.site_url && !/^https?:\/\//.test(form.site_url)) e.site_url = '须为 http(s) 绝对地址'
  if (form.site_title.length > BRAND_LIMITS.site_title) e.site_title = `不能超过 ${BRAND_LIMITS.site_title} 字符`
  if (isInvalidUrl(form.logo_url)) e.logo_url = '须为站内路径（/uploads/…）或 http(s) 地址'
  if (isInvalidUrl(form.favicon_url)) e.favicon_url = '须为站内路径（/uploads/…）或 http(s) 地址'
  return e
})

const hasError = computed(() => Object.keys(errors.value).length > 0)

async function load() {
  loading.value = true
  try {
    const c: BrandConfigData = await configApi.getBrandConfig()
    form.name = c.name
    form.wordmark = c.wordmark
    form.org_name = c.org_name
    form.slogan = c.slogan
    form.site_url = c.site_url
    form.site_title = c.site_title
    form.logo_url = c.logo_url
    form.favicon_url = c.favicon_url
    updated_at.value = c.updated_at
  } catch (e) {
    showFailToast(errmsg(e, '加载失败'))
  } finally {
    loading.value = false
  }
}

/** 复用知识库图片上传接口，成功后将返回的 url 回填到指定字段。 */
async function uploadImage(field: 'logo_url' | 'favicon_url', file: File) {
  uploading.value = true
  try {
    const res = await wikiApi.uploadArticleImage(file)
    form[field] = res.url
    showSuccessToast('上传成功')
  } catch (e) {
    showFailToast(errmsg(e, '上传失败'))
  } finally {
    uploading.value = false
  }
}

function onPick(field: 'logo_url' | 'favicon_url', e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) void uploadImage(field, file)
  input.value = ''
}

async function save() {
  if (hasError.value) {
    showFailToast('请先修正表单错误')
    return
  }
  saving.value = true
  try {
    const patch: BrandConfigUpdateRequest = { ...form }
    const c = await configApi.updateBrandConfig(patch)
    updated_at.value = c.updated_at
    // 同步全局 store：同标签页即时刷新 Logo / 品牌名 / 标题（无需刷新页面）
    brandStore.setBrand(c)
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
 <AppHeader title="品牌配置" @back="router.back" />

 <section class="px-[var(--spacer-16)] py-[var(--spacer-16)]">
 <div class="mb-[var(--spacer-16)] rounded-[var(--radius-card-soft)] bg-[var(--ai-gradient-soft)] p-[var(--spacer-16)]">
 <h2 class="text-[var(--body-lg-strong-font-size)] font-semibold text-text">品牌配置</h2>
 <p class="mt-[var(--spacer-4)] text-body-sm text-text-secondary">单例配置，影响患者端与医护端的 Logo、品牌名称、单位名称、浏览器标题与 favicon。仅超级管理员可修改。</p>
 </div>

 <div class="flex flex-col gap-[var(--spacer-12)]">
 <!-- 品牌名称 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">品牌名称</span>
 <span class="ds-pill ds-pill--sm">≤ {{ BRAND_LIMITS.name }}</span>
 </div>
 <input v-model="form.name" type="text" class="ds-input ds-input--secondary" placeholder="如：Health Nexus">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">展示于 Logo 旁的主名称；留空则回退默认值。</p>
 <p v-if="errors.name" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.name }}</p>
 </div>

 <!-- 品牌字标 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">品牌字标</span>
 <span class="ds-pill ds-pill--sm">≤ {{ BRAND_LIMITS.wordmark }}</span>
 </div>
 <input v-model="form.wordmark" type="text" class="ds-input ds-input--secondary" placeholder="如：HEALTH NEXUS">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">登录/注册页与「关于我们」页顶部的横排字标，建议大写；留空则回退默认值。</p>
 <p v-if="errors.wordmark" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.wordmark }}</p>
 </div>

 <!-- 单位名称 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">单位名称</span>
 <span class="ds-pill ds-pill--sm">≤ {{ BRAND_LIMITS.org_name }}</span>
 </div>
 <input v-model="form.org_name" type="text" class="ds-input ds-input--secondary" placeholder="如：某某市第一人民医院">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">展示于登录 / 注册 / 忘记密码页的品牌行与标语之间；留空则不显示。</p>
 <p v-if="errors.org_name" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.org_name }}</p>
 </div>

 <!-- 系统标语 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">系统标语</span>
 <span class="ds-pill ds-pill--sm">≤ {{ BRAND_LIMITS.slogan }}</span>
 </div>
 <input v-model="form.slogan" type="text" class="ds-input ds-input--secondary" placeholder="例如：智能健康\n宣教平台">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">统一驱动登录页大标题与「关于我们」页副标题；留空则两处各自使用默认文案。</p>
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">支持用 \n 表示换行：输入「智能健康\n宣教平台」即显示为两行。</p>
 <p v-if="errors.slogan" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.slogan }}</p>
 </div>

 <!-- 站点标题 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">站点标题</span>
 <span class="ds-pill ds-pill--sm">≤ {{ BRAND_LIMITS.site_title }}</span>
 </div>
 <input v-model="form.site_title" type="text" class="ds-input ds-input--secondary" placeholder="如：健康宣教平台">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">浏览器标签标题。患者端显示为「患者端 - 站点标题」，医护端为「医护端 - 站点标题」；留空则回退默认标题。</p>
 <p v-if="errors.site_title" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.site_title }}</p>
 </div>

 <!-- Logo -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-8)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">Logo 图片</span>
 <span class="ds-pill ds-pill--sm">≤ 5MB</span>
 </div>
 <div class="mb-[var(--spacer-8)] flex items-center gap-[var(--spacer-12)]">
 <img :src="logoPreview" alt="Logo 预览" class="h-16 w-16 shrink-0 rounded-[var(--radius-card-soft)] bg-[var(--bg-base-default)] object-contain p-[var(--spacer-4)] shadow-[var(--shadow-sm)]">
 <label class="ds-btn ds-btn--secondary ds-btn--sm">
  {{ uploading ? '上传中…' : '上传图片' }}
  <input type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" :disabled="uploading" @change="onPick('logo_url', $event)">
 </label>
 </div>
 <input v-model="form.logo_url" type="text" class="ds-input ds-input--secondary" placeholder="留空使用默认 Logo，或填写图片地址">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">建议正方形 PNG，透明背景更佳。留空则使用内置 Logo。</p>
 <p v-if="errors.logo_url" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.logo_url }}</p>
 </div>

 <!-- favicon -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-8)] flex items-center gap-[var(--spacer-8)]">
 <span class="text-body-sm text-text-secondary">favicon（浏览器图标）</span>
 </div>
 <div class="mb-[var(--spacer-8)] flex items-center gap-[var(--spacer-12)]">
 <img v-if="form.favicon_url" :src="form.favicon_url" alt="favicon 预览" class="h-8 w-8 shrink-0 rounded-[var(--radius-card-soft)] bg-[var(--bg-base-default)] object-contain p-[var(--spacer-4)] shadow-[var(--shadow-sm)]">
 <label class="ds-btn ds-btn--secondary ds-btn--sm">
  上传图片
  <input type="file" accept="image/png,image/x-icon" class="hidden" :disabled="uploading" @change="onPick('favicon_url', $event)">
 </label>
 </div>
 <input v-model="form.favicon_url" type="text" class="ds-input ds-input--secondary" placeholder="留空则使用浏览器默认，或填写图标地址">
 <p v-if="errors.favicon_url" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.favicon_url }}</p>
 </div>

 <!-- 官网链接 -->
 <div class="rounded-[var(--radius-card-soft)] border border-[var(--border-brand)] bg-[var(--bg-base-secondary)] p-[var(--spacer-12)]">
 <div class="mb-[var(--spacer-4)]">
 <span class="text-body-sm text-text-secondary">官网链接</span>
 </div>
 <input v-model="form.site_url" type="text" class="ds-input ds-input--secondary" placeholder="如：https://www.example.com">
 <p class="mt-[var(--spacer-4)] text-body-xs text-text-tertiary">展示于「关于我们」页底部；留空则隐藏该行。须为 http(s) 绝对地址。</p>
 <p v-if="errors.site_url" class="mt-[var(--spacer-4)] text-body-xs text-[var(--status-error-default)]">{{ errors.site_url }}</p>
 </div>
 </div>

 <div class="mt-[var(--spacer-24)] flex items-center justify-between">
 <span v-if="updated_at" class="ds-pill ds-pill--sm">
 上次更新：{{ updated_at.slice(0, 19).replace('T', ' ') }}
 </span>
 <button type="button" class="ds-btn ds-btn--primary ml-auto" :disabled="saving || loading || hasError" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
 </div>
 </section>
 </main>
</template>
