/**
 * sanitize-html 单测：白名单放行/拦截行为。
 * 重点覆盖本次新增：img width/height（数值）、style 仅保留 text-align。
 */
import { describe, expect, it } from 'vitest'
import { sanitizeHtml } from '@/shared/utils/sanitize-html'

describe('sanitizeHtml 基础白名单', () => {
  it('保留白名单标签与文本', () => {
    expect(sanitizeHtml('<p>你好<strong>世界</strong></p>')).toBe('<p>你好<strong>世界</strong></p>')
  })

  it('剥离 script 与事件属性', () => {
    const out = sanitizeHtml('<p onclick="evil()">a</p><script>evil()</script>')
    expect(out).not.toContain('script')
    expect(out).not.toContain('onclick')
    expect(out).toContain('a')
  })

  it('拦截 javascript: 链接', () => {
    const out = sanitizeHtml('<a href="javascript:evil()">x</a>')
    expect(out).not.toContain('javascript:')
  })
})

describe('sanitizeHtml img 属性', () => {
  it('保留 src/alt', () => {
    expect(sanitizeHtml('<img src="https://a.example/x.png" alt="图">'))
      .toBe('<img src="https://a.example/x.png" alt="图">')
  })

  it('保留数值 width/height（resize 输出形态）', () => {
    const out = sanitizeHtml('<img src="/uploads/a.png" width="320" height="200">')
    expect(out).toContain('width="320"')
    expect(out).toContain('height="200"')
  })

  it('丢弃非数值 width（防御属性注入形态）', () => {
    const out = sanitizeHtml('<img src="/uploads/a.png" width="100" onerror="alert(1)">')
    expect(out).not.toContain('onerror')
  })
})

describe('sanitizeHtml style 消毒', () => {
  it('保留安全的 text-align（TextAlign 输出形态）', () => {
    const out = sanitizeHtml('<p style="text-align: center">居中</p>')
    expect(out).toContain('text-align')
    expect(out).toContain('center')
  })

  it('丢弃其他 CSS 声明（position 等危险样式）', () => {
    const out = sanitizeHtml('<p style="position:fixed;top:0;left:0;width:100%">覆盖层</p>')
    expect(out).not.toContain('position')
    expect(out).not.toContain('fixed')
  })

  it('混合声明仅保留 text-align', () => {
    const out = sanitizeHtml('<h2 style="color:red;text-align:right;margin:0">标题</h2>')
    expect(out).toMatch(/text-align\s*:\s*right/i)
    expect(out).not.toContain('color')
    expect(out).not.toContain('margin')
  })

  it('保留图片居中声明（display:block + margin auto）', () => {
    const out = sanitizeHtml(
      '<img src="/uploads/a.png" style="display:block;margin-left:auto;margin-right:auto">',
    )
    expect(out).toContain('display:block')
    expect(out).toContain('margin-left:auto')
    expect(out).toContain('margin-right:auto')
  })

  it('丢弃危险 display 与定位声明', () => {
    const out = sanitizeHtml('<img src="/uploads/a.png" style="display:none;position:fixed;margin-left:calc(1px+1px)">')
    expect(out).not.toContain('display:none')
    expect(out).not.toContain('position')
    expect(out).not.toContain('calc')
  })
})
