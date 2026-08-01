/**
 * useCrudEditor 单元测试
 * 覆盖：初始状态、openCreate/openEdit、submit 新建/编辑/校验/失败、remove 确认/取消
 * 背景：SafetyRule / SensitiveWord / Department 配置页的
 *       openCreate/openEdit/submit/remove 重复（jscpd clone），提取为共享 composable
 */
import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { useCrudEditor } from '@/shared/composables/useCrudEditor'
import { useDsToast } from '@/shared/composables/useDsToast'
import { useDsDialog } from '@/shared/composables/useDsDialog'

interface Item { id: number; name: string }
interface Form { name: string }

function makeOptions() {
  const listRef = ref<Item[]>([])
  const create = vi.fn().mockResolvedValue(undefined)
  const update = vi.fn().mockResolvedValue(undefined)
  const removeRun = vi.fn().mockResolvedValue(undefined)
  const onSaved = vi.fn().mockResolvedValue(undefined)
  const options = {
    listRef,
    defaultForm: (): Form => ({ name: '' }),
    toForm: (item: Item): Form => ({ name: item.name }),
    create,
    update,
    remove: {
      message: (item: Item) => `删除「${item.name}」？`,
      run: removeRun,
    },
    onSaved,
  }
  return { options, listRef, create, update, removeRun, onSaved }
}

describe('useCrudEditor', () => {
  it('初始状态：showEditor=false、editing=null、form=defaultForm()', () => {
    const { options } = makeOptions()
    const { showEditor, editing, form } = useCrudEditor<Item, Form>(options)
    expect(showEditor.value).toBe(false)
    expect(editing.value).toBeNull()
    expect(form.value).toEqual({ name: '' })
  })

  it('openCreate() 重置表单并打开弹窗', () => {
    const { options } = makeOptions()
    const { showEditor, editing, form, openCreate } = useCrudEditor<Item, Form>(options)
    editing.value = { id: 1, name: '旧值' }
    form.value = { name: '旧值' }
    openCreate()
    expect(editing.value).toBeNull()
    expect(form.value).toEqual({ name: '' })
    expect(showEditor.value).toBe(true)
  })

  it('openEdit(item) 回填表单并打开弹窗', () => {
    const { options } = makeOptions()
    const { showEditor, editing, form, openEdit } = useCrudEditor<Item, Form>(options)
    openEdit({ id: 1, name: 'foo' })
    expect(editing.value).toEqual({ id: 1, name: 'foo' })
    expect(form.value).toEqual({ name: 'foo' })
    expect(showEditor.value).toBe(true)
  })

  it('submit() 新建：调用 create、弹成功 toast、关闭弹窗并 onSaved', async () => {
    const { toastState } = useDsToast()
    const { options, create, update, onSaved } = makeOptions()
    const { showEditor, form, submit } = useCrudEditor<Item, Form>(options)
    form.value = { name: '新记录' }
    await submit()
    expect(create).toHaveBeenCalledWith({ name: '新记录' })
    expect(update).not.toHaveBeenCalled()
    expect(toastState.value).toMatchObject({ visible: true, type: 'success', message: '已创建' })
    expect(showEditor.value).toBe(false)
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  it('submit() 编辑：调用 update（记录 + 表单）并弹「已更新」', async () => {
    const { toastState } = useDsToast()
    const { options, update } = makeOptions()
    const { openEdit, submit } = useCrudEditor<Item, Form>(options)
    openEdit({ id: 7, name: '旧名' })
    await submit()
    expect(update).toHaveBeenCalledWith({ id: 7, name: '旧名' }, { name: '旧名' })
    expect(toastState.value).toMatchObject({ visible: true, type: 'success', message: '已更新' })
  })

  it('submit() 校验失败：不调用 create/update，弹失败 toast', async () => {
    const { toastState } = useDsToast()
    const { options, create, update } = makeOptions()
    const { submit } = useCrudEditor<Item, Form>({ ...options, validate: (f) => (!f.name ? '名称必填' : null) })
    await submit()
    expect(create).not.toHaveBeenCalled()
    expect(update).not.toHaveBeenCalled()
    expect(toastState.value).toMatchObject({ visible: true, type: 'fail', message: '名称必填' })
  })

  it('submit() 保存失败：catch 后弹「保存失败」', async () => {
    const { toastState } = useDsToast()
    const { options } = makeOptions()
    const { submit } = useCrudEditor<Item, Form>({ ...options, create: vi.fn().mockRejectedValue(new Error('boom')) })
    await submit()
    expect(toastState.value).toMatchObject({ visible: true, type: 'fail', message: 'boom' })
  })

  it('remove() 确认后：调用 run、本地过滤列表、弹「已删除」', async () => {
    const { toastState } = useDsToast()
    const { options, listRef, removeRun } = makeOptions()
    listRef.value = [{ id: 1, name: 'a' }, { id: 2, name: 'b' }]
    const { remove } = useCrudEditor<Item, Form>(options)
    const p = remove({ id: 1, name: 'a' })
    const { confirm } = useDsDialog()
    confirm()
    await p
    expect(removeRun).toHaveBeenCalledWith(1)
    expect(listRef.value).toEqual([{ id: 2, name: 'b' }])
    expect(toastState.value).toMatchObject({ visible: true, type: 'success', message: '已删除' })
  })

  it('remove() 取消确认：不调用 run', async () => {
    const { options, removeRun } = makeOptions()
    const { remove } = useCrudEditor<Item, Form>(options)
    const p = remove({ id: 1, name: 'a' })
    const { cancel } = useDsDialog()
    cancel()
    await p
    expect(removeRun).not.toHaveBeenCalled()
  })

  it('未配置 remove：remove() 直接返回，不弹确认框', async () => {
    const { options, removeRun } = makeOptions()
    const { remove } = useCrudEditor<Item, Form>({ ...options, remove: undefined })
    await remove({ id: 1, name: 'a' })
    expect(removeRun).not.toHaveBeenCalled()
  })
})
