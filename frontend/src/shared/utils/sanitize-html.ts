/**
 * sanitize-html — 富文本白名单消毒器（v-html 前必须经过此函数）。
 *
 * 场景：文章正文为医护端 TipTap(StarterKit) 产出的 HTML，患者端以 v-html 渲染。
 * 未消毒内容中的 <script>/事件属性/javascript: 链接会导致存储型 XSS。
 *
 * 策略：template 元素惰性解析（不执行脚本、不加载资源）→ 重建白名单树：
 * 仅保留白名单标签与白名单属性（a[href]、img[src|alt|width|height]，URL 限 http/https，
 * width/height 限数值，style 仅保留 text-align），非白名单标签 unwrap（保留已消毒的后代），
 * 文本节点原样保留，其余节点丢弃。
 *
 * ponytail: 手写白名单消毒器而非引入 DOMPurify，省一个依赖；
 * 上限——不处理 SVG/MathML/变异编码等高级绕过向量（当前输入源为
 * markdown-it + TipTap 受限标签集，攻击面有限）。
 * 升级路径：npm i dompurify 后以 DOMPurify.sanitize 替换本函数。
 */

/** 允许保留的标签（TipTap StarterKit 输出集合 + 排版标签）。 */
const ALLOWED_TAGS = new Set([
  'p', 'br', 'hr',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'ul', 'ol', 'li',
  'blockquote', 'pre', 'code',
  'strong', 'b', 'em', 'i', 's', 'u', 'mark', 'sub', 'sup',
  'a', 'img',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td',
  'div', 'span',
])

/** 属性白名单：a[href] 与 img[src|alt|width|height]；其余标签仅允许消毒后的 style（见 sanitizeStyle）。 */
const ALLOWED_ATTRS: Record<string, string[]> = {
  a: ['href'],
  img: ['src', 'alt', 'width', 'height'],
}

/** reNumericAttr width/height 仅允许纯数字或百分比（TipTap resize 与常规排版输出形态）。 */
const reNumericAttr = /^\d{1,4}%?$/

/** reSafeStyleDecl 仅放行无害排版声明（text-align 为 TextAlign 输出；display/margin 为块级图片对齐输出），其余 CSS 一律丢弃。 */
const reSafeStyleDecl =
  /^\s*(?:(text-align)\s*:\s*(left|center|right|justify)|(display)\s*:\s*(block)|(margin-(?:left|right))\s*:\s*(auto|0px?))\s*;?$/i

/** sanitizeStyle 从 style 属性值中仅保留白名单声明，其余丢弃。 */
function sanitizeStyle(raw: string): string | null {
  const kept = raw
    .split(';')
    .map(decl => decl.trim())
    .filter(decl => reSafeStyleDecl.test(decl))
  return kept.length > 0 ? kept.join('; ') : null
}

/** isSafeAttrValue 判断属性值是否可原样保留（URL 类走 isSafeUrl，数值类走白名单正则，style 走 sanitizeStyle）。 */
function isSafeAttrValue(tag: string, name: string, value: string): boolean {
  if (name === 'alt') return true
  if (name === 'width' || name === 'height') return reNumericAttr.test(value)
  if (name === 'style') return sanitizeStyle(value) !== null
  return isSafeUrl(value) // a[href] / img[src]
}

/** isSafeUrl 判断 URL 是否仅允许 http/https 协议（拦截 javascript:/data: 等）。 */
function isSafeUrl(raw: string): boolean {
  try {
    const u = new URL(raw, 'https://placeholder.invalid')
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

/** sanitizeChildren 递归重建消毒后的子树。 */
function sanitizeChildren(parent: Element | DocumentFragment): DocumentFragment {
  const frag = document.createDocumentFragment()
  for (const child of Array.from(parent.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) {
      frag.appendChild(child.cloneNode())
      continue
    }
    if (child.nodeType !== Node.ELEMENT_NODE) continue // 注释等节点直接丢弃
    const el = child as Element
    const tag = el.tagName.toLowerCase()
    if (!ALLOWED_TAGS.has(tag)) {
      // 非白名单标签：unwrap——保留已消毒的后代内容，标签本身丢弃。
      frag.appendChild(sanitizeChildren(el))
      continue
    }
    const clean = document.createElement(tag)
    // style 对所有白名单标签开放，但值经 sanitizeStyle 仅保留 text-align（TextAlign 扩展输出形态）。
    for (const name of [...(ALLOWED_ATTRS[tag] ?? []), 'style']) {
      const v = el.getAttribute(name)
      if (v === null) continue
      if (name === 'style') {
        const safe = sanitizeStyle(v)
        if (safe !== null) clean.setAttribute(name, safe)
        continue
      }
      if (isSafeAttrValue(tag, name, v)) clean.setAttribute(name, v)
    }
    if (tag === 'a') {
      clean.setAttribute('target', '_blank')
      clean.setAttribute('rel', 'noopener noreferrer')
    }
    clean.appendChild(sanitizeChildren(el))
    frag.appendChild(clean)
  }
  return frag
}

/** sanitizeHtml 消毒富文本 HTML，返回可安全用于 v-html 的字符串。 */
export function sanitizeHtml(dirty: string): string {
  const tpl = document.createElement('template')
  tpl.innerHTML = dirty
  const out = document.createElement('template')
  out.content.appendChild(sanitizeChildren(tpl.content))
  return out.innerHTML
}
