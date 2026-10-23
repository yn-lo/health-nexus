import { ref, type Ref } from 'vue'
import { useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import Image from '@tiptap/extension-image'
import TextAlign from '@tiptap/extension-text-align'
import { wikiApi } from '@/shared'
import { errmsg } from '@/shared/api/client'
import { useDsToast } from '@/shared/composables'

// Image.extend：块级图片默认不吃 text-align（不在段落内），补一个 textAlign 属性，
// 渲染为 display:block + margin auto 实现居中/右对齐；患者端消毒器已放行这些声明。
const AlignedImage = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      textAlign: {
        default: null as string | null,
        parseHTML: (el: HTMLElement) => {
          const s = el.style
          if (s.display === 'block' && s.marginLeft === 'auto' && s.marginRight === 'auto') return 'center'
          if (s.display === 'block' && s.marginLeft === 'auto' && s.marginRight === '0px') return 'right'
          return null
        },
        renderHTML: (attrs: { textAlign?: string | null }) => {
          if (attrs.textAlign === 'center') {
            return { style: 'display:block;margin-left:auto;margin-right:auto' }
          }
          if (attrs.textAlign === 'right') {
            return { style: 'display:block;margin-left:auto;margin-right:0' }
          }
          return {}
        },
      },
    }
  },
  // 官方 ResizableNodeView 的 update() 只保留 DOM、不同步节点属性（拖拽时是内部直接改样式），
  // 外部 updateAttributes 改对齐后编辑器画面不会更新。包一层：属性变更时同步容器
  // justify-content（官方容器为 flex 布局，对齐落在这里才生效）。
  addNodeView() {
    const parentFactory = this.parent?.()
    if (!parentFactory) return null
    return ((props: Parameters<typeof parentFactory>[0]) => {
      type ResizableLike = {
        container?: HTMLElement | null
        update?: (node: unknown, decorations: unknown, innerDecorations: unknown) => boolean
      }
      const nv = parentFactory(props) as ResizableLike
      const syncAlign = (node: unknown) => {
        if (!nv.container) return
        const align = (node as { attrs?: { textAlign?: string | null } }).attrs?.textAlign
        nv.container.style.justifyContent =
          align === 'center' ? 'center' : align === 'right' ? 'flex-end' : 'flex-start'
      }
      if (typeof nv.update !== 'function') return nv
      syncAlign(props.node)
      const originalUpdate = nv.update.bind(nv)
      nv.update = (node: unknown, decorations: unknown, innerDecorations: unknown) => {
        const ok = originalUpdate(node, decorations, innerDecorations)
        if (ok) syncAlign(node)
        return ok
      }
      return nv
    }) as typeof parentFactory
  },
})

/** 允许的图片类型与大小上限（与后端 upload.max_size_mb 对齐，前端先拦一道减少无效请求） */
const MAX_IMAGE_MB = 5
const ALLOWED_IMAGE_TYPES = ['image/jpeg', 'image/png', 'image/webp', 'image/gif']

/** TipTap 编辑器 + 图片上传（正文只存 URL，图片本身不参与向量化） */
export function useArticleEditor(fileInput: Ref<HTMLInputElement | null>) {
  const { showFailToast } = useDsToast()
  const uploading = ref(false)

  const content = ref('')
  const editor = useEditor({
    // StarterKit v3 已内置 Underline/Strike/Blockquote/HorizontalRule/OrderedList
    extensions: [
      StarterKit,
      AlignedImage.configure({
        // 官方 resize：选中图片出现拖拽手柄，宽度以 width/height 属性写进正文
        resize: { enabled: true, alwaysPreserveAspectRatio: true },
      }),
      TextAlign.configure({ types: ['heading', 'paragraph'] }),
    ],
    content: '',
    onUpdate: ({ editor: e }) => {
      content.value = e.getHTML()
    },
  })

  function pickImage() {
    fileInput.value?.click()
  }

  async function onImageSelected(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = '' // 清空以便重复选择同一文件
    if (!file || uploading.value) return
    if (!ALLOWED_IMAGE_TYPES.includes(file.type)) {
      showFailToast('仅支持 JPG/PNG/WebP/GIF 格式图片')
      return
    }
    if (file.size > MAX_IMAGE_MB * 1024 * 1024) {
      showFailToast(`图片不能超过 ${MAX_IMAGE_MB}MB`)
      return
    }
    uploading.value = true
    try {
      const { url } = await wikiApi.uploadArticleImage(file)
      editor.value?.chain().focus().setImage({ src: url, alt: file.name }).run()
    } catch (err) {
      showFailToast(errmsg(err, '图片上传失败'))
    } finally {
      uploading.value = false
    }
  }

  return { content, editor, uploading, pickImage, onImageSelected }
}
