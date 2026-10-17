// Shapes of everything the web app sends to and receives from the API Gateway.
//
// Two kinds of contract live here, and the difference matters:
//
//  * REAL — copied from a service's published contract. Sign-in follows the Identity Service;
//    job openings and interview guides follow the Hiring Service's OpenAPI document.
//  * PROPOSED — no service serves these yet. They are this app's proposal, shaped by the
//    functional requirements and the AI Service's .proto, and the mocks implement them. The
//    owning service confirms or changes them; when it does, this file changes to match.

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

// ── Proposal (PROPOSED — Hiring.proposeJobOpening(), FR-1.2, FR-1.5) ──────────────────────

/** POST /job-openings/proposals. Nothing is stored; the Recruiter edits it, then creates. */
export interface JobOpeningProposal {
  proposed_title: string
  criteria: Criterion[]
  /** Parts of the description the model could not interpret (FR-1.5). */
  uninterpreted: string[]
}

// ── Screening (PROPOSED — Hiring, UC-2) ───────────────────────────────────────────────────

export type EntryStatus = 'pending' | 'screened' | 'needs_review' | 'failed'

export interface BatchEntry {
  resume_id: string
  file_name: string
  status: EntryStatus
  /** Why the resume needs manual review or failed (FR-2.7). */
  reason?: string
}

export interface ScreeningBatch {
  id: string
  job_opening_id: string
  submitted_at: string
  /** Finished once every entry is terminal (FR-2.12). */
  status: 'processing' | 'finished'
  total: number
  counts: Record<EntryStatus, number>
  entries: BatchEntry[]
}

export type DecisionState = 'shortlisted' | 'undecided' | 'not_qualified'

/** One row of GET /job-openings/{id}/results, ranked by score (FR-2.6). */
export interface RankedCandidate {
  candidate_id: string
  file_name: string
  batch_id: string
  /** The score the ranking uses: the override when there is one, otherwise the AI's. */
  score: number
  ai_score: number
  override?: { score: number; reason: string }
  meets_must_haves: boolean
  /** Names of the must-have criteria that were not met (FR-2.5). */
  unmet_must_haves: string[]
  decision: DecisionState
}

export interface CriterionAssessment {
  criterion: string
  met: boolean
  score: number
  /** The words in the resume this judgement rests on. */
  evidence: string
}

/** The fields mirror the AI Service's CandidateProfile: no name, contact or protected attributes. */
export interface CandidateProfile {
  work_history: { title: string; organisation: string; start: string; end: string; summary: string }[]
  skills: string[]
  education: { qualification: string; institution: string; completed: string }[]
}

/** GET /job-openings/{id}/candidates/{candidateId} */
export interface CandidateDetail extends RankedCandidate {
  justification: string
  assessments: CriterionAssessment[]
  profile: CandidateProfile
  has_interview_guide: boolean
}

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
