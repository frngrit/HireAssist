// One function per Gateway call. Each names the operation it reaches in the
// Service–Operations–Collaborators table, so a screen can be traced to the service behind it.

import { api, COOKIE_TRANSPORT } from './client'
import type {
  CandidateDetail,
  Criterion,
  InterviewGuide,
  JobOpening,
  JobOpeningDraft,
  JobOpeningProposal,
  QuestionDraft,
  Outcome,
  RankedCandidate,
  ScreeningBatch,
  Session,
  SessionMember,
} from './types'

// ── Auth → Identity ───────────────────────────────────────────────────────────────────────

/** Identity.authenticateMember(). The refresh token comes back as an HttpOnly cookie, not in the body. */
export const signIn = (email: string, password: string) =>
  api<Session>('/auth/sign-in', { method: 'POST', json: { email, password }, anonymous: true, headers: COOKIE_TRANSPORT })

/** The gateway's own route: clears the refresh cookie, which a script cannot do. */
export const signOut = () => api<void>('/auth/sign-out', { method: 'POST', anonymous: true })

/** Identity.changePassword() — takes credentials, not a token: a temporary password cannot sign in (FR-0.5). */
export const changePassword = (email: string, currentPassword: string, newPassword: string) =>
  api<SessionMember>('/auth/change-password', {
    method: 'POST',
    json: { email, current_password: currentPassword, new_password: newPassword },
    anonymous: true,
  })

// ── UC-1 → Hiring ─────────────────────────────────────────────────────────────────────────

/** Hiring.listJobOpenings() */
export const listJobOpenings = () => api<JobOpening[]>('/job-openings')

/** Hiring.getJobOpening() */
export const getJobOpening = (id: string) => api<JobOpening>(`/job-openings/${id}`)

/** Hiring.proposeJobOpening() — the Hiring Service asks the AI Service; nothing is stored. */
export const proposeJobOpening = (description: string) =>
  api<JobOpeningProposal>('/job-openings/proposals', { method: 'POST', json: { description } })

/** Hiring.createJobOpening() */
export const createJobOpening = (draft: JobOpeningDraft) =>
  api<JobOpening>('/job-openings', { method: 'POST', json: draft })

/** Hiring.reviseScreeningCriteria() */
export const reviseScreeningCriteria = (id: string, criteria: Criterion[]) =>
  api<JobOpening>(`/job-openings/${id}/criteria`, { method: 'PUT', json: criteria })

/** Hiring.pauseOpening() */
export const pauseOpening = (id: string) =>
  api<JobOpening>(`/job-openings/${id}/pause`, { method: 'POST' })

/** Hiring.resumeOpening() */
export const resumeOpening = (id: string) =>
  api<JobOpening>(`/job-openings/${id}/resume`, { method: 'POST' })

/** Hiring.closeOpening() */
export const closeOpening = (id: string, outcome: Outcome) =>
  api<JobOpening>(`/job-openings/${id}/close`, { method: 'POST', json: { outcome } })

// ── UC-2 → Hiring ─────────────────────────────────────────────────────────────────────────

/** Hiring.submitScreeningBatch() — answers as soon as the batch is accepted (FR-2.2). */
export const submitScreeningBatch = (jobOpeningId: string, files: File[]) => {
  const form = new FormData()
  for (const file of files) form.append('files', file)
  return api<ScreeningBatch>(`/job-openings/${jobOpeningId}/batches`, { method: 'POST', body: form })
}

/** Hiring.getBatchStatus(), for every batch of the opening. */
export const listBatches = (jobOpeningId: string) =>
  api<ScreeningBatch[]>(`/job-openings/${jobOpeningId}/batches`)

export interface ResultsFilter {
  minScore?: number
  criterion?: string
}

/** Hiring.getRankedResults() — ranked by score; resumes needing review are excluded (FR-2.7). */
export const getRankedResults = (jobOpeningId: string, filter: ResultsFilter = {}) => {
  const query = new URLSearchParams()
  if (filter.minScore) query.set('min_score', String(filter.minScore))
  if (filter.criterion) query.set('criterion', filter.criterion)
  const qs = query.toString()
  return api<RankedCandidate[]>(`/job-openings/${jobOpeningId}/results${qs ? `?${qs}` : ''}`)
}

/** Hiring.getRankedResults() for one candidate, joined with Resume Processing's justification. */
export const getCandidate = (jobOpeningId: string, candidateId: string) =>
  api<CandidateDetail>(`/job-openings/${jobOpeningId}/candidates/${candidateId}`)

/** Hiring.overrideScore() — the AI's original score is kept (FR-2.8). */
export const overrideScore = (jobOpeningId: string, candidateId: string, score: number, reason: string) =>
  api<RankedCandidate>(`/job-openings/${jobOpeningId}/candidates/${candidateId}/override`, {
    method: 'PUT',
    json: { score, reason },
  })

/** Hiring.shortlistCandidate() */
export const shortlistCandidate = (jobOpeningId: string, candidateId: string) =>
  api<RankedCandidate>(`/job-openings/${jobOpeningId}/candidates/${candidateId}/shortlist`, {
    method: 'POST',
  })

// ── UC-3 → Hiring ─────────────────────────────────────────────────────────────────────────

const guidePath = (jobOpeningId: string, candidateId: string) =>
  `/job-openings/${jobOpeningId}/candidates/${candidateId}/interview-guide`

/** Hiring.getInterviewGuide() */
export const getInterviewGuide = (jobOpeningId: string, candidateId: string) =>
  api<InterviewGuide>(guidePath(jobOpeningId, candidateId))

/** Hiring.generateInterviewGuide() — 409 when the candidate already has one. */
export const generateInterviewGuide = (jobOpeningId: string, candidateId: string, emphasis = '') =>
  api<InterviewGuide>(guidePath(jobOpeningId, candidateId), { method: 'POST', json: emphasis ? { emphasis } : {} })

/** Hiring.reviseGuide(), asking the model again: every question is replaced, notes too (FR-3.4). */
export const regenerateInterviewGuide = (jobOpeningId: string, candidateId: string, emphasis = '') =>
  api<InterviewGuide>(`${guidePath(jobOpeningId, candidateId)}/regenerate`, {
    method: 'POST',
    json: emphasis ? { emphasis } : {},
  })

/**
 * Hiring.reviseGuide(), the Recruiter's own edit: the whole set is sent, so adding, rewording,
 * reordering and removing are one request. A note stays with a question whose text is unchanged.
 */
export const reviseGuideQuestions = (jobOpeningId: string, candidateId: string, questions: QuestionDraft[]) =>
  api<InterviewGuide>(`${guidePath(jobOpeningId, candidateId)}/questions`, { method: 'PUT', json: questions })

/** Hiring.recordInterviewNote() (FR-3.5) */
export const recordInterviewNote = (
  jobOpeningId: string,
  candidateId: string,
  questionId: string,
  note: string,
) =>
  api<InterviewGuide>(`${guidePath(jobOpeningId, candidateId)}/questions/${questionId}/note`, {
    method: 'PUT',
    json: { note },
  })
