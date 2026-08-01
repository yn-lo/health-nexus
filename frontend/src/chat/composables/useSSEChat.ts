import { onUnmounted, ref, shallowRef } from 'vue'
import { clearTokens, getAccessToken, getDeviceId, tryRefreshToken } from '@/shared/api/client'
import type { Reference, SSEEvent } from '@/shared/types/chat'
import { parseSSELine } from './sseParse'

interface UseSSEChatOptions {
  conversationId: string
  selectedDeptId?: number
}

/**
 * SSE 流式问答 — POST + JSON body，解析后端标准 SSE 帧（event: <type>\ndata: <payload>\n\n）。
 * 后端事件：conversation / token / references / safety_warning / crisis / error / done。
 *
 * token 刷新复用 shared/api/client 的全局刷新锁：SSE 与普通 API 并发 401 时共享同一次
 * refresh 请求，避免两处各自携带同一 refresh token 触发后端轮换竞态（败者被误判会话失效）。
 */

/** 最大重连次数（仅限连接建立前的网络错误） */
const MAX_RETRIES = 2
/** 重连基础延迟（ms），指数退避 */
const RETRY_BASE_DELAY = 1000
/** 流式读取超时（ms）：超过此时间未收到任何数据则判定连接中断 */
const STREAM_IDLE_TIMEOUT = 60_000

/** safety_warning 解析结果 — 裸字符串无 mode，JSON 载荷含 replace/append */
interface SafetyWarningInfo {
  text: string
  mode?: 'replace' | 'append'
}

