import { onUnmounted, ref, shallowRef } from 'vue'
import { clearTokens, getAccessToken, getDeviceId, tryRefreshToken } from '@/shared/api/client'
import { type NoticeKind, type Reference, type SSEEvent, type TurnResult } from '@/shared/types/chat'
import { parseSSELine } from './sseParse'

interface UseSSEChatOptions {
  conversationId: string
  selectedDeptId?: number
}

/**
 * SSE 流式问答 — POST + JSON body，解析后端标准 SSE 帧（event: <type>\ndata: <payload>\n\n）。
 * 后端事件：conversation / token / references / answer_replaced / notice / crisis / result / error / done。
 *
 * 语义约定：
 *   - token + answer_replaced 构成**答案正文**（与持久化内容一致）；
 *   - notice 是面向用户的独立提示（紧急就医 / 超时），不混入正文；
 *   - result 是本轮权威结果（真实消息 ID / 最终 result_code / 最终引用），
 *     前端据此替换本地乐观消息，无需按内容猜测或整页回拉。
 *
 * token 刷新复用 shared/api/client 的全局刷新锁：SSE 与普通 API 并发 401 时共享同一次
 * refresh 请求，避免两处各自携带同一 refresh token 触发后端轮换竞态（败者被误判会话失效）。
 */

/** 最大重连次数（仅限连接建立前的网络错误） */
const MAX_RETRIES = 2
/** 重连基础延迟（ms），指数退避 */
const RETRY_BASE_DELAY = 1000
/**
 * 空闲超时（ms）：超过此时间未收到任何数据（含心跳 ping）则判定连接中断。
 * 后端每 15s 下发一次 ping，故 60s 足以容忍若干次丢包/抖动。
 */
const STREAM_IDLE_TIMEOUT = 60_000
/**
 * 总超时（ms）：覆盖"生成 + 语义审核"的整轮上限。
 * 后端高风险答案先生成、经语义审核后才下发，最长约 4 分钟（llmStreamTimeout），
 * 故总超时须显著大于该值——空闲超时无法覆盖此阶段（心跳会持续刷新空闲计时）。
 */
const STREAM_TOTAL_TIMEOUT = 5 * 60_000

/** 独立提示（notice 事件） */
interface ChatNotice {
  kind: NoticeKind
  text: string
}

export function useSSEChat(options: UseSSEChatOptions) {
  const isStreaming = ref(false)
  const currentContent = ref('')
  const references = ref<Reference[]>([])
  /** 独立提示（紧急就医 / 超时），不进入答案正文 */
  const notices = ref<ChatNotice[]>([])
  const crisis = ref<{ answer: string } | null>(null)
  const error = ref<string | null>(null)
  /** 本轮权威结果（result 事件）：真实消息 ID / 最终 result_code / 最终引用 */
  const result = ref<TurnResult | null>(null)
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
    notices.value = []
    crisis.value = null
    result.value = null
    error.value = null
    aborted.value = false
    retryCount = MAX_RETRIES
    // 幂等标识：一次发送的多次自动重试复用同一值，服务端据此回放而非重复生成。
    const requestId = crypto.randomUUID()

    let idleTimer: ReturnType<typeof setTimeout> | null = null
    let totalTimer: ReturnType<typeof setTimeout> | null = null

    const resetIdleTimer = () => {
      if (idleTimer) clearTimeout(idleTimer)
      idleTimer = setTimeout(() => {
        error.value = '连接超时，请重试'
        controller.value?.abort()
      }, STREAM_IDLE_TIMEOUT)
    }

    // 总超时覆盖"生成 + 审核"整轮：心跳只刷新空闲计时，无法覆盖这一阶段。
    const startTotalTimer = () => {
      if (totalTimer) clearTimeout(totalTimer)
      totalTimer = setTimeout(() => {
        error.value = '回答生成超时，请重试'
        controller.value?.abort()
      }, STREAM_TOTAL_TIMEOUT)
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
            request_id: requestId,
          }
          // 科室仅在开启新会话时携带（0=全部科室，具体 id=锁定科室）。
          // 已有会话的科室由服务端按会话锁定值决定：继续携带前端的默认值 0 会被后端判为
          // "会话中切换知识库"（CHAT_DEPT_LOCKED 409），导致历史会话无法续聊。
          if (!conversationId.value && options.selectedDeptId != null) {
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
          startTotalTimer()

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
            } else if (eventName === 'answer_replaced') {
              try { evt = { type: 'answer_replaced', data: JSON.parse(raw) as { mode: 'replace' | 'append'; text: string } } } catch { /* ignore */ }
            } else if (eventName === 'notice') {
              try { evt = { type: 'notice', data: JSON.parse(raw) as ChatNotice } } catch { /* ignore */ }
            } else if (eventName === 'references') {
              try { evt = { type: 'references', data: JSON.parse(raw) as Reference[] } } catch { /* ignore */ }
            } else if (eventName === 'crisis') {
              try { evt = { type: 'crisis', data: JSON.parse(raw) as { answer: string } } } catch { /* ignore */ }
            } else if (eventName === 'result') {
              try { evt = { type: 'result', data: JSON.parse(raw) as TurnResult } } catch { /* ignore */ }
            } else if (eventName === 'error') {
              try { evt = { type: 'error', data: JSON.parse(raw) as { message: string } } } catch { /* ignore */ }
            } else if (eventName === 'ping') {
              evt = { type: 'ping', data: raw }
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
              case 'answer_replaced':
                // 正文修正：与持久化内容保持一致
                if (e.data.mode === 'replace') currentContent.value = e.data.text
                else currentContent.value += e.data.text
                break
              case 'notice':
                // 独立提示：不进入答案正文
                notices.value = [...notices.value, e.data]
                break
              case 'crisis':
                crisis.value = e.data
                break
              case 'result':
                result.value = e.data
                // result 是本轮权威结果：引用以它为准（为空也要覆盖），
                // 否则"先推 references、后生成失败"的回合会残留引用卡片（与落库 refs=0 不一致）。
                references.value = e.data.references ?? []
                break
              case 'error':
                error.value = e.data.message
                break
              case 'ping':
                // 心跳：仅用于刷新空闲计时（在读取循环中已完成），不改动任何展示状态。
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
      if (totalTimer) clearTimeout(totalTimer)
      isStreaming.value = false
      controller.value = null
    }
  }

  function abort() {
    aborted.value = true
    controller.value?.abort()
  }

  /** 关闭一条独立提示（仅本地展示层面移除） */
  function dismissNotice(index: number) {
    notices.value = notices.value.filter((_, i) => i !== index)
  }

  onUnmounted(() => {
    if (controller.value) {
      aborted.value = true
      controller.value.abort()
    }
  })

  return {
    isStreaming,
    currentContent,
    references,
    notices,
    crisis,
    error,
    result,
    aborted,
    conversationId,
    sendQuestion,
    abort,
    dismissNotice,
  }
}
