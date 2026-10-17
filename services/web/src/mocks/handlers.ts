// MSW handlers: the behaviour of the Gateway and the services behind it, as the web app sees
// them. Sign-in follows the Identity Service; job openings and interview guides follow the Hiring
// Service's OpenAPI contract, including its error codes and messages; proposals and screening
// follow the same contract.

import { delay, http, HttpResponse, type HttpResponseResolver } from 'msw'
import type {
  Criterion,
  EntryStatus,
  InterviewGuide,
  InterviewQuestion,
  QuestionDraft,
  JobOpening,
  JobOpeningDraft,
  JobOpeningProposal,
  QuestionKind,
  RankedResults,
  ScreenedCandidate,
  ScreeningBatch,
  ScreeningResult,
  Session,
} from '../api/types'
import { API_BASE } from '../api/client'
import { config, db, newId, screen, setRefreshCookie, type MockBatch, type MockResult } from './db'

const error = (status: number, message: string, code?: string) =>
  HttpResponse.json({ error: message, ...(code ? { code } : {}) }, { status })

const wait = () => (config.latencyMs ? delay(config.latencyMs) : Promise.resolve())

// ── Auth ──────────────────────────────────────────────────────────────────────────────────

const SESSION_HOURS = 8

/**
 * The Identity Service's token pair, then what the gateway does with it for a client that asks
 * for the cookie: the refresh token leaves the body and becomes the HttpOnly cookie.
 */
function issueSession(memberId: string, request: Request): Session {
  const member = db.members.find((m) => m.id === memberId)!
  const access = newId('at')
  const refresh = newId('rt')
  db.tokens.set(access, member.id)
  db.refreshTokens.set(refresh, member.id)
  const session: Session = {
    access_token: access,
    token_type: 'Bearer',
    expires_in: 900,
    refresh_token: refresh,
    session_expires_at: new Date(Date.now() + SESSION_HOURS * 3_600_000).toISOString(),
    member: { id: member.id, email: member.email, role: member.role },
  }
  if (wantsCookie(request)) {
    setRefreshCookie(refresh)
    delete session.refresh_token
  }
  return session
}

const wantsCookie = (request: Request) => request.headers.get('X-Refresh-Token-Transport') === 'cookie'

const readJson = async <T>(request: Request) => (await request.json().catch(() => ({}))) as Partial<T>

/** The Gateway's token check (FR-0.2). Returns an error response, or null when the token is good. */
function requireSession(request: Request) {
  const token = request.headers.get('Authorization')?.replace(/^Bearer /, '')
  if (!token || !db.tokens.has(token)) return error(401, 'Missing, invalid or expired session token')
  return null
}

type ResolverInfo = Parameters<HttpResponseResolver>[0]

