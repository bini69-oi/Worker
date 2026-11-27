import { describe, expect, it } from 'vitest'
import type { ErrorBody } from '../types.gen'
import { golden } from './golden'

describe('эталоны api/testdata', () => {
  it('читает ответ с ошибкой как ErrorBody', () => {
    const e = golden<ErrorBody>('error_not_found')
    expect(e.code).toBe('E1001')
    expect(typeof e.text).toBe('string')
  })

  it('отказывает, если эталона нет', () => {
    expect(() => golden('нет-такого')).toThrow(/нет эталона/)
  })
})
