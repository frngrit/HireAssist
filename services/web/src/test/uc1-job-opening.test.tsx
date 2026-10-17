import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { renderSignedIn } from './render'
import { server } from './setup'

describe('creating a job opening (UC-1)', () => {
  it('proposes criteria from a description, lets the recruiter edit them, then creates the opening', async () => {
    const { user } = await renderSignedIn('/job-openings/new')
    await user.type(
      screen.getByLabelText(/Role description/),
      'Platform engineer, 4+ years with Kubernetes. Terraform is a plus. Fun team',
    )
    await user.click(screen.getByRole('button', { name: 'Propose criteria' }))

    // The proposal is shown for review, with what was not understood (FR-1.3, FR-1.5).
    expect(await screen.findByDisplayValue('4+ years with Kubernetes')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Terraform')).toBeInTheDocument()
    expect(screen.getByText('Fun team')).toBeInTheDocument()

    // The recruiter corrects the proposal before anything is stored (FR-1.4).
    await user.click(screen.getByRole('button', { name: 'Add criterion' }))
    const names = screen.getAllByLabelText(/^Criterion \d/)
    await user.type(names[names.length - 1], 'On-call experience')
    await user.click(screen.getByRole('button', { name: 'Create job opening' }))

    expect(await screen.findByRole('heading', { name: 'Platform engineer' })).toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: 'Criteria' }))
    const table = await screen.findByRole('table')
    expect(within(table).getByText('On-call experience')).toBeInTheDocument()
  })

  it('offers manual entry when the AI proposal is unavailable (FR-1.5)', async () => {
    server.use(http.post('/api/job-openings/proposals', () => HttpResponse.json({ error: 'unavailable' }, { status: 503 })))
    const { user } = await renderSignedIn('/job-openings/new')
    await user.type(screen.getByLabelText(/Role description/), 'Anything')
    await user.click(screen.getByRole('button', { name: 'Propose criteria' }))
    expect(await screen.findByText(/You can enter the criteria yourself/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Enter criteria manually' }))
    await user.type(screen.getByLabelText(/Job title/), 'Designer')
    await user.type(screen.getByLabelText('Criterion 1'), 'Portfolio')
    await user.click(screen.getByRole('button', { name: 'Create job opening' }))
    expect(await screen.findByRole('heading', { name: 'Designer' })).toBeInTheDocument()
  })

  it('closes an opening with an outcome, after which it cannot be paused (FR-1.8, FR-1.9)', async () => {
    const { user } = await renderSignedIn('/job-openings/jo_seed_backend')
    await user.click(await screen.findByRole('button', { name: 'Close opening' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByText('Cancelled'))
    await user.click(within(dialog).getByRole('button', { name: 'Close opening' }))

    expect(await screen.findByText('Closed · Cancelled')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument()
  })
})
