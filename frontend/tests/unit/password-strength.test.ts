/**
 * 密码强度 composable 测试
 */
import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { usePasswordStrength } from '@/shared/composables/usePasswordStrength'

describe('usePasswordStrength', () => {
  it('空密码返回 weak', () => {
    const password = ref('')
    const { level, label } = usePasswordStrength(() => password.value)
    expect(level.value).toBe('weak')
    expect(label.value).toBe('弱')
  })

  it('简单密码返回 weak', () => {
    const password = ref('abc')
    const { level } = usePasswordStrength(() => password.value)
    expect(level.value).toBe('weak')
  })

  it('中等密码返回 medium', () => {
    const password = ref('abc12345')
    const { level } = usePasswordStrength(() => password.value)
    expect(level.value).toBe('medium')
  })

  it('强密码返回 strong', () => {
    const password = ref('Abc12345!@#$')
    const { level, label } = usePasswordStrength(() => password.value)
    expect(level.value).toBe('strong')
    expect(label.value).toBe('强')
  })

  it('响应式更新', () => {
    const password = ref('')
    const { level } = usePasswordStrength(() => password.value)
    expect(level.value).toBe('weak')

    password.value = 'StrongPass123!'
    expect(level.value).toBe('strong')
  })
})
