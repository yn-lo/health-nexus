/**
 * useSSEChat composable 单元测试
 * 覆盖：POST + JSON body、conversation 事件、safety_warning 双格式（裸字符串 / JSON mode）、
 *       SSE 流式解析（标准 event:/data: 帧）、HTTP 错误、AbortError、Authorization 头注入
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useSSEChat } from '@/chat/composables/useSSEChat'

/** 构造 SSE 格式的 ReadableStream */
function makeSSEStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk))
      }
      controller.close()
    },
  })
}

/** 构造成功 Response */
function makeOkResponse(stream: ReadableStream<Uint8Array>): Response {
  return new Response(stream, { status: 200, headers: { 'content-type': 'text/event-stream' } })
}

/** 构造错误 Response（后端 {code,message} 格式） */
function makeErrorResponse(status: number, message?: string): Response {
  const body = message ? JSON.stringify({ message }) : ''
  return new Response(body, { status, headers: { 'content-type': 'application/json' } })
}

describe('useSSEChat', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    vi.restoreAllMocks()
  })

  // ── POST + JSON body ──────────────────────────────────────────

  it('使用 POST 方法 + JSON body 发送（非 GET query）', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(makeOkResponse(makeSSEStream([])))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { sendQuestion } = useSSEChat({ conversationId: 'conv-1', selectedDeptId: 3 })
    await sendQuestion('头痛怎么办')

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit]
    // URL 不含 query 参数（PHI 不泄露到日志）
    expect(url).not.toContain('?')
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string)
    expect(body.message).toBe('头痛怎么办')
    expect(body.conversation_id).toBe('conv-1')
    expect(body.selected_dept_id).toBe(3)
  })

  it('无 selectedDeptId 时 body 不含该字段', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(makeOkResponse(makeSSEStream([])))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.selected_dept_id).toBeUndefined()
  })

  // ── conversation 事件 ─────────────────────────────────────────

  it('解析 conversation 事件，更新 conversationId', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: conversation\ndata: {"conversation_id":"new-conv-id"}\n\n',
        'event: token\ndata: ok\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { conversationId, sendQuestion } = useSSEChat({ conversationId: '' })
    expect(conversationId.value).toBe('')
    await sendQuestion('hi')

    expect(conversationId.value).toBe('new-conv-id')
  })

  it('后续请求使用 conversation 事件更新后的 ID', async () => {
    let callCount = 0
    const fetchSpy = vi.fn().mockImplementation(() => {
      callCount++
      if (callCount === 1) {
        return Promise.resolve(makeOkResponse(makeSSEStream([
          'event: conversation\ndata: {"conversation_id":"server-id"}\n\n',
          'event: done\ndata: [DONE]\n\n',
        ])))
      }
      return Promise.resolve(makeOkResponse(makeSSEStream([
        'event: done\ndata: [DONE]\n\n',
      ])))
    })
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { sendQuestion } = useSSEChat({ conversationId: '' })
    await sendQuestion('第一条')
    await sendQuestion('第二条')

    const [, init2] = fetchSpy.mock.calls[1] as [string, RequestInit]
    const body2 = JSON.parse(init2.body as string)
    expect(body2.conversation_id).toBe('server-id')
  })

  // ── safety_warning 双格式 ─────────────────────────────────────

  it('safety_warning 裸字符串 → 呈现在聊天内容中（currentContent）', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: safety_warning\ndata: 紧急就医提醒\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { safetyWarning, currentContent, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(safetyWarning.value).toEqual({ text: '紧急就医提醒', mode: undefined })
    expect(currentContent.value).toBe('紧急就医提醒')
  })

  it('safety_warning 裸字符串追加到已有内容后（换行分隔）', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: token\ndata: 部分回答\n\n',
        'event: safety_warning\ndata: 以上仅供参考\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { currentContent, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(currentContent.value).toBe('部分回答\n\n以上仅供参考')
  })

  it('safety_warning JSON replace → 替换 currentContent', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: token\ndata: 原始内容\n\n',
        'event: safety_warning\ndata: {"mode":"replace","text":"[内容已被安全审查替换]"}\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { safetyWarning, currentContent, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(safetyWarning.value).toEqual({ text: '[内容已被安全审查替换]', mode: 'replace' })
    expect(currentContent.value).toBe('[内容已被安全审查替换]')
  })

  it('safety_warning JSON append → 追加到 currentContent', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: token\ndata: 回答内容\n\n',
        'event: safety_warning\ndata: {"mode":"append","text":"\\n（以上仅供参考）"}\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { safetyWarning, currentContent, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(safetyWarning.value).toEqual({ text: '\n（以上仅供参考）', mode: 'append' })
    expect(currentContent.value).toBe('回答内容\n（以上仅供参考）')
  })

  // ── 原有事件解析 ──────────────────────────────────────────────

  it('成功流式解析多个 token 事件，内容累加到 currentContent', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: token\ndata: Hello\n\n',
        'event: token\ndata:  world\n\n',
        'event: done\ndata: [DONE]\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { currentContent, isStreaming, error, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(currentContent.value).toBe('Hello world')
    expect(isStreaming.value).toBe(false)
    expect(error.value).toBeNull()
  })

  it('解析 references 事件（JSON 数组）', async () => {
    const refs = [{ chunk_id: 'c1', article_id: 'a1', article_title: 't', content: 'x', score: 0.9 }]
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        `event: references\ndata: ${JSON.stringify(refs)}\n\n`,
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { references, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(references.value).toEqual(refs)
  })

  it('解析 crisis 事件（answer）', async () => {
    const crisisData = { answer: '请拨打热线 400-161-9995' }
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        `event: crisis\ndata: ${JSON.stringify(crisisData)}\n\n`,
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { crisis, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(crisis.value).toEqual(crisisData)
  })

  it('解析 error 事件（{message} JSON）', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      makeOkResponse(makeSSEStream([
        'event: error\ndata: {"message":"流式错误"}\n\n',
      ])),
    )
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { error, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(error.value).toBe('流式错误')
  })

  // ── 鉴权头 ────────────────────────────────────────────────────

  it('Authorization 头从 localStorage 注入', async () => {
    localStorage.setItem('hn_access_token', 'test-token')
    const fetchSpy = vi.fn().mockResolvedValue(makeOkResponse(makeSSEStream([])))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit | undefined
    const headers = init?.headers as Record<string, string> | undefined
    expect(headers?.Authorization).toBe('Bearer test-token')
  })

  it('无 token 时不带 Authorization 头', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(makeOkResponse(makeSSEStream([])))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit | undefined
    const headers = init?.headers as Record<string, string> | undefined
    expect(headers?.Authorization).toBeUndefined()
  })

  // ── 错误处理 ──────────────────────────────────────────────────

  it('HTTP 错误时设置 error 并解析 message', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(makeErrorResponse(500, '内部错误'))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { error, isStreaming, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(error.value).toBe('内部错误')
    expect(isStreaming.value).toBe(false)
  })

  it('HTTP 错误且响应非 JSON 时使用默认错误消息', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(new Response('plain text', { status: 502 }))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { error, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(error.value).toContain('502')
  })

  it('AbortError 静默 error，但标记 aborted=true', async () => {
    const abortErr = new DOMException('aborted', 'AbortError')
    const fetchSpy = vi.fn().mockRejectedValue(abortErr)
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { error, aborted, isStreaming, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(error.value).toBeNull()
    expect(aborted.value).toBe(true)
    expect(isStreaming.value).toBe(false)
  })

  it('非 AbortError 的网络异常写入 error', async () => {
    const fetchSpy = vi.fn().mockRejectedValue(new Error('network down'))
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { error, sendQuestion } = useSSEChat({ conversationId: 'conv-1' })
    await sendQuestion('hi')

    expect(error.value).toBe('network down')
  })

  it('abort() 触发 AbortController.abort 并标记 aborted', async () => {
    const fetchSpy = vi.fn().mockImplementation((_url: string, init: RequestInit) => {
      return new Promise<Response>((_, reject) => {
        init.signal?.addEventListener('abort', () => {
          reject(new DOMException('aborted', 'AbortError'))
        })
      })
    })
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

    const { aborted, sendQuestion, abort } = useSSEChat({ conversationId: 'conv-1' })
    const promise = sendQuestion('hi')
    abort()
    await promise

    expect(aborted.value).toBe(true)
  })
})
