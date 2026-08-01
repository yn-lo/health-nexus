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

/** 刷新锁，防止并发刷新。模块级全局唯一——useSSEChat 复用此锁，避免两处各自刷新竞态消费同一 refresh token */
let refreshPromise: Promise<boolean> | null = null;

/** 尝试刷新 token，返回是否成功。全局共享：并发 401（含 SSE 流）复用同一个刷新请求 */
export async function tryRefreshToken(): Promise<boolean> {
  // 复用正在进行的刷新请求
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    const refresh = getRefreshToken();
    if (!refresh) return false;

    try {
      const data = await ofetch<TokenResponse>('/api/auth/refresh', {
        method: 'POST',
        body: { refresh },
      });
      setTokens(data.access, data.refresh);
      return true;
    } catch {
      return false;
    } finally {
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
function isUnauthorizedError(err: unknown): boolean {
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

    const refreshed = await tryRefreshToken();
    if (!refreshed) {
      clearTokens();
      // ponytail:allow-location - 401 刷新失败后必须跳转登录页，跨 MPA 无 Vue Router 替代方案，折中
      window.location.href = LOGIN_PATH;
      throw err;
    }
    // 重试一次：onRequest 会附加新 token；仍失败则直接抛给调用方，不再二次刷新（防循环）
    return await baseClient<T>(request, options);
  }
}
