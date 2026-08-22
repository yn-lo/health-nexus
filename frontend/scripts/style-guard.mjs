/**
 * style-guard.mjs — 前端样式规范门禁检测
 *
 * 检测规则（对应 style-spec.md §5）：
 *   R1  禁止 CSS 中硬编码 hex 色值（tokens.css 除外）
 *   R2  禁止硬编码控件尺寸（44px/48px/32px → 用 --ds-control-*）
 *   R3  禁止单边主题色边框（border-left/right/top/bottom + ai-accent/bg-brand）
 *   R4  禁止组件直接使用原始色板（--color-indigo-* / --color-slate-* 等）
 *   R5  禁止 scoped 中重复控件级样式（min-height + border-radius 同时出现）
 *
 * 用法：node scripts/style-guard.mjs [--fix]
 *   --fix  只输出修复建议，不退出错误码
 *
 * 退出码：0 = 通过，1 = 有违规
 */

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const ROOT = join(import.meta.dirname, '..')
const SRC = join(ROOT, 'src')
const FIX_MODE = process.argv.includes('--fix')

const ALLOWED_HEX_FILES = [
  'tokens.css',
  'main.css',
  'staff-theme.css',
]

const PRIMITIVE_COLOR_PREFIXES = [
  '--color-indigo-',
  '--color-emerald-',
  '--color-amber-',
  '--color-red-',
  '--color-slate-',
]

const CONTROL_SIZE_VALUES = [
  '44px', '48px', '32px',
]

const VIOLATIONS = []

const EXCLUDED_DIRS = ['dev']

function walkDir(dir, ext) {
  const results = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (EXCLUDED_DIRS.includes(entry.name)) continue
      results.push(...walkDir(full, ext))
    } else if (ext.some(e => entry.name.endsWith(e))) {
      results.push(full)
    }
  }
  return results
}

function isAllowedFile(filepath) {
  return ALLOWED_HEX_FILES.some(allowed =>
    filepath.endsWith(allowed)
  )
}

function checkFile(filepath) {
  const content = readFileSync(filepath, 'utf-8')
  const lines = content.split('\n')
  const relPath = relative(ROOT, filepath).replace(/\\/g, '/')
  const allowed = isAllowedFile(filepath)

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const lineNum = i + 1
    const trimmed = line.trim()

    if (trimmed.startsWith('//') || trimmed.startsWith('/*') || trimmed.startsWith('*')) continue
    if (trimmed.startsWith('@import') || trimmed.startsWith('@theme') || trimmed.startsWith('@layer')) continue
    if (line.includes('style-guard:ignore')) continue

    if (!allowed) {
      checkHardcodedHex(relPath, lineNum, line)
      checkPrimitiveColorToken(relPath, lineNum, line)
    }

    checkSingleSideAccentBorder(relPath, lineNum, line)
    checkControlSize(relPath, lineNum, line, filepath)
    checkScopedControlStyles(relPath, lineNum, line, content, i)
  }
}

function checkHardcodedHex(file, lineNum, line) {
  const hexPattern = /#[0-9a-fA-F]{3,8}\b/g
  let match
  while ((match = hexPattern.exec(line)) !== null) {
    const val = match[0]
    if (val.length === 4 || val.length === 7 || val.length === 9) {
      VIOLATIONS.push({
        rule: 'R1',
        severity: 'error',
        file,
        line: lineNum,
        message: `硬编码色值 "${val}"，请使用语义令牌（var(--text-*)/var(--bg-*)/var(--ai-accent) 等）`,
        fix: '替换为对应的语义令牌，如 var(--text-default)、var(--bg-brand) 等',
      })
    }
  }
}

function checkPrimitiveColorToken(file, lineNum, line) {
  for (const prefix of PRIMITIVE_COLOR_PREFIXES) {
    if (line.includes(prefix)) {
      const idx = line.indexOf(prefix)
      const after = line.substring(idx + prefix.length)
      const numMatch = after.match(/^(\d+)/)
      if (numMatch) {
        const token = prefix + numMatch[1]
        VIOLATIONS.push({
          rule: 'R4',
          severity: 'error',
          file,
          line: lineNum,
          message: `直接使用原始色板 "${token}"，组件应使用语义令牌`,
          fix: '替换为语义令牌，如 --bg-brand、--text-default、--ai-accent 等',
        })
      }
    }
  }
}

