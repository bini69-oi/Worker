import { describe, expect, it } from 'vitest'
import css from './style.css?inline'

// Исходники экранов читаются как текст: тест проверяет вёрстку, а не запускает экраны.
const files = import.meta.glob('./*.svelte', { query: '?raw', eager: true, import: 'default' }) as Record<string, string>
const screens = Object.entries(files).map(([name, src]) => ({ name, src }))

// Узкий экран 360 пикселей: таблицы становятся карточками, ничего не шире экрана. Сам просмотр глазами
// остаётся ручной проверкой (задача 9.2), здесь только то, что ломается тихо при правках.
describe('вёрстка для телефона', () => {
  it('таблица превращается в карточки по подписям data-label', () => {
    expect(css).toMatch(/@media \(max-width: 640px\)/)
    expect(css).toMatch(/table\.tbl td::before\s*{[^}]*content:\s*attr\(data-label\)/)
    expect(css).toMatch(/table\.tbl thead\s*{\s*display:\s*none/)
  })

  it('каждая таблица экранов имеет класс tbl', () => {
    for (const { name, src } of screens) {
      for (const m of src.matchAll(/<table([^>]*)>/g)) expect(m[1], name).toContain('class="tbl"')
    }
  })

  it('у каждой ячейки с данными есть подпись, пустые ячейки только с кнопками действий', () => {
    for (const { name, src } of screens) {
      for (const m of src.matchAll(/<td([^>]*)>([\s\S]*?)<\/td>/g)) {
        const attrs = m[1] ?? ''
        const body = m[2] ?? ''
        const actions = /<button|\{#if/.test(body)
        expect(attrs.includes('data-label') || actions, `${name}: ${m[0].slice(0, 60)}`).toBe(true)
      }
    }
  })

  it('нет жёсткой ширины больше экрана', () => {
    expect(css).not.toMatch(/min-width:\s*\d{3,}px/)
    expect(css).not.toMatch(/(?<!max-)width:\s*\d{3,}px/)
    expect(css).toMatch(/max-width:\s*100%/)
    for (const { name, src } of screens) expect(src, name).not.toMatch(/style="[^"]*width:\s*\d{3,}px/)
  })
})
