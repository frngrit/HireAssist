import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { refreshSession, setSession, setSessionLostHandler } from '../api/client'
import { signIn as signInCall, signOut as signOutCall } from '../api/endpoints'
import type { SessionMember } from '../api/types'

interface Auth {
  /** Undefined while the stored session is being restored on first load. */
  member: SessionMember | null | undefined
  signIn: (email: string, password: string) => Promise<void>
  signOut: () => void
}

const AuthContext = createContext<Auth | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [member, setMember] = useState<SessionMember | null | undefined>(undefined)

  // On load the access token is gone (it lived in memory), so ask for one with the refresh
  // cookie. No cookie, or a session that has ended, means signed out (ADR-009).
  useEffect(() => {
    let cancelled = false
    refreshSession().then((session) => {
      if (!cancelled) setMember(session?.member ?? null)
    })
    setSessionLostHandler(() => {
      setSession(null)
      setMember(null)
    })
    return () => {
      cancelled = true
    }
  }, [])

  const signIn = useCallback(async (email: string, password: string) => {
    const session = await signInCall(email, password)
    setSession(session)
    setMember(session.member)
  }, [])

  // The gateway clears the cookie. The Identity Service has no sign-out, so the refresh token
  // stays valid until the session's cap, but no browser holds it any more.
  const signOut = useCallback(() => {
    signOutCall().catch(() => undefined)
    setSession(null)
    setMember(null)
  }, [])

  const value = useMemo(() => ({ member, signIn, signOut }), [member, signIn, signOut])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): Auth {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used inside <AuthProvider>')
  return context
}
