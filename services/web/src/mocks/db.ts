// In-memory stand-in for the services behind the Gateway. Every record is synthetic — no real
// resume or candidate data, ever (PDPA). Reset to the seed with resetDb() or a page reload.

import type {
  CandidateProfile,
  Classification,
  Criterion,
  CriterionAssessment,
  InterviewGuide,
  JobOpening,
  Role,
} from '../api/types'

export interface MockMember {
  id: string
  email: string
  role: Role
  password: string
  temporaryPassword: boolean
}

export interface MockEntry {
  resumeId: string
  candidateId: string
  fileName: string
  /** When the simulated pipeline finishes this resume. */
  readyAt: number
  /** Set for files the pipeline cannot read; the entry ends in needs_review. */
  reviewReason?: string
}

export interface MockBatch {
  id: string
  jobOpeningId: string
  submittedAt: number
  entries: MockEntry[]
}

export interface MockResult {
  candidateId: string
  jobOpeningId: string
  batchId: string
  fileName: string
  aiScore: number
  override?: { score: number; reason: string }
  assessments: CriterionAssessment[]
  justification: string
  profile: CandidateProfile
  shortlisted: boolean
}

export const config = {
  /** Artificial latency per request, so loading states are visible. Zero in tests. */
  latencyMs: 350,
  /** How long the simulated pipeline takes per resume: a random value in this range. */
  processingMs: [1500, 6000] as [number, number],
}

export const db = {
  members: [] as MockMember[],
  openings: [] as JobOpening[],
  batches: [] as MockBatch[],
  results: [] as MockResult[],
  guides: [] as InterviewGuide[],
  /** Access tokens issued → member id. Clearing it expires every access token at once. */
  tokens: new Map<string, string>(),
  /** Live refresh tokens → member id. Each is used once (ADR-009). */
  refreshTokens: new Map<string, string>(),
  /** The browser's HttpOnly refresh cookie, as the gateway would set it. */
  refreshCookie: null as string | null,
}

// A real cookie survives a reload, so the mock's does too: in the browser it is kept in
// sessionStorage with the member it belongs to. Everything else resets to the seed on reload.
const COOKIE_KEY = 'hireassist.mock.refreshCookie'

export function setRefreshCookie(token: string | null) {
  db.refreshCookie = token
  try {
    const memberId = token ? db.refreshTokens.get(token) : undefined
    if (token && memberId) sessionStorage.setItem(COOKIE_KEY, JSON.stringify({ token, memberId }))
    else sessionStorage.removeItem(COOKIE_KEY)
  } catch {
    // Storage unavailable: the session simply ends on reload.
  }
}

function restoreRefreshCookie() {
  try {
    const saved = sessionStorage.getItem(COOKIE_KEY)
    if (!saved) return
    const { token, memberId } = JSON.parse(saved) as { token: string; memberId: string }
    db.refreshTokens.set(token, memberId)
    db.refreshCookie = token
  } catch {
    // Nothing to restore.
  }
}

let counter = 0
export const newId = (prefix: string) =>
  `${prefix}_${Date.now().toString(36)}${(++counter).toString(36).padStart(4, '0')}`

/** A stable pseudo-random number in [0, 1) from a string, so a file always scores the same. */
export function hash01(text: string): number {
  let h = 2166136261
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return ((h >>> 0) % 10000) / 10000
}

const criterion = (name: string, classification: Classification, weight: number): Criterion => ({
  name,
  classification,
  weight,
})

// Evidence is phrased around the criterion it supports, so each line reads as a resume claim
// about that criterion rather than a random sentence.
const EVIDENCE_MET = [
  (c: string) => `Led a two-year project where ${c} was central; shipped to production.`,
  (c: string) => `Lists ${c} under the current role, with three delivered systems described.`,
  (c: string) => `Day-to-day work with ${c} as Senior Engineer, 2021–present.`,
  (c: string) => `Certification and a production case study covering ${c}.`,
]
const SUMMARIES = [
  'Led the migration of a payments service to Go, 2021–2024.',
  'Designed event consumers processing 2M messages per day.',
  'Owned PostgreSQL schema design and query tuning for the core product.',
  'Built CI pipelines with GitHub Actions and Docker.',
]
const EVIDENCE_UNMET = [
  'No mention of this in the resume.',
  'Only coursework-level exposure is described.',
  'Experience listed is under the required duration.',
]

const SKILLS = ['Go', 'Python', 'PostgreSQL', 'Kafka', 'Docker', 'Kubernetes', 'gRPC', 'React', 'AWS']

/**
 * What the Resume Processing pipeline would produce for one resume: a profile, a per-criterion
 * assessment and a justification. Deterministic per file name and criteria.
 */
