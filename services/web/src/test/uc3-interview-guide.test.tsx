import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { db } from '../mocks/db'
import { renderSignedIn } from './render'

const shortlisted = () => db.results.find((r) => r.shortlisted)!

describe('interview guide (UC-3)', () => {
  it('generates questions tagged with the criterion and the evidence behind them (FR-3.3)', async () => {
    const c = shortlisted()
    const { user } = await renderSignedIn(`/job-openings/jo_seed_backend/candidates/${c.candidateId}/interview-guide`)
    expect(await screen.findByText('No interview guide yet.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate' }))

    const questions = await screen.findAllByTestId('question')
    expect(questions.length).toBeGreaterThan(2)
    expect(within(questions[0]).getByText(/^Tests: /)).toBeInTheDocument()
    expect(within(questions[0]).getByText(/^From: /)).toBeInTheDocument()
    expect(db.guides[0]).toMatchObject({ source: 'generated', revision: 1 })
  })

  it('keeps a note when the Recruiter edits another question, and drops it on regenerating', async () => {
    const c = shortlisted()
    const { user } = await renderSignedIn(`/job-openings/jo_seed_backend/candidates/${c.candidateId}/interview-guide`)
    await user.click(await screen.findByRole('button', { name: 'Generate' }))
    const [first, second] = await screen.findAllByTestId('question')
    await user.type(within(first).getByLabelText('Interview notes'), 'Strong answer')
    await user.tab()
    await screen.findByText('Note saved')

    await user.click(within(second).getByRole('button', { name: 'Remove question' }))
    expect(await screen.findByText('Edited by a recruiter')).toBeInTheDocument()
    expect(db.guides[0].questions[0].note).toBe('Strong answer')

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Regenerate' }))
    await screen.findByText(/^Revision 3/)
    expect(db.guides[0].questions.some((q) => q.note)).toBe(false)
  })

  it('regenerates with an emphasis (FR-3.4)', async () => {
    const c = shortlisted()
    const { user } = await renderSignedIn(`/job-openings/jo_seed_backend/candidates/${c.candidateId}/interview-guide`)
    await user.click(await screen.findByRole('button', { name: 'Generate' }))
    await screen.findAllByTestId('question')
    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/Emphasis/), 'system design')
    await user.click(within(dialog).getByRole('button', { name: 'Regenerate' }))
    expect(await screen.findByText('Emphasis: system design')).toBeInTheDocument()
  })

  it('records an interview note against a question (FR-3.5)', async () => {
    const c = shortlisted()
    const { user } = await renderSignedIn(`/job-openings/jo_seed_backend/candidates/${c.candidateId}/interview-guide`)
    await user.click(await screen.findByRole('button', { name: 'Generate' }))
    const [first] = await screen.findAllByTestId('question')
    await user.type(within(first).getByLabelText('Interview notes'), 'Explained the rollback plan clearly')
    await user.tab()
    expect(await screen.findByText('Note saved')).toBeInTheDocument()
    expect(db.guides[0].questions[0].note).toBe('Explained the rollback plan clearly')
  })

  it('is reached from the shortlisted candidate in the ranked list', async () => {
    const c = shortlisted()
    const { user } = await renderSignedIn(`/job-openings/jo_seed_backend?resume=${c.resumeId}`)
    const drawer = await screen.findByRole('dialog')
    await user.click(await within(drawer).findByRole('link', { name: 'Interview guide' }))
    expect(await screen.findByRole('heading', { name: 'Interview guide' })).toBeInTheDocument()
  })
})
