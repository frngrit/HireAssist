# System Overview

How HireAssist behaves at runtime: what each service holds, who calls whom, and the sequence of
calls behind each use case.

**This document is the runtime view, and it is not the source of truth for anything.** It defers
to three documents and must be corrected whenever any of them changes:

| For | See |
|---|---|
| The full operation list per service, and every collaboration | [SERVICE-OPERATIONS-COLLABORATORS.md](SERVICE-OPERATIONS-COLLABORATORS.md) |
| Use case descriptions, actors, main and alternate flows | [PROPOSAL.md](PROPOSAL.md) |
| Why any of this is shaped the way it is | [adr/](adr/) |

Every call below is **REST over HTTP/JSON** ([ADR-001](adr/ADR-001-service-decomposition.md)),
**except calls into the AI Service, which are gRPC**
([ADR-007](adr/ADR-007-grpc-for-the-ai-service.md)). There is no message broker
([ADR-002](adr/ADR-002-async-screening-pipeline.md)).

---

## The six services

### API Gateway

- **Owns** — nothing. No schema, no store.
- **Does** — terminates TLS, verifies the access token's signature and expiry itself against the
  Identity Service's public key, resolves the caller's role from it, rate-limits, routes. Rejects
  any request exceeding the caller's role.
- **Called by** — the recruiter-facing web application. The only public entry point for user
  traffic; the System Scheduler is an internal trigger and does not pass through it.
- **Calls** — no one to check a token: verification is local
  ([ADR-009](adr/ADR-009-jwt-with-refresh-tokens.md)), and the token is forwarded unchanged.
  `Compliance.recordAuthorisationRejected()` on every rejection; routes onward to Identity,
  Hiring, Compliance and Resume Processing. Never the AI Service.
- **It is the only place a token is checked.** The services behind it do not verify what they
  receive, on the assumption that they are reachable only on the deployment's private network.
  That decision reverses ADR-001's position that enforcement cannot live at the gateway alone,
  and has no record of its own yet.

### Identity Service

- **Owns** — PostgreSQL: members, their roles, sessions.
- **Does** — authenticates a member, issues and validates sessions, answers what role a caller
  holds, lets a member replace their password, and lets an Admin list, create, remove and re-role
  members and set a temporary password.
- **Called by** — the Gateway, for signing in and refreshing a token, and for member management.
  Not on every request: ADR-009 takes it off that path.
- **Calls** — nothing. It is the only service with no downstream at all.

### Hiring Service

- **Owns** — PostgreSQL: job openings, criteria, screening batches and per-entry status, scores,
  overrides, shortlist decisions. MongoDB: interview guides, in a database of its own.
  *(ADR-003 places AI-derived documents in MongoDB, which is otherwise Resume Processing's
  store; Hiring having its own is how that was implemented, and is still unrecorded.)*
- **Does** — holds the entire recruiter-facing hiring record: openings and their criteria, batch
  acceptance and per-resume handover, the ranked list, overrides, shortlisting, interview guides.
- **Called by** — the Gateway; Resume Processing, calling `reportScreeningResult()` back;
  Compliance, calling `eraseHiringData()`.
- **Calls** — Resume Processing (`submitResumeForScreening()`, `getCandidateProfile()`); the AI
  Service (`deriveCriteriaFromDescription()`, `generateInterviewQuestions()`);
  `Compliance.recordPipelineEvent()`; the Object Storage and Email adapters.

### Resume Processing Service

- **Owns** — PostgreSQL: the screening work table, candidate identity, the date the data was
  collected. MongoDB: candidate profiles, parsed resume text, per-criterion scoring output and
  justifications.
- **Does** — the per-resume pipeline. Records a handover durably and acknowledges immediately;
  N replicas claim work rows with `SELECT … FOR UPDATE SKIP LOCKED`; each parses the PDF in
  process, calls the model to score it, persists, and calls back. Classifies permanent against
  transient failure, backs off, and parks what will not recover. **This is the service that
  scales** — NFR-07, the demonstrated quality attribute, is delivered here.
- **Called by** — Hiring, for the handover and for profile reads; Compliance, for erasure and
  listing; the Gateway, for recruiter-facing reads. *(How those reads are routed is not yet
  stated — see Open questions.)*
- **Calls** — `AI.scoreAgainstCriteria()`; `Hiring.reportScreeningResult()`; the Object Storage
  adapter.

### AI Service

- **Owns** — no domain data. Prompts as configuration, the model credential as a secret.
- **Does** — the only place a model is ever called. Three operations, expressed in domain terms
  rather than model terms. Owns the token budget and rate limits, strips protected attributes
  before a prompt is built, and records the model version with every answer.
- **Called by** — Hiring and Resume Processing only, over gRPC. Not reachable through the Gateway,
  which is what makes a non-browser-friendly protocol acceptable on this boundary.
- **Calls** — the LLM adapter, to the managed provider. Nothing else.

### Compliance & Insights Service

- **Owns** — PostgreSQL: the retention policy, pipeline metrics, the erasure audit log (which
  names a candidate only by a pseudonymous identifier), and the access audit log.
- **Does** — the clock-driven, destructive half of the system: retention sweeps, erasure
  orchestrated across the services that hold the data and proved afterwards, staleness flagging,
  and the dashboard maintained as a projection rather than computed as a query.
- **Called by** — the Gateway; Hiring, pushing `recordPipelineEvent()`; the Gateway again, pushing
  `recordAuthorisationRejected()`; the System Scheduler.
- **Calls** — `Hiring.eraseHiringData()`; Resume Processing (`eraseCandidateProfile()`,
  `listCandidateProfiles()`); the Email adapter.

---

## Request flows

### UC-0 — Sign in

