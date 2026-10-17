import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { en, type MessageKey } from './en'
import { th } from './th'

export type Language = 'en' | 'th'

const dictionaries: Record<Language, Record<MessageKey, string>> = { en, th }
const STORAGE_KEY = 'hireassist.language'

type Translate = (key: MessageKey, values?: Record<string, string | number>) => string

interface I18n {
  language: Language
  setLanguage: (language: Language) => void
  t: Translate
  /** Formats a date in the current language: Thai uses the Buddhist calendar. */
  formatDate: (iso: string) => string
}

const I18nContext = createContext<I18n | null>(null)

function storedLanguage(): Language {
  try {
    const value = localStorage.getItem(STORAGE_KEY)
    return value === 'th' ? 'th' : 'en'
  } catch {
    return 'en'
  }
}

export function I18nProvider({ children, initial }: { children: ReactNode; initial?: Language }) {
  const [language, setLanguageState] = useState<Language>(initial ?? storedLanguage)

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next)
    document.documentElement.lang = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Remembering the choice is a convenience; the app works without it.
    }
  }, [])

  const value = useMemo<I18n>(() => {
    const dictionary = dictionaries[language]
    const t: Translate = (key, values) =>
      dictionary[key].replace(/\{(\w+)\}/g, (_, name: string) => String(values?.[name] ?? `{${name}}`))
    const dateFormat = new Intl.DateTimeFormat(language === 'th' ? 'th-TH' : 'en-GB', { dateStyle: 'medium' })
    return { language, setLanguage, t, formatDate: (iso) => dateFormat.format(new Date(iso)) }
  }, [language, setLanguage])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18n {
  const context = useContext(I18nContext)
  if (!context) throw new Error('useI18n must be used inside <I18nProvider>')
  return context
}
