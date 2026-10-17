import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { API_BASE } from '../api/client'
import { renderSignedIn } from './render'
import { server } from './setup'

const pdf = (name: string) => new File(['%PDF-1.7 synthetic'], name, { type: 'application/pdf' })

describe('batch screening (UC-2)', () => {
  it('ranks the seeded candidates by score, highest first', async () => {
    await renderSignedIn('/job-openings/jo_seed_backend')
    const rows = await screen.findAllByTestId('result-row')
    const scores = rows.map((r) => Number(within(r).getAllByText(/^\d+$/)[1].textContent))
    expect(scores).toEqual([...scores].sort((a, b) => b - a))
    // The scanned resume is flagged for review, never ranked (FR-2.7).
    expect(screen.queryByRole('cell', { name: 'scanned-resume-007.pdf' })).not.toBeInTheDocument()
    expect(screen.getByText('scanned-resume-007.pdf')).toBeInTheDocument()
  })

  it('accepts a batch, and its results join the ranking as they arrive', async () => {
    const { user, container } = await renderSignedIn('/job-openings/jo_seed_backend')
    await screen.findAllByTestId('result-row')
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, [pdf('new-candidate-a.pdf'), pdf('scan-of-paper-cv.pdf')])

    expect(await screen.findByText(/Batch accepted — 2 resumes/)).toBeInTheDocument()
    expect(await screen.findByRole('cell', { name: 'new-candidate-a.pdf' }, { timeout: 4000 })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('scan-of-paper-cv.pdf')).toBeInTheDocument())
  })

  it('opens a candidate with the justification and per-criterion evidence (FR-2.5)', async () => {
    const { user } = await renderSignedIn('/job-openings/jo_seed_backend')
    const [first] = await screen.findAllByTestId('result-row')
    await user.click(first)
    const drawer = await screen.findByRole('dialog')
    expect(await within(drawer).findByText('Why this score')).toBeInTheDocument()
    expect(within(drawer).getAllByText(/Evidence:/).length).toBe(4)
  })

  it('keeps the AI score beside a recruiter override (FR-2.8)', async () => {
    const { user } = await renderSignedIn('/job-openings/jo_seed_backend')
    const rows = await screen.findAllByTestId('result-row')
    await user.click(rows[rows.length - 1])
    const drawer = await screen.findByRole('dialog')
    const score = await within(drawer).findByLabelText(/New score/)
    await user.clear(score)
    await user.type(score, '99')
    await user.type(within(drawer).getByLabelText('Reason'), 'Strong referral from the team lead')
    await user.click(within(drawer).getByRole('button', { name: 'Save' }))

    expect(await within(drawer).findByText(/AI score:/)).toBeInTheDocument()
    await waitFor(() => expect(within(screen.getAllByTestId('result-row')[0]).getByText('99')).toBeInTheDocument())
  })

  it('says plainly when the Hiring Service does not serve screening yet', async () => {
    // The router's own 404: plain text, not a service's JSON error.
    server.use(http.get(`${API_BASE}/job-openings/:id/batches`, () => new HttpResponse('404 page not found', { status: 404 })))
    await renderSignedIn('/job-openings/jo_seed_backend')
    expect(await screen.findByText('Screening is not available yet')).toBeInTheDocument()
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument()
  })
})
