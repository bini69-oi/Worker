import { describe, expect, it } from 'vitest'
import { render } from './testing/mountTexts'
import Qr from './Qr.svelte'
import { makeQr } from './qr'

describe('makeQr', () => {
  it('сторона = модули + кайма в четыре модуля с каждой стороны', () => {
    const q = makeQr('https://sub.test/abc')
    // 20 байт при уровне M это версия 2: 25 модулей
    expect(q.size).toBe(25 + 8)
  })

  it('тёмные модули лежат внутри каймы', () => {
    const q = makeQr('https://sub.test/abc')
    const cells = [...q.path.matchAll(/M(\d+) (\d+)h1v1h-1z/g)].map((m) => [Number(m[1]), Number(m[2])] as const)
    expect(cells.length).toBeGreaterThan(100)
    for (const [x, y] of cells) {
      expect(x).toBeGreaterThanOrEqual(4)
      expect(y).toBeGreaterThanOrEqual(4)
      expect(x).toBeLessThan(q.size - 4)
      expect(y).toBeLessThan(q.size - 4)
    }
    // три поисковых квадрата: левый верхний угол всегда тёмный
    expect(q.path).toContain('M4 4h1v1h-1z')
  })

  it('разный текст даёт разный код, один текст один и тот же', () => {
    expect(makeQr('a').path).not.toBe(makeQr('b').path)
    expect(makeQr('a').path).toBe(makeQr('a').path)
  })

  it('длинная ссылка подписки помещается', () => {
    const q = makeQr('https://sub.example.com/' + 'x'.repeat(200))
    expect(q.size).toBeGreaterThan(37)
  })
})

describe('Qr', () => {
  it('рисует SVG с подписью и без картинок', () => {
    const { root, unmount } = render(Qr, { value: 'https://sub.test/abc', label: 'QR' })
    const svg = root.querySelector('svg')
    expect(svg?.getAttribute('aria-label')).toBe('QR')
    expect(svg?.querySelector('path')?.getAttribute('d')).toContain('M4 4')
    expect(root.querySelector('img')).toBeNull()
    expect(root.innerHTML).not.toContain('data:')
    unmount()
  })
})
