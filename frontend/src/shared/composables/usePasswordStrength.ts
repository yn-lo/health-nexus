import { computed } from 'vue'

type StrengthLevel = 'weak' | 'medium' | 'strong'

export function usePasswordStrength(password: () => string) {
  const score = computed(() => {
    const pwd = password()
    if (!pwd) return 0
    let s = 0
    if (pwd.length >= 8) s++
    if (/[A-Z]/.test(pwd)) s++
    if (/[a-z]/.test(pwd)) s++
    if (/\d/.test(pwd)) s++
    if (/[^A-Za-z0-9]/.test(pwd)) s++
    return s
  })

  const level = computed<StrengthLevel>(() => {
    if (score.value <= 1) return 'weak'
    if (score.value <= 3) return 'medium'
    return 'strong'
  })

  const label = computed(() => {
    const labels: Record<StrengthLevel, string> = { weak: '弱', medium: '中', strong: '强' }
    return labels[level.value]
  })

  return { level, label }
}
