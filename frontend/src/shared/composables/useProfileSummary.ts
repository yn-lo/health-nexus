/**
 * useProfileSummary — 资料摘要行生成
 * PersonalCenter 与 StaffProfile 的 profileSummary computed 重复（jscpd clone），提取为共享 composable
 */
import { computed } from 'vue'
import { Phone, User, Heart } from '@lucide/vue'
import { useAuthStore } from '@/stores/auth'

const GENDER_LABEL: Record<string, string> = { male: '男', female: '女', other: '其他' }

/** 摘要行：图标 + 文本 */
interface ProfileSummaryLine {
  icon: typeof Phone
  text: string
}

/** 按 phone / gender / date_of_birth 生成资料摘要行，缺失字段跳过 */
export function useProfileSummary() {
  const authStore = useAuthStore()

  const profileSummary = computed<ProfileSummaryLine[]>(() => {
    const u = authStore.user
    if (!u) return []
    const lines: ProfileSummaryLine[] = []
    if (u.phone) lines.push({ icon: Phone, text: u.phone })
    if (u.gender) lines.push({ icon: User, text: GENDER_LABEL[u.gender] ?? u.gender })
    if (u.date_of_birth) lines.push({ icon: Heart, text: u.date_of_birth })
    return lines
  })

  return { profileSummary }
}
