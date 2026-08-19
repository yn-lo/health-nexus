/**
 * 前端架构约束测试 — AC-ARCH-FE-* 追溯矩阵
 * 对齐 CLAUDE.md 前端硬性规则 + harness/frontend/specs/2026-07-17-frontend-rewrite-design.md §9
 *
 * 约束 ID 对照：
 *   AC-ARCH-FE-01  禁止 any 类型
 *   AC-ARCH-FE-02  Vue 模板禁止静态内联 style
 *   AC-ARCH-FE-03  禁止直接 DOM 操作
 *   AC-ARCH-FE-04  Vue 组件必须使用 <script setup>
 *   AC-ARCH-FE-05  双 SPA 跨端隔离（views/components/composables/stores/router 全目录）
 *   AC-ARCH-FE-06  视图禁止直接 fetch/axios
 *   AC-ARCH-FE-07  死代码黑名单（design §3.1/§3.2 已删除文件禁止再 import）
 *   AC-ARCH-FE-08  管理员路由守卫完整性（config/* 路由必须挂 adminRouteGuard）
 *   AC-ARCH-FE-09  角色字面量强制（必须用 ADMIN_ROLES 常量）
 *   AC-ARCH-FE-10  禁止 console.log/debug/info 调试输出
 *   AC-ARCH-FE-11  禁止组件间 prop 传递超过两层
 *   AC-ARCH-FE-12  死代码检测扩展（未被引用的导出函数/类型/组件）
 *   AC-ARCH-FE-13  tsconfig strict 模式强制
 *   AC-ARCH-FE-14  禁止 window.prompt/alert/confirm 命令式对话框
 *   AC-ARCH-FE-15  禁止 window.location 直接赋值（应使用 Vue Router）
 *   AC-ARCH-FE-16  <style scoped> 块中禁止非 CSS 变量覆盖的自定义规则
 *   AC-ARCH-FE-17  Portal 字面量强制（'staff'/'patient' 必须用常量）
 *   AC-ARCH-FE-18  分页类型统一（禁止多处重复定义 Paginated）
 *   AC-ARCH-FE-19  路由跨端 import 检测（staff 路由禁止 import chat 视图）
 *   AC-ARCH-FE-20  v-html 使用审计（仅允许白名单组件使用）
 *   AC-ARCH-FE-21  禁止外部 CDN 资源引用（必须使用本地资源）
 */
import { describe, it } from 'vitest'
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { resolve, relative, extname, sep } from 'node:path'

const SRC_DIR = resolve(__dirname, '../../src')
const ROOT_DIR = resolve(__dirname, '../..')

/** 递归获取目录下所有文件 */
function walkDir(dir: string, exts: string[]): string[] {
  const results: string[] = []
  for (const entry of readdirSync(dir)) {
    const fullPath = resolve(dir, entry)
    const stat = statSync(fullPath)
    if (stat.isDirectory()) {
      results.push(...walkDir(fullPath, exts))
    } else if (exts.includes(extname(entry))) {
      results.push(fullPath)
    }
  }
  return results
}

/** 获取所有 Vue 文件 */
const vueFiles = walkDir(SRC_DIR, ['.vue'])

/** 获取所有 TS 文件（排除 .d.ts） */
const tsFiles = walkDir(SRC_DIR, ['.ts']).filter((f) => !f.endsWith('.d.ts'))

function relPath(file: string): string {
  return relative(SRC_DIR, file).replace(/\\/g, '/')
}

/** 跳过注释行 */
function isCommentLine(line: string): boolean {
  const t = line.trimStart()
  return t.startsWith('//') || t.startsWith('*') || t.startsWith('/*')
}

/** 从文件内容中提取所有 import 路径 */
function extractImports(content: string): string[] {
  const imports: string[] = []
  const importPattern = /import\s+(?:.+?\s+from\s+)?['"]([^'"]+)['"]/g
  let match: RegExpExecArray | null
  while ((match = importPattern.exec(content)) !== null) {
    imports.push(match[1].replace(/\\/g, '/'))
  }
  return imports
}

/** 将 @/ 前缀的 import 路径解析为相对 src/ 的路径 */
function resolveAliasPath(importPath: string): string {
  if (importPath.startsWith('@/')) {
    return importPath.slice(2) // 去掉 @/ 前缀
  }
  return importPath
}

