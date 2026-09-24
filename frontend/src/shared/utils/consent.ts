/**
 * 知情告知本地记录 — 仅存于浏览器 localStorage。
 *
 * 设计约束：匿名用户不落库，服务端不感知「是否已确认」；告知版本号变更后
 * 本地记录不匹配，用户会被再次要求确认（内容有实质变更时递增 CONSENT_VERSION）。
 */

/** 告知版本号 — 与告知内容同源维护，内容实质变更时递增（建议用日期） */
export const CONSENT_VERSION = '2026-09-23'

/** localStorage 键 — 保存用户已确认的告知版本号 */
export const CONSENT_STORAGE_KEY = 'hn_consent_version'

/** 是否已确认当前版本告知 */
export function hasAcceptedConsent(): boolean {
  return localStorage.getItem(CONSENT_STORAGE_KEY) === CONSENT_VERSION
}

/** 记录已确认当前版本告知 */
export function acceptConsent(): void {
  localStorage.setItem(CONSENT_STORAGE_KEY, CONSENT_VERSION)
}