```
Browser → Gateway → Identity.authenticateMember()
                  ← session token
```

Every later request carries that token. The Gateway verifies it locally and reads the role from
it, with no call to Identity (ADR-009); an expired one is renewed through `POST /auth/refresh`. A
request above the caller's role stops at the Gateway, which calls
`Compliance.recordAuthorisationRejected()` for the access audit log.

### UC-1 — Create a job opening

```
Browser → Gateway → Hiring.proposeJobOpening()
                    Hiring  → AI.deriveCriteriaFromDescription() → LLM provider
                            ← proposed title and weighted criteria, uninterpretable parts
                    ← proposal (nothing stored)

[Recruiter reviews and edits in the browser — the correction point]

Browser → Gateway → Hiring.createJobOpening()      (confirmed title, criteria, time-to-fill)
                    Hiring  writes the opening and its criteria to PostgreSQL
                    Hiring  → Compliance.recordPipelineEvent()
```

Two operations, because the review sits between them: FR-1.3 requires the proposal to be shown
*before* the opening is created, and a request cannot stay open while a person edits. Proposing
stores nothing, so an abandoned proposal leaves no trace. Creating makes no AI call, so a Recruiter
who rejects the proposal or cannot get one (FR-1.5, flow 4a) still creates the opening by entering
criteria manually.

The review step is where a human corrects the model, before any resume has been scored against a
wrong criterion.

### UC-2 — Batch-screen resumes

Two halves. The first must return within two seconds (NFR-04); the second takes minutes.

**Accepting the batch**

```
Browser → Gateway → Hiring.submitScreeningBatch()
                    Hiring → ObjectStorage.storeResumeFile()          × N
                    Hiring writes the batch and one pending entry per resume
                    Hiring → ResumeProcessing.submitResumeForScreening()   × N
                             (batch id, resume id, file key, CRITERIA SNAPSHOT)
                           ← acknowledged once the work row is committed
                  ← batch id
```

The criteria snapshot travels in the request body. Resume Processing never calls back to fetch
criteria: that would add a synchronous dependency to the path built to have none, and a snapshot
pins one criteria version to one resume, so a mid-batch edit cannot invalidate a justification
already written.

**Screening each resume** — N replicas, working independently

```
worker claims a work row (SELECT … FOR UPDATE SKIP LOCKED)
   → ObjectStorage.fetchResumeFile()
   → extract text from the PDF in process     ← deterministic; no model sees a file
   → AI.extractProfileFromText() → LLM provider   ← cached: once per resume
   → AI.scoreAgainstCriteria() → LLM provider     ← once per resume per opening
   → persist the profile and the scoring output
   → Hiring.reportScreeningResult()           ← upsert on (batch_id, resume_id)
   → result streams to the ranked list
```

A **permanent** failure — a corrupt file, a scanned image with no text layer — is never retried;
the entry becomes *needs manual review* with the reason. A **transient** failure — 429, 5xx, a
broken connection, a scoring attempt past its 60-second bound — backs off and retries a bounded
number of times, then goes to a terminal failed state. A batch is complete when every entry is
terminal, which triggers the batch-finished notification.

**Reviewing the results**

```
Browser → Gateway → Hiring.getRankedResults()          ← Hiring's own columns, no document read
        opening one candidate:
                    Hiring → ResumeProcessing.getCandidateProfile()   ← the justification
        then        Hiring.overrideScore() · Hiring.shortlistCandidate()
```

### UC-3 — Generate interview questions

```
Browser → Gateway → Hiring.generateInterviewGuide()
                    Hiring → ResumeProcessing.getCandidateProfile()
                    Hiring → AI.generateInterviewQuestions() → LLM provider
                           ← questions, each tagged with the criterion it tests
                             and the resume evidence it came from
                    Hiring retains the guide
```

### UC-4 — Monitor pipeline and stale positions

```
Recruiter:   Browser   → Gateway → Compliance.getPipelineDashboard()
                                   served from Compliance's own counters
Scheduler:   Scheduler → Compliance.evaluateStaleness()
                         flags openings past their expected time-to-fill
                         → Email adapter → the responsible Recruiter
```

Paused openings are excluded from evaluation, so the alerts stay meaningful.

### UC-5 — Enforce candidate data retention

```
Admin:       Browser   → Gateway → Compliance.setRetentionPolicy()
Scheduler:   Scheduler → Compliance.evaluateRetention()
                         warning window  → Email adapter
                         on expiry, orchestrates erasure:
                            → Hiring.eraseHiringData()
                            → ResumeProcessing.eraseCandidateProfile()
                            → the resume file in object storage
                         writes the erasure audit entry (pseudonymous identifier only)
```

A candidate in an active hiring process has expiry held for a Recruiter to resolve. An erasure
that fails is reported as *deletion pending* and retried — never as erased.

### UC-6 — Manage members and roles

```
Admin → Gateway → Identity.listMembers() · getMember() · createMember() · removeMember()
                  · changeMemberRole() · setMemberPassword()
```

---

## Open questions this view exposes

Both are recorded elsewhere; they are repeated here because the runtime view is where they show.

- **Who routes Resume Processing's recruiter-facing reads.** `listUnparsableResumes()` and
  `getCandidateProfile()` are things a Recruiter looks at, but nothing states whether the Gateway
  routes to Resume Processing directly or Hiring proxies. See [CONTEXT.md](CONTEXT.md).
- **`recordPipelineEvent()` is a synchronous dependency** in Hiring's write path, and a dropped
  call leaves a permanently wrong dashboard number with nothing to reconcile against. It is the
  first place a message broker would earn its keep. See
  [SERVICE-OPERATIONS-COLLABORATORS.md](SERVICE-OPERATIONS-COLLABORATORS.md).
