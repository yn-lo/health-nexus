/**
 * sanitize-html — 富文本白名单消毒器（v-html 前必须经过此函数）。
 *
 * 场景：文章正文为医护端 TipTap(StarterKit) 产出的 HTML，患者端以 v-html 渲染。
 * 未消毒内容中的 <script>/事件属性/javascript: 链接会导致存储型 XSS。
 *
 * 策略：template 元素惰性解析（不执行脚本、不加载资源）→ 重建白名单树：
 * 仅保留白名单标签与白名单属性（a[href]、img[src|alt]，URL 限 http/https），
 * 非白名单标签 unwrap（保留已消毒的后代），文本节点原样保留，其余节点丢弃。
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

/** 属性白名单：仅 a[href] 与 img[src|alt]，其余标签不允许任何属性。 */
const ALLOWED_ATTRS: Record<string, string[]> = {
  a: ['href'],
  img: ['src', 'alt'],
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
    for (const name of ALLOWED_ATTRS[tag] ?? []) {
      const v = el.getAttribute(name)
      if (v === null) continue
      if (name === 'alt' || isSafeUrl(v)) clean.setAttribute(name, v)
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
