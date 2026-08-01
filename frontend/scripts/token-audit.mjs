/**
 * token-audit.mjs — 设计令牌使用频率审计报告
 *
 * 扫描所有 .vue/.css/.ts 文件中对 CSS 自定义属性的引用，
 * 按使用次数排序，帮助识别低频/未使用令牌。
 *
 * 用法：node scripts/token-audit.mjs
 * 退出码：0（始终成功，这不是门禁脚本）
 *
 * 输出说明：
 *   ✅ 常用（≥5 次）
 *   🟡 低频（1-4 次）
 *   🔴 未使用（0 次，仅在 tokens.css 中定义）
 *
 * 低频/未使用令牌不一定需要删除，可能是：
 *   - 暗色模式专用令牌（.dark 中定义，亮色代码自然不会引用）
 *   - 新增令牌，页面还未开发
 *   - 仅在 @theme inline 映射中引用（供 Tailwind 工具类使用）
 */

import { readdirSync, readFileSync } from 'node:fs'
import { join, relative, basename } from 'node:path'

const ROOT = join(import.meta.dirname, '..')
const SRC = join(ROOT, 'src')
const TOKENS_FILE = join(SRC, 'assets/styles/tokens.css')
const THEME_FILE = join(SRC, 'assets/styles/main.css')
const VANT_THEME_FILE = join(SRC, 'shared/constants/vant-theme.ts')

function walkDir(dir, ext) {
  const results = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) results.push(...walkDir(full, ext))
    else if (ext.some(e => entry.name.endsWith(e))) results.push(full)
  }
  return results
}

// 从 tokens.css 中提取所有定义的令牌名
function extractTokenNames() {
  const content = readFileSync(TOKENS_FILE, 'utf-8')
  const tokens = new Set()
  const pattern = /(--[a-zA-Z0-9-]+)\s*:/g
  let match
  while ((match = pattern.exec(content)) !== null) {
    const name = match[1]
    if (name.startsWith('--')) tokens.add(name)
  }
  return tokens
}

// 扫描所有文件，统计每个 var(--xxx) 的引用次数
function scanReferences() {
  const files = [
    ...walkDir(SRC, ['.vue', '.css', '.ts']),
  ]

  const refs = {}  // token -> [{file, line}]

  for (const file of files) {
    const relPath = relative(ROOT, file).replace(/\\/g, '/')
    const content = readFileSync(file, 'utf-8')
    const lines = content.split('\n')

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i]
      // 匹配 var(--xxx) 和 --xxx:（CSS 变量引用和 vite config 中的引用）
      const pattern = /var\(\s*(--[a-zA-Z0-9-]+)/g
      let match
      while ((match = pattern.exec(line)) !== null) {
        const token = match[1]
        if (!refs[token]) refs[token] = []
        refs[token].push({ file: relPath, line: i + 1 })
      }

      // 也匹配 vant-theme.ts 中的字符串引用（'var(--xxx)'）
      const strPattern = /var\((--[a-zA-Z0-9-]+)\)/g
      let strMatch
      while ((strMatch = strPattern.exec(line)) !== null) {
        const token = strMatch[1]
        if (!refs[token]) refs[token] = []
        refs[token].push({ file: relPath, line: i + 1 })
      }
    }
  }

  return refs
}

