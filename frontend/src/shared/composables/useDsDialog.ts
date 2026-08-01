import { ref } from 'vue'

interface DialogOptions {
  title?: string
  message: string
  confirmButtonText?: string
  cancelButtonText?: string
  danger?: boolean
}

interface DialogState {
  visible: boolean
  title: string
  message: string
  confirmButtonText: string
  cancelButtonText: string
  isDanger: boolean
  resolve: ((value: void) => void) | null
  reject: ((reason?: 'cancel') => void) | null
}

const state = ref<DialogState>({
  visible: false,
  title: '',
  message: '',
  confirmButtonText: '确认',
  cancelButtonText: '取消',
  isDanger: false,
  resolve: null,
  reject: null,
})

function showConfirmDialog(options: DialogOptions): Promise<void> {
  return new Promise((resolve, reject) => {
    state.value = {
      visible: true,
      title: options.title ?? '',
      message: options.message,
      confirmButtonText: options.confirmButtonText ?? '确认',
      cancelButtonText: options.cancelButtonText ?? '取消',
      isDanger: options.danger ?? false,
      resolve,
      reject,
    }
  })
}

function confirm() {
  state.value.visible = false
  state.value.resolve?.()
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
    showConfirmDialog,
    confirm,
    cancel,
  }
}
