<script setup lang="ts">
import { computed, type Component } from 'vue'
import type { Editor } from '@tiptap/vue-3'
import {
  Bold,
  Italic,
  Underline,
  Strikethrough,
  Heading,
  Quote,
  Minus,
  List,
  ListOrdered,
  AlignLeft,
  AlignCenter,
  AlignRight,
  ImagePlus,
} from '@lucide/vue'

const props = defineProps<{
  editor?: Editor
  uploading: boolean
}>()

const emit = defineEmits<{ pickImage: [] }>()

/** 工具栏按钮配置 */
interface ToolbarButton {
  icon: Component
  label: string
  active: boolean
  action: () => void
}

type AlignValue = 'left' | 'center' | 'right'

/** 对齐动作：选中图片时改图片自身对齐（块级图片不吃段落 text-align），否则改段落/标题 */
function applyAlign(align: AlignValue) {
  const ed = props.editor
  if (!ed) return
  if (ed.isActive('image')) {
    ed.chain().focus().updateAttributes('image', { textAlign: align }).run()
  } else {
    ed.chain().focus().setTextAlign(align).run()
  }
}

/** 对齐激活态：图片节点读自身 textAlign，文本读段落 text-align */
function isAlignActive(align: AlignValue): boolean {
  const ed = props.editor
  if (!ed) return false
  if (ed.isActive('image')) return ed.getAttributes('image').textAlign === align
  return ed.isActive({ textAlign: align })
}

const toolbarButtons = computed<ToolbarButton[]>(() => [
  {
    icon: Bold,
    label: '加粗',
    active: props.editor?.isActive('bold') ?? false,
    action: () => props.editor?.chain().focus().toggleBold().run(),
  },
  {
    icon: Italic,
    label: '斜体',
    active: props.editor?.isActive('italic') ?? false,
    action: () => props.editor?.chain().focus().toggleItalic().run(),
  },
  {
    icon: Underline,
    label: '下划线',
    active: props.editor?.isActive('underline') ?? false,
    action: () => props.editor?.chain().focus().toggleUnderline().run(),
  },
  {
    icon: Strikethrough,
    label: '删除线',
    active: props.editor?.isActive('strike') ?? false,
    action: () => props.editor?.chain().focus().toggleStrike().run(),
  },
  {
    icon: Heading,
    label: '标题',
    active: props.editor?.isActive('heading', { level: 2 }) ?? false,
    action: () => props.editor?.chain().focus().toggleHeading({ level: 2 }).run(),
  },
  {
    icon: Quote,
    label: '引用',
    active: props.editor?.isActive('blockquote') ?? false,
    action: () => props.editor?.chain().focus().toggleBlockquote().run(),
  },
  {
    icon: List,
    label: '无序列表',
    active: props.editor?.isActive('bulletList') ?? false,
    action: () => props.editor?.chain().focus().toggleBulletList().run(),
  },
  {
    icon: ListOrdered,
    label: '有序列表',
    active: props.editor?.isActive('orderedList') ?? false,
    action: () => props.editor?.chain().focus().toggleOrderedList().run(),
  },
  {
    icon: Minus,
    label: '分割线',
    active: false,
    action: () => props.editor?.chain().focus().setHorizontalRule().run(),
  },
  {
    icon: AlignLeft,
    label: '左对齐',
    active: isAlignActive('left'),
    action: () => applyAlign('left'),
  },
  {
    icon: AlignCenter,
    label: '居中',
    active: isAlignActive('center'),
    action: () => applyAlign('center'),
  },
  {
    icon: AlignRight,
    label: '右对齐',
    active: isAlignActive('right'),
    action: () => applyAlign('right'),
  },
  {
    icon: ImagePlus,
    label: '插入图片',
    active: props.uploading,
    action: () => emit('pickImage'),
  },
])
</script>

<template>
  <!-- 移动端横向滑动（13 按钮在窄屏放不下，禁止换行/挤压，配 shrink-0） -->
  <div
    class="flex flex-nowrap items-center gap-[var(--spacer-4)] overflow-x-auto border-b border-[var(--border-neutral-l1)] bg-[var(--bg-base-secondary)] px-[var(--spacer-8)] py-[var(--spacer-8)]"
  >
    <button
      v-for="btn in toolbarButtons"
      :key="btn.label"
      type="button"
      class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-4)] border-none transition-colors"
      :class="
        btn.active
          ? 'bg-[var(--bg-overlay-l2)] text-text'
          : 'bg-transparent text-icon hover:bg-[var(--bg-overlay-l1)]'
      "
      :aria-label="btn.label"
      @click="btn.action"
    >
      <component :is="btn.icon" class="h-4 w-4" />
    </button>
  </div>
</template>