export function useSSEChat(options: UseSSEChatOptions) {
  const isStreaming = ref(false)
  const currentContent = ref('')
  const references = ref<Reference[]>([])
  const safetyWarning = ref<SafetyWarningInfo | null>(null)
  const crisis = ref<{ answer: string } | null>(null)
  const error = ref<string | null>(null)
  /** 用户主动中止标记 — 用于上层区分"完整回答"与"被截断" */
  const aborted = ref(false)
  const controller = shallowRef<AbortController | null>(null)
  /** 权威会话 ID — 初始值来自 options，后端 conversation 事件下发后更新；后续请求自动携带 */
  const conversationId = ref(options.conversationId)
  /** 重连剩余次数 */
  let retryCount = 0

  async function sendQuestion(question: string) {
    isStreaming.value = true
    currentContent.value = ''
    references.value = []
    safetyWarning.value = null
    crisis.value = null
    error.value = null
    aborted.value = false
    retryCount = MAX_RETRIES

    let idleTimer: ReturnType<typeof setTimeout> | null = null

    const resetIdleTimer = () => {
      if (idleTimer) clearTimeout(idleTimer)
      idleTimer = setTimeout(() => {
        error.value = '连接超时，请重试'
        controller.value?.abort()
      }, STREAM_IDLE_TIMEOUT)
    }

    try {
      while (true) {
        controller.value = new AbortController()
        let receivedAnyEvent = false
        let receivedDone = false

        try {
          const token = getAccessToken()

          const body: Record<string, unknown> = {
            message: question,
            conversation_id: conversationId.value,
          }
          // 科室选择始终携带：0=全部科室，具体 id=锁定科室。
          // 修复：旧逻辑过滤掉 0，导致后端收到 nil（未指定）而把会话误锁到解析科室。
          if (options.selectedDeptId != null) {
            body.selected_dept_id = options.selectedDeptId
          }

          const endpoint = token ? '/api/chat/stream' : '/api/public/chat/stream'
          const headers: Record<string, string> = { 'Content-Type': 'application/json' }
          if (token) {
            headers['Authorization'] = `Bearer ${token}`
          } else {
            headers['X-Device-Id'] = getDeviceId()
          }

          let response = await fetch(endpoint, {
            method: 'POST',
            headers,
            body: JSON.stringify(body),
            signal: controller.value.signal,
          })

          if (response.status === 401 && token) {
            const refreshed = await tryRefreshToken()
            if (refreshed) {
              headers['Authorization'] = `Bearer ${getAccessToken()}`
              response = await fetch(endpoint, {
                method: 'POST',
                headers,
                body: JSON.stringify(body),
                signal: controller.value.signal,
              })
            } else {
              clearTokens()
              // ponytail:allow-location - 401 刷新失败后必须跳转登录页
              window.location.href = '/login'
              return
            }
          }

          if (!response.ok) {
            let msg = `请求失败 (${response.status})`
            try {
              const errBody = await response.json()
              if (typeof errBody?.message === 'string') msg = errBody.message
            } catch { /* 非 JSON 响应，用默认消息 */ }
            error.value = msg
            return
          }

          const reader = response.body?.getReader()
          if (!reader) throw new Error('无法读取响应流')

          resetIdleTimer()

          const decoder = new TextDecoder()
          let buffer = ''
          let currentEvent = 'message'
          let dataLines: string[] = []

          const dispatch = () => {
            const eventName = currentEvent
            currentEvent = 'message'
            if (eventName === 'done') {
              dataLines = []
              receivedAnyEvent = true
              receivedDone = true
              return
            }
            if (dataLines.length === 0) return
            const raw = dataLines.join('\n')
            dataLines = []

            let evt: SSEEvent | null = null
            if (eventName === 'token') evt = { type: 'token', data: raw }
            else if (eventName === 'conversation') {
              try { evt = { type: 'conversation', data: JSON.parse(raw) as { conversation_id: string } } } catch { /* ignore */ }
            } else if (eventName === 'safety_warning') {
              evt = parseSafetyWarning(raw)
            } else if (eventName === 'references') {
              try { evt = { type: 'references', data: JSON.parse(raw) as Reference[] } } catch { /* ignore */ }
            } else if (eventName === 'crisis') {
              try { evt = { type: 'crisis', data: JSON.parse(raw) as { answer: string } } } catch { /* ignore */ }
            } else if (eventName === 'error') {
              try { evt = { type: 'error', data: JSON.parse(raw) as { message: string } } } catch { /* ignore */ }
            }

            if (!evt) return
            receivedAnyEvent = true
            applyEvent(evt)
          }

          const applyEvent = (e: SSEEvent) => {
            switch (e.type) {
              case 'conversation':
                conversationId.value = e.data.conversation_id
                break
              case 'token':
                currentContent.value += e.data
                break
              case 'references':
                references.value = e.data
                break
              case 'safety_warning':
                safetyWarning.value = { text: e.data, mode: e.mode }
                if (e.mode === 'replace') currentContent.value = e.data
                else if (e.mode === 'append') currentContent.value += e.data
                else {
                  currentContent.value += currentContent.value ? '\n\n' + e.data : e.data
                }
                break
              case 'crisis':
                crisis.value = e.data
                break
              case 'error':
                error.value = e.data.message
                break
            }
          }

          while (true) {
            const { done, value } = await reader.read()
            if (done) break

            resetIdleTimer()
            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop() ?? ''

            for (const line of lines) {
              if (line === '' || line === '\r') {
                dispatch()
                continue
              }
              const parsed = parseSSELine(line)
              if (parsed?.event !== undefined) {
                currentEvent = parsed.event
              } else if (parsed?.data !== undefined) {
                dataLines.push(parsed.data)
              }
            }
          }
          buffer += decoder.decode()
          if (buffer.trim()) {
            const residualLines = buffer.split('\n')
            for (const line of residualLines) {
              const parsed = parseSSELine(line)
              if (parsed?.event !== undefined) {
                currentEvent = parsed.event
              } else if (parsed?.data !== undefined) {
                dataLines.push(parsed.data)
              }
            }
            dispatch()
          }

          if (!receivedDone && !error.value && !aborted.value) {
            error.value = '连接中断，回答可能不完整'
          }
          return
        } catch (err) {
          if ((err as Error).name === 'AbortError') {
            if (!aborted.value && error.value) {
              // 空闲超时：error 已设置，不覆盖
            } else {
              aborted.value = true
            }
            return
          }
          if (retryCount > 0 && !aborted.value && !receivedAnyEvent) {
            retryCount--
            if (idleTimer) clearTimeout(idleTimer)
            const delay = RETRY_BASE_DELAY * (MAX_RETRIES - retryCount)
            await new Promise(r => setTimeout(r, delay))
            if (!aborted.value) {
              continue
            }
          }
          if (!aborted.value) {
            error.value = (err as Error).message
          }
          return
        }
      }
    } finally {
      if (idleTimer) clearTimeout(idleTimer)
      isStreaming.value = false
      controller.value = null
    }
  }

  function abort() {
    aborted.value = true
    controller.value?.abort()
  }

  onUnmounted(() => {
    if (controller.value) {
      aborted.value = true
      controller.value.abort()
    }
  })

  return { isStreaming, currentContent, references, safetyWarning, crisis, error, aborted, conversationId, sendQuestion, abort }
}

/**
 * 解析 safety_warning 载荷 — 兼容两种后端格式：
 * 1. 裸字符串（紧急就医提醒 / 拒答话术 / 超时提示）→ { text, mode: undefined }
 * 2. JSON {"mode":"replace"|"append","text":"..."}（输出安全审查）→ { text, mode }
 */
function parseSafetyWarning(raw: string): SSEEvent {
  try {
    const parsed = JSON.parse(raw) as { mode?: string; text?: string }
    if (typeof parsed.text === 'string') {
      const mode = parsed.mode === 'replace' || parsed.mode === 'append' ? parsed.mode : undefined
      return { type: 'safety_warning', data: parsed.text, mode }
    }
  } catch { /* 非 JSON，按裸字符串处理 */ }
  return { type: 'safety_warning', data: raw }
}
