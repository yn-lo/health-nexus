import { ofetch, type FetchOptions } from 'ofetch';
import type { TokenResponse, TokenUser } from '../types/auth';

/**
 * 提取用户友好的错误消息。
 * ofetch 的 FetchError.message 是技术文本（"[METHOD] URL: STATUS"），
 * 后端 response.WriteError 统一输出 {code, message}，错误描述在 _data.message 中。
 */
export function errmsg(e: unknown, fallback = '操作失败'): string {
  const msg = (e as { response?: { _data?: { message?: unknown } } })?.response?._data?.message;
  if (typeof msg === 'string') return msg;
  if (e instanceof Error && e.message) return e.message;
  return fallback;
}

/** access token 存储 key */
const TOKEN_KEY = 'hn_access_token';
/** refresh token 存储 key */
const REFRESH_KEY = 'hn_refresh_token';
/** 用户信息存储 key（与 token 同生命周期；route-guard 读取以避免刷新页面后丢失角色） */
const USER_KEY = 'hn_user';
/** 匿名设备标识存储 key */
const DEVICE_ID_KEY = 'hn_device_id';

/** 获取 access token */
export function getAccessToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

/** 获取 refresh token */
export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY);
}

/** 获取或生成匿名设备标识（UUID v4，持久化到 localStorage）。 */
export function getDeviceId(): string {
  let did = localStorage.getItem(DEVICE_ID_KEY);
  if (!did) {
    did = crypto.randomUUID();
    localStorage.setItem(DEVICE_ID_KEY, did);
  }
  return did;
}

/** 存储 token 对 */
export function setTokens(access: string, refresh: string): void {
  localStorage.setItem(TOKEN_KEY, access);
  localStorage.setItem(REFRESH_KEY, refresh);
}

/** 读取持久化的用户信息（页面刷新后 route-guard 据此判断角色） */
export function getUserStored(): TokenUser | null {
  const raw = localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as TokenUser;
  } catch {
    return null;
  }
}

