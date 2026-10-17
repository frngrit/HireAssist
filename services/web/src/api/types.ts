// Shapes of everything the web app sends to and receives from the API Gateway.
//
// Every contract here is copied from the service that owns it: sign-in from the Identity
// Service, and job openings, proposals, screening and interview guides from the Hiring Service's
// OpenAPI document. The mocks implement the same shapes. When a service changes its contract,
// this file changes to match.

// ── Auth (REAL — Gateway → Identity, ADR-009) ─────────────────────────────────────────────

export type Role = 'admin' | 'recruiter'

/** The Identity Service's member record; only the fields the app reads. */
export interface SessionMember {
  id: string
  email: string
  role: Role
}

/** POST /auth/sign-in and POST /auth/refresh both answer with the next pair of tokens. */
export interface Session {
  access_token: string
  token_type: 'Bearer'
  /** Seconds until the access token expires. */
  expires_in: number
  /**
   * Used once: exchanging it returns a new one and retires this one. Absent for this app, which
   * asks the gateway to set it as an HttpOnly cookie instead.
   */
  refresh_token?: string
  /** The cap on the session, however often it is refreshed. */
  session_expires_at: string
  member: SessionMember
}

// ── Job openings (REAL — Hiring Service OpenAPI) ──────────────────────────────────────────

export type Classification = 'must-have' | 'nice-to-have'

export interface Criterion {
  name: string
  classification: Classification
  weight: number
}

export type JobOpeningStatus = 'open' | 'paused' | 'closed'
export type Outcome = 'filled' | 'cancelled'

export interface JobOpeningDraft {
  title: string
  description?: string
  criteria: Criterion[]
  expected_time_to_fill_days: number
}

export interface JobOpening {
  id: string
  title: string
  description: string
  criteria: Criterion[]
  expected_time_to_fill_days: number
  status: JobOpeningStatus
  outcome?: Outcome
  opened_at: string
}

// ── Proposal (REAL — Hiring Service OpenAPI, proposeJobOpening(), FR-1.1–FR-1.5) ─────────

/** Who produced an AI answer. `canned` means scripted demo answers, not a model. */
export interface ModelAttribution {
  provider: string
  model: string
  prompt_version: string
}

/** POST /job-openings/propose. Nothing is stored; the Recruiter edits it, then creates. */
export interface JobOpeningProposal {
  title: string
  criteria: Criterion[]
  /** Parts of the description the model could not interpret (FR-1.5). */
  uninterpreted: string[]
  attribution: ModelAttribution
}

// ── Screening (REAL — Hiring Service OpenAPI, UC-2) ───────────────────────────────────────

export type EntryStatus = 'pending' | 'screened' | 'needs_review' | 'failed'

export interface BatchEntry {
  resume_id: string
  file_name: string
  status: EntryStatus
}

export interface ScreeningBatch {
  id: string
  job_opening_id: string
  submitted_at: string
  /** True once no resume is pending (FR-2.12). */
  complete: boolean
  counts: Record<EntryStatus, number>
  entries: BatchEntry[]
  /** Only in the answer to the upload: files left out of the batch, and why. */
  rejected?: { file_name: string; reason: string }[]
}

/** One resume as the Recruiter sees it (FR-2.5). Score fields appear once it is screened. */
export interface ScreeningResult {
  resume_id: string
  batch_id: string
  file_name: string
  status: EntryStatus
  /** Assigned by the Resume Processing Service; UC-3 is keyed by it. */
  candidate_id?: string
  /** The score ranked on: the Recruiter's override when there is one. */
  score?: number
  /** Present only when overridden: screening's own score (FR-2.8). */
  original_score?: number
  overridden_at?: string
  /** False when a must-have is unmet (FR-2.5). */
  qualified?: boolean
  unmet_must_haves?: string[]
  shortlisted: boolean
  shortlisted_at?: string
  /** Why the resume needs review, or failed (FR-2.7). */
  reason?: string
  completed_at?: string
}

/** GET /job-openings/{id}/results: qualified first, then by score (FR-2.6). */
export interface RankedResults {
  job_opening_id: string
  /** Resumes still being screened. */
  pending: number
  ranked: ScreeningResult[]
  /** Resumes that could not be screened, never ranked (FR-2.7). */
  needs_review: ScreeningResult[]
}

export interface Evidence {
  criterion: string
  classification: Classification
  met: boolean
  score: number
  /** The words in the resume the verdict rests on. */
  evidence: string
}

/** GET /job-openings/{id}/results/{resumeId} */
export interface ScreenedCandidate {
  result: ScreeningResult
  /** Read from Resume Processing; null until screened, or when that service did not answer. */
  justification: { text: string; evidence: Evidence[] } | null
}

/** Not on the wire: the decision the UI shows, derived from a result. */
export type DecisionState = 'shortlisted' | 'undecided' | 'not_qualified'

export const decisionOf = (r: ScreeningResult): DecisionState =>
  r.qualified === false ? 'not_qualified' : r.shortlisted ? 'shortlisted' : 'undecided'

// ── Interview guides (REAL — Hiring Service OpenAPI, UC-3) ────────────────────────────────

export type QuestionKind = 'resume-claim' | 'must-have-gap' | 'role-depth'

export interface InterviewQuestion {
  /** Identifies the question within its guide; renumbered when the questions change. */
  id: string
  text: string
  /** The criterion this question tests, as the job opening spells it (FR-3.3). */
  criterion: string
  /** The resume claim it came from (FR-3.3); absent for a must-have the resume never mentions. */
  evidence?: string
  kind: QuestionKind
  /** What the interviewer wrote against it afterwards (FR-3.5). */
  note?: string
}

/** A question as the Recruiter sends it: the guide assigns ids, and notes are recorded separately. */
export type QuestionDraft = Pick<InterviewQuestion, 'text' | 'criterion' | 'evidence' | 'kind'>

export interface InterviewGuide {
  id: string
  job_opening_id: string
  candidate_id: string
  questions: InterviewQuestion[]
  /** The steer the last generation was asked for, absent when none was given. */
  emphasis?: string
  /** Whether the questions are the model's or the Recruiter's. */
  source: 'generated' | 'edited'
  /** Which model wrote the questions (ADR-004); null once a Recruiter has edited them. */
  attribution: { provider: string; model: string; prompt_version: string } | null
  /** Moves on every generation and every edit, not on a note. */
  revision: number
  created_at: string
  updated_at: string
}

// ── Errors (REAL — every service answers {"error": "..."}) ────────────────────────────────

export interface ErrorBody {
  error: string
  code?: string
}
