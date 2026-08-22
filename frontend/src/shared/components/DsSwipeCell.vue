<script setup lang="ts">
/**
 * DsSwipeCell 自研滑动删除单元格
 * 左滑露出右侧操作区，松手后自动吸附（超过阈值展开，否则回弹）
 * 触摸目标由操作区内容决定；点击内容区时收起
 */
import { ref } from 'vue'

const rightRef = ref<HTMLElement | null>(null)

/** 当前滑动偏移（负数表示左滑露出右侧操作区） */
const offsetX = ref(0)
/** 处于展开状态（操作区完全露出） */
const isOpen = ref(false)
/** 拖动中（禁用过渡，保证跟手） */
const isDragging = ref(false)

let startX = 0
let startY = 0
let startOffset = 0
let isHorizontal = false

function getActionWidth(): number {
  return rightRef.value?.offsetWidth ?? 0
}

function onTouchStart(e: TouchEvent) {
  const touch = e.touches[0]
  startX = touch.clientX
  startY = touch.clientY
  startOffset = isOpen.value ? -getActionWidth() : 0
  isHorizontal = false
  isDragging.value = true
}

function onTouchMove(e: TouchEvent) {
  if (!isDragging.value) return
  const touch = e.touches[0]
  const dx = touch.clientX - startX
  const dy = touch.clientY - startY
  // 横向意图判定：一旦判定为横向滑动则锁定，阻止纵向滚动
  if (!isHorizontal) {
    if (Math.abs(dx) < 6 && Math.abs(dy) < 6) return
    isHorizontal = Math.abs(dx) > Math.abs(dy)
    if (!isHorizontal) {
      isDragging.value = false
      return
    }
  }
  e.preventDefault()
  const actionWidth = getActionWidth()
  // 从当前起始偏移计算，限制在 [-actionWidth, 0] 之间
  offsetX.value = Math.max(-actionWidth, Math.min(0, startOffset + dx))
}

function onTouchEnd() {
  if (!isDragging.value) return
  isDragging.value = false
  const actionWidth = getActionWidth()
  // 超过一半阈值则展开，否则回弹
  isOpen.value = offsetX.value < -actionWidth / 2
  offsetX.value = isOpen.value ? -actionWidth : 0
}

function onContentClick() {
  if (isOpen.value) {
    isOpen.value = false
    offsetX.value = 0
  }
}
</script>

<template>
  <div class="ds-swipe-cell" @touchstart="onTouchStart" @touchmove.stop="onTouchMove" @touchend="onTouchEnd" @touchcancel="onTouchEnd">
    <div
      class="ds-swipe-cell__track"
      :class="{ 'ds-swipe-cell__track--dragging': isDragging }"
      :style="{ transform: `translateX(${offsetX}px)` }"
    >
      <div class="ds-swipe-cell__content" @click="onContentClick">
        <slot />
      </div>
      <div ref="rightRef" class="ds-swipe-cell__right">
        <slot name="right" />
      </div>
    </div>
  </div>
</template>