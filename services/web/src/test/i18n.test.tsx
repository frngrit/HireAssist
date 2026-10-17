import { describe, expect, it } from 'vitest'
import { en } from '../i18n/en'
import { th } from '../i18n/th'

describe('translations (NFR-16)', () => {
  it('keeps every placeholder in Thai that the English text uses', () => {
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      const placeholders = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort()
      expect(placeholders(th[key]), key).toEqual(placeholders(en[key]))
    }
  })
})
