const rtf = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
const dtfShort = new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit' })
const dtfFull = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', hour12: false,
})

const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

/** 相对时间 — "刚刚"/"3分钟前"/"2小时前"/"5天前"，超过 7 天回退 MM/DD */
export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const diff = Date.now() - d.getTime()
  if (diff < MINUTE) return rtf.format(0, 'second')
  if (diff < HOUR) return rtf.format(-Math.floor(diff / MINUTE), 'minute')
  if (diff < DAY) return rtf.format(-Math.floor(diff / HOUR), 'hour')
  if (diff < 7 * DAY) return rtf.format(-Math.floor(diff / DAY), 'day')
  return dtfShort.format(d)
}

/** ISO → YYYY-MM-DD */
export function fmtDate(iso: string): string {
  return iso ? iso.slice(0, 10) : ''
}

/** ISO → YYYY-MM-DD HH:mm */
export function fmtDateTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '' : dtfFull.format(d)
}

/** ISO → MM-DD */
export function fmtShortDate(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '' : dtfShort.format(d)
}

/** 数字缩写 — 1200 → "1.2k" */
export function fmtCompact(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}

/** 用户 ID 补零 — 42 → "HN00000042" */
export function fmtUserId(id: number): string {
  return `HN${String(id).padStart(8, '0')}`
}

/** 剥离 HTML 标签，提取纯文本 */
export function stripHtml(html: string): string {
  if (!html) return ''
  return html.replace(/<[^>]*>/g, '')
}
