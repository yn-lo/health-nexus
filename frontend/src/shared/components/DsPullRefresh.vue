<script setup lang="ts">
/**
 * DsPullRefresh 自研下拉刷新
 * 下拉超出阈值触发 refresh；loading 期间保持展开，父组件完成置回 false 后回弹
 */
import { computed, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  /** 刷新中状态（父组件控制） */
  loading: boolean
}>(), {
  loading: false,
})

const emit = defineEmits<{
  refresh: []
}>()

/** 下拉距离（px），正数表示下拉 */
const pullDistance = ref(0)
/** 拖动中（禁用过渡，保证跟手） */
const isDragging = ref(false)
let startY = 0
let startX = 0
let isVertical = false

const THRESHOLD = 64

const statusText = computed(() => {
  if (props.loading) return '刷新中...'
  if (pullDistance.value >= THRESHOLD) return '释放刷新'
  return '下拉刷新'
})

/**
 * 所有可滚动祖先（含文档）都在顶部时才允许下拉。
 * 注意：仅 overflow-y:auto 不足以判定可滚动——内容不溢出时 scrollTop 恒为 0，
 * 那样的“假滚动容器”（如内容刚好撑满的 .knowledge-list）会让判定永远成立，
 * 从而在页面中部也拦掉原生滚动。故必须同时校验 scrollHeight > clientHeight。
 */
function isAtScrollTop(el: HTMLElement | null): boolean {
  let p = el?.parentElement ?? null
  while (p) {
    const oy = getComputedStyle(p).overflowY
    if ((oy === 'auto' || oy === 'scroll') && p.scrollHeight > p.clientHeight && p.scrollTop > 0) {
      return false
    }
    p = p.parentElement
  }
  const se = document.scrollingElement
  return !se || se.scrollTop <= 0
}

function onTouchStart(e: TouchEvent) {
  if (props.loading) return
  // 仅在滚动容器（含文档）都在顶部时启用下拉，否则与原生滚动叠加冲突
  if (!isAtScrollTop(e.currentTarget as HTMLElement)) return
  const touch = e.touches[0]
  startY = touch.clientY
  startX = touch.clientX
  isVertical = false
  isDragging.value = true
}

function onTouchMove(e: TouchEvent) {
  if (!isDragging.value || props.loading) return
  const touch = e.touches[0]
  const dy = touch.clientY - startY
  const dx = touch.clientX - startX
  if (!isVertical) {
    if (Math.abs(dy) < 8 && Math.abs(dx) < 8) return
    isVertical = Math.abs(dy) > Math.abs(dx)
    if (!isVertical) {
      isDragging.value = false
      return
    }
  }
  if (dy > 0) {
    // 阻尼：下拉距离越大阻力越强
    if (e.cancelable) e.preventDefault()
    pullDistance.value = Math.min(96, dy * 0.5)
  }
}

function onTouchEnd() {
  if (!isDragging.value) return
  isDragging.value = false
  if (pullDistance.value >= THRESHOLD) {
    pullDistance.value = 48 // 保持展开等待刷新完成
    emit('refresh')
  } else {
    pullDistance.value = 0
  }
}

// 刷新完成（loading → false）后回弹归零
watch(() => props.loading, (loading) => {
  if (!loading) pullDistance.value = 0
})
</script>

<template>
  <div class="ds-pull-refresh">
    <div
      class="ds-pull-refresh__head"
      :style="{ height: `${props.loading ? 48 : pullDistance}px` }"
    >
      <span class="ds-pull-refresh__status">
        <span v-if="props.loading" class="ds-loading__spinner ds-loading__spinner--sm" />
        {{ statusText }}
      </span>
    </div>
    <div
      class="ds-pull-refresh__scroller"
      :class="{ 'ds-pull-refresh__scroller--dragging': isDragging }"
      :style="{ transform: `translateY(${props.loading ? 48 : pullDistance}px)` }"
      @touchstart="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @touchcancel="onTouchEnd"
    >
      <slot />
    </div>
  </div>
</template>