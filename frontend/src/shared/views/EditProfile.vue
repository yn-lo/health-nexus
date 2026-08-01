<script setup lang="ts">
/**
 * EditProfile 编辑个人资料页
 *
 * 后端契约：PATCH /api/auth/profile { phone, date_of_birth, gender, emergency_contact, emergency_phone }
 * 双端共享：患者端 (/chat/profile/edit) 与医护端 (/staff/profile/edit) 复用
 */
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Phone, Calendar, User, Heart, PhoneCall, CircleAlert, ChevronDown } from '@lucide/vue'
import { useDsToast } from '@/shared/composables/useDsToast'
import { PageShell, AppHeader } from '@/shared/components'
import { useAuthStore } from '@/stores/auth'
import { errmsg } from '@/shared/api/client'

const router = useRouter()
const authStore = useAuthStore()
const { showFailToast, showSuccessToast } = useDsToast()

const GENDER_OPTIONS = [
  { value: '', label: '未设置' },
  { value: 'male', label: '男' },
  { value: 'female', label: '女' },
  { value: 'other', label: '其他' },
] as const

const phone = ref('')
const dateOfBirth = ref('')
const gender = ref('')
const emergencyContact = ref('')
const emergencyPhone = ref('')
const loading = ref(false)
const errorMsg = ref('')

onMounted(() => {
  const u = authStore.user
  if (!u) {
    router.replace({ name: 'login' })
    return
  }
  phone.value = u.phone ?? ''
  dateOfBirth.value = u.date_of_birth ?? ''
  gender.value = u.gender ?? ''
  emergencyContact.value = u.emergency_contact ?? ''
  emergencyPhone.value = u.emergency_phone ?? ''
})

function goBack() {
  router.back()
}

async function handleSubmit() {
  errorMsg.value = ''
  loading.value = true
  try {
    await authStore.updateProfile({
      phone: phone.value,
      date_of_birth: dateOfBirth.value || null,
      gender: gender.value,
      emergency_contact: emergencyContact.value,
      emergency_phone: emergencyPhone.value,
    })
    showSuccessToast('个人资料已更新')
    router.back()
  } catch (e) {
    errorMsg.value = errmsg(e, '保存失败，请稍后重试')
    showFailToast(errorMsg.value)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <PageShell :bottom-nav="false" :padded="false" background="var(--bg-base-secondary)">
    <AppHeader title="编辑资料" @back="goBack" />

    <div class="flex flex-col gap-[var(--spacer-24)] px-[var(--spacer-16)] pt-[var(--spacer-24)] pb-[var(--spacer-32)]">
      <!-- 标题区 -->
      <div class="flex flex-col gap-[var(--spacer-8)]">
        <h1 class="font-heading text-heading-lg font-semibold text-text">编辑个人资料</h1>
        <p class="text-body-base text-text-secondary">完善个人资料，获得更精准的健康宣教内容</p>
      </div>

      <!-- 表单卡片 -->
      <div class="flex flex-col gap-[var(--spacer-20)] rounded-[var(--radius-card-large)] bg-[var(--bg-base-default)] p-[var(--spacer-24)] border border-[var(--border-neutral-l1)] shadow-[var(--shadow-sm)]">
        <form class="flex flex-col gap-[var(--spacer-16)]" @submit.prevent="handleSubmit">
          <!-- 手机号 -->
          <div class="flex flex-col gap-[var(--spacer-8)]">
            <label class="text-body-sm font-medium text-text-secondary">手机号</label>
            <div class="ds-field-wrap ds-field-wrap--secondary">
              <Phone class="h-4 w-4 shrink-0 text-icon-tertiary" />
              <input
                v-model="phone"
                type="tel"
                placeholder="请输入手机号"
                maxlength="20"
                autocomplete="tel"
                aria-label="手机号"
              />
            </div>
          </div>

          <!-- 出生日期 -->
          <div class="flex flex-col gap-[var(--spacer-8)]">
            <label class="text-body-sm font-medium text-text-secondary">出生日期</label>
            <div class="ds-field-wrap ds-field-wrap--secondary">
              <Calendar class="h-4 w-4 shrink-0 text-icon-tertiary" />
              <input
                v-model="dateOfBirth"
                type="date"
                aria-label="出生日期"
              />
            </div>
          </div>

          <!-- 性别 -->
          <div class="flex flex-col gap-[var(--spacer-8)]">
            <label class="text-body-sm font-medium text-text-secondary">性别</label>
            <div class="ds-field-wrap ds-field-wrap--secondary relative">
              <User class="h-4 w-4 shrink-0 text-icon-tertiary" />
              <select
                v-model="gender"
                class="w-full bg-transparent text-body-base text-text appearance-none outline-none"
                aria-label="性别"
              >
                <option
                  v-for="opt in GENDER_OPTIONS"
                  :key="opt.value"
                  :value="opt.value"
                >{{ opt.label }}</option>
              </select>
              <ChevronDown class="pointer-events-none absolute right-3 h-4 w-4 shrink-0 text-icon-tertiary" />
            </div>
          </div>

          <!-- 紧急联系人 -->
          <div class="flex flex-col gap-[var(--spacer-8)]">
            <label class="text-body-sm font-medium text-text-secondary">紧急联系人</label>
            <div class="ds-field-wrap ds-field-wrap--secondary">
              <Heart class="h-4 w-4 shrink-0 text-icon-tertiary" />
              <input
                v-model="emergencyContact"
                type="text"
                placeholder="请输入紧急联系人姓名"
                maxlength="64"
                aria-label="紧急联系人"
              />
            </div>
          </div>

          <!-- 紧急联系电话 -->
          <div class="flex flex-col gap-[var(--spacer-8)]">
            <label class="text-body-sm font-medium text-text-secondary">紧急联系电话</label>
            <div class="ds-field-wrap ds-field-wrap--secondary">
              <PhoneCall class="h-4 w-4 shrink-0 text-icon-tertiary" />
              <input
                v-model="emergencyPhone"
                type="tel"
                placeholder="请输入紧急联系电话"
                maxlength="20"
                autocomplete="tel"
                aria-label="紧急联系电话"
              />
            </div>
          </div>

          <!-- 错误提示 -->
          <div v-if="errorMsg" class="ds-alert ds-alert--error" role="alert">
            <CircleAlert class="icon" />
            <span>{{ errorMsg }}</span>
          </div>

          <button
            type="submit"
            class="ds-btn ds-btn--primary ds-btn--block"
            :class="{ 'ds-btn--loading': loading }"
            :disabled="loading"
          >
            <span v-if="loading" class="ds-btn__spinner" />
            保存
          </button>
        </form>
      </div>
    </div>
  </PageShell>
</template>
