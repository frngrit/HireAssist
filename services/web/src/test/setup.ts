import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { notifications } from '@mantine/notifications'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, beforeEach } from 'vitest'
import { setSession } from '../api/client'
import { config, resetDb } from '../mocks/db'
import { handlers } from '../mocks/handlers'

// The same handlers the browser demo uses, so a passing test means the demo behaves the same.
export const server = setupServer(...handlers)

// Mantine's auto-sizing textarea listens for web fonts loading; happy-dom has no FontFaceSet.
Object.defineProperty(document, 'fonts', {
  value: { addEventListener: () => {}, removeEventListener: () => {}, ready: Promise.resolve() },
})

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
beforeEach(() => {
  config.latencyMs = 0
  config.processingMs = [0, 0]
  resetDb()
  setSession(null)
  notifications.clean() // the store is global, so one test's toasts would show in the next
  localStorage.clear() // the batches a test uploaded are remembered per browser
  window.history.pushState({}, '', '/')
})
afterEach(() => {
  cleanup()
  server.resetHandlers()
})
afterAll(() => server.close())
