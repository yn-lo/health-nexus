import { ref } from 'vue'

interface DialogOptions {
  title?: string
  message: string
  confirmButtonText?: string
  cancelButtonText?: string
  danger?: boolean
  /** 输入框模式（驳回原因等场景） */
  showInput?: boolean
  inputPlaceholder?: string
  /** 返回 true 表示通过；返回字符串表示校验失败提示 */
  inputValidator?: (value: string) => string | true
}

interface DialogState {
  visible: boolean
  title: string
  message: string
  confirmButtonText: string
  cancelButtonText: string
  isDanger: boolean
  /** 是否显示取消按钮（showDialog 单按钮时为 false） */
  showCancel: boolean
  showInput: boolean
  inputPlaceholder: string
  inputValue: string
  inputError: string
  resolve: ((value: unknown) => void) | null
  reject: ((reason?: 'cancel') => void) | null
}

const state = ref<DialogState>({
  visible: false,
  title: '',
  message: '',
  confirmButtonText: '确认',
  cancelButtonText: '取消',
  isDanger: false,
  showCancel: true,
  showInput: false,
  inputPlaceholder: '',
  inputValue: '',
  inputError: '',
  resolve: null,
  reject: null,
})

function open(options: DialogOptions, showCancel: boolean): Promise<unknown> {
  return new Promise((resolve, reject) => {
    state.value = {
      visible: true,
      title: options.title ?? '',
      message: options.message,
      confirmButtonText: options.confirmButtonText ?? '确认',
      cancelButtonText: options.cancelButtonText ?? '取消',
      isDanger: options.danger ?? false,
      showCancel,
      showInput: options.showInput ?? false,
      inputPlaceholder: options.inputPlaceholder ?? '',
      inputValue: '',
      inputError: '',
      resolve,
      reject,
    }
  })
}

function confirm() {
  // 输入框模式先校验，未通过不关闭
  if (state.value.showInput && state.value.inputValidator) {
    const result = state.value.inputValidator(state.value.inputValue)
    if (result !== true) {
      state.value.inputError = result
      return
    }
  }
  const value = state.value.showInput ? { value: state.value.inputValue } : undefined
  state.value.visible = false
  state.value.resolve?.(value)
  state.value.resolve = null
  state.value.reject = null
}

function cancel() {
  state.value.visible = false
  state.value.reject?.('cancel')
  state.value.resolve = null
  state.value.reject = null
}

export function useDsDialog() {
  return {
    dialogState: state,
    /** 单按钮确认框（无取消），确认时 resolve；无输入框时 resolve undefined */
    showDialog: (options: DialogOptions) => open(options, false) as Promise<{ value?: string } | undefined>,
    /** 确认/取消双按钮框，取消时 reject('cancel') */
    showConfirmDialog: (options: DialogOptions) => open(options, true) as Promise<void>,
    confirm,
    cancel,
  }
}