// 分析 tokens.css 内部的自引用（.dark 中引用 :root 中的令牌）
function detectInternalRefs() {
  const content = readFileSync(TOKENS_FILE, 'utf-8')
  const lines = content.split('\n')
  const internal = {}  // token -> count

  for (const line of lines) {
    const pattern = /var\(\s*(--[a-zA-Z0-9-]+)/g
    let match
    while ((match = pattern.exec(line)) !== null) {
      const token = match[1]
      internal[token] = (internal[token] || 0) + 1
    }
  }
  return internal
}

const definedTokens = extractTokenNames()
const refs = scanReferences()
const internalRefs = detectInternalRefs()

// 统计结果
const results = []
for (const token of definedTokens) {
  const externalRefs = refs[token] || []
  const selfRefs = internalRefs[token] || 0
  // 排除 tokens.css 内部的自引用，只算外部文件引用
  const externalOnly = externalRefs.filter(r => !r.file.includes('tokens.css'))
  const mainCssRefs = externalRefs.filter(r => r.file.includes('main.css'))
  const vantThemeRefs = externalRefs.filter(r => r.file.includes('vant-theme.ts'))
  const pageRefs = externalRefs.filter(r =>
    !r.file.includes('tokens.css') &&
    !r.file.includes('main.css') &&
    !r.file.includes('vant-theme.ts')
  )

  results.push({
    token,
    total: externalRefs.length,
    selfRefs,
    mainCss: mainCssRefs.length,
    vantTheme: vantThemeRefs.length,
    pages: pageRefs.length,
    pageFiles: [...new Set(pageRefs.map(r => r.file))],
  })
}

// 排序：按页面引用次数升序（未使用的排最前）
results.sort((a, b) => {
  if (a.pages !== b.pages) return a.pages - b.pages
  if (a.total !== b.total) return a.total - b.total
  return a.token.localeCompare(b.token)
})

// 输出报告
console.log(`\n📊 Token Audit — 设计令牌使用频率报告\n`)
console.log(`  定义令牌数: ${definedTokens.size}`)
console.log(`  扫描文件数: ${walkDir(SRC, ['.vue', '.css', '.ts']).length}\n`)

// 按区块分组输出
const categories = [
  { name: 'AI-Native AI 动效', prefix: '--ai-', emoji: '🤖' },
  { name: 'Hero-Centric 视觉', prefix: '--hero-', emoji: '✨' },
  { name: 'Micro-interactions 微交互', prefix: '--micro-', emoji: '⚡' },
  { name: 'Accessible 无障碍', prefix: ['--focus-', '--touch-target-', '--reading-'], emoji: '♿' },
  { name: '原始色板 Primitive', prefix: '--color-', emoji: '🎨' },
  { name: '语义色 Brand', prefix: ['--bg-brand', '--text-brand', '--icon-brand', '--border-brand'], emoji: '💜' },
  { name: '语义色 Text', prefix: '--text-', emoji: '📝' },
  { name: '语义色 Background', prefix: '--bg-base-', emoji: '🖼️' },
  { name: '语义色 Overlay', prefix: '--bg-overlay-', emoji: '🪟' },
  { name: '语义色 Status', prefix: ['--status-', '--accent-'], emoji: '🚦' },
  { name: '语义色 Border', prefix: '--border-', emoji: '🔲' },
  { name: '语义色 Icon', prefix: '--icon-', emoji: '🔍' },
  { name: '语义色 Data', prefix: '--data-', emoji: '📊' },
  { name: '语义色 Chart', prefix: '--chart-', emoji: '📈' },
  { name: '语义色 State', prefix: ['--hover-overlay', '--press-overlay'], emoji: '👆' },
  { name: '间距 Spacing', prefix: '--spacer-', emoji: '📏' },
  { name: '圆角 Radius', prefix: '--radius-', emoji: '⬜' },
  { name: '字号 Typography', prefix: ['--body-', '--heading-'], emoji: '🔤' },
  { name: '字重 Font Weight', prefix: '--font-weight-', emoji: '💪' },
  { name: '行高 Line Height', prefix: '--leading-', emoji: '↕️' },
  { name: '布局 Layout', prefix: '--layout-', emoji: '📐' },
  { name: '阴影 Shadow', prefix: '--shadow-', emoji: '🌑' },
  { name: '动画 Animation', prefix: '--animation-', emoji: '🎬' },
  { name: '控件 DS Control', prefix: '--ds-control-', emoji: '🎛️' },
  { name: '透明度 Opacity', prefix: '--opacity-', emoji: '👁️' },
  { name: '宽度 Width', prefix: '--w-', emoji: '↔️' },
  { name: '断点 Breakpoint', prefix: '--breakpoint-', emoji: '📱' },
  { name: '层级 Z-index', prefix: '--z-', emoji: '📚' },
  { name: '其他', prefix: null, emoji: '📦' },
]

function matchCategory(token) {
  for (const cat of categories) {
    if (!cat.prefix) continue
    const prefixes = Array.isArray(cat.prefix) ? cat.prefix : [cat.prefix]
    if (prefixes.some(p => token.startsWith(p))) return cat
  }
  return categories[categories.length - 1]  // "其他"
}

let unusedCount = 0
let lowFreqCount = 0

for (const cat of categories) {
  const items = results.filter(r => matchCategory(r.token) === cat)
  if (items.length === 0) continue

  const catUnused = items.filter(i => i.pages === 0 && i.mainCss === 0 && i.vantTheme === 0)
  const catLow = items.filter(i => i.pages > 0 && i.pages < 5)
  const catOk = items.filter(i => i.pages >= 5 || i.mainCss > 0 || i.vantTheme > 0)

  unusedCount += catUnused.length
  lowFreqCount += catLow.length

  console.log(`${cat.emoji} ${cat.name}（${items.length} 个令牌）`)

  // 只显示低频和未使用的
  const needAttention = [...catUnused, ...catLow]
  if (needAttention.length === 0) {
    console.log(`  ✅ 全部常用\n`)
    continue
  }

  for (const item of needAttention) {
    if (item.pages === 0 && item.mainCss === 0 && item.vantTheme === 0) {
      console.log(`  🔴 ${item.token} — 未使用（外部引用 0 次）`)
    } else if (item.pages < 5) {
      const files = item.pageFiles.length > 2
        ? item.pageFiles.slice(0, 2).join(', ') + ` +${item.pageFiles.length - 2}`
        : item.pageFiles.join(', ')
      console.log(`  🟡 ${item.token} — 页面引用 ${item.pages} 次（${files || 'main.css/vant-theme'}）`)
    }
  }
  console.log()
}

// 汇总
console.log(`── 汇总 ──────────────────────────────────────────\n`)
console.log(`  ✅ 常用（≥5 次页面引用或 main.css/vant-theme 引用）: ${results.length - unusedCount - lowFreqCount}`)
console.log(`  🟡 低频（1-4 次页面引用）: ${lowFreqCount}`)
console.log(`  🔴 未使用（0 次外部引用）: ${unusedCount}`)
console.log()
console.log(`💡 建议：未使用令牌不一定是废令牌。可能是暗色模式专用、新页面预留、或 Tailwind 工具类映射。`)
console.log(`   季度清理时人工审核后再决定是否合并或删除。\n`)
