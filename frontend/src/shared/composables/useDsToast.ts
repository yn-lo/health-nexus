import { ref } from 'vue'

type ToastType = 'success' | 'fail'

interface ToastState {
  visible: boolean
  type: ToastType
  message: string
}

const state = ref<ToastState>({ visible: false, type: 'success', message: '' })
let timer: ReturnType<typeof setTimeout> | null = null

function show(type: ToastType, message: string, duration = 1500) {
  if (timer) clearTimeout(timer)
  state.value = { visible: true, type, message }
  timer = setTimeout(() => {
    state.value.visible = false
    timer = null
  }, duration)
}

export function useDsToast() {
  return {
    toastState: state,
    showSuccessToast: (message: string) => show('success', message),
    showFailToast: (message: string) => show('fail', message),
  }
}