describe('AC-ARCH-FE-* 架构约束', () => {
  // ── AC-ARCH-FE-01: 禁止 any 类型（含泛型场景）──────────────────────
  it('AC-ARCH-FE-01: 禁止使用 any 类型（含 Array<any>/Promise<any>）', () => {
    const violations: string[] = []
    // 覆盖：: any / : any[] / as any / <any> / Array<any> / Promise<any> / Record<string, any>
    const patterns = [
      /:\s*any\b/,
      /\bas\s+any\b/,
      /<any>/,
      /Array<any>/,
      /Promise<any>/,
      /Record<[^>]*,\s*any>/,
    ]

    for (const file of tsFiles) {
      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        // ponytail: 允许显式标注的例外（行尾含 ponytail:allow-any）
        if (line.includes('ponytail:allow-any')) continue
        for (const p of patterns) {
          if (p.test(line)) {
            violations.push(`${relPath(file)}:${i + 1}: ${line.trim()}`)
            break
          }
        }
      }
    }

    // .vue 文件中的 <script setup> 段也扫描
    for (const file of vueFiles) {
      const content = readFileSync(file, 'utf-8')
      const scriptMatch = content.match(/<script[^>]*>([\s\S]*?)<\/script>/)
      if (!scriptMatch) continue
      const script = scriptMatch[1]
      const lines = script.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        if (line.includes('ponytail:allow-any')) continue
        for (const p of patterns) {
          if (p.test(line)) {
            violations.push(`${relPath(file)}:script:${i + 1}: ${line.trim()}`)
            break
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-01 失败：发现 ${violations.length} 处 any 类型:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-02: Vue 模板禁止静态内联 style ────────────────────────
  it('AC-ARCH-FE-02: Vue 模板中禁止静态内联 style 属性（允许 :style 动态绑定）', () => {
    const violations: string[] = []
    // 只匹配静态 style="..."，不匹配 Vue 动态绑定 :style="..."
    const staticStylePattern = /\s(?<!:)style\s*=\s*["'{]/

    for (const file of vueFiles) {
      if (file.includes(`${sep}dev${sep}`)) continue
      const content = readFileSync(file, 'utf-8')
      const templateMatch = content.match(/<template>([\s\S]*?)<\/template>/)
      if (!templateMatch) continue
      const template = templateMatch[1]
      const lines = template.split('\n')
      for (let i = 0; i < lines.length; i++) {
        if (staticStylePattern.test(lines[i])) {
          violations.push(`${relPath(file)}:template:${i + 1}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-02 失败：发现 ${violations.length} 处静态内联 style:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-03: 禁止直接 DOM 操作 ────────────────────────────────
  it('AC-ARCH-FE-03: 禁止直接 DOM 操作（document.querySelector/$()/.innerHTML=）', () => {
    const violations: string[] = []
    const domPatterns = [
      /document\.(querySelector|getElementById|getElementsBy|createElement)/,
      /\$\s*\(/, // jQuery
      /\.innerHTML\s*=/,
      /\.outerHTML\s*=/,
    ]

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      // 豁免：sanitize-html 是富文本 XSS 消毒器，DOM 解析/重建是其核心职责（安全关键代码）。
      if (rel.startsWith('shared/utils/sanitize-html.ts')) continue
      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        for (const pattern of domPatterns) {
          if (pattern.test(line)) {
            violations.push(`${rel}:${i + 1}: ${line.trim()}`)
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-03 失败：发现 ${violations.length} 处直接 DOM 操作:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-04: Vue 组件必须使用 <script setup> ───────────────────
  it('AC-ARCH-FE-04: Vue 组件必须使用 <script setup>', () => {
    const violations: string[] = []
    for (const file of vueFiles) {
      const content = readFileSync(file, 'utf-8')
      const hasScript = content.includes('<script')
      const hasScriptSetup = content.includes('<script setup')
      // 有 script 标签但不是 setup 模式 = 违规
      if (hasScript && !hasScriptSetup) {
        violations.push(relPath(file))
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-04 失败：以下组件未使用 <script setup>:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-05: 双 SPA 跨端隔离（全目录覆盖）─────────────────────
  it('AC-ARCH-FE-05: 双 SPA 跨端隔离（chat/* 不能 import staff/*，反之亦然）', () => {
    const violations: string[] = []
    // 检查范围：所有 .ts 和 .vue 文件
    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')
      const importPattern = /import\s+.+from\s+['"]([^'"]+)['"]/g
      let match: RegExpExecArray | null

      while ((match = importPattern.exec(content)) !== null) {
        const importPath = match[1].replace(/\\/g, '/')
        // chat 任何文件不能 import staff 任何文件
        if (rel.startsWith('chat/')) {
          if (importPath.includes('staff/') || importPath.includes('@/staff/')) {
            violations.push(`${rel} imports ${importPath}`)
          }
        }
        // staff 任何文件不能 import chat 任何文件（除 shared 外）
        if (rel.startsWith('staff/')) {
          if (importPath.includes('chat/') || importPath.includes('@/chat/')) {
            violations.push(`${rel} imports ${importPath}`)
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-05 失败：发现跨端导入:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-06: 视图禁止直接 fetch/axios ──────────────────────────
  it('AC-ARCH-FE-06: 视图中禁止直接 fetch/axios 调用（必须通过 shared/api）', () => {
    const violations: string[] = []
    const directFetchPattern = /\b(fetch|axios)\s*\(/

    for (const file of vueFiles) {
      const rel = relPath(file)
      // 只检查 views 和 components
      if (!rel.includes('views') && !rel.includes('components')) continue
      // 跳过 shared 组件（如 DsLoading 可能需要 fetch）
      if (rel.startsWith('shared/')) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        if (directFetchPattern.test(line)) {
          violations.push(`${rel}:${i + 1}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-06 失败：发现直接 fetch 调用（应通过 shared/api）:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-07: 死代码黑名单 ──────────────────────────────────────
  it('AC-ARCH-FE-07: 死代码黑名单（design §3.1/§3.2 已删除文件禁止再 import）', () => {
    const violations: string[] = []
    // design §3.1 删除的 API 文件
    const BLACKLISTED_API = ['api/care', 'api/bind', 'api/stats', 'api/staff', 'api/review']
    // design §3.2 删除的类型文件
    const BLACKLISTED_TYPES = ['types/care', 'types/review', 'types/staff']
    // design §2.1/§2.2 删除的视图文件
    const BLACKLISTED_VIEWS = [
      'chat/views/PhoneLogin',
      'chat/views/PasswordReset',
      'chat/views/TestAccounts',
      'chat/views/HealthRecords',
      'chat/views/LabTestDetail',
      'chat/views/ExamReport',
      'chat/views/VitalSignsTrend',
      'chat/views/PatientBind',
      'chat/views/ProfileEdit',
      'chat/views/ProfileSettings',
      'staff/views/PatientList',
      'staff/views/PatientDetail',
      'staff/views/BoundPatientList',
      'staff/views/BoundPatients',
      'staff/views/StaffStats',
      'staff/views/FeedbackReview',
    ]
    // design §3.4 删除的 composables
    const BLACKLISTED_COMPOSABLES = ['useSMSCode', 'useLocalAttachments', 'useVoiceInput']

    // 实际文件存在性检查（确保真的删了）
    for (const api of BLACKLISTED_API) {
      const fullPath = resolve(SRC_DIR, 'shared', `${api}.ts`)
      if (existsSync(fullPath)) {
        violations.push(`文件应已删除但仍存在：shared/${api}.ts`)
      }
    }
    for (const type of BLACKLISTED_TYPES) {
      const fullPath = resolve(SRC_DIR, 'shared', `${type}.ts`)
      if (existsSync(fullPath)) {
        violations.push(`文件应已删除但仍存在：shared/${type}.ts`)
      }
    }
    for (const view of BLACKLISTED_VIEWS) {
      const fullPath = resolve(SRC_DIR, `${view}.vue`)
      if (existsSync(fullPath)) {
        violations.push(`文件应已删除但仍存在：${view}.vue`)
      }
    }
    for (const comp of BLACKLISTED_COMPOSABLES) {
      const fullPath = resolve(SRC_DIR, 'chat/composables', `${comp}.ts`)
      if (existsSync(fullPath)) {
        violations.push(`文件应已删除但仍存在：chat/composables/${comp}.ts`)
      }
    }

    // import 检查：禁止任何文件 import 这些黑名单模块
    const allBlacklistPatterns = [
      ...BLACKLISTED_API.map((p) => `shared/${p}`),
      ...BLACKLISTED_TYPES.map((p) => `shared/${p}`),
      ...BLACKLISTED_VIEWS,
      ...BLACKLISTED_COMPOSABLES.map((p) => `composables/${p}`),
    ]

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')
      const importPattern = /import\s+.+from\s+['"]([^'"]+)['"]/g
      let match: RegExpExecArray | null

      while ((match = importPattern.exec(content)) !== null) {
        const importPath = match[1].replace(/\\/g, '/')
        for (const banned of allBlacklistPatterns) {
          // 匹配 '@/shared/api/care'、'./api/care'、'@/chat/views/PhoneLogin' 等
          if (importPath.includes(banned) || importPath.endsWith(banned.split('/').pop()!)) {
            // 严格匹配：完整路径段
            const normalizedBanned = banned.replace(/\//g, '/')
            if (importPath.includes(normalizedBanned)) {
              violations.push(`${rel} imports ${importPath} (banned: ${banned})`)
              break
            }
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-07 失败：发现 ${violations.length} 处死代码引用:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-08: 管理员路由守卫完整性 ──────────────────────────────
  it('AC-ARCH-FE-08: 所有 path 含 config/ 的 staff 路由必须挂 adminRouteGuard', () => {
    const violations: string[] = []
    const routerPath = resolve(SRC_DIR, 'staff/router/index.ts')
    if (!existsSync(routerPath)) return

    const content = readFileSync(routerPath, 'utf-8')
    // 简化解析：查找每个 route 对象，判断 path 是否含 config/ 且 beforeEnter 是否为 adminRouteGuard
    // 用正则切分顶层 route 对象（{ ... }）
    const routePattern = /\{[\s\S]*?path:\s*['"]([^'"]+)['"][\s\S]*?\}/g
    let match: RegExpExecArray | null

    while ((match = routePattern.exec(content)) !== null) {
      const fullBlock = match[0]
      const path = match[1]
      if (!path.includes('config/')) continue

      // 检查 beforeEnter 是否为 adminRouteGuard 或 superAdminRouteGuard（后者是前者的超集）
      const beforeEnterMatch = fullBlock.match(/beforeEnter:\s*(\w+)/)
      if (!beforeEnterMatch || (beforeEnterMatch[1] !== 'adminRouteGuard' && beforeEnterMatch[1] !== 'superAdminRouteGuard')) {
        violations.push(`path="${path}" 缺少 beforeEnter: adminRouteGuard`)
      }
    }

    // 同时检查 adminRouteGuard 已导入
    if (!content.includes('adminRouteGuard')) {
      violations.push('staff/router/index.ts 未导入 adminRouteGuard')
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-08 失败：${violations.length} 处路由守卫缺失:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-09: 角色字面量强制 ────────────────────────────────────
  it('AC-ARCH-FE-09: 角色字面量必须用常量（禁止硬编码）', () => {
    const violations: string[] = []
    // 禁止字面量 'SUPER_ADMIN'/'DEPT_ADMIN'/'DOCTOR'/'NURSE'/'PATIENT'
    const roleLiterals = ["'SUPER_ADMIN'", "'DEPT_ADMIN'", "'DOCTOR'", "'NURSE'", "'PATIENT'"]
    // 豁免文件：常量定义本身、route-guard（含类型断言）、types/auth.ts（含 role 字段定义）、tests
    const exemptFiles = [
      'shared/utils/route-guard.ts',
      'shared/constants/roles.ts',
      'shared/types/auth.ts',
      'tests/',
    ]

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      // 豁免
      if (exemptFiles.some((ex) => rel.startsWith(ex))) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        for (const lit of roleLiterals) {
          if (line.includes(lit)) {
            violations.push(`${rel}:${i + 1}: ${line.trim()} (使用 ${lit})`)
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-09 失败：发现 ${violations.length} 处角色字面量硬编码（应使用 ADMIN_ROLES / PORTAL_PATIENT / PORTAL_STAFF 常量）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-10: 禁止 console.log/debug/info 调试输出 ──────────────
  it('AC-ARCH-FE-10: 禁止 console.log/debug/info 调试输出（允许 warn/error）', () => {
    const violations: string[] = []
    // 禁止 console.log / console.debug / console.info（允许 console.warn / console.error）
    const consolePattern = /console\.(log|debug|info)\s*\(/

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      // 豁免：测试文件本身
      if (rel.startsWith('tests/')) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        if (consolePattern.test(line)) {
          violations.push(`${rel}:${i + 1}: ${line.trim()}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-10 失败：发现 ${violations.length} 处 console 调试输出:\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-11: 禁止组件间 prop 传递超过两层 ──────────────────────
  it('AC-ARCH-FE-11: 禁止组件间 prop 传递超过两层（CLAUDE.md 硬性规则）', () => {
    const violations: string[] = []

    // 构建组件 prop 图：收集每个组件的 defineProps 名称
    const componentProps = new Map<string, Set<string>>() // relPath → prop names
    const componentImports = new Map<string, Map<string, string>>() // relPath → (localName → importPath)

    for (const file of vueFiles) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')

      // 提取 defineProps 中的 prop 名称
      const propsMatch = content.match(/defineProps<(\w+)>|defineProps\s*\(\s*\{([^}]+)\}/)
      if (propsMatch) {
        const props = new Set<string>()
        if (propsMatch[2]) {
          // 对象形式：{ title: string, count: number }
          const propNames = propsMatch[2].match(/(\w+)\s*[?:]/g)
          if (propNames) {
            for (const p of propNames) {
              props.add(p.replace(/[?:]/g, '').trim())
            }
          }
        }
        // 类型引用形式暂不深入解析（需要跨文件追踪类型定义）
        componentProps.set(rel, props)
      }

      // 提取组件 import
      const imports = new Map<string, string>()
      const importPattern = /import\s+(\w+)\s+from\s+['"]([^'"]+\.vue)['"]/g
      let match: RegExpExecArray | null
      while ((match = importPattern.exec(content)) !== null) {
        imports.set(match[1], match[2])
      }
      // 也检查 components 目录的 import
      const compImportPattern = /import\s+\{([^}]+)\}\s+from\s+['"](@\/shared\/components)['"]/g
      while ((match = compImportPattern.exec(content)) !== null) {
        const names = match[1].split(',').map((n) => n.trim())
        for (const name of names) {
          imports.set(name, match[2])
        }
      }
      componentImports.set(rel, imports)
    }

    // 检测 prop 传递链：如果 A 组件把同名 prop 传给 B 组件，且 B 也 defineProps 了同名 prop
    // 这意味着 A 的 prop 被"透传"给 B，形成 prop 链
    for (const [rel, content] of vueFiles.map((f) => [relPath(f), readFileSync(f, 'utf-8')] as const)) {
      const myProps = componentProps.get(rel)
      if (!myProps || myProps.size === 0) continue

      // 在模板中查找 :propName="propName" 模式（同名透传）
      const templateMatch = content.match(/<template>([\s\S]*?)<\/template>/)
      if (!templateMatch) continue
      const template = templateMatch[1]

      for (const prop of myProps) {
        // 匹配 :propName="propName" 或 v-bind:propName="propName"
        const passThroughPattern = new RegExp(
          `(?:v-bind:|:)${prop}\\s*=\\s*["']${prop}["']`,
        )
        if (passThroughPattern.test(template)) {
          violations.push(`${rel}: prop '${prop}' 被同名透传给子组件（超过 2 层 prop 传递风险）`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-11 失败：发现 ${violations.length} 处 prop 同名透传（应使用 Pinia store 或 provide/inject）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-12: 死代码检测扩展 ────────────────────────────────────
  it('AC-ARCH-FE-12: 死代码检测扩展（未被引用的导出函数/类型/组件）', () => {
    const violations: string[] = []

    // 收集所有 export 的名称和定义位置
    const exportedNames = new Map<string, { file: string; type: 'function' | 'type' | 'component' }>()

    // 1. 从 TS 文件收集 export
    for (const file of tsFiles) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')

      // export function / export async function
      const funcPattern = /export\s+(?:async\s+)?function\s+(\w+)/g
      let match: RegExpExecArray | null
      while ((match = funcPattern.exec(content)) !== null) {
        exportedNames.set(match[1], { file: rel, type: 'function' })
      }

      // export const / export let / export var
      const constPattern = /export\s+(?:const|let|var)\s+(\w+)/g
      while ((match = constPattern.exec(content)) !== null) {
        exportedNames.set(match[1], { file: rel, type: 'function' })
      }

      // export type / export interface
      const typePattern = /export\s+(?:type|interface)\s+(\w+)/g
      while ((match = typePattern.exec(content)) !== null) {
        exportedNames.set(match[1], { file: rel, type: 'type' })
      }

      // export enum
      const enumPattern = /export\s+enum\s+(\w+)/g
      while ((match = enumPattern.exec(content)) !== null) {
        exportedNames.set(match[1], { file: rel, type: 'type' })
      }
    }

    // 2. 从 Vue 文件收集组件名（文件名即组件名）
    for (const file of vueFiles) {
      const rel = relPath(file)
      const basename = rel.split('/').pop()!.replace('.vue', '')
      // 仅 shared/components 下的组件需要检测是否被引用
      if (rel.startsWith('shared/components/')) {
        exportedNames.set(basename, { file: rel, type: 'component' })
      }
    }

    // 3. 收集所有文件中的引用（import 名称、模板中使用组件名、命名空间属性访问）
    const referencedNames = new Set<string>()

    for (const file of [...tsFiles, ...vueFiles]) {
      const content = readFileSync(file, 'utf-8')

      // import { X, Y, Z } from '...'
      const namedImportPattern = /import\s+\{([^}]+)\}/g
      let match: RegExpExecArray | null
      while ((match = namedImportPattern.exec(content)) !== null) {
        const names = match[1].split(',').map((n) => {
          let name = n.trim()
          // 处理 type-only import：type X → 取 X
          if (name.startsWith('type ')) name = name.slice(5).trim()
          // 处理 as 别名：X as Y → 取 X
          const parts = name.split(/\s+as\s+/)
          return parts[0].trim()
        })
        for (const name of names) {
          if (name) referencedNames.add(name)
        }
      }

      // import X from '...'
      const defaultImportPattern = /import\s+(\w+)\s+from\s+/g
      while ((match = defaultImportPattern.exec(content)) !== null) {
        referencedNames.add(match[1])
      }

      // import * as X from '...'
      const namespaceImportPattern = /import\s+\*\s+as\s+(\w+)\s+from\s+/g
      while ((match = namespaceImportPattern.exec(content)) !== null) {
        referencedNames.add(match[1])
      }
    }

    // 4. 桶文件 re-export 的名称视为"被引用"（外部通过桶文件命名空间访问）
    // 解析 "export * as xxxApi from '...'" 模式，收集每个命名空间下导出的所有名称
    const barrelFile = resolve(SRC_DIR, 'shared/index.ts')
    if (existsSync(barrelFile)) {
      const barrelContent = readFileSync(barrelFile, 'utf-8')
      const barrelDir = resolve(SRC_DIR, 'shared')

      // 提取 export * as xxxApi from '...' 中的 xxxApi
      const nsExportPattern = /export\s+\*\s+as\s+(\w+)\s+from\s+['"]([^'"]+)['"]/g
      let nsMatch: RegExpExecArray | null
      while ((nsMatch = nsExportPattern.exec(barrelContent)) !== null) {
        const nsName = nsMatch[1]
        const nsModulePath = nsMatch[2]

        // 找到该模块所有 export 的名称，将它们标记为"被引用"
        // 因为外部通过 xxxApi.functionName() 访问
        const resolvedPath = nsModulePath.startsWith('@/') ? nsModulePath.slice(2) : null
        let fullModulePath: string | null = null
        if (resolvedPath) {
          fullModulePath = resolve(SRC_DIR, resolvedPath + '.ts')
        } else {
          // 相对路径：相对于桶文件目录解析
          fullModulePath = resolve(barrelDir, nsModulePath + '.ts')
        }
        if (fullModulePath && existsSync(fullModulePath)) {
          const moduleContent = readFileSync(fullModulePath, 'utf-8')
          const exportPatterns = [
            /export\s+(?:async\s+)?function\s+(\w+)/g,
            /export\s+(?:const|let|var)\s+(\w+)/g,
            /export\s+(?:type|interface)\s+(\w+)/g,
            /export\s+enum\s+(\w+)/g,
          ]
          for (const p of exportPatterns) {
            let m: RegExpExecArray | null
            while ((m = p.exec(moduleContent)) !== null) {
              referencedNames.add(m[1])
            }
          }
        }

        // 命名空间本身也是引用名
        referencedNames.add(nsName)
      }

      // 处理 export { X, Y } / export type { X } / export { RAG_LIMITS } 等具名 re-export
      const namedExportPattern = /export\s+(?:type\s+)?\{([^}]+)\}/g
      let namedMatch: RegExpExecArray | null
      while ((namedMatch = namedExportPattern.exec(barrelContent)) !== null) {
        const names = namedMatch[1].split(',').map((n) => {
          const parts = n.trim().split(/\s+as\s+/)
          return parts[0].trim()
        })
        for (const name of names) {
          if (name) referencedNames.add(name)
        }
      }
    }

    // 5. 检测未被引用的导出
    // 豁免：桶文件（index.ts）的 re-export、测试文件
    const exemptFiles = [
      'shared/index.ts', // 桶文件：re-export 不算死代码
      'tests/', // 测试文件
    ]

    // 组件名在模板中使用（PascalCase 或 kebab-case）
    for (const file of vueFiles) {
      const content = readFileSync(file, 'utf-8')
      const templateMatch = content.match(/<template>([\s\S]*?)<\/template>/)
      if (!templateMatch) continue
      const template = templateMatch[1]

      // 匹配 <ComponentName 或 <component-name
      const componentUsePattern = /<([A-Z][A-Za-z0-9]*)[\s/>]/g
      let match: RegExpExecArray | null
      while ((match = componentUsePattern.exec(template)) !== null) {
        referencedNames.add(match[1])
      }
    }

    for (const [name, { file, type }] of exportedNames) {
      if (exemptFiles.some((ex) => file.startsWith(ex))) continue
      // 豁免：桶文件本身
      if (file.endsWith('index.ts')) continue
      // 豁免：main.ts 入口文件
      if (file.endsWith('main.ts')) continue
      // 豁免：router 文件（路由配置导出 router 实例）
      if (file.includes('router/')) continue
      // 豁免：Vite 环境入口
      if (file.includes('env')) continue

      if (!referencedNames.has(name)) {
        const label = type === 'component' ? '组件' : type === 'type' ? '类型' : '函数'
        violations.push(`${file}: ${label} '${name}' 已导出但未被任何文件引用`)
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-12 失败：发现 ${violations.length} 处死代码（CLAUDE.md: 生产代码不得存在死代码）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-13: tsconfig strict 模式强制 ──────────────────────────
  it('AC-ARCH-FE-13: tsconfig.app.json 必须启用 strict 模式', () => {
    const tsconfigPath = resolve(ROOT_DIR, 'tsconfig.app.json')
    if (!existsSync(tsconfigPath)) return

    const content = readFileSync(tsconfigPath, 'utf-8')
    // 简单检查 strict: true 是否存在（JSON 中可能是 "strict": true）
    if (!/["']strict["']\s*:\s*true/.test(content)) {
      throw new Error(
        'AC-ARCH-FE-13 失败：tsconfig.app.json 未启用 strict: true\n  CLAUDE.md 硬性规则：前端使用 TypeScript strict 模式',
      )
    }
  })

  // ── AC-ARCH-FE-14: 禁止 window.prompt/alert/confirm 命令式对话框 ────
  it('AC-ARCH-FE-14: 禁止 window.prompt/alert/confirm 命令式对话框（应使用 Vant Dialog）', () => {
    const violations: string[] = []
    const dialogPattern = /window\.(prompt|alert|confirm)\s*\(/

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      // 豁免：测试文件
      if (rel.startsWith('tests/')) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        if (dialogPattern.test(line)) {
          violations.push(`${rel}:${i + 1}: ${line.trim()}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-14 失败：发现 ${violations.length} 处命令式对话框（应使用 Vant showDialog/showConfirmDialog）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-15: 禁止 window.location 直接赋值 ────────────────────
  it('AC-ARCH-FE-15: 禁止 window.location 直接赋值（应使用 Vue Router 或 ponytail:allow-location 标注）', () => {
    const violations: string[] = []
    // 匹配 window.location.href = ... 或 window.location = ... 或 window.location.assign(...)
    const locationPattern = /window\.location\.(href\s*=|assign\s*\()/

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      // 豁免：测试文件
      if (rel.startsWith('tests/')) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        // ponytail: 允许显式标注的例外（标注可在当前行或前一行注释中）
        if (line.includes('ponytail:allow-location')) continue
        if (i > 0 && lines[i - 1].includes('ponytail:allow-location')) continue
        if (locationPattern.test(line)) {
          violations.push(`${rel}:${i + 1}: ${line.trim()}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-15 失败：发现 ${violations.length} 处 window.location 直接赋值（应使用 Vue Router；跨 MPA 跳转需标注 ponytail:allow-location）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-16: <style scoped> 非 Tailwind 规则检测 ───────────────
  it('AC-ARCH-FE-16: <style scoped> 块中禁止非 CSS 变量覆盖的自定义规则', () => {
    const violations: string[] = []

    // 允许的模式：
    // 1. CSS 变量覆盖（--van-xxx、--ax-xxx 等）
    // 2. :deep() / :global() 选择器（Vant 组件样式穿透）
    // 3. @keyframes
    // 4. @media 查询
    // 5. 纯工具类（如 .no-scrollbar 隐藏滚动条）
    // 6. Vant 组件变量覆盖类（如 .reg-field { --van-xxx: value }）
    // 7. ponytail:allow-scoped-css 标注的 <style> 块整体豁免
    const allowedClassPatterns = [
      /^\.(no-scrollbar|markdown-body|prose-article)/, // 已知工具类 / :deep() 前缀类
    ]

    for (const file of vueFiles) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')

      // 检查 <style> 标签是否有 ponytail:allow-scoped-css 标注
      const styleTagMatch = content.match(/<style[^>]*>/)
      if (styleTagMatch && styleTagMatch[0].includes('ponytail:allow-scoped-css')) continue

      const styleMatch = content.match(/<style[^>]*>([\s\S]*?)<\/style>/)
      if (!styleMatch) continue

      const styleContent = styleMatch[1]
      const lines = styleContent.split('\n')
      let inAllowedBlock = false
      let braceDepth = 0
      let currentClassIsVarOverride = false

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        const trimmed = line.trim()

        // 跳过空行和注释
        if (!trimmed || trimmed.startsWith('/*') || trimmed.startsWith('*') || trimmed.startsWith('//')) continue

        // 允许的顶层结构
        if (trimmed.startsWith('@keyframes') || trimmed.startsWith('@media')) {
          inAllowedBlock = true
        }
        if (trimmed.startsWith(':deep(') || trimmed.startsWith(':global(')) continue
        if (trimmed.startsWith('--')) continue // CSS 变量声明

        // 追踪花括号深度
        const openBraces = (line.match(/\{/g) || []).length
        const closeBraces = (line.match(/\}/g) || []).length
        braceDepth += openBraces
        braceDepth -= closeBraces

        // 检测类规则块结束
        if (currentClassIsVarOverride && braceDepth <= 0) {
          currentClassIsVarOverride = false
        }

        if (inAllowedBlock && braceDepth <= 0) {
          inAllowedBlock = false
          continue
        }
        if (inAllowedBlock) continue

        // 检测自定义 class 规则（.xxx { ... }）
        const classRuleMatch = trimmed.match(/^\.([\w-]+)\s*\{/)
        if (classRuleMatch) {
          const className = classRuleMatch[1]
          // 跳过允许的模式
          if (allowedClassPatterns.some((p) => p.test(`.${className}`))) continue
          // 检查这个类规则块是否只包含 CSS 变量覆盖
          // 向前扫描到闭合花括号，检查是否所有属性都是 --var: value 形式
          const blockLines: string[] = []
          let scanDepth = braceDepth
          for (let j = i + 1; j < lines.length && scanDepth > 0; j++) {
            const scanLine = lines[j].trim()
            scanDepth += (lines[j].match(/\{/g) || []).length
            scanDepth -= (lines[j].match(/\}/g) || []).length
            if (scanLine && !scanLine.startsWith('/*') && !scanLine.startsWith('*') && !scanLine.startsWith('//') && scanLine !== '}') {
              blockLines.push(scanLine)
            }
          }
          const isOnlyVarOverrides = blockLines.every((bl) => bl.startsWith('--'))
          if (isOnlyVarOverrides) {
            currentClassIsVarOverride = true
            continue
          }
          violations.push(`${rel}:style:${i + 1}: .${className}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-16 失败：发现 ${violations.length} 处非 Tailwind 自定义 CSS 规则（应使用 Tailwind 原子类或 CSS 变量覆盖；复杂视觉效果可标注 ponytail:allow-scoped-css）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-17: Portal 字面量强制 ─────────────────────────────────
  it('AC-ARCH-FE-17: Portal 字面量强制（\'staff\'/\'patient\' 必须用 PORTAL_STAFF/PORTAL_PATIENT 常量）', () => {
    const violations: string[] = []
    // 在路由 meta 和业务逻辑中禁止 'staff'/'patient' 作为 Portal 字面量
    // 检测模式：requiredRole: 'staff' / requiredRole: 'patient' / portal === 'staff' 等
    const portalLiteralPatterns = [
      /requiredRole\s*:\s*['"]staff['"]/,
      /requiredRole\s*:\s*['"]patient['"]/,
      /===?\s*['"]staff['"]/,
      /===?\s*['"]patient['"]/,
      /['"]staff['"]\s*===?/,
      /['"]patient['"]\s*===?/,
    ]

    // 豁免文件：常量定义本身、类型定义、route-guard（含角色判断逻辑）、tests
    const exemptFiles = [
      'shared/constants/roles.ts',
      'shared/types/auth.ts',
      'shared/utils/route-guard.ts',
      'shared/api/client.ts', // determineLoginPath 使用 pathname 判断
      'tests/',
    ]

    for (const file of [...tsFiles, ...vueFiles]) {
      const rel = relPath(file)
      if (exemptFiles.some((ex) => rel.startsWith(ex))) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        for (const pattern of portalLiteralPatterns) {
          if (pattern.test(line)) {
            violations.push(`${rel}:${i + 1}: ${line.trim()}`)
            break
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-17 失败：发现 ${violations.length} 处 Portal 字面量硬编码（应使用 PORTAL_STAFF / PORTAL_PATIENT 常量）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-18: 分页类型统一 ──────────────────────────────────────
  it('AC-ARCH-FE-18: 分页类型统一（Paginated 只能在 shared/types/ 中定义一次）', () => {
    const violations: string[] = []

    // 查找所有定义了 Paginated 或 PaginatedResponse 的文件
    const paginatedDefPattern = /(?:interface|type)\s+Paginated(?:Response)?\s*[<{]/
    let definitionCount = 0
    const definitionFiles: string[] = []

    for (const file of tsFiles) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')
      if (paginatedDefPattern.test(content)) {
        definitionCount++
        definitionFiles.push(rel)
      }
    }

    // 允许的定义位置：shared/types/ 目录下
    const allowedDir = 'shared/types/'
    for (const defFile of definitionFiles) {
      if (!defFile.startsWith(allowedDir)) {
        violations.push(`${defFile}: Paginated 类型定义不在 ${allowedDir} 中（应在 shared/types/ 统一定义）`)
      }
    }

    // 如果有多个定义，即使都在 shared/types/ 下也报错
    const typeDirDefinitions = definitionFiles.filter((f) => f.startsWith(allowedDir))
    if (typeDirDefinitions.length > 1) {
      violations.push(
        `Paginated 类型在 shared/types/ 下重复定义：${typeDirDefinitions.join(', ')}（应只保留一处定义）`,
      )
    }

    if (violations.length > 0) {
      throw new Error(`AC-ARCH-FE-18 失败：\n${violations.join('\n')}`)
    }
  })

  // ── AC-ARCH-FE-19: 路由跨端 import 检测 ──────────────────────────────
  it('AC-ARCH-FE-19: 路由文件禁止跨端 import 视图（staff 路由禁止 import chat 视图）', () => {
    const violations: string[] = []

    // 检查 staff/router 中的 import 是否引用了 chat/ 下的视图
    const staffRouterPath = resolve(SRC_DIR, 'staff/router')
    if (!existsSync(staffRouterPath)) return

    const routerFiles = walkDir(staffRouterPath, ['.ts'])
    for (const file of routerFiles) {
      const content = readFileSync(file, 'utf-8')
      const importPattern = /import\s+.+from\s+['"]([^'"]+)['"]/g
      let match: RegExpExecArray | null

      while ((match = importPattern.exec(content)) !== null) {
        const importPath = match[1].replace(/\\/g, '/')
        // staff 路由 import chat 视图
        if (importPath.includes('@/chat/') || importPath.includes('chat/views/')) {
          violations.push(`${relPath(file)} imports ${importPath}（staff 路由禁止 import chat 视图）`)
        }
      }
    }

    // 同理检查 chat/router
    const chatRouterPath = resolve(SRC_DIR, 'chat/router')
    if (existsSync(chatRouterPath)) {
      const chatRouterFiles = walkDir(chatRouterPath, ['.ts'])
      for (const file of chatRouterFiles) {
        const content = readFileSync(file, 'utf-8')
        const importPattern = /import\s+.+from\s+['"]([^'"]+)['"]/g
        let match: RegExpExecArray | null

        while ((match = importPattern.exec(content)) !== null) {
          const importPath = match[1].replace(/\\/g, '/')
          if (importPath.includes('@/staff/') || importPath.includes('staff/views/')) {
            violations.push(`${relPath(file)} imports ${importPath}（chat 路由禁止 import staff 视图）`)
          }
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-19 失败：发现 ${violations.length} 处路由跨端 import（应在各自端创建独立视图或移至 shared/）:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-20: v-html 使用审计 ───────────────────────────────────
  it('AC-ARCH-FE-20: v-html 使用审计（仅允许白名单组件使用）', () => {
    const violations: string[] = []

    // 白名单：允许使用 v-html 的组件（markdown 渲染等已审计场景）
    const VHTML_ALLOWLIST = [
      'chat/views/ArticleDetail.vue', // markdown-it 渲染文章正文
    ]

    for (const file of vueFiles) {
      const rel = relPath(file)
      const content = readFileSync(file, 'utf-8')

      // 检查模板中是否有 v-html
      const templateMatch = content.match(/<template>([\s\S]*?)<\/template>/)
      if (!templateMatch) continue

      if (/v-html\s*=/.test(templateMatch[1])) {
        if (!VHTML_ALLOWLIST.includes(rel)) {
          violations.push(
            `${rel}: 使用了 v-html（XSS 风险；如需使用请加入白名单并确保内容经过 sanitize）`,
          )
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-20 失败：发现 ${violations.length} 处未审计的 v-html 使用:\n${violations.join('\n')}`,
      )
    }
  })

  // ── AC-ARCH-FE-21: 禁止外部 CDN 资源引用 ────────────────────────────
  it('AC-ARCH-FE-21: 禁止使用外部 CDN 资源（必须使用本地资源）', () => {
    const violations: string[] = []
    const cdnPattern = /url\(\s*['"]?https?:\/\/[^)]*\)/

    const cssFiles = walkDir(SRC_DIR, ['.css'])

    for (const file of [...tsFiles, ...vueFiles, ...cssFiles]) {
      const rel = relPath(file)
      if (rel.startsWith('tests/')) continue

      const content = readFileSync(file, 'utf-8')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (isCommentLine(line)) continue
        if (cdnPattern.test(line)) {
          violations.push(`${rel}:${i + 1}: ${line.trim()}`)
        }
      }
    }

    if (violations.length > 0) {
      throw new Error(
        `AC-ARCH-FE-21 失败：发现 ${violations.length} 处外部 CDN 引用（违反 CLAUDE.md 硬性规则第 8 条：禁止使用 CDN）:\n${violations.join('\n')}`,
      )
    }
  })
})
