/**
 * useCrudEditor — 配置 CRUD 页「编辑弹窗」状态机
 * SafetyRuleConfig / SensitiveWordConfig / DepartmentConfig 的
 * openCreate/openEdit/submit/remove + showEditor/editing/form 重复（jscpd clone），提取为共享 composable
 */
import { ref, type Ref } from 'vue'
import { useDsToast } from './useDsToast'
import { useDsDialog } from './useDsDialog'
import { errmsg } from '@/shared/api/client'

interface CrudRemoveOptions<T extends { id: number }> {
  /** 确认弹窗标题（缺省为「确认删除」） */
  title?: string
  /** 确认弹窗正文（带记录名） */
  message: (item: T) => string
  /** 删除 API 调用 */
  run: (id: number) => Promise<unknown>
}

interface UseCrudEditorOptions<T extends { id: number }, TForm> {
  /** 新建默认表单 */
  defaultForm: () => TForm
  /** 编辑回填：记录 → 表单 */
  toForm: (item: T) => TForm
  /** 表单校验；返回错误消息（null 表示通过） */
  validate?: (form: TForm) => string | null
  /** 新建 API */
  create: (form: TForm) => Promise<unknown>
  /** 更新 API（缺省则提交一律走 create） */
  update?: (item: T, form: TForm) => Promise<unknown>
  /** 删除（缺省则不渲染删除） */
  remove?: CrudRemoveOptions<T>
  /** 列表引用，删除成功后本地过滤 */
  listRef: Ref<T[]>
  /** 保存成功后刷新列表 */
  onSaved?: () => Promise<void>
  /** 成功/失败文案 */
  createdText?: string
  updatedText?: string
  removedText?: string
  saveErrorText?: string
}

export function useCrudEditor<T extends { id: number }, TForm>(options: UseCrudEditorOptions<T, TForm>) {
  const { showSuccessToast, showFailToast } = useDsToast()
  const { showConfirmDialog } = useDsDialog()

  const showEditor = ref(false)
  const editing = ref<T | null>(null)
  const form = ref<TForm>(options.defaultForm())

  function openCreate() {
    editing.value = null
    form.value = options.defaultForm()
    showEditor.value = true
  }

  function openEdit(item: T) {
    editing.value = item
    form.value = options.toForm(item)
    showEditor.value = true
  }

  async function submit() {
    const error = options.validate?.(form.value)
    if (error) {
      showFailToast(error)
      return
    }
    try {
      if (editing.value && options.update) {
        await options.update(editing.value, form.value)
        showSuccessToast(options.updatedText ?? '已更新')
      } else {
        await options.create(form.value)
        showSuccessToast(options.createdText ?? '已创建')
      }
      showEditor.value = false
      await options.onSaved?.()
    } catch (e) {
      showFailToast(errmsg(e, options.saveErrorText ?? '保存失败'))
    }
  }

  async function remove(item: T) {
    if (!options.remove) return
    try {
      await showConfirmDialog({
        title: options.remove.title ?? '确认删除',
        message: options.remove.message(item),
        confirmButtonText: '删除',
        danger: true,
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
    try {
      await options.remove.run(item.id)
      options.listRef.value = options.listRef.value.filter((x) => x.id !== item.id)
      showSuccessToast(options.removedText ?? '已删除')
    } catch (e) {
      showFailToast(errmsg(e, '删除失败'))
    }
  }

  return { showEditor, editing, form, openCreate, openEdit, submit, remove }
}