/** Wraps a handler with the Gateway's behaviour: latency, then the token check. */
function guarded(handler: (info: ResolverInfo) => Response | Promise<Response>): HttpResponseResolver {
  return async (info) => {
    await wait()
    return requireSession(info.request) ?? handler(info)
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────────────────

const findOpening = (id: string) => db.openings.find((o) => o.id === id)

function validateCriteria(criteria: unknown): string | null {
  if (!Array.isArray(criteria) || criteria.length === 0) return 'at least one criterion is required'
  for (const c of criteria as Criterion[]) {
    if (!c?.name?.trim()) return 'every criterion needs a name'
    if (c.classification !== 'must-have' && c.classification !== 'nice-to-have')
      return `criterion "${c.name}" has an unknown classification`
    if (!Number.isInteger(c.weight) || c.weight <= 0) return `criterion "${c.name}" must have a positive weight`
  }
  return null
}

/** The status the simulated pipeline has reached for one entry, as of now. */
function entryStatus(entry: MockBatch['entries'][number]): EntryStatus {
  if (Date.now() < entry.readyAt) return 'pending'
  return entry.reviewReason ? 'needs_review' : 'screened'
}

/**
 * Produces results for entries whose time has come. This is where, in the real system, Resume
 * Processing publishes `screening.completed` and Hiring records the result.
 */
function advancePipeline() {
  for (const batch of db.batches) {
    const opening = findOpening(batch.jobOpeningId)
    if (!opening) continue
    for (const entry of batch.entries) {
      if (entryStatus(entry) !== 'screened') continue
      if (db.results.some((r) => r.candidateId === entry.candidateId)) continue
      db.results.push({
        resumeId: entry.resumeId,
        candidateId: entry.candidateId,
        jobOpeningId: batch.jobOpeningId,
        batchId: batch.id,
        fileName: entry.fileName,
        ...screen(entry.fileName, opening.criteria),
        shortlisted: false,
        completedAt: new Date(entry.readyAt).toISOString(),
      })
    }
  }
}

function toBatch(batch: MockBatch): ScreeningBatch {
  const entries = batch.entries.map((e) => ({ resume_id: e.resumeId, file_name: e.fileName, status: entryStatus(e) }))
  const counts: Record<EntryStatus, number> = { pending: 0, screened: 0, needs_review: 0, failed: 0 }
  for (const e of entries) counts[e.status]++
  return {
    id: batch.id,
    job_opening_id: batch.jobOpeningId,
    submitted_at: new Date(batch.submittedAt).toISOString(),
    complete: counts.pending === 0,
    counts,
    entries,
  }
}

const unmetMustHaves = (r: MockResult) =>
  findOpening(r.jobOpeningId)!
    .criteria.filter((c) => c.classification === 'must-have')
    .filter((c) => !r.assessments.find((a) => a.criterion === c.name)?.met)
    .map((c) => c.name)

function toResult(r: MockResult): ScreeningResult {
  const unmet = unmetMustHaves(r)
  return {
    resume_id: r.resumeId,
    batch_id: r.batchId,
    file_name: r.fileName,
    status: 'screened',
    candidate_id: r.candidateId,
    score: r.overrideScore ?? r.aiScore,
    ...(r.overrideScore !== undefined ? { original_score: r.aiScore, overridden_at: r.overriddenAt } : {}),
    qualified: unmet.length === 0,
    ...(unmet.length ? { unmet_must_haves: unmet } : {}),
    shortlisted: r.shortlisted,
    ...(r.shortlistedAt ? { shortlisted_at: r.shortlistedAt } : {}),
    completed_at: r.completedAt,
  }
}

const findByResume = (openingId: string, resumeId: string) =>
  db.results.find((r) => r.jobOpeningId === openingId && r.resumeId === resumeId)

/** UC-3 is keyed by the candidate, as Resume Processing names them. */
const findByCandidate = (openingId: string, candidateId: string) =>
  db.results.find((r) => r.jobOpeningId === openingId && r.candidateId === candidateId)

const MUST = /\b(must|required|need|at least)\b|\d\+\s*years|ต้อง|จำเป็น/
const NICE = /\b(plus|nice|bonus|preferred|ideally)\b|จะพิจารณาเป็นพิเศษ/
const REQUIREMENT = /\b(experience|years?|knowledge|skills?|with|in|using)\b|ประสบการณ์|ปี/

/**
 * A naive stand-in for the AI Service's DeriveCriteriaFromDescription: one criterion per
 * requirement-like phrase, the opening phrase as the title, and everything else reported back
 * as not understood — which is the behaviour FR-1.5 asks of the real thing.
 */
function proposeFrom(description: string): JobOpeningProposal {
  const phrases = description
    .split(/[.\n;,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  const first = phrases[0] ?? ''
  const titled = first.length < 60 && !MUST.test(first.toLowerCase()) && !REQUIREMENT.test(first.toLowerCase())
  const criteria: Criterion[] = []
  const uninterpreted: string[] = []
  for (const phrase of titled ? phrases.slice(1) : phrases) {
    const lower = phrase.toLowerCase()
    const mustHave = MUST.test(lower)
    const niceToHave = NICE.test(lower)
    if (!(mustHave || niceToHave || REQUIREMENT.test(lower))) {
      uninterpreted.push(phrase)
      continue
    }
    const name = phrase
      .replace(/^(we need|must have|must|required:?|nice to have:?|a plus:?)\s+/i, '')
      .replace(/\s+(is a plus|is a bonus|is required|required|preferred|is nice to have)$/i, '')
    const must = mustHave && !niceToHave
    criteria.push({ name: name.charAt(0).toUpperCase() + name.slice(1), classification: must ? 'must-have' : 'nice-to-have', weight: must ? 5 : 3 })
  }
  return {
    title: titled ? first.charAt(0).toUpperCase() + first.slice(1) : '',
    criteria,
    uninterpreted,
    attribution: { provider: 'mock', model: 'keywords', prompt_version: 'mock-1' },
  }
}

const QUESTION_TEMPLATES: Record<QuestionKind, (criterion: string, evidence: string) => string> = {
  'resume-claim': (c, e) => `Your resume says: “${e}” Walk me through your own part in that, as it relates to ${c}.`,
  'must-have-gap': (c) => `The role requires ${c}, and your resume does not show it. Where have you come closest?`,
  'role-depth': (c) => `Describe the hardest problem you have solved involving ${c}. What would you do differently now?`,
}

/** Ids are the guide's own, renumbered whenever the questions change, as Hiring does. */
const numbered = (drafts: QuestionDraft[]): InterviewQuestion[] => drafts.map((q, i) => ({ ...q, id: `q${i + 1}` }))

/** A stand-in for the AI Service's GenerateInterviewQuestions: the questions only, no guide. */
function generateQuestions(result: MockResult, opening: JobOpening, emphasis: string): InterviewQuestion[] {
  const drafts: QuestionDraft[] = []
  if (emphasis) {
    drafts.push({
      text: `With an emphasis on ${emphasis}: tell me about a time this was central to your work, and what the outcome was.`,
      criterion: opening.criteria[0]?.name ?? '',
      kind: 'role-depth',
    })
  }
  for (const a of result.assessments) {
    const c = opening.criteria.find((x) => x.name === a.criterion)
    if (!c) continue
    if (a.met) {
      drafts.push({ text: QUESTION_TEMPLATES['resume-claim'](c.name, a.evidence), criterion: c.name, evidence: a.evidence, kind: 'resume-claim' })
    } else if (c.classification === 'must-have') {
      // No evidence: the resume never mentions it, which is why it is asked.
      drafts.push({ text: QUESTION_TEMPLATES['must-have-gap'](c.name, ''), criterion: c.name, kind: 'must-have-gap' })
    }
  }
  for (const c of opening.criteria.slice(0, 2)) {
    drafts.push({ text: QUESTION_TEMPLATES['role-depth'](c.name, ''), criterion: c.name, kind: 'role-depth' })
  }
  return numbered(drafts)
}

const MOCK_ATTRIBUTION = { provider: 'mock', model: 'template', prompt_version: 'mock-1' }

const findGuide = (jobOpeningId: unknown, candidateId: unknown) =>
  db.guides.find((g) => g.job_opening_id === jobOpeningId && g.candidate_id === candidateId)

function validDrafts(drafts: unknown): drafts is QuestionDraft[] {
  return (
    Array.isArray(drafts) &&
    drafts.length > 0 &&
    drafts.every((q: Partial<QuestionDraft>) => q.text?.trim() && q.criterion?.trim() && q.kind && q.kind in QUESTION_TEMPLATES)
  )
}

// ── Handlers ──────────────────────────────────────────────────────────────────────────────

export const handlers = [
  // Auth → Identity (ADR-009)
  http.post(`${API_BASE}/auth/sign-in`, async ({ request }) => {
    await wait()
    const { email, password } = await readJson<{ email: string; password: string }>(request)
    const member = db.members.find((m) => m.email === email?.trim().toLowerCase())
    if (!member || member.password !== password) return error(401, 'email or password is wrong')
    if (member.temporaryPassword) return error(403, 'the temporary password must be replaced before signing in')
    return HttpResponse.json(issueSession(member.id, request))
  }),

  http.post(`${API_BASE}/auth/refresh`, async ({ request }) => {
    await wait()
    const token = (await readJson<{ refresh_token: string }>(request)).refresh_token || (wantsCookie(request) ? db.refreshCookie : null)
    if (!token) return error(401, 'no session to refresh; sign in')
    const memberId = db.refreshTokens.get(token)
    if (!memberId) {
      if (wantsCookie(request)) setRefreshCookie(null)
      return error(401, 'refresh token is not valid')
    }
    db.refreshTokens.delete(token) // used once
    return HttpResponse.json(issueSession(memberId, request))
  }),

  // The gateway's own route.
  http.post(`${API_BASE}/auth/sign-out`, async () => {
    await wait()
    setRefreshCookie(null)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post(`${API_BASE}/auth/change-password`, async ({ request }) => {
    await wait()
    const body = await readJson<{ email: string; current_password: string; new_password: string }>(request)
    if ((body.new_password ?? '').length < 15) return error(422, 'password must be at least 15 characters')
    if (body.new_password === body.current_password) return error(422, 'the new password must differ from the current one')
    const member = db.members.find((m) => m.email === body.email?.trim().toLowerCase())
    if (!member || member.password !== body.current_password) return error(401, 'email or password is wrong')
    member.password = body.new_password!
    member.temporaryPassword = false
    // A password change ends every session the member had.
    for (const [token, id] of db.refreshTokens) if (id === member.id) db.refreshTokens.delete(token)
    return HttpResponse.json({ id: member.id, email: member.email, role: member.role })
  }),

  // UC-1 → Hiring. Registered before /job-openings/:id so "propose" is not read as an id.
  http.post(
    `${API_BASE}/job-openings/propose`,
    guarded(async ({ request }) => {
      const { description } = (await request.json().catch(() => ({}))) as { description?: string }
      if (!description?.trim()) return error(422, 'description is required')
      await delay(config.latencyMs ? 1200 : 0) // a model call takes a moment
      return HttpResponse.json(proposeFrom(description))
    }),
  ),

  http.get(
    `${API_BASE}/job-openings`,
    guarded(() => HttpResponse.json(db.openings)),
  ),

  http.post(
    `${API_BASE}/job-openings`,
    guarded(async ({ request }) => {
      const draft = (await request.json().catch(() => null)) as JobOpeningDraft | null
      if (!draft) return error(400, 'malformed JSON body')
      if (!draft.title?.trim()) return error(422, 'invalid job opening: title is required')
      const problem = validateCriteria(draft.criteria)
      if (problem) return error(422, `invalid job opening: ${problem}`)
      if (!Number.isInteger(draft.expected_time_to_fill_days) || draft.expected_time_to_fill_days <= 0)
        return error(422, 'invalid job opening: expected time-to-fill must be positive')
      const opening: JobOpening = {
        id: newId('jo'),
        title: draft.title.trim(),
        description: draft.description ?? '',
        criteria: draft.criteria,
        expected_time_to_fill_days: draft.expected_time_to_fill_days,
        status: 'open',
        opened_at: new Date().toISOString(),
      }
      db.openings.push(opening)
      return HttpResponse.json(opening, { status: 201, headers: { Location: `/job-openings/${opening.id}` } })
    }),
  ),

  http.get(
    `${API_BASE}/job-openings/:id`,
    guarded(({ params }) => {
      const opening = findOpening(params.id as string)
      return opening ? HttpResponse.json(opening) : error(404, 'job opening not found')
    }),
  ),

  http.put(
    `${API_BASE}/job-openings/:id/criteria`,
    guarded(async ({ params, request }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      const criteria = await request.json().catch(() => null)
      if (opening.status === 'closed') return error(409, 'cannot revise the criteria of a closed job opening')
      const problem = validateCriteria(criteria)
      if (problem) return error(422, `invalid job opening: ${problem}`)
      opening.criteria = criteria as Criterion[]
      return HttpResponse.json(opening)
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/pause`,
    guarded(({ params }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      if (opening.status !== 'open') return error(409, `cannot pause a ${opening.status} job opening`)
      opening.status = 'paused'
      return HttpResponse.json(opening)
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/resume`,
    guarded(({ params }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      if (opening.status !== 'paused') return error(409, `cannot resume a ${opening.status} job opening`)
      opening.status = 'open'
      return HttpResponse.json(opening)
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/close`,
    guarded(async ({ params, request }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      const { outcome } = (await request.json().catch(() => ({}))) as { outcome?: string }
      if (opening.status === 'closed') return error(409, 'job opening is already closed')
      if (outcome !== 'filled' && outcome !== 'cancelled') return error(422, `unknown outcome "${outcome}"`)
      opening.status = 'closed'
      opening.outcome = outcome
      return HttpResponse.json(opening)
    }),
  ),

  // UC-2 → Hiring
  http.post(
    `${API_BASE}/job-openings/:id/batches`,
    guarded(async ({ params, request }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      if (opening.status !== 'open') return error(409, `cannot screen resumes against a ${opening.status} job opening`)
      const form = await request.formData()
      const files = form.getAll('resumes').filter((f): f is File => typeof f !== 'string')
      // Files that cannot be screened are left out and listed, not a reason to refuse the batch.
      const isPdf = (f: File) => f.name.toLowerCase().endsWith('.pdf')
      const rejected = files.filter((f) => !isPdf(f)).map((f) => ({ file_name: f.name, reason: 'not a PDF' }))
      const accepted = files.filter(isPdf)
      if (accepted.length === 0) return error(422, 'nothing in the batch can be screened')
      const now = Date.now()
      const [min, max] = config.processingMs
      const batch: MockBatch = {
        id: newId('sb'),
        jobOpeningId: opening.id,
        submittedAt: now,
        entries: accepted.map((f) => ({
          resumeId: newId('rs'),
          candidateId: newId('cd'),
          fileName: f.name,
          readyAt: now + min + Math.random() * (max - min),
          reviewReason: /scan/i.test(f.name) ? 'No text layer — the PDF is a scanned image' : undefined,
        })),
      }
      db.batches.push(batch)
      return HttpResponse.json(
        { ...toBatch(batch), ...(rejected.length ? { rejected } : {}) },
        { status: 202, headers: { Location: `/job-openings/${opening.id}/batches/${batch.id}` } },
      )
    }),
  ),

  http.get(
    `${API_BASE}/job-openings/:id/batches/:batchId`,
    guarded(({ params }) => {
      advancePipeline()
      const batch = db.batches.find((b) => b.jobOpeningId === params.id && b.id === params.batchId)
      return batch ? HttpResponse.json(toBatch(batch)) : error(404, 'screening batch not found')
    }),
  ),

  http.get(
    `${API_BASE}/job-openings/:id/results`,
    guarded(({ params, request }) => {
      const opening = findOpening(params.id as string)
      if (!opening) return error(404, 'job opening not found')
      advancePipeline()
      const url = new URL(request.url)
      const minScore = Number(url.searchParams.get('min_score') ?? 0)
      const criterion = url.searchParams.get('criterion')
      // Qualified first — a must-have is a gate, not a weight — then by score.
      const ranked = db.results
        .filter((r) => r.jobOpeningId === opening.id)
        .map(toResult)
        .filter((r) => r.score! >= minScore)
        .filter((r) => !criterion || !(r.unmet_must_haves ?? []).includes(criterion))
        .sort((a, b) => Number(b.qualified) - Number(a.qualified) || b.score! - a.score!)
      const entries = db.batches.filter((b) => b.jobOpeningId === opening.id).flatMap((b) => b.entries.map((e) => ({ b, e })))
      const body: RankedResults = {
        job_opening_id: opening.id,
        pending: entries.filter(({ e }) => entryStatus(e) === 'pending').length,
        ranked,
        needs_review: entries
          .filter(({ e }) => entryStatus(e) === 'needs_review')
          .map(({ b, e }) => ({
            resume_id: e.resumeId,
            batch_id: b.id,
            file_name: e.fileName,
            status: 'needs_review',
            shortlisted: false,
            reason: e.reviewReason,
            completed_at: new Date(e.readyAt).toISOString(),
          })),
      }
      return HttpResponse.json(body)
    }),
  ),

  http.get(
    `${API_BASE}/job-openings/:id/results/:resumeId`,
    guarded(({ params }) => {
      const result = findByResume(params.id as string, params.resumeId as string)
      if (!result) return error(404, 'screening result not found')
      const opening = findOpening(result.jobOpeningId)!
      const body: ScreenedCandidate = {
        result: toResult(result),
        justification: {
          text: result.justification,
          evidence: result.assessments.map((a) => ({
            ...a,
            classification: opening.criteria.find((c) => c.name === a.criterion)?.classification ?? 'nice-to-have',
          })),
        },
      }
      return HttpResponse.json(body)
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/results/:resumeId/override`,
    guarded(async ({ params, request }) => {
      const result = findByResume(params.id as string, params.resumeId as string)
      if (!result) return error(404, 'screening result not found')
      const { score } = (await request.json().catch(() => ({}))) as { score?: number }
      if (!Number.isInteger(score) || score! < 0 || score! > 100) return error(422, 'score must be a whole number from 0 to 100')
      result.overrideScore = score!
      result.overriddenAt = new Date().toISOString()
      return HttpResponse.json(toResult(result))
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/results/:resumeId/shortlist`,
    guarded(({ params }) => {
      const result = findByResume(params.id as string, params.resumeId as string)
      if (!result) return error(404, 'screening result not found')
      if (!toResult(result).qualified) return error(422, 'a candidate who misses a must-have criterion cannot be shortlisted')
      result.shortlisted = true
      result.shortlistedAt ??= new Date().toISOString()
      return HttpResponse.json(toResult(result))
    }),
  ),

  // UC-3 → Hiring
  http.get(
    `${API_BASE}/job-openings/:id/candidates/:candidateId/interview-guide`,
    guarded(({ params }) => {
      const guide = findGuide(params.id, params.candidateId)
      return guide ? HttpResponse.json(guide) : error(404, 'no interview guide has been generated for this candidate')
    }),
  ),

  http.post(
    `${API_BASE}/job-openings/:id/candidates/:candidateId/interview-guide`,
    guarded(async ({ params, request }) => {
      const opening = findOpening(params.id as string)
      const result = findByCandidate(params.id as string, params.candidateId as string)
      if (!opening || !result) return error(404, 'candidate not found')
      if (!result.shortlisted) return error(409, 'interview guides are generated for shortlisted candidates')
      if (findGuide(opening.id, result.candidateId)) return error(409, 'this candidate already has an interview guide')
      const emphasis = (await readJson<{ emphasis: string }>(request)).emphasis?.trim() ?? ''
      await delay(config.latencyMs ? 1500 : 0) // a model call takes a moment
      const now = new Date().toISOString()
      const guide: InterviewGuide = {
        id: newId('ig'),
        job_opening_id: opening.id,
        candidate_id: result.candidateId,
        questions: generateQuestions(result, opening, emphasis),
        ...(emphasis ? { emphasis } : {}),
        source: 'generated',
        attribution: MOCK_ATTRIBUTION,
        revision: 1,
        created_at: now,
        updated_at: now,
      }
      db.guides.push(guide)
      return HttpResponse.json(guide, { status: 201 })
    }),
  ),

  // Every question is replaced, and the notes with them: they were written against questions
  // that are no longer asked.
  http.post(
    `${API_BASE}/job-openings/:id/candidates/:candidateId/interview-guide/regenerate`,
    guarded(async ({ params, request }) => {
      const guide = findGuide(params.id, params.candidateId)
      const opening = findOpening(params.id as string)
      const result = findByCandidate(params.id as string, params.candidateId as string)
      if (!guide || !opening || !result) return error(404, 'no interview guide has been generated for this candidate')
      const emphasis = (await readJson<{ emphasis: string }>(request)).emphasis?.trim() ?? ''
      await delay(config.latencyMs ? 1500 : 0)
      guide.questions = generateQuestions(result, opening, emphasis)
      if (emphasis) guide.emphasis = emphasis
      else delete guide.emphasis
      guide.source = 'generated'
      guide.attribution = MOCK_ATTRIBUTION
      guide.revision += 1
      guide.updated_at = new Date().toISOString()
      return HttpResponse.json(guide)
    }),
  ),

  // The Recruiter's own set. A note follows a question whose text is unchanged.
  http.put(
    `${API_BASE}/job-openings/:id/candidates/:candidateId/interview-guide/questions`,
    guarded(async ({ params, request }) => {
      const guide = findGuide(params.id, params.candidateId)
      if (!guide) return error(404, 'no interview guide has been generated for this candidate')
      const drafts = await request.json().catch(() => null)
      if (!validDrafts(drafts)) return error(422, 'every question needs text, a criterion and a kind')
      const notes = new Map(guide.questions.filter((q) => q.note).map((q) => [q.text, q.note!]))
      guide.questions = numbered(drafts.map(({ text, criterion, evidence, kind }) => ({ text, criterion, evidence, kind }))).map((q) =>
        notes.has(q.text) ? { ...q, note: notes.get(q.text) } : q,
      )
      guide.source = 'edited'
      guide.attribution = null
      guide.revision += 1
      guide.updated_at = new Date().toISOString()
      return HttpResponse.json(guide)
    }),
  ),

  // A note does not move the revision: the questions have not changed.
  http.put(
    `${API_BASE}/job-openings/:id/candidates/:candidateId/interview-guide/questions/:questionId/note`,
    guarded(async ({ params, request }) => {
      const guide = findGuide(params.id, params.candidateId)
      const question = guide?.questions.find((q) => q.id === params.questionId)
      if (!guide || !question) return error(404, 'question not found')
      const note = (await readJson<{ note: string }>(request)).note ?? ''
      if (note) question.note = note
      else delete question.note
      guide.updated_at = new Date().toISOString()
      return HttpResponse.json(guide)
    }),
  ),
]
