import '@mantine/core/styles.css'
import '@mantine/dropzone/styles.css'
import '@mantine/notifications/styles.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'

/** Mock mode is the default; VITE_USE_MOCKS=false talks to the real Gateway through the dev proxy. */
const usingMocks = import.meta.env.VITE_USE_MOCKS !== 'false'

async function start() {
  if (usingMocks) {
    const { worker } = await import('./mocks/browser')
    await worker.start({ onUnhandledFrame: 'bypass', quiet: true })
  }
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

void start()
