import type { Ref } from 'vue'

/** 点击引用角标：滚动到同一消息内对应的参考来源卡片并高亮一闪 */
export function useCiteScroll(root: Ref<HTMLElement | null | undefined>) {
  function scrollToCite(articleId: number) {
    const card = root.value?.querySelector(`[data-article-id="${articleId}"]`) as HTMLElement | null
    if (!card) return
    card.scrollIntoView({ behavior: 'smooth', block: 'center' })
    card.classList.add('ref-card--flash')
    window.setTimeout(() => card.classList.remove('ref-card--flash'), 1200)
  }

  return { scrollToCite }
}
