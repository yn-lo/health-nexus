/**
 * useProfileSummary 单元测试
 * 覆盖：无用户空数组、按 phone/gender/date_of_birth 生成摘要行、缺失字段跳过
 * 背景：PersonalCenter 与 StaffProfile 的资料摘要 computed 重复（jscpd clone），提取为共享 composable
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useProfileSummary } from '@/shared/composables/useProfileSummary'
import { useAuthStore } from '@/stores/auth'
import type { TokenUser } from '@/shared/types/auth'

function makeUser(partial: Partial<TokenUser>): TokenUser {
  return {
    id: 1,
    username: '测试用户',
    role: 'staff',
    phone: '',
    date_of_birth: null,
    gender: '',
    emergency_contact: '',
    emergency_phone: '',
    dept_id: 0,
    ...partial,
  }
}

describe('useProfileSummary', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('无用户时返回空数组', () => {
    const { profileSummary } = useProfileSummary()
    expect(profileSummary.value).toEqual([])
  })

  it('按 phone/gender/date_of_birth 生成摘要行（gender 映射中文）', () => {
    const store = useAuthStore()
    store.user = makeUser({ phone: '13800000000', gender: 'male', date_of_birth: '1990-01-01' })

    const { profileSummary } = useProfileSummary()
    const lines = profileSummary.value
    expect(lines.map((l) => l.text)).toEqual(['13800000000', '男', '1990-01-01'])
  })

  it('缺失字段不生成对应行', () => {
    const store = useAuthStore()
    store.user = makeUser({ phone: '13800000000' })

    const { profileSummary } = useProfileSummary()
    expect(profileSummary.value.map((l) => l.text)).toEqual(['13800000000'])
  })

  it('未知 gender 原样展示', () => {
    const store = useAuthStore()
    store.user = makeUser({ gender: 'custom' })

    const { profileSummary } = useProfileSummary()
    expect(profileSummary.value.map((l) => l.text)).toEqual(['custom'])
  })
})
