/**
 * 共享 fetch mock 工具 — 用于 API 层接口契约测试
 *
 * 设计：
 * - mockFetchRouter() 替换 globalThis.fetch，按 URL 模式路由响应
 * - addMatcher() 注册 (match, respond) 对；第一个匹配的 matcher 处理请求
 * - callNumForUrl 区分原始请求 vs 重试（同一 URL 字符串累计计数）
 * - callsFor(predicate) 按断言统计调用次数
 * - lastCall() 返回最后一次调用的 {url, init}
 *
 * 关键实现细节：
 * - ofetch 的 createFetch() 在模块加载时捕获 globalThis.fetch 引用，
 *   因此每个测试必须在 mock fetch 之后通过 vi.resetModules() + 动态 import 重新加载被测模块。
 * - ofetch 默认对 5xx 重试 1 次；本工具不模拟重试，需在测试中显式配置 default matcher。
 */
import { vi } from 'vitest'

export interface MockFetchRouter {
  /** fetch mock 函数（可用于 toHaveBeenCalledTimes 断言） */
  fn: ReturnType<typeof vi.fn>
  /** 完整调用日志（url + init） */
  callLog: Array<{ url: string; init?: RequestInit }>
  /** 注册一个 matcher，按注册顺序匹配，第一个匹配的 matcher 处理请求 */
  addMatcher(m: {
    name: string
    match: (url: string) => boolean
    respond: (ctx: { url: string; init?: RequestInit; callNumForUrl: number }) => Response | Promise<Response>
  }): void
  /** 统计满足 predicate 的调用次数 */
  callsFor(predicate: (url: string) => boolean): number
  /** 返回最后一次调用，便于断言 url/init */
  lastCall(): { url: string; init?: RequestInit } | undefined
}

/** 创建 fetch mock router，替换 globalThis.fetch */
export function mockFetchRouter(): MockFetchRouter {
  const callLog: Array<{ url: string; init?: RequestInit }> = []
  type Matcher = {
    name: string
    match: (url: string) => boolean
    respond: (ctx: { url: string; init?: RequestInit; callNumForUrl: number }) => Response | Promise<Response>
  }
  const matchers: Matcher[] = []
  const fn = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    callLog.push({ url, init })
    for (const m of matchers) {
      if (m.match(url)) {
        // 同一 URL 字符串的累计调用次数（用于区分原始请求 vs 重试）
        const callNumForUrl = callLog.filter((c) => c.url === url).length
        return m.respond({ url, init, callNumForUrl })
      }
    }
    throw new Error(`unmatched URL: ${url}`)
  })
  globalThis.fetch = fn as unknown as typeof globalThis.fetch

  return {
    fn,
    callLog,
    addMatcher(m) {
      matchers.push(m)
    },
    callsFor(predicate) {
      return callLog.filter((c) => predicate(c.url)).length
    },
    lastCall() {
      return callLog[callLog.length - 1]
    },
  }
}

/** 构造 JSON Response（最常用的响应工厂） */
export function jsonRes(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