/** 持久化用户信息（登录/注册成功后调用） */
export function setUserStored(user: TokenUser): void {
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

/** 清除 token 对及关联的用户信息 */
export function clearTokens(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
  localStorage.removeItem(USER_KEY);
}

/** 统一登录页路径（chat SPA 下的 /login，所有端共享） */
const LOGIN_PATH = '/login';

/** 患者端公开路径（未登录可正常使用）：会话失效时降级匿名，不硬跳登录 */
const ANON_PATHS = ['/chat', '/wiki', '/about', '/terms', '/privacy'];
/** 位于公开前缀下但仍需登录的子路径：仍需跳登录 */
const AUTH_SUBPATHS = ['/chat/profile', '/chat/change-password'];

/**
 * 当前页面能否匿名使用——决定会话彻底失效时是否硬跳登录。
 * 公开页仅清除本地登录态交给调用方降级，避免打断「暂不登录直接提问」的访客。
 */
function canRunAnonymous(): boolean {
  const p = window.location.pathname;
  if (AUTH_SUBPATHS.some((a) => p === a || p.startsWith(`${a}/`))) return false;
  return ANON_PATHS.some((b) => p === b || p.startsWith(`${b}/`));
}

/** 跨标签页刷新锁 key（localStorage 共享，同一 Web Storage 域内可见） */
const REFRESH_LOCK_KEY = 'hn_refresh_lock';
/** 刷新完成广播频道名（跨标签页通知"已轮换出新 token，请复用"） */
const REFRESH_CHANNEL = 'hn_refresh';

/** 跨标签页刷新锁租约（毫秒）。持锁标签页崩溃后，其它标签页等待超时后可抢锁，避免死锁 */
const REFRESH_LOCK_TTL = 10_000;
/** 等待其它标签页刷新完成的最长时间（毫秒） */
const REFRESH_WAIT_TIMEOUT = 8_000;
/** 轮询间隔（storage 事件在部分场景不可靠，辅以轮询兜底） */
const REFRESH_POLL_INTERVAL = 150;

/** 刷新锁记录：持有者 id + 获取时间戳（判断租约是否过期） */
interface RefreshLock {
  id: string;
  ts: number;
}

function readLock(): RefreshLock | null {
  const raw = localStorage.getItem(REFRESH_LOCK_KEY);
  if (!raw) return null;
  try {
    const lock = JSON.parse(raw) as RefreshLock;
    return typeof lock?.id === 'string' && typeof lock?.ts === 'number' ? lock : null;
  } catch {
    return null;
  }
}

/**
 * 尝试获取跨标签页刷新锁。
 * localStorage 无原子 CAS，采用"写入后回读校验"：写入自己的 id 后立即回读，
 * 若仍是自己的 id 则视为抢锁成功（并发写入时最后写入者胜出，其余回读者读到他人 id 而放弃）。
 */
function acquireRefreshLock(id: string): boolean {
  const lock = readLock();
  if (lock && Date.now() - lock.ts < REFRESH_LOCK_TTL && lock.id !== id) return false;
  localStorage.setItem(REFRESH_LOCK_KEY, JSON.stringify({ id, ts: Date.now() }));
  const check = readLock();
  return check?.id === id;
}

/** 释放刷新锁（仅当锁属于自己时删除，避免误删他人刚获取的锁）。 */
function releaseRefreshLock(id: string): void {
  if (readLock()?.id === id) localStorage.removeItem(REFRESH_LOCK_KEY);
}

/** 广播刷新完成/失败（跨标签页通知；BroadcastChannel 不可用时静默降级为轮询）。 */
function broadcastRefresh(result: 'ok' | 'fail'): void {
  try {
    const ch = new BroadcastChannel(REFRESH_CHANNEL);
    ch.postMessage(result);
    ch.close();
  } catch {
    /* 不支持 BroadcastChannel：依赖轮询兜底 */
  }
}

/** 等待其它标签页完成刷新：轮询 token 是否已被轮换，或广播已到达成功/失败。 */
function waitForOtherTabRefresh(originalRefresh: string): Promise<boolean> {
  return new Promise((resolve) => {
    const deadline = Date.now() + REFRESH_WAIT_TIMEOUT;
    let settled = false;
    let ch: BroadcastChannel | null = null;
    // 用持有对象承载 timer，避免 finish 引用后声明的变量（TDZ），也不触发 prefer-const
    const state: { timer?: ReturnType<typeof setInterval> } = {};
    const finish = (ok: boolean) => {
      if (settled) return;
      settled = true;
      if (state.timer) clearInterval(state.timer);
      try {
        ch?.close();
      } catch {
        /* noop */
      }
      resolve(ok);
    };

    try {
      ch = new BroadcastChannel(REFRESH_CHANNEL);
      ch.onmessage = (ev) => finish(ev.data === 'ok');
    } catch {
      /* 不支持广播：仅轮询 */
    }

    state.timer = setInterval(() => {
      // 令牌已被其它标签页轮换（存储中的 refresh 与本次失败使用的不同）→ 复用新 token
      const current = getRefreshToken();
      if (current && current !== originalRefresh) finish(true);
      else if (Date.now() > deadline) finish(false);
    }, REFRESH_POLL_INTERVAL);
  });
}

/** 刷新锁，防止并发刷新。模块级全局唯一——useSSEChat 复用此锁，避免两处各自刷新竞态消费同一 refresh token */
let refreshPromise: Promise<boolean> | null = null;

/** 尝试刷新 token，返回是否成功。全局共享：并发 401（含 SSE 流）复用同一个刷新请求 */
export async function tryRefreshToken(): Promise<boolean> {
  // 复用正在进行的刷新请求（同一标签页内）
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    const refresh = getRefreshToken();
    if (!refresh) return false;

    // 跨标签页协调：抢锁失败说明另一标签页正在刷新，等它完成并复用轮换出的新 token（P2）。
    const lockId = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    if (!acquireRefreshLock(lockId)) {
      return waitForOtherTabRefresh(refresh);
    }

    try {
      const data = await ofetch<TokenResponse>('/api/auth/refresh', {
        method: 'POST',
        body: { refresh },
      });
      setTokens(data.access, data.refresh);
      broadcastRefresh('ok');
      return true;
    } catch {
      // 失败可能因另一标签页已完成轮换（本次 refresh 已被消费）：先确认存储是否已更新。
      // 若已被轮换出新 token，视为成功（复用），避免"一个页面成功、另一个被清空而跳登录"（P2）。
      if (getRefreshToken() && getRefreshToken() !== refresh) {
        broadcastRefresh('ok');
        return true;
      }
      broadcastRefresh('fail');
      return false;
    } finally {
      releaseRefreshLock(lockId);
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

/** 基础 ofetch 实例：仅负责附加 JWT。401 刷新重试统一由 apiClient 包装层处理 */
const baseClient = ofetch.create({
  baseURL: '/api',
  // 匿名用户需要session cookie来关联会话
  credentials: 'include',

  async onRequest({ options }) {
    const token = getAccessToken();
    if (token) {
      options.headers.set('Authorization', `Bearer ${token}`);
    }
  },
});

/** 判断是否为 ofetch 抛出的 HTTP 401 错误 */
export function isUnauthorizedError(err: unknown): boolean {
  return (err as { response?: { status?: number } })?.response?.status === 401;
}

/**
 * 统一 API 客户端：自动附加 JWT；401 时自动刷新 token 并重试一次，重试结果正常 resolve。
 *
 * 重试不放 ofetch 的 onResponseError：该 hook 无法把 rejection 转为 resolution，
 * 在 hook 内抛出重试结果会让成功数据走调用方 catch 分支（成功的操作被误报为失败）。
 * 包装层重试既让调用方正常 await 到重试结果，又天然保证只重试一次，避免刷新循环。
 */
export async function apiClient<T>(request: string, options?: FetchOptions<'json'>): Promise<T> {
  try {
    return await baseClient<T>(request, options);
  } catch (err) {
    if (!isUnauthorizedError(err)) throw err;
    // 仅在原本持有 token（会话过期）时才尝试刷新并跳转 login。
    // 匿名请求（无 token）的 401 由调用方自行处理，避免匿名页面被强制跳转 login。
    if (!getAccessToken() && !getRefreshToken()) throw err;

    const attemptedRefresh = getRefreshToken();
    const refreshed = await tryRefreshToken();
    if (!refreshed) {
      // 清理前校验：仅当存储中的 refresh token 已被**轮换**（与本次失败尝试使用的不同）时，
      // 说明另一标签页已成功刷新，登录态仍有效 → 复用新 token 重试，而非清空跳登录（P2）。
      const currentRefresh = getRefreshToken();
      if (currentRefresh && currentRefresh !== attemptedRefresh) {
        return await baseClient<T>(request, options);
      }
      clearTokens();
      // 公开页（/chat 等）可匿名继续：仅清登录态、由调用方降级，不硬跳登录打断访客。
      if (!canRunAnonymous()) {
        // ponytail:allow-location - 需登录页面会话失效时必须跳转登录页，跨 MPA 无 Vue Router 替代方案，折中
        window.location.href = LOGIN_PATH;
      }
      throw err;
    }
    // 重试一次：onRequest 会附加新 token；仍失败则直接抛给调用方，不再二次刷新（防循环）
    return await baseClient<T>(request, options);
  }
}