function checkSingleSideAccentBorder(file, lineNum, line) {
  const sides = ['left', 'right']
  const accentTokens = ['--ai-accent', '--bg-brand', '--text-brand', '--accent-medical']
  for (const side of sides) {
    const borderPattern = new RegExp(`border-${side}\\s*:\\s*\\d+px\\s+solid`)
    if (borderPattern.test(line)) {
      for (const token of accentTokens) {
        if (line.includes(token)) {
          VIOLATIONS.push({
            rule: 'R3',
            severity: 'error',
            file,
            line: lineNum,
            message: `单边主题色边框 border-${side}，禁止仅一条边添加主题色边框`,
            fix: '使用 box-shadow 实现光晕效果，或使用四周统一 border',
          })
          break
        }
      }
    }
  }
}

function checkControlSize(file, lineNum, line, filepath) {
  if (isAllowedFile(filepath)) return
  for (const size of CONTROL_SIZE_VALUES) {
    const patterns = [
      new RegExp(`min-height\\s*:\\s*${size.replace('px', '')}px`, 'i'),
      new RegExp(`height\\s*:\\s*${size.replace('px', '')}px`, 'i'),
    ]
    for (const pattern of patterns) {
      if (pattern.test(line) && !line.includes('var(--ds-control') && !line.includes('var(--touch-target') && !line.includes('var(--layout')) {
        VIOLATIONS.push({
          rule: 'R2',
          severity: 'warning',
          file,
          line: lineNum,
          message: `硬编码控件尺寸 ${size}，请使用 --ds-control-height-* 令牌`,
          fix: `替换为 var(--ds-control-height-sm/md/lg)`,
        })
      }
    }
  }
}

function checkScopedControlStyles(file, lineNum, line, content, lineIdx) {
  if (!file.includes('.vue')) return
  const scopedMatch = content.match(/<style[^>]*scoped[^>]*>/)
  if (!scopedMatch) return

  const scopedStart = content.substring(0, content.indexOf(scopedMatch[0])).split('\n').length
  const styleEnd = content.indexOf('</style>')
  if (styleEnd === -1) return
  const styleEndLine = content.substring(0, styleEnd).split('\n').length

  if (lineNum < scopedStart || lineNum > styleEndLine) return

  const hasMinHeight = /min-height\s*:/.test(line) && !line.includes('var(--ds-control') && !line.includes('var(--touch-target')
  const hasBorderRadius = /border-radius\s*:/.test(line) && !line.includes('var(--ds-control') && !line.includes('var(--radius-') && !line.includes('var(--ds-control-radius')

  if (hasMinHeight && hasBorderRadius) {
    VIOLATIONS.push({
      rule: 'R5',
      severity: 'warning',
      file,
      line: lineNum,
      message: 'scoped 中同时设置 min-height + border-radius（控件级样式），应由 @layer components 全局处理',
      fix: '移除控件级样式，只保留装饰（背景/阴影/渐变）',
    })
  }
}

const files = [
  ...walkDir(SRC, ['.vue', '.css', '.ts']),
]

for (const file of files) {
  checkFile(file)
}

const errors = VIOLATIONS.filter(v => v.severity === 'error')
const warnings = VIOLATIONS.filter(v => v.severity === 'warning')

if (VIOLATIONS.length > 0) {
  console.log(`\n🎨 Style Guard — 前端样式规范检测\n`)
  console.log(`  扫描文件: ${files.length}`)
  console.log(`  ❌ Errors: ${errors.length}`)
  console.log(`  ⚠️  Warnings: ${warnings.length}\n`)

  const grouped = {}
  for (const v of VIOLATIONS) {
    const key = v.rule
    if (!grouped[key]) grouped[key] = []
    grouped[key].push(v)
  }

  for (const [rule, items] of Object.entries(grouped)) {
    const ruleNames = {
      R1: 'R1 禁止硬编码色值',
      R2: 'R2 禁止硬编码控件尺寸',
      R3: 'R3 禁止单边主题色边框',
      R4: 'R4 禁止直接使用原始色板',
      R5: 'R5 禁止 scoped 重复控件级样式',
    }
    console.log(`── ${ruleNames[rule] || rule} (${items.length}) ──\n`)
    for (const item of items) {
      const icon = item.severity === 'error' ? '❌' : '⚠️ '
      console.log(`  ${icon} ${item.file}:${item.line}`)
      console.log(`     ${item.message}`)
      if (FIX_MODE) {
        console.log(`     💡 ${item.fix}`)
      }
      console.log()
    }
  }

  if (!FIX_MODE && errors.length > 0) {
    process.exit(1)
  }
} else {
  console.log(`\n🎨 Style Guard — 全部通过 ✓\n  扫描文件: ${files.length}\n`)
  process.exit(0)
}