export function screen(fileName: string, criteria: Criterion[]) {
  const seed = hash01(fileName)
  const assessments: CriterionAssessment[] = criteria.map((c, i) => {
    const r = hash01(`${fileName}#${c.name}`)
    const met = c.classification === 'must-have' ? r > 0.3 : r > 0.4
    return {
      criterion: c.name,
      met,
      score: met ? 60 + Math.round(r * 40) : Math.round(r * 35),
      evidence: met ? EVIDENCE_MET[(i + Math.floor(seed * 10)) % EVIDENCE_MET.length](c.name) : EVIDENCE_UNMET[i % EVIDENCE_UNMET.length],
    }
  })
  const totalWeight = criteria.reduce((sum, c) => sum + c.weight, 0) || 1
  const aiScore = Math.round(
    criteria.reduce((sum, c, i) => sum + assessments[i].score * c.weight, 0) / totalWeight,
  )
  const unmet = criteria.filter((c, i) => c.classification === 'must-have' && !assessments[i].met)
  const years = 2 + Math.floor(seed * 8)
  const justification = unmet.length
    ? `Not qualified: the resume shows no evidence for ${unmet.map((c) => `"${c.name}"`).join(', ')}. ` +
      `Strongest match is ${assessments.find((a) => a.met)?.criterion ?? 'none'}.`
    : `Meets every must-have. ${years} years of relevant experience; strongest evidence: ` +
      `"${assessments.reduce((a, b) => (a.score >= b.score ? a : b)).evidence}"`
  const profile: CandidateProfile = {
    work_history: [
      {
        title: seed > 0.5 ? 'Senior Software Engineer' : 'Software Engineer',
        organisation: ['Example Fintech Co.', 'Sample Logistics Ltd.', 'Demo Retail Group'][Math.floor(seed * 3)],
        start: String(2026 - years),
        end: '',
        summary: SUMMARIES[Math.floor(seed * SUMMARIES.length)],
      },
      {
        title: 'Junior Developer',
        organisation: 'Placeholder Studio',
        start: String(2026 - years - 2),
        end: String(2026 - years),
        summary: 'Maintained internal tools and wrote integration tests.',
      },
    ],
    skills: SKILLS.filter((_, i) => hash01(`${fileName}@${i}`) > 0.45),
    education: [
      { qualification: 'B.Eng. Computer Engineering', institution: 'Example University', completed: String(2026 - years - 3) },
    ],
  }
  return { aiScore, assessments, justification, profile }
}

function seed() {
  db.members = [
    // The same synthetic members as the Identity Service's demo seed.
    { id: '00000000-0000-4000-8000-000000000001', email: 'nok@acme.example', role: 'admin', password: 'nok-admin-demo-password', temporaryPassword: false },
    { id: '00000000-0000-4000-8000-000000000002', email: 'somchai@acme.example', role: 'recruiter', password: 'somchai-recruiter-password', temporaryPassword: false },
  ]

  const backend: JobOpening = {
    id: 'jo_seed_backend',
    title: 'Backend Engineer',
    description:
      'Backend engineer, 3+ years with Go or Python. Must have shipped production APIs and worked with PostgreSQL. Kafka is a plus. Thai and English.',
    criteria: [
      criterion('3+ years of Go or Python', 'must-have', 5),
      criterion('Production REST or gRPC APIs', 'must-have', 4),
      criterion('PostgreSQL', 'nice-to-have', 3),
      criterion('Kafka or another message broker', 'nice-to-have', 2),
    ],
    expected_time_to_fill_days: 45,
    status: 'open',
    opened_at: '2026-09-20T03:00:00Z',
  }
  const qa: JobOpening = {
    id: 'jo_seed_qa',
    title: 'QA Engineer',
    description: 'QA engineer for the mobile app. Test automation experience required.',
    criteria: [criterion('Test automation (Playwright, Appium or similar)', 'must-have', 5), criterion('Mobile testing', 'nice-to-have', 3)],
    expected_time_to_fill_days: 30,
    status: 'paused',
    opened_at: '2026-09-02T03:00:00Z',
  }
  const analyst: JobOpening = {
    id: 'jo_seed_analyst',
    title: 'Data Analyst',
    description: 'Data analyst, strong SQL, dashboards in Looker or Power BI.',
    criteria: [criterion('Advanced SQL', 'must-have', 5), criterion('Looker or Power BI', 'nice-to-have', 3)],
    expected_time_to_fill_days: 30,
    status: 'closed',
    outcome: 'filled',
    opened_at: '2026-08-01T03:00:00Z',
  }
  db.openings = [backend, qa, analyst]

  // One finished batch on the backend opening, so the ranked list is populated from the start.
  const submittedAt = Date.parse('2026-10-10T02:00:00Z')
  const files = ['resume-001.pdf', 'resume-002.pdf', 'resume-003.pdf', 'resume-004.pdf', 'resume-005.pdf', 'resume-006.pdf', 'scanned-resume-007.pdf']
  const batch: MockBatch = {
    id: 'sb_seed_1',
    jobOpeningId: backend.id,
    submittedAt,
    entries: files.map((fileName, i) => ({
      resumeId: `rs_seed_${i + 1}`,
      candidateId: `cd_seed_${i + 1}`,
      fileName,
      readyAt: submittedAt + 60_000,
      reviewReason: fileName.startsWith('scanned') ? 'No text layer — the PDF is a scanned image' : undefined,
    })),
  }
  db.batches = [batch]
  db.results = batch.entries
    .filter((e) => !e.reviewReason)
    .map((e) => ({
      candidateId: e.candidateId,
      jobOpeningId: backend.id,
      batchId: batch.id,
      fileName: e.fileName,
      ...screen(e.fileName, backend.criteria),
      shortlisted: false,
    }))

  // The best qualified candidate is shortlisted, so UC-3 can be shown straight away.
  const best = db.results
    .filter((r) => r.assessments.every((a, i) => a.met || backend.criteria[i].classification !== 'must-have'))
    .sort((a, b) => b.aiScore - a.aiScore)[0]
  if (best) best.shortlisted = true

  db.guides = []
  db.tokens.clear()
  db.refreshTokens.clear()
  db.refreshCookie = null
}

/** Back to the seed, signed out. */
export function resetDb() {
  seed()
  setRefreshCookie(null)
}

seed()
restoreRefreshCookie()
