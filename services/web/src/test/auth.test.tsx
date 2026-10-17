import { cleanup, screen, waitFor } from '@testing-library/react'
import { setSession } from '../api/client'
import { describe, expect, it } from 'vitest'
import { db } from '../mocks/db'
import { renderApp, renderSignedIn } from './render'

describe('signing in (UC-0)', () => {
  it('sends a signed-out visitor to sign-in', async () => {
    renderApp('/job-openings')
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('shows the error for a wrong password and stays on sign-in', async () => {
    const { user } = renderApp('/sign-in')
    await user.type(await screen.findByLabelText(/Email/), 'somchai@acme.example')
    await user.type(screen.getByLabelText(/Password/), 'not-the-password')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('email or password is wrong')).toBeInTheDocument()
  })

  it('signs in and returns to the page that was asked for', async () => {
    const { user } = renderApp('/job-openings/jo_seed_backend')
    await user.type(await screen.findByLabelText(/Email/), 'somchai@acme.example')
    await user.type(screen.getByLabelText(/Password/), 'somchai-recruiter-password')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('heading', { name: 'Backend Engineer' })).toBeInTheDocument()
  })

  it('renews an expired access token without the user noticing', async () => {
    const { user } = await renderSignedIn('/job-openings')
    await screen.findByText('QA Engineer')
    db.tokens.clear() // every access token the browser holds is now expired
    await user.click(screen.getByText('Backend Engineer'))
    expect(await screen.findByRole('heading', { name: 'Backend Engineer' })).toBeInTheDocument()
  })

  it('returns to sign-in when the session cannot be renewed', async () => {
    const { user } = await renderSignedIn('/job-openings')
    await screen.findByText('QA Engineer')
    db.tokens.clear()
    db.refreshTokens.clear() // the session has ended at the Identity Service too
    await user.click(screen.getByText('Backend Engineer'))
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('makes a member with a temporary password choose their own before signing in (FR-0.5)', async () => {
    const member = db.members.find((m) => m.role === 'recruiter')!
    member.temporaryPassword = true
    const { user } = renderApp('/sign-in')
    await user.type(await screen.findByLabelText(/Email/), member.email)
    await user.type(screen.getByLabelText(/Password/), member.password)
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('heading', { name: 'Choose your password' })).toBeInTheDocument()
    await user.type(screen.getByLabelText(/^New password/), 'a-password-of-my-own')
    await user.type(screen.getByLabelText(/^Confirm new password/), 'a-password-of-my-own')
    await user.click(screen.getByRole('button', { name: 'Save password and sign in' }))

    expect(await screen.findByText('somchai@acme.example')).toBeInTheDocument()
    expect(member).toMatchObject({ password: 'a-password-of-my-own', temporaryPassword: false })
  })

  it('signs out and clears the refresh cookie', async () => {
    const { user } = await renderSignedIn('/job-openings')
    await user.click(await screen.findByRole('button', { name: 'Sign out' }))
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument()
    await waitFor(() => expect(db.refreshCookie).toBeNull())
  })

  it('keeps the session across a reload, and never hands the refresh token to the page (ADR-009)', async () => {
    const { user } = renderApp('/sign-in')
    await user.type(await screen.findByLabelText(/Email/), 'somchai@acme.example')
    await user.type(screen.getByLabelText(/Password/), 'somchai-recruiter-password')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    await screen.findByText('somchai@acme.example')
    expect(db.refreshCookie).not.toBeNull()

    cleanup()
    setSession(null) // a reload: the access token in memory is gone, the cookie is not
    renderApp('/job-openings')
    expect(await screen.findByText('QA Engineer')).toBeInTheDocument()
  })
})
