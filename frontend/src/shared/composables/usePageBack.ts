import { useRouter } from 'vue-router'

/**
 * usePageBack — 返回上一页（有浏览历史则 back，否则回首页）
 * PrivacyPolicy / TermsOfService 的 goBack 重复（jscpd clone），提取为共享 composable
 */
export function usePageBack() {
  const router = useRouter()

  function goBack() {
    if (window.history.length > 1) {
      router.back()
    } else {
      router.push('/')
    }
  }

  return { goBack }
}
