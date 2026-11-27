import { get } from 'svelte/store'
import { beforeEach, describe, expect, it } from 'vitest'
import { createRouter, match, pathOf } from './router'

describe('pathOf', () => {
  it('берёт путь из #', () => {
    expect(pathOf('#/buy')).toBe('/buy')
    expect(pathOf('#/buy/')).toBe('/buy')
    expect(pathOf('#/pay/12?from=x')).toBe('/pay/12')
  })
  it('всё чужое это главная', () => {
    expect(pathOf('')).toBe('/')
    expect(pathOf('#')).toBe('/')
    expect(pathOf('#/')).toBe('/')
    expect(pathOf('#tgWebAppData=query_id%3Dx&tgWebAppVersion=8')).toBe('/')
  })
})

describe('match', () => {
  it('сверяет образец и достаёт части', () => {
    expect(match('/pay/:id', '/pay/42')).toEqual({ id: '42' })
    expect(match('/buy/:tariff/:days', '/buy/3/30')).toEqual({ tariff: '3', days: '30' })
    expect(match('/buy', '/buy')).toEqual({})
    expect(match('/', '/')).toEqual({})
  })
  it('не путает разные пути', () => {
    expect(match('/pay/:id', '/pay')).toBeNull()
    expect(match('/pay/:id', '/pay/1/2')).toBeNull()
    expect(match('/buy', '/sell')).toBeNull()
  })
  it('раскодирует части', () => {
    expect(match('/x/:a', '/x/a%20b')).toEqual({ a: 'a b' })
  })
})

describe('createRouter', () => {
  beforeEach(() => {
    window.location.hash = ''
  })

  it('следит за адресом', async () => {
    const r = createRouter(window)
    const seen: string[] = []
    const off = r.path.subscribe((p) => seen.push(p))
    r.go('/buy')
    await new Promise((res) => setTimeout(res, 0))
    expect(get(r.path)).toBe('/buy')
    expect(seen.at(-1)).toBe('/buy')
    off()
  })

  it('назад ведёт на прошлый шаг, а с первого экрана на главную', async () => {
    window.location.hash = '#/topup'
    const r = createRouter(window)
    const off = r.path.subscribe(() => {})
    r.back() // открыли сразу на /topup: истории внутри страницы нет
    await new Promise((res) => setTimeout(res, 0))
    expect(window.location.hash).toBe('#/')
    r.go('/buy')
    r.go('/buy/1')
    await new Promise((res) => setTimeout(res, 0))
    r.back()
    await new Promise((res) => setTimeout(res, 10))
    expect(pathOf(window.location.hash)).toBe('/buy')
    off()
  })

  it('canBack: на главной нельзя', () => {
    const r = createRouter(window)
    expect(r.canBack()).toBe(false)
    window.location.hash = '#/devices'
    expect(r.canBack()).toBe(true)
  })

  it('переход туда же не добавляет шаг', () => {
    const r = createRouter(window)
    r.go('/buy')
    r.go('/buy')
    expect(window.location.hash).toBe('#/buy')
  })
})
